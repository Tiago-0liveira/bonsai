package antigravity

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
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

func TestParseCredits(t *testing.T) {
	for body, want := range map[string]float64{
		`{"remaining_credits":0,"upgrade_uri":"https://example.com/upgrade"}`:           0,
		`{"command":{"name":"credits","data":{"remaining_credits":250.5}}}`:             250.5,
		`{"conversation_id":"","command":{"data":{"remaining_credits":12}},"usage":{}}`: 12,
	} {
		if got, ok := ParseCredits([]byte(body)); !ok || got != want {
			t.Errorf("%s = %v, %v", body, got, ok)
		}
	}
	for _, body := range []string{``, `nope`, `{}`, `{"remaining_credits":"x"}`, `{"remaining_credits":-1}`} {
		if _, ok := ParseCredits([]byte(body)); ok {
			t.Errorf("%q parsed", body)
		}
	}
}

func TestFindPlan(t *testing.T) {
	lines := []string{"", "  Me@Example.com (Google AI Pro)   ", "other@example.com (Free)"}
	if plan, ok := findPlan(lines, "me@example.com"); !ok || plan != "Google AI Pro" {
		t.Fatalf("plan = %q, %v", plan, ok)
	}
	// Only the signed-in email counts, matched literally.
	for _, email := range []string{"", "nobody@example.com", "me@example.co"} {
		if plan, ok := findPlan([]string{"me@exampleXcom (Pro)", "other@example.com (Free)"}, email); ok {
			t.Fatalf("%q matched %q", email, plan)
		}
	}
	if plan, _ := findPlan([]string{"me@example.com (An extremely long plan name that keeps going on and on)"}, "me@example.com"); len([]rune(plan)) > maxPlanLength {
		t.Fatalf("plan not truncated: %q", plan)
	}
}

func TestNeedsProbe(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	for name, tc := range map[string]struct {
		state planState
		email string
		want  bool
	}{
		"no email":            {planState{}, "", false},
		"never probed":        {planState{}, "me@x.com", true},
		"fresh plan":          {planState{Plan: "Pro", Email: "me@x.com", CheckedAt: now.Add(-time.Hour)}, "me@x.com", false},
		"old plan":            {planState{Plan: "Pro", Email: "me@x.com", CheckedAt: now.Add(-25 * time.Hour)}, "me@x.com", true},
		"email changed":       {planState{Plan: "Pro", Email: "old@x.com", CheckedAt: now}, "me@x.com", true},
		"recent failure":      {planState{Email: "me@x.com", FailedAt: now.Add(-time.Minute)}, "me@x.com", false},
		"old failure":         {planState{Email: "me@x.com", FailedAt: now.Add(-16 * time.Minute)}, "me@x.com", true},
		"old plan, re-failed": {planState{Plan: "Pro", Email: "me@x.com", CheckedAt: now.Add(-48 * time.Hour), FailedAt: now.Add(-time.Minute)}, "me@x.com", false},
	} {
		if got := needsProbe(tc.state, tc.email, now); got != tc.want {
			t.Errorf("%s = %v", name, got)
		}
	}
}

// accountEnv is a provider over real stores with a fake agy runner and probe.
type accountEnv struct {
	provider *Provider
	accounts *agents.FileAccountStore
	account  agents.Account
	runs     atomic.Int32
	probes   atomic.Int32
	mu       sync.Mutex
	plan     string
	probeErr error
	gate     chan struct{} // when set, a probe waits for it
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
	e := &accountEnv{accounts: accounts, plan: "Google AI Pro"}
	e.account = agents.Account{ID: "acct_agy", Provider: ProviderID, Name: "personal", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := accounts.Create(e.account); err != nil {
		t.Fatal(err)
	}
	e.provider = New(accounts, sessions)
	e.provider.binaryResolver = &providerTestBinaryResolver{path: "agy-test"}
	e.provider.run = func(_ context.Context, _, _ string, _ map[string]string, args ...string) ([]byte, error) {
		e.runs.Add(1)
		switch args[2] {
		case "/usage":
			return os.ReadFile(filepath.Join("testdata", "usage.json"))
		case "/credits":
			return []byte(`{"remaining_credits":250,"upgrade_uri":"https://example.com"}`), nil
		}
		return nil, errors.New("unexpected command")
	}
	e.provider.probe = func(ctx context.Context, exe, dir string, env []string, email string) (string, error) {
		e.probes.Add(1)
		if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
			t.Errorf("probe dir %q is not empty: %v %v", dir, entries, err)
		}
		e.mu.Lock()
		gate, plan, err := e.gate, e.plan, e.probeErr
		e.mu.Unlock()
		if gate != nil {
			<-gate
		}
		return plan, err
	}
	t.Cleanup(e.provider.waitPlanProbes)
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
	if e.runs.Load() != 0 || e.probes.Load() != 0 {
		t.Fatalf("describe started agy: runs=%d probes=%d", e.runs.Load(), e.probes.Load())
	}
	// A plan stored for another email is not shown.
	e.signIn(t, "new@example.com")
	if info := e.provider.DescribeAccount(t.Context(), e.account); info.Plan != "" {
		t.Fatalf("stale plan = %+v", info)
	}
}

