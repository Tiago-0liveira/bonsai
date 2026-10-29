package ui

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/reviewpass"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/accounts"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/launchform"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// Local implementation launch (plan 09). Nothing here starts work by itself: reading an Issue, approving a plan,
// refreshing or navigating only read. One explicit, cancel-first confirmation of an exact preview starts one local
// holder; the worker that runs it is never tied to the form, so closing the form or leaving the screen cannot
// abandon a start halfway. The terminal reuses the Agents controller (agentattach.go) as is and only detaches.

// launchReadTimeout bounds the read-only calls so a hung backend cannot leave a screen loading forever. Writes
// have none: abandoning one halfway is worse than waiting.
const launchReadTimeout = 30 * time.Second

type launchStage int

const (
	launchStageNone      launchStage = iota
	launchStageHub                   // the Issue's attempts, newest first
	launchStageChoose                // account, model, effort and native permissions
	launchStageIsolation             // branch, base ref and remote
	launchStageConfirm               // the cancel-first confirmation of one action
	launchStageAttempt               // one attempt in full
	launchStageSession               // the attempt's terminal, reused from the Agents controller
)

// launchAction is what a confirmation, once accepted, does. Each has its own confirmation and its own backend call.
type launchAction int

const (
	launchActStart launchAction = iota
	launchActRetry
	launchActStop
	launchActCleanup
	launchActPublish
)

type gardenLaunchState struct {
	stage launchStage
	// origin is where closing the form goes back to.
	origin launchStage

	attempts        []launch.Attempt // oldest first, as read
	reviews         map[string]reviewpass.Pass
	activity        map[string]gardenapi.LaunchActivity
	activityStale   map[string]bool
	reviewUnknown   map[string]bool
	overviewGen     uint64
	overviewPending bool
	loaded          bool
	readErr         string
	sel             int
	attempt         string           // the attempt in detail or in the terminal
	run             int              // index into the attempt's runs; -1 follows the newest
	adapted         agents.AccountID // the account whose Codex form got its own sandbox and approval controls

	dlg *launchDialog
	req gardenapi.LaunchPreviewRequest

	preview *gardenapi.LaunchPreviewView
	// stale means the preview no longer fits what is on screen (the terminal was resized): Confirm is off until
	// the refreshed preview answers.
	stale bool

	action         launchAction
	btn            int // 0 Cancel, 1 Confirm
	escalate       bool
	reviewEscalate bool
	stopGen        uint64
	stopPolls      int
	stopPending    bool
	stopAttempt    string
	hooks          bool
	cleanup        *gardenapi.LaunchCleanupView
	scroll         int

	iso      [3]textinput.Model
	isoFocus int
	isoFrom  launchStage

	previewSeq, readSeq uint64
	readGen             uint64 // generation of the newest accepted read: an older one is obsolete
	// inflight is an accepted write: every key is ignored until it answers, so a second Enter cannot repeat it.
	inflight bool
	note     string
}

type (
	gardenLaunchPreviewMsg struct {
		seq   uint64
		issue uint64
		view  gardenapi.LaunchPreviewView
		err   error
	}
	// gardenLaunchStartMsg is the answer of an accepted start. It is handled whatever the screen shows now.
	gardenLaunchStartMsg struct {
		issue     uint64
		scope     launchScope
		previewID string
		view      gardenapi.LaunchAttemptView
		err       error
	}
	gardenLaunchAttemptsMsg struct {
		overviewGen uint64
		stopGen     uint64
		seq         uint64
		issue       uint64
		view        gardenapi.LaunchAttemptsView
		err         error
	}
	gardenLaunchActionMsg struct {
		kind      launchAction
		scope     launchScope
		issue     uint64
		attemptID string
		view      gardenapi.LaunchAttemptView
		err       error
	}
	gardenLaunchCleanupMsg struct {
		seq   uint64
		issue uint64
		id    string
		view  gardenapi.LaunchCleanupView
		err   error
	}
)

const launchNotReadyText = "Only a Ready Issue, with its exact approved specification and plan, can start an implementation."

const launchUnavailableText = "Starting an implementation locally is unavailable here: this Garden runtime has no local launch."

// launchBackend is the optional local launch of the Garden backend.
func (m Model) launchBackend() (gardenapi.LaunchBackend, bool) {
	lb, ok := m.garden.api.(gardenapi.LaunchBackend)
	return lb, ok
}

func (m Model) launchSupported() bool {
	_, ok := m.launchBackend()
	return ok
}

// launchOffered reports whether the opened Issue is Ready, the only stage a local start is offered from. The
// backend still verifies the exact approved pair live before anything is previewed.
func (m Model) launchOffered() bool {
	r, _, ok := m.garden.board.collection.Find(m.garden.board.opened)
	return ok && r.Item.Classification.Stage == garden.StageReady
}

// launchActive reports whether the opened Issue shows a launch screen.
func (m Model) launchActive() bool {
	return m.garden.launch.stage != launchStageNone && m.garden.board.opened != 0
}

// launchSessionShown reports whether an attempt's terminal is the body in view.
func (m Model) launchSessionShown() bool {
	return m.layoutKind == LayoutRedesign && m.workspace == wsGarden && m.launchActive() && m.garden.launch.stage == launchStageSession &&
		m.garden.hasStatus && m.garden.sub != subDrafts && m.agents.view != nil
}

// launchDims is the terminal area a start would size its terminal to, clamped to what the holder accepts. The
// confirmation freezes it, so a resize that changes it asks again.
func (m Model) launchDims() (cols, rows int) {
	cols, rows = m.plannerAreaSize()
	if cols <= 0 || rows <= 0 {
		cols, rows = 80, 24 // the terminal resizes to the real area when it is focused
	}
	return min(max(cols, 1), 500), min(max(rows, 1), 500)
}

// --- attempts of the Issue ---

// display lists the attempts newest first, the order the screen shows them in.
func (l gardenLaunchState) display() []launch.Attempt {
	out := make([]launch.Attempt, len(l.attempts))
	for i, a := range l.attempts {
		out[len(out)-1-i] = a
	}
	return out
}

func (l gardenLaunchState) selected() (launch.Attempt, bool) {
	list := l.display()
	if l.sel < 0 || l.sel >= len(list) {
		return launch.Attempt{}, false
	}
	return list[l.sel], true
}

// current is the attempt the detail or the terminal shows.
func (l gardenLaunchState) current() (launch.Attempt, bool) {
	for _, a := range l.attempts {
		if a.ID == l.attempt {
			return a, true
		}
	}
	return launch.Attempt{}, false
}

// replaceAttempts swaps the shown history for a newer read and keeps the cursor on the same attempt.
func (l *gardenLaunchState) replaceAttempts(list []launch.Attempt) {
	var keep string
	if a, ok := l.selected(); ok {
		keep = a.ID
	}
	l.attempts = sortedAttempts(list)
	l.loaded = true
	l.sel = 0
	for i, a := range l.display() {
		if a.ID == keep {
			l.sel = i
		}
	}
}

