package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"cloudchat/internal/database"
	"cloudchat/internal/models"
	"cloudchat/internal/ratelimit"
	"cloudchat/internal/service"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 8192
	// Each server refreshes its connections' presence every heartbeatPeriod;
	// members not refreshed for presenceStale (e.g. their server crashed)
	// are dropped from the room.
	heartbeatPeriod = 15 * time.Second
	presenceStale   = 45 * time.Second
	// MaxContentLength is the maximum number of characters in a chat message.
	MaxContentLength = 2000

	channelPrefix = "room:"
)

// RoomKey is the Redis key marking a room as existing.
func RoomKey(roomID string) string { return "room:" + roomID + ":meta" }

func membersKey(roomID string) string { return "room:" + roomID + ":members" }

func clientKey(token string) string { return "client:" + token }

func historyKey(roomID string) string { return "room:" + roomID + ":history" }

// seenKey is a sorted set of member tokens scored by last heartbeat (ms).
func seenKey(roomID string) string { return "room:" + roomID + ":seen" }

// seqKey is the room's message counter; each published message takes the
// next number. Ordering by it is immune to server clock jumps.
func seqKey(roomID string) string { return "room:" + roomID + ":seq" }

// joinedKey maps each identity (sender ID) to the message seq current when
// it first joined; it only sees history after that point (unless allowed).
func joinedKey(roomID string) string { return "room:" + roomID + ":joinedseq" }

type Client struct {
	Hub   *Hub
	ID    string
	Token string
	Room  string
	Conn  *websocket.Conn
	Send  chan []byte
	Name  string
	// JoinedSeq is the room's message seq when this identity first joined
	// (set by RegisterClient); earlier history is hidden unless allowed.
	JoinedSeq int64
	member    string // JSON of models.Member, kept for presence heartbeats
	// Throttle limits how fast this connection may send messages (optional).
	Throttle *ratelimit.TokenBucket
	// OnClose runs once when the connection ends (optional).
	OnClose     func()
	lastWarning time.Time
}

// notify sends a server notice to this client only, without blocking.
func (c *Client) notify(text string) {
	payload, _ := json.Marshal(models.Message{Type: "error", Content: text, Time: time.Now()})
	c.Hub.Mu.Lock()
	defer c.Hub.Mu.Unlock()
	if _, ok := c.Hub.Rooms[c.Room][c]; !ok {
		return // already unregistered; Send is closed
	}
	select {
	case c.Send <- payload:
	default:
	}
}

type Hub struct {
	Rooms map[string]map[*Client]bool
	Mu    sync.Mutex
	// presenceMu serialises membership scripts (join/leave/heartbeat) from
	// this server, so a heartbeat can't re-add someone who just left.
	presenceMu sync.Mutex
	// RoomTTL is how long an idle room stays joinable.
	RoomTTL time.Duration
	// EmptyRoomTTL is how long a room and its data survive once everyone
	// has left (long enough to cover a page reload).
	EmptyRoomTTL time.Duration
	// HistoryLimit is how many recent messages are kept per room for
	// clients that (re)join; 0 disables history.
	HistoryLimit int
	// HistoryForNewMembers lets people see messages sent before they first
	// joined; when false, each identity only sees history from its first join.
	HistoryForNewMembers bool
	Upgrader             websocket.Upgrader
}

type Options struct {
	RoomTTL              time.Duration
	EmptyRoomTTL         time.Duration
	HistoryLimit         int
	HistoryForNewMembers bool
	// AllowedOrigins may open WebSockets in addition to the same origin
	// ("*" allows any origin).
	AllowedOrigins []string
}

