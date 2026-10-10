package webui

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

const testIndex = `<!doctype html>
<html lang="en">
  <head>
    <meta name="bonsai-relay-origin" content="__BONSAI_RELAY_ORIGIN__" />
    <title>Bonsai</title>
    <script type="module" crossorigin src="/assets/index-abc123.js"></script>
  </head>
  <body><div id="root"></div></body>
</html>
`

func testBundle() fstest.MapFS {
	return fstest.MapFS{
		"index.html":            {Data: []byte(testIndex)},
		"assets/index-abc.js":   {Data: []byte(strings.Repeat("console.log('bonsai');\n", 200))},
		"assets/font-abc.woff2": {Data: []byte("wOF2 binary")},
		"_headers":              {Data: []byte("/*\n  X: y\n")},
	}
}

func serve(t *testing.T, h http.Handler, method, target string, header map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	req.Host = "127.0.0.1:7001"
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRootRedirectsToApp(t *testing.T) {
	rec := serve(t, New(testBundle(), Options{}), http.MethodGet, "/", nil)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/app/" {
		t.Fatalf("GET / = %d %q, want 302 /app/", rec.Code, rec.Header().Get("Location"))
	}
}

func TestAppRoutesServeIndexWithRuntimeConfig(t *testing.T) {
	h := New(testBundle(), Options{})
	for _, target := range []string{"/app", "/app/", "/app/settings", "/app/github?pr=23", "/app/deep/route.js"} {
		rec := serve(t, h, http.MethodGet, target, nil)
		body := rec.Body.String()
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", target, rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
			t.Fatalf("GET %s content type %q", target, got)
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("GET %s must not be cached, got %q", target, rec.Header().Get("Cache-Control"))
		}
		for _, want := range []string{
			`<meta name="bonsai-local-api-origin" content="http://127.0.0.1:7001" />`,
			`<meta name="bonsai-entry" content="local" />`,
			`<meta name="bonsai-relay-origin" content="" />`,
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("GET %s body lacks %s:\n%s", target, want, body)
			}
		}
		if strings.Contains(body, relayPlaceholder) {
			t.Fatalf("GET %s left the relay placeholder in place", target)
		}
		if strings.Index(body, "bonsai-local-api-origin") > strings.Index(body, "</head>") {
			t.Fatalf("meta tags must be inside <head>")
		}
	}
}

func TestPageEchoesTheRequestHostAsAPIOrigin(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/app", nil)
	req.Host = "localhost:7011"
	rec := httptest.NewRecorder()
	New(testBundle(), Options{}).ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `content="http://localhost:7011"`) {
		t.Fatalf("local API origin should follow the validated Host:\n%s", rec.Body.String())
	}
}

func TestRelayOriginIsInjectedAndAllowedByCSP(t *testing.T) {
	rec := serve(t, New(testBundle(), Options{RelayOrigin: "https://relay.example"}), http.MethodGet, "/app", nil)
	if !strings.Contains(rec.Body.String(), `<meta name="bonsai-relay-origin" content="https://relay.example" />`) {
		t.Fatalf("relay origin not injected:\n%s", rec.Body.String())
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "connect-src 'self' https://relay.example;") || !strings.Contains(csp, "form-action 'self' https://relay.example") {
		t.Fatalf("CSP should allow the relay: %s", csp)
	}
}

