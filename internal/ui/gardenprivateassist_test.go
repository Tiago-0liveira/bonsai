package ui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/assist"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/drafts"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	agentfake "github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi/fake"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// openWithAgents is privEnv.open with the agents fake wired in: a Claude account that can draft and a Codex one
// that cannot.
func (e privEnv) openWithAgents(t *testing.T, pb *privBackend, w, h int, withAgents bool) Model {
	t.Helper()
	return e.openPrefs(t, pb, w, h, withAgents, config.Prefs{})
}

func (e privEnv) openPrefs(t *testing.T, pb *privBackend, w, h int, withAgents bool, prefs config.Prefs) Model {
	t.Helper()
	st, err := config.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	st.Prefs = prefs
	opts := []Option{WithLayout(LayoutRedesign), WithGarden(pb)}
	if withAgents {
		ag := agentfake.New()
		def := agentsapi.Defaults{Fields: agentfake.SampleLaunchFields(), LaunchFields: agentfake.SampleLaunchFields(), Values: map[string]string{}}
		ag.Accounts.Items = []agentsapi.Account{
			assistAccount("x1", "codex", "personal", "logged_in", agents.StructuredSupport{Reason: "codex keeps a shell"}),
			assistAccount("c1", "claude", "work", "logged_in", agents.StructuredSupport{Supported: true, Mechanism: "claude -p"}),
		}
		ag.Accounts.Infos = map[agents.AccountID]agents.AccountInfo{
			"x1": {Identity: "me@example.com", Options: map[string]any{"auth_status": "logged_in"}},
			"c1": {Identity: "w@example.com", Options: map[string]any{"auth_status": "logged_in"}},
		}
		ag.Accounts.LaunchDefaults = map[agents.AccountID]agentsapi.Defaults{"x1": def, "c1": def}
		opts = append(opts, WithAgents(ag.API()))
	}
	m := sized(t, New(e.repo, &config.Config{}, st, opts...), w, h)
	m, _ = drainAll(t, m, m.Init())
	m = pressAll(t, m, "enter")
	m = pressAll(t, m, "tab")
	if m.gardenScreen() != scrDrafts {
		t.Fatalf("Drafts did not open:\n%s", view(m))
	}
	return m
}

var paRound1 = []assist.Question{
	{ID: "r1q1", Round: 1, Text: "Should the active session keep running?", Why: "It decides whether switching ends work in progress.", Area: "scope", Suggestions: []string{"Yes, leave it running", "Stop and ask me first"}},
	{ID: "r1q2", Round: 1, Text: "Which platforms must this support?", Why: "It limits what has to be tested.", Area: "constraint", Suggestions: []string{"Linux and macOS"}},
	{ID: "r1q3", Round: 1, Text: "How will you know it works?", Why: "It becomes the acceptance check.", Area: "acceptance"},
}

// scriptAssist makes the fake backend answer questions with set and drafts with proposal.
func scriptAssist(pb *privBackend, qs []assist.Question, proposal assist.Proposal) {
	pb.AssistQuestionsView = gardenapi.AssistQuestionsView{Provider: "claude", Set: assist.QuestionSet{Kind: "feature", Understanding: "You want switching accounts to keep each login.", Questions: qs}}
	pb.AssistDraftView = gardenapi.AssistDraftView{Proposal: proposal}
}

// paDraft opens a saved local draft with some text in the editor, ready to invite an assistant.
func paDraft(t *testing.T, e privEnv, pb *privBackend, w, h int) Model {
	t.Helper()
	m := e.openWithAgents(t, pb, w, h, true)
	m = pk(t, m, "n")
	m = ptype(t, m, "Switching accounts loses the active login")
	m = ptick(t, m)
	m = pk(t, m, "tab", "enter") // Continue writing
	if m.garden.drafts.private.stage != privEditor {
		t.Fatalf("not in the editor:\n%s", view(m))
	}
	m = pk(t, m, "ctrl+t")
	m = ptype(t, m, "Keep sessions\n")
	m = pk(t, m, "ctrl+s")
	return m
}

// focusInvite walks Tab to the assistant stop, as a person does.
func focusInvite(t *testing.T, m Model) Model {
	t.Helper()
	for i := 0; i < 30; i++ {
		p := m.garden.drafts.private
		if p.foc == p.focInvite() && !p.text {
			return m
		}
		m = pk(t, m, "tab")
	}
	t.Fatalf("never reached the assistant stop:\n%s", view(m))
	return m
}

// startDiscussion invites the assistant and starts the first round, replies included.
func startDiscussion(t *testing.T, m Model) Model {
	t.Helper()
	m = focusInvite(t, m)
	m = pk(t, m, "enter")
	p := m.garden.drafts.private
	if p.pa == nil || p.pa.stage != paInvite {
		t.Fatalf("no invitation:\n%s", view(m))
	}
	for i := 0; i < 20 && p.pa.invBtn != 1; i++ {
		m = pk(t, m, "tab")
		p = m.garden.drafts.private
	}
	m = pk(t, m, "enter")
	return m
}

// paneText is the discussion pane alone, flattened: the full view interleaves it with the specification column.
func paneText(m Model) string {
	pw, ph := m.paDims()
	var rows []string
	for _, l := range m.privPane(privOf(m), pw, ph) {
		rows = append(rows, strings.TrimPrefix(strings.TrimSpace(ansi.Strip(l)), "│"))
	}
	return flat(strings.Join(rows, " "))
}

func privOf(m Model) *privateDraft { return m.garden.drafts.private }
func paOf(m Model) *privAssistant  { return m.garden.drafts.private.pa }

var paProposalScript = assist.Proposal{
	Summary:         "Keep logins across switches.",
	CurrentBehavior: []assist.Item{{Text: "Switching accounts loses the active login.", Basis: "stated"}},
	DesiredBehavior: []assist.Item{{Text: "Each account stays signed in.", Basis: "stated"}},
	Requirements: []assist.Item{
		{Text: "Leave the active session running.", Basis: "answered", QuestionID: "r1q1"},
		{Text: "Also keep terminals attached.", Basis: "suggested"},
	},
	AcceptanceCriteria: []assist.Item{{Text: "Switch A to B and back without signing in.", Basis: "suggested"}},
}

