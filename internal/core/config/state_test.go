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

func TestPrefsShellInitJSON(t *testing.T) {
	b, err := json.Marshal(Prefs{DepthGlyph: "B", ShellPrompt: "off"})
	if err != nil {
		t.Fatal(err)
	}
	var p Prefs
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	if p.DepthGlyph != "B" || p.ShellPrompt != "off" {
		t.Errorf("round trip = %+v", p)
	}
	b, _ = json.Marshal(Prefs{})
	if strings.Contains(string(b), "depth_glyph") || strings.Contains(string(b), "shell_prompt") {
		t.Errorf("empty fields should be omitted: %s", b)
	}
}
