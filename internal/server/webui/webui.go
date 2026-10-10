// Package webui serves the web UI bundle from the local API, on the API's own
// origin. The page needs no session: it is public, static and carries no
// secrets. Everything it does afterwards goes through the API's normal Origin,
// session and WebSocket checks.
//
// Routes:
//
//	GET /            redirect to /app/
//	GET /app, /app/* index.html (SPA fallback) with runtime config meta tags
//	GET /assets/*    hashed build assets, cached as immutable
package webui

import (
	"bytes"
	"compress/gzip"
	"html"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
)

// Options configures the runtime values injected into index.html.
type Options struct {
	// RelayOrigin is the hosted GitHub relay origin. Empty while the relay is
	// frozen (decision D1): the page then hides relay UI and its CSP allows no
	// origin other than its own.
	RelayOrigin string
}

const relayPlaceholder = "__BONSAI_RELAY_ORIGIN__"

type handler struct {
	ui    fs.FS
	opts  Options
	csp   string
	index string
	gz    sync.Map // asset path -> []byte (gzip body), or nil when not worth it
}

// New returns the static UI handler. A nil ui, or one without index.html,
// serves a placeholder page that explains how to build the UI.
func New(ui fs.FS, opts Options) http.Handler {
	h := &handler{ui: ui, opts: opts, csp: ContentSecurityPolicy(opts.RelayOrigin)}
	if ui != nil {
		if raw, err := fs.ReadFile(ui, "index.html"); err == nil {
			h.index = string(raw)
		}
	}
	return h
}

// IsStaticPath reports whether path belongs to the UI rather than the API.
func IsStaticPath(p string) bool {
	return p == "/" || p == "/app" || strings.HasPrefix(p, "/app/") || strings.HasPrefix(p, "/assets/")
}

// ContentSecurityPolicy mirrors productionSecurityHeaders in web/vite.config.ts
// for a page whose API is its own origin. 'self' covers the same-origin
// WebSocket. The relay origin is added only when one is configured.
func ContentSecurityPolicy(relayOrigin string) string {
	extra := ""
	if relayOrigin != "" {
		extra = " " + relayOrigin
	}
	return "default-src 'self'; script-src 'self'; style-src 'self'; style-src-elem 'self'; " +
		"style-src-attr 'unsafe-inline'; img-src 'self' data:; font-src 'self'; " +
		"connect-src 'self'" + extra + "; object-src 'none'; frame-ancestors 'none'; " +
		"base-uri 'none'; form-action 'self'" + extra
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.securityHeaders(w)
	switch p := r.URL.Path; {
	case p == "/":
		http.Redirect(w, r, "/app/", http.StatusFound)
	case strings.HasPrefix(p, "/assets/"):
		h.asset(w, r, strings.TrimPrefix(p, "/"))
	case p == "/app" || strings.HasPrefix(p, "/app/"):
		h.page(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *handler) securityHeaders(w http.ResponseWriter) {
	header := w.Header()
	header.Set("Content-Security-Policy", h.csp)
	header.Set("X-Frame-Options", "DENY")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("Permissions-Policy", "loopback-network=(self), local-network=(self), local-network-access=(self)")
}

// page serves index.html for every /app route, so client-side routes survive a
// reload. The caller has already validated r.Host against the API's own host
// allow-list, so it is safe to echo as the API origin.
func (h *handler) page(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if h.index == "" {
		write(w, r, http.StatusOK, []byte(placeholderPage))
		return
	}
	write(w, r, http.StatusOK, []byte(h.render("http://"+r.Host)))
}

func (h *handler) render(apiOrigin string) string {
	relay := html.EscapeString(h.opts.RelayOrigin)
	page := h.index
	metas := `<meta name="bonsai-local-api-origin" content="` + html.EscapeString(apiOrigin) + `" />` +
		`<meta name="bonsai-entry" content="local" />`
	if strings.Contains(page, relayPlaceholder) {
		page = strings.ReplaceAll(page, relayPlaceholder, relay)
	} else {
		metas += `<meta name="bonsai-relay-origin" content="` + relay + `" />`
	}
	if i := strings.Index(page, "</head>"); i >= 0 {
		return page[:i] + metas + page[i:]
	}
	return metas + page
}

func (h *handler) asset(w http.ResponseWriter, r *http.Request, name string) {
	if h.ui == nil || !fs.ValidPath(name) {
		http.NotFound(w, r)
		return
	}
	body, err := fs.ReadFile(h.ui, name)
	if err != nil {
		// Also covers directories: a missing asset is a 404, never index.html.
		http.NotFound(w, r)
		return
	}
	header := w.Header()
	header.Set("Cache-Control", "public, max-age=31536000, immutable")
	header.Set("Content-Type", contentType(name))
	if compressible(name) {
		header.Add("Vary", "Accept-Encoding")
		if gz := h.gzipped(name, body); gz != nil && acceptsGzip(r) {
			header.Set("Content-Encoding", "gzip")
			body = gz
		}
	}
	write(w, r, http.StatusOK, body)
}

// gzipped compresses an asset once and keeps the result; the bundle is
// immutable for the life of the process.
func (h *handler) gzipped(name string, body []byte) []byte {
	if cached, ok := h.gz.Load(name); ok {
		return cached.([]byte)
	}
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	_, _ = zw.Write(body)
	_ = zw.Close()
	var out []byte
	if buf.Len() < len(body) {
		out = buf.Bytes()
	}
	actual, _ := h.gz.LoadOrStore(name, out)
	return actual.([]byte)
}

func write(w http.ResponseWriter, r *http.Request, status int, body []byte) {
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		coding, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(coding), "gzip") {
			continue
		}
		name, value, found := strings.Cut(strings.TrimSpace(params), "=")
		if !found || !strings.EqualFold(strings.TrimSpace(name), "q") {
			return true
		}
		q, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		return err == nil && q > 0
	}
	return false
}

// Fixed types so the answer never depends on the host's MIME registry (on
// Windows that comes from the registry and can be wrong for .js).
var contentTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".mjs":   "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".json":  "application/json; charset=utf-8",
	".map":   "application/json; charset=utf-8",
	".svg":   "image/svg+xml",
	".png":   "image/png",
	".webp":  "image/webp",
	".ico":   "image/x-icon",
	".woff2": "font/woff2",
	".woff":  "font/woff",
	".txt":   "text/plain; charset=utf-8",
}

func contentType(name string) string {
	ext := strings.ToLower(path.Ext(name))
	if value, ok := contentTypes[ext]; ok {
		return value
	}
	if value := mime.TypeByExtension(ext); value != "" {
		return value
	}
	return "application/octet-stream"
}

func compressible(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".html", ".js", ".mjs", ".css", ".json", ".map", ".svg", ".txt":
		return true
	}
	return false
}

const placeholderPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<title>Bonsai — UI not built</title>
</head>
<body>
<h1>UI not built</h1>
<p>This bonsai binary was built without the web UI. The local API is running.</p>
<p>Build the UI, then rebuild bonsai with it embedded:</p>
<pre>pnpm -C web install
pnpm -C web build
go build -tags embedui .</pre>
<p>Or use the hosted app at <a href="https://app.bonsai.dev/app">https://app.bonsai.dev/app</a> while this API allows it.</p>
</body>
</html>
`
