package cli

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
)

const fakeTunnelURL = "https://fake-live-test.trycloudflare.com"

// TestFakeTunnelHelper is the tunnel program of the live integration test: it
// prints a public URL like a quick tunnel and stays up until it is stopped.
func TestFakeTunnelHelper(t *testing.T) {
	if os.Getenv("BONSAI_FAKE_TUNNEL") != "1" {
		t.Skip("helper process for TestWebLiveStartsReceiverAndTunnel")
	}
	fmt.Printf("fake tunnel forwarding %s\n", os.Args[len(os.Args)-1])
	fmt.Printf("|  %s  |\n", fakeTunnelURL)
	time.Sleep(10 * time.Minute)
	os.Exit(0)
}

func setLiveMode(t *testing.T, live bool, tunnel []string, webhookPort int) {
	t.Helper()
	path, err := config.WebConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := config.EnsureWebConfig(path); err != nil {
		t.Fatal(err)
	}
	if _, err := config.UpdateWebConfig(path, ^uint64(0), func(c *config.WebConfig) error {
		c.SetupVersion = config.WebSetupVersion
		c.OpenBrowser = false
		c.Updates.Mode = config.WebUpdatesStandard
		if live {
			c.Updates.Mode = config.WebUpdatesLive
		}
		c.Updates.Live.Tunnel = "custom"
		c.Updates.Live.WebhookPort = webhookPort
		c.Updates.Live.Command = tunnel
		c.Updates.Live.URLPattern = `https://\S+\.trycloudflare\.com`
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestWebLiveStartsReceiverAndTunnel(t *testing.T) {
	e := newWebEnv(t)
	t.Setenv("BONSAI_FAKE_TUNNEL", "1") // inherited by the daemon and its tunnel
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	apiPort, hookPort := freePort(t), freePort(t)
	for hookPort == apiPort {
		hookPort = freePort(t)
	}
	api, hook := strconv.Itoa(apiPort), strconv.Itoa(hookPort)
	tunnel := []string{self, "-test.run=^TestFakeTunnelHelper$", "--", "http://127.0.0.1:{port}"}
	setLiveMode(t, true, tunnel, hookPort)

	out := e.mustRun(t, "web", "--no-open", "--port", api)
	if !strings.Contains(out, "updates   live · custom · tunnel") || !strings.Contains(out, "receiver 127.0.0.1:"+hook) {
		t.Fatalf("live start output:\n%s", out)
	}
	if !portListening(t, hook) {
		t.Fatal("webhook receiver is not listening")
	}
	status := e.mustRun(t, "web", "status")
	if !strings.Contains(status, "tunnel") || !strings.Contains(status, "127.0.0.1:"+api) {
		t.Fatalf("status:\n%s", status)
	}
	if !waitUntil(10*time.Second, func() bool {
		logs, _, _ := e.run(t, "web", "logs", "tunnel", "-n", "20")
		return strings.Contains(logs, fakeTunnelURL)
	}) {
		logs, errOut, _ := e.run(t, "web", "logs", "tunnel", "-n", "20")
		t.Fatalf("tunnel logs lack the public URL:\n%s\n%s", logs, errOut)
	}

	// Only the webhook route exists on the receiver port.
	for _, path := range []string{"/", "/healthz", "/version", "/api/projects", "/app"} {
		if code, _ := httpGet(t, "http://127.0.0.1:"+hook+path); code != http.StatusNotFound {
			t.Fatalf("receiver GET %s = %d, want 404", path, code)
		}
	}
	if sent := e.mustRun(t, "__dev-webhook", "send", "--web", "--repo", "acme/live", "push"); !strings.Contains(sent, "202") {
		t.Fatalf("dev webhook send:\n%s", sent)
	}
	if sent := e.mustRun(t, "__dev-webhook", "send", "--web", "ping"); !strings.Contains(sent, "202") {
		t.Fatalf("dev webhook ping:\n%s", sent)
	}
	if !waitUntil(5*time.Second, func() bool {
		logs := e.mustRun(t, "web", "logs", "api", "-n", "50")
		return strings.Contains(logs, "live updates: push for acme/live") && strings.Contains(logs, "live updates: ping from octo/fixture")
	}) {
		t.Fatalf("API did not log the deliveries:\n%s", e.mustRun(t, "web", "logs", "api", "-n", "50"))
	}

	// The secret never reaches a process record (argv, environment) or a log.
	secretPath, err := config.WebWebhookSecretPath()
	if err != nil {
		t.Fatal(err)
	}
	secret, err := config.ReadWebWebhookSecret(secretPath)
	if err != nil {
		t.Fatal(err)
	}
	records, err := procstore.New(e.home).ListRecords()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range records {
		raw := r.Program + " " + strings.Join(r.Args, " ") + fmt.Sprint(r.Environment)
		if strings.Contains(raw, string(secret)) {
			t.Fatalf("secret in process record %s", r.Label)
		}
	}
	if logs := e.mustRun(t, "web", "logs", "-n", "500"); strings.Contains(logs, string(secret)) {
		t.Fatal("secret in serve logs")
	}

	if out := e.mustRun(t, "web", "restart", "tunnel"); !strings.Contains(out, "✓ restarted") || !strings.Contains(out, "tunnel") {
		t.Fatalf("restart tunnel:\n%s", out)
	}
	if again := e.mustRun(t, "web", "--no-open", "--port", api); !strings.Contains(again, "already running") {
		t.Fatalf("unchanged live settings did not reuse:\n%s", again)
	}

	// Back to standard: the stack restarts without the receiver and tunnel.
	setLiveMode(t, false, tunnel, hookPort)
	out = e.mustRun(t, "web", "--no-open", "--port", api)
	if !strings.Contains(out, "apply changed settings (live updates)") || !strings.Contains(out, "updates   standard") {
		t.Fatalf("standard restart output:\n%s", out)
	}
	if !waitUntil(5*time.Second, func() bool { return !portListening(t, hook) }) {
		t.Fatal("webhook receiver still listening after switching to standard")
	}
	if _, errOut, code := e.run(t, "web", "logs", "tunnel"); code == 0 || !strings.Contains(errOut, "live updates are off") {
		t.Fatalf("tunnel logs in standard mode: code %d %s", code, errOut)
	}
	e.mustRun(t, "web", "stop")
	if !waitUntil(5*time.Second, func() bool { return webRecordsSettled(t, e.home) }) {
		t.Fatal("processes survived bonsai web stop")
	}
}