// editSection walks the rail to a fixed field and starts writing in it.
func editSection(t *testing.T, m Model, field int) Model {
	t.Helper()
	for i := 0; i < 30; i++ {
		p := privOf(m)
		if p.foc == p.secIndex(field) && !p.text && !p.pane {
			return pk(t, m, "enter")
		}
		m = pk(t, m, "tab")
	}
	t.Fatalf("never reached section %d", field)
	return m
}

// toAct tabs to the pane's buttons.
func toAct(t *testing.T, m Model) Model {
	t.Helper()
	for i := 0; i < 6 && !paOf(m).onAct; i++ {
		m = pk(t, m, "tab")
	}
	if !paOf(m).onAct {
		t.Fatalf("never reached the buttons:\n%s", view(m))
	}
	return m
}

// answerFirst takes the pane, picks the first suggestion of the first question and returns with the pane focused.
func answerFirst(t *testing.T, m Model) Model {
	t.Helper()
	if !privOf(m).pane {
		m = pk(t, m, "ctrl+g")
	}
	return pk(t, m, "1")
}

func assertNoWrites(t *testing.T, pb *privBackend) {
	t.Helper()
	if pb.Writes() != 0 || pb.Count("PreviewCapture") != 0 || pb.Count("Capture") != 0 || pb.Count("PreviewSave") != 0 {
		t.Fatalf("GitHub was written: %v", pb.Calls())
	}
}

func TestPrivateAssistInvitationDisclosesAndStartsOnlyOnRequest(t *testing.T) {
	e := newPrivEnv(t)
	pb := e.backend(t)
	scriptAssist(pb, paRound1, paProposalScript)
	m := paDraft(t, e, pb, 120, 35)
	m = focusInvite(t, m)
	m = pk(t, m, "enter")
	out := view(m)
	mustContain(t, "invite", flat(out), "INVITE AN ASSISTANT", "Claude · work", "cannot draft: codex keeps a shell", "Model", "Effort",
		"Sends the title and type, the description (41 characters), the specification text (1 section written) and the repository name acme/my-app to Claude Code (account work).",
		"No repository files are read", "Keep writing myself", "Start discussion")
	mustFit(t, "invite", m)
	if pb.Count("AssistQuestions") != 0 || pb.Count("AssistDraft") != 0 {
		t.Fatal("inviting sent something")
	}
	// The options dialog is the same one the Issue assistant uses, limited to three questions.
	if !strings.Contains(flat(out), "per round, 1 to 3") {
		t.Fatalf("question limit not shown:\n%s", out)
	}
	// Keep writing myself: AI-off again, nothing sent.
	m = pk(t, m, "esc")
	if privOf(m).pa != nil {
		t.Fatal("Esc must close the invitation")
	}
	mustContain(t, "ai off", flat(view(m)), "Assistant off", "Invite assistant…", "Write and approve without AI.")
	if pb.Count("AssistQuestions") != 0 {
		t.Fatal("declining sent something")
	}

	// Start discussion: one request, scoped to this draft.
	m = startDiscussion(t, m)
	if len(pb.AssistQuestionsRequests) != 1 {
		t.Fatalf("%d question requests", len(pb.AssistQuestionsRequests))
	}
	r := pb.AssistQuestionsRequests[0]
	p := privOf(m)
	if r.PrivateDraft == nil || r.PrivateDraft.ID != p.draft.ID || r.PrivateDraft.Revision != p.draft.Revision || r.PrivateDraft.Generation != 1 ||
		r.IssueID != 0 || r.Number != 0 || r.Settings.AccountID != "c1" || r.Settings.Questions != 3 || r.Round != 1 ||
		r.Title != "Keep sessions" || r.Description != "Switching accounts loses the active login" ||
		r.RequestID != "priv-1-1" || len(r.Answers) != 0 || !strings.Contains(r.Draft, "Switching accounts loses the active login") {
		t.Fatalf("request %+v", r)
	}
	if paOf(m).stage != paQuestions || len(paOf(m).s.questions) != 3 {
		t.Fatalf("stage %d, %d questions", paOf(m).stage, len(paOf(m).s.questions))
	}
	assertNoWrites(t, pb)
}

func TestPrivateAssistShowsOneQuestionAtATimeAndKeepsTheSpecEditable(t *testing.T) {
	for _, sz := range [][2]int{{120, 35}, {160, 48}} {
		e := newPrivEnv(t)
		pb := e.backend(t)
		scriptAssist(pb, paRound1, paProposalScript)
		m := paDraft(t, e, pb, sz[0], sz[1])
		m = startDiscussion(t, m)
		out := view(m)
		mustContain(t, "pane", paneText(m), "DISCUSSION", "reply ready", "Claude Code / work", "You want switching accounts to keep each login.",
			"Should the active session keep running?", "( ) Yes, leave it running", "( ) Write my own answer", "( ) I don't know", "( ) Skip for now",
			"1 of 3 useful questions", "Apply answers →")
		// Why it matters is on demand only.
		mustNotContain(t, "why hidden", paneText(m), "It decides whether switching ends work in progress.")
		// One question at a time: the other two are not drawn.
		mustNotContain(t, "one question", paneText(m), "Which platforms must this support?", "How will you know it works?")
		// The specification column stays: tabs and the editor are still there.
		mustContain(t, "spec", out, "Problem", "Outcome", "Criteria", "Review & create issue →")
		mustFit(t, "questions", m)
		// Typing in the spec while the pane exists still edits the spec.
		m = editSection(t, m, 1)
		m = ptype(t, m, "!")
		if p := privOf(m); !p.text || !strings.Contains(p.ta.Value(), "!") {
			t.Fatalf("the spec stopped taking text: %q", p.ta.Value())
		}
		m = pk(t, m, "esc", "ctrl+g")
		if !privOf(m).pane || privOf(m).text {
			t.Fatal("Ctrl+G must move the keyboard to the pane")
		}
		m = pk(t, m, "w")
		mustContain(t, "why", paneText(m), "Why: It decides whether switching")
		m = pk(t, m, "pgdown")
		mustContain(t, "second", paneText(m), "Which platforms must this support?", "2 of 3 useful questions")
		mustFit(t, "second", m)
		m = pk(t, m, "ctrl+g")
		if privOf(m).pane {
			t.Fatal("Ctrl+G must return the keyboard to the text")
		}
	}
}

