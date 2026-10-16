package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/providers/antigravity"
	"github.com/Tiago-0liveira/bonsai/internal/providers/claude"
	"github.com/Tiago-0liveira/bonsai/internal/providers/codex"
)

// usageTestRegistry holds the real providers, whose UsageLayout fills the
// headlines the dashboard reads.
var usageTestRegistry = func() *agents.Registry {
	registry := agents.NewRegistry()
	for _, provider := range []agents.Provider{antigravity.New(nil, nil), claude.New(nil, nil), codex.New(nil, nil)} {
		if err := registry.Register(provider); err != nil {
			panic(err)
		}
	}
	return registry
}()

// renderUsageTestDashboard fills each result's headline as UsageService does,
// then renders the dashboard.
func renderUsageTestDashboard(out *bytes.Buffer, results []agents.AccountUsageResult, now time.Time, color bool) {
	renderUsageTestDashboardView(out, results, now, color, usageView{})
}

func renderUsageTestDashboardView(out *bytes.Buffer, results []agents.AccountUsageResult, now time.Time, color bool, view usageView) {
	for i := range results {
		if results[i].Usage != nil {
			results[i].Headline = usageTestRegistry.UsageHeadline(*results[i].Usage)
		}
	}
	renderUsageDashboard(out, results, now, color, view)
}

// usageTestRow is the dashboard row of one result.
func usageTestRow(result agents.AccountUsageResult) usageDashboardRow {
	return usageDashboardRow{account: result.Account, usage: result.Usage, headline: usageTestRegistry.UsageHeadline(*result.Usage)}
}

func usageTestLimit(id, group, window string, remaining float64, reset time.Time) agents.UsageLimit {
	value := remaining
	limit := agents.UsageLimit{
		ID:                id,
		Group:             group,
		Window:            window,
		RemainingFraction: &value,
	}
	if !reset.IsZero() {
		limit.ResetsAt = &reset
	}
	return limit
}

func usageTestResult(name string, weekly, five float64, weeklyReset, fiveReset time.Time) agents.AccountUsageResult {
	snapshot := agents.UsageSnapshot{
		Provider:  "antigravity",
		AccountID: agents.AccountID("acct_" + name),
		Limits: []agents.UsageLimit{
			usageTestLimit("gemini-5h", "Gemini Models", "5h", five, fiveReset),
			usageTestLimit("gemini-weekly", "Gemini Models", "weekly", weekly, weeklyReset),
		},
	}
	return agents.AccountUsageResult{
		Account: agents.Account{
			ID:       agents.AccountID("acct_" + name),
			Provider: "antigravity",
			Name:     name,
		},
		Usage: &snapshot,
	}
}

func TestRenderUsageDashboardFleetRanking(t *testing.T) {
	now := time.Date(2026, 9, 25, 14, 0, 0, 0, time.UTC)
	results := []agents.AccountUsageResult{
		usageTestResult("low", 0.12, 1.0, now.Add(72*time.Hour), now.Add(4*time.Hour)),
		usageTestResult("middle", 0.59, 1.0, now.Add(60*time.Hour), now.Add(4*time.Hour)),
		usageTestResult("best", 0.75, 1.0, now.Add(55*time.Hour), now.Add(4*time.Hour)),
	}

	var out bytes.Buffer
	renderUsageTestDashboard(&out, results, now, false)
	got := out.String()

	for _, want := range []string{
		"Agent Fleet Usage",
		"Fleet Capacity (3 Accounts · 1 Provider)",
		"Usage Pool:",
		"◆ Antigravity · 3 accounts",
		"● 1 Ready",
		"▲ 1 Active",
		"✖ 1 Low",
		"Next Reset: best in 55h00m",
		"Gemini Models",
		"Weekly",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("dashboard missing %q:\n%s", want, got)
		}
	}

	sectionStart := strings.Index(got, "Gemini Models")
	if sectionStart < 0 {
		t.Fatalf("Gemini section missing:\n%s", got)
	}
	section := got[sectionStart:]
	best := strings.Index(section, "best")
	middle := strings.Index(section, "middle")
	low := strings.Index(section, "low")
	if best < 0 || middle < 0 || low < 0 || !(best < middle && middle < low) {
		t.Fatalf("rows not ranked by provider score:\n%s", got)
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("non-color render contains ANSI escapes: %q", got)
	}
}

