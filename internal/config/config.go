package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config holds runtime settings, read from environment variables. Variables
// may also be set in a .env file (path overridable via ENV_FILE); values
// already present in the environment take precedence over the file.
type Config struct {
	// ServerAddr is the HTTP listen address (SERVER_ADDR, default ":8080").
	ServerAddr string
	// PublicURL is the site's public origin, used for canonical URLs, social
	// previews and the sitemap (PUBLIC_URL, e.g. "https://chat.example.com").
	// When empty it is derived from each request's Host header.
	PublicURL string
	// RedisAddr is the Redis host:port (REDIS_ADDR, default "localhost:6379").
	RedisAddr string
	// RedisPassword is the Redis password (REDIS_PASSWORD, default empty).
	RedisPassword string
	// RedisDB is the Redis database number (REDIS_DB, default 0).
	RedisDB int
	// AllowedOrigins lists extra origins allowed to open WebSockets
	// (ALLOWED_ORIGINS, comma-separated; "*" allows any). Same-origin
	// requests are always allowed.
	AllowedOrigins []string
	// RoomTTL is how long an idle room stays joinable (ROOM_TTL, Go duration, default "24h").
	RoomTTL time.Duration
	// EmptyRoomTTL is how long a room and its messages/files survive after
	// everyone has left (EMPTY_ROOM_TTL, Go duration, default "5m"). It must
	// be long enough to cover a page reload and no longer than ROOM_TTL.
	EmptyRoomTTL time.Duration
	// RateLimit turns per-client request limits on or off (RATE_LIMIT,
	// default true). Only disable it for local testing.
	RateLimit bool
	// TrustedProxies are the reverse proxies (IPs or CIDRs) whose
	// X-Forwarded-For header is believed when finding the client IP
	// (TRUSTED_PROXIES, comma-separated, default none). Without this, anyone
	// could dodge rate limits by sending a fake X-Forwarded-For.
	TrustedProxies []string
	// StorageLimitMB stops new rooms, files and secrets once Redis uses this
	// much memory (STORAGE_LIMIT_MB, default 1024; 0 disables).
	StorageLimitMB int
	// Legal details shown in the Terms of Service and Privacy Policy. Pages
	// show a highlighted placeholder for anything left empty.
	OperatorName     string // OPERATOR_NAME, e.g. "Example LLC"
	OperatorAddress  string // OPERATOR_ADDRESS, postal address (needed for DMCA notices)
	ContactEmail     string // CONTACT_EMAIL, for privacy, legal and abuse reports
	GoverningState   string // GOVERNING_STATE, e.g. "Delaware"
	LogRetentionDays int    // LOG_RETENTION_DAYS, how long server logs are kept (default 7)
	// HistoryLimit is how many recent messages per room are kept and replayed
	// to clients that (re)join (HISTORY_LIMIT, default 200; 0 disables).
	HistoryLimit int
	// HistoryForNewMembers lets people see messages sent before they first
	// joined a room (HISTORY_FOR_NEW_MEMBERS, default false). Either way,
	// people who refresh or rejoin still see everything since their first join.
	HistoryForNewMembers bool
}