// launchScope is the identity an accepted write was requested under. A reply is kept only when the record it
// returns is exactly that operation's: another clone, Issue or attempt is never merged into this history.
type launchScope struct {
	local     garden.LocalRepoID
	repo      garden.RepoID
	issue     garden.IssueID
	attemptID string
}

func scopeOfAttempt(a launch.Attempt) launchScope {
	return launchScope{local: a.Local, repo: a.Repo, issue: a.Issue, attemptID: a.ID}
}

// valid reports whether a returned record and stamp belong to the scope. The zero scope validates nothing.
func (s launchScope) valid(a launch.Attempt, st gardenapi.Stamp) bool {
	return s.local != "" && a.ID != "" && a.ID == s.attemptID && a.Local == s.local && a.Repo == s.repo && a.Issue == s.issue &&
		st.LocalRepoID == string(s.local)
}

// merge keeps a record unless the history already holds a newer revision of it, so a late or older reply can
// never move an attempt backwards.
func (l *gardenLaunchState) merge(a launch.Attempt) {
	list := append([]launch.Attempt(nil), l.attempts...)
	for i := range list {
		if list[i].ID != a.ID {
			continue
		}
		if list[i].Revision <= a.Revision {
			list[i] = a
			l.replaceAttempts(list)
		}
		return
	}
	l.replaceAttempts(append(list, a))
}

func sortedAttempts(list []launch.Attempt) []launch.Attempt {
	out := append([]launch.Attempt(nil), list...)
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].RequestedAt.Equal(out[j].RequestedAt) {
			return out[i].RequestedAt.Before(out[j].RequestedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// newestRun is the run a terminal opens on: the last one started.
func newestRun(a launch.Attempt) (launch.Run, bool) {
	if len(a.Runs) == 0 {
		return launch.Run{}, false
	}
	best := a.Runs[0]
	for _, r := range a.Runs[1:] {
		if r.Number > best.Number {
			best = r
		}
	}
	return best, true
}

func attemptTerminal(a launch.Attempt) bool {
	switch a.State {
	case launch.StateAwaitingReview, launch.StateFailed, launch.StateStopped:
		return true
	}
	return false
}

func attemptRetryable(a launch.Attempt) bool {
	switch a.State {
	case launch.StateFailed, launch.StateStopped, launch.StateRecoverable:
		return true
	}
	return false
}

func attemptCleanable(a launch.Attempt) bool {
	return !a.State.Active() && a.State != launch.StateCleaned
}

// hooksNeedAck is true when an earlier run of the hooks may have partly executed, which a retry must acknowledge.
func hooksNeedAck(a launch.Attempt) bool {
	switch a.Steps[launch.StepWorktree].Code {
	case "hooks-failed", "hooks-unknown":
		return true
	}
	return false
}

// sameClone accepts a local record for this Garden even when a newer unrelated reply was accepted first: attempts
// are durable local facts, so only another clone's answer is refused.
func (m Model) sameClone(s gardenapi.Stamp) bool {
	last := m.garden.gate.Last()
	if s.LocalRepoID == "" {
		return false
	}
	if last.LocalRepoID == "" {
		return true
	}
	return s.LocalRepoID == last.LocalRepoID && s.GardenID == last.GardenID && s.Epoch >= last.Epoch
}

// --- permissions ---

// launchPermissionBlocker says why the resolved native permissions cannot be confirmed, or "". A provider whose
// permission is not fully resolved never reaches an active Confirm.
func launchPermissionBlocker(r agents.LaunchResolution) string {
	o := r.Launch
	switch string(r.Provider) {
	case "claude":
		if o.PermissionMode == "" {
			return "The Claude permission mode is unresolved. Choose one explicitly, including default."
		}
	case "codex":
		if o.Sandbox == "" || (o.Sandbox != "bypass" && o.Approval == "") {
			return "The Codex sandbox and approval are not both resolved. Choose them explicitly."
		}
	case "antigravity":
		if o.FullAccess == nil {
			return "Antigravity full access is unresolved. Choose it explicitly."
		}
	default:
		return "These native permissions are not known to this version, so they cannot be confirmed."
	}
	return ""
}

// launchEscalated reports a native mode that lets the agent act without asking. A retry has no preview to carry
// it, so the attempt's own accepted options are judged the same way the backend does.
func launchEscalated(provider agents.ProviderID, o launch.Options) bool {
	switch string(provider) {
	case "codex":
		return o.Sandbox == "danger-full-access" || o.Sandbox == "bypass"
	case "claude":
		return o.PermissionMode == "bypassPermissions" || o.PermissionMode == "auto" || o.PermissionMode == "dontAsk"
	case "antigravity":
		return o.FullAccess != nil && *o.FullAccess
	}
	return false
}

// launchOverrides is what the form chose, as overrides of the profile. Only what the user changed is sent, so an
// inherited default stays labelled as inherited and a stored pair the form cannot show (a Codex sandbox and
// approval that match no preset) is never replaced by what the form happens to display. A native field the
// profile does not store is the exception: its visible value is the user's explicit choice, which is how an
// unresolved permission gets resolved.
func launchOverrides(d launchDialog) agents.LaunchOptions {
	if !d.formLoaded {
		return agents.LaunchOptions{}
	}
	changes := d.form.Changes()
	values := d.form.Values()
	stored := d.Defaults().Values
	var fields []agents.LaunchField
	picked := map[string]string{}
	model := ""
	formFields := launchDialogFields(d.Defaults().LaunchFields)
	if a, ok := d.Current(); ok && string(a.Provider) == "codex" {
		formFields = codexNativeFields(formFields)
	}
	for _, f := range formFields {
		_, changed := changes[f.ID]
		isModel := f.ID == "model" || f.Kind == agents.LaunchFieldModel
		switch {
		case isModel:
			if changed {
				model = values[f.ID]
			}
			continue
		case f.ID == "effort":
			if !changed {
				continue
			}
		default:
			preset := false
			for _, o := range f.Options {
				if len(o.Set) > 0 {
					preset = true
				}
			}
			if !changed && (preset || stored[f.ID] != "") {
				continue
			}
		}
		fields = append(fields, f)
		picked[f.ID] = values[f.ID]
	}
	o := agents.LaunchOptionsFrom(fields, model, picked)
	if o.Sandbox == "bypass" {
		o.Approval = "" // a bypassed sandbox has no approval; the backend refuses a pair
	}
	return o
}

// --- opening ---

// gardenLaunchOpen changes run settings on a Ready Issue: it opens the account and options form. It starts nothing and reads
// nothing but the accounts.
func (m Model) gardenLaunchOpen() (Model, tea.Cmd) {
	g := &m.garden
	l := &g.launch
	if g.api == nil || g.board.opened == 0 || l.inflight {
		return m, nil
	}
	if !m.launchSupported() {
		g.notice = launchUnavailableText
		return m, nil
	}
	if !m.launchOffered() {
		g.notice = launchNotReadyText
		return m, nil
	}
	if m.agents.api.Accounts == nil {
		g.notice = "Agent accounts are unavailable here, so no implementation can run."
		return m, nil
	}
	origin := launchStageNone
	if l.stage == launchStageHub {
		origin = launchStageHub
	}
	cols, rows := m.launchDims()
	m.agents.launchSeq++
	d := newLaunchDialog(m.agents.launchSeq, m.agents.api.Accounts, launchTarget{branch: "isolated worktree / local machine"}, "", m.agents.usage, m.agentGlyphsASCII())
	d.SetSize(m.width, m.gardenBoardRows())
	keep := l.attempts
	loaded := l.loaded
	*l = gardenLaunchState{stage: launchStageChoose, origin: origin, attempts: keep, reviews: l.reviews, activity: l.activity, stopGen: l.stopGen + 1, loaded: loaded, dlg: &d,
		req: gardenapi.LaunchPreviewRequest{IssueID: uint64(g.board.opened), Cols: cols, Rows: rows}, previewSeq: l.previewSeq, readSeq: l.readSeq, run: -1}
	m.shellFocus = shellInput
	return m, tea.Batch(d.Init(), m.launchSubscribe())
}

// gardenLaunchHub is i on an opened Issue: the local attempts, which exist whether or not a new launch is allowed.
func (m Model) gardenLaunchHub() (Model, tea.Cmd) {
	g := &m.garden
	l := &g.launch
	if g.api == nil || g.board.opened == 0 {
		return m, nil
	}
	if !m.launchSupported() {
		g.notice = launchUnavailableText
		return m, nil
	}
	if l.stage == launchStageNone {
		l.stage, l.run = launchStageHub, -1
	}
	sub := m.launchSubscribe()
	m, read := m.launchLoad(uint64(g.board.opened))
	return m, tea.Batch(sub, read)
}

// launchClose leaves the launch screens for the Issue document. The terminal is detached, never stopped.
func (m Model) launchClose() (Model, tea.Cmd) {
	l := &m.garden.launch
	var rel tea.Cmd
	if l.stage == launchStageSession {
		rel = m.plannerRelease()
	}
	prev := *l
	*l = gardenLaunchState{inflight: prev.inflight, previewSeq: prev.previewSeq + 1, readSeq: prev.readSeq + 1, attempts: prev.attempts, reviews: prev.reviews, activity: prev.activity, activityStale: prev.activityStale, reviewUnknown: prev.reviewUnknown, loaded: prev.loaded, run: -1, stopGen: prev.stopGen + 1, overviewGen: prev.overviewGen + 1}
	if m.shellFocus == shellInput {
		m.shellFocus = shellBody
	}
	m, refresh := m.handoffRefresh()
	cmd := tea.Batch(rel, m.launchOverviewNext(), refresh)
	return m, cmd
}

// launchDetach releases the terminal when the user leaves the Garden workspace: the attachment closes, the
// holder keeps running.
func (m *Model) launchDetach() tea.Cmd {
	l := &m.garden.launch
	l.stopGen++
	l.stopAttempt = ""
	l.overviewGen++
	l.overviewPending = false
	if l.stage != launchStageSession {
		return nil
	}
	l.stage = launchStageAttempt
	return m.plannerRelease()
}

// --- reads ---

func (m Model) launchLoad(issue uint64) (Model, tea.Cmd) {
	lb, ok := m.launchBackend()
	if !ok || issue == 0 {
		return m, nil
	}
	l := &m.garden.launch
	l.readSeq++
	seq := l.readSeq
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), launchReadTimeout)
		defer cancel()
		v, err := lb.LaunchAttempts(ctx, issue)
		return gardenLaunchAttemptsMsg{seq: seq, issue: issue, view: v, err: err}
	}
}