func TestRenderUsageDashboardIncludesThirdPartyPool(t *testing.T) {
	now := time.Date(2026, 9, 25, 14, 0, 0, 0, time.UTC)
	result := usageTestResult("work", 0.70, 0.90, now.Add(48*time.Hour), now.Add(3*time.Hour))
	result.Usage.Limits = append(result.Usage.Limits,
		usageTestLimit("3p-5h", "Claude and GPT models", "5h", 0.55, now.Add(2*time.Hour)),
		usageTestLimit("3p-weekly", "Claude and GPT models", "weekly", 0.40, now.Add(24*time.Hour)),
	)

	var out bytes.Buffer
	renderUsageTestDashboard(&out, []agents.AccountUsageResult{result}, now, false)
	if got := out.String(); strings.Contains(got, "Claude & GPT Models") || !strings.Contains(got, "Gemini Models") {
		t.Fatalf("third-party pool shown by default:\n%s", got)
	}

	out.Reset()
	renderUsageTestDashboardView(&out, []agents.AccountUsageResult{result}, now, false, usageView{claudeGPT: true})
	got := out.String()
	if !strings.Contains(got, "Claude & GPT Models") ||
		!strings.Contains(got, "5h") ||
		!strings.Contains(got, "Weekly") {
		t.Fatalf("third-party pool missing:\n%s", got)
	}
}

func TestUsageProvidersAreOrderedClaudeCodexAntigravity(t *testing.T) {
	rows := []usageDashboardRow{
		{account: agents.Account{Provider: "antigravity"}},
		{account: agents.Account{Provider: "zeta"}},
		{account: agents.Account{Provider: "codex"}},
		{account: agents.Account{Provider: "claude"}},
		{account: agents.Account{Provider: "alpha"}},
	}
	got := usageProviders(rows)
	want := []agents.ProviderID{"claude", "codex", "antigravity", "alpha", "zeta"}
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestAntigravityFleetScoreUsesGeminiPrimaryPool(t *testing.T) {
	now := time.Date(2026, 9, 25, 14, 0, 0, 0, time.UTC)
	result := usageTestResult("work", 0.80, 0.90, now.Add(48*time.Hour), now.Add(3*time.Hour))
	result.Usage.Limits = append(result.Usage.Limits,
		usageTestLimit("3p-5h", "Claude and GPT models", "5h", 0.05, now.Add(2*time.Hour)),
		usageTestLimit("3p-weekly", "Claude and GPT models", "weekly", 0.05, now.Add(24*time.Hour)),
	)

	row := usageTestRow(result)

	score, ok := usageRowScore(row)
	if !ok || score != 0.80 {
		t.Fatalf("Antigravity fleet score = %v, %v; want 0.80 from Gemini primary pool", score, ok)
	}
}

func TestRenderUsageDashboardSeparatesProviders(t *testing.T) {
	now := time.Date(2026, 9, 25, 14, 0, 0, 0, time.UTC)
	antigravity := usageTestResult("google", 0.70, 0.90, now.Add(48*time.Hour), now.Add(3*time.Hour))
	codexRemaining := 0.60
	// A provider the dashboard has no adapter for still gets its own section.
	codex := agents.AccountUsageResult{
		Account: agents.Account{ID: "acct_other", Provider: "other", Name: "openai"},
		Usage: &agents.UsageSnapshot{
			Provider:  "other",
			AccountID: "acct_other",
			Limits: []agents.UsageLimit{
				{ID: "session", Window: "5h", RemainingFraction: &codexRemaining},
			},
		},
	}

	var out bytes.Buffer
	renderUsageTestDashboard(&out, []agents.AccountUsageResult{codex, antigravity}, now, false)
	got := out.String()
	for _, want := range []string{
		"Fleet Capacity (2 Accounts · 2 Providers)",
		"◆ Antigravity · 1 account",
		"◆ other · 1 account",
		"Usage dashboard adapter not implemented yet.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("multi-provider dashboard missing %q:\n%s", want, got)
		}
	}
}

