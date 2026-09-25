package antigravity

import (
	"testing"
	"time"
)

func TestParseUsageModernShape(t *testing.T) {
	data := []byte(`{
		"command": {
			"data": {
				"groups": [
					{
						"name": "Gemini Models",
						"buckets": [
							{"id":"gemini-5h","window":"5h","remaining_fraction":0.83,"reset_time":"2026-09-25T18:00:00Z"},
							{"id":"gemini-weekly","window":"weekly","remainingFraction":0.72}
						]
					},
					{
						"name": "Claude and GPT models",
						"buckets": [{"id":"third-party-5h","window":"5h","remaining_fraction":1.2}]
					}
				]
			}
		}
	}`)
	limits, err := ParseUsage(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(limits) != 3 {
		t.Fatalf("limit count = %d, want 3", len(limits))
	}
	if limits[0].Group != "Gemini Models" || limits[0].Window != "5h" {
		t.Fatalf("first limit = %#v", limits[0])
	}
	if limits[0].RemainingFraction == nil || *limits[0].RemainingFraction != 0.83 {
		t.Fatalf("remaining = %#v", limits[0].RemainingFraction)
	}
	wantReset, _ := time.Parse(time.RFC3339, "2026-09-25T18:00:00Z")
	if limits[0].ResetsAt == nil || !limits[0].ResetsAt.Equal(wantReset) {
		t.Fatalf("reset = %#v", limits[0].ResetsAt)
	}
	if limits[2].RemainingFraction == nil || *limits[2].RemainingFraction != 1 {
		t.Fatalf("out-of-range fraction not clamped: %#v", limits[2].RemainingFraction)
	}
}

func TestParseUsageRejectsMalformedShapes(t *testing.T) {
	for _, data := range [][]byte{
		[]byte(`not-json`),
		[]byte(`{"command":{"data":{"other":[]}}}`),
	} {
		if _, err := ParseUsage(data); err == nil {
			t.Fatalf("expected parse error for %s", data)
		}
	}
}