func (m Model) onGardenLaunchAttempts(msg gardenLaunchAttemptsMsg) (Model, tea.Cmd) {
	g := &m.garden
	l := &g.launch
	if msg.seq != l.readSeq || msg.issue != uint64(g.board.opened) || msg.stopGen != 0 && msg.stopGen != l.stopGen || msg.overviewGen != 0 && (msg.overviewGen != l.overviewGen || !m.launchOverviewWatching()) {
		return m, nil
	}
	if msg.err != nil {
		l.staleActivity()
		// The local list stays: an unreadable store or a remote outage never empties what was known.
		l.readErr = safeGardenError(msg.err).Message
		cmd := tea.Batch(m.launchStopNext(), m.launchOverviewNext())
		return m, cmd
	}
	if !m.sameClone(msg.view.Stamp) || msg.view.IssueID != msg.issue || msg.view.Stamp.Generation < l.readGen {
		l.readErr = staleReply.Message
		cmd := tea.Batch(m.launchStopNext(), m.launchOverviewNext())
		return m, cmd
	}
	local := garden.LocalRepoID(m.garden.gate.Last().LocalRepoID)
	if local == "" {
		local = garden.LocalRepoID(msg.view.Stamp.LocalRepoID)
	}
	l.readErr = ""
	l.staleActivity()
	l.readGen = msg.view.Stamp.Generation
	for _, a := range msg.view.Attempts {
		// Only this clone's attempts of this Issue belong in this history, and an older revision never replaces a newer one.
		if a.ID != "" && a.Local == local && uint64(a.Issue) == msg.issue {
			l.merge(a)
			l.setReviewUnknown(a.ID, msg.view.ReviewsErr)
			if pass, ok := msg.view.Reviews[a.ID]; ok {
				l.keepReview(a.ID, &pass)
			}
			if obs, ok := msg.view.Activity[a.ID]; ok {
				prior, known := l.activity[a.ID]
				if !known || obs.ObservedAt.After(prior.ObservedAt) {
					if l.activity == nil {
						l.activity = map[string]gardenapi.LaunchActivity{}
					}
					l.activity[a.ID] = obs
					l.activityStale[a.ID] = false
				}
			}
			if a.ID == l.attempt && !a.State.Active() && l.note == "Stop requested; closure not yet proven" {
				l.note = ""
			}
		}
	}
	l.loaded = true
	cmd := tea.Batch(m.launchStopNext(), m.launchOverviewNext())
	return m, cmd
}

func (m Model) launchPreview() (Model, tea.Cmd) {
	lb, ok := m.launchBackend()
	if !ok {
		return m, nil
	}
	l := &m.garden.launch
	l.escalate, l.reviewEscalate, l.btn = false, false, 0
	l.previewSeq++
	seq, req := l.previewSeq, l.req
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), launchReadTimeout)
		defer cancel()
		v, err := lb.PreviewLaunch(ctx, req)
		return gardenLaunchPreviewMsg{seq: seq, issue: req.IssueID, view: v, err: err}
	}
}

