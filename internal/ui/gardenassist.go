package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/assist"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/accounts"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/launchform"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/usagestore"
)

// AI draft elaboration (plan 08b, TUI). Sessions live in memory only, keyed like the editors. The assist track has
// its own request counter and at most one flight per process: it must not use gardenState.begin/abandon or the Gate,
// because the next Board refresh would cancel a call that takes minutes, or the Gate would reject its older generation.

type assistStage int

const (
	assistSetup     assistStage = iota // description, options and the Ask buttons
	assistQuestions                    // rail of questions and one answer editor
	assistReview                       // the proposal; Part 2 draws and drives it
)

type assistFocus int

const (
	sfDesc assistFocus = iota
	sfOptions
	sfAsk
	sfDraft
)

const assistOutOfDate = "Ignored an out-of-date AI reply"

// assistSel is what the options list has selected for one question.
type assistSel int

const (
	selNone assistSel = iota
	selChoice
	selOwn
	selUnknown
	selSkip
)

// assistZone is the part of the Questions stage that has the keyboard.
type assistZone int

const (
	zoneRail assistZone = iota
	zoneOptions
	zoneText
)

// assistAnswer is one question's answer. text is shared by every selection that has a text field (a choice's notes,
// the own answer, a don't-know's notes), so switching between them never loses what was typed.
type assistAnswer struct {
	sel  assistSel
	pick int // the suggestion, when sel is selChoice
	text string
}

// state is the answer state the assistant is told about. Write my own with no text is still open.
func (a assistAnswer) state() assist.AnswerState {
	switch a.sel {
	case selChoice:
		return assist.Answered
	case selOwn:
		if strings.TrimSpace(a.text) != "" {
			return assist.Answered
		}
	case selUnknown:
		return assist.Unknown
	case selSkip:
		return assist.Skipped
	}
	return assist.Open
}

// assistSent is one request, kept so R can send the same inputs again under a new RequestID.
type assistSent struct {
	draft    bool
	ctx      gardenapi.AssistContext
	round    int
	feedback string
}

// assistFlight is the one request in the air. The call itself runs in a tea.Cmd; only its identity lives here.
type assistFlight struct {
	session, seq uint64
	requestID    string
	draft        bool
	cancel       context.CancelFunc
	secs         int
	provider     string
	model        string
	effort       string
}

type assistSession struct {
	id     uint64
	key    editKey
	stamp  gardenapi.Stamp // the clone, Garden and epoch the session was started under
	isNew  bool
	number int64
	title  string
	typ    garden.ItemType
	from   draftsStage // the Drafts stage Esc returns to

	stage assistStage
	focus assistFocus
	desc  textarea.Model // the free description; new ideas only
	opts  *assistOptions
	// optsOpen shows the options dialog over Setup.
	optsOpen bool
	// settings are what the first request used; later rounds and drafts reuse them.
	settings    gardenapi.AssistSettings
	provider    string
	accountName string

	questions     []assist.Question
	answers       map[string]assistAnswer
	round         int
	understanding string
	sel           int
	zone          assistZone
	optCur        int            // the options row under the cursor
	qScroll       int            // first visible line of the question text when the panel is too short for it
	ta            textarea.Model // notes or the own answer
	taFor         string         // the question the editor shows

	draftText string // the working draft last sent, for an existing draft whose editor was closed
	proposal  *assist.Proposal
	inputHash string
	review    *reviewState
	refine    bool            // the refine feedback input has the keyboard
	feedback  textinput.Model // refine feedback, at most assist.MaxFeedbackRunes
	// after is what to do once the editor exists: the document read that creates it is asynchronous.
	after         assistAfter
	afterFeedback string

	last    *assistSent
	err     *gardenapi.Error
	notice  string
	confirm bool // the cancel question is showing
}

type gardenAssistState struct {
	sessions map[editKey]*assistSession
	open     *assistSession
	nextID   uint64
	seq      uint64
	flight   *assistFlight
}

func (a *gardenAssistState) byID(id uint64) *assistSession {
	for _, s := range a.sessions {
		if s.id == id {
			return s
		}
	}
	return nil
}

// running reports whether s owns the flight.
func (a *gardenAssistState) running(s *assistSession) bool {
	return a.flight != nil && s != nil && a.flight.session == s.id
}

// discard forgets a session and ends its flight, if it has one.
func (a *gardenAssistState) discard(key editKey) {
	s := a.sessions[key]
	if s == nil {
		return
	}
	if a.running(s) {
		a.flight.cancel()
		a.flight = nil
	}
	if a.open == s {
		a.open = nil
	}
	delete(a.sessions, key)
}

// release drops what a changed association invalidates: the open screen and the flight. Sessions stay in memory
// but are inactive until their epoch is current again; the next entry replaces them.
func (a *gardenAssistState) release() {
	if a.flight != nil {
		a.flight.cancel()
		a.flight = nil
	}
	a.open = nil
}

func (s *assistSession) setFocus(f assistFocus) {
	s.focus = f
	if f == sfDesc {
		s.desc.Focus()
		return
	}
	s.desc.Blur()
}

func (s *assistSession) focusOrder() []assistFocus {
	if s.isNew {
		return []assistFocus{sfDesc, sfOptions, sfAsk, sfDraft}
	}
	return []assistFocus{sfOptions, sfAsk, sfDraft}
}

func (s *assistSession) cycle(delta int) {
	order := s.focusOrder()
	at := 0
	for i, f := range order {
		if f == s.focus {
			at = i
		}
	}
	s.setFocus(order[(at+delta+len(order))%len(order)])
}