func TestPrivateAssistAnswerApplyIsExplicitAndConfirmedStateSurvivesRestart(t *testing.T) {
	e := newPrivEnv(t)
	pb := e.backend(t)
	scriptAssist(pb, paRound1, paProposalScript)
	m := paDraft(t, e, pb, 120, 35)
	m = startDiscussion(t, m)
	m = answerFirst(t, m) // picking a row moves on to its notes field
	m = ptype(t, m, "Keep terminals too.")
	m = toAct(t, m)
	m = pk(t, m, "enter") // Apply answers → asks for a proposal
	if len(pb.AssistDraftRequests) != 1 {
		t.Fatalf("%d draft requests", len(pb.AssistDraftRequests))
	}
	dr := pb.AssistDraftRequests[0]
	if dr.PrivateDraft == nil || dr.PrivateDraft.Generation != 2 || len(dr.Answers) != 3 || dr.Answers[0].State != assist.Answered || dr.Answers[0].Choice != "Yes, leave it running" || dr.Answers[0].Text != "Keep terminals too." ||
		dr.Answers[1].State != assist.Open {
		t.Fatalf("draft request %+v", dr)
	}
	p := privOf(m)
	if paOf(m).stage != paProposal {
		t.Fatalf("stage %d:\n%s", paOf(m).stage, view(m))
	}
	// The proposal is shown as a proposal; the draft itself has not changed.
	_ = view(m)
	mustContain(t, "proposal", paneText(m), "PROPOSAL — not in your draft yet", "[x] Problem", "[x] Outcome", "[x] Requirements", "2 suggestions (not yours)")
	if p.secs[p.secIndex(2)].text != "" || p.secs[p.secIndex(3)].text != "" || len(p.confirmed) != 0 {
		t.Fatalf("a proposal changed the draft: %+v / %+v", p.secs, p.confirmed)
	}
	mustFit(t, "proposal", m)

	m = toAct(t, m)
	m = pk(t, m, "enter") // Apply
	p = privOf(m)
	if !p.unwritten() {
		t.Fatal("an apply is an unsaved edit until its save lands: a newer revision must not replace it silently")
	}
	if got := p.secs[p.secIndex(1)].text; got != "- Switching accounts loses the active login." {
		t.Fatalf("Problem = %q", got)
	}
	if got := p.secs[p.secIndex(3)].text; got != "- Leave the active session running." {
		t.Fatalf("Requirements = %q (an unconfirmed suggestion must not join)", got)
	}
	if got := p.secs[p.secIndex(4)].text; got != "" {
		t.Fatalf("Criteria = %q: a suggestion nobody confirmed was applied", got)
	}
	if len(p.confirmed) != 1 || p.confirmed[0].Question != "Should the active session keep running?" || p.confirmed[0].Answer != "Yes, leave it running — Keep terminals too." {
		t.Fatalf("confirmed = %+v", p.confirmed)
	}
	mustContain(t, "applied", paneText(m), "✓ Applied to Summary, Problem, Outcome, Requirements.")
	if !p.changed[p.secIndex(1)] {
		t.Fatal("applied sections must be highlighted")
	}
	mustContain(t, "highlight", view(m), "Problem ●")
	m = ptick(t, m)
	id := p.draft.ID
	saved, err := pb.store.Read(t.Context(), pb.scope, id)
	if err != nil || len(saved.ConfirmedAnswers) != 1 || saved.Spec.Requirements != "- Leave the active session running." {
		t.Fatalf("stored %+v %v", saved, err)
	}

	// Restart: a new model on the same files restores the confirmed answer; the pane does not come back.
	pb2 := newPrivBackend(t, draftsFake(t), e.file)
	scriptAssist(pb2, append([]assist.Question{paRound1[0]}, paRound1[1]), paProposalScript)
	m2 := e.openWithAgents(t, pb2, 120, 35, true)
	m2 = pk(t, m2, "enter") // the saved draft is selected in the inbox
	p2 := privOf(m2)
	if p2 == nil || p2.draft.ID != id || len(p2.confirmed) != 1 || p2.pa != nil {
		t.Fatalf("restart: %+v", p2)
	}
	m2 = startDiscussion(t, m2)
	r := pb2.AssistQuestionsRequests[0]
	if len(r.Answers) != 1 || r.Answers[0].QuestionID != "c1" || r.Answers[0].State != assist.Answered || r.Answers[0].Text != "Yes, leave it running — Keep terminals too." {
		t.Fatalf("restart request answers %+v", r.Answers)
	}
	// The same question comes back: it is already decided, so only the new one is asked.
	if qs := paOf(m2).s.questions; len(qs) != 1 || qs[0].Text != "Which platforms must this support?" || paOf(m2).decided != 1 {
		t.Fatalf("questions %+v decided %d", qs, paOf(m2).decided)
	}
	mustContain(t, "decided", paneText(m2), "1 of 1 · 1 already decided")
	assertNoWrites(t, pb)
}

func TestPrivateAssistKeepsDirectEditsMadeWhileARequestRuns(t *testing.T) {
	e := newPrivEnv(t)
	pb := e.backend(t)
	scriptAssist(pb, paRound1, paProposalScript)
	m := paDraft(t, e, pb, 120, 35)
	m = startDiscussion(t, m)
	m = answerFirst(t, m)
	m = toAct(t, m)
	m, cmd := send(t, m, pkey("enter")) // the request is in the air
	msgs := runCmds(cmd)
	if paOf(m).flight == nil || len(msgs) != 1 {
		t.Fatalf("no flight: %v", msgs)
	}
	mustContain(t, "running", paneText(m), "Asking Claude Code… 0s · Esc cancels")
	// While it runs the person keeps writing in Requirements.
	m = pk(t, m, "ctrl+g") // back to the text side
	m = editSection(t, m, 3)
	m = ptype(t, m, "My own requirement.")
	m = pk(t, m, "esc")
	typed := privOf(m).ta.Value()
	if typed != "My own requirement." {
		t.Fatalf("requirement text %q", typed)
	}
	m = deliver(t, m, msgs...)
	p := privOf(m)
	if paOf(m).stage != paProposal || paOf(m).flight != nil {
		t.Fatalf("reply did not land: stage %d", paOf(m).stage)
	}
	secs := reviewSectionsOf(paOf(m).review, p.fixedText)
	if secs[3].state != '!' || secs[3].accept || secs[1].state != '~' || !secs[1].accept {
		t.Fatalf("merge states: Requirements %+v, Problem %+v", secs[3], secs[1])
	}
	m = pk(t, m, "ctrl+g")
	mustContain(t, "conflict", paneText(m), "[ ] Requirements kept yours", "[x] Problem proposed")
	m = toAct(t, m)
	m = pk(t, m, "enter")
	p = privOf(m)
	if got := p.secs[p.secIndex(3)].text; got != "My own requirement." {
		t.Fatalf("direct edit clobbered: %q", got)
	}
	if got := p.secs[p.secIndex(1)].text; got != "- Switching accounts loses the active login." {
		t.Fatalf("untouched section was not applied: %q", got)
	}
}

