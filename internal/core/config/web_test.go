package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestEnsureWebConfigWritesDefaultsOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bonsai", "web.json")
	cfg, created, err := EnsureWebConfig(path)
	if err != nil || !created {
		t.Fatalf("EnsureWebConfig = %+v, %v, %v", cfg, created, err)
	}
	if cfg.Revision != 1 || cfg.APIPort != DefaultWebAPIPort || !cfg.OpenBrowser ||
		!cfg.Interfaces.Local || !cfg.Interfaces.Hosted || cfg.Updates.Mode != WebUpdatesStandard ||
		cfg.SetupVersion != 0 || cfg.Updates.Live.WebhookPort != DefaultWebWebhookPort {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("web.json mode = %v, want 0600", info.Mode().Perm())
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"version", "revision", "setup_version", "api_port", "open_browser", "interfaces", "updates"} {
		if _, ok := generic[key]; !ok {
			t.Fatalf("web.json lacks %q: %s", key, raw)
		}
	}
	if _, ok := generic["workspaces"]; ok {
		t.Fatalf("web.json must leave the reserved workspaces key unused: %s", raw)
	}

	again, created, err := EnsureWebConfig(path)
	if err != nil || created || again.Revision != 1 {
		t.Fatalf("second EnsureWebConfig = %+v, %v, %v", again, created, err)
	}
}

func TestUpdateWebConfigRevisionAndValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "web.json")
	if _, _, err := EnsureWebConfig(path); err != nil {
		t.Fatal(err)
	}
	cfg, err := UpdateWebConfig(path, 1, func(c *WebConfig) error { c.APIPort = 7011; return nil })
	if err != nil || cfg.Revision != 2 || cfg.APIPort != 7011 {
		t.Fatalf("update = %+v, %v", cfg, err)
	}
	if _, err := UpdateWebConfig(path, 1, func(c *WebConfig) error { return nil }); !errors.Is(err, ErrWebConfigRevision) {
		t.Fatalf("stale revision err = %v", err)
	}
	if _, err := UpdateWebConfig(path, 2, func(c *WebConfig) error { c.APIPort = 0; return nil }); err == nil {
		t.Fatal("invalid port accepted")
	}
	if _, err := UpdateWebConfig(path, 2, func(c *WebConfig) error { c.Updates.Live.WebhookPort = 7011; return nil }); err == nil {
		t.Fatal("webhook port equal to api port accepted")
	}
	if _, err := UpdateWebConfig(path, 2, func(c *WebConfig) error { c.StartupTimeoutSeconds = 601; return nil }); err == nil {
		t.Fatal("out-of-range startup timeout accepted")
	}
	if _, err := UpdateWebConfig(path, 2, func(c *WebConfig) error { c.Interfaces = WebInterfaces{}; return nil }); err == nil {
		t.Fatal("both browser interfaces disabled accepted")
	}
	got, exists, err := ReadWebConfig(path)
	if err != nil || !exists || got.Revision != 2 || got.APIPort != 7011 {
		t.Fatalf("rejected updates were persisted: %+v, %v", got, err)
	}
}

func TestUpdateWebConfigSerializesWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "web.json")
	if _, _, err := EnsureWebConfig(path); err != nil {
		t.Fatal(err)
	}
	const writers = 8
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := UpdateWebConfig(path, ^uint64(0), func(c *WebConfig) error { return nil }); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	cfg, _, err := ReadWebConfig(path)
	if err != nil || cfg.Revision != 1+writers {
		t.Fatalf("revision = %d, %v; want %d", cfg.Revision, err, 1+writers)
	}
}