func (s *assistSession) current() (assist.Question, bool) {
	if s.sel < 0 || s.sel >= len(s.questions) {
		return assist.Question{}, false
	}
	return s.questions[s.sel], true
}

// answer is the question's answer. A question with no suggestions starts on Write my own, so its text field is
// usable at once.
func (s *assistSession) answer(q assist.Question) assistAnswer {
	a := s.answers[q.ID]
	if a.sel == selNone && len(q.Suggestions) == 0 {
		a.sel = selOwn
	}
	return a
}

func (s *assistSession) setAnswer(id string, a assistAnswer) {
	if s.answers == nil {
		s.answers = map[string]assistAnswer{}
	}
	if a.sel == selNone && a.text == "" {
		delete(s.answers, id)
		return
	}
	s.answers[id] = a
}

// answerList is every question with the user's state, in the order they were asked. Open questions are sent too, so
// the assistant knows what stayed unanswered. Text travels only where the table of 08c says: a skip sends none.
func (s *assistSession) answerList() []assist.Answer {
	out := make([]assist.Answer, 0, len(s.questions))
	for _, q := range s.questions {
		a := s.answer(q)
		item := assist.Answer{QuestionID: q.ID, Question: q.Text, State: a.state()}
		switch a.sel {
		case selChoice:
			item.Choice = capText(q.Suggestions[a.pick], assist.MaxAnswerBytes)
			item.Text = strings.TrimSpace(a.text)
		case selOwn, selUnknown:
			item.Text = strings.TrimSpace(a.text)
		}
		out = append(out, item)
	}
	return out
}

func (s *assistSession) counts() (answered, skipped, unknown int) {
	for _, q := range s.questions {
		switch s.answer(q).state() {
		case assist.Answered:
			answered++
		case assist.Skipped:
			skipped++
		case assist.Unknown:
			unknown++
		}
	}
	return
}

// loadEditor shows the selected question's text in the editor.
func (s *assistSession) loadEditor() {
	q, ok := s.current()
	if !ok {
		s.taFor = ""
		s.ta.SetValue("")
		return
	}
	if s.taFor == q.ID {
		return
	}
	s.taFor = q.ID
	s.ta.SetValue(s.answer(q).text)
	textareaTop(&s.ta)
}

// optionRows is how many rows the options list selects among: the suggestions, Write my own, I don't know, Skip.
func optionRows(q assist.Question) int { return len(q.Suggestions) + 3 }

// selectedRow is the options row of the current selection; the first row when nothing is selected.
func selectedRow(q assist.Question, a assistAnswer) int {
	n := len(q.Suggestions)
	switch a.sel {
	case selChoice:
		return a.pick
	case selOwn:
		return n
	case selUnknown:
		return n + 1
	case selSkip:
		return n + 2
	}
	return 0
}

// defaultZone is where a question takes the keyboard: its options, or its text when it has no suggestions.
func defaultZone(q assist.Question) assistZone {
	if len(q.Suggestions) == 0 {
		return zoneText
	}
	return zoneOptions
}

// zones are the zones tab visits: Skip has no text field, so it has no text zone.
func (s *assistSession) zones() []assistZone {
	out := []assistZone{zoneRail, zoneOptions}
	if q, ok := s.current(); ok && s.answer(q).sel != selSkip {
		out = append(out, zoneText)
	}
	return out
}

func (s *assistSession) setZone(z assistZone) {
	q, ok := s.current()
	if !ok {
		z = zoneRail
	}
	if z == zoneText && s.answer(q).sel == selSkip {
		z = zoneOptions
	}
	s.zone = z
	if z == zoneRail {
		s.ta.Blur()
		return
	}
	s.loadEditor()
	if z == zoneText {
		s.ta.Focus()
		return
	}
	s.ta.Blur()
}

func (s *assistSession) cycleZone(delta int) {
	zs := s.zones()
	at := 0
	for i, z := range zs {
		if z == s.zone {
			at = i
		}
	}
	s.setZone(zs[(at+delta+len(zs))%len(zs)])
}

func (s *assistSession) moveSel(delta int) {
	if len(s.questions) == 0 {
		return
	}
	s.sel = max(0, min(s.sel+delta, len(s.questions)-1))
	s.qScroll = 0
	s.loadEditor()
	q, _ := s.current()
	s.optCur = selectedRow(q, s.answer(q))
}

// selectRow selects options row r. Everything but Skip continues in the text field.
func (s *assistSession) selectRow(r int) {
	q, ok := s.current()
	if !ok || r < 0 || r >= optionRows(q) {
		return
	}
	n := len(q.Suggestions)
	a := s.answer(q)
	switch {
	case r < n:
		a.sel, a.pick = selChoice, r
	case r == n:
		a.sel = selOwn
	case r == n+1:
		a.sel = selUnknown
	default:
		a.sel = selSkip
	}
	s.setAnswer(q.ID, a)
	s.optCur = r
	if a.sel == selSkip {
		s.setZone(zoneOptions)
		return
	}
	s.setZone(zoneText)
}

// storeText records typed text. Typing with nothing selected writes an own answer.
func (s *assistSession) storeText(text string) {
	q, ok := s.current()
	if !ok {
		return
	}
	a := s.answer(q)
	a.text = text
	if a.sel == selNone || a.sel == selSkip {
		a.sel = selOwn
		s.optCur = selectedRow(q, a)
	}
	s.setAnswer(q.ID, a)
}

