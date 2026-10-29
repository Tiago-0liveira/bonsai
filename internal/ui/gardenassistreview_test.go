package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/assist"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

func reviewProposal() assist.Proposal {
	return assist.Proposal{
		Title: "Faster export", Type: garden.TypeFeature,
		Summary:      "Exports finish within ten seconds.",
		Requirements: []assist.Item{{Text: "Stream rows", Basis: "stated"}},
		Notes:        []string{"Check the CSV size."}, Carried: 2, Dropped: 1,
	}
}

func noApproval(t *testing.T, fb interface{ Count(string) int }) {
	t.Helper()
	for _, c := range []string{"PreviewValidate", "Validate", "StartPlanner", "DecidePlan"} {
		if fb.Count(c) != 0 {
			t.Errorf("the assistant flow called %s", c)
		}
	}
}

// reviewEnv is the sample drafts with the proposal scripted and #143 (a bug with only a title) elaborated up to the
// review, so its empty sections give the diff something to show.
func reviewEnv(t *testing.T, prefs config.Prefs, w, h int) (Model, assistRig, editKey) {
	t.Helper()
	m, rig := assistEnv(t, prefs, w, h, true)
	rig.garden.AssistDraftView = gardenapi.AssistDraftView{Proposal: reviewProposal()}
	m = apress(t, m, "down", "enter", "i", "ctrl+d")
	s := m.garden.assist.open
	if s == nil || s.stage != assistReview || s.number != 143 {
		t.Fatalf("no review for #143\n%s", view(m))
	}
	return m, rig, s.key
}

func TestAssistReviewDefaultsAcceptUntouchedAndNothingAppliesBeforeE(t *testing.T) {
	m, rig, key := reviewEnv(t, config.Prefs{}, 120, 35)
	e := m.garden.drafts.editors[key]
	before := e.content()
	out := view(m)
	mustContain(t, "review", out, "~ Summary", "= Constraints", "~ Requirements", "+ Exports finish within ten seconds.", "Note: Check the CSV size.",
		"2 open questions added for unanswered questions", "1 item dropped", "Decision: accept the proposal")
	mustFit(t, "review", m)
	// Moving and deciding changes no text.
	m = apress(t, m, "down", "down", "k", "up", "a")
	if e.content() != before {
		t.Fatal("deciding changed the editor")
	}
	// Esc applies nothing and keeps the proposal one key away.
	m = apress(t, m, "esc")
	if s := m.garden.assist.open; s.stage != assistSetup || e.content() != before {
		t.Fatalf("esc: stage %d", s.stage)
	}
	m = apress(t, m, "p")
	if m.garden.assist.open.stage != assistReview {
		t.Fatal("p did not return to the proposal")
	}
	m = apress(t, m, "e")
	if m.garden.drafts.stage != draftsEditor || m.garden.drafts.edit != e || m.garden.assist.open != nil {
		t.Fatalf("e: stage %d", m.garden.drafts.stage)
	}
	text := e.content()
	if !strings.Contains(text, "Exports finish within ten seconds.") || !strings.Contains(text, "- Stream rows") || !e.dirty() {
		t.Fatalf("decisions not applied:\n%s", text)
	}
	noWrites(t, rig.garden)
	// Saving the editor ends the session through the ordinary save flow.
	m = apress(t, m, "ctrl+s", "right", "enter")
	if len(m.garden.assist.sessions) != 0 || rig.garden.Count("Save") != 1 {
		t.Fatalf("session after save: %d, saves %d", len(m.garden.assist.sessions), rig.garden.Count("Save"))
	}
}

