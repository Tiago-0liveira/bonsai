package cli

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	git "github.com/Tiago-0liveira/bonsai/internal/git/local"
	"github.com/Tiago-0liveira/bonsai/internal/server/webhooks"
)

// devWebhookFixture is a built-in GitHub delivery the development stack can
// receive without a real GitHub App.
type devWebhookFixture struct {
	event string
	body  string
}

// Fixture bodies name their repository with {{repo}} and {{repo_id}}, so the
// product receiver (--web) can target a real local project by name.
var devWebhookFixtures = map[string]devWebhookFixture{
	"push": {"push", `{
  "ref": "refs/heads/main",
  "repository": {"id": {{repo_id}}, "full_name": "{{repo}}"},
  "installation": {"id": 456}
}`},
	"pull_request_opened": {"pull_request", `{
  "action": "opened",
  "number": 42,
  "pull_request": {"number": 42, "merged": false},
  "repository": {"id": {{repo_id}}, "full_name": "{{repo}}"},
  "installation": {"id": 456}
}`},
	"pull_request_synchronize": {"pull_request", `{
  "action": "synchronize",
  "number": 42,
  "pull_request": {"number": 42, "merged": false},
  "repository": {"id": {{repo_id}}, "full_name": "{{repo}}"},
  "installation": {"id": 456}
}`},
	"check_run_completed": {"check_run", `{
  "action": "completed",
  "check_run": {"head_sha": "0123456789abcdef0123456789abcdef01234567"},
  "repository": {"id": {{repo_id}}, "full_name": "{{repo}}"},
  "installation": {"id": 456}
}`},
	// status and ping are live-receiver only; the development relay's
	// frozen allowlist rejects them.
	"status_success": {"status", `{
  "sha": "0123456789abcdef0123456789abcdef01234567",
  "state": "success",
  "context": "ci",
  "repository": {"id": {{repo_id}}, "full_name": "{{repo}}"}
}`},
	"ping": {"ping", `{
  "zen": "Bonsai fixture.",
  "hook_id": 1,
  "repository": {"id": {{repo_id}}, "full_name": "{{repo}}"}
}`},
}

const (
	devWebhookUsage = "usage: bonsai __dev-webhook send [--webhook-port PORT] <fixture>\n" +
		"       bonsai __dev-webhook send --web [--repo OWNER/NAME] [--webhook-port PORT] <fixture>"
	devWebhookFixtureRepo   = "octo/fixture"
	devWebhookFixtureRepoID = "123"
)

// devWebhookBody fills a fixture's repository. A --repo target carries no ID,
// so the receiver matches it by name.
func devWebhookBody(fixture devWebhookFixture, repo string) ([]byte, error) {
	id := devWebhookFixtureRepoID
	if repo == "" {
		repo = devWebhookFixtureRepo
	} else {
		id = "0"
	}
	name, err := json.Marshal(repo)
	if err != nil {
		return nil, err
	}
	body := strings.ReplaceAll(fixture.body, `"{{repo}}"`, string(name))
	return []byte(strings.ReplaceAll(body, "{{repo_id}}", id)), nil
}

func cmdDevWebhook(repoDir string, args []string, out, errOut io.Writer) error {
	if len(args) == 0 || args[0] != "send" {
		return fmt.Errorf("%s", devWebhookUsage)
	}
	fs := flag.NewFlagSet("__dev-webhook send", flag.ContinueOnError)
	fs.SetOutput(errOut)
	portFlag := fs.Int("webhook-port", 0, "webhook receiver port")
	product := fs.Bool("web", false, "send to the bonsai web live-updates receiver instead of the development relay")
	repo := fs.String("repo", "", "repository the fixture names (owner/name; --web only)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 1 || (*repo != "" && !*product) {
		return fmt.Errorf("%s", devWebhookUsage)
	}
	name := fs.Arg(0)
	fixture, ok := devWebhookFixtures[name]
	if !ok {
		return fmt.Errorf("unknown webhook fixture %q", name)
	}
	body, err := devWebhookBody(fixture, *repo)
	if err != nil {
		return err
	}
	var port int
	var secret []byte
	if *product {
		port, secret, err = webReceiverTarget(*portFlag)
	} else {
		port, secret, err = devRelayTarget(repoDir, *portFlag)
	}
	if err != nil {
		return err
	}
	delivery := fmt.Sprintf("fixture-%s-%d", name, time.Now().UnixNano())
	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/github/webhook"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Delivery", delivery)
	req.Header.Set("X-GitHub-Event", fixture.event)
	req.Header.Set("X-Hub-Signature-256", webhooks.Sign(secret, body))
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("webhook receiver returned %s: %s", resp.Status, strings.TrimSpace(string(detail)))
	}
	fmt.Fprintf(out, "sent %s -> %s\n", name, resp.Status)
	return nil
}

// devRelayTarget is the development stack's relay port and decoded secret.
func devRelayTarget(repoDir string, portFlag int) (int, []byte, error) {
	if repoDir == "" {
		return 0, nil, fmt.Errorf("the development relay needs a repository; use --web for bonsai web")
	}
	port, err := resolveDevPort(portFlag, "BONSAI_DEV_WEBHOOK_PORT", devDefaultWebhookPort)
	if err != nil {
		return 0, nil, err
	}
	workspace, err := git.RepoRoot(".")
	if err != nil {
		return 0, nil, err
	}
	workspace, err = filepath.Abs(workspace)
	if err != nil {
		return 0, nil, err
	}
	secretPath := filepath.Join(procstore.New(repoDir).Dir(), "serve", serveWorkspaceID(workspace)+".dev-webhook-secret")
	encoded, err := os.ReadFile(secretPath)
	if err != nil {
		return 0, nil, fmt.Errorf("development webhook secret unavailable; start bonsai __serve-dev-stack first")
	}
	secret, err := hex.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil || len(secret) == 0 {
		return 0, nil, fmt.Errorf("development webhook secret is invalid")
	}
	return port, secret, nil
}

// webReceiverTarget is the bonsai web live-updates receiver: its port from
// web.json (unless given) and the product webhook secret, used as GitHub uses
// it.
func webReceiverTarget(portFlag int) (int, []byte, error) {
	port := portFlag
	if port == 0 {
		path, err := config.WebConfigPath()
		if err != nil {
			return 0, nil, err
		}
		cfg, _, err := config.ReadWebConfig(path)
		if err != nil {
			return 0, nil, err
		}
		port = cfg.Updates.Live.WebhookPort
	}
	if port < 1 || port > 65535 {
		return 0, nil, fmt.Errorf("--webhook-port must be between 1 and 65535")
	}
	path, err := config.WebWebhookSecretPath()
	if err != nil {
		return 0, nil, err
	}
	secret, err := config.ReadWebWebhookSecret(path)
	if err != nil {
		return 0, nil, fmt.Errorf("bonsai web webhook secret unavailable (turn on live updates and run bonsai web): %w", err)
	}
	return port, secret, nil
}
