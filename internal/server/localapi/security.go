package localapi

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/server/webui"
)

const ProductionBrowserOrigin = "https://app.bonsai.dev"

type BrowserSecurityMode string

const (
	BrowserSecurityProduction  BrowserSecurityMode = "production"
	BrowserSecurityDevelopment BrowserSecurityMode = "development"
)

func requireLoopback(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid listen address %q: %w", address, err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("local API must bind to a loopback IP, got %q", host)
	}
	return nil
}

func validateBrowserOrigin(origin string, mode BrowserSecurityMode) error {
	if mode == "" {
		mode = BrowserSecurityProduction
	}
	switch mode {
	case BrowserSecurityProduction:
		return validateProductionOrigin(origin)
	case BrowserSecurityDevelopment:
		return validateDevelopmentOrigin(origin)
	default:
		return fmt.Errorf("unknown browser security mode %q", mode)
	}
}

// validateProductionOrigin accepts the hosted HTTPS origin, or "" when the
// hosted interface is disabled and only the API's own origin may connect.
func validateProductionOrigin(origin string) error {
	if origin == "" {
		return nil
	}
	u, err := url.Parse(strings.TrimSpace(origin))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("production browser origin must be an explicit HTTPS origin")
	}
	return nil
}

func validateDevelopmentOrigin(origin string) error {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" || u.Host == "" || u.User != nil ||
		u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("development browser origin must be an explicit loopback HTTP origin")
	}
	host := u.Hostname()
	if host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return fmt.Errorf("development browser origin must be loopback, got %q", host)
		}
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("development browser origin requires an explicit valid port")
	}
	return nil
}

// allowedHost reports whether host names this API: exactly its listen address,
// or localhost on the same port. Anything else (including DNS-rebinding names
// that resolve to loopback) is refused.
func (s *Server) allowedHost(host string) bool {
	if host == "" {
		return false
	}
	if host == s.expectedHost {
		return true
	}
	_, port, err := net.SplitHostPort(s.expectedHost)
	return err == nil && host == net.JoinHostPort("localhost", port)
}

// allowedOrigin reports whether a browser origin may use the API. The set is
// fixed by the configuration: the API's own origin (it serves the UI, at
// http://<address> or http://localhost:<port>) plus the configured browser
// origin, which is the hosted app (production, empty when that interface is
// disabled) or the Vite dev server (development). No wildcard, no patterns.
func (s *Server) allowedOrigin(origin string) bool {
	if origin == "" {
		return false
	}
	if origin == s.browserOrigin {
		return true
	}
	return strings.HasPrefix(origin, "http://") && s.allowedHost(strings.TrimPrefix(origin, "http://"))
}

// allowedOrigins lists the origins allowedOrigin accepts, for logs.
func (s *Server) allowedOrigins() []string {
	origins := []string{"http://" + s.expectedHost}
	if _, port, err := net.SplitHostPort(s.expectedHost); err == nil {
		origins = append(origins, "http://"+net.JoinHostPort("localhost", port))
	}
	if s.browserOrigin != "" {
		origins = append(origins, s.browserOrigin)
	}
	return origins
}

func (s *Server) securityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")

		if !s.allowedHost(r.Host) {
			writeAPIError(w, http.StatusForbidden, "invalid_host", "Host is not allowed")
			return
		}

		// The embedded UI is public and static. Navigations and same-origin
		// subresource loads carry no Origin, so these routes skip the Origin and
		// session checks; the Host check above still applies.
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) && webui.IsStaticPath(r.URL.Path) {
			w.Header().Del("Cache-Control")
			next.ServeHTTP(w, r)
			return
		}

		origin := requestOrigin(r)
		publicProbe := r.Method == http.MethodGet && (r.URL.Path == "/health" || r.URL.Path == "/version")
		if publicProbe && origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		if !s.allowedOrigin(origin) {
			writeAPIError(w, http.StatusForbidden, "invalid_origin", "Origin is not allowed")
			return
		}
		setCORS(w, origin)

		if r.Method == http.MethodOptions {
			if !validPreflightMethod(r.Header.Get("Access-Control-Request-Method")) {
				writeAPIError(w, http.StatusMethodNotAllowed, "invalid_preflight", "requested method is not allowed")
				return
			}
			if !validPreflightHeaders(r.Header.Get("Access-Control-Request-Headers")) {
				writeAPIError(w, http.StatusForbidden, "invalid_preflight", "requested headers are not allowed")
				return
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Bonsai-Session, Idempotency-Key")
			w.Header().Set("Access-Control-Max-Age", "600")
			if strings.EqualFold(r.Header.Get("Access-Control-Request-Private-Network"), "true") {
				w.Header().Set("Access-Control-Allow-Private-Network", "true")
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if publicProbe || (r.Method == http.MethodPost && r.URL.Path == "/api/session") {
			next.ServeHTTP(w, r)
			return
		}

		// WebSockets authenticate in their first data message so the token never
		// appears in the URL or browser-managed cookie state.
		if r.Method == http.MethodGet && (r.URL.Path == "/events" || isAgentTerminalRoute(r.URL.Path) || isProcessTerminalRoute(r.URL.Path)) {
			next.ServeHTTP(w, r)
			return
		}

		if !s.sessions.valid(strings.TrimSpace(r.Header.Get("X-Bonsai-Session"))) {
			writeAPIError(w, http.StatusUnauthorized, "invalid_session", "valid local session required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requestOrigin is the browser origin of r. Browsers omit Origin on
// same-origin GET and HEAD requests (every other method sends it), which is how the embedded UI reads the
// API. They do send Sec-Fetch-Site, which page scripts cannot set or forge, so
// "same-origin" there means the page came from this Host, which the caller has
// already validated. Other clients cannot use this to get further than an
// Origin header would take them: privileged routes still need a session.
func requestOrigin(r *http.Request) string {
	if origin := r.Header.Get("Origin"); origin != "" {
		return origin
	}
	if (r.Method == http.MethodGet || r.Method == http.MethodHead) && r.Header.Get("Sec-Fetch-Site") == "same-origin" {
		return "http://" + r.Host
	}
	return ""
}

func setCORS(w http.ResponseWriter, origin string) {
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Vary", "Origin")
}

func validPreflightMethod(method string) bool {
	switch method {
	case "", http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func validPreflightHeaders(value string) bool {
	if strings.TrimSpace(value) == "" {
		return true
	}
	allowed := map[string]struct{}{
		"content-type":     {},
		"x-bonsai-session": {},
		"idempotency-key":  {},
	}
	for _, header := range strings.Split(value, ",") {
		if _, ok := allowed[strings.ToLower(strings.TrimSpace(header))]; !ok {
			return false
		}
	}
	return true
}