// capText cuts text to the answer limit on a rune boundary.
func capText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	text = text[:limit]
	for len(text) > 0 && !utf8.ValidString(text) {
		text = text[:len(text)-1]
	}
	return text
}

// assistDefaults snapshots personal preferences when a session starts.
func (m Model) assistDefaults() gardenapi.AssistSettings {
	out := gardenapi.AssistSettings{Questions: assist.DefaultQuestions}
	if p := m.work.GardenAssist; p != nil {
		out.AccountID = agents.AccountID(p.Account)
		out.Model, out.Effort = p.Model, p.Effort
		if p.Questions != 0 {
			out.Questions = max(1, min(p.Questions, assist.MaxQuestions))
		}
		out.Guidance = assist.SanitizeGuidance(p.Guidance)
	}
	return out
}

func newAssistSession(id uint64, key editKey, stamp gardenapi.Stamp, isNew bool, number int64, title string, typ garden.ItemType, from draftsStage) *assistSession {
	s := &assistSession{id: id, key: key, stamp: stamp, isNew: isNew, number: number, title: title, typ: typ, from: from}
	s.desc = newTextarea()
	s.desc.CharLimit = garden.MaxDescriptionBytes
	s.desc.Placeholder = "What is on your mind? A bug, a request, a complaint, a rough idea…"
	s.ta = newTextarea()
	s.ta.CharLimit = assist.MaxAnswerBytes
	s.feedback = textinput.New()
	s.feedback.Prompt = ""
	s.feedback.CharLimit = assist.MaxFeedbackRunes
	s.feedback.Cursor.SetMode(cursor.CursorStatic)
	if isNew {
		s.setFocus(sfDesc)
	} else {
		s.setFocus(sfOptions)
	}
	return s
}

// assistProviderName is the provider as the status line names it.
func assistProviderName(p agents.ProviderID) string {
	if p == "claude" {
		return "Claude Code"
	}
	return providerName(p)
}

// --- the options dialog ---

type assistOptFocus int

const (
	optAccount assistOptFocus = iota
	optForm
	optQuestions
	optGuidance
	optCancel
	optSave
)

// assistOptions edits session options, or just account/model/effort in Settings defaults mode.
type assistOptions struct {
	accounts.Browser
	ascii        bool
	focus        assistOptFocus
	form         launchform.Form
	formLoaded   bool
	formErr      string
	questions    int
	guidance     textinput.Model
	touched      bool // the user chose an account; the first supported one is not picked for them anymore
	notice       string
	defaultsMode bool
	submit       bool
	initial      gardenapi.AssistSettings
}

func newAssistOptions(api agentsapi.Accounts, usage usagestore.Store, ascii bool, def gardenapi.AssistSettings) *assistOptions {
	g := textinput.New()
	g.Prompt = ""
	g.CharLimit = assist.MaxGuidanceRunes
	g.Placeholder = "Optional: how drafts should read"
	g.Cursor.SetMode(cursor.CursorStatic)
	g.SetValue(def.Guidance)
	q := def.Questions
	if q < 1 || q > assist.MaxQuestions {
		q = assist.DefaultQuestions
	}
	return &assistOptions{Browser: accounts.NewBrowser(api, def.AccountID, usage, true), ascii: ascii, questions: q, guidance: g, initial: def}
}

func (o *assistOptions) setFocus(f assistOptFocus) {
	o.focus = f
	if o.focus == optGuidance {
		o.guidance.Focus()
	} else {
		o.guidance.Blur()
	}
	o.form = o.form.Focus(o.focus == optForm)
}

func (o *assistOptions) moveFocus(delta int) {
	order := []assistOptFocus{optAccount, optForm, optQuestions, optGuidance}
	if o.defaultsMode {
		order = []assistOptFocus{optAccount, optForm, optCancel, optSave}
	}
	for i, f := range order {
		if o.focus == f {
			o.setFocus(order[(i+delta+len(order))%len(order)])
			return
		}
	}
}

func (o *assistOptions) resetForm() {
	o.formLoaded, o.formErr = false, ""
}

func (o *assistOptions) onDefaults() {
	if msg := o.DefaultsErr(); msg != "" {
		o.formErr = msg
		return
	}
	o.formErr = ""
	def := o.Defaults()
	fields := plannerDialogFields(launchDialogFields(def.LaunchFields))
	// Empty selections remain overrides of nothing: the backend resolves profile defaults.
	for i := range fields {
		fields[i].Default = ""
		if fields[i].Kind == agents.LaunchFieldModel {
			fields[i].Options = []agents.FieldOption{{Value: "", Label: "Profile default"}}
		}
	}
	values := map[string]string{"model": o.initial.Model, "effort": o.initial.Effort}
	o.form = launchform.New(fields, def.Models, values, o.ascii).Focus(o.focus == optForm)
	o.formLoaded = true
}

// supported says whether an account can run a draft: signed in and tool-free structured calls available.
func assistSupported(a agentsapi.Account) bool {
	return a.Structured.Supported && !accounts.NeedsSignIn(a.Info)
}