func TestAssistReviewKeepsSectionsEditedWhileTheAssistantWorked(t *testing.T) {
	m, rig := assistEnv(t, config.Prefs{}, 120, 35, true)
	rig.garden.AssistDraftView = gardenapi.AssistDraftView{Proposal: reviewProposal()}
	m = apress(t, m, "down", "enter", "i")
	key := m.garden.assist.open.key
	m, call := ahold(t, m, "ctrl+d")
	// The user edits the Summary in the editor while the request is in the air.
	m.garden.drafts.editors[key].setSection(0, "My own summary.")
	m = plannerRun(t, m, call)
	out := view(m)
	mustContain(t, "edited", out, "! Summary", "~ Requirements", "Decision: keep your text", "you edited this section while the assistant worked")
	m = apress(t, m, "e")
	e := m.garden.drafts.editors[key]
	if e.secs[0].text != "My own summary." || !strings.Contains(e.secs[3].text, "Stream rows") {
		t.Fatalf("sections: %q / %q", e.secs[0].text, e.secs[3].text)
	}
	// Back in the review, a explicitly takes the proposal over the edit; A would leave it alone.
	m = apress(t, m, "i")
	m = apress(t, m, "A")
	if m.garden.assist.open.review.choice[0] != 0 {
		t.Fatal("A decided an edited section")
	}
	m = apress(t, m, "a", "e")
	if e.secs[0].text != "Exports finish within ten seconds." {
		t.Fatalf("a did not accept: %q", e.secs[0].text)
	}
}

func TestAssistReviewNeverEmptiesWrittenSectionsByDefault(t *testing.T) {
	m, _, key := reviewEnv(t, config.Prefs{}, 120, 35)
	s := m.garden.assist.open
	// Constraints holds the person's text, unchanged since the request; the proposal has none.
	s.review.input[5], s.review.proposed[5] = "Keep the CSV format.", ""
	m.garden.drafts.editors[key].setSection(5, "Keep the CSV format.")
	if sec := m.assistSections(s)[5]; sec.accept || !sec.clears {
		t.Fatalf("emptying section: accept %v clears %v", sec.accept, sec.clears)
	}
	m = apress(t, m, "A")
	if s.review.choice[5] != 0 {
		t.Fatal("A accepted a proposal that empties a written section")
	}
	m = apress(t, m, "e")
	if got := m.garden.drafts.editors[key].secs[5].text; got != "Keep the CSV format." {
		t.Fatalf("constraints after apply: %q", got)
	}
}

func TestAssistRefineAndMoreSendTheWorkingDraftAndKeepEveryAnswer(t *testing.T) {
	m, rig := assistEnv(t, config.Prefs{}, 120, 35, true)
	rig.garden.AssistDraftView = gardenapi.AssistDraftView{Proposal: reviewProposal()}
	m = apress(t, m, "down", "enter", "i", "ctrl+s")
	m = apress(t, m, "enter", "4")
	m = typed(t, m, "Admins")
	m = apress(t, m, "esc", "esc", "down", "enter", "esc", "3", "ctrl+s")
	s := m.garden.assist.open
	key := s.key
	if s.stage != assistReview {
		t.Fatalf("stage %d", s.stage)
	}
	e := m.garden.drafts.editors[key]
	before := e.content()

	m = apress(t, m, "r")
	m = typed(t, m, "shorter please")
	m = apress(t, m, "esc")
	if s.refine || e.content() != before || len(rig.garden.AssistDraftRequests) != 1 {
		t.Fatal("Esc did not cancel the refinement")
	}
	m = apress(t, m, "r")
	m = typed(t, m, "shorter please")
	m = apress(t, m, "enter")
	if len(rig.garden.AssistDraftRequests) != 2 {
		t.Fatalf("%d draft requests", len(rig.garden.AssistDraftRequests))
	}
	r := rig.garden.AssistDraftRequests[1]
	if r.Feedback != "shorter please" || r.Draft != e.content() || !strings.Contains(r.Draft, "Exports finish within ten seconds.") || r.Draft == before {
		t.Fatalf("refine request: feedback %q\n%s", r.Feedback, r.Draft)
	}
	if len(r.Answers) != 3 || r.Answers[0].Text != "Admins" || r.Answers[1].State != assist.Skipped {
		t.Fatalf("answers lost: %+v", r.Answers)
	}
	// More questions send the same working draft and every answer.
	rig.garden.AssistQuestionsView = gardenapi.AssistQuestionsView{Provider: "claude", Set: assistSet(2, "Which locale for dates?")}
	m = apress(t, m, "m")
	q := rig.garden.AssistQuestionsRequests[len(rig.garden.AssistQuestionsRequests)-1]
	if q.Round != 2 || q.Draft != e.content() || len(q.Answers) != 3 || q.Answers[0].Text != "Admins" {
		t.Fatalf("more request: round %d answers %+v", q.Round, q.Answers)
	}
	if s := m.garden.assist.open; s.stage != assistQuestions || len(s.questions) != 4 {
		t.Fatalf("round two: stage %d", s.stage)
	}
	noApproval(t, rig.garden)
}

