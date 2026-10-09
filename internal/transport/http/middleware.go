package http

import (
	"fmt"
	"net/http"
	"regexp"
	"time"

	"cloudchat/internal/database"
	"github.com/gin-gonic/gin"
)

// contentSecurityPolicy for every page. Scripts, styles, fonts and
// connections are same-origin only; style-src allows inline styles because
// Vue sets style attributes. File downloads set a stricter policy themselves.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; font-src 'self'; connect-src 'self' ws: wss:; " +
	"object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"

// SecurityHeaders sets protective response headers on every response.
// https should be true when the site is served over HTTPS (even if TLS ends
// at a reverse proxy), which enables HSTS.
func SecurityHeaders(https bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		if https {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		c.Next()
	}
}

// Health reports whether the server can reach Redis (for monitoring and
// load balancers).
func Health(c *gin.Context) {
	if err := database.RDB.Ping(database.Ctx).Err(); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "redis unavailable"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// privateIDs matches room UUIDs and the random IDs of files and secrets.
var privateIDs = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}|[A-Za-z0-9_-]{22}`)

// AccessLog is gin's request log with room, file and secret IDs (and query
// strings, which carry nicknames and room IDs) removed, so logs can't be used
// to find or join anyone's conversation.
func AccessLog() gin.HandlerFunc {
	return gin.LoggerWithConfig(gin.LoggerConfig{
		SkipPaths: []string{"/healthz"},
		Formatter: func(p gin.LogFormatterParams) string {
			return fmt.Sprintf("[GIN] %s | %3d | %10v | %15s | %-7s %s\n",
				p.TimeStamp.Format(time.DateTime), p.StatusCode, p.Latency.Round(time.Microsecond),
				p.ClientIP, p.Method, privateIDs.ReplaceAllString(p.Request.URL.Path, ":id"))
		},
	})
}