// update prevents the browser fallback from replacing a missing saved account.
// Without a saved account, the first supported account is selected as before.
func (o *assistOptions) update(msg tea.Msg) tea.Cmd {
	prev := o.Selected()
	var cmd, extra tea.Cmd
	var ch accounts.Change
	o.Browser, cmd, ch = o.Browser.Update(msg)
	if ch&accounts.ChangedList != 0 && o.ListErr() == "" && !o.touched {
		if o.initial.AccountID != "" {
			found := false
			for _, a := range o.Accounts() {
				if a.ID == o.initial.AccountID && assistSupported(a) {
					found = true
					break
				}
			}
			if !found {
				o.Browser, extra = o.Browser.Select("")
				o.notice = "Saved default account is missing: choose again"
			}
		} else if cur, ok := o.Current(); !ok || !assistSupported(cur) {
			for _, a := range o.Ordered() {
				if assistSupported(a) {
					o.Browser, extra = o.Browser.Select(a.ID)
					break
				}
			}
		}
	}
	if o.Selected() != prev || (ch&accounts.ChangedList != 0 && o.ListErr() == "") {
		o.resetForm()
	}
	if ch&accounts.ChangedDefaults != 0 {
		o.onDefaults()
	}
	return tea.Batch(cmd, extra)
}

// key handles a key of the dialog; done is true when it should close.
func (o *assistOptions) key(msg tea.KeyMsg) (cmd tea.Cmd, done bool) {
	s := msg.String()
	editing := o.focus == optForm && o.form.Editing()
	if !editing {
		switch s {
		case "ctrl+s":
			if o.defaultsMode {
				o.submit = true
				return nil, true
			}
		case "esc":
			return nil, true
		case "tab":
			if o.focus == optForm && !o.form.AtLast() {
				o.form, _ = o.form.Update(tea.KeyMsg{Type: tea.KeyDown})
				return nil, false
			}
			o.moveFocus(1)
			return nil, false
		case "shift+tab":
			if o.focus == optForm && !o.form.AtFirst() {
				o.form, _ = o.form.Update(tea.KeyMsg{Type: tea.KeyUp})
				return nil, false
			}
			o.moveFocus(-1)
			return nil, false
		}
	}
	switch o.focus {
	case optAccount:
		return o.accountKey(s), false
	case optForm:
		if !editing && s == "down" && o.form.AtLast() {
			o.moveFocus(1)
			return nil, false
		}
		if !editing && s == "up" && o.form.AtFirst() {
			o.setFocus(optAccount)
			return nil, false
		}
		if o.formLoaded {
			o.form, _ = o.form.Update(msg)
		} else if s == "enter" || s == "down" {
			o.moveFocus(1)
		}
	case optQuestions:
		switch s {
		case "left", "h", "-":
			o.questions = max(1, o.questions-1)
		case "right", "l", "+":
			o.questions = min(assist.MaxQuestions, o.questions+1)
		case "up":
			o.setFocus(optForm)
		case "down", "enter":
			o.setFocus(optGuidance)
		}
	case optCancel, optSave:
		switch s {
		case "enter":
			o.submit = o.focus == optSave
			return nil, true
		case "left", "up":
			o.moveFocus(-1)
		case "right", "down":
			o.moveFocus(1)
		}
	case optGuidance:
		switch s {
		case "enter":
			return nil, true
		case "up":
			o.setFocus(optQuestions)
		default:
			var c tea.Cmd
			o.guidance, c = o.guidance.Update(msg)
			return c, false
		}
	}
	return nil, false
}

func (o *assistOptions) accountKey(s string) tea.Cmd {
	delta := 0
	switch s {
	case "j", "down":
		delta = 1
	case "k", "up":
		delta = -1
	case "enter":
		o.setFocus(optForm)
		return nil
	}
	ordered := o.Ordered()
	if delta == 0 || len(ordered) == 0 {
		return nil
	}
	at := -1
	for n, a := range ordered {
		if a.ID == o.Selected() {
			at = n
		}
	}
	next := ordered[min(max(at+delta, 0), len(ordered)-1)].ID
	if next == o.Selected() {
		return nil
	}
	o.touched = true
	o.notice = ""
	var cmd tea.Cmd
	o.Browser, cmd = o.Browser.Select(next)
	o.resetForm()
	return cmd
}

// settings are the options as the request carries them.
func (o *assistOptions) settings() gardenapi.AssistSettings {
	out := gardenapi.AssistSettings{AccountID: o.Selected(), Questions: o.questions, Guidance: assist.SanitizeGuidance(o.guidance.Value())}
	if o.formLoaded {
		vals := o.form.Values()
		lo := agents.LaunchOptionsFrom(o.Defaults().LaunchFields, vals["model"], vals)
		out.Model, out.Effort = lo.Model, lo.Effort
	}
	return out
}

// --- entry ---

// assistUnavailable is why no assistant can start, or "".
func (m Model) assistUnavailable() string {
	if m.agents.api.Accounts == nil {
		return "Agent accounts are unavailable here, so AI drafting cannot run."
	}
	return ""
}

// assistNotice shows a refusal where the user pressed the key.
func (m Model) assistNotice(why string) Model {
	d := &m.garden.drafts
	if d.stage == draftsEditor && d.edit != nil {
		d.edit.notice = why
		return m
	}
	d.notice = why
	return m
}

// gardenAssistNew is i on the Drafts list: a new idea with AI, resumed when one is already in progress.
func (m Model) gardenAssistNew() (tea.Model, tea.Cmd) {
	if ok, why := m.canCapture(); !ok {
		return m.assistNotice(why), nil
	}
	if why := m.assistUnavailable(); why != "" {
		return m.assistNotice(why), nil
	}
	m.garden.drafts.notice = ""
	return m.gardenAssistOpen(m.editKeyFor(0), true, 0, "", "")
}

