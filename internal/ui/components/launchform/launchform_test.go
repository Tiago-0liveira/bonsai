package launchform

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/providers/antigravity"
	"github.com/Tiago-0liveira/bonsai/internal/providers/claude"
	"github.com/Tiago-0liveira/bonsai/internal/providers/codex"
)

func testModels() []agents.ModelOption {
	return []agents.ModelOption{
		{ID: "opus", Label: "Opus", Description: "most capable", Efforts: []string{"low", "medium", "high", "xhigh"}, DefaultEffort: "high"},
		{ID: "sonnet", Label: "Sonnet", Description: "balanced", Efforts: []string{"low", "high"}, DefaultEffort: "low", Default: true},
		{ID: "haiku", Label: "Haiku", Description: "fast", NoEffort: true},
	}
}

func claudeProfile() []agents.LaunchField {
	return agents.ProfileFieldsFrom((&claude.Provider{}).LaunchFields())
}

func codexLaunch() []agents.LaunchField { return (&codex.Provider{}).LaunchFields() }

func agyLaunch() []agents.LaunchField { return antigravity.New(nil, nil).LaunchFields() }

func press(f Form, keys ...string) Form {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "up":
			msg = tea.KeyMsg{Type: tea.KeyUp}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "left":
			msg = tea.KeyMsg{Type: tea.KeyLeft}
		case "right":
			msg = tea.KeyMsg{Type: tea.KeyRight}
		case " ":
			msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		f, _ = f.Update(msg)
	}
	return f
}

func view(f Form, w, h int) string { return ansi.Strip(f.View(w, h)) }

func TestClaudeProfilePickModelAndEffort(t *testing.T) {
	f := New(claudeProfile(), testModels(), nil, false)
	if f.Dirty() || len(f.Changes()) != 0 {
		t.Fatalf("fresh form changes = %v", f.Changes())
	}
	f = press(f, "enter")
	if !f.Editing() {
		t.Fatal("enter on model should open the list")
	}
	// Provider default, Opus, Sonnet (default), Haiku.
	f = press(f, "j", "enter")
	if f.Editing() {
		t.Fatal("pick should close the list")
	}
	if got := f.Values()["model"]; got != "opus" {
		t.Fatalf("model = %q, want opus", got)
	}
	// permission_mode, then effort.
	f = press(f, "j", "j", "enter", "j", "j", "j", "enter")
	if got := f.Values()["effort"]; got != "high" {
		t.Fatalf("effort = %q, want high", got)
	}
	ch := f.Changes()
	if len(ch) != 2 || *ch["model"] != "opus" || *ch["effort"] != "high" {
		t.Fatalf("changes = %v", ch)
	}
	out := view(press(f, "enter"), 80, 24)
	if !strings.Contains(out, "high (default)") {
		t.Fatalf("effort list should mark the model's default effort:\n%s", out)
	}
}

func TestChangesAgainstInitialAndReset(t *testing.T) {
	f := New(claudeProfile(), testModels(), map[string]string{"model": "opus", "effort": "low"}, false)
	f = press(f, "j", "j", "right")
	if got := f.Values()["effort"]; got != "medium" {
		t.Fatalf("effort = %q, want medium", got)
	}
	if ch := f.Changes(); len(ch) != 1 || *ch["effort"] != "medium" {
		t.Fatalf("changes = %v", ch)
	}
	f = press(f, "left")
	if f.Dirty() {
		t.Fatalf("back to initial should not be dirty: %v", f.Changes())
	}
	f = press(f, "r")
	ch := f.Changes()
	if v, ok := ch["effort"]; !ok || v != nil {
		t.Fatalf("reset should report nil for effort, got %v", ch)
	}
	if _, ok := f.Values()["effort"]; ok {
		t.Fatalf("reset effort still in values: %v", f.Values())
	}
}

