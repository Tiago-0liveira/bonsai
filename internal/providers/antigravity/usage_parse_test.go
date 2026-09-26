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

func TestParseUsageAntigravityCLIShape(t *testing.T) {
	data := []byte(`{
		"response": {
			"groups": [
				{
					"displayName": "Gemini Models",
					"buckets": [
						{"bucketId":"gemini-5h","remainingFraction":1,"resetTime":"2026-09-25T18:00:00Z"},
						{"bucketId":"gemini-weekly","remainingFraction":0.28,"resetTime":"2026-09-27T21:49:00Z"}
					]
				},
				{
					"displayName": "Claude and GPT models",
					"buckets": [
						{"bucketId":"3p-5h","window":"5h","remainingFraction":0.91},
						{"bucketId":"3p-weekly","window":"weekly","remainingFraction":0.66}
					]
				}
			]
		}
	}`)
	limits, err := ParseUsage(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(limits) != 4 {
		t.Fatalf("limit count = %d, want 4", len(limits))
	}
	if limits[0].Group != "Gemini Models" || limits[0].ID != "gemini-5h" || limits[0].Window != "5h" {
		t.Fatalf("gemini 5h = %#v", limits[0])
	}
	if limits[1].Group != "Gemini Models" || limits[1].ID != "gemini-weekly" || limits[1].Window != "weekly" {
		t.Fatalf("gemini weekly = %#v", limits[1])
	}
	if limits[1].RemainingFraction == nil || *limits[1].RemainingFraction != 0.28 {
		t.Fatalf("weekly remaining = %#v", limits[1].RemainingFraction)
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