// gardenAssistExisting is i on a draft: elaborate it. The assistant works from the editor's text, so an editor must
// exist: the open one (with its unsaved edits) or one made from the specification read now.
func (m Model) gardenAssistExisting(id garden.IssueID) (tea.Model, tea.Cmd) {
	d := &m.garden.drafts
	if id == 0 {
		return m, nil
	}
	if ok, why := m.canEdit(); !ok {
		return m.assistNotice(why), nil
	}
	if why := m.assistUnavailable(); why != "" {
		return m.assistNotice(why), nil
	}
	d.notice = ""
	if e := d.editors[m.editKeyFor(id)]; e != nil {
		return m.gardenAssistOpen(e.key, false, e.number, e.title, m.assistTypeOf(id))
	}
	return m.gardenLoadDocument(id, docAssist)
}

func (m Model) assistTypeOf(id garden.IssueID) garden.ItemType {
	if r, _, ok := m.garden.drafts.collection.Find(id); ok {
		return r.Item.Classification.Type
	}
	return ""
}

// assistActive says whether a session still belongs to the Garden connection in view.
func (m Model) assistActive(s *assistSession) bool {
	st := m.garden.status.Stamp
	return s.stamp.LocalRepoID == st.LocalRepoID && s.stamp.GardenID == st.GardenID && s.stamp.Epoch == st.Epoch
}

// gardenAssistOpen shows the session for key, creating it first when there is none.
func (m Model) gardenAssistOpen(key editKey, isNew bool, number int64, title string, typ garden.ItemType) (Model, tea.Cmd) {
	g := &m.garden
	a := &g.assist
	d := &g.drafts
	if a.sessions == nil {
		a.sessions = map[editKey]*assistSession{}
	}
	var cmds []tea.Cmd
	s := a.sessions[key]
	if s != nil && !m.assistActive(s) {
		a.discard(key)
		s = nil
		d.notice = "The earlier AI session belonged to another Garden connection and was dropped."
	}
	if s == nil {
		a.nextID++
		s = newAssistSession(a.nextID, key, g.status.Stamp, isNew, number, title, typ, d.stage)
		s.opts = newAssistOptions(m.agents.api.Accounts, m.agents.usage, m.agentGlyphsASCII(), m.assistDefaults())
		a.sessions[key] = s
		cmds = append(cmds, s.opts.Init())
	} else {
		s.from = d.stage
		if !s.opts.Listed() {
			cmds = append(cmds, s.opts.Relist(s.opts.Selected()))
		}
	}
	a.open = s
	d.stage = draftsAssist
	m.shellFocus = shellInput
	m.gardenAssistSize()
	return m, tea.Batch(cmds...)
}

// gardenAssistLeave returns to the stage the session was opened from. The session and its flight stay.
func (m Model) gardenAssistLeave() Model {
	g := &m.garden
	d := &g.drafts
	s := g.assist.open
	g.assist.open = nil
	m.shellFocus = shellBody
	from := draftsList
	if s != nil {
		from = s.from
	}
	switch from {
	case draftsEditor:
		if d.edit != nil {
			d.stage = draftsEditor
			m.shellFocus = shellInput
			return m
		}
		fallthrough
	case draftsDetail:
		if d.opened != 0 {
			d.stage = draftsDetail
			return m
		}
	}
	d.stage = draftsList
	return m
}

// gardenAssistForward feeds the options dialogs the results of their own backend calls. Every dialog ignores what is
// not its own, and the message continues to the rest of the shell.
func (m Model) gardenAssistForward(msg tea.Msg) (Model, tea.Cmd) {
	if _, isKey := msg.(tea.KeyMsg); isKey {
		return m, nil
	}
	if _, done := msg.(agentExecDoneMsg); done {
		msg = accounts.ReloadMsg{}
	}
	var cmds []tea.Cmd
	if m.set.assist != nil {
		cmds = append(cmds, m.set.assist.update(msg))
	}
	for _, s := range m.garden.assist.sessions {
		if s.opts != nil {
			cmds = append(cmds, s.opts.update(msg))
		}
	}
	return m, tea.Batch(cmds...)
}

// --- requests ---

type (
	assistQuestionsMsg struct {
		session, seq uint64
		view         gardenapi.AssistQuestionsView
		err          error
	}
	assistDraftMsg struct {
		session, seq uint64
		view         gardenapi.AssistDraftView
		err          error
	}
	// assistTickMsg advances the elapsed seconds of one flight; a tick of any other flight is dropped, which ends it.
	assistTickMsg struct{ session, seq uint64 }
)

// assistTickCmd fires after a second. Tests skip it by its name.
func assistTickCmd(session, seq uint64) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(time.Second)
		return assistTickMsg{session: session, seq: seq}
	}
}

// assistDraftText is the working draft the next request sends: the editor's text, unsaved edits included. A closed
// clean editor leaves the last text the session saw.
func (m Model) assistDraftText(s *assistSession) string {
	if e := m.garden.drafts.editors[s.key]; e != nil {
		s.draftText = e.content()
	}
	if s.isNew && s.draftText == "" {
		return ""
	}
	return s.draftText
}

// assistContext snapshots the inputs of a request.
func (m Model) assistContext(s *assistSession, settings gardenapi.AssistSettings) gardenapi.AssistContext {
	c := gardenapi.AssistContext{
		Stamp: s.stamp, IssueID: uint64(s.key.issue), Number: s.number, Title: s.title, Type: s.typ,
		Draft: m.assistDraftText(s), Answers: s.answerList(), Settings: settings,
	}
	if s.isNew {
		c.Description = strings.TrimSpace(s.desc.Value())
	}
	return c
}