func TestUsageReadsCreditsAndProbesThePlanOnce(t *testing.T) {
	if !planProbeSupported {
		t.Skip("no plan probe on this platform")
	}
	e := newAccountEnv(t)
	e.signIn(t, "me@example.com")
	gate := make(chan struct{})
	e.mu.Lock()
	e.gate = gate
	e.mu.Unlock()

	snapshot, err := e.provider.Usage(t.Context(), e.account, agents.UsageOptions{})
	if err != nil || len(snapshot.Limits) != 4 || snapshot.Plan != "" {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	if len(snapshot.Credits) != 1 || *snapshot.Credits[0].Balance != 250 || snapshot.Credits[0].Unit != agents.CreditUnitCredits {
		t.Fatalf("credits = %+v", snapshot.Credits)
	}
	// While a probe runs, more reads and refreshes do not start another.
	for _, refresh := range []bool{false, true} {
		if _, err := e.provider.Usage(t.Context(), e.account, agents.UsageOptions{Refresh: refresh}); err != nil {
			t.Fatal(err)
		}
	}
	close(gate)
	e.provider.waitPlanProbes()
	if e.probes.Load() != 1 {
		t.Fatalf("probes = %d", e.probes.Load())
	}
	snapshot, _ = e.provider.Usage(t.Context(), e.account, agents.UsageOptions{})
	e.provider.waitPlanProbes()
	if snapshot.Plan != "Google AI Pro" || e.probes.Load() != 1 {
		t.Fatalf("plan=%q probes=%d", snapshot.Plan, e.probes.Load())
	}
	if info, err := os.Stat(planPath(e.accounts, e.account)); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("plan.json = %v, %v", info, err)
	}
	// An explicit refresh probes again.
	if _, err := e.provider.Usage(t.Context(), e.account, agents.UsageOptions{Refresh: true}); err != nil {
		t.Fatal(err)
	}
	e.provider.waitPlanProbes()
	if e.probes.Load() != 2 {
		t.Fatalf("refresh probes = %d", e.probes.Load())
	}
}

func TestFailedProbeLeavesThePlanBlankAndBacksOff(t *testing.T) {
	if !planProbeSupported {
		t.Skip("no plan probe on this platform")
	}
	e := newAccountEnv(t)
	e.signIn(t, "me@example.com")
	e.probeErr = errors.New("plan header not found")
	e.plan = ""
	for range 3 {
		snapshot, err := e.provider.Usage(t.Context(), e.account, agents.UsageOptions{})
		e.provider.waitPlanProbes()
		if err != nil || snapshot.Plan != "" {
			t.Fatalf("snapshot=%+v err=%v", snapshot, err)
		}
	}
	if e.probes.Load() != 1 {
		t.Fatalf("probes = %d, want one then a 15 minute backoff", e.probes.Load())
	}
	if state := e.provider.readPlanState(e.account); state.FailedAt.IsZero() || state.Plan != "" || state.Email != "me@example.com" {
		t.Fatalf("state = %+v", state)
	}
	if info := e.provider.DescribeAccount(t.Context(), e.account); info.Plan != "" || len(info.Warnings) != 0 {
		t.Fatalf("info = %+v", info)
	}
}
