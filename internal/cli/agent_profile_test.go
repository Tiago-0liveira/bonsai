package cli

import (
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func storedSettings(t *testing.T, provider agents.ProviderID, name string) (agents.Account, map[string]any) {
	t.Helper()
	store, err := agents.NewFileAccountStore("")
	if err != nil {
		t.Fatal(err)
	}
	account, err := store.GetByName(provider, name)
	if err != nil {
		t.Fatal(err)
	}
	settings := map[string]any{}
	if len(account.Settings) > 0 {
		if err := json.Unmarshal(account.Settings, &settings); err != nil {
			t.Fatal(err)
		}
	}
	return account, settings
}

func TestCodexAccountSetShowAndRemoveThroughCLI(t *testing.T) {
	codexCLIEnv(t)
	if _, errOut, err := runAgent(t, cliTestAPIKey+"\n", "account", "add", "codex", "work", "--auth", "api-key", "--token-stdin", "--no-seed"); err != nil {
		t.Fatalf("add: %v (stderr=%q)", err, errOut)
	}

	out, errOut, err := runAgent(t, "", "account", "set", "work", "model=gpt-5.5", "effort=high", "permissions=auto")
	if err != nil || out != "updated codex account \"work\" defaults\n" || errOut != "" {
		t.Fatalf("set: %v stdout=%q stderr=%q", err, out, errOut)
	}
	_, settings := storedSettings(t, "codex", "work")
	want := map[string]any{"auth_mode": "api-key", "model": "gpt-5.5", "effort": "high", "sandbox": "workspace-write", "approval": "on-request"}
	if !reflect.DeepEqual(settings, want) {
		t.Fatalf("settings = %v", settings)
	}

	if _, _, err := runAgent(t, "", "account", "set", "work", "--unset", "effort", "--unset=permissions"); err != nil {
		t.Fatal(err)
	}
	_, settings = storedSettings(t, "codex", "work")
	if !reflect.DeepEqual(settings, map[string]any{"auth_mode": "api-key", "model": "gpt-5.5"}) {
		t.Fatalf("settings after unset = %v", settings)
	}

	out, _, err = runAgent(t, "", "account", "show", "codex/work")
	if err != nil {
		t.Fatal(err)
	}
	account, _ := storedSettings(t, "codex", "work")
	masked := regexp.MustCompile(`\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ`).ReplaceAllString(strings.ReplaceAll(out, string(account.ID), "<id>"), "<time>")
	const wantShow = `name:      work
provider:  codex
id:        <id>
auth:      api-key
identity:  API key
status:    logged_in
created:   <time>
updated:   <time>

Defaults (bonsai agent account set):
  FIELD        VALUE    VALUES
  model        gpt-5.5  any model name
  permissions  -        read-only, auto, full-access, bypass
  effort       -        none, minimal, low, medium, high, xhigh, max, ultra
`
	if masked != wantShow {
		t.Fatalf("show =\n%s\nwant\n%s", masked, wantShow)
	}
	if strings.Contains(out, cliTestAPIKey) || strings.Contains(out, "auth.json") {
		t.Fatalf("show leaks: %s", out)
	}

	for _, args := range [][]string{
		{"account", "set", "work"},
		{"account", "set", "work", "model="},
		{"account", "set", "work", "model"},
		{"account", "set", "work", "effort=extreme"},
		{"account", "set", "work", "auth_mode=chatgpt"},
		{"account", "set", "work", "sandbox=read-only"},
		{"account", "set", "work", "effort=low", "--unset", "effort"},
		{"account", "set", "work", "--unset"},
		{"account", "set", "work", "--bogus"},
		{"account", "set", "missing", "model=x"},
		{"account", "show"},
	} {
		if _, _, err := runAgent(t, "", args...); err == nil {
			t.Errorf("%v accepted", args)
		}
	}
	if _, settings := storedSettings(t, "codex", "work"); !reflect.DeepEqual(settings, map[string]any{"auth_mode": "api-key", "model": "gpt-5.5"}) {
		t.Fatalf("rejected sets changed settings: %v", settings)
	}

	// A session record (an agent still running, here faked) gets a warning; the
	// profile is still removed.
	sessions, err := agents.NewFileSessionStore("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Create(account, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	out, errOut, err = runAgent(t, "", "account", "remove", "work")
	if err != nil {
		t.Fatal(err)
	}
	if out != "removed codex account \"work\"\n" || !strings.HasPrefix(errOut, "warning: work still has 1 session record(s); a running agent loses its profile\n") {
		t.Fatalf("remove: stdout=%q stderr=%q", out, errOut)
	}
}

// Without session records, remove prints exactly what it always did.
func TestAccountRemoveWithoutSessionRecordsIsUnchanged(t *testing.T) {
	codexCLIEnv(t)
	if _, errOut, err := runAgent(t, cliTestAPIKey+"\n", "account", "add", "codex", "solo", "--auth", "api-key", "--token-stdin", "--no-seed"); err != nil {
		t.Fatalf("add: %v (stderr=%q)", err, errOut)
	}
	out, errOut, err := runAgent(t, "", "account", "remove", "solo")
	if err != nil || out != "removed codex account \"solo\"\n" || strings.Contains(errOut, "session record") {
		t.Fatalf("remove: %v stdout=%q stderr=%q", err, out, errOut)
	}
}
