package webtunnel

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestArgvPerPreset(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts Options
		want []string
	}{
		{"quick", Options{Preset: CloudflaredQuick, WebhookPort: 7002},
			[]string{"cloudflared", "tunnel", "--no-autoupdate", "--url", "http://127.0.0.1:7002"}},
		{"named", Options{Preset: CloudflaredNamed, WebhookPort: 7002, TunnelName: "bonsai", PublicURL: "https://hooks.example.com"},
			[]string{"cloudflared", "tunnel", "--no-autoupdate", "run", "--url", "http://127.0.0.1:7002", "bonsai"}},
		{"ngrok", Options{Preset: Ngrok, WebhookPort: 7002},
			[]string{"ngrok", "http", "127.0.0.1:7002", "--log", "stdout"}},
		{"ngrok domain", Options{Preset: Ngrok, WebhookPort: 7002, PublicURL: "calm.ngrok-free.app"},
			[]string{"ngrok", "http", "127.0.0.1:7002", "--log", "stdout", "--url", "https://calm.ngrok-free.app"}},
		{"tailscale", Options{Preset: Tailscale, WebhookPort: 7002},
			[]string{"tailscale", "funnel", "7002"}},
		{"external", Options{Preset: ExternalURL, WebhookPort: 7002, PublicURL: "https://proxy.example.com"}, nil},
		{"custom", Options{Preset: Custom, WebhookPort: 7002, Command: []string{"relay", "--to", "http://127.0.0.1:{port}"}, URLPattern: `https://\S+`},
			[]string{"relay", "--to", "http://127.0.0.1:7002"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Argv(tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("Argv = %q, want %q", got, tc.want)
			}
			if got != nil {
				if err := ValidateArgv(got, tc.opts.WebhookPort, 7001); err != nil {
					t.Fatalf("preset argv fails its own validation: %v", err)
				}
			}
		})
	}
}

func TestValidateOptionsRejects(t *testing.T) {
	for name, opts := range map[string]Options{
		"unknown":             {Preset: "frp", WebhookPort: 7002},
		"bad port":            {Preset: CloudflaredQuick, WebhookPort: 0},
		"named without name":  {Preset: CloudflaredNamed, WebhookPort: 7002, PublicURL: "https://a.example.com"},
		"named with flag":     {Preset: CloudflaredNamed, WebhookPort: 7002, TunnelName: "--config", PublicURL: "https://a.example.com"},
		"named without url":   {Preset: CloudflaredNamed, WebhookPort: 7002, TunnelName: "bonsai"},
		"external http":       {Preset: ExternalURL, WebhookPort: 7002, PublicURL: "http://a.example.com"},
		"external with path":  {Preset: ExternalURL, WebhookPort: 7002, PublicURL: "https://a.example.com/hooks"},
		"external with creds": {Preset: ExternalURL, WebhookPort: 7002, PublicURL: "https://u:p@a.example.com"},
		"custom no command":   {Preset: Custom, WebhookPort: 7002, URLPattern: "https://.*"},
		"custom no port":      {Preset: Custom, WebhookPort: 7002, Command: []string{"relay"}, URLPattern: "https://.*"},
		"custom no pattern":   {Preset: Custom, WebhookPort: 7002, Command: []string{"relay", "{port}"}},
		"custom bad pattern":  {Preset: Custom, WebhookPort: 7002, Command: []string{"relay", "{port}"}, URLPattern: "("},
	} {
		if err := ValidateOptions(opts); err == nil {
			t.Errorf("%s: accepted %+v", name, opts)
		}
	}
}

