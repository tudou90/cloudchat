package main

import (
	"context"
	"errors"
	"log"
	"mime"
	nethttp "net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"cloudchat/internal/admin"
	"cloudchat/internal/config"
	"cloudchat/internal/database"
	"cloudchat/internal/ratelimit"
	"cloudchat/internal/transport/http"
	"cloudchat/internal/transport/ws"

	"github.com/gin-gonic/gin"
)

func main() {
	// Not in Go's built-in table; browsers expect this type for PWA manifests.
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")

	// 0. Load Config
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	// 1. Initialize Database
	database.InitRedis(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)

	// `cloudchat admin …`: moderation commands, then exit.
	if len(os.Args) > 1 && os.Args[1] == "admin" {
		if err := admin.Run(os.Args[2:], os.Stdin, os.Stdout); err != nil {
			log.SetFlags(0)
			log.Fatalf("admin: %v", err)
		}
		return
	}

	// 2. Initialize Hub
	hub := ws.NewHub(ws.Options{
		RoomTTL:              cfg.RoomTTL,
		EmptyRoomTTL:         cfg.EmptyRoomTTL,
		HistoryLimit:         cfg.HistoryLimit,
		HistoryForNewMembers: cfg.HistoryForNewMembers,
		AllowedOrigins:       cfg.AllowedOrigins,
	})

	// 3. Initialize Handlers
	if !cfg.RateLimit {
		log.Println("WARNING: rate limiting is disabled (RATE_LIMIT=false)")
	}
	h := http.NewHandler(hub, http.Protection{
		Limiter: ratelimit.New(cfg.RateLimit),
		Conns:   ratelimit.NewConnCounter(http.MaxConnsPerClient),
		Storage: ratelimit.NewStorageGuard(int64(cfg.StorageLimitMB)<<20, int64(cfg.FileStorageLimitMB)<<20),
	})
	pages, err := http.NewPages("./templates", http.SiteInfo{
		PublicURL:            cfg.PublicURL,
		RoomTTL:              cfg.RoomTTL,
		EmptyRoomTTL:         cfg.EmptyRoomTTL,
		HistoryLimit:         cfg.HistoryLimit,
		HistoryForNewMembers: cfg.HistoryForNewMembers,
		Legal: http.LegalInfo{
			OperatorName:     cfg.OperatorName,
			OperatorAddress:  cfg.OperatorAddress,
			ContactEmail:     cfg.ContactEmail,
			GoverningState:   cfg.GoverningState,
			LogRetentionDays: cfg.LogRetentionDays,
		},
	})
	if err != nil {
		log.Fatalf("Failed to load page templates: %v", err)
	}

	// 4. Setup Router
	https := strings.HasPrefix(cfg.PublicURL, "https://")
	h.SecureCookies = https
	if https {
		log.Println("Serving as HTTPS (PUBLIC_URL): secure cookies and HSTS enabled")
	}

	r := gin.New()
	r.Use(gin.Recovery(), http.AccessLog(), http.SecurityHeaders(https))
	// Only believe X-Forwarded-For from configured proxies; otherwise clients
	// could fake their IP and dodge rate limits.
	if err := r.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		log.Fatalf("Invalid TRUSTED_PROXIES: %v", err)
	}
	r.Use(http.NoIndex())

	// Marketing pages (server-rendered) and SEO files
	r.GET("/", pages.Index)
	r.GET("/changelog", pages.Changelog)
	r.GET("/terms", pages.Terms)
	r.GET("/privacy", pages.Privacy)
	r.GET("/news", func(c *gin.Context) { c.Redirect(nethttp.StatusMovedPermanently, "/changelog") })
	r.GET("/robots.txt", pages.Robots)
	r.GET("/healthz", http.Health)
	r.GET("/sitemap.xml", pages.Sitemap)
	r.StaticFile("/site.webmanifest", "./static/site.webmanifest")
	r.Static("/static", "./static")
	// Browsers and iOS request these at the root even without <link> tags.
	r.StaticFile("/favicon.ico", "./static/favicon.ico")
	r.StaticFile("/apple-touch-icon.png", "./static/apple-touch-icon.png")

	// Vue App (Built). Its pages (/chat/, /chat/secret, /chat/secret/:id)
	// are index.html with per-page meta tags so the app can route them.
	chatApp := pages.ChatApp("./frontend/dist")
	r.GET("/chat/*filepath", chatApp)
	r.HEAD("/chat/*filepath", chatApp)

	// API
	api := r.Group("/api")
	{
		api.POST("/rooms", h.RateLimit(http.LimitRoomCreate, http.LimitRoomCreateDay), h.CreateRoom)
		api.GET("/rooms/:id", h.RateLimit(http.LimitRoomLookup), h.GetRoom)
		api.POST("/rooms/:id/files", h.RateLimit(http.LimitUpload), h.UploadFile)
		api.GET("/rooms/:id/files/:fileId", h.RateLimit(http.LimitDownload), h.DownloadFile)
		api.POST("/secrets", h.RateLimit(http.LimitSecretCreate, http.LimitSecretDay), h.CreateSecret)
		api.GET("/secrets/:id", h.RateLimit(http.LimitSecretLookup), h.GetSecret)
		api.POST("/secrets/:id/reveal", h.RateLimit(http.LimitSecretReveal), h.RevealSecret)
	}

	// WebSocket
	r.GET("/ws/:roomID", h.RateLimit(http.LimitWSConnect), h.ServeWS)

	srv := &nethttp.Server{
		Addr:    cfg.ServerAddr,
		Handler: r,
		// Timeouts stop slow or stalled clients from tying up connections
		// (e.g. slowloris). Read/Write timeouts allow a 10 MB upload or
		// download on a slow link; WebSockets set their own deadlines after
		// the upgrade, so they are unaffected.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       2 * time.Minute,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
	}
	go func() {
		log.Printf("CloudChat Server starting on %s", cfg.ServerAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, nethttp.ErrServerClosed) {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Graceful shutdown: take our clients out of their rooms first, so
	// presence and the empty-room countdown don't see them as still online.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Println("Shutting down...")
	hub.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("HTTP shutdown: %v", err)
	}
}
