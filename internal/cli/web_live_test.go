package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
)

// isolateUserConfig points every user config/state location at a temp dir.
func isolateUserConfig(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("APPDATA", filepath.Join(home, "appdata"))
	t.Setenv("LocalAppData", filepath.Join(home, "localappdata"))
}

func liveTestCLI(t *testing.T, installed bool) *webCLI {
	t.Helper()
	isolateUserConfig(t)
	configPath, err := config.WebConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	return &webCLI{
		out: &bytes.Buffer{}, errOut: &bytes.Buffer{}, configPath: configPath,
		lookPath: func(name string) (string, error) {
			if installed {
				return "/usr/bin/" + name, nil
			}
			return "", errors.New("executable file not found in $PATH")
		},
	}
}

func liveConfig(tunnel string) config.WebConfig {
	cfg := config.DefaultWebConfig()
	cfg.Updates.Mode = config.WebUpdatesLive
	cfg.Updates.Live.Tunnel = tunnel
	if tunnel == "external-url" {
		cfg.Updates.Live.PublicURL = "https://hooks.example.com"
	}
	return cfg
}

func TestLiveServeSpecParts(t *testing.T) {
	w := liveTestCLI(t, true)
	if live, failure := w.liveServe(config.DefaultWebConfig(), 7001); failure != nil || live.webhookPort != 0 || live.sidecars != nil {
		t.Fatalf("standard updates produced live parts: %+v %+v", live, failure)
	}
	secretPath, _ := config.WebWebhookSecretPath()
	if _, err := os.Stat(secretPath); !os.IsNotExist(err) {
		t.Fatal("standard updates created a webhook secret")
	}

	live, failure := w.liveServe(liveConfig("cloudflared-quick"), 7001)
	if failure != nil {
		t.Fatalf("quick tunnel: %+v", failure)
	}
	want := []string{"cloudflared", "tunnel", "--no-autoupdate", "--url", "http://127.0.0.1:7002"}
	if live.webhookPort != 7002 || len(live.sidecars) != 1 || live.sidecars[0].Name != "tunnel" || !slices.Equal(live.tunnel(), want) {
		t.Fatalf("quick tunnel parts = %+v", live)
	}
	secret, err := config.ReadWebWebhookSecret(secretPath)
	if err != nil {
		t.Fatalf("live start did not create the secret: %v", err)
	}
	// The secret only ever lives in its file: never in the spec the daemon
	// stores and puts in process argv.
	spec, _ := json.Marshal(procstore.ServeSpec{WebhookPort: live.webhookPort, Sidecars: live.sidecars})
	if strings.Contains(string(spec), string(secret)) {
		t.Fatal("webhook secret in the serve spec")
	}

	external, failure := w.liveServe(liveConfig("external-url"), 7001)
	if failure != nil || external.webhookPort != 7002 || external.sidecars != nil {
		t.Fatalf("external-url parts = %+v %+v", external, failure)
	}
	if _, failure := w.liveServe(liveConfig("cloudflared-quick"), 7002); failure == nil || !strings.Contains(failure.detail, "both the API port") {
		t.Fatalf("api port equal to webhook port: %+v", failure)
	}

	missing := liveTestCLI(t, false)
	live, failure = missing.liveServe(liveConfig("cloudflared-quick"), 7001)
	if failure != nil || live.webhookPort != 7002 || live.sidecars != nil || !strings.Contains(live.warning, "cloudflared is not installed") {
		t.Fatalf("missing tool: %+v %+v", live, failure)
	}
	var out bytes.Buffer
	live.warn(&out)
	if !strings.Contains(out.String(), "fix") {
		t.Fatalf("warning has no fix:\n%s", out.String())
	}
}

