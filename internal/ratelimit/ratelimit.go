// Package ratelimit protects the service from abuse: per-client request
// limits shared across servers (Redis), per-connection message throttling,
// per-IP connection caps, and a guard that stops new uploads when Redis
// storage runs high.
package ratelimit

import (
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cloudchat/internal/database"
	"cloudchat/internal/stats"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// Rule allows Limit units (requests, or bytes) per Window for each client.
type Rule struct {
	Name   string
	Limit  int64
	Window time.Duration
}

// Limiter enforces Rules with fixed-window counters in Redis, so limits hold
// across all server instances.
type Limiter struct {
	enabled bool
}

func New(enabled bool) *Limiter { return &Limiter{enabled: enabled} }

// allowLua adds cost to the window counter; if that would exceed the limit it
// rolls the cost back and refuses. Returns {allowed, window ms remaining}.
var allowLua = redis.NewScript(`
local n = redis.call('INCRBY', KEYS[1], ARGV[1])
local ttl = redis.call('PTTL', KEYS[1])
if ttl < 0 then
  redis.call('PEXPIRE', KEYS[1], ARGV[2])
  ttl = tonumber(ARGV[2])
end
if n > tonumber(ARGV[3]) then
  redis.call('DECRBY', KEYS[1], ARGV[1])
  return {0, ttl}
end
return {1, ttl}
`)

// Allow consumes cost units of rule for key. On refusal it returns how long
// until the window resets. Redis errors fail open: availability matters more
// than strictness for a blip.
func (l *Limiter) Allow(rule Rule, key string, cost int64) (bool, time.Duration) {
	if !l.enabled {
		return true, 0
	}
	res, err := allowLua.Run(database.Ctx, database.RDB, []string{"rl:" + rule.Name + ":" + key},
		cost, rule.Window.Milliseconds(), rule.Limit).Int64Slice()
	if err != nil || len(res) != 2 {
		log.Printf("Rate limit check %s failed (allowing): %v", rule.Name, err)
		return true, 0
	}
	return res[0] == 1, time.Duration(res[1]) * time.Millisecond
}

// Middleware refuses the request with 429 when any rule is exhausted.
func (l *Limiter) Middleware(rules ...Rule) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := ClientKey(c)
		for _, r := range rules {
			if ok, retry := l.Allow(r, key, 1); !ok {
				Reject(c, retry)
				return
			}
		}
		c.Next()
	}
}

// Reject aborts with 429 Too Many Requests and a Retry-After header.
func Reject(c *gin.Context, retry time.Duration) {
	stats.Inc(stats.RateLimited)
	secs := int(math.Ceil(retry.Seconds()))
	if secs < 1 {
		secs = 1
	}
	c.Header("Retry-After", strconv.Itoa(secs))
	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
		"message":    "Too many requests. Please try again in " + humanSeconds(secs) + ".",
		"retryAfter": secs,
	})
}

func humanSeconds(s int) string {
	switch {
	case s < 60:
		return plural(s, "second")
	case s < 3600:
		return plural((s+59)/60, "minute")
	default:
		return plural((s+3599)/3600, "hour")
	}
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// ClientKey identifies the client for limiting: its IPv4 address, or its
// IPv6 /64 network (one subscriber usually controls a whole /64). The IP
// comes from gin's ClientIP, which honours X-Forwarded-For only from the
// configured trusted proxies.
func ClientKey(c *gin.Context) string { return NormalizeIP(c.ClientIP()) }

func NormalizeIP(s string) string {
	ip := net.ParseIP(strings.TrimSpace(s))
	if ip == nil {
		return "unknown"
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

// TokenBucket throttles a single stream (e.g. one connection's messages):
// bursts up to Capacity, refilling at Rate tokens per second. Not safe for
// concurrent use.
type TokenBucket struct {
	Capacity float64
	Rate     float64
	tokens   float64
	last     time.Time
}

func NewTokenBucket(capacity, ratePerSec float64) *TokenBucket {
	return &TokenBucket{Capacity: capacity, Rate: ratePerSec, tokens: capacity}
}

func (b *TokenBucket) Allow(now time.Time) bool {
	if !b.last.IsZero() {
		b.tokens = math.Min(b.Capacity, b.tokens+now.Sub(b.last).Seconds()*b.Rate)
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// ConnCounter caps concurrent connections per client on this server.
type ConnCounter struct {
	max int
	mu  sync.Mutex
	n   map[string]int
}

func NewConnCounter(max int) *ConnCounter { return &ConnCounter{max: max, n: map[string]int{}} }

// Acquire reserves a slot for key; callers must Release it when done.
func (cc *ConnCounter) Acquire(key string) bool {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	if cc.max > 0 && cc.n[key] >= cc.max {
		return false
	}
	cc.n[key]++
	return true
}

func (cc *ConnCounter) Release(key string) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	if cc.n[key] <= 1 {
		delete(cc.n, key)
	} else {
		cc.n[key]--
	}
}

// StorageGuard watches Redis memory use and reports when it reaches the
// configured limit, so new rooms, files and secrets can be refused before
// Redis runs out of memory.
type StorageGuard struct {
	limit      int64
	filesLimit int64
	filesUsed  func() int64
	used       atomic.Int64
}

// NewStorageGuard starts polling Redis. Full reports when Redis memory use
// reaches limitBytes; FilesFull also reports when the files stored on disk
// (filesUsed) reach filesLimitBytes. A limit <= 0 disables that check.
func NewStorageGuard(limitBytes, filesLimitBytes int64, filesUsed func() int64) *StorageGuard {
	g := &StorageGuard{limit: limitBytes, filesLimit: filesLimitBytes, filesUsed: filesUsed}
	if limitBytes <= 0 {
		return g
	}
	g.refresh()
	go func() {
		for range time.Tick(5 * time.Second) {
			g.refresh()
		}
	}()
	return g
}

func (g *StorageGuard) refresh() {
	info, err := database.RDB.Info(database.Ctx, "memory").Result()
	if err != nil {
		log.Printf("Storage guard: reading Redis memory failed: %v", err)
		return
	}
	for _, line := range strings.Split(info, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "used_memory:"); ok {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				g.used.Store(n)
			}
		}
	}
}

// Full reports whether Redis memory use has reached the overall limit.
func (g *StorageGuard) Full() bool { return g.limit > 0 && g.used.Load() >= g.limit }

// FilesFull reports whether new file uploads should be refused: the files
// on disk have reached their limit, or Redis is full.
func (g *StorageGuard) FilesFull() bool {
	return g.Full() || (g.filesLimit > 0 && g.filesUsed != nil && g.filesUsed() >= g.filesLimit)
}