func TestAssistApplyNeverTouchesExtraSections(t *testing.T) {
	m, rig := assistEnv(t, config.Prefs{}, 120, 35, true)
	rig.garden.AssistDraftView = gardenapi.AssistDraftView{Proposal: reviewProposal()}
	m = apress(t, m, "down", "enter", "i")
	key := m.garden.assist.open.key
	e := m.garden.drafts.editors[key]
	e.secs = append(e.secs, edSection{title: "Rollout", text: "Ship behind a flag."})
	e.orig = append(e.orig, "")
	m = apress(t, m, "ctrl+d", "A", "e")
	if len(e.secs) != spec7+1 || e.secs[spec7].text != "Ship behind a flag." || !strings.Contains(e.content(), "Ship behind a flag.") {
		t.Fatalf("the extra section changed: %+v", e.secs)
	}
}

const spec7 = 7

func TestAssistClosedCleanEditorIsReadAgainBeforeApplying(t *testing.T) {
	m, rig := assistEnv(t, config.Prefs{}, 120, 35, true)
	rig.garden.AssistDraftView = gardenapi.AssistDraftView{Proposal: reviewProposal()}
	m = apress(t, m, "down", "enter", "i", "ctrl+d")
	key := m.garden.assist.open.key
	delete(m.garden.drafts.editors, key) // what closing a clean editor does
	docs := rig.garden.Count("Document")
	m = apress(t, m, "e")
	e := m.garden.drafts.editors[key]
	if e == nil || rig.garden.Count("Document") != docs+1 || m.garden.drafts.stage != draftsEditor || !strings.Contains(e.content(), "Stream rows") {
		t.Fatal("the editor was not recreated and applied")
	}
}

func ideaReview(t *testing.T, w, h int) (Model, assistRig) {
	t.Helper()
	m, rig := assistEnv(t, config.Prefs{}, w, h, true)
	rig.garden.AssistDraftView = gardenapi.AssistDraftView{Proposal: reviewProposal()}
	m = newIdea(t, m, "Slow export")
	m = apress(t, m, "ctrl+d", "e")
	e := m.garden.drafts.edit
	if e == nil || e.key.issue != 0 || e.title != "Faster export" || !e.dirty() || m.garden.drafts.stage != draftsEditor {
		t.Fatalf("no idea editor\n%s", view(m))
	}
	return m, rig
}

func callOrder(fb interface{ Calls() []string }, names ...string) []string {
	var out []string
	for _, c := range fb.Calls() {
		for _, n := range names {
			if c == n {
				out = append(out, c)
			}
		}
	}
	return out
}

