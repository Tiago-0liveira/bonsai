package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/assist"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	agentfake "github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi/fake"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
	gardenfake "github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi/fake"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

type assistRig struct {
	garden *gardenfake.Backend
	agents *agentfake.Backend
}

func assistAccount(id, provider, name, auth string, st agents.StructuredSupport) agentsapi.Account {
	a := wsAccount(id, provider, name, auth)
	a.Structured = st
	return a
}

func assistSet(round int, texts ...string) assist.QuestionSet {
	set := assist.QuestionSet{Kind: "questions", Title: "Questions", Understanding: "You want a faster export."}
	for i, t := range texts {
		q := assist.Question{ID: fmt.Sprintf("r%dq%d", round, i+1), Round: round, Text: t, Why: "It decides the scope of the change."}
		if i == 0 {
			q.Suggestions = []string{"A signed-in user", "An admin", "A scheduled job"}
		}
		set.Questions = append(set.Questions, q)
	}
	return set
}

var assistRound1 = []string{"Who triggers the export?", "Which formats are required?", "How large are the exports?"}

// assistEnv is a connected Garden with the sample drafts, the agents fake with a Claude account that can draft and a
// Codex one that cannot, and the Drafts submenu open. withAgents false leaves the agents backend out.
func assistEnv(t *testing.T, prefs config.Prefs, w, h int, withAgents bool) (Model, assistRig) {
	t.Helper()
	fb := draftsFake(t)
	fb.AssistQuestionsView = gardenapi.AssistQuestionsView{Provider: "claude", Set: assistSet(1, assistRound1...)}
	fb.AssistDraftView = gardenapi.AssistDraftView{Proposal: assist.Proposal{Title: "Faster export"}}
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
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Cleanup(func() {
		theme.Current = theme.Resolve("bonsai", nil)
		applyTheme()
	})
	st, err := config.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	st.Prefs = prefs
	opts := []Option{WithLayout(LayoutRedesign), WithGarden(fb)}
	if withAgents {
		opts = append(opts, WithAgents(ag.API()))
	}
	m := sized(t, New(t.TempDir(), &config.Config{}, st, opts...), w, h)
	m, _ = drainAll(t, m, m.Init())
	m = pressAll(t, m, "enter", "tab")
	if m.gardenScreen() != scrDrafts {
		t.Fatalf("Drafts did not open: screen %d", m.gardenScreen())
	}
	return m, assistRig{fb, ag}
}

func akey(k string) tea.KeyMsg {
	switch k {
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	case "ctrl+d":
		return tea.KeyMsg{Type: tea.KeyCtrlD}
	case "ctrl+k":
		return tea.KeyMsg{Type: tea.KeyCtrlK}
	case "ctrl+u":
		return tea.KeyMsg{Type: tea.KeyCtrlU}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "enter", "esc", "tab", "left", "right", "up", "down":
		return keyOf(k)
	}
	return runeKey(k)
}

// apress sends keys one at a time and runs what each returns, replies included.
func apress(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		var cmd tea.Cmd
		m, cmd = send(t, m, akey(k))
		m = plannerRun(t, m, cmd)
	}
	return m
}

// ahold sends a key and returns its commands without running them, so the reply can be made late.
func ahold(t *testing.T, m Model, k string) (Model, tea.Cmd) {
	t.Helper()
	return send(t, m, akey(k))
}

// deliver feeds replies to Update one by one without running what they return.
func deliver(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	for _, msg := range msgs {
		m, _ = send(t, m, msg)
	}
	return m
}

func assistSession0(m Model) *assistSession { return m.garden.assist.sessions[m.editKeyFor(0)] }

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// newIdea opens the assistant for a new idea and types the description.
func newIdea(t *testing.T, m Model, desc string) Model {
	t.Helper()
	m = apress(t, m, "i")
	if m.garden.drafts.stage != draftsAssist || assistSession0(m) == nil {
		t.Fatalf("i did not open the assistant: stage %d\n%s", m.garden.drafts.stage, view(m))
	}
	return typed(t, m, desc)
}

// askQuestions sends the first round and delivers the reply.
func askQuestions(t *testing.T, m Model) Model {
	t.Helper()
	m = apress(t, m, "ctrl+s")
	if s := assistSession0(m); s == nil || s.stage != assistQuestions {
		t.Fatalf("Ask did not reach the questions\n%s", view(m))
	}
	return m
}

// rereadStatus reads the Garden status again, as a workspace re-entry does.
func rereadStatus(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := m.gardenLoadStatus()
	return plannerRun(t, m, cmd)
}

func noWrites(t *testing.T, fb *gardenfake.Backend) {
	t.Helper()
	for _, call := range []string{"PreviewCapture", "Capture", "PreviewSave", "Save", "PreviewResolve", "Resolve", "PreviewValidate", "Validate", "StartPlanner", "DecidePlan", "PublishPlan"} {
		if n := fb.Count(call); n != 0 {
			t.Errorf("the assistant called %s %d times", call, n)
		}
	}
}