func TestReadWebConfigRejectsCorruptAndUnknownVersion(t *testing.T) {
	dir := t.TempDir()
	corrupt := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(corrupt, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadWebConfig(corrupt); err == nil || !strings.Contains(err.Error(), corrupt) {
		t.Fatalf("corrupt err = %v", err)
	}
	future := filepath.Join(dir, "future.json")
	if err := os.WriteFile(future, []byte(`{"version":2,"api_port":7001,"updates":{"mode":"standard"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadWebConfig(future); err == nil {
		t.Fatal("future version accepted")
	}
}

func TestWebStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "web-state.json")
	state, err := ReadWebState(path)
	if err != nil || state.Version != WebStateVersion || state.Revision != 0 {
		t.Fatalf("empty state = %+v, %v", state, err)
	}
	state, err = UpdateWebState(path, func(*WebState) error { return nil })
	if err != nil || state.Revision != 1 {
		t.Fatalf("update = %+v, %v", state, err)
	}
	if got, err := ReadWebState(path); err != nil || got.Revision != 1 {
		t.Fatalf("read back = %+v, %v", got, err)
	}
}

func TestWebHomeIsUserLevel(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	}
	home, err := WebHome()
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(home) || filepath.Base(home) != "web" || filepath.Base(filepath.Dir(home)) != "bonsai" {
		t.Fatalf("WebHome = %q", home)
	}
	if runtime.GOOS == "linux" {
		t.Setenv("XDG_STATE_HOME", "relative")
		if _, err := UserStateDir(); err == nil {
			t.Fatal("relative XDG_STATE_HOME accepted")
		}
	}
}

func TestWebConfigValidatesLiveOnlyWhenOn(t *testing.T) {
	cfg := DefaultWebConfig()
	cfg.Updates.Live.Tunnel = "frp" // ignored while updates are standard
	if err := cfg.Validate(); err != nil {
		t.Fatalf("standard mode checked the live section: %v", err)
	}
	cfg.Updates.Mode = WebUpdatesLive
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "updates.live") {
		t.Fatalf("unknown tunnel accepted in live mode: %v", err)
	}
	cfg.Updates.Live.Tunnel = "cloudflared-quick"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default live config rejected: %v", err)
	}
	cfg.Updates.Live.WebhookPort = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("live mode without a webhook port accepted")
	}
	cfg.Updates.Live.WebhookPort = DefaultWebWebhookPort
	cfg.Updates.Live.Tunnel = "cloudflared-named"
	if err := cfg.Validate(); err == nil {
		t.Fatal("named tunnel without a name accepted")
	}
	cfg.Updates.Live.TunnelName, cfg.Updates.Live.PublicURL = "bonsai", "https://hooks.example.com"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("named tunnel rejected: %v", err)
	}
}

func TestWebWebhookSecretLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bonsai", "web-webhook-secret")
	if _, err := ReadWebWebhookSecret(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing secret: %v", err)
	}
	first, err := EnsureWebWebhookSecret(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 64 {
		t.Fatalf("secret is %d characters, want 64 hex", len(first))
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("secret mode = %v, want 0600", info.Mode().Perm())
		}
	}
	again, err := EnsureWebWebhookSecret(path)
	if err != nil || string(again) != string(first) {
		t.Fatalf("second ensure changed the secret: %v", err)
	}
	read, err := ReadWebWebhookSecret(path)
	if err != nil || string(read) != string(first) {
		t.Fatalf("read = %v", err)
	}
	rotated, err := RotateWebWebhookSecret(path)
	if err != nil || string(rotated) == string(first) {
		t.Fatalf("rotate did not change the secret: %v", err)
	}
	if read, _ := ReadWebWebhookSecret(path); string(read) != string(rotated) {
		t.Fatal("rotated secret not persisted")
	}
	if err := os.WriteFile(path, []byte("not-hex\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadWebWebhookSecret(path); err == nil || !strings.Contains(err.Error(), "delete it") {
		t.Fatalf("corrupt secret: %v", err)
	}
	if _, err := EnsureWebWebhookSecret(path); err == nil {
		t.Fatal("ensure silently replaced a corrupt secret")
	}
}
