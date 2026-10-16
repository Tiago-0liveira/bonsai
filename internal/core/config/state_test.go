package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPrefsAgentGlyphsJSON(t *testing.T) {
	b, err := json.Marshal(Prefs{AgentGlyphs: "ascii"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"agent_glyphs":"ascii"`) {
		t.Fatalf("marshal = %s, want agent_glyphs key", b)
	}
	var p Prefs
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	if p.AgentGlyphs != "ascii" {
		t.Errorf("round trip AgentGlyphs = %q", p.AgentGlyphs)
	}
	b, _ = json.Marshal(Prefs{})
	if strings.Contains(string(b), "agent_glyphs") {
		t.Errorf("empty AgentGlyphs should be omitted: %s", b)
	}
}