func TestResetAll(t *testing.T) {
	f := New(claudeProfile(), testModels(), map[string]string{"model": "opus", "permission_mode": "plan", "effort": "low"}, false)
	f = f.ResetAll()
	ch := f.Changes()
	if len(ch) != 3 {
		t.Fatalf("changes = %v, want 3 resets", ch)
	}
	for id, v := range ch {
		if v != nil {
			t.Errorf("%s = %q, want nil", id, *v)
		}
	}
}

func TestPerModelEffortFollowsModel(t *testing.T) {
	f := New(claudeProfile(), testModels(), map[string]string{"model": "opus", "effort": "xhigh"}, false)
	// Sonnet has no xhigh: fall back to its default effort.
	f = press(f, "right") // opus -> sonnet
	if got := f.Values()["model"]; got != "sonnet" {
		t.Fatalf("model = %q", got)
	}
	if got := f.Values()["effort"]; got != "low" {
		t.Fatalf("effort after switching to sonnet = %q, want low", got)
	}
	// Haiku has no effort control: the field disappears.
	f = press(f, "right")
	out := view(f, 80, 24)
	if strings.Contains(out, "Effort") {
		t.Fatalf("effort should be hidden for a NoEffort model:\n%s", out)
	}
	if _, ok := f.Values()["effort"]; ok {
		t.Fatalf("hidden effort leaked into values: %v", f.Values())
	}
	if v, ok := f.Changes()["effort"]; !ok || v != nil {
		t.Fatalf("hidden effort should be reset, changes = %v", f.Changes())
	}
	// Back to a model with efforts: field returns, unset.
	f = press(f, "left")
	if !strings.Contains(view(f, 80, 24), "Effort") {
		t.Fatal("effort should return for sonnet")
	}
}

func TestPerModelFallsBackToOptionsForUnknownModel(t *testing.T) {
	f := New(claudeProfile(), testModels(), map[string]string{"model": "my-custom"}, false)
	f = press(f, "j", "j", "enter")
	out := view(f, 80, 24)
	for _, want := range []string{"low", "medium", "high"} {
		if !strings.Contains(out, want) {
			t.Errorf("fallback options missing %q:\n%s", want, out)
		}
	}
}

func TestCodexDangerHelpAndPresetDefault(t *testing.T) {
	f := New(codexLaunch(), nil, nil, false)
	if got := f.Values()["permissions"]; got != "auto" {
		t.Fatalf("seeded permissions = %q, want auto", got)
	}
	if strings.Contains(view(f, 80, 24), "without asking") {
		t.Fatal("help for a safe option should not show")
	}
	f = press(f, "right") // full-access
	out := view(f, 80, 24)
	if !strings.Contains(out, "Full access") || !strings.Contains(out, "can change any file and use the network without asking") {
		t.Fatalf("danger help missing:\n%s", out)
	}
	// Inside the open list the highlighted danger option shows its help too.
	f = press(f, "enter", "j")
	if out = view(f, 80, 24); !strings.Contains(out, "without a sandbox") {
		t.Fatalf("open list should show the highlighted danger help:\n%s", out)
	}
	f = press(f, "esc")
	if f.Editing() {
		t.Fatal("esc should close the list")
	}
	if got := f.Values()["permissions"]; got != "full-access" {
		t.Fatalf("esc must not change the value, got %q", got)
	}
}

func TestOpenListPickAndCancel(t *testing.T) {
	f := New(codexLaunch(), nil, nil, false)
	f = press(f, "enter", "k", "k", "k", "esc")
	if f.Editing() || f.Values()["permissions"] != "auto" {
		t.Fatalf("cancel changed state: editing=%v values=%v", f.Editing(), f.Values())
	}
	f = press(f, "enter", "k", "enter")
	if got := f.Values()["permissions"]; got != "read-only" {
		t.Fatalf("permissions = %q, want read-only", got)
	}
	// j/k inside an open list must not move between fields.
	f = press(f, "enter", "j", "j", "j", "j", "j")
	if !f.Editing() {
		t.Fatal("list should stay open")
	}
	f = press(f, "enter")
	if got := f.Values()["permissions"]; got != "bypass" {
		t.Fatalf("permissions = %q, want bypass", got)
	}
}

