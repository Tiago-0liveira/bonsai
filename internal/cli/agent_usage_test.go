package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

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
	renderUsageDashboard(&out, results, now, false)
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
	renderUsageDashboard(&out, []agents.AccountUsageResult{result}, now, false)
	got := out.String()
	if !strings.Contains(got, "Claude & GPT Models") ||
		!strings.Contains(got, "5h") ||
		!strings.Contains(got, "Weekly") {
		t.Fatalf("third-party pool missing:\n%s", got)
	}
}

func TestAntigravityFleetScoreUsesGeminiPrimaryPool(t *testing.T) {
	now := time.Date(2026, 9, 25, 14, 0, 0, 0, time.UTC)
	result := usageTestResult("work", 0.80, 0.90, now.Add(48*time.Hour), now.Add(3*time.Hour))
	result.Usage.Limits = append(result.Usage.Limits,
		usageTestLimit("3p-5h", "Claude and GPT models", "5h", 0.05, now.Add(2*time.Hour)),
		usageTestLimit("3p-weekly", "Claude and GPT models", "weekly", 0.05, now.Add(24*time.Hour)),
	)

	row := usageDashboardRow{account: result.Account, usage: result.Usage}
	for i := range result.Usage.Limits {
		limit := &result.Usage.Limits[i]
		switch classifyUsageLimit(*limit) {
		case "gemini-5h":
			row.gemini5h = limit
		case "gemini-weekly":
			row.geminiWeek = limit
		case "third-5h":
			row.third5h = limit
		case "third-weekly":
			row.thirdWeek = limit
		}
	}

	score, ok := usageRowScore(row)
	if !ok || score != 0.80 {
		t.Fatalf("Antigravity fleet score = %v, %v; want 0.80 from Gemini primary pool", score, ok)
	}
}

func TestRenderUsageDashboardSeparatesProviders(t *testing.T) {
	now := time.Date(2026, 9, 25, 14, 0, 0, 0, time.UTC)
	antigravity := usageTestResult("google", 0.70, 0.90, now.Add(48*time.Hour), now.Add(3*time.Hour))
	codexRemaining := 0.60
	codex := agents.AccountUsageResult{
		Account: agents.Account{ID: "acct_codex", Provider: "codex", Name: "openai"},
		Usage: &agents.UsageSnapshot{
			Provider:  "codex",
			AccountID: "acct_codex",
			Limits: []agents.UsageLimit{
				{ID: "session", Window: "5h", RemainingFraction: &codexRemaining},
			},
		},
	}

	var out bytes.Buffer
	renderUsageDashboard(&out, []agents.AccountUsageResult{codex, antigravity}, now, false)
	got := out.String()
	for _, want := range []string{
		"Fleet Capacity (2 Accounts · 2 Providers)",
		"◆ Antigravity · 1 account",
		"◆ Codex · 1 account",
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

func TestClassifyUsageLimitAntigravityNames(t *testing.T) {
	tests := []struct {
		limit agents.UsageLimit
		want  string
	}{
		{agents.UsageLimit{ID: "gemini-5h", Group: "Gemini Models", Window: "5h"}, "gemini-5h"},
		{agents.UsageLimit{ID: "gemini-weekly", Group: "Gemini Models", Window: "weekly"}, "gemini-weekly"},
		{agents.UsageLimit{ID: "3p-5h", Group: "Claude and GPT models", Window: "5h"}, "third-5h"},
		{agents.UsageLimit{ID: "3p-weekly", Group: "Claude and GPT models", Window: "weekly"}, "third-weekly"},
	}
	for _, tt := range tests {
		if got := classifyUsageLimit(tt.limit); got != tt.want {
			t.Fatalf("classify %#v = %q, want %q", tt.limit, got, tt.want)
		}
	}
}
