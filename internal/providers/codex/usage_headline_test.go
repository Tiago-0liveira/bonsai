package codex

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// describeHeadline flattens a headline for comparison.
func describeHeadline(h agents.UsageHeadline) string {
	id := func(l *agents.UsageLimit) string {
		if l == nil {
			return "-"
		}
		return l.ID
	}
	extra := make([]string, 0, len(h.Extra))
	for _, l := range h.Extra {
		extra = append(extra, l.ID)
	}
	score, reset := "-", "-"
	if h.Score != nil {
		score = fmt.Sprintf("%.3f", *h.Score)
	}
	if h.NextReset != nil {
		reset = h.NextReset.UTC().Format(time.RFC3339)
	}
	return fmt.Sprintf("primary=%s secondary=%s extra=[%s] score=%s low=%v reset=%s", id(h.Primary), id(h.Secondary), strings.Join(extra, " "), score, h.Low, reset)
}

func TestUsageHeadline(t *testing.T) {
	var p agents.UsageLayout = New(nil, nil)
	for _, tt := range []struct {
		fixture []string
		want    string
	}{
		{[]string{"phase0", "rate-limits.json"}, "primary=five_hour secondary=weekly extra=[] score=0.720 low=false reset=2026-10-18T11:18:39Z"},
		{[]string{"rate-limits-multi.json"}, "primary=five_hour secondary=weekly extra=[codex_spark_five_hour codex_spark_window_90m review_window_1440m] score=0.000 low=true reset=2026-10-18T11:18:39Z"},
	} {
		var response struct {
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(readFixture(t, tt.fixture...), &response); err != nil {
			t.Fatal(err)
		}
		limits, _, _, err := ParseRateLimits(response.Result)
		if err != nil {
			t.Fatal(err)
		}
		snapshot := agents.UsageSnapshot{Provider: ProviderID, Limits: limits}
		if got := describeHeadline(p.UsageHeadline(snapshot)); got != tt.want {
			t.Errorf("%v:\n got %s\nwant %s", tt.fixture, got, tt.want)
		}
	}
}

func TestUsageHeadlineWithoutMainWindows(t *testing.T) {
	remaining := 0.5
	h := New(nil, nil).UsageHeadline(agents.UsageSnapshot{Limits: []agents.UsageLimit{{ID: "review_window_1440m", RemainingFraction: &remaining}}})
	if got := describeHeadline(h); got != "primary=- secondary=- extra=[review_window_1440m] score=- low=false reset=-" {
		t.Fatal(got)
	}
}