// gardenAssistSend starts one request under a new RequestID. It refuses a second flight: a duplicate submit of the
// same session is ignored, and another session is told to wait.
func (m Model) gardenAssistSend(s *assistSession, sent assistSent) (Model, tea.Cmd) {
	g := &m.garden
	a := &g.assist
	if a.flight != nil {
		if a.flight.session != s.id {
			s.notice = "Another AI request is running."
		}
		return m, nil
	}
	if g.api == nil {
		return m, nil
	}
	a.seq++
	sent.ctx.RequestID = fmt.Sprintf("session-%d-seq-%d", s.id, a.seq)
	ctx, cancel := context.WithCancel(context.Background())
	f := &assistFlight{session: s.id, seq: a.seq, requestID: sent.ctx.RequestID, draft: sent.draft, cancel: cancel,
		provider: s.provider, model: sent.ctx.Settings.Model, effort: sent.ctx.Settings.Effort}
	a.flight = f
	kept := sent
	s.last = &kept
	s.err, s.notice, s.confirm = nil, "", false
	api := g.api
	sid, seq := s.id, a.seq
	var call tea.Cmd
	if sent.draft {
		req := gardenapi.AssistDraftRequest{AssistContext: sent.ctx, Feedback: sent.feedback}
		call = func() tea.Msg {
			v, err := api.AssistDraft(ctx, req)
			return assistDraftMsg{session: sid, seq: seq, view: v, err: err}
		}
	} else {
		req := gardenapi.AssistQuestionsRequest{AssistContext: sent.ctx, Round: sent.round}
		call = func() tea.Msg {
			v, err := api.AssistQuestions(ctx, req)
			return assistQuestionsMsg{session: sid, seq: seq, view: v, err: err}
		}
	}
	return m, tea.Batch(call, assistTickCmd(sid, seq))
}

// assistPrepare checks what a first request from Setup needs and returns the settings it will use.
func (m Model) assistPrepare(s *assistSession, needDescription bool) (gardenapi.AssistSettings, tea.Cmd, bool) {
	if needDescription && s.isNew && strings.TrimSpace(s.desc.Value()) == "" {
		s.notice = "Describe the idea first."
		s.setFocus(sfDesc)
		return gardenapi.AssistSettings{}, nil, false
	}
	o := s.opts
	a, ok := o.Current()
	switch {
	case !ok:
		s.notice = "Choose an account first (o)."
		return gardenapi.AssistSettings{}, nil, false
	case accounts.NeedsSignIn(a.Info):
		s.notice = providerName(a.Provider) + " needs a sign-in first."
		if c := o.LoginCommand(a.ID); c != nil {
			return gardenapi.AssistSettings{}, emitMsg(accounts.ExecMsg{Cmd: c}), false
		}
		return gardenapi.AssistSettings{}, nil, false
	case !a.Structured.Supported:
		s.notice = assistUnsupportedText(a)
		return gardenapi.AssistSettings{}, nil, false
	case !o.formLoaded && o.formErr == "":
		s.notice = "Options are still loading."
		return gardenapi.AssistSettings{}, nil, false
	}
	s.provider, s.accountName = assistProviderName(a.Provider), a.Name
	return o.settings(), nil, true
}

func assistUnsupportedText(a agentsapi.Account) string {
	reason := a.Structured.Reason
	if reason == "" {
		reason = "no tool-free mode"
	}
	return providerName(a.Provider) + " cannot draft: " + reason + ". Choose another account."
}

// gardenAssistAsk sends the first question round from Setup.
func (m Model) gardenAssistAsk(s *assistSession) (Model, tea.Cmd) {
	if m.garden.assist.flight != nil {
		return m.gardenAssistSend(s, assistSent{})
	}
	settings, cmd, ok := m.assistPrepare(s, true)
	if !ok {
		return m, cmd
	}
	s.settings = settings
	return m.gardenAssistSend(s, assistSent{ctx: m.assistContext(s, settings), round: 1})
}

// gardenAssistDraftNow asks for a draft with no questions, from Setup.
func (m Model) gardenAssistDraftNow(s *assistSession) (Model, tea.Cmd) {
	if m.garden.assist.flight != nil {
		return m.gardenAssistSend(s, assistSent{draft: true})
	}
	settings, cmd, ok := m.assistPrepare(s, true)
	if !ok {
		return m, cmd
	}
	s.settings = settings
	return m.gardenAssistSend(s, assistSent{draft: true, ctx: m.assistContext(s, settings)})
}

// gardenAssistDraft asks for a draft with every answer so far. feedback is Part 2's refine text.
func (m Model) gardenAssistDraft(s *assistSession, feedback string) (Model, tea.Cmd) {
	return m.gardenAssistSend(s, assistSent{draft: true, ctx: m.assistContext(s, s.settings), feedback: feedback})
}

// gardenAssistMore asks for the next round of questions.
func (m Model) gardenAssistMore(s *assistSession) (Model, tea.Cmd) {
	if len(s.questions) >= assist.MaxSessionQuestions {
		s.notice = fmt.Sprintf("The session already asked %d questions, the most it allows. Draft now.", assist.MaxSessionQuestions)
		return m, nil
	}
	return m.gardenAssistSend(s, assistSent{ctx: m.assistContext(s, s.settings), round: s.round + 1})
}

// gardenAssistRetry sends the last request again under a new RequestID with the inputs it had.
func (m Model) gardenAssistRetry(s *assistSession) (Model, tea.Cmd) {
	if s.last == nil || s.err == nil {
		return m, nil
	}
	return m.gardenAssistSend(s, *s.last)
}

