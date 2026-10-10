package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

// Web configuration lives next to project-roots.json and follows the same
// rules: versioned, revisioned, cross-process locked and written atomically.
// web.json is what the user chooses; web-state.json is what bonsai learns at
// runtime. Neither ever holds a secret.
const (
	WebConfigVersion = 1
	WebStateVersion  = 1
	// WebSetupVersion is the setup flow this build ships. A web.json with an
	// older setup_version (including 0, "defaults written without setup") is
	// offered the setup flow once that exists (Phase 5).
	WebSetupVersion = 0

	DefaultWebAPIPort     = 7001
	DefaultWebWebhookPort = 7002

	WebUpdatesStandard = "standard"
	WebUpdatesLive     = "live"
)

type WebConfig struct {
	Version      int           `json:"version"`
	Revision     uint64        `json:"revision"`
	SetupVersion int           `json:"setup_version"`
	APIPort      int           `json:"api_port"`
	OpenBrowser  bool          `json:"open_browser"`
	Interfaces   WebInterfaces `json:"interfaces"`
	Updates      WebUpdates    `json:"updates"`
	// Zero means the daemon defaults (30 s to become ready, 5 s to stop).
	StartupTimeoutSeconds  int `json:"startup_timeout_seconds,omitempty"`
	ShutdownTimeoutSeconds int `json:"shutdown_timeout_seconds,omitempty"`
}

type WebInterfaces struct {
	Local  bool `json:"local"`
	Hosted bool `json:"hosted"`
}

type WebUpdates struct {
	Mode string         `json:"mode"`
	Live WebLiveUpdates `json:"live"`
}

type WebLiveUpdates struct {
	Tunnel       string   `json:"tunnel"`
	WebhookPort  int      `json:"webhook_port"`
	PublicURL    string   `json:"public_url"`
	Command      []string `json:"command"`
	Repositories []string `json:"repositories"`
}

// WebState is machine-managed runtime state (hook IDs, last public URL, ...).
// Later phases add fields here; secrets never go in it (they live in their
// own 0600 files).
type WebState struct {
	Version  int    `json:"version"`
	Revision uint64 `json:"revision"`
}

// DefaultWebConfig is the configuration written when none exists: the local
// UI plus the hosted app, standard (polling) updates, browser auto-open.
func DefaultWebConfig() WebConfig {
	return WebConfig{
		Version:      WebConfigVersion,
		SetupVersion: WebSetupVersion,
		APIPort:      DefaultWebAPIPort,
		OpenBrowser:  true,
		Interfaces:   WebInterfaces{Local: true, Hosted: true},
		Updates: WebUpdates{
			Mode: WebUpdatesStandard,
			Live: WebLiveUpdates{
				Tunnel:       "cloudflared-quick",
				WebhookPort:  DefaultWebWebhookPort,
				Command:      []string{},
				Repositories: []string{},
			},
		},
	}
}

var ErrWebConfigRevision = errors.New("web settings changed; reload and retry")

func webConfigDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "bonsai"), nil
}

// WebConfigPath is <user config dir>/bonsai/web.json.
func WebConfigPath() (string, error) {
	dir, err := webConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "web.json"), nil
}

// WebStatePath is <user config dir>/bonsai/web-state.json.
func WebStatePath() (string, error) {
	dir, err := webConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "web-state.json"), nil
}

// UserStateDir is the per-user directory for runtime state that is neither
// configuration nor disposable cache: $XDG_STATE_HOME or ~/.local/state on
// Unix, ~/Library/Application Support on macOS and %LocalAppData% on Windows.
func UserStateDir() (string, error) {
	switch runtime.GOOS {
	case "windows":
		if dir := os.Getenv("LocalAppData"); dir != "" {
			return dir, nil
		}
		return "", errors.New("%LocalAppData% is not defined")
	case "darwin", "ios":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support"), nil
	default:
		if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
			if !filepath.IsAbs(dir) {
				return "", errors.New("path in $XDG_STATE_HOME is relative")
			}
			return dir, nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "state"), nil
	}
}

// WebHome is the user-level daemon home that supervises `bonsai web`. It is
// deliberately not a Git repository: the daemon keeps its .bonsai runtime
// directory, socket hash and lock under it exactly as a per-repo daemon would.
func WebHome() (string, error) {
	dir, err := UserStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "bonsai", "web"), nil
}