func TestAssistNewIdeaFromSetupThroughQuestionsToProposal(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {120, 35}} {
		for _, ascii := range []bool{false, true} {
			label := fmt.Sprintf("%dx%d ascii=%v", sz[0], sz[1], ascii)
			prefs := config.Prefs{}
			if ascii {
				prefs.AgentGlyphs = "ascii"
			}
			m, rig := assistEnv(t, prefs, sz[0], sz[1], true)
			mustContain(t, label+" list", view(m), "i")
			m = newIdea(t, m, "The export button is slow")
			out := view(m)
			mustContain(t, label+" setup", out, "ELABORATE WITH AI", "Description", "The export button is slow", "Options", "Ask questions", "Draft without questions")
			mustContain(t, label+" disclosure", flat(out), "Sends your description, answers and draft to Claude Code (account work). No repository files are read.")
			mustContain(t, label+" options", flat(out), "Claude Code", "5 questions")
			mustFit(t, label+" setup", m)

			m = askQuestions(t, m)
			if len(rig.garden.AssistQuestionsRequests) != 1 {
				t.Fatalf("%s: %d question requests", label, len(rig.garden.AssistQuestionsRequests))
			}
			req := rig.garden.AssistQuestionsRequests[0]
			if req.Description != "The export button is slow" || req.Round != 1 || req.Settings.Questions != 5 || req.Settings.AccountID != "c1" ||
				req.RequestID != "session-1-seq-1" || req.Draft != "" || req.IssueID != 0 || req.LocalRepoID != gardenfake.Clone {
				t.Fatalf("%s: request %+v", label, req)
			}
			openGlyph, ansGlyph := "·", "●"
			if ascii {
				openGlyph, ansGlyph = ".", "o"
			}
			out = view(m)
			mustContain(t, label+" questions", out, "Round 1", "Who triggers the export?", openGlyph+"  Which formats", "Skipped and don't-know questions stay")
			mustContain(t, label+" suggestions", out, "( ) A signed-in user", "( ) An admin", "( ) Write my own answer", "( ) I don't know", "( ) Skip for now")
			mustFit(t, label+" questions", m)

			// Answer, skip, and mark one unknown.
			m = apress(t, m, "enter", "1", "esc", "esc", "down", "enter", "esc", "3", "esc", "down", "enter", "esc", "2", "esc", "esc")
			out = view(m)
			mustContain(t, label+" marks", out, ansGlyph+"  Who triggers", "-  Which formats", "?  How large")
			mustFit(t, label+" marks", m)

			m = apress(t, m, "ctrl+s")
			s := assistSession0(m)
			if s.stage != assistReview || s.proposal == nil || s.proposal.Title != "Faster export" {
				t.Fatalf("%s: stage %d proposal %+v", label, s.stage, s.proposal)
			}
			mustContain(t, label+" review", view(m), "AI proposal", "Suggested title: Faster export")
			mustFit(t, label+" review", m)
			if len(rig.garden.AssistDraftRequests) != 1 {
				t.Fatalf("%s: %d draft requests", label, len(rig.garden.AssistDraftRequests))
			}
			dr := rig.garden.AssistDraftRequests[0]
			want := []assist.Answer{
				{QuestionID: "r1q1", Question: "Who triggers the export?", State: assist.Answered, Choice: "A signed-in user"},
				{QuestionID: "r1q2", Question: "Which formats are required?", State: assist.Skipped},
				{QuestionID: "r1q3", Question: "How large are the exports?", State: assist.Unknown},
			}
			if fmt.Sprint(dr.Answers) != fmt.Sprint(want) || dr.RequestID != "session-1-seq-2" || dr.Description != "The export button is slow" {
				t.Fatalf("%s: draft request %+v", label, dr)
			}
			noWrites(t, rig.garden)
			m = apress(t, m, "esc", "esc")
			if m.garden.drafts.stage != draftsList || m.garden.assist.open != nil {
				t.Fatalf("%s: Esc did not leave", label)
			}
			mustContain(t, label+" list", view(m), "AI idea in progress")
		}
	}
}

func TestAssistLateReplyAfterCancelIsDropped(t *testing.T) {
	m, _ := assistEnv(t, config.Prefs{}, 80, 24, true)
	m = newIdea(t, m, "Slow export")
	m, cmd := ahold(t, m, "ctrl+s")
	msgs := runCmds(cmd)
	if m.garden.assist.flight == nil {
		t.Fatal("no flight")
	}
	m = apress(t, m, "esc")
	mustContain(t, "confirm", view(m), "Cancel the AI request?")
	if m.garden.assist.flight == nil {
		t.Fatal("Esc alone cancelled")
	}
	m = apress(t, m, "enter")
	s := assistSession0(m)
	if m.garden.assist.flight != nil || s.notice == "" {
		t.Fatalf("cancel: flight %v notice %q", m.garden.assist.flight, s.notice)
	}
	text := s.desc.Value()
	m = deliver(t, m, msgs...)
	if s.stage != assistSetup || len(s.questions) != 0 || s.desc.Value() != text {
		t.Fatalf("late reply changed the session: stage %d, %d questions", s.stage, len(s.questions))
	}
}

func TestAssistLateReplyAfterNewerRequestIsDropped(t *testing.T) {
	m, rig := assistEnv(t, config.Prefs{}, 80, 24, true)
	m = newIdea(t, m, "Slow export")
	m, first := ahold(t, m, "ctrl+s")
	old := runCmds(first)
	m = apress(t, m, "esc", "enter") // cancel
	m, second := ahold(t, m, "ctrl+s")
	cur := runCmds(second)
	f := m.garden.assist.flight
	if f == nil || f.requestID != "session-1-seq-2" {
		t.Fatalf("second flight: %+v", f)
	}
	m = deliver(t, m, old...)
	s := assistSession0(m)
	if len(s.questions) != 0 || m.garden.assist.flight != f || !strings.Contains(s.notice, assistOutOfDate) {
		t.Fatalf("old reply landed: %d questions, notice %q", len(s.questions), s.notice)
	}
	m = deliver(t, m, cur...)
	if len(s.questions) != 3 || m.garden.assist.flight != nil || len(rig.garden.AssistQuestionsRequests) != 2 {
		t.Fatalf("current reply: %d questions, %d requests", len(s.questions), len(rig.garden.AssistQuestionsRequests))
	}
}