func TestAntigravityToggle(t *testing.T) {
	f := New(agyLaunch(), nil, nil, false)
	f = press(f, "j")
	if out := view(f, 80, 24); !strings.Contains(out, "Off") || !strings.Contains(out, "[space]") {
		t.Fatalf("toggle row:\n%s", out)
	}
	f = press(f, " ")
	if got := f.Values()["full_access"]; got != "true" {
		t.Fatalf("full_access = %q", got)
	}
	if out := view(f, 80, 24); !strings.Contains(out, "On") {
		t.Fatalf("toggle should read On:\n%s", out)
	}
	if ch := f.Changes(); len(ch) != 1 || *ch["full_access"] != "true" {
		t.Fatalf("changes = %v", ch)
	}
	f = press(f, "enter")
	if f.Dirty() || f.Values()["full_access"] != "false" {
		t.Fatalf("enter should toggle back: %v %v", f.Values(), f.Changes())
	}
	f = press(f, "right", "r")
	if v, ok := f.Changes()["full_access"]; ok && v != nil {
		t.Fatalf("reset toggle changes = %v", f.Changes())
	}
}

func TestUnknownKindIsReadOnly(t *testing.T) {
	fields := []agents.LaunchField{{ID: "mystery", Label: "Mystery", Kind: "slider"}}
	f := New(fields, nil, map[string]string{"mystery": "7"}, false)
	f = press(f, "enter", " ", "right", "r", "j", "k")
	if f.Editing() || f.Dirty() || f.Values()["mystery"] != "7" {
		t.Fatalf("read-only row changed: %v %v", f.Values(), f.Changes())
	}
	if out := view(f, 40, 6); !strings.Contains(out, "Mystery") || !strings.Contains(out, "7") {
		t.Fatalf("read-only row missing:\n%s", out)
	}
}

func TestBlurredIgnoresKeys(t *testing.T) {
	f := New(codexLaunch(), nil, nil, false).Focus(false)
	f = press(f, "right", "enter")
	if f.Dirty() || f.Editing() {
		t.Fatal("blurred form handled keys")
	}
	f = press(f.Focus(true), "right")
	if !f.Dirty() {
		t.Fatal("focused form ignored keys")
	}
}

func TestASCIIMarker(t *testing.T) {
	if out := view(New(codexLaunch(), nil, nil, true), 40, 8); !strings.Contains(out, "[v]") || strings.Contains(out, "▼") {
		t.Fatalf("ascii marker:\n%s", out)
	}
	if out := view(New(codexLaunch(), nil, nil, false), 40, 8); !strings.Contains(out, "[▼]") {
		t.Fatalf("unicode marker:\n%s", out)
	}
}

func fieldsN(n int) []agents.LaunchField {
	out := make([]agents.LaunchField, n)
	for i := range out {
		out[i] = agents.LaunchField{ID: fmt.Sprintf("f%d", i), Label: fmt.Sprintf("Field %d", i), Kind: agents.LaunchFieldSelect,
			Options: []agents.FieldOption{{Value: "a", Label: "Alpha"}, {Value: "b", Label: "Beta"}}}
	}
	return out
}