func TestUsageRankStyleThresholds(t *testing.T) {
	tests := []struct {
		value float64
		code  string
	}{
		{0.90, "\x1b[92m"},
		{0.60, "\x1b[93m"},
		{0.30, "\x1b[38;5;208m"},
		{0.10, "\x1b[91m"},
	}
	for _, tt := range tests {
		got := usageRankStyle("value", tt.value, true)
		if !strings.HasPrefix(got, tt.code) || !strings.HasSuffix(got, "\x1b[0m") {
			t.Fatalf("rank %.2f = %q, want code %q", tt.value, got, tt.code)
		}
	}
}

func claudeTestResult(name string, five, week float64, models map[string]float64, now time.Time) agents.AccountUsageResult {
	limits := []agents.UsageLimit{
		usageTestLimit("five_hour", "claude", "5h", five, now.Add(3*time.Hour)),
		usageTestLimit("seven_day", "claude", "weekly", week, now.Add(48*time.Hour)),
	}
	for id, remaining := range models {
		limits = append(limits, usageTestLimit(id, "claude", "weekly", remaining, now.Add(48*time.Hour)))
	}
	return agents.AccountUsageResult{
		Account: agents.Account{ID: agents.AccountID("acct_" + name), Provider: "claude", Name: name},
		Usage:   &agents.UsageSnapshot{Provider: "claude", AccountID: agents.AccountID("acct_" + name), Limits: limits},
	}
}

func TestRenderUsageDashboardClaudeNextToAntigravity(t *testing.T) {
	now := time.Date(2026, 9, 25, 14, 0, 0, 0, time.UTC)
	results := []agents.AccountUsageResult{
		usageTestResult("google", 0.70, 0.90, now.Add(48*time.Hour), now.Add(3*time.Hour)),
		claudeTestResult("work", 0.90, 0.50, map[string]float64{"seven_day_opus": 0.25, "seven_day_sonnet": 1}, now),
		claudeTestResult("spare", 0.10, 0.80, nil, now),
		{Account: agents.Account{ID: "acct_tok", Provider: "claude", Name: "ci"}, Error: fmt.Errorf("%w: long-lived tokens cannot read usage", agents.ErrUsageUnsupported)},
		{Account: agents.Account{ID: "acct_bad", Provider: "claude", Name: "broken"}, Error: errors.New("not logged in")},
	}
	var out bytes.Buffer
	renderUsageTestDashboard(&out, results, now, false)
	got := out.String()
	for _, want := range []string{
		"Fleet Capacity (5 Accounts · 2 Providers)",
		"◆ Antigravity · 1 account",
		"◆ Claude Code · 4 accounts",
		"Per model",
		"opus 25% · sonnet 100%",
		"n/a",
		"long-lived tokens cannot read usage",
		"ERR",
		"undocumented endpoint",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("dashboard missing %q:\n%s", want, got)
		}
	}
	section := got[strings.Index(got, "◆ Claude Code"):]
	work, spare, ci, broken := strings.Index(section, "work"), strings.Index(section, "spare"), strings.Index(section, "ci "), strings.Index(section, "broken")
	if !(work >= 0 && work < spare && spare < ci && spare < broken) {
		t.Fatalf("Claude rows not ranked by min remaining, errors last:\n%s", section)
	}
	// Score is the lower of 5h and weekly: work 50%, spare 10%.
	if score, ok := usageRowScore(usageTestRow(results[1])); !ok || score != 0.50 {
		t.Fatalf("score = %v, %v", score, ok)
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatal("non-color render contains ANSI escapes")
	}
}

func TestRenderUsageDashboardClaudeWithoutModelsAndNextReset(t *testing.T) {
	now := time.Date(2026, 9, 25, 14, 0, 0, 0, time.UTC)
	var out bytes.Buffer
	renderUsageTestDashboard(&out, []agents.AccountUsageResult{claudeTestResult("work", 0.9, 0.5, nil, now)}, now, false)
	got := out.String()
	if strings.Contains(got, "Per model") || !strings.Contains(got, "Next Reset: work in 48h00m") {
		t.Fatalf("unexpected render:\n%s", got)
	}
}