// NewHub creates a hub with a single Redis pattern subscription covering all
// rooms, so there is no per-room subscription to leak or duplicate.
func NewHub(opts Options) *Hub {
	h := &Hub{
		Rooms:                make(map[string]map[*Client]bool),
		RoomTTL:              opts.RoomTTL,
		EmptyRoomTTL:         opts.EmptyRoomTTL,
		HistoryLimit:         opts.HistoryLimit,
		HistoryForNewMembers: opts.HistoryForNewMembers,
		Upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin:     originChecker(opts.AllowedOrigins),
		},
	}

	pubsub := database.RDB.PSubscribe(database.Ctx, channelPrefix+"*")
	// Wait for the subscription to be confirmed so no early messages are lost.
	if _, err := pubsub.Receive(database.Ctx); err != nil {
		log.Fatalf("Could not subscribe to room channels: %v", err)
	}

	go func() {
		for msg := range pubsub.Channel() {
			h.broadcast(strings.TrimPrefix(msg.Channel, channelPrefix), []byte(msg.Payload))
		}
	}()

	go h.heartbeatLoop()

	return h
}

func originChecker(allowed []string) func(r *http.Request) bool {
	allowAll := false
	set := make(map[string]bool, len(allowed))
	for _, o := range allowed {
		if o == "*" {
			allowAll = true
		}
		set[strings.ToLower(o)] = true
	}
	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" || allowAll {
			return true
		}
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}
		if strings.EqualFold(u.Host, r.Host) {
			return true
		}
		return set[strings.ToLower(u.Scheme+"://"+u.Host)]
	}
}

func (h *Hub) broadcast(roomID string, payload []byte) {
	h.Mu.Lock()
	defer h.Mu.Unlock()
	for client := range h.Rooms[roomID] {
		select {
		case client.Send <- payload:
		default:
		}
	}
}

func (h *Hub) RegisterClient(c *Client) {
	h.presenceMu.Lock()
	defer h.presenceMu.Unlock()
	h.Mu.Lock()
	if h.Rooms[c.Room] == nil {
		h.Rooms[c.Room] = make(map[*Client]bool)
	}
	h.Rooms[c.Room][c] = true
	h.Mu.Unlock()

	// Members are keyed by connection token: one identity may have several
	// tabs open. Joining also restores the full TTL if the room was emptying.
	member, _ := json.Marshal(models.Member{ID: c.ID, Name: c.Name})
	c.member = string(member)
	joinedSeq, err := joinRoomLua.Run(database.Ctx, database.RDB, roomDataKeys(c.Room),
		c.Token, c.member, c.Hub.RoomTTL.Milliseconds(), service.FileKeyPrefix(c.Room), channelPrefix+c.Room,
		time.Now().UnixMilli(), presenceStale.Milliseconds(), clientKey(""), c.ID).Int64()
	if err != nil {
		log.Printf("Failed to register member in room %s: %v", c.Room, err)
		joinedSeq, _ = database.RDB.Get(database.Ctx, seqKey(c.Room)).Int64()
	}
	c.JoinedSeq = joinedSeq
	ck := clientKey(c.Token)
	database.RDB.HMSet(database.Ctx, ck, "id", c.ID, "name", c.Name, "room", c.Room)
	database.RDB.Expire(database.Ctx, ck, c.Hub.RoomTTL)
}

// ClientIdentity is a connected client resolved from its token.
type ClientIdentity struct {
	ID   string
	Name string
	Room string
}

// LookupClient resolves a connection token issued in the welcome message.
// It works across server instances because the mapping lives in Redis.
func LookupClient(token string) (*ClientIdentity, bool) {
	if token == "" {
		return nil, false
	}
	vals, err := database.RDB.HMGet(database.Ctx, clientKey(token), "id", "name", "room").Result()
	if err != nil {
		return nil, false
	}
	id, _ := vals[0].(string)
	name, _ := vals[1].(string)
	room, _ := vals[2].(string)
	if id == "" || room == "" {
		return nil, false
	}
	return &ClientIdentity{ID: id, Name: name, Room: room}, true
}

