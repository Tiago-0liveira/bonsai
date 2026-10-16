package accounts

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi/fake"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

func ptr(f float64) *float64 { return &f }

// mockupFake is the account set the UX mockups draw: two Claude accounts with
// two running sessions on work, two Codex accounts and one Antigravity.
func mockupFake() *fake.Accounts {
	fiveReset := testNow.Add(2*time.Hour + 14*time.Minute)
	weekReset := testNow.Add(3*24*time.Hour + 8*time.Hour)
	five := &agents.UsageLimit{ID: "five_hour", Window: "5h", RemainingFraction: ptr(0.72), ResetsAt: &fiveReset}
	week := &agents.UsageLimit{ID: "seven_day", Window: "weekly", RemainingFraction: ptr(0.41), ResetsAt: &weekReset}
	sonnet := agents.UsageLimit{ID: "sonnet_weekly", Label: "Sonnet weekly", RemainingFraction: ptr(0.82)}
	credit := 12.5
	claudeUsage := agentsapi.Usage{
		Snapshot: agents.UsageSnapshot{Credits: []agents.UsageCredit{
			{ID: "credits", Label: "Credits", Unit: agents.CreditUnitUSD, Enabled: true, Balance: &credit},
		}},
		Headline:  agents.NewUsageHeadline(five, week, []agents.UsageLimit{sonnet}),
		FetchedAt: testNow.Add(-35 * time.Second),
	}

	codexDefaults := agentsapi.Defaults{
		Fields: mockupFields(),
		Models: []agents.ModelOption{
			{ID: "gpt-5.5", Label: "gpt-5.5", Efforts: []string{"low", "medium", "high"}, DefaultEffort: "high"},
		},
		Values: map[string]string{"model": "gpt-5.5", "effort": "high"},
	}
	claudeDefaults := agentsapi.Defaults{
		Fields: mockupFields(),
		Models: []agents.ModelOption{
			{ID: "opus", Label: "Opus", Efforts: []string{"low", "medium", "high"}, DefaultEffort: "high"},
		},
		Values: map[string]string{"model": "opus", "effort": "high"},
	}

	api := &fake.Accounts{
		Items: []agentsapi.Account{
			{Account: agents.Account{ID: "c1", Provider: "claude", Name: "work"}, Running: 2,
				Info: agents.AccountInfo{Identity: "work@example.com", Plan: "Max", AuthMode: "login", Options: map[string]any{"auth_status": "logged_in"}}},
			{Account: agents.Account{ID: "c2", Provider: "claude", Name: "personal"},
				Info: agents.AccountInfo{Identity: "me@example.com", Plan: "Pro", AuthMode: "login", Options: map[string]any{"auth_status": "logged_in"}}},
			{Account: agents.Account{ID: "x1", Provider: "codex", Name: "work"},
				Info: agents.AccountInfo{Identity: "work@example.com", AuthMode: "chatgpt", Options: map[string]any{"auth_status": "logged_in"}}},
			{Account: agents.Account{ID: "x2", Provider: "codex", Name: "personal"},
				Info: agents.AccountInfo{Identity: "me@example.com", AuthMode: "chatgpt", Options: map[string]any{"auth_status": "logged_in"}}},
			{Account: agents.Account{ID: "a1", Provider: "antigravity", Name: "personal"},
				Info: agents.AccountInfo{Identity: "me@example.com", Options: map[string]any{"auth_status": "logged_in"}}},
		},
		Infos: map[agents.AccountID]agents.AccountInfo{
			"c1": {Identity: "work@example.com", Plan: "Max", AuthMode: "login", Options: map[string]any{"auth_status": "logged_in"}},
			"c2": {Identity: "me@example.com", Plan: "Pro", AuthMode: "login", Options: map[string]any{"auth_status": "logged_in"}},
			"x1": {Identity: "work@example.com", AuthMode: "chatgpt", Options: map[string]any{"auth_status": "logged_in"}},
			"x2": {Identity: "me@example.com", AuthMode: "chatgpt", Options: map[string]any{"auth_status": "logged_in"}},
			"a1": {Identity: "me@example.com", Options: map[string]any{"auth_status": "logged_in"}},
		},
		Usages: map[agents.AccountID]agentsapi.Usage{
			"c1": claudeUsage,
			"c2": claudeUsage,
			"x1": {Headline: agents.NewUsageHeadline(&agents.UsageLimit{ID: "5h", Window: "5h", RemainingFraction: ptr(0.91)}, nil, nil), FetchedAt: testNow},
			"x2": {Headline: agents.NewUsageHeadline(&agents.UsageLimit{ID: "5h", Window: "5h", RemainingFraction: ptr(0.15)}, nil, nil), FetchedAt: testNow},
			"a1": {Snapshot: agents.UsageSnapshot{Limits: []agents.UsageLimit{
				{ID: "pro", Group: "Gemini", Label: "Gemini Pro", RemainingFraction: ptr(0.12)},
				{ID: "flash", Group: "Gemini", Label: "Flash", RemainingFraction: ptr(0.8)},
			}}, FetchedAt: testNow},
		},
		LaunchDefaults: map[agents.AccountID]agentsapi.Defaults{
			"c1": claudeDefaults,
			"c2": claudeDefaults,
			"x1": codexDefaults,
			"x2": codexDefaults,
		},
	}
	return api
}

// mockupFields are the launch fields the Defaults mockup shows: model, reasoning
// effort and permissions, then a provider-specific option.
func mockupFields() []agents.LaunchField {
	return []agents.LaunchField{
		{ID: "model", Label: "Model", Kind: agents.LaunchFieldModel},
		{ID: "effort", Label: "Reasoning effort", Kind: agents.LaunchFieldSelect, PerModel: true, Options: []agents.FieldOption{
			{Value: "", Label: "Profile default"}, {Value: "low", Label: "Low"}, {Value: "medium", Label: "Medium"}, {Value: "high", Label: "High"},
		}},
		{ID: "permissions", Label: "Permissions", Kind: agents.LaunchFieldSelect, Default: "auto", Options: []agents.FieldOption{
			{Value: "read-only", Label: "Read only"},
			{Value: "auto", Label: "Auto"},
			{Value: "full-access", Label: "Full access", Tone: agents.ToneDanger, Help: "Can change any file without asking.",
				Set: map[string]string{"sandbox": "danger-full-access"}},
		}},
		{ID: "web_search", Label: "Web search", Kind: agents.LaunchFieldToggle, Options: []agents.FieldOption{
			{Value: "false", Label: "Off"}, {Value: "true", Label: "On"},
		}},
	}
}

func TestGoldenAccounts(t *testing.T) {
	cases := []struct {
		name    string
		initial agents.AccountID
		tab     Tab
	}{
		{"accounts-overview-100x30", "c1", TabOverview},
		{"accounts-usage-100x30", "c1", TabUsage},
		{"accounts-defaults-100x30", "x2", TabDefaults},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := New(mockupFake(), Options{Initial: c.initial, Tab: c.tab, Now: func() time.Time { return testNow }})
			m.SetSize(104, 34) // a 100x30 frame
			m, _ = drain(m, m.Init())
			checkGolden(t, c.name, plain(m))
		})
	}
}

// checkGolden compares got with testdata/<name>.golden, after trimming the
// trailing spaces of every line so the files stay diff-friendly.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	lines := strings.Split(got, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	got = strings.Join(lines, "\n") + "\n"
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s (run with -update): %v", path, err)
	}
	if string(want) != got {
		t.Errorf("%s differs from the golden file (run with -update to accept):\n--- got\n%s--- want\n%s", name, got, want)
	}
}