func TestAssistReplyForDiscardedOrOtherSessionIsDropped(t *testing.T) {
	m, _ := assistEnv(t, config.Prefs{}, 80, 24, true)
	m = newIdea(t, m, "Slow export")
	m, cmd := ahold(t, m, "ctrl+s")
	msgs := runCmds(cmd)
	s := assistSession0(m)
	q := msgs[0].(assistQuestionsMsg)

	// Another session's ID, same sequence.
	other := q
	other.session = s.id + 7
	m = deliver(t, m, other)
	if m.garden.assist.flight == nil || len(s.questions) != 0 {
		t.Fatal("a reply for another session touched the flight or the session")
	}
	// The right session with a RequestID that is not the flight's: the flight ends, nothing applies.
	bad := q
	bad.view.RequestID = "session-9-seq-9"
	m = deliver(t, m, bad)
	if m.garden.assist.flight != nil || len(s.questions) != 0 || s.stage != assistSetup || !strings.Contains(s.notice, assistOutOfDate) {
		t.Fatalf("mismatched request ID applied: %d questions, notice %q", len(s.questions), s.notice)
	}

	// A discarded session: the reply finds nobody.
	m = apress(t, m, "ctrl+s") // fresh flight, replied at once
	m.garden.assist.discard(m.editKeyFor(0))
	m = deliver(t, m, msgs...)
	if m.garden.assist.sessions[m.editKeyFor(0)] != nil || m.garden.assist.flight != nil {
		t.Fatal("a discarded session came back")
	}
}

func TestAssistDiscardEndsItsFlightAndIgnoresTheReply(t *testing.T) {
	m, _ := assistEnv(t, config.Prefs{}, 80, 24, true)
	m = newIdea(t, m, "Slow export")
	m, cmd := ahold(t, m, "ctrl+s")
	msgs := runCmds(cmd)
	m.garden.assist.discard(m.editKeyFor(0))
	m = deliver(t, m, msgs...)
	if m.garden.assist.flight != nil || len(m.garden.assist.sessions) != 0 {
		t.Fatal("discarded session left state behind")
	}
}

func TestAssistReplyAfterEpochChangeIsDropped(t *testing.T) {
	t.Run("status epoch moved under the reply", func(t *testing.T) {
		m, _ := assistEnv(t, config.Prefs{}, 80, 24, true)
		m = newIdea(t, m, "Slow export")
		m, cmd := ahold(t, m, "ctrl+s")
		msgs := runCmds(cmd)
		m.garden.status.Stamp.Epoch++
		m = deliver(t, m, msgs...)
		s := assistSession0(m)
		if len(s.questions) != 0 || s.stage != assistSetup || m.garden.assist.flight != nil || !strings.Contains(s.notice, assistOutOfDate) {
			t.Fatalf("epoch-changed reply applied: %d questions, notice %q", len(s.questions), s.notice)
		}
	})
	t.Run("association change releases the flight and the screen", func(t *testing.T) {
		m, _ := assistEnv(t, config.Prefs{}, 80, 24, true)
		m = newIdea(t, m, "Slow export")
		m, cmd := ahold(t, m, "ctrl+s")
		msgs := runCmds(cmd)
		v := m.garden.status
		v.Stamp.Epoch++
		v.Stamp.Generation += 10
		m, _ = m.gardenAdopt(v, false)
		m.garden.sub = subDrafts // the reset of the setup flow put the submenu back on Setup
		if m.garden.assist.flight != nil || m.garden.assist.open != nil || m.garden.drafts.stage == draftsAssist {
			t.Fatal("the association change left the assistant running")
		}
		m = deliver(t, m, msgs...)
		if s := assistSession0(m); s == nil || len(s.questions) != 0 {
			t.Fatal("the reply changed the old session")
		}
		// The old session is inactive: the next entry starts a fresh one and says so.
		m = apress(t, m, "i")
		s := assistSession0(m)
		if s == nil || s.stamp.Epoch != v.Stamp.Epoch || !strings.Contains(m.garden.drafts.notice, "another Garden connection") {
			t.Fatalf("stale session reused: %v / %q", s != nil, m.garden.drafts.notice)
		}
	})
}

func TestAssistBoardRefreshAndWorkspaceSwitchDoNotCancelTheFlight(t *testing.T) {
	m, rig := assistEnv(t, config.Prefs{}, 80, 24, true)
	m = newIdea(t, m, "Slow export")
	m, call := ahold(t, m, "ctrl+s")
	f := m.garden.assist.flight
	if f == nil {
		t.Fatal("no flight")
	}
	// A Board/Work refresh supersedes every other Garden read: the assistant's must survive it.
	m, work := m.gardenLoadWork()
	m = plannerRun(t, m, work)
	m, _ = m.selectWorkspace(wsAccounts)
	m, _ = m.selectWorkspace(wsGarden)
	if m.garden.assist.flight != f {
		t.Fatal("a refresh or workspace switch ended the flight")
	}
	// The call runs only now, after the refresh abandoned the Garden read track: its context must be live.
	m = plannerRun(t, m, call)
	s := assistSession0(m)
	if len(s.questions) != 3 || s.stage != assistQuestions || m.garden.assist.flight != nil {
		t.Fatalf("the reply did not land: %d questions, stage %d, err %v", len(s.questions), s.stage, s.err)
	}
	if len(rig.garden.AssistQuestionsRequests) != 1 {
		t.Fatalf("%d requests", len(rig.garden.AssistQuestionsRequests))
	}
}