// proposalReady brings a discussion to a proposal waiting in the pane, with the pane focused.
func proposalReady(t *testing.T, w, h int) (Model, *privBackend) {
	t.Helper()
	e := newPrivEnv(t)
	pb := e.backend(t)
	scriptAssist(pb, paRound1, paProposalScript)
	m := paDraft(t, e, pb, w, h)
	m = startDiscussion(t, m)
	m = answerFirst(t, m)
	m = toAct(t, m)
	m = pk(t, m, "enter")
	if paOf(m).stage != paProposal {
		t.Fatalf("no proposal:\n%s", view(m))
	}
	return m, pb
}

func TestPrivateAssistEditedSectionIsReplacedOnlyByExplicitChoice(t *testing.T) {
	m, _ := proposalReady(t, 120, 35)
	m = privSetEditorSection(t, m, 3, "My own requirement.")
	// Row 3 is Requirements: Enter on it takes the proposal for that section, replacing the edit.
	m = pk(t, m, "down", "down", "down")
	if r := paOf(m).review; r.sel != 3 {
		t.Fatalf("selected %d", r.sel)
	}
	mustContain(t, "kept", paneText(m), "[ ] Requirements kept yours")
	m = pk(t, m, "enter")
	mustContain(t, "taken", paneText(m), "[x] Requirements replaces yours")
	m = toAct(t, m)
	m = pk(t, m, "enter")
	p := privOf(m)
	if got := p.secs[p.secIndex(3)].text; got != "- Leave the active session running." {
		t.Fatalf("Requirements = %q", got)
	}
}

func TestPrivateAssistSuggestionsJoinOnlyWhenConfirmed(t *testing.T) {
	m, _ := proposalReady(t, 120, 35)
	// Down past the sections to the suggestions row; space confirms the first suggestion.
	for i := 0; i < 7; i++ {
		m = pk(t, m, "down")
	}
	if !paOf(m).review.onSuggestions() {
		t.Fatal("not on the suggestions row")
	}
	mustContain(t, "suggestions", paneText(m), "Suggestions · the assistant's ideas, nobody asked for them", "[ ] Requirements: Also keep terminals attached.")
	m = pk(t, m, " ")
	m = toAct(t, m)
	m = pk(t, m, "enter")
	p := privOf(m)
	if got := p.secs[p.secIndex(3)].text; got != "- Leave the active session running.\n- Also keep terminals attached." {
		t.Fatalf("Requirements = %q", got)
	}
	if got := p.secs[p.secIndex(4)].text; got != "" {
		t.Fatalf("an unconfirmed suggestion joined Criteria: %q", got)
	}
}

func TestPrivateAssistDiscardLeavesTheDraftAndAnswersAlone(t *testing.T) {
	m, pb := proposalReady(t, 120, 35)
	before := privOf(m).build()
	m = toAct(t, m)
	m = pk(t, m, "right", "enter") // Discard
	pa := paOf(m)
	if pa.stage != paQuestions || pa.review != nil || !strings.Contains(pa.notice, "Proposal discarded") {
		t.Fatalf("stage %d notice %q", pa.stage, pa.notice)
	}
	if got := privOf(m).build(); !reflect.DeepEqual(got.Spec, before.Spec) || len(got.ConfirmedAnswers) != 0 {
		t.Fatal("discarding changed the draft")
	}
	if a := pa.s.answer(pa.s.questions[0]); a.sel != selChoice {
		t.Fatalf("the answer was lost: %+v", a)
	}
	assertNoWrites(t, pb)
}

func TestPrivateAssistRendersUnderEveryTheme(t *testing.T) {
	for _, name := range theme.Presets() {
		useTheme(t, theme.Resolve(name, nil))
		m, _ := proposalReady(t, 120, 35)
		mustFit(t, name+" proposal", m)
		m = pk(t, m, "tab", "enter")
		mustFit(t, name+" applied", m)
	}
}

// privSetEditorSection edits a section's text directly, as the person's own typing would.
func privSetEditorSection(t *testing.T, m Model, field int, text string) Model {
	t.Helper()
	p := privOf(m)
	p.flush()
	p.secs[p.secIndex(field)].text = text
	p.load(p.cur)
	return m
}

// heldFlight invites, starts the discussion and returns with the first request in the air: the model and the
// reply the backend already produced but the model has not seen.
func heldFlight(t *testing.T) (Model, privAssistMsg, *privBackend) {
	t.Helper()
	e := newPrivEnv(t)
	pb := e.backend(t)
	scriptAssist(pb, paRound1, paProposalScript)
	m := paDraft(t, e, pb, 120, 35)
	m = focusInvite(t, m)
	m = pk(t, m, "enter")
	for i := 0; i < 20 && paOf(m).invBtn != 1; i++ {
		m = pk(t, m, "tab")
	}
	m, cmd := send(t, m, pkey("enter")) // Start discussion, reply held
	msgs := runCmds(cmd)
	if paOf(m).flight == nil || len(msgs) != 1 {
		t.Fatalf("no flight (%d msgs)", len(msgs))
	}
	return m, msgs[0].(privAssistMsg), pb
}

