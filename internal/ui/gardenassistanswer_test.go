package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/assist"
)

func answerEnv(t *testing.T, prefs config.Prefs, w, h int) (Model, *assistSession) {
	t.Helper()
	m, _ := assistEnv(t, prefs, w, h, true)
	m = newIdea(t, m, "Slow export")
	m = askQuestions(t, m)
	return m, assistSession0(m)
}

func TestAssistTextZoneTypesDigitsAndNeverQuits(t *testing.T) {
	m, s := answerEnv(t, config.Prefs{}, 120, 35)
	m = apress(t, m, "enter", "4") // options, then Write my own: the text field
	if s.zone != zoneText {
		t.Fatalf("zone %d", s.zone)
	}
	for _, k := range []string{"1", "2", "9", "q", "?", " "} {
		var cmd tea.Cmd
		m, cmd = send(t, m, akey(k))
		if isQuit(cmd) {
			t.Fatalf("%q quit from the text field", k)
		}
	}
	if got := s.ta.Value(); got != "129q? " {
		t.Fatalf("typed %q", got)
	}
	if a := s.answer(s.questions[0]); a.sel != selOwn || a.text != "129q? " {
		t.Fatalf("answer %+v", a)
	}
}

func TestAssistOptionsSelectByDigitAndSpace(t *testing.T) {
	m, s := answerEnv(t, config.Prefs{}, 120, 35)
	q := s.questions[0]
	m = apress(t, m, "enter")
	if s.zone != zoneOptions {
		t.Fatalf("enter on the rail: zone %d", s.zone)
	}
	m = apress(t, m, "down", "down", " ")
	if a := s.answer(q); a.sel != selChoice || a.pick != 2 || s.zone != zoneText {
		t.Fatalf("space: %+v zone %d", a, s.zone)
	}
	m = apress(t, m, "esc", "2")
	if a := s.answer(q); a.sel != selChoice || a.pick != 1 {
		t.Fatalf("digit 2: %+v", a)
	}
	m = apress(t, m, "esc", "5") // I don't know
	if a := s.answer(q); a.sel != selUnknown || s.zone != zoneText {
		t.Fatalf("digit 5: %+v zone %d", a, s.zone)
	}
	m = apress(t, m, "esc", "6") // Skip stays in the options: there is no field
	if a := s.answer(q); a.sel != selSkip || s.zone != zoneOptions {
		t.Fatalf("digit 6: %+v zone %d", a, s.zone)
	}
	mustNotContain(t, "skip", view(m), "Notes (optional)", "Your answer")
	m = apress(t, m, "9") // beyond the list: ignored
	if a := s.answer(q); a.sel != selSkip {
		t.Fatalf("digit 9 changed the answer: %+v", a)
	}
	// tab skips the text zone while Skip is selected.
	m = apress(t, m, "tab")
	if s.zone != zoneRail {
		t.Fatalf("tab from options with Skip: zone %d", s.zone)
	}
}

func TestAssistChoiceAndOwnAnswerShareTheText(t *testing.T) {
	m, s := answerEnv(t, config.Prefs{}, 120, 35)
	q := s.questions[0]
	m = apress(t, m, "enter", "2")
	m = typed(t, m, "only on files over 50 MB")
	list := s.answerList()[0]
	if list.State != assist.Answered || list.Choice != "An admin" || list.Text != "only on files over 50 MB" {
		t.Fatalf("choice with notes: %+v", list)
	}
	mustContain(t, "notes", view(m), "Notes (optional)")
	m = apress(t, m, "esc", "4") // Write my own: the notes become the answer
	if s.ta.Value() != "only on files over 50 MB" {
		t.Fatalf("switch to own lost the text: %q", s.ta.Value())
	}
	list = s.answerList()[0]
	if list.State != assist.Answered || list.Choice != "" || list.Text != "only on files over 50 MB" {
		t.Fatalf("own answer: %+v", list)
	}
	mustContain(t, "own", view(m), "Your answer")
	m = apress(t, m, "esc", "1") // back to a suggestion: the text is notes again
	if list = s.answerList()[0]; list.Choice != "A signed-in user" || list.Text != "only on files over 50 MB" {
		t.Fatalf("back to a choice: %+v", list)
	}
	m = apress(t, m, "esc", "5") // I don't know keeps notes
	if list = s.answerList()[0]; list.State != assist.Unknown || list.Choice != "" || list.Text != "only on files over 50 MB" {
		t.Fatalf("unknown with notes: %+v", list)
	}
	m = apress(t, m, "esc", "6") // Skip sends nothing but keeps the text for a later selection
	if list = s.answerList()[0]; list.State != assist.Skipped || list.Choice != "" || list.Text != "" {
		t.Fatalf("skip: %+v", list)
	}
	if a := s.answer(q); a.text != "only on files over 50 MB" {
		t.Fatalf("skip dropped the stored text: %+v", a)
	}
}

