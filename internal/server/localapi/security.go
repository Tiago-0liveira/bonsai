package localapi

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

const ProductionBrowserOrigin = "https://app.bonsai.dev"

var developmentOrigins = map[string]struct{}{
	"http://localhost:5173": {},
	"http://127.0.0.1:5173": {},
}

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

func validateBrowserOrigin(origin string, development bool) error {
	if origin == ProductionBrowserOrigin {
		return nil
	}
	if !development {
		return fmt.Errorf("browser origin must be exactly %s", ProductionBrowserOrigin)
	}
	if _, ok := developmentOrigins[origin]; !ok {
		return fmt.Errorf("development origin must be http://localhost:5173 or http://127.0.0.1:5173")
	}
	return nil
}

func (s *Server) securityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")

		if r.Host != s.expectedHost {
			writeAPIError(w, http.StatusForbidden, "invalid_host", "Host is not allowed")
			return
		}

		origin := r.Header.Get("Origin")
		publicProbe := r.Method == http.MethodGet && (r.URL.Path == "/health" || r.URL.Path == "/version")
		if publicProbe && origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		if origin != s.browserOrigin {
			writeAPIError(w, http.StatusForbidden, "invalid_origin", "Origin is not allowed")
			return
		}
		setCORS(w, s.browserOrigin)

		if r.Method == http.MethodOptions {
			if !validPreflightMethod(r.Header.Get("Access-Control-Request-Method")) {
				writeAPIError(w, http.StatusMethodNotAllowed, "invalid_preflight", "requested method is not allowed")
				return
			}
			if !validPreflightHeaders(r.Header.Get("Access-Control-Request-Headers")) {
				writeAPIError(w, http.StatusForbidden, "invalid_preflight", "requested headers are not allowed")
				return
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
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
		if r.Method == http.MethodGet && r.URL.Path == "/events" {
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

func setCORS(w http.ResponseWriter, origin string) {
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Vary", "Origin")
}

func validPreflightMethod(method string) bool {
	switch method {
	case "", http.MethodGet, http.MethodPost, http.MethodDelete:
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
		"content-type": {},
		"x-bonsai-session": {},
		"idempotency-key": {},
	}
	for _, header := range strings.Split(value, ",") {
		if _, ok := allowed[strings.ToLower(strings.TrimSpace(header))]; !ok {
			return false
		}
	}
	return true
}
