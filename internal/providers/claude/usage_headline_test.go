package claude

import (
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
	var p agents.UsageLayout = New(nil, nil, nil, nil)
	for name, tt := range map[string]struct {
		body []byte
		want string
	}{
		"recorded limits[] response": {readFixture(t, "usage-response.json"), "primary=five_hour secondary=seven_day extra=[] score=0.500 low=false reset=2026-10-11T09:00:00Z"},
		// The weekly total has no reset, so the 5h reset is next.
		"flat buckets with per-model weekly": {[]byte(`{"five_hour":{"utilization":25,"resets_at":"2026-10-09T19:00:00Z"},"seven_day":{"utilization":40.5,"resets_at":null},"seven_day_opus":{"utilization":100,"resets_at":"2026-10-11T09:00:00Z"},"seven_day_sonnet":null}`), "primary=five_hour secondary=seven_day extra=[seven_day_opus] score=0.595 low=false reset=2026-10-09T19:00:00Z"},
		"limits[] with scope":                {[]byte(`{"limits":[{"kind":"weekly_all","group":"weekly","percent":85,"resets_at":"2026-10-11T09:00:00Z"},{"kind":"weekly_model","group":"weekly","percent":10,"scope":"haiku"},{"kind":"session","percent":5}]}`), "primary=five_hour secondary=seven_day extra=[weekly_model_haiku] score=0.150 low=true reset=2026-10-11T09:00:00Z"},
		"no limits":                          {[]byte(`{}`), "primary=- secondary=- extra=[] score=- low=false reset=-"},
	} {
		limits, _, err := ParseUsage(tt.body)
		if err != nil {
			t.Fatal(err)
		}
		if got := describeHeadline(p.UsageHeadline(agents.UsageSnapshot{Provider: ProviderID, Limits: limits})); got != tt.want {
			t.Errorf("%s:\n got %s\nwant %s", name, got, tt.want)
		}
	}
}

func TestUsageNote(t *testing.T) {
	var noter agents.UsageNoter = New(nil, nil, nil, nil)
	if !strings.Contains(noter.UsageNote(), "undocumented endpoint") {
		t.Fatal(noter.UsageNote())
	}
}
