package antigravity

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// describeHeadline flattens a headline for comparison; extras show their window.
func describeHeadline(h agents.UsageHeadline) string {
	id := func(l *agents.UsageLimit) string {
		if l == nil {
			return "-"
		}
		return l.ID
	}
	extra := make([]string, 0, len(h.Extra))
	for _, l := range h.Extra {
		extra = append(extra, l.ID+"@"+l.Window)
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
	for name, tt := range map[string]struct {
		body string
		want string
	}{
		// Weekly has no reset, so the 5h reset is next.
		"modern shape": {`{"command":{"data":{"groups":[
			{"name":"Gemini Models","buckets":[{"id":"gemini-5h","window":"5h","remaining_fraction":0.83,"reset_time":"2026-09-25T18:00:00Z"},{"id":"gemini-weekly","window":"weekly","remainingFraction":0.72}]},
			{"name":"Claude and GPT models","buckets":[{"id":"third-party-5h","window":"5h","remaining_fraction":1.2}]}]}}}`,
			"primary=gemini-5h secondary=gemini-weekly extra=[third-party-5h@5h] score=0.720 low=false reset=2026-09-25T18:00:00Z"},
		"antigravity CLI shape": {`{"response":{"groups":[
			{"displayName":"Gemini Models","buckets":[{"bucketId":"gemini-5h","remainingFraction":1,"resetTime":"2026-09-25T18:00:00Z"},{"bucketId":"gemini-weekly","remainingFraction":0.28,"resetTime":"2026-09-27T21:49:00Z"}]},
			{"displayName":"Claude and GPT models","buckets":[{"bucketId":"3p-5h","window":"5h","remainingFraction":0.05},{"bucketId":"3p-weekly","window":"weekly","remainingFraction":0.66}]}]}}`,
			"primary=gemini-5h secondary=gemini-weekly extra=[3p-5h@5h 3p-weekly@weekly] score=0.280 low=false reset=2026-09-27T21:49:00Z"},
		// The third-party pool never lowers the score; a window named only by
		// the label is set from the classification.
		"low gemini weekly": {`{"groups":[
			{"name":"Gemini Models","buckets":[{"id":"gemini-5h","remainingFraction":0.9},{"id":"gemini-weekly","remainingFraction":0.1,"resetTime":"2026-09-27T21:49:00Z"},{"id":"gemini-weekly-dup","window":"weekly","remainingFraction":1}]},
			{"name":"Claude and GPT models","buckets":[{"id":"3p-a","label":"Weekly 3p"}]},
			{"name":"Other","buckets":[{"id":"daily","remainingFraction":0}]}]}`,
			"primary=gemini-5h secondary=gemini-weekly extra=[3p-a@weekly] score=0.100 low=true reset=2026-09-27T21:49:00Z"},
	} {
		limits, err := ParseUsage([]byte(tt.body))
		if err != nil {
			t.Fatal(err)
		}
		if got := describeHeadline(p.UsageHeadline(agents.UsageSnapshot{Provider: ProviderID, Limits: limits})); got != tt.want {
			t.Errorf("%s:\n got %s\nwant %s", name, got, tt.want)
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
		{agents.UsageLimit{ID: "daily", Group: "Other"}, ""},
	}
	for _, tt := range tests {
		if got := classifyUsageLimit(tt.limit); got != tt.want {
			t.Fatalf("classify %#v = %q, want %q", tt.limit, got, tt.want)
		}
	}
}
