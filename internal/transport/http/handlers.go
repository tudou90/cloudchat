package http

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"cloudchat/internal/database"
	"cloudchat/internal/models"
	"cloudchat/internal/ratelimit"
	"cloudchat/internal/service"
	"cloudchat/internal/transport/ws"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	// identityCookie holds a random per-browser secret from which a stable,
	// unforgeable sender ID is derived, so "my" messages stay mine across
	// page reloads. It is httpOnly: page scripts never see it.
	identityCookie       = "cc_identity"
	identityCookieMaxAge = 365 * 24 * 60 * 60

	maxNameLength = 20
	// maxSecretBody bounds secret request bodies (base64 ciphertext plus fields).
	maxSecretBody = 128 * 1024
)

type Handler struct {
	Hub     *ws.Hub
	Protect Protection
}

func NewHandler(hub *ws.Hub, protect Protection) *Handler {
	return &Handler{Hub: hub, Protect: protect}
}

// ensureIdentity sets the identity cookie if the browser doesn't have one yet.
// It is called on the room endpoints the client hits before opening a WebSocket.
func ensureIdentity(c *gin.Context) {
	if v, err := c.Cookie(identityCookie); err == nil && len(v) == 64 {
		return
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(identityCookie, hex.EncodeToString(raw), identityCookieMaxAge, "/", "", c.Request.TLS != nil, true)
}

// senderID derives the public sender ID from the identity cookie, falling
// back to a random per-connection ID when the cookie is missing.
func senderID(c *gin.Context) string {
	v, err := c.Cookie(identityCookie)
	if err != nil || len(v) != 64 {
		return uuid.New().String()
	}
	sum := sha256.Sum256([]byte("cloudchat-sender:" + v))
	return hex.EncodeToString(sum[:16])
}

func (h *Handler) CreateRoom(c *gin.Context) {
	if h.storageFull(c) {
		return
	}
	ensureIdentity(c)
	roomID := uuid.New().String()
	if err := database.RDB.Set(database.Ctx, ws.RoomKey(roomID), 1, h.Hub.RoomTTL).Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to create room"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": roomID})
}

func (h *Handler) GetRoom(c *gin.Context) {
	ensureIdentity(c)
	roomID := c.Param("id")
	if !h.roomExists(roomID) {
		c.JSON(http.StatusNotFound, gin.H{"message": "Room not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": roomID})
}

func (h *Handler) roomExists(roomID string) bool {
	if _, err := uuid.Parse(roomID); err != nil {
		return false
	}
	n, err := database.RDB.Exists(database.Ctx, ws.RoomKey(roomID)).Result()
	return err == nil && n > 0
}

func (h *Handler) ServeWS(c *gin.Context) {
	roomID := c.Param("roomID")
	if !h.roomExists(roomID) {
		c.JSON(http.StatusNotFound, gin.H{"message": "Room not found"})
		return
	}

	name := strings.TrimSpace(c.Query("name"))
	if name == "" {
		name = "Anonymous"
	}
	if utf8.RuneCountInString(name) > maxNameLength {
		name = string([]rune(name)[:maxNameLength])
	}

	clientKey := ratelimit.ClientKey(c)
	if !h.Protect.Conns.Acquire(clientKey) {
		c.JSON(http.StatusTooManyRequests, gin.H{"message": "Too many open connections from your network"})
		return
	}
	conn, err := h.Hub.Upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.Protect.Conns.Release(clientKey)
		return
	}

	client := &ws.Client{
		Hub:   h.Hub,
		ID:    senderID(c),
		Token: uuid.New().String(),
		Room:  roomID,
		Conn:  conn,
		Send:  make(chan []byte, 256),
		Name:  name,
		// Bursts of 10 messages, then 2 per second.
		Throttle: ratelimit.NewTokenBucket(10, 2),
		OnClose:  func() { h.Protect.Conns.Release(clientKey) },
	}

	// Tell the client its server-assigned identity before any chat traffic.
	welcome, _ := json.Marshal(models.Message{Type: "welcome", Sender: name, SenderID: client.ID, Token: client.Token})
	client.Send <- welcome

	// Register before loading history so nothing published in between is
	// missed; the client de-duplicates by message ID.
	h.Hub.RegisterClient(client)
	if msgs := h.Hub.History(client); len(msgs) > 0 {
		history, _ := json.Marshal(models.Message{Type: "history", Messages: msgs})
		client.Send <- history
	}

	go client.WritePump()
	go client.ReadPump()
}

