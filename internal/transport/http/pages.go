package http

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"cloudchat/internal/service"
	"cloudchat/internal/transport/ws"
	"github.com/gin-gonic/gin"
)

// SiteInfo is what the marketing pages need to describe the service
// accurately; numbers come from the live configuration, not the copy.
type SiteInfo struct {
	// PublicURL is the canonical origin (e.g. https://chat.example.com).
	// When empty it is derived from each request.
	PublicURL            string
	RoomTTL              time.Duration
	EmptyRoomTTL         time.Duration
	HistoryLimit         int
	HistoryForNewMembers bool
	Legal                LegalInfo
}

// LegalInfo is the operator's identity and contact details for the legal pages.
type LegalInfo struct {
	OperatorName     string
	OperatorAddress  string
	ContactEmail     string
	GoverningState   string
	LogRetentionDays int
}

// Missing lists the legal settings that are still empty.
func (l LegalInfo) Missing() []string {
	var m []string
	for name, v := range map[string]string{"OPERATOR_NAME": l.OperatorName, "OPERATOR_ADDRESS": l.OperatorAddress, "CONTACT_EMAIL": l.ContactEmail, "GOVERNING_STATE": l.GoverningState} {
		if strings.TrimSpace(v) == "" {
			m = append(m, name)
		}
	}
	sort.Strings(m)
	return m
}

// Pages renders the server-side marketing pages and SEO files.
type Pages struct {
	info         SiteInfo
	pages        map[string]*template.Template
	assetVersion string
}

type faqItem struct {
	Question string
	Answer   string
}

type siteFacts struct {
	EmptyRoomAfter string
	RoomIdle       string
	MaxFileMB      int
	MaxMessageLen  int
	HistoryLimit   int
	SecretAttempts int
}

type pageData struct {
	Title        string
	Description  string
	Path         string
	BaseURL      string
	Year         int
	AssetVersion string
	Facts        siteFacts
	FAQ          []faqItem
	JSONLD       template.JS
	Legal        LegalInfo
	Updated      string // "last updated" date of a legal page
}

// NewPages parses templates/<page>.html together with templates/partials.html.
func NewPages(dir string, info SiteInfo) (*Pages, error) {
	funcs := template.FuncMap{"capfirst": capFirst, "orTodo": orTodo, "email": emailLink}
	p := &Pages{info: info, pages: map[string]*template.Template{}}
	if missing := info.Legal.Missing(); len(missing) > 0 {
		log.Printf("WARNING: Terms/Privacy pages show placeholders; set %s", strings.Join(missing, ", "))
	}
	for _, name := range []string{"index", "changelog", "terms", "privacy"} {
		t, err := template.New(name+".html").Funcs(funcs).ParseFiles(
			filepath.Join(dir, name+".html"), filepath.Join(dir, "partials.html"))
		if err != nil {
			return nil, err
		}
		p.pages[name] = t
	}
	// Cache-bust the stylesheet whenever it is rebuilt.
	if css, err := os.ReadFile("./static/site.css"); err == nil {
		sum := sha256.Sum256(css)
		p.assetVersion = hex.EncodeToString(sum[:4])
	} else {
		log.Printf("static/site.css not found; run `npm run build:site` in frontend/: %v", err)
	}
	return p, nil
}

func (p *Pages) baseURL(c *gin.Context) string {
	if p.info.PublicURL != "" {
		return p.info.PublicURL
	}
	scheme := "http"
	if c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host
}

func (p *Pages) facts() siteFacts {
	return siteFacts{
		EmptyRoomAfter: humanDuration(p.info.EmptyRoomTTL),
		RoomIdle:       humanDuration(p.info.RoomTTL),
		MaxFileMB:      service.MaxFileSize >> 20,
		MaxMessageLen:  ws.MaxContentLength,
		HistoryLimit:   p.info.HistoryLimit,
		SecretAttempts: service.MaxSecretAttempts,
	}
}