func TestAssistNewIdeaCreatesOneIssueThenOneRevision(t *testing.T) {
	m, rig := ideaReview(t, 120, 35)
	if rig.garden.Count("Capture") != 0 {
		t.Fatal("capture before the save")
	}
	m = apress(t, m, "ctrl+s")
	d := m.garden.drafts
	if d.stage != draftsForm || d.form.title.Value() != "Faster export" || d.form.desc.Value() != "Slow export" || captureTypes[d.form.typ] != garden.TypeFeature {
		t.Fatalf("form: stage %d title %q desc %q", d.stage, d.form.title.Value(), d.form.desc.Value())
	}
	m = apress(t, m, "enter") // Preview
	if m.garden.drafts.stage != draftsPreview {
		t.Fatalf("stage %d\n%s", m.garden.drafts.stage, view(m))
	}
	mustContain(t, "confirmation", flat(view(m)), "then saves your specification as its first revision", "Create and save", "append the first specification revision")
	if rig.garden.Count("Capture") != 0 {
		t.Fatal("capture before the confirmation")
	}
	m = apress(t, m, "right", "enter")
	if got := callOrder(rig.garden, "Capture", "Document", "PreviewSave", "Save"); strings.Join(got, ",") != "Capture,Document,PreviewSave,Save" {
		t.Fatalf("call order %v", got)
	}
	if len(rig.garden.Captured) != 1 || rig.garden.Captured[0].Request.Description != "Slow export" || rig.garden.Captured[0].Request.Title != "Faster export" {
		t.Fatalf("capture: %+v", rig.garden.Captured)
	}
	if len(rig.garden.Saved) != 1 || rig.garden.Saved[0].IssueID != 9001 || !strings.Contains(rig.garden.Saved[0].Content, "Exports finish within ten seconds.") {
		t.Fatalf("save: %+v", rig.garden.Saved)
	}
	d = m.garden.drafts
	if d.stage != draftsList || d.selected != 9001 || len(d.editors) != 0 || len(m.garden.assist.sessions) != 0 || d.idea.active {
		t.Fatalf("after: stage %d selected %d editors %d sessions %d", d.stage, d.selected, len(d.editors), len(m.garden.assist.sessions))
	}
	mustContain(t, "done", view(m), "Created #150 as a Draft")
	noApproval(t, rig.garden)
	for _, c := range []string{"PreviewValidate", "Validate", "StartPlanner", "DecidePlan"} {
		if rig.garden.Count(c) != 0 {
			t.Fatal(c)
		}
	}
}

func TestAssistNewIdeaCaptureUnknownKeepsTheEditorAndContinuesAfterReconcile(t *testing.T) {
	m, rig := ideaReview(t, 120, 35)
	rig.garden.CapturedView = &gardenapi.CaptureView{State: gardenapi.CaptureUnknown, Err: &gardenapi.Error{Code: gardenapi.CodeCaptureUnknown, Message: "GitHub did not confirm."}}
	m = apress(t, m, "ctrl+s", "enter", "right", "enter")
	d := m.garden.drafts
	if d.stage != draftsResult || d.editors[m.editKeyFor(0)] == nil || rig.garden.Count("Save") != 0 {
		t.Fatalf("unknown: stage %d", d.stage)
	}
	m = apress(t, m, "enter") // Reconcile
	if rig.garden.Count("ReconcileCapture") != 1 || rig.garden.Count("Capture") != 1 || rig.garden.Count("Save") != 1 || len(m.garden.drafts.editors) != 0 {
		t.Fatalf("after reconcile: %v", rig.garden.Calls())
	}
	if m.garden.drafts.stage != draftsList || m.garden.drafts.selected != 9001 {
		t.Fatalf("stage %d selected %d", m.garden.drafts.stage, m.garden.drafts.selected)
	}
}

func TestAssistNewIdeaConflictAfterCaptureStopsOnTheStandardScreen(t *testing.T) {
	m, rig := ideaReview(t, 120, 35)
	rig.garden.PreviewConflict = &gardenapi.ConflictView{IssueID: 9001, Remote: []gardenapi.RemoteRevision{{Hash: "r1", Content: "## Summary\nElsewhere", Source: "Bonsai/GitHub comment"}}}
	m = apress(t, m, "ctrl+s", "enter", "right", "enter")
	if m.garden.drafts.stage != draftsConflict || rig.garden.Count("Capture") != 1 || rig.garden.Count("Save") != 0 {
		t.Fatalf("stage %d, calls %v", m.garden.drafts.stage, rig.garden.Calls())
	}
	if e := m.garden.drafts.editors[m.editKeyFor(9001)]; e == nil || !strings.Contains(e.content(), "Exports finish") {
		t.Fatal("the idea's text was lost on the conflict")
	}
}