// gardenAssistCancel stops the flight. The session's text is not touched; a reply that still arrives is stale.
func (m Model) gardenAssistCancel(s *assistSession) Model {
	a := &m.garden.assist
	if a.running(s) {
		a.flight.cancel()
		a.flight = nil
		s.notice = "Cancelled. Nothing was changed."
	}
	s.confirm = false
	return m
}

// --- replies ---

// assistLand matches a reply to the flight and the session. The flight ends whenever the reply is the flight's own;
// the content applies only when every identity matches. ok is false when the reply must not change the session.
func (m *Model) assistLand(sessionID, seq uint64, requestID string, st gardenapi.Stamp, err error) (s *assistSession, ok bool) {
	g := &m.garden
	a := &g.assist
	f := a.flight
	if f == nil || f.session != sessionID || f.seq != seq {
		// Cancelled, superseded or dropped with its session: it says nothing, unless it is a real answer nobody wants.
		if err == nil || !errors.Is(err, context.Canceled) {
			if s := a.byID(sessionID); s != nil && a.open == s {
				s.notice = assistOutOfDate
			}
		}
		return nil, false
	}
	a.flight = nil
	f.cancel()
	s = a.byID(sessionID)
	if s == nil {
		return nil, false
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, false
		}
		s.err = safeGardenError(err)
		if s.stage == assistSetup {
			s.setFocus(sfAsk) // out of the text field, so R retries
		}
		return s, false
	}
	cur := g.status.Stamp
	if requestID != f.requestID || st.LocalRepoID != s.stamp.LocalRepoID || st.GardenID != s.stamp.GardenID ||
		st.Epoch != s.stamp.Epoch || cur.Epoch != s.stamp.Epoch || cur.LocalRepoID != s.stamp.LocalRepoID || cur.GardenID != s.stamp.GardenID {
		if a.open == s {
			s.notice = assistOutOfDate
		}
		return nil, false
	}
	return s, true
}

func (m Model) onAssistQuestions(msg assistQuestionsMsg) (Model, tea.Cmd) {
	s, ok := m.assistLand(msg.session, msg.seq, msg.view.RequestID, msg.view.Stamp, msg.err)
	if !ok {
		return m.assistReplyBackground(s), nil
	}
	s.err, s.notice = nil, ""
	first := len(s.questions)
	seen := map[string]bool{}
	for _, q := range s.questions {
		seen[q.ID] = true
	}
	added := 0
	for _, q := range msg.view.Set.Questions {
		if q.ID == "" || seen[q.ID] {
			continue
		}
		seen[q.ID] = true
		s.questions = append(s.questions, q)
		added++
	}
	s.round = max(s.round+1, msg.view.Round)
	if t := strings.TrimSpace(msg.view.Set.Understanding); t != "" {
		s.understanding = t
	}
	s.stage = assistQuestions
	if added > 0 {
		s.sel = first
	} else {
		s.notice = "No further questions — draft now."
		s.sel = max(0, min(s.sel, len(s.questions)-1))
	}
	s.resetPanel()
	return m.assistReplyBackground(s), nil
}

// resetPanel shows the selected question's answer panel from the rail, after the questions or the selection changed.
func (s *assistSession) resetPanel() {
	s.taFor = ""
	s.setZone(zoneRail)
	s.loadEditor()
	s.optCur = 0
	if q, ok := s.current(); ok {
		s.optCur = selectedRow(q, s.answer(q))
	}
}

func (m Model) onAssistDraft(msg assistDraftMsg) (Model, tea.Cmd) {
	s, ok := m.assistLand(msg.session, msg.seq, msg.view.RequestID, msg.view.Stamp, msg.err)
	if !ok {
		return m.assistReplyBackground(s), nil
	}
	p := msg.view.Proposal
	s.err, s.notice = nil, ""
	s.proposal, s.inputHash = &p, msg.view.InputHash
	input := ""
	if s.last != nil {
		input = s.last.ctx.Draft
	}
	s.review = newAssistReview(input, p)
	s.refine = false
	s.stage = assistReview
	s.setZone(zoneRail)
	return m.assistReplyBackground(s), nil
}

// assistReplyBackground tells the Drafts list a reply landed for a session nobody is looking at.
func (m Model) assistReplyBackground(s *assistSession) Model {
	g := &m.garden
	if s == nil || g.assist.open == s || s.err != nil {
		return m
	}
	g.drafts.notice = "An AI reply is ready. Press i on the draft to continue."
	if s.isNew {
		g.drafts.notice = "An AI reply is ready for your new idea. Press i to continue."
	}
	return m
}

func (m Model) onAssistTick(msg assistTickMsg) (Model, tea.Cmd) {
	f := m.garden.assist.flight
	if f == nil || f.session != msg.session || f.seq != msg.seq {
		return m, nil // the flight ended: stop ticking
	}
	f.secs++
	return m, assistTickCmd(msg.session, msg.seq)
}

// gardenAssistMsg handles the assist replies; handled is false for any other message.
func (m Model) gardenAssistMsg(msg tea.Msg) (handled bool, nm Model, cmd tea.Cmd) {
	switch msg := msg.(type) {
	case assistQuestionsMsg:
		nm, cmd = m.onAssistQuestions(msg)
	case assistDraftMsg:
		nm, cmd = m.onAssistDraft(msg)
	case assistTickMsg:
		nm, cmd = m.onAssistTick(msg)
	default:
		return false, m, nil
	}
	return true, nm, cmd
}

// --- keys ---