// onGardenLaunchSubmit is Ctrl+S or the form's primary button. It only asks for a preview: the form never starts work.
func (m Model) onGardenLaunchSubmit(msg launchSubmitMsg) (Model, tea.Cmd) {
	g := &m.garden
	l := &g.launch
	if l.stage != launchStageChoose || l.dlg == nil || l.dlg.id != msg.dlg || l.inflight {
		return m, nil
	}
	cols, rows := m.launchDims()
	// An untouched name stays empty so the backend names the session after the Issue, not after the account.
	name := ""
	if l.dlg.nameTouched {
		name = strings.TrimSpace(l.dlg.name.Value())
	}
	l.req.IssueID, l.req.AccountID, l.req.Launch, l.req.Name = uint64(g.board.opened), msg.account, launchOverrides(*l.dlg), name
	l.req.Cols, l.req.Rows = cols, rows
	return m.launchPreview()
}

func (m Model) onGardenLaunchPreview(msg gardenLaunchPreviewMsg) (Model, tea.Cmd) {
	g := &m.garden
	l := &g.launch
	if msg.seq != l.previewSeq || l.stage == launchStageNone || msg.issue != uint64(g.board.opened) {
		return m, nil
	}
	l.escalate, l.reviewEscalate = false, false
	if msg.err != nil {
		fail := safeGardenError(msg.err)
		l.stale = false
		if l.dlg == nil {
			l.preview, l.note = nil, launchErrText(msg.err)
			return m, nil
		}
		if l.stage != launchStageChoose {
			// The confirmation no longer holds: go back to the form with the reason.
			l.preview, l.stage = nil, launchStageChoose
			m.shellFocus = shellInput
		}
		if l.dlg != nil {
			d := l.dlg.failed(failText(fail))
			l.dlg = &d
		}
		return m, nil
	}
	if !g.gate.Accept(msg.view.Stamp) || msg.view.IssueID != msg.issue {
		// A rejected answer never re-enables the confirmation it was meant to replace.
		l.stale, l.preview = false, nil
		if l.dlg == nil {
			l.note = staleReply.Message
			return m, nil
		}
		if l.stage != launchStageChoose {
			l.stage = launchStageChoose
			m.shellFocus = shellInput
		}
		if l.dlg != nil {
			d := l.dlg.failed(failText(staleReply))
			l.dlg = &d
		}
		return m, nil
	}
	v := msg.view
	// The frozen dimensions must still be the area the terminal has now: a resize that raced the answer asks again.
	if cols, rows := m.launchDims(); v.Preview.Request.Cols != cols || v.Preview.Request.Rows != rows {
		l.preview, l.stage, l.stale, l.btn = &v, launchStageConfirm, true, 0
		l.action = launchActStart
		l.req.Cols, l.req.Rows = cols, rows
		m.shellFocus = shellBody
		return m.launchPreview()
	}
	l.preview, l.stale, l.stage = &v, false, launchStageConfirm
	l.action, l.btn, l.escalate, l.hooks, l.scroll, l.note = launchActStart, 0, false, false, 0, ""
	if l.dlg != nil {
		d := *l.dlg
		d.starting, d.err = false, ""
		l.dlg = &d
	}
	m.shellFocus = shellBody
	return m, nil
}

// gardenLaunchResize keeps a confirmation honest about the terminal it will start: when the resize changes the
// dimensions the start would use, the preview is read again and Confirm stays off until it answers.
func (m Model) gardenLaunchResize() (Model, tea.Cmd) {
	l := &m.garden.launch
	if l.dlg != nil {
		l.dlg.SetSize(m.width, m.gardenBoardRows())
	}
	if l.stage != launchStageConfirm || l.action != launchActStart || l.preview == nil || l.inflight {
		return m, nil
	}
	cols, rows := m.launchDims()
	p := l.preview.Preview.Request
	if cols == p.Cols && rows == p.Rows && !l.stale {
		return m, nil
	}
	if l.stale && cols == l.req.Cols && rows == l.req.Rows {
		return m, nil // the refresh already in flight is for exactly this area
	}
	l.req.Cols, l.req.Rows = cols, rows
	l.stale, l.btn = true, 0
	return m.launchPreview()
}

// --- the accepted writes ---

func (m Model) launchRunStart() (Model, tea.Cmd) {
	g := &m.garden
	l := &g.launch
	lb, ok := m.launchBackend()
	if !ok || l.preview == nil || l.inflight {
		return m, nil
	}
	// The exact preview the user confirmed, unchanged: the backend verifies it again against fresh data.
	req := gardenapi.LaunchStartRequest{Preview: *l.preview, Confirmed: true, EscalationConfirmed: l.escalate, ReviewEscalationConfirmed: l.reviewEscalate}
	issue, id := uint64(g.board.opened), l.preview.Preview.ID
	pv := l.preview.Preview
	scope := launchScope{local: pv.Remote.Local, repo: pv.Request.Target.Repo, issue: pv.Request.Target.Issue, attemptID: id}
	l.inflight = true
	return m, func() tea.Msg {
		v, err := lb.StartLaunch(context.Background(), req)
		return gardenLaunchStartMsg{issue: issue, scope: scope, previewID: id, view: v, err: err}
	}
}

func (m Model) onGardenLaunchStart(msg gardenLaunchStartMsg) (Model, tea.Cmd) {
	g := &m.garden
	l := &g.launch
	l.inflight = false
	a := msg.view.Attempt
	text := ""
	if msg.err != nil {
		text = launchErrText(msg.err)
	}
	if !msg.scope.valid(a, msg.view.Stamp) {
		a = launch.Attempt{} // another operation's record is never shown as this one
	}
	if a.ID != "" && msg.issue == uint64(g.board.opened) {
		l.merge(a)
		l.keepReview(a.ID, msg.view.Review)
		l.setReviewUnknown(a.ID, msg.view.ReviewUnknown)
	}
	m, read := m.launchLoad(msg.issue)
	l = &m.garden.launch
	here := m.workspace == wsGarden && l.stage == launchStageConfirm && l.action == launchActStart && l.preview != nil &&
		l.preview.Preview.ID == msg.previewID && msg.issue == uint64(m.garden.board.opened)
	switch {
	case here && a.ID != "":
		l.stage, l.attempt, l.run, l.note, l.preview = launchStageAttempt, a.ID, -1, text, nil
		m.shellFocus = shellBody
	case here:
		if msg.err != nil && safeGardenError(msg.err).Code == gardenapi.CodeLaunchChanged {
			l.preview, l.stale, l.btn = nil, true, 0
			l.note = "The review policy changed since this preview. Preview again."
			m.shellFocus = shellBody
			return m, read
		}
		if l.dlg == nil {
			l.preview, l.note = nil, text
			return m, read
		}
		// Refused before anything was reserved: nothing exists, so the form is still the place to fix it.
		l.stage, l.preview = launchStageChoose, nil
		m.shellFocus = shellInput
		if l.dlg != nil {
			d := l.dlg.failed(failText(safeGardenError(msg.err)))
			l.dlg = &d
		}
	default:
		// The user went elsewhere: the record is kept, the screen is not taken back.
		if l.stage == launchStageConfirm {
			l.stage, l.preview = launchStageHub, nil
		}
		if a.ID != "" {
			m.garden.board.notice = "Implementation attempt " + a.ID + " was recorded. Open Implementation attempts (i) to see it."
		}
	}
	return m, read
}