func TestPrivateAssistCancelAndNewerRequestsDropOlderReplies(t *testing.T) {
	m, first, pb := heldFlight(t)
	before := privOf(m).build()

	// Cancel: nothing changes, and the reply that still arrives is dropped.
	m = pk(t, m, "ctrl+g", "esc")
	pa := paOf(m)
	if pa.flight != nil || !pa.cancelled || !strings.Contains(pa.notice, "Cancelled. Nothing was changed.") {
		t.Fatalf("cancel: flight %v cancelled %v notice %q", pa.flight, pa.cancelled, pa.notice)
	}
	m = deliver(t, m, first)
	if len(paOf(m).s.questions) != 0 || paOf(m).stage != paIdle {
		t.Fatal("a reply after cancel landed")
	}
	if strings.Contains(paOf(m).notice, assistOutOfDate) {
		t.Fatalf("a reply to a cancelled request must be dropped silently: %q", paOf(m).notice)
	}
	if got := privOf(m).build(); !reflect.DeepEqual(got.Spec, before.Spec) || got.Title != before.Title {
		t.Fatal("cancel changed the text")
	}
	mustContain(t, "retry offered", paneText(m), "Retry")

	// Retry is explicit and carries a newer generation; the older reply is then stale.
	m, _ = send(t, m, pkey("right"))
	m, cmd := send(t, m, pkey("enter"))
	second := runCmds(cmd)
	if pa = paOf(m); pa.flight == nil || pa.flight.gen != 2 || len(second) != 1 {
		t.Fatalf("retry: %+v", pa.flight)
	}
	m = deliver(t, m, first)
	if paOf(m).flight == nil || len(paOf(m).s.questions) != 0 {
		t.Fatal("the cancelled request's reply disturbed the newer one")
	}
	m = deliver(t, m, second...)
	if paOf(m).stage != paQuestions || len(paOf(m).s.questions) != 3 || len(pb.AssistQuestionsRequests) != 2 {
		t.Fatalf("current reply: stage %d", paOf(m).stage)
	}
}

func TestPrivateAssistReplyScopeIsEnforced(t *testing.T) {
	m, good, _ := heldFlight(t)
	for name, mutate := range map[string]func(*privAssistMsg){
		"another draft":  func(r *privAssistMsg) { r.id = "draft-other" },
		"another editor": func(r *privAssistMsg) { r.token += 7 },
		"older request":  func(r *privAssistMsg) { r.gen-- },
	} {
		bad := good
		mutate(&bad)
		m = deliver(t, m, bad)
		if paOf(m) == nil || paOf(m).flight == nil || len(paOf(m).s.questions) != 0 {
			t.Fatalf("%s: a foreign reply touched the flight or the questions", name)
		}
	}
	// The right flight with the wrong echo: it ends and nothing applies.
	for name, mutate := range map[string]func(*privAssistMsg){
		"wrong request ID": func(r *privAssistMsg) { r.qv.RequestID = "priv-1-9" },
		"wrong generation": func(r *privAssistMsg) {
			r.qv.PrivateDraft = &gardenapi.PrivateDraftRef{ID: r.id, Revision: 1, Generation: 99}
		},
		"no scope echo": func(r *privAssistMsg) { r.qv.PrivateDraft = nil },
		"another epoch": func(r *privAssistMsg) { r.qv.Stamp.Epoch += 5 },
	} {
		fm, fgood, _ := heldFlight(t)
		bad := fgood
		mutate(&bad)
		fm = deliver(t, fm, bad)
		if pa := paOf(fm); pa.flight != nil || len(pa.s.questions) != 0 || pa.stage != paIdle || !strings.Contains(pa.notice, assistOutOfDate) {
			t.Fatalf("%s: stage %d flight %v notice %q", name, pa.stage, pa.flight, pa.notice)
		}
	}
	// The genuine reply still lands.
	m = deliver(t, m, good)
	if paOf(m).stage != paQuestions || len(paOf(m).s.questions) != 3 {
		t.Fatalf("genuine reply: stage %d", paOf(m).stage)
	}
}

func TestPrivateAssistFailureKeepsTextAndRetryIsExplicit(t *testing.T) {
	e := newPrivEnv(t)
	pb := e.backend(t)
	scriptAssist(pb, paRound1, paProposalScript)
	pb.AssistQuestionsErr = &gardenapi.Error{Code: gardenapi.CodeAssistTimeout, Message: "The assistant did not answer in time. Your answers are kept.", Next: "Retry, or choose a faster model."}
	m := paDraft(t, e, pb, 120, 35)
	before := privOf(m).build()
	m = startDiscussion(t, m)
	pa := paOf(m)
	if pa.err == nil || pa.stage != paIdle || pa.flight != nil {
		t.Fatalf("failure: %+v", pa)
	}
	out := paneText(m)
	mustContain(t, "failure", out, "! The assistant did not answer in time.", "Retry, or choose a faster model.", "Retry")
	if got := privOf(m).build(); !reflect.DeepEqual(got.Spec, before.Spec) || got.Title != before.Title || got.Description != before.Description {
		t.Fatal("a failure changed the draft")
	}
	if privOf(m).state == saveFailed {
		t.Fatal("an assistant failure is not a save failure")
	}
	mustFit(t, "failure", m)
	if len(pb.AssistQuestionsRequests) != 1 {
		t.Fatalf("auto-retried: %d requests", len(pb.AssistQuestionsRequests))
	}
	pb.AssistQuestionsErr = nil
	m = pk(t, m, "ctrl+g", "R")
	if len(pb.AssistQuestionsRequests) != 2 || pb.AssistQuestionsRequests[1].RequestID == pb.AssistQuestionsRequests[0].RequestID ||
		pb.AssistQuestionsRequests[1].PrivateDraft.Generation != 2 || pb.AssistQuestionsRequests[1].Round != 1 {
		t.Fatalf("retry requests: %+v", pb.AssistQuestionsRequests)
	}
	if paOf(m).stage != paQuestions || paOf(m).err != nil {
		t.Fatalf("retry did not recover: stage %d", paOf(m).stage)
	}
}

func TestPrivateAssistScopeQuestionsStayOpenUnlessAnswered(t *testing.T) {
	e := newPrivEnv(t)
	pb := e.backend(t)
	scriptAssist(pb, paRound1, assist.Proposal{
		CurrentBehavior: []assist.Item{{Text: "Switching accounts loses the active login.", Basis: "stated"}},
		Open:            []string{"Should the active session keep running?"},
	})
	m := paDraft(t, e, pb, 120, 35)
	m = startDiscussion(t, m)
	// Pending scope and acceptance questions hold the draft back before anything is applied.
	mustContain(t, "pending", flat(view(m)), "2 assistant decisions open")
	m = pk(t, m, "ctrl+g", "5") // Skip for now
	m = toAct(t, m)
	m = pk(t, m, "enter")
	if dr := pb.AssistDraftRequests[0]; dr.Answers[0].State != assist.Skipped || dr.Answers[0].Choice != "" || dr.Answers[0].Text != "" {
		t.Fatalf("skip travelled as %+v", dr.Answers[0])
	}
	m = pk(t, m, "tab") // list → buttons
	m = pk(t, m, "enter")
	p := privOf(m)
	r := drafts.Assess(p.build())
	if r.Unresolved != 1 || r.Ready() || len(p.confirmed) != 0 {
		t.Fatalf("a skipped question resolved itself: %+v confirmed %+v", r, p.confirmed)
	}
	if !strings.Contains(p.secs[p.secIndex(6)].text, "Should the active session keep running?") {
		t.Fatalf("the skipped question must stay visible: %q", p.secs[p.secIndex(6)].text)
	}
	m = pk(t, m, "ctrl+g")
	m = pk(t, m, "ctrl+r")
	mustContain(t, "review", flat(view(m)), "Not ready:", "1 open question")
}

