package githubapp

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	store "github.com/Tiago-0liveira/bonsai/internal/storage/git"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestInstallationScopeTokenCacheAndUserIdentity(t *testing.T) {
	st, _ := store.Open(filepath.Join(t.TempDir(), "db.json"))
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	calls := 0
	m := &Manager{AppID: "app", PrivateKey: key, Store: st, EncryptionKey: make([]byte, 32), Resolve: func(repo string) (RepositoryAccess, error) {
		if repo != "owner/repo" {
			return RepositoryAccess{}, domain.ErrForbidden
		}
		return RepositoryAccess{InstallationID: 10, RepositoryID: 42}, nil
	}}
	m.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != "/app/installations/10/access_tokens" {
			t.Error(r.URL)
		}
		if len(strings.Split(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), ".")) != 3 {
			t.Error("missing app JWT")
		}
		var body struct {
			RepositoryIDs []int64 `json:"repository_ids"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if len(body.RepositoryIDs) != 1 || body.RepositoryIDs[0] != 42 {
			t.Error("unscoped installation token", body)
		}
		b, _ := json.Marshal(map[string]any{"token": "installation-token", "expires_at": time.Now().Add(time.Hour)})
		return &http.Response{StatusCode: 201, Body: io.NopCloser(strings.NewReader(string(b))), Header: make(http.Header)}, nil
	})}
	for i := 0; i < 2; i++ {
		token, e := m.Token(context.Background(), "owner/repo", false)
		if e != nil || token != "installation-token" {
			t.Fatal(token, e)
		}
	}
	if calls != 1 {
		t.Fatal("uncached token", calls)
	}
	if _, e := m.Token(context.Background(), "owner/other", false); e != domain.ErrForbidden {
		t.Fatal(e)
	}
	if _, e := m.Token(context.Background(), "owner/repo", true); e != domain.ErrAuth {
		t.Fatal("mutation used installation identity", e)
	}
	if e := m.saveUser("user", UserToken{AccessToken: "user-access", ExpiresAt: time.Now().Add(time.Hour)}); e != nil {
		t.Fatal(e)
	}
	token, e := m.Token(WithUser(context.Background(), "user"), "owner/repo", true)
	if e != nil || token != "user-access" {
		t.Fatal(token, e)
	}
	st.View(func(d store.Data) error {
		if strings.Contains(string(d["github_users"]["user"]), "user-access") {
			t.Error("plaintext access token persisted")
		}
		return nil
	})
	m.Invalidate()
	if _, e = m.Token(context.Background(), "owner/repo", false); e != nil || calls != 2 {
		t.Fatal(calls, e)
	}
}
