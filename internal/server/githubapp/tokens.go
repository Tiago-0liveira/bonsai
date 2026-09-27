// Package githubapp owns App installation and user authentication, independent
// of the GitHub domain adapter. No personal access tokens are accepted.
package githubapp

import (
	"bytes"
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	store "github.com/Tiago-0liveira/bonsai/internal/storage/git"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type identityKey struct{}

func WithUser(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}
func User(ctx context.Context) string { v, _ := ctx.Value(identityKey{}).(string); return v }

type RepositoryAccess struct {
	InstallationID int64
	RepositoryID   int64
}
type UserToken struct {
	AccessToken      string    `json:"access_token"`
	RefreshToken     string    `json:"refresh_token"`
	ExpiresAt        time.Time `json:"expires_at"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
}
type cached struct {
	Token     string
	ExpiresAt time.Time
}
type Manager struct {
	AppID, ClientID, ClientSecret, Origin string
	PrivateKey                            *rsa.PrivateKey
	EncryptionKey                         []byte
	Store                                 *store.Store
	HTTP                                  *http.Client
	Resolve                               func(string) (RepositoryAccess, error)
	mu                                    sync.Mutex
	installations                         map[string]cached
}

func (m *Manager) client() *http.Client {
	if m.HTTP != nil {
		return m.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func (m *Manager) jwt() (string, error) {
	if m.PrivateKey == nil || m.AppID == "" {
		return "", domain.ErrAuth
	}
	now := time.Now()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	body, _ := json.Marshal(map[string]any{"iat": now.Add(-time.Minute).Unix(), "exp": now.Add(9 * time.Minute).Unix(), "iss": m.AppID})
	text := header + "." + base64.RawURLEncoding.EncodeToString(body)
	hash := sha256.Sum256([]byte(text))
	signature, e := rsa.SignPKCS1v15(rand.Reader, m.PrivateKey, crypto.SHA256, hash[:])
	return text + "." + base64.RawURLEncoding.EncodeToString(signature), e
}
func (m *Manager) Token(ctx context.Context, repo string, mutation bool) (string, error) {
	if m.Resolve == nil {
		return "", domain.ErrForbidden
	}
	access, e := m.Resolve(repo)
	if e != nil {
		return "", e
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if mutation {
		user := User(ctx)
		if user == "" {
			return "", domain.ErrAuth
		}
		t, e := m.loadUser(user)
		if e != nil {
			return "", e
		}
		if time.Now().Add(time.Minute).Before(t.ExpiresAt) {
			return t.AccessToken, nil
		}
		if t.RefreshToken == "" || time.Now().After(t.RefreshExpiresAt) {
			return "", domain.ErrAuth
		}
		t, e = m.exchange(ctx, map[string]string{"grant_type": "refresh_token", "refresh_token": t.RefreshToken})
		if e != nil {
			return "", e
		}
		if e = m.saveUser(user, t); e != nil {
			return "", e
		}
		return t.AccessToken, nil
	}
	if m.installations == nil {
		m.installations = map[string]cached{}
	}
	if v := m.installations[repo]; time.Now().Add(time.Minute).Before(v.ExpiresAt) {
		return v.Token, nil
	}
	if access.InstallationID <= 0 || access.RepositoryID <= 0 {
		return "", domain.ErrForbidden
	}
	jwt, e := m.jwt()
	if e != nil {
		return "", e
	}
	var result struct {
		Token     string
		ExpiresAt time.Time `json:"expires_at"`
	}
	e = m.jsonRequest(ctx, "https://api.github.com/app/installations/"+strconv.FormatInt(access.InstallationID, 10)+"/access_tokens", jwt, map[string]any{"repository_ids": []int64{access.RepositoryID}}, &result)
	if e != nil {
		return "", e
	}
	if result.Token == "" {
		return "", domain.ErrAuth
	}
	m.installations[repo] = cached{result.Token, result.ExpiresAt}
	return result.Token, nil
}
func (m *Manager) Invalidate() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.installations = map[string]cached{}
}
func (m *Manager) jsonRequest(ctx context.Context, url, token string, body, out any) error {
	method := "GET"
	var b []byte
	if body != nil {
		method = "POST"
		var e error
		b, e = json.Marshal(body)
		if e != nil {
			return e
		}
	}
	req, e := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(b))
	if e != nil {
		return e
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, e := m.client().Do(req)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return domain.E("unauthorized", fmt.Sprintf("GitHub authentication failed (%d)", res.StatusCode))
	}
	return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out)
}
func (m *Manager) exchange(ctx context.Context, body map[string]string) (UserToken, error) {
	body["client_id"] = m.ClientID
	body["client_secret"] = m.ClientSecret
	var response struct {
		AccessToken      string `json:"access_token"`
		RefreshToken     string `json:"refresh_token"`
		ExpiresIn        int64  `json:"expires_in"`
		RefreshExpiresIn int64  `json:"refresh_token_expires_in"`
		Error            string `json:"error"`
	}
	e := m.jsonRequest(ctx, "https://github.com/login/oauth/access_token", "", body, &response)
	if e != nil {
		return UserToken{}, e
	}
	if response.AccessToken == "" || response.Error != "" {
		return UserToken{}, domain.ErrAuth
	}
	if response.ExpiresIn <= 0 {
		return UserToken{}, domain.E("unauthorized", "enable expiring user tokens for the GitHub App")
	}
	now := time.Now()
	return UserToken{AccessToken: response.AccessToken, RefreshToken: response.RefreshToken, ExpiresAt: now.Add(time.Duration(response.ExpiresIn) * time.Second), RefreshExpiresAt: now.Add(time.Duration(response.RefreshExpiresIn) * time.Second)}, nil
}
func (m *Manager) aead() (cipher.AEAD, error) {
	block, e := aes.NewCipher(m.EncryptionKey)
	if e != nil {
		return nil, e
	}
	return cipher.NewGCM(block)
}
func (m *Manager) saveUser(user string, t UserToken) error {
	a, e := m.aead()
	if e != nil {
		return e
	}
	nonce := make([]byte, a.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return e
	}
	b, _ := json.Marshal(t)
	sealed := a.Seal(nonce, nonce, b, []byte(user))
	return m.Store.Update(func(d store.Data) error {
		return store.Put(d, "github_users", user, base64.StdEncoding.EncodeToString(sealed))
	})
}
func (m *Manager) loadUser(user string) (UserToken, error) {
	var text string
	m.Store.View(func(d store.Data) error { text, _ = store.Get[string](d, "github_users", user); return nil })
	raw, e := base64.StdEncoding.DecodeString(text)
	if e != nil {
		return UserToken{}, domain.ErrAuth
	}
	a, e := m.aead()
	if e != nil {
		return UserToken{}, e
	}
	if len(raw) < a.NonceSize() {
		return UserToken{}, domain.ErrAuth
	}
	plain, e := a.Open(nil, raw[:a.NonceSize()], raw[a.NonceSize():], []byte(user))
	if e != nil {
		return UserToken{}, domain.ErrAuth
	}
	var t UserToken
	e = json.Unmarshal(plain, &t)
	return t, e
}
func Hash(secret string) string { sum := sha256.Sum256([]byte(secret)); return fmt.Sprintf("%x", sum) }
func bearer(r *http.Request) string {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}