func TestValidateArgv(t *testing.T) {
	ok := [][]string{
		{"cloudflared", "tunnel", "--url", "http://127.0.0.1:7002"},
		{"tailscale", "funnel", "7002"},
		{"relay", "--to=localhost:7002", "--retries", "3"},
		{"relay", "http://[::1]:7002"},
	}
	for _, argv := range ok {
		if err := ValidateArgv(argv, 7002, 7001); err != nil {
			t.Errorf("%q rejected: %v", argv, err)
		}
	}
	bad := map[string][]string{
		"empty":               {},
		"blank program":       {" ", "7002"},
		"api host:port":       {"cloudflared", "tunnel", "--url", "http://127.0.0.1:7002", "--metrics", "127.0.0.1:7001"},
		"api bare colon":      {"cloudflared", "tunnel", "--url", "http://127.0.0.1:7002", "--metrics", ":7001"},
		"api in url":          {"relay", "http://127.0.0.1:7001/", "7002"},
		"api bare number":     {"tailscale", "funnel", "7001", "7002"},
		"no webhook port":     {"cloudflared", "tunnel", "--url", "http://127.0.0.1:8080"},
		"only longer number":  {"relay", "--id", "170021"},
		"project service":     {"relay", "--to", "http://127.0.0.1:7002", "--also", "localhost:3000"},
		"api glued in flag":   {"relay", "--to=127.0.0.1:7002", "--admin=7001"},
		"api inside equation": {"relay", "7002", "x=7001"},
	}
	for name, argv := range bad {
		if err := ValidateArgv(argv, 7002, 7001); err == nil {
			t.Errorf("%s: %q accepted", name, argv)
		}
	}
	if err := ValidateArgv([]string{"relay", "7002"}, 7002, 7002); err == nil {
		t.Error("equal webhook and api ports accepted")
	}
	// A longer number containing the API port's digits is not the API port.
	if err := ValidateArgv([]string{"relay", "--id", "70011", "http://127.0.0.1:7002"}, 7002, 7001); err != nil {
		t.Errorf("digit boundary not respected: %v", err)
	}
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestURLFromLogFixtures(t *testing.T) {
	for _, tc := range []struct {
		preset, pattern, fixture, want string
	}{
		// A restarted quick tunnel prints a new URL; the last one wins.
		{CloudflaredQuick, "", "cloudflared-quick.log", "https://second-bright-example-words.trycloudflare.com"},
		{Ngrok, "", "ngrok.log", "https://calm-example-domain.ngrok-free.app"},
		{Custom, `public endpoint (https://\S+)`, "custom.log", "https://hooks.example.dev"},
	} {
		got, ok := URLFromLog(tc.preset, tc.pattern, readFixture(t, tc.fixture))
		if !ok || got != tc.want {
			t.Errorf("%s: URLFromLog = %q, %v; want %q", tc.preset, got, ok, tc.want)
		}
	}
	if _, ok := URLFromLog(CloudflaredQuick, "", "INF Requesting new quick Tunnel on trycloudflare.com..."); ok {
		t.Error("quick tunnel URL found before one was printed")
	}
	// The custom pattern must still yield an https origin.
	if _, ok := URLFromLog(Custom, `relay for (\S+)`, readFixture(t, "custom.log")); ok {
		t.Error("custom pattern matching the local http target was accepted")
	}
	if _, ok := URLFromLog(Tailscale, "", "anything"); ok {
		t.Error("tailscale URL is not learned from the log")
	}
}

func TestParseNgrokAndTailscale(t *testing.T) {
	got, err := ParseNgrokTunnels([]byte(readFixture(t, "ngrok-tunnels.json")), 7002)
	if err != nil || got != "https://calm-example-domain.ngrok-free.app" {
		t.Fatalf("ngrok = %q, %v", got, err)
	}
	if _, err := ParseNgrokTunnels([]byte(readFixture(t, "ngrok-tunnels.json")), 7009); err == nil {
		t.Fatal("ngrok tunnel to another port accepted")
	}
	got, err = ParseTailscaleStatus([]byte(readFixture(t, "tailscale-status.json")))
	if err != nil || got != "https://this-laptop.tail0000.ts.net" {
		t.Fatalf("tailscale = %q, %v", got, err)
	}
	if _, err := ParseTailscaleStatus([]byte(`{"Self":{}}`)); err == nil || !strings.Contains(err.Error(), "MagicDNS") {
		t.Fatalf("missing DNS name: %v", err)
	}
}

func TestConfiguredURL(t *testing.T) {
	if got, ok := ConfiguredURL(Options{Preset: ExternalURL, PublicURL: "https://Proxy.Example.com/"}); !ok || got != "https://proxy.example.com" {
		t.Fatalf("external = %q, %v", got, ok)
	}
	if _, ok := ConfiguredURL(Options{Preset: CloudflaredQuick}); ok {
		t.Fatal("quick tunnel has no configured URL")
	}
}
