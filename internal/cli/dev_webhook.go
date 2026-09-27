package cli

import (
	"bytes"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	git "github.com/Tiago-0liveira/bonsai/internal/git/local"
	"github.com/Tiago-0liveira/bonsai/internal/server/webhooks"
)

var devWebhookFixtures = map[string]string{
	"push":                     "push",
	"pull_request_opened":      "pull_request",
	"pull_request_synchronize": "pull_request",
	"check_run_completed":      "check_run",
}

func cmdDevWebhook(repoDir string, args []string, out, errOut io.Writer) error {
	if len(args) == 0 || args[0] != "send" {
		return fmt.Errorf("usage: bonsai __dev-webhook send [--webhook-port PORT] <fixture>")
	}
	fs := flag.NewFlagSet("__dev-webhook send", flag.ContinueOnError)
	fs.SetOutput(errOut)
	portFlag := fs.Int("webhook-port", 0, "development webhook port")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: bonsai __dev-webhook send [--webhook-port PORT] <fixture>")
	}
	name := fs.Arg(0)
	eventName, ok := devWebhookFixtures[name]
	if !ok {
		return fmt.Errorf("unknown webhook fixture %q", name)
	}
	port, err := resolveDevPort(*portFlag, "BONSAI_DEV_WEBHOOK_PORT", devDefaultWebhookPort)
	if err != nil {
		return err
	}
	workspace, err := git.RepoRoot(".")
	if err != nil {
		return err
	}
	workspace, err = filepath.Abs(workspace)
	if err != nil {
		return err
	}
	body, err := os.ReadFile(filepath.Join(workspace, "testdata", "webhooks", name+".json"))
	if err != nil {
		return fmt.Errorf("read webhook fixture: %w", err)
	}

	secretPath := filepath.Join(procstore.New(repoDir).Dir(), "serve", serveWorkspaceID(workspace)+".dev-webhook-secret")
	encoded, err := os.ReadFile(secretPath)
	if err != nil {
		return fmt.Errorf("development webhook secret unavailable; start bonsai __serve-dev-stack first")
	}
	secret, err := hex.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil || len(secret) == 0 {
		return fmt.Errorf("development webhook secret is invalid")
	}
	delivery := fmt.Sprintf("fixture-%s-%d", name, time.Now().UnixNano())
	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/github/webhook"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Delivery", delivery)
	req.Header.Set("X-GitHub-Event", eventName)
	req.Header.Set("X-Hub-Signature-256", webhooks.Sign(secret, body))
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("development webhook returned %s: %s", resp.Status, strings.TrimSpace(string(detail)))
	}
	fmt.Fprintf(out, "sent %s -> %s\n", name, resp.Status)
	return nil
}