// UploadFile stores a file in the room and announces it to the room as a
// "file" message. The uploader is identified by the connection token from
// their WebSocket welcome message.
func (h *Handler) UploadFile(c *gin.Context) {
	roomID := c.Param("id")
	ident, ok := ws.LookupClient(c.GetHeader("X-Client-Token"))
	if !ok || ident.Room != roomID {
		c.JSON(http.StatusForbidden, gin.H{"message": "Join the session before uploading"})
		return
	}

	// Allow some room for multipart framing around the file itself.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, service.MaxFileSize+(1<<20))
	if h.storageFull(c) {
		return
	}
	fh, err := c.FormFile("file")
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			fileError(c, service.ErrFileTooLarge)
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"message": "Missing file"})
		return
	}
	if fh.Size > service.MaxFileSize {
		fileError(c, service.ErrFileTooLarge)
		return
	}
	if ok, retry := h.Protect.Limiter.Allow(LimitUploadBytes, ratelimit.ClientKey(c), fh.Size); !ok {
		ratelimit.Reject(c, retry)
		return
	}
	f, err := fh.Open()
	if err != nil {
		fileError(c, err)
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, service.MaxFileSize+1))
	if err != nil {
		fileError(c, err)
		return
	}

	info, err := service.SaveFile(roomID, fh.Filename, data, h.Hub.RoomTTL)
	if err != nil {
		fileError(c, err)
		return
	}
	info.URL = fmt.Sprintf("/api/rooms/%s/files/%s", roomID, info.ID)

	msg := models.Message{Type: "file", Sender: ident.Name, SenderID: ident.ID, File: info, Time: time.Now()}
	if err := h.Hub.Publish(roomID, msg); err != nil {
		log.Printf("Failed to announce file in room %s: %v", roomID, err)
	}
	c.JSON(http.StatusOK, info)
}

func (h *Handler) DownloadFile(c *gin.Context) {
	roomID, fileID := c.Param("id"), c.Param("fileId")
	if _, err := uuid.Parse(roomID); err != nil || !service.ValidFileID(fileID) {
		fileError(c, service.ErrFileNotFound)
		return
	}
	f, err := service.GetFile(roomID, fileID)
	if err != nil {
		fileError(c, err)
		return
	}

	// Only known raster images render inline; anything else (HTML, SVG,
	// scripts...) is forced to download so it can't run in our origin.
	disposition, contentType := "attachment", "application/octet-stream"
	if service.InlineImage(f.Mime) {
		disposition, contentType = "inline", f.Mime
	}
	if v := mime.FormatMediaType(disposition, map[string]string{"filename": f.Name}); v != "" {
		disposition = v
	}
	c.Header("Content-Disposition", disposition)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	c.Header("Cache-Control", "private, max-age=86400")
	c.Data(http.StatusOK, contentType, f.Data)
}

func fileError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrFileNotFound):
		c.JSON(http.StatusNotFound, gin.H{"message": "File not found or expired"})
	case errors.Is(err, service.ErrFileTooLarge):
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"message": fmt.Sprintf("Files must be %d MB or smaller", service.MaxFileSize>>20)})
	case errors.Is(err, service.ErrRoomQuota):
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"message": fmt.Sprintf("This session reached its %d MB file limit", service.MaxRoomFileBytes>>20)})
	case errors.Is(err, service.ErrFileEmpty):
		c.JSON(http.StatusBadRequest, gin.H{"message": "File is empty"})
	default:
		log.Printf("File operation failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Internal error"})
	}
}

func (h *Handler) CreateSecret(c *gin.Context) {
	if h.storageFull(c) {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSecretBody)
	var req models.CreateSecretRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request"})
		return
	}
	res, err := service.CreateSecret(&req)
	if err != nil {
		secretError(c, err, 0)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (h *Handler) GetSecret(c *gin.Context) {
	id := c.Param("id")
	if !service.ValidSecretID(id) {
		secretError(c, service.ErrSecretNotFound, 0)
		return
	}
	meta, err := service.GetSecretMeta(id)
	if err != nil {
		secretError(c, err, 0)
		return
	}
	c.JSON(http.StatusOK, meta)
}

func (h *Handler) RevealSecret(c *gin.Context) {
	id := c.Param("id")
	if !service.ValidSecretID(id) {
		secretError(c, service.ErrSecretNotFound, 0)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSecretBody)
	var req models.RevealSecretRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request"})
		return
	}
	secret, left, err := service.RevealSecret(id, req.Auth)
	if err != nil {
		secretError(c, err, left)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, secret)
}

func secretError(c *gin.Context, err error, attemptsLeft int) {
	var verr *service.ValidationError
	switch {
	case errors.As(err, &verr):
		c.JSON(http.StatusBadRequest, gin.H{"message": verr.Msg})
	case errors.Is(err, service.ErrSecretNotFound):
		c.JSON(http.StatusNotFound, gin.H{"message": "Secret not found, already read, or expired"})
	case errors.Is(err, service.ErrSecretDestroyed):
		c.JSON(http.StatusGone, gin.H{"message": "Secret destroyed after too many wrong attempts"})
	case errors.Is(err, service.ErrWrongPassword):
		c.JSON(http.StatusForbidden, gin.H{"message": "Wrong password", "attemptsLeft": attemptsLeft})
	default:
		log.Printf("Secret operation failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Internal error"})
	}
}