func (m Model) launchRunAction() (Model, tea.Cmd) {
	g := &m.garden
	l := &g.launch
	lb, ok := m.launchBackend()
	a, have := l.current()
	if !ok || !have || l.inflight {
		return m, nil
	}
	issue, id, kind := uint64(g.board.opened), a.ID, l.action
	scope := scopeOfAttempt(a)
	var run func() (gardenapi.LaunchAttemptView, error)
	switch kind {
	case launchActRetry:
		req := launch.RetryRequest{AttemptID: id, Confirmed: true, RetryHooks: l.hooks, EscalationConfirmed: l.escalate}
		run = func() (gardenapi.LaunchAttemptView, error) { return lb.RetryLaunch(context.Background(), req) }
	case launchActStop:
		run = func() (gardenapi.LaunchAttemptView, error) { return lb.StopLaunch(context.Background(), id) }
	case launchActPublish:
		req := launch.PublishRequest{AttemptID: id, Confirmed: true}
		run = func() (gardenapi.LaunchAttemptView, error) { return lb.PublishLaunch(context.Background(), req) }
	case launchActCleanup:
		if l.cleanup == nil {
			return m, nil
		}
		// The preview the user saw, unchanged: a different revision or fingerprint is refused by the backend.
		view := *l.cleanup
		run = func() (gardenapi.LaunchAttemptView, error) { return lb.CleanupLaunch(context.Background(), view, true) }
	default:
		return m, nil
	}
	l.inflight = true
	return m, func() tea.Msg {
		v, err := run()
		return gardenLaunchActionMsg{kind: kind, scope: scope, issue: issue, attemptID: id, view: v, err: err}
	}
}

func (m Model) onGardenLaunchAction(msg gardenLaunchActionMsg) (Model, tea.Cmd) {
	g := &m.garden
	l := &g.launch
	l.inflight = false
	a := msg.view.Attempt
	text := launchActionDone(msg.kind)
	if msg.err != nil {
		text = launchErrText(msg.err)
	}
	if !msg.scope.valid(a, msg.view.Stamp) {
		a = launch.Attempt{}
	}
	if a.ID != "" && msg.issue == uint64(g.board.opened) {
		l.merge(a)
		l.keepReview(a.ID, msg.view.Review)
		l.setReviewUnknown(a.ID, msg.view.ReviewUnknown)
	}
	m, read := m.launchLoad(msg.issue)
	l = &m.garden.launch
	if l.stage == launchStageConfirm && l.attempt == msg.attemptID {
		l.stage, l.cleanup, l.preview = launchStageAttempt, nil, nil
		if m.workspace != wsGarden {
			l.stage = launchStageHub
		}
		l.note = text
	}
	if msg.kind == launchActStop && a.ID != "" && attemptStopping(a) && m.launchActive() && m.workspace == wsGarden {
		l.stopGen++
		l.stopAttempt, l.stopPolls, l.stopPending = a.ID, 0, false
		return m, tea.Batch(read, m.launchStopNext())
	}
	return m, read
}

func launchActionDone(k launchAction) string {
	switch k {
	case launchActStop:
		return "Stop requested. The attempt shows stopped only once the holder is confirmed closed."
	case launchActRetry:
		return "Retry accepted. The retained worktree was reused, never reset."
	case launchActCleanup:
		return "Cleanup finished. The branch and the attempt history remain."
	case launchActPublish:
		return "Publish finished. Nothing was merged, and no reviewer was asked."
	}
	return ""
}

// launchErrText is the one safe line of a failed call.
func launchErrText(err error) string {
	f := safeGardenError(err)
	text := f.Message
	if f.Next != "" {
		text += " " + f.Next
	}
	return text
}

// --- confirmations ---

// launchOpenConfirm shows the cancel-first confirmation of an action on the attempt in detail.
func (m Model) launchOpenConfirm(kind launchAction) (Model, tea.Cmd) {
	l := &m.garden.launch
	a, ok := l.current()
	if !ok {
		return m, nil
	}
	switch {
	case kind == launchActStop && !a.State.Active():
		l.note = "Only an active attempt can be stopped."
		return m, nil
	case kind == launchActRetry && !attemptRetryable(a):
		l.note = "Only a failed, stopped or recoverable attempt can be retried."
		return m, nil
	case kind == launchActPublish && !attemptTerminal(a):
		l.note = "Pushing needs an ended attempt: a running or unknown holder may still be changing the worktree."
		return m, nil
	case kind == launchActCleanup:
		if !attemptCleanable(a) {
			l.note = "A running attempt cannot be cleaned up, and a cleaned one has nothing left."
			return m, nil
		}
		return m.launchPreviewCleanup(a.ID)
	}
	l.stage, l.action, l.btn, l.escalate, l.hooks, l.scroll, l.note = launchStageConfirm, kind, 0, false, false, 0, ""
	return m, nil
}

func (m Model) launchPreviewCleanup(id string) (Model, tea.Cmd) {
	lb, ok := m.launchBackend()
	if !ok {
		return m, nil
	}
	l := &m.garden.launch
	l.previewSeq++
	seq, issue := l.previewSeq, uint64(m.garden.board.opened)
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), launchReadTimeout)
		defer cancel()
		v, err := lb.PreviewLaunchCleanup(ctx, id)
		return gardenLaunchCleanupMsg{seq: seq, issue: issue, id: id, view: v, err: err}
	}
}

func (m Model) onGardenLaunchCleanup(msg gardenLaunchCleanupMsg) (Model, tea.Cmd) {
	g := &m.garden
	l := &g.launch
	if msg.seq != l.previewSeq || l.stage != launchStageAttempt || l.attempt != msg.id || msg.issue != uint64(g.board.opened) {
		return m, nil
	}
	if msg.err != nil {
		l.note = launchErrText(msg.err)
		return m, nil
	}
	// The sequence number already drops an older answer; the stamp only has to be this clone's.
	if !m.sameClone(msg.view.Stamp) || msg.view.Preview.AttemptID != msg.id {
		l.note = staleReply.Message + " " + staleReply.Next
		return m, nil
	}
	v := msg.view
	l.cleanup, l.stage, l.action, l.btn, l.scroll, l.note = &v, launchStageConfirm, launchActCleanup, 0, 0, ""
	return m, nil
}

