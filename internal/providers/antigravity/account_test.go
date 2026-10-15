package antigravity

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func TestParseUsageRecordedOutput(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "usage.json"))
	if err != nil {
		t.Fatal(err)
	}
	limits, err := ParseUsage(data)
	if err != nil || len(limits) != 4 {
		t.Fatalf("limits=%+v err=%v", limits, err)
	}
	byID := map[string]agents.UsageLimit{}
	for _, l := range limits {
		byID[l.ID] = l
	}
	week := byID["gemini-weekly"]
	if week.Window != "weekly" || week.ResetsAt == nil || week.ResetsAt.Format(time.RFC3339) != "2026-10-19T00:05:35Z" || *week.RemainingFraction < 0.99 {
		t.Fatalf("gemini weekly = %+v", week)
	}
	if five := byID["3p-5h"]; five.Window != "5h" || five.Group != "Claude and GPT models" || *five.RemainingFraction != 1 {
		t.Fatalf("3p 5h = %+v", five)
	}
}

// accountEnv is a provider over real stores with a fake agy runner.
type accountEnv struct {
	provider  *Provider
	accounts  *agents.FileAccountStore
	account   agents.Account
	runs      atomic.Int32
	usageCmds atomic.Int32 // the /usage runs in runs
}

func newAccountEnv(t *testing.T) *accountEnv {
	t.Helper()
	accounts, err := agents.NewFileAccountStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := agents.NewFileSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// No test reaches the Cloud Code API unless it serves one.
	useCloudCodeHosts(t)
	e := &accountEnv{accounts: accounts}
	e.account = agents.Account{ID: "acct_agy", Provider: ProviderID, Name: "personal", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := accounts.Create(e.account); err != nil {
		t.Fatal(err)
	}
	e.provider = New(accounts, sessions)
	e.provider.binaryResolver = fixedBinaryResolver("agy-test")
	e.provider.run = func(_ context.Context, _, _ string, _ map[string]string, args ...string) ([]byte, error) {
		e.runs.Add(1)
		switch args[2] {
		case "/usage":
			e.usageCmds.Add(1)
			return os.ReadFile(filepath.Join("testdata", "usage.json"))
		}
		return nil, errors.New("unexpected command")
	}
	return e
}

// signIn stores a login, as setup would.
func (e *accountEnv) signIn(t *testing.T, email string) {
	t.Helper()
	oauth, _ := json.Marshal(map[string]any{"token": map[string]any{"email": email, "access_token": "a", "refresh_token": "r"}})
	identity := email
	if identity == "" {
		identity = fallbackIdentity(e.account)
	}
	vault := credentialVault{Version: 1, Identity: identity, Generation: 1, OAuth: oauth}
	if err := (&fileCredentialManager{accounts: e.accounts}).writeVault(e.account, vault); err != nil {
		t.Fatal(err)
	}
}

func TestDescribeReadsLoginAndPlanWithoutStartingAgy(t *testing.T) {
	e := newAccountEnv(t)
	info := e.provider.DescribeAccount(t.Context(), e.account)
	if info.Options["auth_status"] != "logged_out" || len(info.Warnings) != 1 || info.Identity != "" {
		t.Fatalf("no vault = %+v", info)
	}
	e.signIn(t, "")
	if info := e.provider.DescribeAccount(t.Context(), e.account); info.Options["auth_status"] != "logged_in" || info.Identity != "" {
		t.Fatalf("fallback identity = %+v", info)
	}
	e.signIn(t, "me@example.com")
	if err := writePrivateJSON(planPath(e.accounts, e.account), planState{Plan: "Google AI Pro", Email: "me@example.com", CheckedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		info = e.provider.DescribeAccount(t.Context(), e.account)
	}
	if info.Options["auth_status"] != "logged_in" || info.Identity != "me@example.com" || info.Plan != "Google AI Pro" || len(info.Warnings) != 0 {
		t.Fatalf("signed in = %+v", info)
	}
	if e.runs.Load() != 0 {
		t.Fatalf("describe started agy: runs=%d", e.runs.Load())
	}
	// A plan stored for another email is not shown.
	e.signIn(t, "new@example.com")
	if info := e.provider.DescribeAccount(t.Context(), e.account); info.Plan != "" {
		t.Fatalf("stale plan = %+v", info)
	}
}

// vaultSettings reads the vault's provider settings as a map.
func vaultSettings(t *testing.T, e *accountEnv) map[string]json.RawMessage {
	t.Helper()
	vault, err := (&fileCredentialManager{accounts: e.accounts}).readVault(e.account)
	if err != nil {
		t.Fatal(err)
	}
	settings := map[string]json.RawMessage{}
	if len(vault.ProviderSettings) > 0 {
		if err := json.Unmarshal(vault.ProviderSettings, &settings); err != nil {
			t.Fatal(err)
		}
	}
	return settings
}

func trustedIn(t *testing.T, settings map[string]json.RawMessage) []string {
	t.Helper()
	var trusted []string
	if raw, ok := settings["trustedWorkspaces"]; ok {
		if err := json.Unmarshal(raw, &trusted); err != nil {
			t.Fatal(err)
		}
	}
	return trusted
}

func TestTrustWorkspaceAddsTheResolvedPath(t *testing.T) {
	real := filepath.Join(t.TempDir(), "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("no symlinks:", err)
	}
	resolved, _ := filepath.EvalSymlinks(real)
	out, changed, err := trustWorkspace(json.RawMessage(`{"trustedWorkspaces":null}`), link)
	if err != nil || !changed {
		t.Fatalf("changed = %v, err = %v", changed, err)
	}
	var settings struct {
		TrustedWorkspaces []string `json:"trustedWorkspaces"`
	}
	if err := json.Unmarshal(out, &settings); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(settings.TrustedWorkspaces, []string{link, resolved}) {
		t.Fatalf("trustedWorkspaces = %v", settings.TrustedWorkspaces)
	}
	if _, changed, err := trustWorkspace(out, link); err != nil || changed {
		t.Fatalf("second add: changed = %v, err = %v", changed, err)
	}
	if _, _, err := trustWorkspace(json.RawMessage(`{"trustedWorkspaces":"x"}`), link); err == nil {
		t.Fatal("malformed trustedWorkspaces accepted")
	}
}

func envValue(env []string, key string) string {
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			return v
		}
	}
	return ""
}

// fixedBinaryResolver is safe for concurrent probes.
type fixedBinaryResolver string

func (r fixedBinaryResolver) Resolve() (string, error) { return string(r), nil }

type failingBinaryResolver struct{}

func (failingBinaryResolver) Resolve() (string, error) { return "", errors.New("agy not found") }