func TestAssistWriteMyOwnWithoutTextStaysOpen(t *testing.T) {
	m, s := answerEnv(t, config.Prefs{}, 120, 35)
	m = apress(t, m, "enter", "4")
	list := s.answerList()[0]
	if list.State != assist.Open || list.Text != "" || list.Choice != "" {
		t.Fatalf("empty own answer: %+v", list)
	}
	m = typed(t, m, "   ")
	if got := s.answerList()[0]; got.State != assist.Open || got.Text != "" {
		t.Fatalf("blank own answer: %+v", got)
	}
	m = typed(t, m, "x")
	if got := s.answerList()[0]; got.State != assist.Answered || got.Text != "x" {
		t.Fatalf("own answer: %+v", got)
	}
}

func TestAssistQuestionWithoutSuggestionsOpensOnTheTextField(t *testing.T) {
	m, s := answerEnv(t, config.Prefs{}, 120, 35)
	m = apress(t, m, "down", "enter") // Q2 has no suggestions
	if s.sel != 1 || s.zone != zoneText || !s.ta.Focused() {
		t.Fatalf("sel %d zone %d", s.sel, s.zone)
	}
	out := view(m)
	mustContain(t, "preselected", out, "(•) Write my own answer", "Your answer")
	m = typed(t, m, "PDF")
	if got := s.answerList()[1]; got.Text != "PDF" || got.State != assist.Answered {
		t.Fatalf("answer %+v", got)
	}
}

