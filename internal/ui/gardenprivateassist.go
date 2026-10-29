package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden/assist"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/drafts"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/spec"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// The optional assistant beside a private draft. The specification stays visible and editable the whole time: the
// assistant only ever proposes, and nothing reaches the text until the person applies it. Every request is scoped to
// the draft's ID, the revision it started from and a per-draft request generation, and its reply is applied
// section by section against what the request sent, so text typed while it ran is never overwritten. Pending
// questions and proposals live in memory only; what the person confirmed travels with the draft's own saves.

type paStage int

const (
	paInvite    paStage = iota // account, model and effort, and what would be sent; nothing is sent yet
	paIdle                     // between requests: ask, retry or close
	paQuestions                // one focused question of the current round
	paProposal                 // a proposal waiting for an explicit apply
)

// maxPaneQuestions is the most questions one round asks of the person.
const maxPaneQuestions = 3

// paAction is a button of the pane's action row.
type paAction int

const (
	actAsk paAction = iota
	actRetry
	actClose
	actApplyAnswers
	actDefer
	actAssume
	actWhy
	actApplyProposal
	actDiscard
)

type privFlight struct {
	gen       uint64
	revision  uint64 // the durable revision the request started from
	requestID string
	draft     bool
	round     int
	sentText  string
	stamp     gardenapi.Stamp
	cancel    context.CancelFunc
	secs      int
}

type privAssistant struct {
	stage paStage
	// s holds the options dialog and the round's questions and answers. It is the same state the GitHub draft
	// assistant uses, so its answer panel and navigation are shared rather than copied.
	s        *assistSession
	invBtn   int // invite stage: -1 in the options, 0 Keep writing myself, 1 Start discussion
	settings gardenapi.AssistSettings

	round     int // the last round that landed
	flight    *privFlight
	last      *privSent
	cancelled bool
	err       *gardenapi.Error
	notice    string
	unseen    bool // a reply landed while the person was elsewhere

	act   int
	onAct bool

	history   []assist.Answer // skipped and unknown answers of earlier rounds: not decisions, never persisted
	sent      []assist.Answer // the answers the proposal's request carried
	decided   int             // questions of the last round dropped because they were already decided
	proposal  *assist.Proposal
	review    *reviewState
	startNext bool // start the first round once the draft has been saved
}

// privSent is what Retry sends again, with a fresh generation and the draft as it is then.
type privSent struct{ draft bool }

type privAssistMsg struct {
	token uint64
	id    drafts.ID
	gen   uint64
	draft bool
	qv    gardenapi.AssistQuestionsView
	dv    gardenapi.AssistDraftView
	err   error
}

type privAssistTickMsg struct{ token, gen uint64 }

// privAssistTickCmd advances the elapsed seconds of one flight. Tests skip it by name.
func privAssistTickCmd(token, gen uint64) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(time.Second)
		return privAssistTickMsg{token, gen}
	}
}