// Validate rejects configurations bonsai cannot run. It never rewrites values.
func (c WebConfig) Validate() error {
	if c.Version != WebConfigVersion {
		return fmt.Errorf("unsupported web settings version %d", c.Version)
	}
	if c.APIPort < 1 || c.APIPort > 65535 {
		return fmt.Errorf("api_port %d is not a valid port", c.APIPort)
	}
	switch c.Updates.Mode {
	case WebUpdatesStandard, WebUpdatesLive:
	default:
		return fmt.Errorf("updates.mode must be %q or %q", WebUpdatesStandard, WebUpdatesLive)
	}
	if c.StartupTimeoutSeconds < 0 || c.StartupTimeoutSeconds > 600 || c.ShutdownTimeoutSeconds < 0 || c.ShutdownTimeoutSeconds > 600 {
		return fmt.Errorf("startup/shutdown timeouts must be between 0 and 600 seconds")
	}
	if p := c.Updates.Live.WebhookPort; p != 0 && (p < 1 || p > 65535 || p == c.APIPort) {
		return fmt.Errorf("updates.live.webhook_port %d must be a valid port different from api_port", p)
	}
	return nil
}

// ReadWebConfig loads web.json. A missing file reports exists=false together
// with the defaults; a corrupt or invalid file is an error naming the path.
func ReadWebConfig(path string) (cfg WebConfig, exists bool, err error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultWebConfig(), false, nil
	}
	if err != nil {
		return WebConfig{}, false, err
	}
	cfg = DefaultWebConfig()
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return WebConfig{}, true, fmt.Errorf("read web settings %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return WebConfig{}, true, fmt.Errorf("invalid web settings %s: %w", path, err)
	}
	if cfg.Updates.Live.Command == nil {
		cfg.Updates.Live.Command = []string{}
	}
	if cfg.Updates.Live.Repositories == nil {
		cfg.Updates.Live.Repositories = []string{}
	}
	return cfg, true, nil
}

// UpdateWebConfig runs fn on the current configuration under the cross-process
// lock and persists the result with the revision bumped. expected guards
// against lost updates; pass the revision the caller read, or ^uint64(0) to
// skip the check (used when writing first-run defaults).
func UpdateWebConfig(path string, expected uint64, fn func(*WebConfig) error) (WebConfig, error) {
	var out WebConfig
	err := withWebLock(path, func() error {
		cfg, _, err := ReadWebConfig(path)
		if err != nil {
			return err
		}
		if expected != ^uint64(0) && cfg.Revision != expected {
			return ErrWebConfigRevision
		}
		if err := fn(&cfg); err != nil {
			return err
		}
		cfg.Version = WebConfigVersion
		if err := cfg.Validate(); err != nil {
			return err
		}
		cfg.Revision++
		raw, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return err
		}
		if err := gitstore.WriteJSON(path, append(raw, '\n')); err != nil {
			return err
		}
		out = cfg
		return nil
	})
	return out, err
}

// EnsureWebConfig returns web.json, writing the defaults first when it does not
// exist. created reports whether this call wrote the file.
func EnsureWebConfig(path string) (cfg WebConfig, created bool, err error) {
	err = withWebLock(path, func() error {
		current, exists, err := ReadWebConfig(path)
		if err != nil {
			return err
		}
		if exists {
			cfg = current
			return nil
		}
		current.Revision = 1
		raw, err := json.MarshalIndent(current, "", "  ")
		if err != nil {
			return err
		}
		if err := gitstore.WriteJSON(path, append(raw, '\n')); err != nil {
			return err
		}
		cfg, created = current, true
		return nil
	})
	return cfg, created, err
}

// ReadWebState loads web-state.json, returning an empty state when missing.
func ReadWebState(path string) (WebState, error) {
	out := WebState{Version: WebStateVersion}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return WebState{}, fmt.Errorf("read web state %s: %w", path, err)
	}
	if out.Version != WebStateVersion {
		return WebState{}, fmt.Errorf("unsupported web state version %d in %s", out.Version, path)
	}
	return out, nil
}

// UpdateWebState mutates web-state.json under its own lock.
func UpdateWebState(path string, fn func(*WebState) error) (WebState, error) {
	var out WebState
	err := withWebLock(path, func() error {
		state, err := ReadWebState(path)
		if err != nil {
			return err
		}
		if err := fn(&state); err != nil {
			return err
		}
		state.Version = WebStateVersion
		state.Revision++
		raw, err := json.MarshalIndent(state, "", "  ")
		if err != nil {
			return err
		}
		if err := gitstore.WriteJSON(path, append(raw, '\n')); err != nil {
			return err
		}
		out = state
		return nil
	})
	return out, err
}

func withWebLock(path string, fn func() error) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("web settings path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	lock, err := procstore.Lock(path + ".lock")
	if err != nil {
		return err
	}
	defer lock.Unlock()
	return fn()
}