// launchConfirmable is whether Confirm can be pressed at all.
func (m Model) launchConfirmable() bool {
	l := m.garden.launch
	if l.inflight || l.stale {
		return false
	}
	switch l.action {
	case launchActStart:
		p := l.preview
		if p == nil || !p.Preview.Remote.Live || launchPermissionBlocker(p.Preview.Account) != "" {
			return false
		}
		return (!p.Preview.Escalated || l.escalate) && (!m.launchReviewNeedsEscalation() || l.reviewEscalate)
	case launchActRetry:
		a, ok := l.current()
		if !ok || !attemptRetryable(a) {
			return false
		}
		return (!launchEscalated(a.Provider, a.Options) || l.escalate) && (!hooksNeedAck(a) || l.hooks)
	case launchActStop:
		a, ok := l.current()
		return ok && a.State.Active()
	case launchActCleanup:
		return l.cleanup != nil && l.cleanup.Preview.Allowed
	case launchActPublish:
		a, ok := l.current()
		return ok && attemptTerminal(a)
	}
	return false
}

// launchCancel backs out of a confirmation without doing anything.
func (m Model) launchCancel() (Model, tea.Cmd) {
	l := &m.garden.launch
	if l.action == launchActStart {
		if l.dlg == nil {
			return m.launchClose()
		}
		l.stage, l.stale = launchStageChoose, false
		l.previewSeq++ // a refresh in flight no longer applies
		if l.dlg != nil {
			d := *l.dlg
			d.starting = false
			l.dlg = &d
		}
		m.shellFocus = shellInput
		return m, nil
	}
	l.stage, l.cleanup, l.btn = launchStageAttempt, nil, 0
	return m, nil
}

// --- the isolation inputs ---

func (m *Model) launchOpenIsolation(from launchStage) {
	l := &m.garden.launch
	var branch, base, remote string
	if p := l.preview; p != nil {
		branch, base, remote = p.Preview.Proposal.Branch, p.Preview.Proposal.BaseRef, p.Preview.Proposal.RemoteName
	}
	if l.req.Branch != "" {
		branch = l.req.Branch
	}
	if l.req.BaseRef != "" {
		base = l.req.BaseRef
	}
	if l.req.RemoteName != "" {
		remote = l.req.RemoteName
	}
	for i, v := range []string{branch, base, remote} {
		ti := textinput.New()
		ti.Prompt = ""
		ti.CharLimit = 200
		ti.Cursor.SetMode(cursor.CursorStatic)
		ti.Placeholder = [...]string{"default: bonsai/<issue>-<plan hash>", "default: the project's upstream", "default: origin"}[i]
		ti.SetValue(v)
		l.iso[i] = ti
	}
	l.isoFocus, l.isoFrom, l.stage = 0, from, launchStageIsolation
	l.iso[0].Focus()
	m.shellFocus = shellInput
}

func (m Model) launchIsolationKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	l := &m.garden.launch
	move := func(d int) {
		l.iso[l.isoFocus].Blur()
		l.isoFocus = (l.isoFocus + d + len(l.iso)) % len(l.iso)
		l.iso[l.isoFocus].Focus()
	}
	switch msg.String() {
	case "esc":
		l.stage = l.isoFrom
		if l.stage == launchStageConfirm {
			m.shellFocus = shellBody
		}
		return m, nil
	case "tab", "down":
		move(1)
		return m, nil
	case "shift+tab", "up":
		move(-1)
		return m, nil
	case "enter", "ctrl+s":
		l.req.Branch = strings.TrimSpace(l.iso[0].Value())
		l.req.BaseRef = strings.TrimSpace(l.iso[1].Value())
		l.req.RemoteName = strings.TrimSpace(l.iso[2].Value())
		// Any edit makes the old confirmation obsolete: the backend answers with a fresh preview and ID.
		if l.isoFrom == launchStageConfirm {
			l.stage, l.stale, l.btn = launchStageConfirm, true, 0
			m.shellFocus = shellBody
			cols, rows := m.launchDims()
			l.req.Cols, l.req.Rows = cols, rows
			return m.launchPreview()
		}
		l.stage = launchStageChoose
		return m, nil
	}
	var cmd tea.Cmd
	l.iso[l.isoFocus], cmd = l.iso[l.isoFocus].Update(msg)
	return m, cmd
}

// --- keys ---

func (m Model) gardenLaunchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	l := &m.garden.launch
	if l.inflight {
		return m, nil // a write answers first; a second Enter must not repeat it
	}
	switch l.stage {
	case launchStageChoose:
		return m.launchChooseKey(msg)
	case launchStageIsolation:
		return m.launchIsolationKey(msg)
	case launchStageConfirm:
		return m.launchConfirmKey(msg)
	case launchStageAttempt:
		return m.launchAttemptKey(msg)
	case launchStageSession:
		return m.launchSessionKey(msg)
	}
	return m.launchHubKey(msg)
}

func (m Model) launchChooseKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	l := &m.garden.launch
	if l.dlg == nil {
		return m, nil
	}
	if msg.String() == "ctrl+e" && !l.dlg.Capturing() {
		m.launchOpenIsolation(launchStageChoose)
		return m, nil
	}
	d, cmd := l.dlg.Update(msg)
	l.dlg = &d
	return m, tea.Batch(cmd, m.launchAdapt())
}

// gardenLaunchChooseClose backs out of the form without starting anything.
func (m Model) gardenLaunchChooseClose() (Model, tea.Cmd) {
	l := &m.garden.launch
	l.previewSeq++
	l.dlg, l.stage, l.preview = nil, l.origin, nil
	m.shellFocus = shellBody
	return m, nil
}

func (m Model) launchConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	l := &m.garden.launch
	k := msg.String()
	switch k {
	case "esc", "backspace":
		return m.launchCancel()
	case "left", "h", "shift+tab":
		l.btn = 0
	case "right", "l", "tab":
		if m.launchConfirmable() {
			l.btn = 1
		}
	case "up", "k":
		l.scroll = max(l.scroll-1, 0)
	case "down", "j":
		l.scroll++
	case "pgup":
		l.scroll = max(l.scroll-5, 0)
	case "pgdown":
		l.scroll += 5
	case "e":
		if l.action == launchActStart {
			if l.preview == nil {
				return m.gardenLaunchOpen()
			}
			m.launchOpenIsolation(launchStageConfirm)
		}
	case "x":
		l.escalate = !l.escalate && m.launchNeedsEscalation()
		if !l.escalate && l.btn == 1 && !m.launchConfirmable() {
			l.btn = 0
		}
	case "r":
		if m.launchReviewNeedsEscalation() {
			l.reviewEscalate = !l.reviewEscalate
			if !m.launchConfirmable() {
				l.btn = 0
			}
		} else if l.action == launchActStart && l.preview == nil {
			return m.launchPreview()
		}
	case "y":
		if a, ok := l.current(); ok && l.action == launchActRetry && hooksNeedAck(a) {
			l.hooks = !l.hooks
			if !l.hooks && l.btn == 1 {
				l.btn = 0
			}
		}
	case "enter":
		if l.btn == 0 || !m.launchConfirmable() {
			return m.launchCancel()
		}
		if l.action == launchActStart {
			return m.launchRunStart()
		}
		return m.launchRunAction()
	}
	return m, nil
}

