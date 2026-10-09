package http

import (
	"net/http"
	"time"

	"cloudchat/internal/ratelimit"
	"github.com/gin-gonic/gin"
)

// Per-client (IP, or IPv6 /64) limits. Generous for people, tight for
// scripts. Counters live in Redis, so they apply across all servers.
var (
	LimitRoomCreate     = ratelimit.Rule{Name: "room-create", Limit: 10, Window: time.Minute}
	LimitRoomCreateDay  = ratelimit.Rule{Name: "room-create-day", Limit: 200, Window: 24 * time.Hour}
	LimitRoomLookup     = ratelimit.Rule{Name: "room-lookup", Limit: 60, Window: time.Minute}
	LimitWSConnect      = ratelimit.Rule{Name: "ws-connect", Limit: 30, Window: time.Minute}
	LimitUpload         = ratelimit.Rule{Name: "upload", Limit: 30, Window: 10 * time.Minute}
	LimitUploadBytes    = ratelimit.Rule{Name: "upload-bytes", Limit: 100 << 20, Window: time.Hour}
	LimitUploadBytesDay = ratelimit.Rule{Name: "upload-bytes-day", Limit: 300 << 20, Window: 24 * time.Hour}
	LimitDownload       = ratelimit.Rule{Name: "download", Limit: 300, Window: time.Minute}
	LimitSecretCreate   = ratelimit.Rule{Name: "secret-create", Limit: 20, Window: 10 * time.Minute}
	LimitSecretDay      = ratelimit.Rule{Name: "secret-create-day", Limit: 200, Window: 24 * time.Hour}
	LimitSecretLookup   = ratelimit.Rule{Name: "secret-lookup", Limit: 60, Window: time.Minute}
	LimitSecretReveal   = ratelimit.Rule{Name: "secret-reveal", Limit: 30, Window: time.Minute}

	// MaxConnsPerClient caps concurrent WebSocket connections per client on
	// one server.
	MaxConnsPerClient = 20
)

// Protection bundles the abuse defences the handlers use.
type Protection struct {
	Limiter *ratelimit.Limiter
	Conns   *ratelimit.ConnCounter
	Storage *ratelimit.StorageGuard
}

// RateLimit returns middleware enforcing the given rules for the client.
func (h *Handler) RateLimit(rules ...ratelimit.Rule) gin.HandlerFunc {
	return h.Protect.Limiter.Middleware(rules...)
}

// storageFull refuses the request with 503 when Redis storage is at its limit.
func (h *Handler) storageFull(c *gin.Context) bool {
	if !h.Protect.Storage.Full() {
		return false
	}
	c.Header("Retry-After", "300")
	c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
		"message": "CloudChat is at capacity right now. Please try again in a few minutes.",
	})
	return true
}

// filesFull refuses an upload with 503 when file storage is at its limit.
// Chat, rooms and secrets keep working; only file sharing pauses.
func (h *Handler) filesFull(c *gin.Context) bool {
	if !h.Protect.Storage.FilesFull() {
		return false
	}
	c.Header("Retry-After", "600")
	c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
		"message": "File sharing is busy right now. Please try again later — chat still works.",
	})
	return true
}