func (m Model) gardenAssistKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	g := &m.garden
	d := &g.drafts
	s := g.assist.open
	if s == nil {
		m = m.gardenAssistLeave()
		d.stage = draftsList
		return m, nil
	}
	m.gardenAssistSize()
	k := msg.String()
	if s.confirm {
		switch k {
		case "enter", "y":
			return m.gardenAssistCancel(s), nil
		case "l":
			s.confirm = false
			return m.gardenAssistLeave(), nil
		}
		s.confirm = false
		return m, nil
	}
	if s.optsOpen {
		return m.gardenAssistOptionsKey(s, msg)
	}
	if k == "esc" && g.assist.running(s) && !(s.stage == assistQuestions && s.zone != zoneRail) && !s.refine {
		s.confirm = true
		return m, nil
	}
	switch s.stage {
	case assistQuestions:
		return m.gardenAssistQuestionsKey(s, msg)
	case assistReview:
		return m.gardenAssistReviewKey(s, msg)
	}
	return m.gardenAssistSetupKey(s, msg)
}

func (m Model) gardenAssistOptionsKey(s *assistSession, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+s":
		s.optsOpen = false
		return m.gardenAssistAsk(s)
	case "ctrl+d":
		s.optsOpen = false
		return m.gardenAssistDraftNow(s)
	}
	cmd, done := s.opts.key(msg)
	if done {
		s.optsOpen = false
	}
	return m, cmd
}

func (m Model) gardenAssistSetupKey(s *assistSession, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	switch k {
	case "ctrl+s":
		return m.gardenAssistAsk(s)
	case "ctrl+d":
		return m.gardenAssistDraftNow(s)
	case "esc":
		return m.gardenAssistLeave(), nil
	case "tab":
		s.cycle(1)
		return m, nil
	case "shift+tab":
		s.cycle(-1)
		return m, nil
	}
	switch s.focus {
	case sfDesc:
		var cmd tea.Cmd
		s.notice = ""
		s.desc, cmd = s.desc.Update(msg)
		return m, cmd
	case sfOptions:
		switch k {
		case "enter", "o":
			s.optsOpen = true
			s.opts.setFocus(optAccount)
		}
	case sfAsk:
		switch k {
		case "enter":
			return m.gardenAssistAsk(s)
		case "right", "l":
			s.setFocus(sfDraft)
		}
	case sfDraft:
		switch k {
		case "enter":
			return m.gardenAssistDraftNow(s)
		case "left", "h":
			s.setFocus(sfAsk)
		}
	}
	if k == "R" {
		return m.gardenAssistRetry(s)
	}
	if k == "p" && s.proposal != nil && s.review != nil && s.focus != sfDesc {
		s.stage = assistReview
	}
	return m, nil
}

func (m Model) gardenAssistQuestionsKey(s *assistSession, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	// These keys work in every zone, the text field included: they are not printable.
	switch k {
	case "ctrl+s":
		return m.gardenAssistDraft(s, "")
	case "tab":
		s.cycleZone(1)
		return m, nil
	case "shift+tab":
		s.cycleZone(-1)
		return m, nil
	case "pgdown", "pgup":
		delta := 1
		if k == "pgup" {
			delta = -1
		}
		s.moveSel(delta)
		if s.zone != zoneRail {
			if q, ok := s.current(); ok {
				s.setZone(defaultZone(q))
			}
		}
		return m, nil
	case "ctrl+down":
		s.qScroll++ // the view clamps it to the text it has
		return m, nil
	case "ctrl+up":
		s.qScroll = max(0, s.qScroll-1)
		return m, nil
	case "esc":
		switch s.zone {
		case zoneText:
			s.setZone(zoneOptions)
		case zoneOptions:
			s.setZone(zoneRail)
		default:
			return m.gardenAssistLeave(), nil
		}
		return m, nil
	}
	switch s.zone {
	case zoneText:
		return m.gardenAssistAnswerKey(s, msg)
	case zoneOptions:
		s.optionsKey(k)
		return m, nil
	}
	switch k {
	case "up", "k":
		s.moveSel(-1)
	case "down", "j":
		s.moveSel(1)
	case "enter", "right", "l":
		if q, ok := s.current(); ok {
			s.setZone(defaultZone(q))
		}
	case "m":
		return m.gardenAssistMore(s)
	case "p":
		if s.proposal != nil && s.review != nil {
			s.stage = assistReview
		}
	case "R":
		return m.gardenAssistRetry(s)
	}
	return m, nil
}

// optionsKey moves the cursor or selects. Digits select a row at once, so they never reach the text field.
func (s *assistSession) optionsKey(k string) {
	q, ok := s.current()
	if !ok {
		return
	}
	switch k {
	case "up", "k":
		s.optCur = max(0, s.optCur-1)
	case "down", "j":
		s.optCur = min(optionRows(q)-1, s.optCur+1)
	case "enter", " ":
		s.selectRow(s.optCur)
	default:
		if len(k) == 1 && k[0] >= '1' && k[0] <= '9' {
			s.selectRow(int(k[0] - '1'))
		}
	}
}

func (m Model) gardenAssistAnswerKey(s *assistSession, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	before := s.ta.Value()
	var cmd tea.Cmd
	s.ta, cmd = s.ta.Update(msg)
	if after := s.ta.Value(); after != before {
		if len(after) > assist.MaxAnswerBytes {
			s.ta.SetValue(before)
			s.notice = "An answer holds up to 2 KB."
		} else {
			s.storeText(after)
			s.notice = ""
		}
	}
	return m, cmd
}
