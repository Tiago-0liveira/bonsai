package antigravity

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func readModelsFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "models.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// seedModels caches the recorded `agy models` list for the account, as if the
// launch dialog had listed it.
func seedModels(t *testing.T, p *Provider, id agents.AccountID) {
	t.Helper()
	p.modelsMu.Lock()
	defer p.modelsMu.Unlock()
	if p.modelsCache == nil {
		p.modelsCache = map[agents.AccountID]modelsEntry{}
	}
	p.modelsCache[id] = modelsEntry{models: ParseModels(readModelsFixture(t)), at: time.Now(), ok: true}
}

func describeModels(models []agents.ModelOption) []string {
	out := []string{}
	for _, m := range models {
		line := m.ID + " | " + m.Label
		if len(m.Efforts) > 0 {
			line += " | " + strings.Join(m.Efforts, ",") + " default=" + m.DefaultEffort
		}
		if m.Source != "bundled" || m.NoEffort == (len(m.Efforts) > 0) {
			line += " | unexpected source or no_effort"
		}
		out = append(out, line)
	}
	return out
}

func TestParseModelsRecordedOutput(t *testing.T) {
	got := describeModels(ParseModels(readModelsFixture(t)))
	want := []string{
		"gemini-3.8-flash | Gemini 3.8 Flash | low,medium,high default=high",
		"claude-sonnet-4-6 | Claude Sonnet 4.6 (Thinking)",
		"claude-opus-4-6-thinking | Claude Opus 4.6 (Thinking)",
		"gpt-oss-120b-medium | GPT-OSS 120B (Medium)",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("models =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	crlf := strings.ReplaceAll(string(readModelsFixture(t)), "\n", "\r\n")
	if got := describeModels(ParseModels([]byte("\n\n" + crlf + "\n\n"))); !reflect.DeepEqual(got, want) {
		t.Fatalf("CRLF and blank lines = %v", got)
	}
}

func TestParseModelsEdgeCases(t *testing.T) {
	for name, tc := range map[string]struct {
		in   string
		want []string
	}{
		"empty":       {"", []string{}},
		"header only": {"Fetching available models...\n", []string{}},
		"two groups and ordering": {
			"a-max\tA (Max)\na-low\tA (Low)\nb-xhigh\tB (XHigh)\nb-turbo\tB (Turbo)\nb-medium\tB (Medium)\n",
			[]string{"a | A | low,max default=max", "b | B | medium,xhigh,turbo default=xhigh"},
		},
		"same id prefix, different labels": {"m-low\tM (Low)\nm-high\tOther (High)\n", []string{"m-low | M (Low)", "m-high | Other (High)"}},
		"word mismatch":                    {"m-low\tM (Fast)\nm-high\tM (High)\n", []string{"m-low | M (Fast)", "m-high | M (High)"}},
		"duplicates and bad ids":           {"x\tX\nx\tX again\n-evil\tEvil\n\tNo id\n", []string{"x | X"}},
		"missing label":                    {"y\t\n", []string{"y | y"}},
	} {
		if got := describeModels(ParseModels([]byte(tc.in))); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
}

func TestComposeModel(t *testing.T) {
	models := ParseModels(readModelsFixture(t))
	for _, tc := range []struct{ model, effort, want string }{
		{"", "high", ""},
		{"gemini-3.8-flash", "", "gemini-3.8-flash-high"},
		{"gemini-3.8-flash", "low", "gemini-3.8-flash-low"},
		{"gemini-3.8-flash-medium", "", "gemini-3.8-flash-medium"},
		{"gemini-3.8-flash-medium", "low", "gemini-3.8-flash-low"},
		{"claude-sonnet-4-6", "", "claude-sonnet-4-6"},
		{"claude-sonnet-4-6", "high", "claude-sonnet-4-6"},
		{"gpt-oss-120b-medium", "low", "gpt-oss-120b-medium"},
		{"gpt-oss-120b-medium", "", "gpt-oss-120b-medium"},
		{"unknown", "", "unknown"},
		{"unknown", "high", "unknown-high"},
	} {
		if got := composeModel(tc.model, tc.effort, models); got != tc.want {
			t.Errorf("composeModel(%q, %q) = %q, want %q", tc.model, tc.effort, got, tc.want)
		}
	}
	// Without a list nothing is guessed.
	if got := composeModel("gemini-3.8-flash", "", nil); got != "gemini-3.8-flash" {
		t.Fatalf("no list = %q", got)
	}
}

type memorySessions struct{ cleaned atomic.Int32 }

func (s *memorySessions) Create(account agents.Account, workDir string) (agents.Session, error) {
	return agents.Session{ID: "sess_models", AccountID: account.ID, WorkDir: workDir}, nil
}
func (s *memorySessions) Get(agents.SessionID) (agents.Session, error) {
	return agents.Session{}, errors.New("not found")
}
func (s *memorySessions) Cleanup(agents.Session) error { s.cleaned.Add(1); return nil }

func TestModelsRunsAgyOnceAndCaches(t *testing.T) {
	sessions := &memorySessions{}
	credentials := &providerTestCredentialManager{}
	var calls atomic.Int32
	var gotArgs []string
	now := time.Now()
	p := &Provider{sessions: sessions, credentialMgr: credentials, binaryResolver: &providerTestBinaryResolver{path: "agy-test"}, now: func() time.Time { return now }}
	fail := false
	p.run = func(_ context.Context, exe, _ string, _ map[string]string, args ...string) ([]byte, error) {
		calls.Add(1)
		gotArgs = args
		if fail {
			return nil, errors.New("boom")
		}
		return readModelsFixture(t), nil
	}
	account := agents.Account{ID: "acct_m", Provider: ProviderID}
	models, err := p.Models(t.Context(), account)
	if err != nil || len(models) != 4 || models[0].ID != "gemini-3.8-flash" || !reflect.DeepEqual(gotArgs, []string{"models"}) {
		t.Fatalf("models=%v err=%v args=%v", models, err, gotArgs)
	}
	if credentials.materialize != 1 || credentials.reconcile != 1 || sessions.cleaned.Load() != 1 {
		t.Fatalf("materialize=%d reconcile=%d cleanup=%d", credentials.materialize, credentials.reconcile, sessions.cleaned.Load())
	}
	if _, _ = p.Models(t.Context(), account); calls.Load() != 1 {
		t.Fatalf("agy ran %d times within the TTL", calls.Load())
	}

	// After the TTL a failure is an empty list, remembered for a minute.
	now = now.Add(modelsTTL + time.Second)
	fail = true
	if models, err := p.Models(t.Context(), account); err != nil || len(models) != 0 {
		t.Fatalf("failure = %v, %v", models, err)
	}
	if _, _ = p.Models(t.Context(), account); calls.Load() != 2 {
		t.Fatalf("failure not cached: %d runs", calls.Load())
	}
	now = now.Add(modelsFailureTTL + time.Second)
	fail = false
	if models, _ := p.Models(t.Context(), account); len(models) != 4 || calls.Load() != 3 {
		t.Fatalf("retry = %v after %d runs", models, calls.Load())
	}
}

func TestPrepareSessionListsModelsWhenNoneAreCached(t *testing.T) {
	p := &Provider{sessions: &memorySessions{}, credentialMgr: &providerTestCredentialManager{}, binaryResolver: &providerTestBinaryResolver{path: "agy-test"}}
	p.run = func(context.Context, string, string, map[string]string, ...string) ([]byte, error) {
		return readModelsFixture(t), nil
	}
	account := agents.Account{ID: "acct_p", Provider: ProviderID, Settings: []byte(`{"model":"gemini-3.8-flash","effort":"low"}`)}
	prepared, err := p.PrepareSession(t.Context(), agents.PrepareSessionRequest{Account: account, Session: agents.Session{WorkDir: t.TempDir(), HomeDir: t.TempDir()}})
	if err != nil || !reflect.DeepEqual(prepared.Args, []string{"--model", "gemini-3.8-flash-low"}) {
		t.Fatalf("args = %v, %v", prepared.Args, err)
	}
}