// Publish broadcasts a message to every client in the room, on all
// instances, and appends it to the room's history.
func (h *Hub) Publish(roomID string, msg models.Message) error {
	if msg.ID == "" {
		msg.ID = uuid.New().String()
	}
	seq, err := database.RDB.Incr(database.Ctx, seqKey(roomID)).Result()
	if err != nil {
		return err
	}
	msg.Seq = seq
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = database.RDB.TxPipelined(database.Ctx, func(p redis.Pipeliner) error {
		if h.HistoryLimit > 0 {
			hk := historyKey(roomID)
			p.RPush(database.Ctx, hk, payload)
			p.LTrim(database.Ctx, hk, int64(-h.HistoryLimit), -1)
			p.Expire(database.Ctx, hk, h.RoomTTL)
		}
		p.Publish(database.Ctx, channelPrefix+roomID, payload)
		p.Expire(database.Ctx, RoomKey(roomID), h.RoomTTL)
		p.Expire(database.Ctx, seqKey(roomID), h.RoomTTL)
		return nil
	})
	return err
}

// History returns the room's recent messages visible to c, oldest first.
func (h *Hub) History(c *Client) []models.Message {
	roomID := c.Room
	if h.HistoryLimit <= 0 {
		return nil
	}
	raw, err := database.RDB.LRange(database.Ctx, historyKey(roomID), int64(-h.HistoryLimit), -1).Result()
	if err != nil {
		log.Printf("Failed to load history for room %s: %v", roomID, err)
		return nil
	}
	msgs := make([]models.Message, 0, len(raw))
	for _, r := range raw {
		var m models.Message
		if json.Unmarshal([]byte(r), &m) != nil {
			continue
		}
		if !h.HistoryForNewMembers && m.Seq <= c.JoinedSeq {
			continue
		}
		msgs = append(msgs, m)
	}
	return msgs
}

// UnregisterClient removes a client; it is safe to call more than once.
func (h *Hub) UnregisterClient(c *Client) {
	h.Mu.Lock()
	_, ok := h.Rooms[c.Room][c]
	if ok {
		delete(h.Rooms[c.Room], c)
		close(c.Send)
		if len(h.Rooms[c.Room]) == 0 {
			delete(h.Rooms, c.Room)
		}
	}
	h.Mu.Unlock()
	if !ok {
		return
	}

	h.presenceMu.Lock()
	defer h.presenceMu.Unlock()
	err := leaveRoomLua.Run(database.Ctx, database.RDB, roomDataKeys(c.Room),
		c.Token, c.Hub.EmptyRoomTTL.Milliseconds(), service.FileKeyPrefix(c.Room), channelPrefix+c.Room,
		time.Now().UnixMilli(), presenceStale.Milliseconds(), clientKey("")).Err()
	if err != nil {
		log.Printf("Failed to unregister member in room %s: %v", c.Room, err)
	}
	database.RDB.Del(database.Ctx, clientKey(c.Token))
}

// roomDataKeys lists every key holding a room's data. Members and the file-ID
// set come first, as the scripts below expect.
func roomDataKeys(roomID string) []string {
	return []string{
		membersKey(roomID),
		service.RoomFilesKey(roomID),
		RoomKey(roomID),
		historyKey(roomID),
		service.RoomFileBytesKey(roomID),
		seenKey(roomID),
		joinedKey(roomID),
		seqKey(roomID),
	}
}

// Shutdown removes every local client from its room (so presence and the
// empty-room countdown stay correct) and closes their connections. Call it
// on graceful shutdown.
func (h *Hub) Shutdown() {
	h.Mu.Lock()
	var clients []*Client
	for _, room := range h.Rooms {
		for c := range room {
			clients = append(clients, c)
		}
	}
	h.Mu.Unlock()
	for _, c := range clients {
		h.UnregisterClient(c) // closes Send, so WritePump sends a close frame
	}
	if len(clients) > 0 {
		time.Sleep(300 * time.Millisecond) // let close frames flush
	}
	log.Printf("Disconnected %d client(s)", len(clients))
}

