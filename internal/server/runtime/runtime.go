package runtime

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"github.com/Tiago-0liveira/bonsai/internal/git/github/app"
	api "github.com/Tiago-0liveira/bonsai/internal/server/git"
	"github.com/Tiago-0liveira/bonsai/internal/server/githubapp"
	"github.com/Tiago-0liveira/bonsai/internal/server/webhooks"
	store "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

type Config struct {
	Origin       string       `json:"origin"`
	Address      string       `json:"address"`
	Database     string       `json:"database"`
	StaticDir    string       `json:"static_dir"`
	Repositories []Repository `json:"repositories"`
}

type Repository struct {
	ID                 string            `json:"id"`
	WorkspaceID        string            `json:"workspace_id"`
	FullName           string            `json:"full_name"`
	GitHubRepositoryID int64             `json:"github_repository_id"`
	InstallationID     int64             `json:"installation_id"`
	DefaultBranch      string            `json:"default_branch"`
	Members            map[string]string `json:"members"`
}

func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	cfg.Origin = strings.TrimRight(cfg.Origin, "/")
	if !strings.HasPrefix(cfg.Origin, "https://") {
		return Config{}, fmt.Errorf("origin must use HTTPS")
	}
	if cfg.Database == "" {
		return Config{}, fmt.Errorf("database path required")
	}
	return cfg, nil
}

// RunAPI starts only the Bonsai API. The serve path always binds it to loopback.
func RunAPI(configPath, address string) error {
	cfg, err := LoadConfig(configPath)
	if err != nil {
		return err
	}
	if err := requireLoopback(address); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(cfg.Database), 0o700); err != nil {
		return err
	}
	lock, err := procstore.TryLock(cfg.Database + ".lock")
	if err != nil {
		return err
	}
	defer lock.Unlock()
	st, err := store.Open(cfg.Database)
	if err != nil {
		return err
	}
	svc, err := buildService(cfg, st)
	if err != nil {
		return err
	}
	defer svc.Close()

	mux := http.NewServeMux()
	svc.Register(mux)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go svc.Run(ctx)

	server := &http.Server{
		Addr:              address,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	go shutdownWithContext(ctx, server)
	log.Printf("Bonsai API listening at %s", address)
	err = server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// RunWebhookShell starts the isolated webhook-only HTTP boundary. It verifies
// GitHub signatures before parsing and intentionally exposes no other route.
func RunWebhookShell(configPath, address string) error {
	if _, err := LoadConfig(configPath); err != nil {
		return err
	}
	if err := requireLoopback(address); err != nil {
		return err
	}
	secret := os.Getenv("GITHUB_WEBHOOK_SECRET")
	if secret == "" {
		return fmt.Errorf("GITHUB_WEBHOOK_SECRET is required")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /github/webhook", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
			return
		}
		if !webhooks.Verify([]byte(secret), body, r.Header.Get("X-Hub-Signature-256")) {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("X-GitHub-Delivery") == "" || r.Header.Get("X-GitHub-Event") == "" || !json.Valid(body) {
			http.Error(w, "invalid delivery", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server := &http.Server{
		Addr:              address,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	go shutdownWithContext(ctx, server)
	log.Printf("Bonsai webhook listener at %s", address)
	err := server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func buildService(cfg Config, st *store.Store) (*api.Service, error) {
	key, err := base64.StdEncoding.DecodeString(os.Getenv("BONSAI_TOKEN_KEY"))
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("BONSAI_TOKEN_KEY must be a base64 encoded 32-byte key")
	}
	pemBytes, err := os.ReadFile(os.Getenv("GITHUB_APP_PRIVATE_KEY_FILE"))
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("invalid App private key")
	}
	var private *rsa.PrivateKey
	if key1, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		private = key1
	} else {
		key8, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		var ok bool
		private, ok = key8.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("App key must be RSA")
		}
	}

	repos := make([]api.Repository, 0, len(cfg.Repositories))
	for _, r := range cfg.Repositories {
		repos = append(repos, api.Repository{
			ID:                 r.ID,
			WorkspaceID:        r.WorkspaceID,
			FullName:           r.FullName,
			GitHubRepositoryID: r.GitHubRepositoryID,
			InstallationID:     r.InstallationID,
			DefaultBranch:      r.DefaultBranch,
			Members:            r.Members,
		})
	}
	auth := &githubapp.Manager{
		AppID:         os.Getenv("GITHUB_APP_ID"),
		ClientID:      os.Getenv("GITHUB_APP_CLIENT_ID"),
		ClientSecret:  os.Getenv("GITHUB_APP_CLIENT_SECRET"),
		PrivateKey:    private,
		EncryptionKey: key,
		Store:         st,
		Origin:        cfg.Origin,
	}
	if auth.AppID == "" || auth.ClientID == "" || auth.ClientSecret == "" {
		return nil, fmt.Errorf("GitHub App credentials are required")
	}
	auth.Resolve = func(full string) (githubapp.RepositoryAccess, error) {
		for _, r := range repos {
			if r.FullName == full {
				var revoked bool
				_ = st.View(func(d store.Data) error {
					revoked, _ = store.Get[bool](d, "revoked_repositories", r.ID)
					return nil
				})
				if revoked {
					return githubapp.RepositoryAccess{}, domain.ErrForbidden
				}
				return githubapp.RepositoryAccess{
					InstallationID: r.InstallationID,
					RepositoryID:   r.GitHubRepositoryID,
				}, nil
			}
		}
		return githubapp.RepositoryAccess{}, domain.ErrForbidden
	}
	return api.New(st, auth, app.New(auth), repos, cfg.Origin)
}

func requireLoopback(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid listen address %q: %w", address, err)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("serve address must be loopback, got %q", host)
	}
	return nil
}

func shutdownWithContext(ctx context.Context, server *http.Server) {
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdown)
}