// launchNeedsEscalation is whether the confirmation in view carries a permission that needs its own acknowledgment.
func (m Model) launchNeedsEscalation() bool {
	l := m.garden.launch
	switch l.action {
	case launchActStart:
		return l.preview != nil && l.preview.Preview.Escalated
	case launchActRetry:
		a, ok := l.current()
		return ok && launchEscalated(a.Provider, a.Options)
	}
	return false
}

func (m Model) launchHubKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	g := &m.garden
	l := &g.launch
	if msg.String() == "esc" || msg.String() == "backspace" {
		return m.launchClose()
	}
	if m.keyIsRefresh(msg) {
		return m.launchLoad(uint64(g.board.opened))
	}
	list := l.display()
	switch msg.String() {
	case "up", "k":
		l.sel = max(l.sel-1, 0)
	case "down", "j":
		l.sel = max(min(l.sel+1, len(list)-1), 0)
	case "s":
		return m.gardenLaunchOpen()
	case "o":
		if a, ok := l.selected(); ok {
			l.attempt, l.run = a.ID, -1
			return m.launchOpenTerminal()
		}
	case "enter":
		if a, ok := l.selected(); ok {
			l.attempt, l.run, l.stage, l.note = a.ID, -1, launchStageAttempt, ""
			return m.launchLoad(uint64(g.board.opened))
		}
	}
	return m, nil
}

func (m Model) launchAttemptKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	g := &m.garden
	l := &g.launch
	if m.keyIsRefresh(msg) {
		return m.launchLoad(uint64(g.board.opened))
	}
	a, ok := l.current()
	switch msg.String() {
	case "esc", "backspace":
		l.stage, l.note = launchStageHub, ""
		return m.launchLoad(uint64(g.board.opened))
	case "o", "enter":
		if ok {
			l.scroll = 0
			return m.launchOpenTerminal()
		}
	case "up", "k":
		l.scroll = max(l.scroll-1, 0)
	case "down", "j":
		l.scroll++
	case "pgup":
		l.scroll = max(l.scroll-5, 0)
	case "pgdown":
		l.scroll += 5
	case "x":
		return m.launchOpenConfirm(launchActStop)
	case "r":
		return m.launchOpenConfirm(launchActRetry)
	case "w":
		return m.launchOpenConfirm(launchActCleanup)
	case "p":
		return m.launchOpenConfirm(launchActPublish)
	case "[", "]":
		if ok && len(a.Runs) > 1 {
			i := l.run
			if i < 0 {
				i = len(a.Runs) - 1
			}
			if msg.String() == "[" {
				i--
			} else {
				i++
			}
			l.run = min(max(i, 0), len(a.Runs)-1)
		}
	}
	return m, nil
}

// runSession is the session of the run the terminal opens on: the selected run, else the newest.
func (l gardenLaunchState) runSession(a launch.Attempt) (string, bool) {
	run, ok := newestRun(a)
	if l.run >= 0 && l.run < len(a.Runs) {
		run, ok = a.Runs[l.run], true
	}
	return run.SessionID, ok && run.SessionID != ""
}

// launchOpenTerminal attaches the stored run's session directly, as an observer. It needs no Worktree selection and
// never starts a session: an unknown or ended one is shown as it is.
func (m Model) launchOpenTerminal() (Model, tea.Cmd) {
	l := &m.garden.launch
	a, ok := l.current()
	if !ok {
		return m, nil
	}
	id, ok := l.runSession(a)
	if !ok {
		l.note = "This attempt has no holder session yet, so there is no terminal to open."
		return m, nil
	}
	l.stage = launchStageSession
	return m, m.plannerAttach(id, false)
}

func (m Model) launchSessionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	g := &m.garden
	l := &g.launch
	if m.keyIsRefresh(msg) {
		return m.launchLoad(uint64(g.board.opened))
	}
	switch msg.String() {
	case "esc", "backspace":
		// Back to the attempt: the attachment is released, the holder is not stopped.
		rel := m.plannerRelease()
		l.stage = launchStageAttempt
		m, cmd := m.launchLoad(uint64(g.board.opened))
		return m, tea.Batch(rel, cmd)
	case "enter":
		if v := m.agents.view; v != nil && v.err != "" {
			// A failed attach dials the same session again; it never starts one.
			if a, ok := l.current(); ok {
				if id, ok := l.runSession(a); ok {
					return m, m.plannerAttach(id, false)
				}
			}
		}
		_, cmd := m.enterTerminalMode()
		return m, cmd
	}
	return m, nil
}

func (m Model) keyIsRefresh(msg tea.KeyMsg) bool { return key.Matches(msg, m.keys.Refresh) }

// --- hints ---

func (m Model) gardenLaunchHints() []shellHint {
	l := m.garden.launch
	refresh := shellHint{m.keys.Refresh.Help().Key, "refresh"}
	switch l.stage {
	case launchStageChoose:
		return []shellHint{{"↑/↓", "choose"}, {"tab", "next field"}, {"ctrl+s", "review (starts nothing)"}, {"ctrl+e", "branch and base"}, {"esc", "back"}}
	case launchStageIsolation:
		return []shellHint{{"tab", "next field"}, {"enter", "apply and review again"}, {"esc", "keep as it was"}}
	case launchStageConfirm:
		h := []shellHint{{"tab", "choose"}, {"enter", "activate"}, {"↑/↓", "scroll"}}
		if m.launchNeedsEscalation() {
			h = append(h, shellHint{"x", "acknowledge permission"})
		}
		if m.launchReviewNeedsEscalation() {
			h = append(h, shellHint{"r", "acknowledge reviewer"})
		} else if l.action == launchActStart && l.preview == nil {
			h = append(h, shellHint{"r", "preview again"})
		}
		if a, ok := l.current(); ok && l.action == launchActRetry && hooksNeedAck(a) {
			h = append(h, shellHint{"y", "acknowledge hooks"})
		}
		if l.action == launchActStart {
			h = append(h, shellHint{"e", "edit branch and base"})
		}
		return append(h, shellHint{"esc", "cancel"})
	case launchStageAttempt:
		a, _ := l.current()
		h := []shellHint{{"o", "open session terminal"}}
		if a.State.Active() {
			h = append(h, shellHint{"x", "stop attempt…"})
		}
		if attemptRetryable(a) {
			h = append(h, shellHint{"r", "retry…"})
		}
		if attemptCleanable(a) {
			h = append(h, shellHint{"w", "cleanup worktree…"})
		}
		if attemptTerminal(a) {
			h = append(h, shellHint{"p", "push branch / Draft PR…"})
		}
		if len(a.Runs) > 1 {
			h = append(h, shellHint{"[/]", "run"})
		}
		return append(h, refresh, shellHint{"esc", "attempts"})
	case launchStageSession:
		if m.plannerTerminalMode() {
			return []shellHint{{"ctrl+]", "unfocus"}}
		}
		return []shellHint{{"enter", "focus terminal"}, refresh, {"esc", "attempt"}}
	}
	h := []shellHint{{"↑/↓", "attempt"}, {"enter", "open attempt"}, {"o", "open session terminal"}}
	if m.launchSupported() {
		h = append(h, shellHint{"s", "start implementation locally…"})
	}
	return append(h, refresh, shellHint{"esc", "back"})
}

