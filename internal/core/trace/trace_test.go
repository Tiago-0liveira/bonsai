package trace

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestSpanDisabledEmitsNothing(t *testing.T) {
	var buf bytes.Buffer
	defer SetOutput(&buf)()
	SetEnabled(false)
	Start("local.inventory", "p1")()
	if buf.Len() != 0 {
		t.Fatalf("expected no output, got %q", buf.String())
	}
}

func TestSpanEmitsJSONWithSpawnDeltas(t *testing.T) {
	var buf bytes.Buffer
	defer SetOutput(&buf)()
	SetEnabled(true)
	defer SetEnabled(false)
	end := Start("provider.prs", "p1", Attrs{Page: 2})
	AddGit()
	AddGit()
	AddGH()
	end()
	var rec map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &rec); err != nil {
		t.Fatalf("not JSON: %v: %q", err, buf.String())
	}
	if rec["span"] != "provider.prs" || rec["project"] != "p1" || rec["page"] != float64(2) {
		t.Fatalf("unexpected record: %v", rec)
	}
	if rec["git_spawns"] != float64(2) || rec["gh_spawns"] != float64(1) {
		t.Fatalf("spawn deltas wrong: %v", rec)
	}
	if _, ok := rec["dur_ms"]; !ok {
		t.Fatal("missing dur_ms")
	}
}

func TestCountersAccumulate(t *testing.T) {
	g0, h0 := Counts()
	AddGit()
	AddGH()
	AddGH()
	g1, h1 := Counts()
	if g1-g0 != 1 || h1-h0 != 2 {
		t.Fatalf("got git +%d gh +%d", g1-g0, h1-h0)
	}
}