func TestAssistLeavingTheScreenKeepsTheFlightAndTheReplyLands(t *testing.T) {
	m, _ := assistEnv(t, config.Prefs{}, 80, 24, true)
	m = newIdea(t, m, "Slow export")
	m, call := ahold(t, m, "ctrl+s")
	m = apress(t, m, "esc")
	mustContain(t, "confirm", view(m), "Cancel the AI request?", "l leaves it running")
	m = apress(t, m, "l")
	if m.garden.drafts.stage != draftsList || m.garden.assist.flight == nil {
		t.Fatalf("leave: stage %d flight %v", m.garden.drafts.stage, m.garden.assist.flight)
	}
	m = plannerRun(t, m, call)
	s := assistSession0(m)
	if s.stage != assistQuestions || len(s.questions) != 3 {
		t.Fatalf("reply did not land while away: stage %d", s.stage)
	}
	mustContain(t, "notice", view(m), "An AI reply is ready")
	m = apress(t, m, "i")
	mustContain(t, "resume", view(m), "Who triggers the export?")
}

func TestAssistRunningLineTicksAndStopsWithTheFlight(t *testing.T) {
	m, _ := assistEnv(t, config.Prefs{}, 80, 24, true)
	m = newIdea(t, m, "Slow export")
	m, call := ahold(t, m, "ctrl+s")
	f := m.garden.assist.flight
	mustContain(t, "running", view(m), "Asking Claude Code (default model)… 0s · esc cancel")
	var cmd tea.Cmd
	m, cmd = send(t, m, assistTickMsg{session: f.session, seq: f.seq})
	if cmd == nil || f.secs != 1 {
		t.Fatalf("tick: secs %d, cmd %v", f.secs, cmd)
	}
	mustContain(t, "after a tick", view(m), "Asking Claude Code (default model)… 1s · esc cancel")
	// Another flight's tick does nothing.
	if _, cmd := send(t, m, assistTickMsg{session: f.session, seq: f.seq + 1}); cmd != nil {
		t.Fatal("a foreign tick kept ticking")
	}
	m = plannerRun(t, m, call)
	if _, cmd := send(t, m, assistTickMsg{session: f.session, seq: f.seq}); cmd != nil {
		t.Fatal("the tick outlived its flight")
	}
	line := (Model{}).assistRunning(&assistFlight{provider: "Claude Code", model: "sonnet", effort: "high", secs: 23})
	if line != "Asking Claude Code (sonnet, high)… 23s · esc cancel" {
		t.Fatalf("status line %q", line)
	}
}

func TestAssistDuplicateSubmitIgnoredAndAnotherSessionIsToldToWait(t *testing.T) {
	m, rig := assistEnv(t, config.Prefs{}, 120, 35, true)
	m = newIdea(t, m, "Slow export")
	m, call := ahold(t, m, "ctrl+s")
	var dup tea.Cmd
	m, dup = ahold(t, m, "ctrl+s")
	m, dup2 := ahold(t, m, "ctrl+d")
	if len(runCmds(dup)) != 0 || len(runCmds(dup2)) != 0 {
		t.Fatal("a duplicate submit started another request")
	}
	// Leave without cancelling and elaborate an existing draft: it must wait.
	m = apress(t, m, "esc", "l")
	m = apress(t, m, "enter", "i") // detail of #142, then assist
	s := m.garden.assist.open
	if s == nil || s.isNew || s.number != 142 {
		t.Fatalf("existing draft session: %+v", s)
	}
	m = apress(t, m, "tab", "tab") // options -> ask
	m, c2 := ahold(t, m, "ctrl+s")
	if len(runCmds(c2)) != 0 || !strings.Contains(s.notice, "Another AI request is running.") {
		t.Fatalf("second session: notice %q", s.notice)
	}
	mustContain(t, "wait", view(m), "Another AI request is running.")
	m = plannerRun(t, m, call)
	if len(rig.garden.AssistQuestionsRequests) != 1 {
		t.Fatalf("%d requests, want 1", len(rig.garden.AssistQuestionsRequests))
	}
}

func TestAssistErrorsAreSafeAndRetryUsesANewRequestIDWithTheSameInputs(t *testing.T) {
	m, rig := assistEnv(t, config.Prefs{}, 80, 24, true)
	rig.garden.AssistQuestionsErr = &gardenapi.Error{Code: gardenapi.CodeAssistTimeout, Message: "The assistant did not answer in time.", Next: "Try again."}
	m = newIdea(t, m, "Slow export")
	m = apress(t, m, "ctrl+s")
	s := assistSession0(m)
	if s.stage != assistSetup || s.err == nil || m.garden.assist.flight != nil {
		t.Fatalf("error state: stage %d err %v", s.stage, s.err)
	}
	mustContain(t, "error", view(m), "The assistant did not answer in time.", "Try again.", "R retries")
	rig.garden.AssistQuestionsErr = nil
	m = apress(t, m, "R") // the error moved the focus out of the text field
	if len(rig.garden.AssistQuestionsRequests) != 2 {
		t.Fatalf("%d requests after retry", len(rig.garden.AssistQuestionsRequests))
	}
	a, b := rig.garden.AssistQuestionsRequests[0], rig.garden.AssistQuestionsRequests[1]
	if a.RequestID != "session-1-seq-1" || b.RequestID != "session-1-seq-2" {
		t.Fatalf("request IDs %q %q", a.RequestID, b.RequestID)
	}
	b.RequestID = a.RequestID
	if fmt.Sprint(a) != fmt.Sprint(b) {
		t.Fatalf("retry changed the inputs:\n%+v\n%+v", a, b)
	}
	if s.stage != assistQuestions || s.err != nil {
		t.Fatalf("retry did not land: stage %d err %v", s.stage, s.err)
	}

	// An unmarked error never reaches the screen.
	m2, rig2 := assistEnv(t, config.Prefs{}, 80, 24, true)
	rig2.garden.AssistQuestionsErr = errors.New("boom token ghp_secret https://example.test/x")
	m2 = newIdea(t, m2, "Slow export")
	m2 = apress(t, m2, "ctrl+s")
	out := view(m2)
	mustNotContain(t, "raw error", out, "ghp_secret", "example.test", "boom")
	mustContain(t, "generic", out, "Garden could not complete that request.")
}