func TestLiveUpdatesTextAndOldDaemon(t *testing.T) {
	group := &procstore.ServeGroup{WebhookPort: 7002, Processes: []procstore.ServeProcess{{Name: "api", State: "ready"}, {Name: "tunnel", State: "running"}}}
	none := config.WebLiveState{}
	if got := webUpdatesText(liveConfig("cloudflared-quick"), group, none); got != "live · cloudflared-quick · no repos yet · tunnel running · receiver 127.0.0.1:7002" {
		t.Fatalf("live text = %q", got)
	}
	withRepos := liveConfig("cloudflared-quick")
	withRepos.Updates.Live.Repositories = []string{"acme/one", "acme/two"}
	state := config.WebLiveState{Repositories: map[string]config.WebLiveRepository{"ACME/one": {State: config.WebLiveStateLive}, "acme/two": {State: config.WebLiveStateNeedsAdmin}}}
	if got := webUpdatesText(withRepos, group, state); got != "live · cloudflared-quick · 2 repos (1 live) · tunnel running · receiver 127.0.0.1:7002" {
		t.Fatalf("live text with repos = %q", got)
	}
	group.Processes = group.Processes[:1]
	if got := webUpdatesText(liveConfig("cloudflared-quick"), group, none); !strings.Contains(got, "tunnel not running") {
		t.Fatalf("live text without tunnel = %q", got)
	}
	if got := webUpdatesText(liveConfig("external-url"), group, none); got != "live · external-url · no repos yet · receiver 127.0.0.1:7002" {
		t.Fatalf("external text = %q", got)
	}
	if got := webUpdatesText(config.DefaultWebConfig(), group, none); !strings.HasPrefix(got, "standard (every ~") {
		t.Fatalf("standard text = %q", got)
	}
	if !oldDaemonRefusesLive(errors.New("production serve cannot supervise development services")) || oldDaemonRefusesLive(errors.New("port 7002 is already in use")) {
		t.Fatal("old daemon refusal not recognised")
	}
}

func TestDevWebhookFixturesNameTheRepository(t *testing.T) {
	for name, fixture := range devWebhookFixtures {
		for _, repo := range []string{"", "Acme/Repo"} {
			body, err := devWebhookBody(fixture, repo)
			if err != nil {
				t.Fatal(err)
			}
			var parsed struct {
				Repository struct {
					ID       int64  `json:"id"`
					FullName string `json:"full_name"`
				} `json:"repository"`
			}
			if err := json.Unmarshal(body, &parsed); err != nil {
				t.Fatalf("%s: %v\n%s", name, err, body)
			}
			wantName, wantID := "octo/fixture", int64(123)
			if repo != "" {
				wantName, wantID = repo, 0
			}
			if parsed.Repository.FullName != wantName || parsed.Repository.ID != wantID {
				t.Fatalf("%s repo %q: %+v", name, repo, parsed.Repository)
			}
		}
	}
}

func TestLiveStatusLines(t *testing.T) {
	if got := livePublicText(config.WebLiveState{PublicURL: "https://a.trycloudflare.com"}); got != "https://a.trycloudflare.com" {
		t.Fatal(got)
	}
	if got := livePublicText(config.WebLiveState{PublicURL: "https://old.trycloudflare.com", TunnelError: "the tunnel is failed"}); got != "unknown: the tunnel is failed" {
		t.Fatal(got)
	}
	if got := livePublicText(config.WebLiveState{}); got != "waiting for the tunnel" {
		t.Fatal(got)
	}

	var out bytes.Buffer
	w := &webCLI{out: &out}
	cfg := liveConfig("cloudflared-quick")
	if err := w.printLiveRepositories(cfg, config.WebLiveState{}); err != nil || !strings.Contains(out.String(), "bonsai web setup") {
		t.Fatalf("%v\n%s", err, out.String())
	}
	out.Reset()
	cfg.Updates.Live.Repositories = []string{"acme/one", "acme/two", "acme/new"}
	now := time.Now()
	state := config.WebLiveState{Repositories: map[string]config.WebLiveRepository{
		"acme/one": {State: config.WebLiveStateLive, LastPingAt: now.Add(-time.Hour), LastDeliveryAt: now.Add(-3 * time.Minute)},
		"acme/two": {State: config.WebLiveStateScopeMissing, LastError: "your gh login cannot manage webhooks; run: gh auth refresh -h github.com -s admin:repo_hook"},
	}}
	if err := w.printLiveRepositories(cfg, state); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"REPOSITORY", "acme/one    live", "delivery 3 min ago", "scope_missing  none yet", "run: gh auth refresh -h github.com -s admin:repo_hook", "acme/new    pending"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("status lacks %q:\n%s", want, out.String())
		}
	}
}