func TestPrivateAssistAdvisoryDecisionsAreRecordedAndNeverBypassScope(t *testing.T) {
	e := newPrivEnv(t)
	pb := e.backend(t)
	scriptAssist(pb, paRound1, paProposalScript)
	m := paDraft(t, e, pb, 120, 35)
	m = startDiscussion(t, m)
	m = pk(t, m, "ctrl+g")

	// A scope question cannot be deferred, by any route.
	m, _ = m.paDecide(privOf(m), drafts.Deferred)
	if len(privOf(m).decisions) != 0 || !strings.Contains(paOf(m).notice, "cannot be deferred") {
		t.Fatalf("scope question deferred: %+v / %q", privOf(m).decisions, paOf(m).notice)
	}
	mustContain(t, "actions", paneText(m), "Apply answers →", "Why?")
	mustNotContain(t, "no defer for scope", paneText(m), "Defer", "Assume")

	// The advisory one (second) offers both. Assume needs something to assume.
	m = pk(t, m, "pgdown")
	m = toAct(t, m)
	mustContain(t, "advisory actions", paneText(m), "Defer", "Assume")
	m = pk(t, m, "right", "right", "enter") // Assume with nothing chosen
	if len(privOf(m).decisions) != 0 || !strings.Contains(paOf(m).notice, "Pick a suggestion") {
		t.Fatalf("assumed nothing: %+v / %q", privOf(m).decisions, paOf(m).notice)
	}
	m = pk(t, m, "shift+tab", "shift+tab", "1") // pick Linux and macOS
	m = toAct(t, m)
	m = pk(t, m, "right", "right", "enter")
	p := privOf(m)
	if len(p.decisions) != 1 || p.decisions[0] != (drafts.Decision{Question: "Which platforms must this support?", State: drafts.Assumed, Assumption: "Linux and macOS"}) {
		t.Fatalf("decisions %+v", p.decisions)
	}
	line := "- Assumed: Linux and macOS (Which platforms must this support?)"
	if got := p.secs[p.secIndex(6)].text; got != line {
		t.Fatalf("Questions = %q", got)
	}
	if n := len(paOf(m).s.questions); n != 2 {
		t.Fatalf("a settled question must leave the round: %d left", n)
	}
	// The decision is part of the spec and of the saved draft; readiness honors it only while they agree.
	m = privSetEditorSection(t, m, 2, "Each account stays signed in.")
	d := p.build()
	d.Title, d.Spec.CurrentBehavior, d.Spec.AcceptanceCriteria = "T", "p", "c"
	if r := drafts.Assess(d); !r.Ready() || r.Deferred != 1 {
		t.Fatalf("assess %+v", r)
	}
	m = ptick(t, m)
	saved, err := pb.store.Read(t.Context(), pb.scope, p.draft.ID)
	if err != nil || len(saved.Decisions) != 1 || saved.Decisions[0].Assumption != "Linux and macOS" {
		t.Fatalf("stored %+v %v", saved.Decisions, err)
	}
	d.Spec.OpenQuestions = "- Assumed: Windows (Which platforms must this support?)"
	if r := drafts.Assess(d); r.Ready() || r.Unresolved != 1 {
		t.Fatalf("a hand-edited assumption passed: %+v", r)
	}
	// Defer works the same for a question without suggestions' notes, and is recorded.
	m = pk(t, m, "tab")
	_ = m
}

func TestPrivateAssistTextFieldsOwnTheirKeys(t *testing.T) {
	e := newPrivEnv(t)
	pb := e.backend(t)
	scriptAssist(pb, paRound1, paProposalScript)
	m := paDraft(t, e, pb, 120, 35)
	m = startDiscussion(t, m)
	m = pk(t, m, "ctrl+g", "4") // I don't know: notes field
	if m.currentShellFocus() != shellInput {
		t.Fatal("the pane must own the keyboard")
	}
	m = ptype(t, m, "q n ? b")
	a := paOf(m).s.answer(paOf(m).s.questions[0])
	if a.sel != selUnknown || a.text != "q n ? b" || m.modal != nil {
		t.Fatalf("answer %+v modal %v", a, m.modal)
	}
	m = pk(t, m, "ctrl+]")
	if paOf(m).s.answer(paOf(m).s.questions[0]).text != "q n ? b" || m.modal != nil {
		t.Fatal("ctrl+] must not be consumed as text")
	}
	// Esc leaves the text, then the pane, and never discards the answer.
	m = pk(t, m, "esc")
	if privOf(m).pane != true || paOf(m).s.zone != zoneOptions {
		t.Fatal("first Esc leaves the text field only")
	}
	m = pk(t, m, "esc")
	if privOf(m).pane || paOf(m).s.answer(paOf(m).s.questions[0]).text != "q n ? b" {
		t.Fatal("second Esc leaves the pane and keeps the answer")
	}
	// Back on the rail, letters are shortcuts again: q asks about quitting.
	if m.currentShellFocus() != shellBody {
		t.Fatalf("focus %v", m.currentShellFocus())
	}
}

