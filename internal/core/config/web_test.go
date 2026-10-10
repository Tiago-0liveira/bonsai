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
		cfg.SetupVersion != WebSetupVersion || cfg.Updates.Live.WebhookPort != DefaultWebWebhookPort {
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