func TestRenderUsageDashboardShowsClaudeWarnings(t *testing.T) {
	now := time.Date(2026, 9, 25, 14, 0, 0, 0, time.UTC)
	result := claudeTestResult("work", 0.9, 0.5, nil, now)
	result.Usage.Limits = nil
	result.Usage.Warnings = []string{"Claude usage response had no recognizable limits"}
	var out bytes.Buffer
	renderUsageTestDashboard(&out, []agents.AccountUsageResult{result}, now, false)
	if !strings.Contains(out.String(), "work: Claude usage response had no recognizable limits") {
		t.Fatalf("warning missing:\n%s", out.String())
	}
}

func codexTestResult(name, plan string, five, week float64, other map[string]float64, now time.Time) agents.AccountUsageResult {
	limits := []agents.UsageLimit{
		usageTestLimit("five_hour", "codex", "5h", five, now.Add(3*time.Hour)),
		usageTestLimit("weekly", "codex", "weekly", week, now.Add(48*time.Hour)),
	}
	ids := make([]string, 0, len(other))
	for id := range other {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		limit := usageTestLimit(id, "Spark", "5h", other[id], now.Add(time.Hour))
		limit.Label = "Spark 5h"
		limits = append(limits, limit)
	}
	return agents.AccountUsageResult{
		Account: agents.Account{ID: agents.AccountID("acct_" + name), Provider: "codex", Name: name},
		Usage:   &agents.UsageSnapshot{Provider: "codex", AccountID: agents.AccountID("acct_" + name), Plan: plan, Limits: limits},
	}
}

func TestRenderUsageDashboardCodex(t *testing.T) {
	now := time.Date(2026, 9, 25, 14, 0, 0, 0, time.UTC)
	spare := codexTestResult("spare", "free", 0.10, 0.80, nil, now)
	spare.Usage.Credits = []agents.UsageCredit{{ID: "codex_credits", Label: "Credits", Unit: agents.CreditUnitCredits, Balance: ptrFloat(12.5), Enabled: true}}
	results := []agents.AccountUsageResult{
		spare,
		codexTestResult("work", "plus", 0.90, 0.50, map[string]float64{"codex_spark_five_hour": 0.35}, now),
		{Account: agents.Account{ID: "acct_ci", Provider: "codex", Name: "ci"}, Error: fmt.Errorf("%w: API key profiles have no plan limits", agents.ErrUsageUnsupported)},
		{Account: agents.Account{ID: "acct_out", Provider: "codex", Name: "out"}, Error: fmt.Errorf("%w: run `bonsai agent run out -- login`", agents.ErrNotAuthenticated)},
		{Account: agents.Account{ID: "acct_bad", Provider: "codex", Name: "broken"}, Error: errors.New("codex usage unavailable: timed out")},
	}
	var out bytes.Buffer
	renderUsageTestDashboard(&out, results, now, false)
	got := out.String()
	for _, want := range []string{
		"Fleet Capacity (5 Accounts · 1 Provider)",
		"◆ Codex · 5 accounts",
		"Plan",
		"Other",
		"plus",
		"Spark 5h 35%",
		"API key profiles have no plan limits",
		"not logged in",
		"ERR",
		"spare: Credits 12.5 left",
		"Next Reset: work in 48h00m",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("dashboard missing %q:\n%s", want, got)
		}
	}
	section := got[strings.Index(got, "◆ Codex"):]
	work, spareAt, ci, broken := strings.Index(section, "work"), strings.Index(section, "spare "), strings.Index(section, "ci "), strings.Index(section, "broken")
	if !(work >= 0 && work < spareAt && spareAt < ci && spareAt < broken) {
		t.Fatalf("Codex rows not ranked by min remaining, errors last:\n%s", section)
	}
	// Score is the lower of 5h and weekly; other limits do not count.
	if score, ok := usageRowScore(usageTestRow(results[1])); !ok || score != 0.50 {
		t.Fatalf("score = %v, %v", score, ok)
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatal("non-color render contains ANSI escapes")
	}
}

func TestRenderUsageDashboardCodexWithoutOtherLimits(t *testing.T) {
	now := time.Date(2026, 9, 25, 14, 0, 0, 0, time.UTC)
	var out bytes.Buffer
	renderUsageTestDashboard(&out, []agents.AccountUsageResult{codexTestResult("work", "plus", 0.9, 0.5, nil, now)}, now, false)
	if got := out.String(); strings.Contains(got, "Other") || !strings.Contains(got, "Plan") {
		t.Fatalf("unexpected render:\n%s", got)
	}
}