func TestOtherOptionsHeadingAndScroll(t *testing.T) {
	f := New(fieldsN(6), nil, nil, false)
	out := view(f, 40, 30)
	if !strings.Contains(out, otherHeading) {
		t.Fatalf("heading missing:\n%s", out)
	}
	if strings.Index(out, otherHeading) > strings.Index(out, "Field 3") || strings.Index(out, "Field 2") > strings.Index(out, otherHeading) {
		t.Fatalf("heading must sit between the third and fourth field:\n%s", out)
	}
	if view(New(fieldsN(3), nil, nil, false), 40, 30) == "" || strings.Contains(view(New(fieldsN(3), nil, nil, false), 40, 30), otherHeading) {
		t.Fatal("three fields should have no sub-heading")
	}
	f = press(f, "j", "j", "j", "j", "j")
	out = view(f, 40, 6)
	if !strings.Contains(out, "Field 5") {
		t.Fatalf("focused field scrolled out of view:\n%s", out)
	}
	if strings.Contains(out, "Field 0") {
		t.Fatalf("expected the top to scroll away:\n%s", out)
	}
	f = press(f, "enter")
	if out = view(f, 40, 7); !strings.Contains(out, "Alpha") || !strings.Contains(out, "Beta") {
		t.Fatalf("open list must be visible:\n%s", out)
	}
}

func TestViewInvariants(t *testing.T) {
	forms := map[string]func(ascii bool) Form{
		"claude":      func(a bool) Form { return New(claudeProfile(), testModels(), map[string]string{"model": "opus"}, a) },
		"codex":       func(a bool) Form { return New(codexLaunch(), nil, nil, a) },
		"antigravity": func(a bool) Form { return New(agyLaunch(), testModels(), nil, a) },
		"many":        func(a bool) Form { return New(fieldsN(8), nil, nil, a) },
	}
	for name, mk := range forms {
		for _, w := range []int{40, 80} {
			for _, h := range []int{1, 6, 24} {
				for _, ascii := range []bool{false, true} {
					f := mk(ascii)
					for step := 0; step < 4; step++ {
						for _, open := range []bool{false, true} {
							cur := f
							if open {
								cur = press(cur, "enter")
							}
							lines := strings.Split(cur.View(w, h), "\n")
							if len(lines) != h {
								t.Fatalf("%s w=%d h=%d open=%v: %d lines", name, w, h, open, len(lines))
							}
							for _, l := range lines {
								if got := ansi.StringWidth(l); got > w {
									t.Fatalf("%s w=%d h=%d open=%v: line width %d: %q", name, w, h, open, got, l)
								}
							}
						}
						f = press(f, "j")
					}
				}
			}
		}
	}
	if got := New(codexLaunch(), nil, nil, false).View(40, 0); got != "" {
		t.Fatalf("h=0 view = %q", got)
	}
}

func TestModelListShowsDescriptionAndDefault(t *testing.T) {
	f := press(New(claudeProfile(), testModels(), nil, false), "enter")
	out := view(f, 80, 24)
	for _, want := range []string{"Provider default", "Opus", "most capable", "Sonnet (default)", "Haiku"} {
		if !strings.Contains(out, want) {
			t.Errorf("model list missing %q:\n%s", want, out)
		}
	}
}

func TestAtLastTracksCursor(t *testing.T) {
	f := New(fieldsN(3), nil, map[string]string{}, false).Focus(true)
	if f.AtLast() {
		t.Fatal("cursor starts on the first field, not the last")
	}
	f = press(f, "down", "down")
	if !f.AtLast() {
		t.Fatal("cursor on the last field must report AtLast")
	}
	if f = press(f, "up"); f.AtLast() {
		t.Fatal("moving up must leave the last field")
	}
	if f = press(New(fieldsN(1), nil, map[string]string{}, false)); !f.AtLast() {
		t.Fatal("a single field is the last one")
	}
}

func TestStoredValueTheProviderNoLongerOffersIsNamed(t *testing.T) {
	f := New(claudeProfile(), testModels(), map[string]string{"model": "retired-model"}, false)
	if out := ansi.Strip(f.View(60, 20)); !strings.Contains(out, "retired-model (unlisted)") {
		t.Errorf("unlisted value passes as a normal choice:\n%s", out)
	}
	if f.Dirty() {
		t.Error("showing an unlisted value must not make a draft")
	}
}