// gardenLaunchForward feeds the form the results of its own backend calls, as the planner's account choice gets
// them. The message continues to the rest of the shell: the accounts browser ignores what is not its own.
func (m Model) gardenLaunchForward(msg tea.Msg) (Model, tea.Cmd) {
	l := &m.garden.launch
	if l.dlg == nil || l.stage == launchStageNone {
		return m, nil
	}
	if _, isKey := msg.(tea.KeyMsg); isKey {
		return m, nil
	}
	if _, done := msg.(agentExecDoneMsg); done {
		// A sign-in finished in the real terminal: read the accounts again.
		msg = accounts.ReloadMsg{}
	}
	d, cmd := l.dlg.Update(msg)
	l.dlg = &d
	return m, tea.Batch(cmd, m.launchAdapt())
}

// launchTerminalChanged refreshes the attempt after the terminal attached or ended, which is when its state is
// worth reading again.
func (m Model) launchTerminalChanged() (Model, tea.Cmd) {
	if m.garden.launch.stage != launchStageSession || m.garden.launch.inflight {
		return m, nil
	}
	return m.launchLoad(uint64(m.garden.board.opened))
}

// launchSubscribe opens the shared Sessions.Changes subscription the first time a launch screen shows, so a
// holder that exits while it is watched detached is reconciled. It is one subscription for the whole shell.
func (m *Model) launchSubscribe() tea.Cmd {
	if !m.agents.active() || m.garden.launchSub {
		return nil
	}
	m.garden.launchSub = true
	return subscribeAgentsCmd(m.agents.ctx, m.agents.api.Sessions)
}

func (m Model) onShellAgentsSubscribed(msg agentsSubscribedMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		m.garden.launchSub = false // opened again with the next launch screen
		return m, nil
	}
	return m, waitAgentsCmd(msg.ch)
}

// onShellAgentsChanged re-arms the subscription and reads the open Issue's attempts once, which reconciles them
// with their sessions. It starts nothing.
func (m Model) onShellAgentsChanged(msg agentsChangedMsg) (Model, tea.Cmd) {
	wait := waitAgentsCmd(msg.ch)
	l := m.garden.launch
	_, overview := m.launchOverviewAttempt()
	if !l.inflight && overview && m.gardenOverlayOpen() && m.garden.board.section == 0 {
		m, read := m.launchLoad(uint64(m.garden.board.opened))
		return m, tea.Batch(wait, read)
	}
	switch l.stage {
	case launchStageHub, launchStageAttempt, launchStageSession:
		if !l.inflight && m.garden.board.opened != 0 {
			m, read := m.launchLoad(uint64(m.garden.board.opened))
			return m, tea.Batch(wait, read)
		}
	}
	return m, wait
}

// --- distinct Codex sandbox and approval controls ---

// codexNativeFields replaces the single Permissions preset with the two native controls, so a valid pair no
// preset names (workspace-write with never, say) can be chosen and a stored one is shown exactly.
func codexNativeFields(fields []agents.LaunchField) []agents.LaunchField {
	var out []agents.LaunchField
	for _, f := range fields {
		isPreset := false
		for _, o := range f.Options {
			if len(o.Set) > 0 {
				isPreset = true
			}
		}
		if !isPreset {
			out = append(out, f)
			continue
		}
		out = append(out,
			agents.LaunchField{ID: "sandbox", Label: "Sandbox", Kind: agents.LaunchFieldSelect, Options: []agents.FieldOption{
				{Value: "read-only", Label: "Read only"}, {Value: "workspace-write", Label: "Workspace write"},
				{Value: "danger-full-access", Label: "Full access", Tone: agents.ToneDanger, Help: "Codex can change any file and use the network."},
				{Value: "bypass", Label: "Bypass", Tone: agents.ToneDanger, Help: "Codex runs every command without a sandbox and without asking."}}},
			agents.LaunchField{ID: "approval", Label: "Approval", Kind: agents.LaunchFieldSelect, Options: []agents.FieldOption{
				{Value: "", Label: "Not set"}, {Value: "untrusted", Label: "Untrusted"}, {Value: "on-request", Label: "On request"},
				{Value: "never", Label: "Never", Tone: agents.ToneDanger, Help: "Codex never asks before acting."}}})
	}
	return out
}

type gardenLaunchResolvedMsg struct {
	dlg     uint64
	account agents.AccountID
	res     agents.LaunchResolution
	err     error
}

// launchAdapt gives a loaded Codex form its own sandbox and approval controls. The exact resolved pair seeds
// them when the accounts backend can resolve it; without that, the preset's pair does. It reads no model list.
func (m *Model) launchAdapt() tea.Cmd {
	l := &m.garden.launch
	d := l.dlg
	if d == nil {
		return nil
	}
	if !d.formLoaded {
		l.adapted = ""
		return nil
	}
	a, ok := d.Current()
	if !ok || string(a.Provider) != "codex" || l.adapted == a.ID {
		return nil
	}
	l.adapted = a.ID
	if r, ok := m.agents.api.Accounts.(agentsapi.LaunchResolver); ok {
		id, dlg := a.ID, d.id
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), launchReadTimeout)
			defer cancel()
			res, err := r.ResolveLaunch(ctx, id, agents.LaunchOptions{})
			return gardenLaunchResolvedMsg{dlg: dlg, account: id, res: res, err: err}
		}
	}
	m.applyCodexForm(agents.LaunchResolution{})
	return nil
}

func (m *Model) applyCodexForm(res agents.LaunchResolution) {
	l := &m.garden.launch
	d := *l.dlg
	def := d.Defaults()
	values := map[string]string{}
	for k, v := range def.Values {
		values[k] = v
	}
	if p := values["permissions"]; p != "" {
		for _, f := range def.LaunchFields {
			for _, o := range f.Options {
				if o.Value == p {
					values["sandbox"], values["approval"] = o.Set["sandbox"], o.Set["approval"]
				}
			}
		}
	}
	delete(values, "permissions")
	if res.Launch.Sandbox != "" || res.Launch.Approval != "" {
		values["sandbox"], values["approval"] = res.Launch.Sandbox, res.Launch.Approval
	}
	fields := codexNativeFields(launchDialogFields(def.LaunchFields))
	d.form = launchform.New(fields, def.Models, values, d.ascii).Focus(d.focus == launchFocusForm)
	l.dlg = &d
}

func (m Model) onGardenLaunchResolved(msg gardenLaunchResolvedMsg) (Model, tea.Cmd) {
	l := &m.garden.launch
	d := l.dlg
	if d == nil || d.id != msg.dlg || d.Selected() != msg.account || !d.formLoaded {
		return m, nil
	}
	if msg.err != nil {
		msg.res = agents.LaunchResolution{} // the preset's pair stays; the preview still resolves it again
	}
	m.applyCodexForm(msg.res)
	return m, nil
}