// heartbeatLoop periodically marks this server's clients as alive and drops
// members whose heartbeats stopped (their server died without cleanup).
func (h *Hub) heartbeatLoop() {
	ticker := time.NewTicker(heartbeatPeriod)
	defer ticker.Stop()
	for range ticker.C {
		h.heartbeat()
	}
}

func (h *Hub) heartbeat() {
	h.Mu.Lock()
	roomIDs := make([]string, 0, len(h.Rooms))
	for roomID := range h.Rooms {
		roomIDs = append(roomIDs, roomID)
	}
	h.Mu.Unlock()

	for _, roomID := range roomIDs {
		h.heartbeatRoom(roomID)
	}
}

// heartbeatRoom refreshes (and if missing, restores) this server's members
// of one room. Restoring makes presence self-healing, e.g. after a clock
// jump made live members look stale, or Redis lost the data.
func (h *Hub) heartbeatRoom(roomID string) {
	h.presenceMu.Lock()
	defer h.presenceMu.Unlock()

	h.Mu.Lock()
	var live [][2]string // {token, member JSON}
	for c := range h.Rooms[roomID] {
		live = append(live, [2]string{c.Token, c.member})
	}
	h.Mu.Unlock()
	if len(live) == 0 {
		return
	}
	liveJSON, _ := json.Marshal(live)
	err := heartbeatLua.Run(database.Ctx, database.RDB, roomDataKeys(roomID),
		time.Now().UnixMilli(), presenceStale.Milliseconds(), clientKey(""), channelPrefix+roomID, liveJSON).Err()
	if err != nil {
		log.Printf("Presence heartbeat failed for room %s: %v", roomID, err)
	}
}

// publishPresenceLua is shared by the join/leave scripts: it publishes the
// room's distinct members (one entry per identity, however many tabs) as a
// "presence" message. Publishing inside the script keeps presence updates in
// the same order as the membership changes that caused them.
const publishPresenceLua = `
local function publishPresence(membersKey, channel)
  local seen, list = {}, {}
  for _, v in ipairs(redis.call('HVALS', membersKey)) do
    local ok, m = pcall(cjson.decode, v)
    if ok and type(m) == 'table' and m.id and not seen[m.id] then
      seen[m.id] = true
      list[#list + 1] = {id = m.id, name = m.name}
    end
  end
  if #list > 0 then
    redis.call('PUBLISH', channel, cjson.encode({type = 'presence', members = list}))
  end
end

-- sweep drops members whose heartbeat is older than staleMs, and members
-- with no heartbeat at all (left behind by older versions). Returns the
-- number removed.
local function sweep(membersKey, seenKey, now, staleMs, clientPrefix)
  local removed = 0
  local cutoff = tonumber(now) - tonumber(staleMs)
  for _, tok in ipairs(redis.call('ZRANGEBYSCORE', seenKey, '-inf', '(' .. cutoff)) do
    redis.call('ZREM', seenKey, tok)
    removed = removed + redis.call('HDEL', membersKey, tok)
    redis.call('DEL', clientPrefix .. tok)
  end
  for _, tok in ipairs(redis.call('HKEYS', membersKey)) do
    if not redis.call('ZSCORE', seenKey, tok) then
      removed = removed + redis.call('HDEL', membersKey, tok)
      redis.call('DEL', clientPrefix .. tok)
    end
  end
  return removed
end
`

// joinRoomLua adds a member, (re)sets every room key, including each file,
// to the full room TTL, and publishes presence.
// It records the identity's first-join time and returns it (ms).
// KEYS: roomDataKeys. ARGV: member token, member JSON, TTL ms, file key
// prefix, channel, now ms, stale ms, client key prefix, sender ID.
var joinRoomLua = redis.NewScript(publishPresenceLua + `
redis.call('HSET', KEYS[1], ARGV[1], ARGV[2])
redis.call('HSETNX', KEYS[7], ARGV[9], redis.call('GET', KEYS[8]) or '0')
redis.call('ZADD', KEYS[6], ARGV[6], ARGV[1])
sweep(KEYS[1], KEYS[6], ARGV[6], ARGV[7], ARGV[8])
for i = 1, #KEYS do redis.call('PEXPIRE', KEYS[i], ARGV[3]) end
for _, id in ipairs(redis.call('SMEMBERS', KEYS[2])) do
  redis.call('PEXPIRE', ARGV[4] .. id, ARGV[3])
end
publishPresence(KEYS[1], ARGV[5])
return tonumber(redis.call('HGET', KEYS[7], ARGV[9]))
`)