func Load() (*Config, error) {
	envFile := getEnv("ENV_FILE", ".env")
	if err := godotenv.Load(envFile); err != nil {
		// A missing default .env is fine; an explicitly requested or unreadable one is not.
		if !errors.Is(err, fs.ErrNotExist) || os.Getenv("ENV_FILE") != "" {
			return nil, fmt.Errorf("loading %s: %w", envFile, err)
		}
	}

	cfg := &Config{
		ServerAddr:    getEnv("SERVER_ADDR", ":8080"),
		RedisAddr:     getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
	}

	if v := strings.TrimRight(os.Getenv("PUBLIC_URL"), "/"); v != "" {
		u, err := url.Parse(v)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || (u.Path != "" && u.Path != "/") {
			return nil, fmt.Errorf("invalid PUBLIC_URL %q: must be an origin like https://chat.example.com", v)
		}
		cfg.PublicURL = u.Scheme + "://" + u.Host
	}

	db, err := strconv.Atoi(getEnv("REDIS_DB", "0"))
	if err != nil || db < 0 {
		return nil, fmt.Errorf("invalid REDIS_DB %q: must be a non-negative integer", os.Getenv("REDIS_DB"))
	}
	cfg.RedisDB = db

	limit, err := strconv.Atoi(getEnv("HISTORY_LIMIT", "200"))
	if err != nil || limit < 0 || limit > 10000 {
		return nil, fmt.Errorf("invalid HISTORY_LIMIT %q: must be an integer between 0 and 10000", os.Getenv("HISTORY_LIMIT"))
	}
	cfg.HistoryLimit = limit

	forNew, err := strconv.ParseBool(getEnv("HISTORY_FOR_NEW_MEMBERS", "false"))
	if err != nil {
		return nil, fmt.Errorf("invalid HISTORY_FOR_NEW_MEMBERS %q: must be true or false", os.Getenv("HISTORY_FOR_NEW_MEMBERS"))
	}
	cfg.HistoryForNewMembers = forNew

	cfg.OperatorName = os.Getenv("OPERATOR_NAME")
	cfg.OperatorAddress = os.Getenv("OPERATOR_ADDRESS")
	cfg.ContactEmail = os.Getenv("CONTACT_EMAIL")
	cfg.GoverningState = os.Getenv("GOVERNING_STATE")
	if cfg.LogRetentionDays, err = strconv.Atoi(getEnv("LOG_RETENTION_DAYS", "7")); err != nil || cfg.LogRetentionDays < 1 {
		return nil, fmt.Errorf("invalid LOG_RETENTION_DAYS %q: must be a positive integer", os.Getenv("LOG_RETENTION_DAYS"))
	}

	if cfg.RateLimit, err = strconv.ParseBool(getEnv("RATE_LIMIT", "true")); err != nil {
		return nil, fmt.Errorf("invalid RATE_LIMIT %q: must be true or false", os.Getenv("RATE_LIMIT"))
	}
	for _, p := range strings.Split(os.Getenv("TRUSTED_PROXIES"), ",") {
		if p = strings.TrimSpace(p); p != "" {
			cfg.TrustedProxies = append(cfg.TrustedProxies, p)
		}
	}
	if cfg.StorageLimitMB, err = strconv.Atoi(getEnv("STORAGE_LIMIT_MB", "1024")); err != nil || cfg.StorageLimitMB < 0 {
		return nil, fmt.Errorf("invalid STORAGE_LIMIT_MB %q: must be a non-negative integer", os.Getenv("STORAGE_LIMIT_MB"))
	}

	ttl, err := time.ParseDuration(getEnv("ROOM_TTL", "24h"))
	if err != nil || ttl <= 0 {
		return nil, fmt.Errorf("invalid ROOM_TTL %q: must be a positive duration like 24h or 30m", os.Getenv("ROOM_TTL"))
	}
	cfg.RoomTTL = ttl

	emptyTTL, err := time.ParseDuration(getEnv("EMPTY_ROOM_TTL", "5m"))
	if err != nil || emptyTTL < time.Second || emptyTTL > ttl {
		return nil, fmt.Errorf("invalid EMPTY_ROOM_TTL %q: must be a duration between 1s and ROOM_TTL (%s)", os.Getenv("EMPTY_ROOM_TTL"), ttl)
	}
	cfg.EmptyRoomTTL = emptyTTL

	for _, o := range strings.Split(os.Getenv("ALLOWED_ORIGINS"), ",") {
		if o = strings.TrimRight(strings.TrimSpace(o), "/"); o != "" {
			cfg.AllowedOrigins = append(cfg.AllowedOrigins, o)
		}
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