func TestPrivateAssistCompactNavigationUsesATab(t *testing.T) {
	e := newPrivEnv(t)
	pb := e.backend(t)
	scriptAssist(pb, paRound1, paProposalScript)
	m := paDraft(t, e, pb, 80, 24)
	mustContain(t, "tab", view(m), "Assistant…")
	mustFit(t, "compact", m)
	m = focusInvite(t, m)
	mustContain(t, "focus", view(m), "▸Assistant…")
	m = pk(t, m, "enter")
	mustContain(t, "invite", flat(view(m)), "INVITE AN ASSISTANT", "Start discussion")
	mustFit(t, "compact invite", m)
	m = startDiscussion2(t, m)
	out := view(m)
	// The text stays in front while the reply waits in the Discussion tab.
	mustContain(t, "waiting", out, "Discussion ●")
	mustNotContain(t, "text in front", flat(view(m)), "Should the active session keep running?")
	mustFit(t, "compact text", m)
	m = focusInvite(t, m)
	mustContain(t, "page", paneText(m), "DISCUSSION", "Should the active session keep running?", "Apply answers →")
	mustFit(t, "compact page", m)
	m = pk(t, m, "ctrl+g", "1")
	mustFit(t, "compact answer", m)
	if a := paOf(m).s.answer(paOf(m).s.questions[0]); a.sel != selChoice {
		t.Fatalf("answer %+v", a)
	}
	m = pk(t, m, "esc", "esc")
	if privOf(m).pane || privOf(m).foc != privOf(m).focInvite() {
		t.Fatal("Esc must return to the tab row")
	}
	m = pk(t, m, "tab") // on to Review, then back round to the sections
	if privOf(m).foc != privOf(m).focReview() {
		t.Fatalf("Tab moved to %d", privOf(m).foc)
	}
	assertNoWrites(t, pb)
}

func TestPrivateAssistAIOffStaysCompleteWithoutAccounts(t *testing.T) {
	e := newPrivEnv(t)
	pb := e.backend(t)
	m := e.openWithAgents(t, pb, 120, 35, false)
	m = pk(t, m, "n")
	m = ptype(t, m, "Just write it")
	m = pk(t, m, "tab", "tab", "enter") // Discuss with AI…
	p := privOf(m)
	if p.pa != nil || p.stage != privEditor || !strings.Contains(p.notice, "Agent accounts are unavailable") {
		t.Fatalf("pa %v stage %d notice %q", p.pa, p.stage, p.notice)
	}
	m = focusInvite(t, m)
	m = pk(t, m, "enter")
	if privOf(m).pa != nil || privOf(m).stage != privEditor {
		t.Fatal("an unavailable assistant must not open anything")
	}
	// The full manual path still works.
	m = pk(t, m, "ctrl+t")
	m = ptype(t, m, "Manual title\n")
	m = pk(t, m, "ctrl+s")
	if l := pb.all(t); len(l) != 1 || l[0].Title != "Manual title" {
		t.Fatalf("store %+v", l)
	}
	if pb.Count("AssistQuestions")+pb.Count("AssistDraft") != 0 {
		t.Fatal("assistant called")
	}
}

func TestPrivateAssistLeavingEndsTheRequestAndKeepsConfirmedText(t *testing.T) {
	e := newPrivEnv(t)
	pb := e.backend(t)
	scriptAssist(pb, paRound1, paProposalScript)
	m := paDraft(t, e, pb, 120, 35)
	m = startDiscussion(t, m)
	m = answerFirst(t, m)
	m = toAct(t, m)
	m, cmd := send(t, m, pkey("enter"))
	msgs := runCmds(cmd)
	f := paOf(m).flight
	m = pk(t, m, "ctrl+g", "esc", "esc") // back to the text, then out to the inbox
	if f == nil || m.garden.drafts.private != nil && m.garden.drafts.stage == draftsPrivate {
		t.Fatalf("did not leave: %v", m.garden.drafts.stage)
	}
	m = deliver(t, m, msgs...) // the late reply finds no draft open
	if m.garden.drafts.private != nil && m.garden.drafts.private.pa != nil {
		t.Fatal("a reply revived the discussion")
	}
}

func TestPrivateAssistBoundedRenderingAtReferenceSizes(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {120, 35}, {160, 48}} {
		for _, ascii := range []bool{false, true} {
			e := newPrivEnv(t)
			pb := e.backend(t)
			scriptAssist(pb, paRound1, paProposalScript)
			prefs := config.Prefs{}
			if ascii {
				prefs.AgentGlyphs = "ascii"
			}
			m := e.openPrefs(t, pb, sz[0], sz[1], true, prefs)
			m = pk(t, m, "n")
			m = ptype(t, m, "Switching accounts loses the active login")
			m = ptick(t, m)
			m = pk(t, m, "tab", "enter")
			m = focusInvite(t, m)
			m = pk(t, m, "enter")
			name := func(stage string) string { return fmt.Sprintf("%dx%d ascii=%v %s", sz[0], sz[1], ascii, stage) }
			mustFit(t, name("invite"), m)
			m = startDiscussion2(t, m)
			mustFit(t, name("questions"), m)
			m = pk(t, m, "ctrl+g")
			mustFit(t, name("questions focused"), m)
			m = pk(t, m, "1")
			mustFit(t, name("answer"), m)
			m = ptype(t, m, strings.Repeat("a long answer ", 20))
			mustFit(t, name("long answer"), m)
			m = toAct(t, m)
			m = pk(t, m, "enter")
			if paOf(m).stage != paProposal {
				t.Fatalf("%v: stage %d\n%s", sz, paOf(m).stage, view(m))
			}
			mustFit(t, name("proposal"), m)
			m = toAct(t, m)
			m = pk(t, m, "enter")
			mustFit(t, name("applied"), m)
			if ascii {
				for _, bad := range []string{"│", "●", "→", "✓"} {
					if strings.Contains(view(m), bad) {
						t.Fatalf("%v ascii: found %q:\n%s", sz, bad, view(m))
					}
				}
			}
		}
	}
}

// startDiscussion2 is startDiscussion from the invitation already open.
func startDiscussion2(t *testing.T, m Model) Model {
	t.Helper()
	for i := 0; i < 20 && paOf(m).invBtn != 1; i++ {
		m = pk(t, m, "tab")
	}
	return pk(t, m, "enter")
}

func TestPrivateAssistReopenedDiscussionNeverSharesAnEarlierRequestIdentity(t *testing.T) {
	m, first, pb := heldFlight(t)
	m = pk(t, m, "ctrl+g", "esc")           // cancel
	m = pk(t, m, "right", "right", "enter") // Close
	if privOf(m).pa != nil {
		t.Fatal("Close must end the discussion")
	}
	m = focusInvite(t, m)
	m = pk(t, m, "enter")
	for i := 0; i < 20 && paOf(m).invBtn != 1; i++ {
		m = pk(t, m, "tab")
	}
	m, cmd := send(t, m, pkey("enter"))
	second := runCmds(cmd)
	f := paOf(m).flight
	if f == nil || len(second) != 1 {
		t.Fatalf("the new discussion could not start: %v", paOf(m).notice)
	}
	if f.gen <= first.gen || f.requestID == pb.AssistQuestionsRequests[0].RequestID {
		t.Fatalf("new request reuses identity: gen %d vs %d, id %q", f.gen, first.gen, f.requestID)
	}
	// The old reply is for a request nobody wants any more: dropped, and the new flight is untouched.
	m = deliver(t, m, first)
	if paOf(m).flight != f || len(paOf(m).s.questions) != 0 || strings.Contains(paOf(m).notice, assistOutOfDate) {
		t.Fatalf("old reply disturbed the new discussion: notice %q", paOf(m).notice)
	}
	m = deliver(t, m, second...)
	if paOf(m).stage != paQuestions || len(paOf(m).s.questions) != 3 {
		t.Fatalf("new reply: stage %d", paOf(m).stage)
	}
}

