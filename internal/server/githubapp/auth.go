package githubapp

import (
	"crypto/rand"
	"crypto/subtle"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	store "github.com/Tiago-0liveira/bonsai/internal/storage/git"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Session struct {
	UserID    string    `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (m *Manager) Authenticate(r *http.Request) (string, error) {
	cookie, e := r.Cookie("bonsai_session")
	if e != nil {
		return "", domain.ErrAuth
	}
	var s Session
	var ok bool
	m.Store.View(func(d store.Data) error { s, ok = store.Get[Session](d, "sessions", Hash(cookie.Value)); return nil })
	if !ok || time.Now().After(s.ExpiresAt) {
		return "", domain.ErrAuth
	}
	return s.UserID, nil
}
func (m *Manager) Login(w http.ResponseWriter, r *http.Request) {
	state := rand.Text()
	if e := m.Store.Update(func(d store.Data) error {
		return store.Put(d, "oauth_states", Hash(state), time.Now().Add(10*time.Minute))
	}); e != nil {
		http.Error(w, "authentication unavailable", 503)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "bonsai_oauth_state", Value: state, Path: "/auth/github/callback", Secure: m.secureCookies(), HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 600})
	q := url.Values{"client_id": {m.ClientID}, "redirect_uri": {m.Origin + "/auth/github/callback"}, "state": {state}}
	http.Redirect(w, r, "https://github.com/login/oauth/authorize?"+q.Encode(), http.StatusFound)
}
func (m *Manager) Callback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	cookie, e := r.Cookie("bonsai_oauth_state")
	if e != nil || state == "" || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) != 1 {
		http.Error(w, "invalid OAuth state", 400)
		return
	}
	e = m.Store.Update(func(d store.Data) error {
		expiry, ok := store.Get[time.Time](d, "oauth_states", Hash(state))
		if !ok || time.Now().After(expiry) {
			return domain.ErrAuth
		}
		delete(d["oauth_states"], Hash(state))
		return nil
	})
	if e != nil {
		http.Error(w, "expired OAuth state", 400)
		return
	}
	token, e := m.exchange(r.Context(), map[string]string{"code": r.URL.Query().Get("code"), "redirect_uri": m.Origin + "/auth/github/callback"})
	if e != nil {
		http.Error(w, "GitHub sign-in failed", 401)
		return
	}
	var user struct{ ID int64 }
	if e = m.jsonRequest(r.Context(), "https://api.github.com/user", token.AccessToken, nil, &user); e != nil || user.ID <= 0 {
		http.Error(w, "GitHub identity unavailable", 401)
		return
	}
	id := strconv.FormatInt(user.ID, 10)
	if e = m.saveUser(id, token); e != nil {
		http.Error(w, "session unavailable", 503)
		return
	}
	secret := rand.Text()
	expiry := time.Now().Add(24 * time.Hour)
	if e = m.Store.Update(func(d store.Data) error {
		return store.Put(d, "sessions", Hash(secret), Session{UserID: id, ExpiresAt: expiry})
	}); e != nil {
		http.Error(w, "session unavailable", 503)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "bonsai_session", Value: secret, Path: "/", Secure: m.secureCookies(), HttpOnly: true, SameSite: http.SameSiteLaxMode, Expires: expiry})
	http.SetCookie(w, &http.Cookie{Name: "bonsai_oauth_state", Path: "/auth/github/callback", Secure: m.secureCookies(), HttpOnly: true, MaxAge: -1})
	http.Redirect(w, r, "/", http.StatusFound)
}
func (m *Manager) Logout(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != m.Origin {
		http.Error(w, "invalid origin", 403)
		return
	}
	if c, e := r.Cookie("bonsai_session"); e == nil {
		if e = m.Store.Update(func(d store.Data) error { delete(d["sessions"], Hash(c.Value)); return nil }); e != nil {
			http.Error(w, "logout unavailable", 503)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{Name: "bonsai_session", Path: "/", Secure: m.secureCookies(), HttpOnly: true, MaxAge: -1})
	w.WriteHeader(204)
}


func (m *Manager) secureCookies() bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(m.Origin)), "https://")
}
