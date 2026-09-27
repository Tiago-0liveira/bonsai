// bonsai-server hosts the Git API. Terminate TLS at a trusted reverse proxy or
// supply TLS_CERT/TLS_KEY. It never opens local repositories or invokes git/gh.
package main

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"github.com/Tiago-0liveira/bonsai/internal/git/github/app"
	api "github.com/Tiago-0liveira/bonsai/internal/server/git"
	"github.com/Tiago-0liveira/bonsai/internal/server/githubapp"
	"github.com/Tiago-0liveira/bonsai/internal/server/webhooks"
	store "github.com/Tiago-0liveira/bonsai/internal/storage/git"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type config struct {
	Origin       string       `json:"origin"`
	Address      string       `json:"address"`
	Database     string       `json:"database"`
	StaticDir    string       `json:"static_dir"`
	Repositories []repository `json:"repositories"`
}
type repository struct {
	ID                 string            `json:"id"`
	WorkspaceID        string            `json:"workspace_id"`
	FullName           string            `json:"full_name"`
	GitHubRepositoryID int64             `json:"github_repository_id"`
	InstallationID     int64             `json:"installation_id"`
	DefaultBranch      string            `json:"default_branch"`
	Members            map[string]string `json:"members"`
}

func main() {
	if e := run(); e != nil {
		log.Fatal(e)
	}
}
func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: bonsai-server <config.json>")
	}
	b, e := os.ReadFile(os.Args[1])
	if e != nil {
		return e
	}
	var cfg config
	if e = json.Unmarshal(b, &cfg); e != nil {
		return e
	}
	cfg.Origin = strings.TrimRight(cfg.Origin, "/")
	if !strings.HasPrefix(cfg.Origin, "https://") {
		return fmt.Errorf("origin must use HTTPS")
	}
	if cfg.Address == "" {
		cfg.Address = "127.0.0.1:8080"
	}
	if cfg.Database == "" {
		return fmt.Errorf("database path required")
	}
	if err := os.MkdirAll(filepath.Dir(cfg.Database), 0700); err != nil {
		return err
	}
	lock, err := procstore.TryLock(cfg.Database + ".lock")
	if err != nil {
		return err
	}
	defer lock.Unlock()
	st, e := store.Open(cfg.Database)
	if e != nil {
		return e
	}
	key, e := base64.StdEncoding.DecodeString(os.Getenv("BONSAI_TOKEN_KEY"))
	if e != nil || len(key) != 32 {
		return fmt.Errorf("BONSAI_TOKEN_KEY must be a base64 encoded 32-byte key")
	}
	pemBytes, e := os.ReadFile(os.Getenv("GITHUB_APP_PRIVATE_KEY_FILE"))
	if e != nil {
		return e
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return fmt.Errorf("invalid App private key")
	}
	var private *rsa.PrivateKey
	if k, e := x509.ParsePKCS1PrivateKey(block.Bytes); e == nil {
		private = k
	} else {
		k, e := x509.ParsePKCS8PrivateKey(block.Bytes)
		if e != nil {
			return e
		}
		var ok bool
		private, ok = k.(*rsa.PrivateKey)
		if !ok {
			return fmt.Errorf("App key must be RSA")
		}
	}
	repos := []api.Repository{}
	for _, r := range cfg.Repositories {
		repos = append(repos, api.Repository{ID: r.ID, WorkspaceID: r.WorkspaceID, FullName: r.FullName, GitHubRepositoryID: r.GitHubRepositoryID, InstallationID: r.InstallationID, DefaultBranch: r.DefaultBranch, Members: r.Members})
	}
	auth := &githubapp.Manager{AppID: os.Getenv("GITHUB_APP_ID"), ClientID: os.Getenv("GITHUB_APP_CLIENT_ID"), ClientSecret: os.Getenv("GITHUB_APP_CLIENT_SECRET"), PrivateKey: private, EncryptionKey: key, Store: st, Origin: cfg.Origin}
	auth.Resolve = func(full string) (githubapp.RepositoryAccess, error) {
		for _, r := range repos {
			if r.FullName == full {
				var revoked bool
				st.View(func(d store.Data) error { revoked, _ = store.Get[bool](d, "revoked_repositories", r.ID); return nil })
				if revoked {
					return githubapp.RepositoryAccess{}, domain.ErrForbidden
				}
				return githubapp.RepositoryAccess{InstallationID: r.InstallationID, RepositoryID: r.GitHubRepositoryID}, nil
			}
		}
		return githubapp.RepositoryAccess{}, domain.ErrForbidden
	}
	svc, e := api.New(st, auth, app.New(auth), repos, cfg.Origin)
	if e != nil {
		return e
	}
	secret := os.Getenv("GITHUB_WEBHOOK_SECRET")
	if secret == "" || auth.AppID == "" || auth.ClientID == "" || auth.ClientSecret == "" {
		return fmt.Errorf("GitHub App credentials and webhook secret are required")
	}
	defer svc.Close()
	hooks := webhooks.New([]byte(secret), st, svc.Webhook)
	mux := http.NewServeMux()
	svc.Register(mux)
	mux.Handle("POST /webhooks/github", hooks)
	if cfg.StaticDir != "" {
		fs := http.FileServer(http.Dir(cfg.StaticDir))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/auth/") {
				http.NotFound(w, r)
				return
			}
			if !strings.Contains(r.URL.Path, ".") {
				http.ServeFile(w, r, cfg.StaticDir+"/index.html")
				return
			}
			fs.ServeHTTP(w, r)
		})
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go svc.Run(ctx)
	go hooks.Run(ctx)
	server := &http.Server{Addr: cfg.Address, Handler: mux, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 32 << 10}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("Bonsai Git API listening at %s", cfg.Address)
	if os.Getenv("TLS_CERT") != "" {
		e = server.ListenAndServeTLS(os.Getenv("TLS_CERT"), os.Getenv("TLS_KEY"))
	} else {
		e = server.ListenAndServe()
	}
	if e == http.ErrServerClosed {
		return nil
	}
	return e
}