// The three-provider dashboard is pinned in full so a change to any adapter
// shows up as one readable diff.
func ptrFloat(v float64) *float64    { return &v }
func ptrTime(t time.Time) *time.Time { return &t }

func TestRenderUsageDashboardThreeProvidersGolden(t *testing.T) {
	now := time.Date(2026, 9, 25, 14, 0, 0, 0, time.UTC)
	results := []agents.AccountUsageResult{
		usageTestResult("google", 0.70, 0.90, now.Add(48*time.Hour), now.Add(3*time.Hour)),
		claudeTestResult("work", 0.90, 0.50, map[string]float64{"seven_day_opus": 0.25}, now),
		codexTestResult("main", "Plus", 0.72, 0.96, map[string]float64{"codex_spark_five_hour": 0.35}, now),
	}
	results[0].Usage.Plan = "Google AI Pro"
	results[0].Usage.Credits = []agents.UsageCredit{{ID: "antigravity_credits", Label: "Credits", Unit: agents.CreditUnitCredits, Balance: ptrFloat(250), Enabled: true}}
	results[1].Usage.Plan = "Max 5x"
	results[1].Usage.Credits = []agents.UsageCredit{
		{ID: "iguana_necktie", Label: "Usage credits", Unit: agents.CreditUnitUSD, Used: ptrFloat(1.90559), Limit: ptrFloat(100), Balance: ptrFloat(98.09441), Enabled: true, ResetsAt: ptrTime(now.Add(41 * 24 * time.Hour))},
		{ID: "extra_usage", Label: "Extra usage", Unit: agents.CreditUnitUSD, Used: ptrFloat(0)},
	}
	results[2].Usage.Credits = []agents.UsageCredit{{ID: "codex_credits", Label: "Credits", Unit: agents.CreditUnitCredits, Balance: ptrFloat(12.5), Enabled: true}}
	results[2].Usage.ResetCredits = []agents.ResetCredit{
		{Title: "Full reset", Status: "available", ExpiresAt: ptrTime(now.Add(20 * 24 * time.Hour))},
		{Title: "Full reset", Status: "available", ExpiresAt: ptrTime(now.Add(9 * 24 * time.Hour))},
		{Title: "Full reset", Status: "redeemed"},
		{Title: "Full reset", Status: "available", ExpiresAt: ptrTime(now.Add(-time.Hour))},
	}
	var out bytes.Buffer
	renderUsageTestDashboard(&out, results, now, false)
	want, err := os.ReadFile(filepath.Join("testdata", "usage-three-providers.golden"))
	if err != nil {
		t.Fatal(err)
	}
	// A checkout with core.autocrlf rewrites the golden with CRLF.
	want = bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))
	if got := out.String(); got != string(want) {
		t.Fatalf("dashboard differs from testdata/usage-three-providers.golden:\n%s", got)
	}
}

// The API reports a Gemini pool only for some profiles. One with only its
// Claude & GPT pool must not render as a blank row.
func TestRenderUsageDashboardShowsThirdPartyPoolWhenGeminiIsMissing(t *testing.T) {
	now := time.Date(2026, 9, 25, 14, 0, 0, 0, time.UTC)
	result := agents.AccountUsageResult{
		Account: agents.Account{ID: "acct_tp", Provider: "antigravity", Name: "thirdonly"},
		Usage: &agents.UsageSnapshot{Provider: "antigravity", AccountID: "acct_tp", Limits: []agents.UsageLimit{
			usageTestLimit("3p-5h", "Claude and GPT models", "5h", 0.40, now.Add(2*time.Hour)),
			usageTestLimit("3p-weekly", "Claude and GPT models", "weekly", 0.70, now.Add(24*time.Hour)),
		}},
	}
	var out bytes.Buffer
	renderUsageTestDashboard(&out, []agents.AccountUsageResult{result}, now, false)
	got := out.String()
	for _, want := range []string{"40%", "70%", "(Claude & GPT)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("dashboard missing %q:\n%s", want, got)
		}
	}
}
