package agym

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"
)

func TestParseEventsPage(t *testing.T) {
	data, err := os.ReadFile("testdata/v1/events_page.json")
	if err != nil {
		t.Fatal(err)
	}

	client := NewClient("").WithRunner(func(ctx context.Context, stdin io.Reader, args ...string) ([]byte, []byte, error) {
		return data, nil, nil
	})

	page, err := client.GetEvents(context.Background(), "run-xyz-789", 0, 200)
	if err != nil {
		t.Fatalf("GetEvents() error: %v", err)
	}
	if len(page.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(page.Events))
	}
	if page.NextCursor != 2 {
		t.Errorf("next_cursor = %d, want 2", page.NextCursor)
	}

	out, err := ParseOutputPayload(page.Events[0])
	if err != nil {
		t.Fatalf("ParseOutputPayload() error: %v", err)
	}
	if out.Stream != "stdout" {
		t.Errorf("stream = %q, want stdout", out.Stream)
	}
	if out.Text != "Initializing Antigravity workspace...\n" {
		t.Errorf("text = %q", out.Text)
	}
}

func TestParseNDJSONEvent(t *testing.T) {
	line := []byte(`{"run_id":"run-1","seq":1,"timestamp":"2026-09-23T12:00:00Z","type":"output","payload":{"stream":"stdout","text":"hello world\n"}}`)
	var ev Event
	if err := json.Unmarshal(line, &ev); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if ev.Seq != 1 || ev.RunID != "run-1" {
		t.Errorf("unexpected event: %+v", ev)
	}
	payload, err := ParseOutputPayload(ev)
	if err != nil {
		t.Fatalf("parse output payload: %v", err)
	}
	if payload.Text != "hello world\n" {
		t.Errorf("payload text = %q, want hello world\n", payload.Text)
	}
}