func TestAssistSkipUnknownPersistAcrossNavigationAndRoundTwoKeepsRoundOne(t *testing.T) {
	m, rig := assistEnv(t, config.Prefs{}, 120, 35, true)
	m = newIdea(t, m, "Slow export")
	m = askQuestions(t, m)
	m = apress(t, m, "enter", "4") // options, then Write my own
	m = typed(t, m, "Admins")
	m = apress(t, m, "esc", "esc", "down", "enter", "esc", "3", "esc", "down", "enter", "esc", "2", "esc", "esc")
	// Leave, open the session again.
	m = apress(t, m, "esc", "i")
	s := assistSession0(m)
	if s.stage != assistQuestions {
		t.Fatalf("resumed in stage %d", s.stage)
	}
	states := func() string {
		var out []string
		for _, q := range s.questions {
			out = append(out, string(s.answer(q).state()))
		}
		return strings.Join(out, ",")
	}
	if got := states(); got != "answered,skipped,unknown" {
		t.Fatalf("states after navigation: %s", got)
	}
	// Choosing Write my own in a skipped question clears the skip.
	m = apress(t, m, "up", "enter", "1")
	m = typed(t, m, "PDF and CSV")
	if got := states(); got != "answered,answered,unknown" {
		t.Fatalf("typing did not clear the skip: %s", got)
	}
	m = apress(t, m, "esc", "3", "esc") // skipped again, back on the rail
	if got := states(); got != "answered,skipped,unknown" {
		t.Fatalf("skip again: %s", got)
	}

	rig.garden.AssistQuestionsView = gardenapi.AssistQuestionsView{Provider: "claude", Set: assistSet(2, "Which locale for dates?", "Is a progress bar wanted?")}
	m = apress(t, m, "m")
	if len(rig.garden.AssistQuestionsRequests) != 2 {
		t.Fatalf("%d question requests", len(rig.garden.AssistQuestionsRequests))
	}
	r2 := rig.garden.AssistQuestionsRequests[1]
	if r2.Round != 2 || len(r2.Answers) != 3 || r2.Answers[0].State != assist.Answered || r2.Answers[0].Text != "Admins" ||
		r2.Answers[1].State != assist.Skipped || r2.Answers[2].State != assist.Unknown {
		t.Fatalf("round two request: round %d answers %+v", r2.Round, r2.Answers)
	}
	if len(s.questions) != 5 || s.sel != 3 {
		t.Fatalf("round two questions: %d, sel %d", len(s.questions), s.sel)
	}
	out := view(m)
	mustContain(t, "rounds", out, "Round 1", "Round 2", "Which locale for dates?")
	m = apress(t, m, "enter", "4")
	m = typed(t, m, "ISO")
	m = apress(t, m, "ctrl+s")
	dr := rig.garden.AssistDraftRequests[0]
	if len(dr.Answers) != 5 || dr.Answers[0].Text != "Admins" || dr.Answers[1].State != assist.Skipped || dr.Answers[2].State != assist.Unknown ||
		dr.Answers[3].Text != "ISO" || dr.Answers[4].State != assist.Open {
		t.Fatalf("draft after round two: %+v", dr.Answers)
	}
	mustContain(t, "review", view(m), "AI proposal")
}