func TestAssistNewIdeaEscFromTheFormAndDiscardKeepNothingBehind(t *testing.T) {
	m, rig := ideaReview(t, 120, 35)
	m = apress(t, m, "ctrl+s", "esc")
	if m.garden.drafts.stage != draftsEditor || rig.garden.Count("Capture") != 0 {
		t.Fatalf("stage %d", m.garden.drafts.stage)
	}
	// The editor is always unsaved: Esc asks, and Discard removes it with the session.
	m = apress(t, m, "esc")
	if !m.garden.drafts.edit.discard {
		t.Fatal("no discard question")
	}
	m = apress(t, m, "right", "enter")
	if len(m.garden.drafts.editors) != 0 || len(m.garden.assist.sessions) != 0 || m.garden.drafts.idea.active {
		t.Fatal("discard left state behind")
	}
}

func TestAssistReviewRendersAtEverySizeAndGlyphSet(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {120, 35}} {
		for _, ascii := range []bool{false, true} {
			label := fmt.Sprintf("%dx%d ascii=%v", sz[0], sz[1], ascii)
			prefs := config.Prefs{}
			if ascii {
				prefs.AgentGlyphs = "ascii"
			}
			m, _, _ := reviewEnv(t, prefs, sz[0], sz[1])
			out := view(m)
			mustContain(t, label, out, "~ Summary", "= Constraints", "AI proposal", "Exports finish")
			mustContain(t, label, flat(out), "2 open questions added for unanswered questions")
			mustFit(t, label, m)
			m = apress(t, m, "r")
			mustContain(t, label+" refine", view(m), "Refine:")
			mustFit(t, label+" refine", m)
			m = apress(t, m, "esc", "down", "down", "down")
			mustFit(t, label+" requirements", m)
			if ascii {
				mustNotContain(t, label, view(m), "✓", "▸", "‹", "…")
			}
		}
	}
}

func suggestionProposal() assist.Proposal {
	p := reviewProposal()
	p.Requirements = append(p.Requirements, assist.Item{Text: "Cap exports at 50 MB", Basis: "suggested"})
	p.AcceptanceCriteria = []assist.Item{{Text: "A failed export shows an error", Basis: "suggested"}}
	p.Open = []string{"Who pays for the export?", "Which month does the cap reset?"}
	p.Carried = 0 // both lines were added by the assistant, none carried from skipped answers
	return p
}

func suggestionEnv(t *testing.T, prefs config.Prefs, w, h int, p assist.Proposal) (Model, assistRig, editKey) {
	t.Helper()
	m, rig := assistEnv(t, prefs, w, h, true)
	rig.garden.AssistDraftView = gardenapi.AssistDraftView{Proposal: p}
	m = apress(t, m, "down", "enter", "i", "ctrl+d")
	s := m.garden.assist.open
	if s == nil || s.stage != assistReview {
		t.Fatalf("no review\n%s", view(m))
	}
	return m, rig, s.key
}

func TestAssistReviewSuggestionsAreConfirmedOneByOne(t *testing.T) {
	m, rig, key := suggestionEnv(t, config.Prefs{}, 120, 35, suggestionProposal())
	s := m.garden.assist.open
	r := s.review
	if strings.Contains(r.proposed[3], "Cap exports") || strings.Contains(r.proposed[4], "failed export") {
		t.Fatalf("unconfirmed suggestions are proposed: %q / %q", r.proposed[3], r.proposed[4])
	}
	mustContain(t, "row", view(m), "? Suggestions 2")
	m = apress(t, m, "down", "down", "down", "down", "down", "down", "down")
	if !r.onSuggestions() {
		t.Fatalf("sel %d", r.sel)
	}
	mustContain(t, "pane", view(m), "[ ] Requirements: Cap exports at 50 MB", "[ ] Acceptance criteria: A failed export shows an error")
	m = apress(t, m, " ")
	if !strings.Contains(r.proposed[3], "Cap exports at 50 MB") || strings.Contains(r.proposed[4], "failed export") {
		t.Fatalf("confirming one changed the wrong sections: %q / %q", r.proposed[3], r.proposed[4])
	}
	mustContain(t, "confirmed", view(m), "[x] Requirements: Cap exports at 50 MB")
	m = apress(t, m, "down", " ", " ") // second suggestion on, then off again
	if strings.Contains(r.proposed[4], "failed export") {
		t.Fatalf("toggling twice left it confirmed: %q", r.proposed[4])
	}
	m = apress(t, m, "e")
	e := m.garden.drafts.editors[key]
	got := e.content()
	if !strings.Contains(got, "Cap exports at 50 MB") || strings.Contains(got, "A failed export shows an error") {
		t.Fatalf("applied draft:\n%s", got)
	}
	noApproval(t, rig.garden)
}