func (p *Pages) faq(f siteFacts, https bool) []faqItem {
	laterJoiners := "No. Everyone only sees messages sent after they first joined the room. " +
		"Refreshing the page or reconnecting keeps everything you have already seen."
	if p.info.HistoryForNewMembers {
		laterJoiners = fmt.Sprintf("Yes. People who join a room can read its recent messages (up to the last %d).", f.HistoryLimit)
	}
	transit := "Chat messages travel to the server and are deleted together with the room, "
	if https {
		transit = "Chat messages are protected in transit by HTTPS and deleted together with the room, "
	}
	return []faqItem{
		{"Is CloudChat free?", "Yes. CloudChat is free to use, with no ads and no account required."},
		{"Do I need to sign up or install an app?", "No. Open the site, pick a nickname and start a room. It works in any modern browser on phones, tablets and computers."},
		{"Who can join my chat room?", "Anyone who has the invite link. Room links contain a long random ID that can't be guessed, so share the link only with the people you want in the room."},
		{"How long are messages kept?", fmt.Sprintf("Messages and files are deleted %s after the last person leaves the room. A room with no activity for %s expires as well. While a room is active, only its last %d messages are kept.", f.EmptyRoomAfter, f.RoomIdle, f.HistoryLimit)},
		{"Can people who join later read earlier messages?", laterJoiners},
		{"Are chat rooms end-to-end encrypted?", "No. " + transit + "but the server relays them, so they are not end-to-end encrypted. For anything sensitive, use a secret note: it is encrypted in your browser and we cannot read it."},
		{"What can I share in a chat room?", fmt.Sprintf("Text messages up to %d characters, and files up to %d MB each. PNG, JPEG, GIF and WebP images preview inside the chat; other files are shared as downloads.", f.MaxMessageLen, f.MaxFileMB)},
		{"How do secret notes work?", fmt.Sprintf("You write a note and set a password. Your browser encrypts it before sending, and the key needed to decrypt it travels only in the link. The recipient opens the link, enters the password, and the note is deleted right after it is read. It is also destroyed after %d wrong password attempts or when it expires (1 day, 1 week or 1 month).", f.SecretAttempts)},
	}
}