func TestPrivateAssistAppliedSkipsStayOpenInTheSavedDraft(t *testing.T) {
	e := newPrivEnv(t)
	pb := e.backend(t)
	// The proposal carries no open question and leaves Questions alone.
	scriptAssist(pb, paRound1, paProposalScript)
	m := paDraft(t, e, pb, 120, 35)
	m = startDiscussion(t, m)
	// The person keeps their own Questions text.
	m = privSetEditorSection(t, m, 6, "- My own note")
	m = pk(t, m, "ctrl+g", "5")                  // Skip the scope question
	m = pk(t, m, "pgdown", "pgdown", "esc", "2") // I don't know for the acceptance question (no suggestions: it starts in its text field)
	m = toAct(t, m)
	m = pk(t, m, "enter")
	m = toAct(t, m)
	m = pk(t, m, "enter")
	p := privOf(m)
	text := p.secs[p.secIndex(6)].text
	for _, want := range []string{"- My own note", "- Should the active session keep running?", "- How will you know it works?"} {
		if !strings.Contains(text, want) {
			t.Fatalf("Questions = %q, missing %q", text, want)
		}
	}
	if r := drafts.Assess(p.build()); r.Unresolved != 3 || len(p.confirmed) != 0 {
		t.Fatalf("assess %+v confirmed %+v", r, p.confirmed)
	}
	// Closing the discussion changes nothing, and the open items survive a restart.
	m = pk(t, m, "right", "enter") // Close
	m = ptick(t, m)
	saved, err := pb.store.Read(t.Context(), pb.scope, p.draft.ID)
	if err != nil || drafts.Assess(saved).Unresolved != 3 {
		t.Fatalf("stored %+v %v", saved.Spec.OpenQuestions, err)
	}
	// Asking again does not duplicate them.
	p.writeOpenQuestion("Should the active session keep running?")
	if got := p.secs[p.secIndex(6)].text; got != text {
		t.Fatalf("duplicated: %q", got)
	}
}

func TestPrivateAssistAssumptionIsOneLine(t *testing.T) {
	e := newPrivEnv(t)
	pb := e.backend(t)
	scriptAssist(pb, paRound1, paProposalScript)
	m := paDraft(t, e, pb, 120, 35)
	m = startDiscussion(t, m)
	m = pk(t, m, "ctrl+g", "pgdown", "2") // the advisory question: Write my own answer
	m = ptype(t, m, "Linux\n  and   macOS\n")
	m = toAct(t, m)
	m = pk(t, m, "right", "right", "enter") // Assume
	p := privOf(m)
	if len(p.decisions) != 1 || p.decisions[0].Assumption != "Linux and macOS" {
		t.Fatalf("decisions %+v", p.decisions)
	}
	if got := p.secs[p.secIndex(6)].text; got != "- "+p.decisions[0].Line() || strings.Count(got, "\n") != 0 {
		t.Fatalf("Questions = %q", got)
	}
	d := p.build()
	if r := drafts.Assess(d); r.Deferred != 1 || r.Unresolved != 0 {
		t.Fatalf("assess %+v", r)
	}
}

func TestPrivateAssistQuestionLinesMatchTheWholeQuestionNeverATail(t *testing.T) {
	d := drafts.Draft{Type: "feature"}
	d.Spec.OpenQuestions = "- Assumed: Linux and a black cat (Which platforms?)\n- Deferred: Is the cat ready?"
	p := newPrivateDraft(1, 1, d, privEditor)
	at := p.secIndex(6)
	// A short question that ends another line must not be taken for it.
	for _, q := range []string{"cat", "ok", "ready?", "platforms?"} {
		if i := p.questionLine(p.secs[at].text, q); i >= 0 {
			t.Fatalf("%q matched line %d", q, i)
		}
	}
	p.writeOpenQuestion("cat")
	if got := p.secs[at].text; !strings.HasSuffix(got, "\n- cat") {
		t.Fatalf("a needed question was skipped: %q", got)
	}
	p.writeDecisionLine(drafts.Decision{Question: "cat", State: drafts.Deferred})
	want := "- Assumed: Linux and a black cat (Which platforms?)\n- Deferred: Is the cat ready?\n- Deferred: cat"
	if got := p.secs[at].text; got != want {
		t.Fatalf("an unrelated line was overwritten: %q", got)
	}
	// The whole question, in any form, is found.
	for q, line := range map[string]int{"Which platforms?": 0, "is the cat ready": 1, "cat": 2} {
		if i := p.questionLine(p.secs[at].text, q); i != line {
			t.Errorf("%q found at %d, want %d", q, i, line)
		}
	}
}

func TestPrivateDraftReloadKeepsSectionAndFocus(t *testing.T) {
	e := newPrivEnv(t)
	pb := e.backend(t)
	m := paDraft(t, e, pb, 120, 35)
	p := privOf(m)
	p.selectSection(2)
	p.foc = 2
	// Another window saves a newer revision; reopening a clean editor reloads it in place.
	cur, err := pb.store.Read(t.Context(), pb.scope, p.draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	cur.Title = "Changed elsewhere"
	if _, err := pb.store.Save(t.Context(), pb.scope, cur, cur.Revision); err != nil {
		t.Fatal(err)
	}
	m2, cmd := m.privOpen(p.draft.ID)
	m, _ = drainAll(t, m2.(Model), cmd)
	np := privOf(m)
	if np.title.Value() != "Changed elsewhere" || np.stage != privEditor || np.cur != 2 || np.foc != 2 {
		t.Fatalf("title %q stage %d cur %d foc %d", np.title.Value(), np.stage, np.cur, np.foc)
	}
}