func TestAssistPageKeysMoveQuestionFromAnyZone(t *testing.T) {
	m, s := answerEnv(t, config.Prefs{}, 120, 35)
	m = apress(t, m, "pgdown")
	if s.sel != 1 || s.zone != zoneRail {
		t.Fatalf("rail pgdown: sel %d zone %d", s.sel, s.zone)
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	if s.sel != 0 || s.zone != zoneRail {
		t.Fatalf("rail pgup: sel %d zone %d", s.sel, s.zone)
	}
	m = apress(t, m, "enter", "4")
	m = typed(t, m, "keep")
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	if s.sel != 1 || s.zone != zoneText || s.ta.Value() != "" {
		t.Fatalf("text pgdown: sel %d zone %d value %q", s.sel, s.zone, s.ta.Value())
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	if s.sel != 0 || s.zone != zoneOptions {
		t.Fatalf("text pgup: sel %d zone %d", s.sel, s.zone)
	}
	m = apress(t, m, "enter") // the cursor sits on Write my own: select it again, text intact
	if s.ta.Value() != "keep" {
		t.Fatalf("answer text after paging: %q", s.ta.Value())
	}
	// ctrl+s drafts from the text field.
	m = apress(t, m, "ctrl+s")
	if s.stage != assistReview {
		t.Fatalf("ctrl+s from text: stage %d", s.stage)
	}
}

func TestAssistEscStepsBackOneZoneThenLeaves(t *testing.T) {
	m, s := answerEnv(t, config.Prefs{}, 120, 35)
	m = apress(t, m, "enter", "4")
	m = apress(t, m, "esc")
	if s.zone != zoneOptions {
		t.Fatalf("zone %d", s.zone)
	}
	m = apress(t, m, "esc")
	if s.zone != zoneRail || m.garden.drafts.stage != draftsAssist {
		t.Fatalf("zone %d", s.zone)
	}
	m = apress(t, m, "esc")
	if m.garden.drafts.stage == draftsAssist {
		t.Fatal("esc on the rail did not leave")
	}
}

func TestAssistTabCyclesZones(t *testing.T) {
	m, s := answerEnv(t, config.Prefs{}, 120, 35)
	var seen []assistZone
	for i := 0; i < 3; i++ {
		m = apress(t, m, "tab")
		seen = append(seen, s.zone)
	}
	if seen[0] != zoneOptions || seen[1] != zoneText || seen[2] != zoneRail {
		t.Fatalf("tab order %v", seen)
	}
	m = apress(t, m, "shift+tab")
	if s.zone != zoneText {
		t.Fatalf("shift+tab from the rail: %d", s.zone)
	}
}

func TestAssistRailGlyphs(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		prefs := config.Prefs{}
		if ascii {
			prefs.AgentGlyphs = "ascii"
		}
		m, s := answerEnv(t, prefs, 120, 35)
		s.questions = append(s.questions,
			assist.Question{ID: "g4", Round: 1, Text: "Fourth question"},
			assist.Question{ID: "g5", Round: 1, Text: "Fifth question"})
		s.answers = map[string]assistAnswer{
			"r1q1": {sel: selChoice, pick: 0},
			"r1q2": {sel: selOwn, text: "PDF"},
			"r1q3": {sel: selUnknown},
			"g4":   {sel: selSkip},
		}
		want := map[string]string{"●": "●", "✎": "✎", "·": "·"}
		if ascii {
			want = map[string]string{"●": "o", "✎": "w", "·": "."}
		}
		out := view(m)
		mustContain(t, "glyphs", out,
			want["●"]+"  Who triggers", want["✎"]+"  Which formats", "?  How large", "-  Fourth", want["·"]+"  Fifth")
		s.answers["r1q1"] = assistAnswer{sel: selChoice, pick: 0, text: "notes"}
		plus := want["●"] + "+ Who triggers"
		mustContain(t, "choice with notes", view(m), plus)
	}
}

// longQuestion is a question whose text and reason cannot fit a 80x24 panel next to the options and the field.
func longQuestion() assist.Question {
	return assist.Question{
		ID: "long", Round: 1, Area: "behavior",
		Text:        strings.Repeat("When the export fails halfway through a very large file, what exactly do you see on screen? ", 3),
		Why:         strings.Repeat("It decides whether the fix has to report partial failures to the person. ", 3),
		Suggestions: []string{"An error appears with a really long explanation that goes past the pane width for sure, yes", "It looks successful", "The download never starts"},
	}
}

func TestAssistAnswerPanelRendersAtEverySize(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {120, 35}} {
		for _, ascii := range []bool{false, true} {
			prefs := config.Prefs{}
			radio, rule := "(•)", "─────"
			if ascii {
				prefs.AgentGlyphs = "ascii"
				radio, rule = "(*)", "-----"
			}
			m, s := answerEnv(t, prefs, sz[0], sz[1])
			label := fmt.Sprintf("%dx%d ascii=%v", sz[0], sz[1], ascii)
			s.questions[0] = longQuestion()
			s.taFor = ""
			m = apress(t, m, "enter", "2") // a choice, so the field is the notes
			m = typed(t, m, "only over 50 MB")
			out := view(m)
			mustFit(t, label, m)
			mustContain(t, label, out, radio+" It looks successful", "( ) Write my own answer", "( ) I don't know", "( ) Skip for now",
				"Choose one", "Notes (optional)", "[only over 50 MB", rule)
			for _, line := range strings.Split(out, "\n") {
				if strings.Contains(line, "An error appears with a really") && strings.Contains(line, "pane width for sure") && sz[0] == 80 {
					t.Fatalf("option row not truncated at 80 columns: %q", line)
				}
			}
			// The question text scrolls when it does not fit; the options and the field stay.
			if sz[0] == 80 {
				before := view(m)
				m = apress(t, m, "esc", "ctrl+down", "ctrl+down")
				after := view(m)
				if before == after || s.qScroll == 0 {
					t.Fatalf("ctrl+down did not scroll the question (scroll %d)", s.qScroll)
				}
				mustContain(t, "after scrolling", after, "( ) Skip for now", "Notes (optional)")
				mustFit(t, "after scrolling", m)
			}
		}
	}
}

func TestAssistAnswerPanelFitsEveryQuestionState(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {120, 35}} {
		m, s := answerEnv(t, config.Prefs{}, sz[0], sz[1])
		s.questions = append(s.questions, longQuestion())
		for sel := range s.questions {
			s.sel = sel
			s.taFor = ""
			for _, a := range []assistAnswer{{}, {sel: selChoice, pick: 1, text: "note"}, {sel: selUnknown}, {sel: selSkip}} {
				s.answers = map[string]assistAnswer{s.questions[sel].ID: a}
				mustFit(t, "state", m)
				mustContain(t, "options", view(m), "Write my own answer", "Skip for now")
			}
		}
	}
}