func TestAssistReviewOpenRoundIsLocalAndFeedsTheNextDraft(t *testing.T) {
	m, rig, _ := suggestionEnv(t, config.Prefs{}, 120, 35, suggestionProposal())
	asked := len(rig.garden.AssistQuestionsRequests)
	m = apress(t, m, "o")
	s := m.garden.assist.open
	if s.stage != assistQuestions || len(s.questions) != 2 || s.round != 1 || s.sel != 0 {
		t.Fatalf("stage %d questions %d round %d sel %d", s.stage, len(s.questions), s.round, s.sel)
	}
	if q := s.questions[0]; q.Text != "Who pays for the export?" || len(q.Suggestions) != 0 || q.Round != 1 {
		t.Fatalf("question %+v", q)
	}
	if len(rig.garden.AssistQuestionsRequests) != asked {
		t.Fatal("o made an AI call")
	}
	if s.zone != zoneText || !s.ta.Focused() {
		t.Fatalf("open round is not ready to type: zone %d", s.zone)
	}
	m = typed(t, m, "The requester")
	m = apress(t, m, "pgdown")
	m = typed(t, m, "March")
	before := len(rig.garden.AssistDraftRequests)
	m = apress(t, m, "ctrl+s")
	if len(rig.garden.AssistDraftRequests) != before+1 {
		t.Fatalf("no draft request after the round\n%s", view(m))
	}
	dr := rig.garden.AssistDraftRequests[before]
	if len(dr.Answers) != 2 || dr.Answers[0].Text != "The requester" || dr.Answers[0].State != assist.Answered ||
		dr.Answers[0].Question != "Who pays for the export?" || dr.Answers[1].Text != "March" {
		t.Fatalf("answers %+v", dr.Answers)
	}
	noApproval(t, rig.garden)
}

func TestAssistReviewOpenRoundCapsAtTenAndKeepsTheSessionLimit(t *testing.T) {
	p := reviewProposal()
	for i := 0; i < 12; i++ {
		p.Open = append(p.Open, fmt.Sprintf("Open question number %d?", i+1))
	}
	m, _, _ := suggestionEnv(t, config.Prefs{}, 120, 35, p)
	m = apress(t, m, "o")
	s := m.garden.assist.open
	if len(s.questions) != maxOpenRound || !strings.Contains(s.notice, "2 more open questions") {
		t.Fatalf("%d questions, notice %q", len(s.questions), s.notice)
	}

	m, _, _ = suggestionEnv(t, config.Prefs{}, 120, 35, suggestionProposal())
	s = m.garden.assist.open
	for i := 0; i < assist.MaxSessionQuestions; i++ {
		s.questions = append(s.questions, assist.Question{ID: fmt.Sprintf("f%d", i), Round: 1, Text: "Filler"})
	}
	m = apress(t, m, "o")
	if s.stage != assistReview || !strings.Contains(s.notice, "most it allows") {
		t.Fatalf("stage %d notice %q", s.stage, s.notice)
	}
}

func TestAssistReviewRendersSuggestionsRowAndOpenNotice(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {120, 35}} {
		for _, ascii := range []bool{false, true} {
			prefs := config.Prefs{}
			if ascii {
				prefs.AgentGlyphs = "ascii"
			}
			label := fmt.Sprintf("%dx%d ascii=%v", sz[0], sz[1], ascii)
			m, _, _ := suggestionEnv(t, prefs, sz[0], sz[1], suggestionProposal())
			mustContain(t, label, view(m), "? Suggestions 2", "AI added 2 open questions; they block validation.")
			mustFit(t, label, m)
			m = apress(t, m, "down", "down", "down", "down", "down", "down", "down", " ")
			mustContain(t, label+" pane", view(m), "[x] Requirements: Cap exports", "[ ] Acceptance criteria")
			mustFit(t, label+" pane", m)
		}
	}
}