func TestAssistKeyOwnershipInEveryTextField(t *testing.T) {
	m, _ := assistEnv(t, config.Prefs{}, 80, 24, true)
	// Description.
	m = apress(t, m, "i")
	if m.currentShellFocus() != shellInput || !m.gardenTyping() {
		t.Fatalf("Setup does not own the keyboard (focus %v)", m.currentShellFocus())
	}
	for _, k := range []string{"q", "?", "n", "R"} {
		var cmd tea.Cmd
		m, cmd = send(t, m, akey(k))
		if isQuit(cmd) {
			t.Fatalf("%q quit from the description", k)
		}
	}
	if got := assistSession0(m).desc.Value(); got != "q?nR" {
		t.Fatalf("description %q", got)
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2"), Alt: true})
	if m.workspace != wsGarden {
		t.Fatal("alt+digit left the Garden from a text field")
	}
	// ctrl+k and ctrl+u are textarea line kills here.
	m = apress(t, m, "left", "left", "ctrl+k")
	if got := assistSession0(m).desc.Value(); got != "q?n" {
		t.Fatalf("ctrl+k: %q", got) // alt+2 typed a 2: the field owns the key
	}
	m = apress(t, m, "ctrl+u")
	if got := assistSession0(m).desc.Value(); got != "" {
		t.Fatalf("ctrl+u: %q", got)
	}
	m = typed(t, m, "Slow export")

	// The options dialog: the guidance input.
	m = apress(t, m, "tab", "enter")
	s := assistSession0(m)
	if !s.optsOpen || !m.gardenTyping() {
		t.Fatal("options did not open")
	}
	mustContain(t, "options", view(m), "AI OPTIONS", "Claude", "personal", "cannot draft: codex keeps a shell", "Questions", "Guidance")
	mustFit(t, "options", m)
	m = apress(t, m, "tab", "tab", "tab", "q", "?") // account -> form -> (form fields) ... -> guidance eventually
	m = apress(t, m, "esc")
	if s.optsOpen {
		t.Fatal("Esc did not close the options")
	}

	// The answer editor.
	m = apress(t, m, "tab", "tab")
	m = askQuestions(t, m)
	m = apress(t, m, "enter", "4")
	if s.zone != zoneText {
		t.Fatal("choosing Write my own did not open the text field")
	}
	for _, k := range []string{"q", "?", "s", "u", "x", "m", "1", "2"} {
		var cmd tea.Cmd
		m, cmd = send(t, m, akey(k))
		if isQuit(cmd) {
			t.Fatalf("%q quit from the answer editor", k)
		}
	}
	if got := s.ta.Value(); got != "q?sux"+"m12" {
		t.Fatalf("answer %q", got)
	}
	m = apress(t, m, "ctrl+u")
	m = typed(t, m, "abc")
	m = apress(t, m, "left", "ctrl+k")
	if got := s.ta.Value(); got != "ab" {
		t.Fatalf("ctrl+k in the answer: %q", got)
	}
	if st := s.answer(s.questions[0]).state(); st != assist.Answered {
		t.Fatalf("state %s", st)
	}
	m = apress(t, m, "esc", "esc")
	if s.zone != zoneRail || m.garden.drafts.stage != draftsAssist || s.stage != assistQuestions {
		t.Fatal("Esc twice did not return to the rail")
	}
	// On the rail, q and ? still do nothing.
	for _, k := range []string{"q", "?"} {
		var cmd tea.Cmd
		m, cmd = send(t, m, akey(k))
		if isQuit(cmd) {
			t.Fatalf("%q quit from the rail", k)
		}
	}
	if m.currentShellFocus() != shellInput || !m.gardenTyping() {
		t.Fatal("the rail released the keyboard")
	}
}

func TestAssistChoiceNotesCapsAndNavigation(t *testing.T) {
	m, _ := assistEnv(t, config.Prefs{}, 120, 35, true)
	m = newIdea(t, m, "Slow export")
	m = askQuestions(t, m)
	s := assistSession0(m)
	m = apress(t, m, "enter", "2")
	if a := s.answer(s.questions[0]); a.sel != selChoice || a.pick != 1 || a.state() != assist.Answered || s.zone != zoneText {
		t.Fatalf("suggestion 2: %+v zone %d", a, s.zone)
	}
	m = apress(t, m, "pgdown")
	if s.sel != 1 || s.zone != zoneText {
		t.Fatalf("pgdown from text: sel %d zone %d", s.sel, s.zone)
	}
	m = apress(t, m, "pgup")
	if s.sel != 0 || s.zone != zoneOptions || s.ta.Value() != "" {
		t.Fatalf("pgup from text: sel %d zone %d value %q", s.sel, s.zone, s.ta.Value())
	}
	m = apress(t, m, "2")
	// Answers are capped in bytes: 1500 two-byte runes are 3000 bytes.
	m = apress(t, m, "ctrl+u")
	m, _ = send(t, m, runeKey(strings.Repeat("é", 1500)))
	if len(s.ta.Value()) > assist.MaxAnswerBytes {
		t.Fatalf("answer holds %d bytes", len(s.ta.Value()))
	}
	// A suggestion longer than the cap is cut on a rune boundary.
	if got := capText(strings.Repeat("é", 2000), assist.MaxAnswerBytes); len(got) != assist.MaxAnswerBytes {
		t.Fatalf("capText %d", len(got))
	}
	if got := capText("aé", 2); got != "a" {
		t.Fatalf("capText split a rune: %q", got)
	}
}

func TestAssistNoFurtherQuestionsAndTheSessionLimit(t *testing.T) {
	m, rig := assistEnv(t, config.Prefs{}, 80, 24, true)
	rig.garden.AssistQuestionsView = gardenapi.AssistQuestionsView{Provider: "claude", Set: assist.QuestionSet{Understanding: "Clear enough."}}
	m = newIdea(t, m, "Slow export")
	m = askQuestions(t, m)
	mustContain(t, "empty", view(m), "No further questions — draft now.")
	mustFit(t, "empty", m)
	m = apress(t, m, "ctrl+s")
	if assistSession0(m).stage != assistReview || len(rig.garden.AssistDraftRequests) != 1 {
		t.Fatal("draft without questions failed")
	}

	m, rig = assistEnv(t, config.Prefs{}, 80, 24, true)
	m = newIdea(t, m, "Slow export")
	m = askQuestions(t, m)
	s := assistSession0(m)
	for i := len(s.questions); i < assist.MaxSessionQuestions; i++ {
		s.questions = append(s.questions, assist.Question{ID: fmt.Sprintf("x%d", i), Round: 1, Text: "Filler"})
	}
	m = apress(t, m, "m")
	if len(rig.garden.AssistQuestionsRequests) != 1 || !strings.Contains(s.notice, "most it allows") {
		t.Fatalf("past the limit: %d requests, notice %q", len(rig.garden.AssistQuestionsRequests), s.notice)
	}
}

func TestAssistDraftWithoutQuestionsFromSetup(t *testing.T) {
	m, rig := assistEnv(t, config.Prefs{}, 80, 24, true)
	m = newIdea(t, m, "Slow export")
	m = apress(t, m, "ctrl+d")
	s := assistSession0(m)
	if s.stage != assistReview || len(rig.garden.AssistDraftRequests) != 0 && rig.garden.AssistDraftRequests[0].Description != "Slow export" {
		t.Fatalf("stage %d", s.stage)
	}
	if len(rig.garden.AssistQuestionsRequests) != 0 || len(rig.garden.AssistDraftRequests) != 1 {
		t.Fatal("ctrl+d asked questions or sent no draft request")
	}
}

func TestAssistRefusals(t *testing.T) {
	t.Run("new idea without Write access", func(t *testing.T) {
		m, rig := assistEnv(t, config.Prefs{}, 120, 35, true)
		rig.garden.StatusView.Access = gardenapi.AccessRead
		m = rereadStatus(t, m)
		m = apress(t, m, "i")
		mustContain(t, "refusal", view(m), "Write access")
		if m.garden.drafts.stage != draftsList || len(m.garden.assist.sessions) != 0 {
			t.Fatal("a refused start opened a session")
		}
	})
	t.Run("existing draft without Write access", func(t *testing.T) {
		m, rig := assistEnv(t, config.Prefs{}, 120, 35, true)
		rig.garden.StatusView.Access = gardenapi.AccessRead
		m = rereadStatus(t, m)
		m = apress(t, m, "enter", "i")
		if m.garden.drafts.stage != draftsDetail || !strings.Contains(m.garden.drafts.notice, "Write access") || rig.garden.Count("Document") != 0 {
			t.Fatalf("stage %d notice %q", m.garden.drafts.stage, m.garden.drafts.notice)
		}
	})
	t.Run("agents unavailable", func(t *testing.T) {
		m, _ := assistEnv(t, config.Prefs{}, 120, 35, false)
		m = apress(t, m, "i")
		if m.garden.drafts.stage != draftsList || !strings.Contains(m.garden.drafts.notice, "unavailable") {
			t.Fatalf("stage %d notice %q", m.garden.drafts.stage, m.garden.drafts.notice)
		}
	})
	t.Run("a capture that may exist", func(t *testing.T) {
		m, _ := assistEnv(t, config.Prefs{}, 120, 35, true)
		m.garden.drafts.pending = []gardenapi.CaptureView{{OperationID: "op-1"}}
		m = apress(t, m, "i")
		if m.garden.drafts.stage != draftsList || !strings.Contains(m.garden.drafts.notice, "may already exist") {
			t.Fatalf("notice %q", m.garden.drafts.notice)
		}
	})
	t.Run("competing specification heads", func(t *testing.T) {
		m, rig := assistEnv(t, config.Prefs{}, 120, 35, true)
		m = apress(t, m, "down", "down", "enter", "i")
		d := m.garden.drafts
		if d.stage != draftsDetail || !strings.Contains(d.notice, "competing heads") || len(d.editors) != 0 || len(m.garden.assist.sessions) != 0 {
			t.Fatalf("stage %d notice %q", d.stage, d.notice)
		}
		if rig.garden.Count("AssistQuestions")+rig.garden.Count("AssistDraft") != 0 {
			t.Fatal("assist ran on a conflicted specification")
		}
	})
	t.Run("no account can draft", func(t *testing.T) {
		m, rig := assistEnv(t, config.Prefs{}, 80, 24, true)
		rig.agents.Accounts.Items = rig.agents.Accounts.Items[:1] // Codex only
		m = newIdea(t, m, "Slow export")
		m = apress(t, m, "ctrl+s")
		mustContain(t, "unsupported", view(m), "cannot draft: codex keeps a shell")
		if rig.garden.Count("AssistQuestions") != 0 || m.garden.assist.flight != nil {
			t.Fatal("an unsupported account was asked")
		}
	})
	t.Run("signed out offers the login command", func(t *testing.T) {
		m, rig := assistEnv(t, config.Prefs{}, 80, 24, true)
		rig.agents.Accounts.Items[1] = assistAccount("c1", "claude", "work", "logged_out", agents.StructuredSupport{Supported: true})
		rig.agents.Accounts.Infos["c1"] = agents.AccountInfo{Options: map[string]any{"auth_status": "logged_out"}}
		m = newIdea(t, m, "Slow export")
		m = apress(t, m, "ctrl+s")
		if rig.garden.Count("AssistQuestions") != 0 || m.garden.assist.flight != nil {
			t.Fatal("a signed-out account was asked")
		}
		mustContain(t, "sign-in", view(m), "needs a sign-in first")
	})
	t.Run("empty description", func(t *testing.T) {
		m, rig := assistEnv(t, config.Prefs{}, 80, 24, true)
		m = apress(t, m, "i", "ctrl+s")
		mustContain(t, "describe", view(m), "Describe the idea first.")
		if rig.garden.Count("AssistQuestions") != 0 {
			t.Fatal("an empty idea was sent")
		}
	})
}

func TestAssistExistingDraftUsesTheEditorsUnsavedText(t *testing.T) {
	m, rig := assistEnv(t, config.Prefs{}, 120, 35, true)
	// Open the editor on #142 and type: the text is dirty and unsaved.
	m = apress(t, m, "enter", "e")
	e := m.garden.drafts.edit
	if e == nil {
		t.Fatalf("no editor\n%s", view(m))
	}
	m = apress(t, m, "enter")
	m = typed(t, m, "UNSAVED-MARKER")
	m = apress(t, m, "esc") // text -> rail
	// i on the rail opens the assistant over the same editor.
	m = apress(t, m, "i")
	s := m.garden.assist.open
	if s == nil || s.isNew || s.number != 142 || s.from != draftsEditor {
		t.Fatalf("assistant from the editor rail: %+v", s)
	}
	if rig.garden.Count("Document") != 1 {
		t.Fatalf("the editor's text was re-read: %d Document calls", rig.garden.Count("Document"))
	}
	mustContain(t, "existing setup", flat(view(m)), "ELABORATE #142 WITH AI", "Per-repository agent budget limits", "current text, including unsaved edits")
	mustNotContain(t, "existing setup", view(m), "Description")
	m = apress(t, m, "ctrl+s")
	req := rig.garden.AssistQuestionsRequests[0]
	if !strings.Contains(req.Draft, "UNSAVED-MARKER") || req.IssueID != 142 || req.Number != 142 || req.Title != "Per-repository agent budget limits" ||
		req.Type != garden.TypeFeature || req.Description != "" {
		t.Fatalf("request %+v", req)
	}
	if e.dirty() == false {
		t.Fatal("the assistant clobbered the editor")
	}
	// Esc leaves to the editor and the unsaved text is intact.
	m = apress(t, m, "esc")
	if m.garden.drafts.stage != draftsEditor || !strings.Contains(m.garden.drafts.edit.content(), "UNSAVED-MARKER") {
		t.Fatalf("stage %d", m.garden.drafts.stage)
	}
	noWrites(t, rig.garden)
}

func TestAssistExistingDraftFromDetailLoadsTheDocumentOnce(t *testing.T) {
	m, rig := assistEnv(t, config.Prefs{}, 80, 24, true)
	m = apress(t, m, "down", "enter", "i") // #143: a bug with only a title
	s := m.garden.assist.open
	if s == nil || s.number != 143 || rig.garden.Count("Document") != 1 || m.garden.drafts.editors[m.editKeyFor(143)] == nil {
		t.Fatalf("assistant for #143: %+v", s)
	}
	if s.typ != garden.TypeBug {
		t.Fatalf("type %q", s.typ)
	}
	mustFit(t, "existing setup", m)
	m = apress(t, m, "esc")
	if m.garden.drafts.stage != draftsDetail {
		t.Fatalf("Esc returned to stage %d", m.garden.drafts.stage)
	}
}

func TestAssistScreensFitAtEverySizeAndGlyphSet(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {120, 35}} {
		for _, ascii := range []bool{false, true} {
			label := fmt.Sprintf("%dx%d ascii=%v", sz[0], sz[1], ascii)
			prefs := config.Prefs{}
			if ascii {
				prefs.AgentGlyphs = "ascii"
			}
			m, rig := assistEnv(t, prefs, sz[0], sz[1], true)
			long := strings.Repeat("A very long question about formats and limits that keeps going. ", 4)
			rig.garden.AssistQuestionsView = gardenapi.AssistQuestionsView{Provider: "claude", Set: assistSet(1, long, "Second?", "Third?", "Fourth?", "Fifth?")}
			m = newIdea(t, m, strings.Repeat("A long description line. ", 30))
			mustFit(t, label+" setup", m)
			m = apress(t, m, "tab", "enter")
			mustFit(t, label+" options", m)
			out := view(m)
			if ascii {
				mustNotContain(t, label+" ascii options", out, "✹", "▸", "‹", "›", "…")
				mustContain(t, label+" options", out, "C Claude", "cannot draft")
			} else {
				mustContain(t, label+" options", out, "✹ Claude")
			}
			m = apress(t, m, "esc", "ctrl+s")
			mustFit(t, label+" questions", m)
			out = view(m)
			mustContain(t, label+" rail", out, "Round 1", "A very long question")
			if ascii {
				mustNotContain(t, label+" ascii", out, "✓", "·", "▸", "▼", "‹", "…")
			}
			m = apress(t, m, "enter")
			mustFit(t, label+" editor", m)
			m = apress(t, m, "esc", "ctrl+s")
			mustFit(t, label+" review", m)
			// The running line at both sizes.
			m, _ = ahold(t, apress(t, m, "esc"), "i")
			m = apress(t, m, "esc")
		}
	}
}