func (p *Pages) render(c *gin.Context, name string, d pageData) {
	d.BaseURL = p.baseURL(c)
	d.Year = time.Now().Year()
	d.AssetVersion = p.assetVersion
	d.Legal = p.info.Legal
	if d.Facts == (siteFacts{}) {
		d.Facts = p.facts()
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	if err := p.pages[name].Execute(c.Writer, d); err != nil {
		log.Printf("Failed to render %s: %v", name, err)
	}
}

func (p *Pages) Index(c *gin.Context) {
	base := p.baseURL(c)
	f := p.facts()
	faq := p.faq(f, strings.HasPrefix(base, "https://"))
	desc := "Create a free temporary chat room in one click — no sign-up, no app. Share the link, chat and send files. Everything is deleted when everyone leaves."

	mainEntity := make([]map[string]any, 0, len(faq))
	for _, q := range faq {
		mainEntity = append(mainEntity, map[string]any{
			"@type":          "Question",
			"name":           q.Question,
			"acceptedAnswer": map[string]any{"@type": "Answer", "text": q.Answer},
		})
	}
	ld, _ := json.Marshal(map[string]any{
		"@context": "https://schema.org",
		"@graph": []map[string]any{
			{"@type": "WebSite", "@id": base + "/#website", "name": "CloudChat", "url": base + "/"},
			{
				"@type":               "WebApplication",
				"@id":                 base + "/#app",
				"name":                "CloudChat",
				"url":                 base + "/",
				"description":         desc,
				"applicationCategory": "CommunicationApplication",
				"operatingSystem":     "Any",
				"browserRequirements": "Requires JavaScript and a modern web browser",
				"image":               base + "/static/og-image.png",
				"offers":              map[string]any{"@type": "Offer", "price": "0", "priceCurrency": "USD"},
				"featureList": []string{
					"Temporary chat rooms with no sign-up",
					"Invite by link",
					fmt.Sprintf("File and image sharing up to %d MB", f.MaxFileMB),
					"Live online member list",
					"Rooms self-destruct when everyone leaves",
					"Self-destructing, browser-encrypted secret notes",
				},
			},
			{"@type": "FAQPage", "@id": base + "/#faq", "mainEntity": mainEntity},
		},
	})

	p.render(c, "index", pageData{
		Title:       "CloudChat — Free Temporary Chat Rooms, No Sign-Up",
		Description: desc,
		Path:        "/",
		Facts:       f,
		FAQ:         faq,
		JSONLD:      template.JS(ld), // json.Marshal escapes <, > and &, so this is safe in <script>
	})
}

func (p *Pages) Changelog(c *gin.Context) {
	p.render(c, "changelog", pageData{
		Title:       "Changelog — CloudChat",
		Description: "What's new in CloudChat: temporary chat rooms, file sharing, invite links, self-destructing rooms and browser-encrypted secret notes, plus what's coming next.",
		Path:        "/changelog",
	})
}

// legalUpdated is the "last updated" date of the Terms and Privacy Policy.
// Change it whenever their text changes.
const legalUpdated = "October 9, 2026"

func (p *Pages) Terms(c *gin.Context) {
	p.render(c, "terms", pageData{
		Title:       "Terms of Service — CloudChat",
		Description: "The rules for using CloudChat's temporary chat rooms and secret notes.",
		Path:        "/terms",
		Updated:     legalUpdated,
	})
}

func (p *Pages) Privacy(c *gin.Context) {
	p.render(c, "privacy", pageData{
		Title:       "Privacy Policy — CloudChat",
		Description: "What CloudChat stores, for how long, and why. No accounts, no tracking, no ads.",
		Path:        "/privacy",
		Updated:     legalUpdated,
	})
}

func (p *Pages) Robots(c *gin.Context) {
	c.String(http.StatusOK, "User-agent: *\nAllow: /\nDisallow: /api/\nDisallow: /ws/\nDisallow: /chat/secret/\nDisallow: /chat/?room=\n\nSitemap: %s/sitemap.xml\n", p.baseURL(c))
}

func (p *Pages) Sitemap(c *gin.Context) {
	base := p.baseURL(c)
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	for _, u := range []struct{ path, prio string }{{"/", "1.0"}, {"/chat/", "0.8"}, {"/chat/secret", "0.7"}, {"/changelog", "0.5"}, {"/privacy", "0.3"}, {"/terms", "0.3"}} {
		fmt.Fprintf(&b, "  <url><loc>%s%s</loc><priority>%s</priority></url>\n", template.HTMLEscapeString(base), u.path, u.prio)
	}
	b.WriteString("</urlset>\n")
	c.Data(http.StatusOK, "application/xml; charset=utf-8", []byte(b.String()))
}

// NoIndex keeps private, unguessable URLs (secret notes, room invites) and
// API responses out of search engines.
func NoIndex() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/chat/secret/") ||
			(strings.HasPrefix(path, "/chat") && c.Query("room") != "") {
			c.Header("X-Robots-Tag", "noindex, nofollow")
		}
		c.Next()
	}
}

func humanDuration(d time.Duration) string {
	plural := func(n int64, unit string) string {
		if n == 1 {
			return fmt.Sprintf("1 %s", unit)
		}
		return fmt.Sprintf("%d %ss", n, unit)
	}
	switch {
	case d >= time.Hour && d%time.Hour == 0:
		return plural(int64(d/time.Hour), "hour")
	case d >= time.Minute && d%time.Minute == 0:
		return plural(int64(d/time.Minute), "minute")
	default:
		return plural(int64(d/time.Second), "second")
	}
}

// orTodo renders a configured value, or a highlighted placeholder so a
// missing legal detail is obvious on the page.
func orTodo(value, label string) template.HTML {
	if strings.TrimSpace(value) == "" {
		return template.HTML(`<mark class="todo">[` + template.HTMLEscapeString(label) + `]</mark>`)
	}
	return template.HTML(template.HTMLEscapeString(value))
}

// emailLink renders a mailto link, or a placeholder when no address is set.
func emailLink(addr string) template.HTML {
	if strings.TrimSpace(addr) == "" {
		return orTodo("", "CONTACT_EMAIL")
	}
	e := template.HTMLEscapeString(addr)
	return template.HTML(`<a href="mailto:` + e + `">` + e + `</a>`)
}

func capFirst(s string) string {
	for i, r := range s {
		return string(unicode.ToUpper(r)) + s[i+len(string(r)):]
	}
	return s
}
