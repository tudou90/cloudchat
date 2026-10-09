package http

import (
	"bytes"
	"html/template"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

// appMeta is the per-page title and description of the chat app (the Vue
// SPA under /chat/), so each page has its own search snippet and link
// preview even though they share one index.html.
type appMeta struct {
	Title, Description string
	Canonical          bool // public page: emit a canonical link
}

var (
	appTitleRe = regexp.MustCompile(`(?s)<title>.*?</title>`)
	appDescRe  = regexp.MustCompile(`<meta name="description"[^>]*>`)
)

func chatAppMeta(rel string) (appMeta, bool) {
	switch {
	case rel == "":
		return appMeta{
			Title:       "Start a Temporary Chat Room — CloudChat",
			Description: "Create a free temporary chat room in one click. No sign-up, no app — share the invite link and start chatting.",
			Canonical:   true,
		}, true
	case rel == "secret":
		return appMeta{
			Title:       "Create a Self-Destructing Secret Note — CloudChat",
			Description: "Share a password or private message with a one-time link. Encrypted in your browser, protected by a password and destroyed after it's read.",
			Canonical:   true,
		}, true
	case strings.HasPrefix(rel, "secret/") && !strings.Contains(rel[len("secret/"):], "/"):
		return appMeta{
			Title:       "You've Received a Secret Note — CloudChat",
			Description: "Someone sent you a self-destructing secret note. It can be read only once.",
		}, true
	}
	return appMeta{}, false
}

// ChatApp serves the built chat app from dist: real files as-is, and every
// app page as index.html with that page's title, description, canonical
// link and Open Graph tags filled in. Unknown pages get the app with a 404.
func (p *Pages) ChatApp(dist string) gin.HandlerFunc {
	files := http.StripPrefix("/chat", http.FileServer(http.Dir(dist)))
	return func(c *gin.Context) {
		rel := strings.Trim(path.Clean("/"+c.Param("filepath")), "/")
		if rel != "" && rel != "index.html" {
			if fi, err := os.Stat(filepath.Join(dist, filepath.FromSlash(rel))); err == nil && !fi.IsDir() {
				files.ServeHTTP(c.Writer, c.Request)
				return
			}
		}
		if rel == "index.html" {
			rel = ""
		}
		html, err := os.ReadFile(filepath.Join(dist, "index.html"))
		if err != nil {
			c.String(http.StatusServiceUnavailable, "The chat app is not built yet.")
			return
		}
		meta, found := chatAppMeta(rel)
		status := http.StatusOK
		if !found {
			status = http.StatusNotFound
			meta, _ = chatAppMeta("")
			meta.Canonical = false
			c.Header("X-Robots-Tag", "noindex")
		}
		pageURL := p.baseURL(c) + "/chat/" + rel
		if meta.Canonical && c.Query("room") != "" {
			meta.Canonical = false // invite links are private
		}

		esc := template.HTMLEscapeString
		html = appTitleRe.ReplaceAllLiteral(html, []byte("<title>"+esc(meta.Title)+"</title>"))
		html = appDescRe.ReplaceAllLiteral(html, []byte(`<meta name="description" content="`+esc(meta.Description)+`" />`))
		var head strings.Builder
		if meta.Canonical {
			head.WriteString(`    <link rel="canonical" href="` + esc(pageURL) + `" />` + "\n")
			head.WriteString(`    <meta property="og:url" content="` + esc(pageURL) + `" />` + "\n")
		}
		head.WriteString(`    <meta property="og:type" content="website" />` + "\n")
		head.WriteString(`    <meta property="og:site_name" content="CloudChat" />` + "\n")
		head.WriteString(`    <meta property="og:title" content="` + esc(meta.Title) + `" />` + "\n")
		head.WriteString(`    <meta property="og:description" content="` + esc(meta.Description) + `" />` + "\n")
		head.WriteString(`    <meta property="og:image" content="` + esc(p.baseURL(c)) + `/static/og-image.png" />` + "\n")
		head.WriteString(`    <meta name="twitter:card" content="summary_large_image" />` + "\n")
		html = bytes.Replace(html, []byte("</head>"), []byte(head.String()+"  </head>"), 1)

		c.Header("Cache-Control", "no-cache")
		c.Data(status, "text/html; charset=utf-8", html)
	}
}