// leaveRoomLua removes a member and publishes presence; if nobody is left
// (on any server), it shortens every room key, including each file, to the
// empty-room TTL.
// KEYS: roomDataKeys. ARGV: member token, empty-room TTL ms, file key
// prefix, channel, now ms, stale ms, client key prefix.
var leaveRoomLua = redis.NewScript(publishPresenceLua + `
redis.call('HDEL', KEYS[1], ARGV[1])
redis.call('ZREM', KEYS[6], ARGV[1])
sweep(KEYS[1], KEYS[6], ARGV[5], ARGV[6], ARGV[7])
if redis.call('HLEN', KEYS[1]) > 0 then
  publishPresence(KEYS[1], ARGV[4])
  return 0
end
local ttl = tonumber(ARGV[2])
local function shorten(k)
  local cur = redis.call('PTTL', k)
  if cur == -1 or cur > ttl then redis.call('PEXPIRE', k, ttl) end
end
for i = 2, #KEYS do shorten(KEYS[i]) end
for _, id in ipairs(redis.call('SMEMBERS', KEYS[2])) do shorten(ARGV[3] .. id) end
return 1
`)

// heartbeatLua marks this server's live members of a room as seen, restores
// any that went missing, sweeps stale ones, and publishes presence if the
// member list changed.
// KEYS: roomDataKeys. ARGV: now ms, stale ms, client key prefix, channel,
// JSON array of [token, member JSON] for this server's live members.
var heartbeatLua = redis.NewScript(publishPresenceLua + `
local changed = false
for _, m in ipairs(cjson.decode(ARGV[5])) do
  if redis.call('HSETNX', KEYS[1], m[1], m[2]) == 1 then changed = true end
  redis.call('ZADD', KEYS[6], ARGV[1], m[1])
end
if sweep(KEYS[1], KEYS[6], ARGV[1], ARGV[2], ARGV[3]) > 0 then changed = true end
if changed then publishPresence(KEYS[1], ARGV[4]) end
return 1
`)

func (c *Client) ReadPump() {
	defer func() {
		c.Hub.UnregisterClient(c)
		c.Conn.Close()
		if c.OnClose != nil {
			c.OnClose()
		}
	}()
	c.Conn.SetReadLimit(maxMessageSize)
	c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	c.Conn.SetPongHandler(func(string) error { c.Conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			break
		}

		var in models.Message
		if err := json.Unmarshal(message, &in); err != nil {
			continue
		}
		content := strings.TrimSpace(in.Content)
		if content == "" || utf8.RuneCountInString(content) > MaxContentLength {
			continue
		}
		if now := time.Now(); c.Throttle != nil && !c.Throttle.Allow(now) {
			if now.Sub(c.lastWarning) > 2*time.Second {
				c.lastWarning = now
				c.notify("You're sending messages too fast. Some were not delivered.")
			}
			continue
		}
		// Only the content comes from the client; everything else is set by
		// the server so clients can't forge identity or file messages.
		msg := models.Message{
			Type:     "chat",
			Sender:   c.Name,
			SenderID: c.ID,
			Content:  content,
			Time:     time.Now(),
		}
		if err := c.Hub.Publish(c.Room, msg); err != nil {
			log.Printf("Failed to publish message to room %s: %v", c.Room, err)
		}
	}
}

func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.Conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			n := len(c.Send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-c.Send)
			}

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
