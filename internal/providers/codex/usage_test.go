package codex

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func limitByID(t *testing.T, limits []agents.UsageLimit, id string) agents.UsageLimit {
	t.Helper()
	for _, l := range limits {
		if l.ID == id {
			return l
		}
	}
	t.Fatalf("limit %q missing in %+v", id, limits)
	return agents.UsageLimit{}
}

func readFixture(t *testing.T, parts ...string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(append([]string{"testdata"}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestUsageReadsRateLimits(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	auth := authPath(e.profileHome(account))
	before, _ := os.ReadFile(auth)

	snapshot, err := e.provider.Usage(t.Context(), account, agents.UsageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Provider != ProviderID || snapshot.AccountID != account.ID || snapshot.Plan != "Plus" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if len(snapshot.Limits) != 2 || len(snapshot.Warnings) != 0 {
		t.Fatalf("limits = %+v warnings = %v", snapshot.Limits, snapshot.Warnings)
	}
	five := limitByID(t, snapshot.Limits, "five_hour")
	week := limitByID(t, snapshot.Limits, "weekly")
	if *five.RemainingFraction != 0.72 || five.Window != "5h" || five.Label != "5h" || five.Group != "codex" {
		t.Fatalf("five = %+v", five)
	}
	if *week.RemainingFraction != 0.96 || week.Window != "weekly" {
		t.Fatalf("week = %+v", week)
	}
	if want := time.Unix(1791735519, 0).UTC(); five.ResetsAt == nil || !five.ResetsAt.Equal(want) {
		t.Fatalf("resets = %v", five.ResetsAt)
	}
	if want := []string{"initialize", "initialized", "account/rateLimits/read"}; !reflect.DeepEqual(e.rpcs(), want) {
		t.Fatalf("rpcs = %v", e.rpcs())
	}
	if after, _ := os.ReadFile(auth); string(after) != string(before) {
		t.Fatal("auth.json changed")
	}
	if !e.provider.Capabilities().Usage || e.provider.UsageTTL() != 5*time.Minute {
		t.Fatal("usage capability and 5 minute policy expected")
	}
}

func TestUsageGroupsOtherLimitsAndCredits(t *testing.T) {
	e := newTestEnv(t)
	e.fixtures(map[string]string{"rate-limits.json": string(readFixture(t, "rate-limits-multi.json"))})
	account := e.addLogin("work")
	snapshot, err := e.provider.Usage(t.Context(), account, agents.UsageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, l := range snapshot.Limits {
		ids = append(ids, l.ID)
	}
	want := []string{"five_hour", "weekly", "codex_spark_five_hour", "codex_spark_window_90m", "review_window_1440m"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	if got := *limitByID(t, snapshot.Limits, "five_hour").RemainingFraction; got < 0.094 || got > 0.096 {
		t.Fatalf("five remaining = %v", got)
	}
	if got := *limitByID(t, snapshot.Limits, "weekly").RemainingFraction; got != 0 {
		t.Fatalf("over-100%% usage must clamp to 0 remaining, got %v", got)
	}
	spark := limitByID(t, snapshot.Limits, "codex_spark_window_90m")
	if spark.Group != "Spark" || spark.Label != "Spark 90m" || spark.Window != "90m" || spark.ResetsAt != nil {
		t.Fatalf("spark = %+v", spark)
	}
	if got := limitByID(t, snapshot.Limits, "review_window_1440m"); got.Group != "review" || got.Label != "review 1d" {
		t.Fatalf("review = %+v", got)
	}
	if snapshot.Plan != "Pro" || len(snapshot.Warnings) != 0 || len(snapshot.Credits) != 1 {
		t.Fatalf("plan %q warnings %v credits %+v", snapshot.Plan, snapshot.Warnings, snapshot.Credits)
	}
	if c := snapshot.Credits[0]; c.Unit != agents.CreditUnitCredits || !c.Enabled || c.Balance == nil || *c.Balance != 12.5 {
		t.Fatalf("credit = %+v", c)
	}
	if data, _ := json.Marshal(snapshot); strings.Contains(string(data), "sk-should-never-show") {
		t.Fatal("unknown response fields must not be carried over")
	}
}

func TestParseRateLimitsResetCredits(t *testing.T) {
	var response struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(readFixture(t, "phase0", "rate-limits.json"), &response); err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseRateLimits(response.Result)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.ResetCredits) != 3 || len(parsed.Warnings) != 0 {
		t.Fatalf("parsed = %+v", parsed)
	}
	first := parsed.ResetCredits[0]
	if first.Title != "Full reset (Weekly + 5 hr)" || first.Status != "available" ||
		first.GrantedAt.Unix() != 1790100821 || first.ExpiresAt.Unix() != 1792692821 {
		t.Fatalf("first = %+v", first)
	}
	// hasCredits false with a zero balance is still a balance, just not enabled.
	if len(parsed.Credits) != 1 || parsed.Credits[0].Enabled || *parsed.Credits[0].Balance != 0 {
		t.Fatalf("credits = %+v", parsed.Credits)
	}
	if data, _ := json.Marshal(parsed.ResetCredits); strings.Contains(string(data), "RateLimitResetCredit_") || strings.Contains(string(data), "Thanks") {
		t.Fatalf("credit ids or descriptions leaked: %s", data)
	}
	// A malformed block is ignored, an unlimited balance kept.
	parsed, _ = ParseRateLimits([]byte(`{"rateLimits":{"credits":{"unlimited":true,"balance":"x"}},"rateLimitResetCredits":"nope"}`))
	if len(parsed.ResetCredits) != 0 || len(parsed.Credits) != 1 || !parsed.Credits[0].Unlimited || parsed.Credits[0].Balance != nil {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestParseRateLimitsToleratesUnknownShapes(t *testing.T) {
	for name, body := range map[string]string{
		"empty object":   `{}`,
		"wrong types":    `{"rateLimits":"nope","rateLimitsByLimitId":[1,2]}`,
		"no windows":     `{"rateLimits":{"limitId":"codex","primary":null,"secondary":null}}`,
		"missing usedPc": `{"rateLimits":{"primary":{"windowDurationMins":300}}}`,
	} {
		parsed, err := ParseRateLimits([]byte(body))
		if err != nil || len(parsed.Limits) != 0 || len(parsed.Warnings) != 1 {
			t.Errorf("%s: parsed=%+v err=%v", name, parsed, err)
		}
	}
	for _, body := range []string{`"text"`, `[]`, `null`, `not json`} {
		if _, err := ParseRateLimits([]byte(body)); err == nil {
			t.Errorf("%s accepted", body)
		}
	}
	// A window without a length is named by its slot rather than dropped.
	parsed, _ := ParseRateLimits([]byte(`{"rateLimits":{"primary":{"usedPercent":30},"secondary":{"usedPercent":10}}}`))
	limits := parsed.Limits
	if len(limits) != 2 || limits[0].ID != "primary" || limits[1].ID != "secondary" {
		t.Fatalf("limits = %+v", limits)
	}
	// With only the by-id map, the `codex` entry is the main group.
	parsed, _ = ParseRateLimits([]byte(`{"rateLimitsByLimitId":{"codex":{"limitId":"codex","primary":{"usedPercent":50,"windowDurationMins":300}}}}`))
	limits = parsed.Limits
	if len(limits) != 1 || limits[0].ID != "five_hour" {
		t.Fatalf("limits = %+v", limits)
	}
}

func TestUsageNeverStartsCodexWhenTheAnswerIsKnown(t *testing.T) {
	e := newTestEnv(t)
	key := e.addAPIKey("ci")
	if _, err := e.provider.Usage(t.Context(), key, agents.UsageOptions{}); !errors.Is(err, agents.ErrUsageUnsupported) ||
		!strings.Contains(err.Error(), "API key profiles have no plan limits") {
		t.Fatalf("api key: %v", err)
	}
	out := e.addLogin("out")
	if err := os.Remove(authPath(e.profileHome(out))); err != nil {
		t.Fatal(err)
	}
	if _, err := e.provider.Usage(t.Context(), out, agents.UsageOptions{}); !errors.Is(err, agents.ErrNotAuthenticated) {
		t.Fatalf("logged out: %v", err)
	}
	if e.spawns() != 0 {
		t.Fatalf("spawns = %d", e.spawns())
	}
}

func TestUsageRemembersFailuresForAMinute(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	now := e.clock(time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC))
	t.Setenv("FAKE_CODEX_APPSERVER", "exit")
	_, first := e.provider.Usage(t.Context(), account, agents.UsageOptions{})
	if first == nil || !strings.Contains(first.Error(), "exited early") {
		t.Fatalf("first = %v", first)
	}
	_, second := e.provider.Usage(t.Context(), account, agents.UsageOptions{})
	if second == nil || second.Error() != first.Error() || e.spawns() != 1 {
		t.Fatalf("second = %v, spawns = %d", second, e.spawns())
	}
	// A forced refresh goes to Codex again.
	e.provider.Usage(t.Context(), account, agents.UsageOptions{Refresh: true})
	if e.spawns() != 2 {
		t.Fatalf("spawns = %d after refresh", e.spawns())
	}
	t.Setenv("FAKE_CODEX_APPSERVER", "")
	if _, err := e.provider.Usage(t.Context(), account, agents.UsageOptions{}); err == nil {
		t.Fatal("failure should still be remembered")
	}
	*now = now.Add(usageFailureTTL + time.Second)
	if _, err := e.provider.Usage(t.Context(), account, agents.UsageOptions{}); err != nil {
		t.Fatalf("after the memo: %v", err)
	}
}

func TestUsageErrorsNeverCarryServerText(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	for name, mode := range map[string]string{"error reply": "error", "early exit": "exit", "after init": "exit-after-init", "garbage": "noisy"} {
		t.Setenv("FAKE_CODEX_APPSERVER", mode)
		if mode == "noisy" { // noisy still answers; make the answer itself bad
			e.fixtures(map[string]string{"rate-limits.json": `{"id":3,"error":{"code":-32000,"message":"Bearer sk-SECRETSECRET1234 refresh_token_reused"}}`})
		}
		_, err := e.provider.Usage(t.Context(), account, agents.UsageOptions{Refresh: true})
		if err == nil {
			t.Fatalf("%s: expected an error", name)
		}
		for _, leak := range []string{"sk-", "Bearer", "SECRET", "refresh_token", "fake-access", "fake-refresh"} {
			if strings.Contains(err.Error(), leak) {
				t.Errorf("%s: error %q contains %q", name, err, leak)
			}
		}
	}
}

func TestUsageDoesNotLeakAuthFile(t *testing.T) {
	e := newTestEnv(t)
	e.fixtures(map[string]string{"rate-limits.json": `{"id":3,"result":{"rateLimits":{"limitId":"codex","planType":"plus","primary":{"usedPercent":1,"windowDurationMins":300}},"accessToken":"fake-access"}}`})
	account := e.addLogin("work")
	snapshot, err := e.provider.Usage(t.Context(), account, agents.UsageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(snapshot)
	for _, leak := range []string{"fake-access", "fake-refresh", "id_token"} {
		if strings.Contains(string(data), leak) {
			t.Fatalf("snapshot leaks %q: %s", leak, data)
		}
	}
}

func TestUsageServiceIncludesCodex(t *testing.T) {
	e := newTestEnv(t)
	e.addLogin("work")
	e.addAPIKey("ci")
	service := &agents.UsageService{Accounts: e.accounts, Registry: e.registry, TTL: time.Minute}
	results := service.All(t.Context(), agents.UsageOptions{})
	if len(results) != 2 {
		t.Fatalf("results = %+v", results)
	}
	for _, r := range results {
		switch r.Account.Name {
		case "work":
			if r.Error != nil || r.Usage == nil || r.Usage.Plan != "Plus" {
				t.Errorf("work = %+v", r)
			}
		case "ci":
			if !errors.Is(r.Error, agents.ErrUsageUnsupported) {
				t.Errorf("ci = %+v", r)
			}
		}
	}
}