func TestAssistRunningScreenFitsNarrow(t *testing.T) {
	m, _ := assistEnv(t, config.Prefs{}, 80, 24, true)
	m = newIdea(t, m, "Slow export")
	m, _ = ahold(t, m, "ctrl+s")
	mustFit(t, "running setup", m)
	mustContain(t, "running", view(m), "esc cancel")
	m = apress(t, m, "esc")
	mustFit(t, "confirm", m)
	mustContain(t, "confirm hints", view(m), "enter cancel request")
}

func TestAssistRailRoundsGroupAndKeepTheSelectedRoundOpen(t *testing.T) {
	m, rig := assistEnv(t, config.Prefs{}, 80, 24, true)
	m = newIdea(t, m, "Slow export")
	m = askQuestions(t, m)
	rig.garden.AssistQuestionsView = gardenapi.AssistQuestionsView{Provider: "claude", Set: assistSet(2, "Which locale for dates?")}
	m = apress(t, m, "m")
	out := view(m)
	mustContain(t, "round two open", out, "Round 1  0/3", "Round 2  0/1", "Which locale")
	mustNotContain(t, "round one collapsed", out, "Who triggers the export?  ")
	m = apress(t, m, "up")
	out = view(m)
	mustContain(t, "round one open", out, "Who triggers")
	mustFit(t, "rounds", m)
}