func TestIndexWithoutPlaceholderStillGetsRelayMeta(t *testing.T) {
	ui := fstest.MapFS{"index.html": {Data: []byte("<html><head><title>x</title></head><body></body></html>")}}
	body := serve(t, New(ui, Options{}), http.MethodGet, "/app", nil).Body.String()
	if !strings.Contains(body, `<meta name="bonsai-relay-origin" content="" />`) {
		t.Fatalf("relay meta missing:\n%s", body)
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := New(testBundle(), Options{})
	for _, target := range []string{"/app", "/assets/index-abc.js", "/"} {
		rec := serve(t, h, http.MethodGet, target, nil)
		want := map[string]string{
			"Content-Security-Policy": "default-src 'self'; script-src 'self'; style-src 'self'; style-src-elem 'self'; style-src-attr 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'",
			"X-Frame-Options":         "DENY",
			"X-Content-Type-Options":  "nosniff",
			"Referrer-Policy":         "no-referrer",
		}
		for name, value := range want {
			if got := rec.Header().Get(name); got != value {
				t.Fatalf("%s %s = %q, want %q", target, name, got, value)
			}
		}
	}
}

func TestAssetsAreImmutableAndGzipped(t *testing.T) {
	h := New(testBundle(), Options{})
	plain := serve(t, h, http.MethodGet, "/assets/index-abc.js", nil)
	if plain.Code != http.StatusOK || plain.Header().Get("Content-Encoding") != "" {
		t.Fatalf("plain asset = %d encoding %q", plain.Code, plain.Header().Get("Content-Encoding"))
	}
	if got := plain.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("asset cache control %q", got)
	}
	if got := plain.Header().Get("Content-Type"); got != "text/javascript; charset=utf-8" {
		t.Fatalf("asset content type %q", got)
	}
	if plain.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatalf("compressible asset must vary on Accept-Encoding")
	}

	gz := serve(t, h, http.MethodGet, "/assets/index-abc.js", map[string]string{"Accept-Encoding": "br, gzip;q=0.8"})
	if gz.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("gzip not applied")
	}
	if gz.Body.Len() >= plain.Body.Len() {
		t.Fatalf("gzip body %d not smaller than %d", gz.Body.Len(), plain.Body.Len())
	}
	zr, err := gzip.NewReader(bytes.NewReader(gz.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	decoded, _ := io.ReadAll(zr)
	if !bytes.Equal(decoded, plain.Body.Bytes()) {
		t.Fatal("gzip body does not decode to the asset")
	}

	refused := serve(t, h, http.MethodGet, "/assets/index-abc.js", map[string]string{"Accept-Encoding": "gzip;q=0"})
	if refused.Header().Get("Content-Encoding") != "" {
		t.Fatal("gzip;q=0 must not be gzipped")
	}

	font := serve(t, h, http.MethodGet, "/assets/font-abc.woff2", map[string]string{"Accept-Encoding": "gzip"})
	if font.Header().Get("Content-Encoding") != "" || font.Header().Get("Content-Type") != "font/woff2" {
		t.Fatalf("woff2 = encoding %q type %q", font.Header().Get("Content-Encoding"), font.Header().Get("Content-Type"))
	}
}

func TestMissingAssetsAreNotFound(t *testing.T) {
	h := New(testBundle(), Options{})
	for _, target := range []string{"/assets/missing.js", "/assets/", "/assets/../index.html", "/_headers"} {
		if rec := serve(t, h, http.MethodGet, target, nil); rec.Code != http.StatusNotFound {
			t.Fatalf("GET %s = %d, want 404", target, rec.Code)
		}
	}
}

func TestHeadHasNoBody(t *testing.T) {
	rec := serve(t, New(testBundle(), Options{}), http.MethodHead, "/assets/index-abc.js", nil)
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 || rec.Header().Get("Content-Length") == "" {
		t.Fatalf("HEAD = %d body %d length %q", rec.Code, rec.Body.Len(), rec.Header().Get("Content-Length"))
	}
}

func TestWritesAreRejected(t *testing.T) {
	if rec := serve(t, New(testBundle(), Options{}), http.MethodPost, "/app", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /app = %d", rec.Code)
	}
}

func TestPlaceholderWithoutBundle(t *testing.T) {
	for _, ui := range []fstest.MapFS{nil, {}} {
		var h http.Handler
		if ui == nil {
			h = New(nil, Options{})
		} else {
			h = New(ui, Options{})
		}
		rec := serve(t, h, http.MethodGet, "/app/settings", nil)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "UI not built") || !strings.Contains(rec.Body.String(), "pnpm -C web build") {
			t.Fatalf("placeholder = %d\n%s", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Content-Security-Policy") == "" {
			t.Fatal("placeholder needs the same security headers")
		}
		if asset := serve(t, h, http.MethodGet, "/assets/index-abc.js", nil); asset.Code != http.StatusNotFound {
			t.Fatalf("asset without bundle = %d", asset.Code)
		}
	}
}

func TestIsStaticPath(t *testing.T) {
	for p, want := range map[string]bool{
		"/": true, "/app": true, "/app/": true, "/app/x": true, "/assets/a.js": true,
		"/apple": false, "/api/projects": false, "/events": false, "/health": false, "/version": false,
	} {
		if IsStaticPath(p) != want {
			t.Fatalf("IsStaticPath(%q) != %v", p, want)
		}
	}
}
