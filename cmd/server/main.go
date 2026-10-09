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
		Storage: ratelimit.NewStorageGuard(int64(cfg.StorageLimitMB) << 20),
	})
	pages, err := http.NewPages("./templates", http.SiteInfo{
		PublicURL:            cfg.PublicURL,
		RoomTTL:              cfg.RoomTTL,
		EmptyRoomTTL:         cfg.EmptyRoomTTL,
		HistoryLimit:         cfg.HistoryLimit,
		HistoryForNewMembers: cfg.HistoryForNewMembers,
	})
	if err != nil {
		log.Fatalf("Failed to load page templates: %v", err)
	}

	// 4. Setup Router
	r := gin.Default()
	// Only believe X-Forwarded-For from configured proxies; otherwise clients
	// could fake their IP and dodge rate limits.
	if err := r.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		log.Fatalf("Invalid TRUSTED_PROXIES: %v", err)
	}
	r.Use(http.NoIndex())

	// Marketing pages (server-rendered) and SEO files
	r.GET("/", pages.Index)
	r.GET("/changelog", pages.Changelog)
	r.GET("/news", func(c *gin.Context) { c.Redirect(nethttp.StatusMovedPermanently, "/changelog") })
	r.GET("/robots.txt", pages.Robots)
	r.GET("/sitemap.xml", pages.Sitemap)
	r.StaticFile("/site.webmanifest", "./static/site.webmanifest")
	r.Static("/static", "./static")
	// Browsers and iOS request these at the root even without <link> tags.
	r.StaticFile("/favicon.ico", "./static/favicon.ico")
	r.StaticFile("/apple-touch-icon.png", "./static/apple-touch-icon.png")

	// Vue App (Built). Unknown /chat/ paths (e.g. /chat/secret/:id) fall
	// back to index.html so the app can route them.
	r.Static("/chat", "./frontend/dist")
	r.NoRoute(func(c *gin.Context) {
		if c.Request.Method == "GET" && strings.HasPrefix(c.Request.URL.Path, "/chat/") {
			c.File("./frontend/dist/index.html")
		}
	})

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

	srv := &nethttp.Server{Addr: cfg.ServerAddr, Handler: r}
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