func privNorm(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if !unicode.IsPunct(r) {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// Drafts state -----------------------------------------------------------------------

// secIndex is the position in p.secs of fixed specification field f.
func (p *privateDraft) secIndex(f int) int {
	for i, e := range p.secs {
		if e.field == f {
			return i
		}
	}
	return -1
}

// fixedText is the working text of fixed field f, including edits not yet flushed to the section.
func (p *privateDraft) fixedText(f int) (string, bool) {
	p.flush()
	if i := p.secIndex(f); i >= 0 {
		return p.secs[i].text, true
	}
	return "", false
}

func (p *privateDraft) specText() string {
	p.flush()
	return spec.Render(privSpecOf(p.secs))
}

// decidedQuestion says the person already answered or settled q, so it is not asked again.
func (p *privateDraft) decidedQuestion(text string) bool {
	key := privNorm(text)
	for _, a := range p.confirmed {
		if privNorm(a.Question) == key {
			return true
		}
	}
	for _, d := range p.decisions {
		if privNorm(d.Question) == key {
			return true
		}
	}
	return false
}

// paPending counts the scope and acceptance questions still waiting for a decision. Pending questions are not
// stored, so only the open round counts: they hold the draft back from approval until applied or left open.
func (p *privateDraft) paPending() int {
	pa := p.pa
	if pa == nil || (pa.stage != paQuestions && pa.stage != paProposal) {
		return 0
	}
	n := 0
	for _, q := range pa.s.questions {
		if !assist.AdvisoryArea(q.Area) {
			n++
		}
	}
	return n
}

func (p *privateDraft) paActive() bool { return p.pa != nil && p.pa.stage != paInvite }

// Entry --------------------------------------------------------------------------------

// privInvite opens the invitation, or the pane when a discussion already exists. Nothing is sent.
func (m Model) privInvite(p *privateDraft) (Model, tea.Cmd) {
	if why := m.assistUnavailable(); why != "" {
		p.notice = why + " Keep writing; nothing is lost."
		return m, nil
	}
	if _, ok := m.privateDrafts(); !ok {
		p.notice = "Private drafts are unavailable here, so the assistant cannot work from this draft."
		return m, nil
	}
	p.text, p.titleEditing = false, false
	p.ta.Blur()
	p.foc = p.focInvite()
	if p.pa != nil {
		p.pane = p.pa.stage != paInvite
		return m, nil
	}
	s := newAssistSession(0, editKey{}, m.garden.status.Stamp, false, 0, strings.TrimSpace(p.title.Value()), captureTypes[p.typ], draftsList)
	s.private, s.hideWhy = p, true
	s.opts = newAssistOptions(m.agents.api.Accounts, m.agents.usage, m.agentGlyphsASCII(), m.assistDefaults())
	s.opts.maxQuestions = maxPaneQuestions
	s.opts.questions = min(s.opts.questions, maxPaneQuestions)
	s.opts.setFocus(optAccount)
	p.pa = &privAssistant{stage: paInvite, s: s, invBtn: -1}
	return m, s.opts.Init()
}

func (m Model) paClose(p *privateDraft) Model {
	if pa := p.pa; pa != nil && pa.flight != nil {
		pa.flight.cancel()
	}
	p.pa, p.pane = nil, false
	return m
}

// paCancel ends a flight without touching any text; a reply that still arrives is dropped.
func (m Model) paCancel(p *privateDraft) Model {
	pa := p.pa
	if pa == nil || pa.flight == nil {
		return m
	}
	pa.flight.cancel()
	pa.flight, pa.cancelled = nil, true
	pa.err = nil
	pa.notice = "Cancelled. Nothing was changed. Retry when you like."
	return m
}

// paStart begins the first round after an explicit Start discussion.
func (m Model) paStart(p *privateDraft) (Model, tea.Cmd) {
	pa := p.pa
	s := pa.s
	settings, cmd, ok := m.assistPrepare(s, false)
	if !ok {
		pa.err = nil
		return m, cmd
	}
	settings.Questions = min(max(1, settings.Questions), maxPaneQuestions)
	pa.settings = settings
	if p.draft.ID == "" {
		if p.blank() {
			s.notice = "Write a few words first, so there is something to discuss."
			return m, nil
		}
		// A request is scoped to a stored draft: save it now and start when the save lands.
		pa.startNext = true
		s.notice = "Saving the draft first…"
		if p.inflight {
			return m, nil
		}
		return m.privStartSave(p)
	}
	s.notice = ""
	pa.stage, pa.invBtn = paIdle, -1
	p.pane = false
	p.foc = p.cur
	return m.paSend(p, false)
}

// Requests -----------------------------------------------------------------------------

// paAnswers is what a request tells the assistant the person has decided. Confirmed answers come first and the
// round in progress last; when the session limit is exceeded the oldest confirmed answers are the ones left out,
// so the round in progress is always sent.
func (m Model) paAnswers(p *privateDraft, draft bool) []assist.Answer {
	pa := p.pa
	var out []assist.Answer
	for i, a := range p.confirmed {
		out = append(out, assist.Answer{QuestionID: fmt.Sprintf("c%d", i+1), Question: a.Question, State: assist.Answered, Text: capText(a.Answer, assist.MaxAnswerBytes)})
	}
	if draft {
		out = append(out, pa.s.answerList()...)
	} else {
		// A question already settled or set aside must not come back. Deferrals travel as skipped so the
		// assistant can see them; they never become answers.
		for i, d := range p.decisions {
			out = append(out, assist.Answer{QuestionID: fmt.Sprintf("d%d", i+1), Question: d.Question, State: assist.Skipped})
		}
		out = append(out, pa.history...)
	}
	limit := assist.MaxSessionQuestions
	if !draft {
		limit--
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

// paSend issues one request under a new generation. Two requests never overlap.
func (m Model) paSend(p *privateDraft, draft bool) (Model, tea.Cmd) {
	pa := p.pa
	g := &m.garden
	if pa == nil || pa.flight != nil || g.api == nil {
		return m, nil
	}
	if p.draft.ID == "" || p.draft.Revision == 0 {
		pa.notice = "Save the draft first (Ctrl+S); the assistant works from a stored draft."
		return m, nil
	}
	round := pa.round + 1
	p.paGen++ // never reset: a closed and reopened discussion must not reuse an earlier request's identity
	gen := p.paGen
	text := p.specText()
	stamp := g.status.Stamp
	ctx := gardenapi.AssistContext{
		Stamp: stamp, RequestID: fmt.Sprintf("priv-%d-%d", p.token, gen),
		PrivateDraft: &gardenapi.PrivateDraftRef{ID: p.draft.ID, Revision: p.draft.Revision, Generation: gen},
		Title:        strings.TrimSpace(p.title.Value()), Type: captureTypes[p.typ], Description: p.desc.Value(),
		Draft: text, Answers: m.paAnswers(p, draft), Settings: pa.settings,
	}
	cctx, cancel := context.WithCancel(context.Background())
	f := &privFlight{gen: gen, revision: p.draft.Revision, requestID: ctx.RequestID, draft: draft, round: round, sentText: text, stamp: stamp, cancel: cancel}
	pa.flight, pa.cancelled, pa.err, pa.notice = f, false, nil, ""
	pa.last = &privSent{draft: draft}
	if draft {
		pa.sent = pa.s.answerList()
	}
	api, token, id := g.api, p.token, p.draft.ID
	var call tea.Cmd
	if draft {
		req := gardenapi.AssistDraftRequest{AssistContext: ctx}
		call = func() tea.Msg {
			v, err := api.AssistDraft(cctx, req)
			return privAssistMsg{token: token, id: id, gen: gen, draft: true, dv: v, err: err}
		}
	} else {
		req := gardenapi.AssistQuestionsRequest{AssistContext: ctx, Round: round}
		call = func() tea.Msg {
			v, err := api.AssistQuestions(cctx, req)
			return privAssistMsg{token: token, id: id, gen: gen, qv: v, err: err}
		}
	}
	return m, tea.Batch(call, privAssistTickCmd(token, gen))
}

func (m Model) paRetry(p *privateDraft) (Model, tea.Cmd) {
	pa := p.pa
	if pa == nil || pa.last == nil || pa.flight != nil || (pa.err == nil && !pa.cancelled) {
		return m, nil
	}
	return m.paSend(p, pa.last.draft)
}

func (m Model) onPrivAssistTick(msg privAssistTickMsg) (Model, tea.Cmd) {
	p := m.garden.drafts.private
	if p == nil || p.token != msg.token || p.pa == nil || p.pa.flight == nil || p.pa.flight.gen != msg.gen {
		return m, nil // the flight ended: stop ticking
	}
	p.pa.flight.secs++
	return m, privAssistTickCmd(msg.token, msg.gen)
}

// Replies ------------------------------------------------------------------------------

func (m Model) onPrivAssist(msg privAssistMsg) (Model, tea.Cmd) {
	p := m.garden.drafts.private
	if p == nil || p.token != msg.token || p.draft.ID != msg.id || p.pa == nil {
		return m, nil
	}
	pa := p.pa
	f := pa.flight
	if f == nil || f.gen != msg.gen {
		// Cancelled, superseded or closed: the reply says nothing and changes nothing.
		return m, nil
	}
	pa.flight = nil
	f.cancel()
	if msg.err != nil {
		if errors.Is(msg.err, context.Canceled) {
			return m, nil
		}
		pa.err = safeGardenError(msg.err)
		return m, nil
	}
	var ref *gardenapi.PrivateDraftRef
	var st gardenapi.Stamp
	var requestID string
	if msg.draft {
		ref, st, requestID = msg.dv.PrivateDraft, msg.dv.Stamp, msg.dv.RequestID
	} else {
		ref, st, requestID = msg.qv.PrivateDraft, msg.qv.Stamp, msg.qv.RequestID
	}
	cur := m.garden.status.Stamp
	if ref == nil || ref.ID != msg.id || ref.Generation != f.gen || ref.Revision != f.revision || requestID != f.requestID ||
		st.LocalRepoID != f.stamp.LocalRepoID || st.GardenID != f.stamp.GardenID || st.Epoch != f.stamp.Epoch ||
		cur.LocalRepoID != f.stamp.LocalRepoID || cur.GardenID != f.stamp.GardenID || cur.Epoch != f.stamp.Epoch {
		pa.notice = assistOutOfDate
		return m, nil
	}
	pa.err, pa.notice, pa.cancelled = nil, "", false
	pa.unseen = !p.pane
	if msg.draft {
		return m.paLandProposal(p, f, msg.dv)
	}
	return m.paLandQuestions(p, f, msg.qv)
}

func (m Model) paLandQuestions(p *privateDraft, f *privFlight, v gardenapi.AssistQuestionsView) (Model, tea.Cmd) {
	pa := p.pa
	s := pa.s
	if v.Round != f.round {
		pa.notice = assistOutOfDate
		return m, nil
	}
	pa.round = f.round
	s.questions, s.answers, pa.decided = nil, nil, 0
	for _, q := range v.Set.Questions {
		if p.decidedQuestion(q.Text) {
			pa.decided++
			continue
		}
		s.questions = append(s.questions, q)
	}
	s.understanding = strings.TrimSpace(v.Set.Understanding)
	s.sel, pa.onAct = 0, false
	if len(s.questions) == 0 {
		pa.stage = paIdle
		pa.notice = "Nothing new to ask: what is left is already decided."
		return m, nil
	}
	pa.stage = paQuestions
	s.resetPanel()
	s.setZone(defaultZone(s.questions[0]))
	return m, nil
}

func (m Model) paLandProposal(p *privateDraft, f *privFlight, v gardenapi.AssistDraftView) (Model, tea.Cmd) {
	pa := p.pa
	pr := v.Proposal
	pa.proposal = &pr
	pa.review = newAssistReview(f.sentText, pr)
	pa.s.review, pa.s.proposal = pa.review, pa.proposal
	pa.stage, pa.onAct, pa.act = paProposal, false, 0
	return m, nil
}

// Applying -----------------------------------------------------------------------------

func (p *privateDraft) markChanged(i int) {
	if p.changed == nil {
		p.changed = map[int]bool{}
	}
	p.changed[i] = true
}

func (m Model) paApplyProposal(p *privateDraft) (Model, tea.Cmd) {
	pa := p.pa
	r := pa.review
	if r == nil {
		return m, nil
	}
	// Compare what the request sent, what the draft holds now and the proposal: a section the person edited
	// since the request began defaults to their text, and only an explicit choice replaces it.
	secs := reviewSectionsOf(r, p.fixedText)
	var names []string
	differs := 0
	for i, sec := range secs {
		if sec.state != '=' {
			differs++
		}
		if !sec.accept {
			continue
		}
		at := p.secIndex(i)
		if at < 0 {
			continue
		}
		p.secs[at].text = r.proposed[i]
		p.markChanged(at)
		names = append(names, privFieldLabel(i))
	}
	if differs > 0 && len(names) == 0 {
		pa.notice = "No section is chosen: press Enter on one to take it, or discard the proposal."
		return m, nil
	}
	if len(names) > 0 {
		p.load(p.cur)
	}
	// Applying is the person's confirmation of what they answered, so those answers now belong to the draft.
	for _, a := range pa.sent {
		if a.State == assist.Answered && strings.HasPrefix(a.QuestionID, "r") {
			p.confirm(a)
		}
	}
	// A skipped or unknown answer decides nothing: its question goes into the Questions section as an open item, so
	// it keeps blocking approval in the saved draft whatever happened to the proposal's own Questions text.
	for _, a := range pa.sent {
		if (a.State == assist.Skipped || a.State == assist.Unknown) && strings.HasPrefix(a.QuestionID, "r") {
			pa.history = append(pa.history, a)
			p.writeOpenQuestion(a.Question)
		}
	}
	pa.proposal, pa.review, pa.s.review, pa.s.proposal = nil, nil, nil, nil
	pa.s.questions, pa.s.answers, pa.sent = nil, nil, nil
	pa.stage, pa.onAct, pa.act = paIdle, false, 0
	switch len(names) {
	case 0:
		pa.notice = "✓ Answers kept. The draft already says it."
	default:
		pa.notice = fmt.Sprintf("✓ Applied to %s.", strings.Join(names, ", "))
	}
	m, cmd := m.privEdited(p)
	return m, cmd
}

// confirm records an answered question as the person's confirmed answer, replacing an earlier one.
func (p *privateDraft) confirm(a assist.Answer) {
	text := strings.TrimSpace(a.Text)
	switch {
	case a.Choice != "" && text != "":
		text = a.Choice + " — " + text
	case a.Choice != "":
		text = a.Choice
	}
	ans := drafts.Answer{Question: a.Question, Answer: capText(text, assist.MaxAnswerBytes)}
	key := privNorm(a.Question)
	for i, old := range p.confirmed {
		if privNorm(old.Question) == key {
			p.confirmed[i] = ans
			return
		}
	}
	p.confirmed = append(p.confirmed, ans)
}

// paDecide records a deferral or an assumption for an advisory question: a durable decision plus its line in the
// Questions section, so the specification itself says what was settled and how.
func (m Model) paDecide(p *privateDraft, state drafts.DecisionState) (Model, tea.Cmd) {
	pa := p.pa
	s := pa.s
	q, ok := s.current()
	if !ok {
		return m, nil
	}
	if !assist.AdvisoryArea(q.Area) {
		pa.notice = "This one shapes scope or acceptance, so it cannot be deferred. Answer it, or leave it open."
		return m, nil
	}
	// One line each: a newline in an assumption would split the recorded line and readiness could never match it.
	dec := drafts.Decision{Question: strings.Join(strings.Fields(q.Text), " "), State: state}
	if state == drafts.Assumed {
		a := s.answer(q)
		switch {
		case a.sel == selChoice:
			dec.Assumption = q.Suggestions[a.pick]
			if t := strings.TrimSpace(a.text); t != "" {
				dec.Assumption += " — " + t
			}
		case a.sel == selOwn:
			dec.Assumption = strings.TrimSpace(a.text)
		}
		if dec.Assumption == "" {
			pa.notice = "Pick a suggestion or write the assumption first."
			return m, nil
		}
		dec.Assumption = strings.Join(strings.Fields(capText(dec.Assumption, assist.MaxAnswerBytes)), " ")
	}
	key := privNorm(q.Text)
	replaced := false
	for i, old := range p.decisions {
		if privNorm(old.Question) == key {
			p.decisions[i], replaced = dec, true
		}
	}
	if !replaced {
		p.decisions = append(p.decisions, dec)
	}
	p.writeDecisionLine(dec)
	// The question is settled: it leaves the round.
	s.questions = append(s.questions[:s.sel:s.sel], s.questions[s.sel+1:]...)
	delete(s.answers, q.ID)
	pa.onAct = false
	if len(s.questions) == 0 {
		pa.stage = paIdle
		pa.notice = "✓ Recorded in the Questions section."
	} else {
		s.sel = min(s.sel, len(s.questions)-1)
		s.resetPanel()
		s.setZone(defaultZone(s.questions[s.sel]))
		pa.notice = "✓ Recorded in the Questions section."
	}
	return m.privEdited(p)
}

// writeOpenQuestion keeps question as an open item of the Questions section unless a line already asks it, or a
// recorded decision already settles it.
func (p *privateDraft) writeOpenQuestion(question string) {
	at := p.secIndex(6)
	question = strings.Join(strings.Fields(question), " ")
	if at < 0 || question == "" {
		return
	}
	p.flush()
	if p.questionLine(p.secs[at].text, question) >= 0 {
		return
	}
	text := strings.TrimRight(p.secs[at].text, "\n")
	if strings.TrimSpace(text) == "" {
		text = "- " + question
	} else {
		text += "\n- " + question
	}
	p.secs[at].text = text
	p.markChanged(at)
	if at == p.cur {
		p.load(at)
	}
}

// questionLine is the index of the line of text that asks question, in any of its forms: plain, "Deferred: …" or
// "Assumed: … (…)". Matching is on the whole question, never on a line's tail.
func (p *privateDraft) questionLine(text, question string) int {
	key := privNorm(question)
	tail := " (" + strings.ToLower(strings.Join(strings.Fields(question), " ")) + ")"
	for i, l := range strings.Split(text, "\n") {
		bare := strings.Join(strings.Fields(strings.TrimLeft(strings.TrimSpace(l), "-*•")), " ")
		switch {
		case strings.HasPrefix(bare, "Deferred:"):
			if privNorm(strings.TrimPrefix(bare, "Deferred:")) == key {
				return i
			}
		case strings.HasPrefix(bare, "Assumed:"):
			if strings.HasSuffix(strings.ToLower(bare), tail) {
				return i
			}
		case privNorm(bare) == key:
			return i
		}
	}
	return -1
}

// writeDecisionLine puts the decision where readiness looks for it, replacing a line that asked the same question.
func (p *privateDraft) writeDecisionLine(dec drafts.Decision) {
	at := p.secIndex(6)
	if at < 0 {
		return
	}
	p.flush()
	line := "- " + dec.Line()
	lines := strings.Split(p.secs[at].text, "\n")
	done := false
	if i := p.questionLine(p.secs[at].text, dec.Question); i >= 0 {
		lines[i], done = line, true
	}
	text := strings.Join(lines, "\n")
	switch {
	case done:
	case strings.TrimSpace(text) == "":
		text = line
	default:
		text = strings.TrimRight(text, "\n") + "\n" + line
	}
	p.secs[at].text = text
	p.markChanged(at)
	if at == p.cur {
		p.load(at)
	}
}

// Keys ---------------------------------------------------------------------------------

func (m Model) paKey(p *privateDraft, msg tea.KeyMsg) (Model, tea.Cmd) {
	pa := p.pa
	k := msg.String()
	if pa.stage == paInvite {
		return m.paInviteKey(p, msg)
	}
	if pa.flight != nil {
		if k == "esc" {
			return m.paCancel(p), nil
		}
		return m, nil
	}
	switch pa.stage {
	case paQuestions:
		return m.paQuestionsKey(p, msg)
	case paProposal:
		return m.paProposalKey(p, msg)
	}
	return m.paActKey(p, k)
}

func (m Model) paInviteKey(p *privateDraft, msg tea.KeyMsg) (Model, tea.Cmd) {
	pa := p.pa
	o := pa.s.opts
	k := msg.String()
	editing := o.focus == optForm && o.form.Editing()
	if k == "esc" && !editing {
		return m.paClose(p), nil
	}
	if pa.invBtn >= 0 {
		switch k {
		case "shift+tab", "up":
			pa.invBtn = -1
			o.setFocus(optGuidance)
		case "tab":
			pa.invBtn = (pa.invBtn + 1) % 2
		case "left", "h":
			pa.invBtn = 0
		case "right", "l":
			pa.invBtn = 1
		case "enter":
			if pa.invBtn == 0 {
				return m.paClose(p), nil
			}
			return m.paStart(p)
		}
		return m, nil
	}
	if k == "tab" && o.focus == optGuidance {
		o.guidance.Blur()
		pa.invBtn = 1
		return m, nil
	}
	if k == "enter" && o.focus == optGuidance {
		o.guidance.Blur()
		pa.invBtn = 1
		return m, nil
	}
	if k == "shift+tab" && o.focus == optAccount {
		pa.invBtn = 1
		return m, nil
	}
	pa.s.notice = ""
	cmd, _ := o.key(msg)
	return m, cmd
}

// paActions are the buttons the current stage offers.
func (m Model) paActions(p *privateDraft) []paAction {
	pa := p.pa
	switch pa.stage {
	case paQuestions:
		out := []paAction{actApplyAnswers}
		if q, ok := pa.s.current(); ok && assist.AdvisoryArea(q.Area) {
			out = append(out, actDefer, actAssume)
		}
		return append(out, actWhy, actClose)
	case paProposal:
		return []paAction{actApplyProposal, actDiscard}
	}
	out := []paAction{actAsk}
	if pa.err != nil || pa.cancelled {
		out = append(out, actRetry)
	}
	return append(out, actClose)
}

func (m Model) paRun(p *privateDraft, act paAction) (Model, tea.Cmd) {
	pa := p.pa
	s := pa.s
	switch act {
	case actAsk:
		if len(p.confirmed)+len(p.decisions) >= assist.MaxSessionQuestions {
			pa.notice = "The assistant has asked enough for one draft. Edit it directly, or apply what you have."
			return m, nil
		}
		return m.paSend(p, false)
	case actRetry:
		return m.paRetry(p)
	case actClose:
		return m.paClose(p), nil
	case actWhy:
		s.hideWhy = !s.hideWhy
	case actDefer:
		return m.paDecide(p, drafts.Deferred)
	case actAssume:
		return m.paDecide(p, drafts.Assumed)
	case actApplyAnswers:
		answered := 0
		for _, a := range s.answerList() {
			if a.State != assist.Open {
				answered++
			}
		}
		if answered == 0 {
			pa.notice = "Answer, skip or mark at least one question first."
			return m, nil
		}
		return m.paSend(p, true)
	case actApplyProposal:
		return m.paApplyProposal(p)
	case actDiscard:
		pa.proposal, pa.review, s.review, s.proposal = nil, nil, nil, nil
		pa.stage, pa.onAct = paQuestions, false
		if len(s.questions) == 0 {
			pa.stage = paIdle
		}
		pa.notice = "Proposal discarded. Your text is unchanged."
	}
	return m, nil
}

func (m Model) paActKey(p *privateDraft, k string) (Model, tea.Cmd) {
	pa := p.pa
	acts := m.paActions(p)
	pa.act = max(0, min(pa.act, len(acts)-1))
	switch k {
	case "esc":
		p.pane = false
	case "left", "h", "shift+tab", "up", "k":
		pa.act = max(0, pa.act-1)
	case "right", "l", "tab", "down", "j":
		pa.act = min(len(acts)-1, pa.act+1)
	case "enter", " ":
		return m.paRun(p, acts[pa.act])
	case "R":
		return m.paRetry(p)
	}
	return m, nil
}

// cycle moves among the places tab visits in the questions stage: the options, the text field and the buttons.
func (pa *privAssistant) cycle(delta int) {
	s := pa.s
	type stop struct {
		zone assistZone
		act  bool
	}
	stops := []stop{{zone: zoneOptions}}
	if q, ok := s.current(); ok && s.answer(q).sel != selSkip {
		stops = append(stops, stop{zone: zoneText})
	}
	stops = append(stops, stop{act: true})
	at := 0
	for i, st := range stops {
		if (st.act && pa.onAct) || (!st.act && !pa.onAct && st.zone == s.zone) {
			at = i
		}
	}
	next := stops[(at+delta+len(stops))%len(stops)]
	pa.onAct = next.act
	if next.act {
		pa.act = 0
		s.ta.Blur()
		return
	}
	s.setZone(next.zone)
}

func (m Model) paQuestionsKey(p *privateDraft, msg tea.KeyMsg) (Model, tea.Cmd) {
	pa := p.pa
	s := pa.s
	k := msg.String()
	switch k {
	case "tab":
		pa.cycle(1)
		return m, nil
	case "shift+tab":
		pa.cycle(-1)
		return m, nil
	case "ctrl+down":
		s.qScroll++ // the geometry clamps it to the text it has
		return m, nil
	case "ctrl+up":
		s.qScroll = max(0, s.qScroll-1)
		return m, nil
	case "pgdown", "pgup":
		delta := 1
		if k == "pgup" {
			delta = -1
		}
		s.moveSel(delta)
		pa.onAct = false
		if q, ok := s.current(); ok {
			s.setZone(defaultZone(q))
		}
		return m, nil
	case "esc":
		switch {
		case !pa.onAct && s.zone == zoneText:
			s.setZone(zoneOptions)
		default:
			s.ta.Blur()
			p.pane = false
		}
		return m, nil
	}
	if pa.onAct {
		return m.paActKey(p, k)
	}
	if s.zone == zoneText {
		before := s.ta.Value()
		var cmd tea.Cmd
		s.ta, cmd = s.ta.Update(msg)
		if after := s.ta.Value(); after != before {
			if len(after) > assist.MaxAnswerBytes {
				s.ta.SetValue(before)
				pa.notice = "An answer holds up to 2 KB."
			} else {
				s.storeText(after)
				pa.notice = ""
			}
		}
		return m, cmd
	}
	switch k {
	case "R":
		return m.paRetry(p)
	case "w":
		s.hideWhy = !s.hideWhy
		return m, nil
	}
	s.optionsKey(k)
	return m, nil
}

func (m Model) paProposalKey(p *privateDraft, msg tea.KeyMsg) (Model, tea.Cmd) {
	pa := p.pa
	r := pa.review
	k := msg.String()
	if r == nil {
		pa.stage = paIdle
		return m, nil
	}
	switch k {
	case "tab", "shift+tab":
		pa.onAct = !pa.onAct
		pa.act = 0
		return m, nil
	case "esc":
		if pa.onAct {
			pa.onAct = false
			return m, nil
		}
		p.pane = false
		return m, nil
	}
	if pa.onAct {
		return m.paActKey(p, k)
	}
	switch k {
	case "up", "k":
		if r.onSuggestions() && r.sugCur > 0 {
			r.sugCur--
		} else {
			r.sel = max(0, r.sel-1)
		}
	case "down", "j":
		if r.onSuggestions() {
			r.sugCur = min(len(r.sugg)-1, r.sugCur+1)
		} else {
			r.sel = min(r.rows()-1, r.sel+1)
			r.sugCur = 0
		}
	case "enter", " ":
		if r.onSuggestions() {
			it := r.sugg[r.sugCur].item
			r.confirmed[it] = !r.confirmed[it]
			r.rebuild(*pa.proposal)
			return m, nil
		}
		secs := reviewSectionsOf(p.pa.review, p.fixedText)
		if secs[r.sel].accept {
			r.choice[r.sel] = 2
		} else {
			r.choice[r.sel] = 1
		}
	case "a":
		if !r.onSuggestions() {
			r.choice[r.sel] = 1
		}
	case "x":
		if !r.onSuggestions() {
			r.choice[r.sel] = 2
		}
	}
	return m, nil
}

// paBadge marks a reply that landed while the pane was not in front.
func (p *privateDraft) paBadge() string {
	if p.pa != nil && p.pa.unseen && p.pa.flight == nil {
		return "●"
	}
	return ""
}

// privFieldLabel is a fixed field's name as the workspace shows it: Problem, Outcome and so on.
func privFieldLabel(field int) string {
	for _, group := range [][]privSection{privPrimary, privMore} {
		for _, e := range group {
			if e.field == field {
				return e.title
			}
		}
	}
	return spec.Names[field]
}

// privDisclosure says exactly what the first request carries and where it goes.
func (m Model) privDisclosure(p *privateDraft) string {
	who := "the chosen account"
	if p.pa != nil {
		if a, ok := p.pa.s.opts.Current(); ok {
			who = fmt.Sprintf("%s (account %s)", assistProviderName(a.Provider), boardTitle(a.Name))
		}
	}
	p.flush()
	written := 0
	for _, e := range p.secs {
		if strings.TrimSpace(e.text) != "" {
			written++
		}
	}
	parts := []string{"the title and type"}
	if n := len(strings.TrimSpace(p.desc.Value())); n > 0 {
		parts = append(parts, fmt.Sprintf("the description (%d characters)", n))
	}
	parts = append(parts, fmt.Sprintf("the specification text (%d section%s written)", written, plural(written)))
	if n := len(p.confirmed); n > 0 {
		parts = append(parts, fmt.Sprintf("%d confirmed answer%s", n, plural(n)))
	}
	if p.pa != nil && strings.TrimSpace(p.pa.s.opts.guidance.Value()) != "" {
		parts = append(parts, "your guidance")
	}
	repo := ""
	if name := m.garden.status.Target.FullName; name != "" {
		repo = " and the repository name " + name
	}
	return "Sends " + strings.Join(parts, ", ") + repo + " to " + who + ". No repository files are read. The assistant only proposes: nothing changes until you apply it."
}

// privAssistForward feeds the invitation's account list the results of its own backend calls.
func (m Model) privAssistForward(msg tea.Msg) tea.Cmd {
	p := m.garden.drafts.private
	if p == nil || p.pa == nil || p.pa.s.opts == nil {
		return nil
	}
	return p.pa.s.opts.update(msg)
}
