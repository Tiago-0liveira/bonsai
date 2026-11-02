package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/planrev"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/reviewpass"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/accounts"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

type gardenLaunchReadyState struct {
	gen           uint64
	issue         uint64
	implementer   agents.LaunchResolution
	cached        bool
	reviewer      agents.LaunchResolution
	reviewEnabled bool
	pending       bool
	start         bool
}

type gardenLaunchReadyMsg struct{ state gardenLaunchReadyState }

// Ready reads cached defaults; exact launch resolution belongs to the preview.
func (m *Model) launchReadyLoad() tea.Cmd {
	r := &m.garden.launchReady
	r.gen++
	r.issue, r.pending, r.start = uint64(m.garden.board.opened), true, false
	r.implementer, r.reviewer = agents.LaunchResolution{}, agents.LaunchResolution{}
	r.reviewEnabled = false
	r.cached = false
	state, acc := *r, m.agents.api.Accounts
	prefs := m.work.GardenAutoReview.Clone()
	if m.autoReviewPreserved() || prefs != nil && prefs.Validate() != nil {
		prefs = nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), launchReadTimeout)
		defer cancel()
		if prefs != nil && prefs.Enabled {
			state.reviewEnabled = true
			state.reviewer = agents.LaunchResolution{AccountID: agents.AccountID(prefs.Account), AccountName: prefs.Account, Launch: agents.LaunchOptions{Model: prefs.Model, Effort: prefs.Effort}}
		}
		if acc != nil {
			if list, err := acc.List(ctx); err == nil {
				list = (accounts.Sidebar{Accounts: list}).Ordered()
				if state.reviewEnabled {
					for _, a := range list {
						if a.ID == state.reviewer.AccountID {
							state.reviewer.Provider, state.reviewer.AccountName = a.Provider, a.Name
						}
					}
				}
				if len(list) > 0 {
					a := list[0]
					state.implementer = agents.LaunchResolution{AccountID: a.ID, AccountName: a.Name, Provider: a.Provider}
					if cached, ok := acc.(agentsapi.CachedDefaulter); ok {
						if d, err := cached.CachedDefaults(ctx, a.ID); err == nil {
							state.implementer.Launch = agents.LaunchOptionsFrom(d.LaunchFields, d.Values["model"], d.Values)
							state.cached = true
						}
					}
				}
			}
		}
		if state.reviewEnabled {
			if cached, ok := acc.(agentsapi.CachedDefaulter); ok && state.reviewer.AccountID != "" {
				if d, err := cached.CachedDefaults(ctx, state.reviewer.AccountID); err == nil {
					if state.reviewer.Launch.Model == "" {
						state.reviewer.Launch.Model = d.Values["model"]
					}
					if state.reviewer.Launch.Effort == "" {
						state.reviewer.Launch.Effort = d.Values["effort"]
					}
				}
			}
		}
		state.pending = false
		return gardenLaunchReadyMsg{state: state}
	}
}

func (m Model) onGardenLaunchReady(msg gardenLaunchReadyMsg) (Model, tea.Cmd) {
	r := &m.garden.launchReady
	if msg.state.gen != r.gen || msg.state.issue != uint64(m.garden.board.opened) {
		return m, nil
	}
	start := r.start
	*r = msg.state
	if start && m.workspace == wsGarden && m.gardenOverlayOpen() {
		return m.gardenLaunchDefault()
	}
	return m, nil
}

func (m Model) gardenLaunchDefault() (Model, tea.Cmd) {
	if !m.launchSupported() || !m.launchOffered() {
		return m.gardenLaunchOpen()
	}
	r := &m.garden.launchReady
	if r.pending {
		r.start = true
		return m, nil
	}
	if r.implementer.AccountID == "" {
		m.garden.notice = "The default implementer is unknown. Change run settings (e)."
		return m, nil
	}
	l := &m.garden.launch
	if l.inflight {
		return m, nil
	}
	cols, rows := m.launchDims()
	l.stage, l.action, l.dlg, l.preview, l.stale, l.note = launchStageConfirm, launchActStart, nil, nil, true, "Reading the exact launch preview…"
	l.req.IssueID, l.req.AccountID, l.req.Launch = uint64(m.garden.board.opened), r.implementer.AccountID, agents.LaunchOptions{}
	l.req.Cols, l.req.Rows = cols, rows
	m.shellFocus = shellBody
	m, preview := m.launchPreview()
	return m, tea.Batch(preview, m.launchSubscribe())
}

func launchWho(provider agents.ProviderID, account, effort string) string {
	name := "unknown"
	if provider != "" {
		name = providerName(provider)
	}
	return name + " / " + orDefault(account, "unknown") + " · " + orDefault(effort, "unknown")
}

func (m Model) launchReadyOverview() string {
	r := m.garden.launchReady
	spec, plan := "unknown", "unknown"
	defaultBranch := "new isolated worktree"
	if rec, _, ok := m.garden.board.collection.Find(m.garden.board.opened); ok {
		rv := planrev.FromVerification(rec.Item.Issue, rec.Item.Verification)
		if rv.Gate.Head != nil {
			spec = fmt.Sprintf("s%d", rv.Gate.Ordinal)
		}
		if rv.Approved != nil {
			plan = fmt.Sprintf("v%d", rv.Approved.Ordinal)
			if rv.Approved.Hash != "" {
				defaultBranch = fmt.Sprintf("bonsai/%d-%s", rec.Item.Issue.Number, shortHash(rv.Approved.Hash))
			}
		}
	}
	a := r.implementer
	reviewer := "Off: stops at your review"
	limit := ""
	if r.pending {
		reviewer = "unknown"
	} else if r.reviewEnabled {
		v := r.reviewer
		reviewer = launchWho(v.Provider, v.AccountName, v.Launch.Effort) + " · model " + orDefault(v.Launch.Model, "unknown") +
			"\nReviewer availability is checked in the preview"
		limit = "\nReview limit    One review + fixes + checks"
	}
	permissions := "unknown"
	if r.cached && a.Provider != "" {
		var parts []string
		for _, f := range nativeFromLaunch(a.Provider, a.Launch, nil).facts()[2:] {
			parts = append(parts, f.label+": "+strings.ReplaceAll(f.value, "unresolved", "unknown"))
		}
		permissions = strings.Join(parts, "; ")
	}
	worktree := orDefault(m.garden.launch.req.Branch, defaultBranch)
	if base := m.garden.launch.req.BaseRef; base != "" {
		worktree += " · base " + base
	} else if m.cfg != nil && m.cfg.Upstream != "" {
		worktree += " · base " + m.cfg.Upstream
	}
	// Cached fields are not normalized by the provider (that needs the account), so they are named as saved defaults.
	implementer := launchWho(a.Provider, a.AccountName, a.Launch.Effort)
	if r.cached {
		implementer += " (saved default)"
	}
	text := "✓ READY TO BUILD\n\nSpecification " + spec + " and plan " + plan + " are approved.\n\nImplementer     " + implementer +
		"\nAI reviewer     " + reviewer + limit + "\nWorktree        " + worktree + "\nPermissions     " + permissions
	if !r.cached {
		text += " (not cached yet)"
	}
	for _, a := range m.garden.launch.display() {
		if uint64(a.Issue) == uint64(m.garden.board.opened) && !a.State.Active() {
			text += "\nEarlier attempt: " + string(a.State) + " · Attempts (i)"
			break
		}
	}
	return text
}

func (m Model) launchReviewNeedsEscalation() bool {
	l := m.garden.launch
	return l.action == launchActStart && l.preview != nil && l.preview.Review.Policy.Reviewer.Escalated
}

func (l *gardenLaunchState) keepReview(id string, pass *reviewpass.Pass) {
	if pass == nil || pass.Snapshot.AttemptID != id {
		return
	}
	if prev, ok := l.reviews[id]; ok && prev.Revision >= pass.Revision {
		return
	}
	if l.reviews == nil {
		l.reviews = map[string]reviewpass.Pass{}
	}
	l.reviews[id] = *pass
}

func (m Model) launchOverviewAttempt() (launch.Attempt, bool) {
	var newest launch.Attempt
	for _, a := range m.garden.launch.display() {
		if uint64(a.Issue) == uint64(m.garden.board.opened) {
			if a.State.Active() {
				return a, true
			}
			if newest.ID == "" {
				newest = a
			}
		}
	}
	if !m.launchOffered() && attemptTerminal(newest) {
		return newest, true
	}
	return launch.Attempt{}, false
}

// runClosed is proven closure of the newest run: the holder is known to be gone even when the attempt itself stays
// active (an unacknowledged start that was stopped is recoverable, not stopped).
func runClosed(a launch.Attempt) bool {
	run, ok := newestRun(a)
	if !ok {
		return false
	}
	switch run.State {
	case "exited", "failed", "not-started":
		return true
	}
	return run.EndedAt != nil
}

func attemptStopping(a launch.Attempt) bool {
	return a.StopRequested && a.State.Active() && !runClosed(a)
}

func launchSupervisionHeading(a launch.Attempt) string {
	if attemptStopping(a) {
		return "Stopping… waiting for the holder to close"
	}
	switch a.State {
	case launch.StateRunning:
		return "▶ IMPLEMENTING THE APPROVED PLAN"
	case launch.StatePreparing, launch.StateStarting:
		return "Starting…"
	case launch.StateStartUnknown:
		return "Start outcome unknown: reconciling; nothing starts again until the holder is known"
	case launch.StateStopped:
		return "Stopped"
	case launch.StateFailed:
		return "Failed"
	case launch.StateAwaitingReview:
		return "Awaiting your review"
	default:
		return launchStateText(a)
	}
}

func (m Model) launchNext(a launch.Attempt) string {
	if attemptStopping(a) {
		return "Next: waiting for the holder to close."
	}
	switch a.State {
	case launch.StateStartUnknown:
		return "Next: wait for the holder to be known; nothing starts again."
	case launch.StateRecoverable:
		return "Next: retry (r) once the holder is closed; nothing restarts on its own."
	case launch.StateStopped, launch.StateFailed:
		return "Next: retry (r) or clean up the worktree (w) from the attempt."
	case launch.StateCleaned:
		return "Next: nothing; the worktree was cleaned up."
	}
	if m.garden.launch.reviewUnknown[a.ID] {
		return "Next: automatic review status unknown; your review is still required."
	}
	if pass, ok := m.garden.launch.reviews[a.ID]; ok {
		if a.State == launch.StateAwaitingReview {
			switch pass.State {
			case reviewpass.StatePending, reviewpass.StateStarting, reviewpass.StateStartUnknown, reviewpass.StateRunning, reviewpass.StateChecking:
				return "Next: the automatic review (" + providerName(pass.Snapshot.Policy.Reviewer.Provider) + "), fixes and checks, then your review."
			case reviewpass.StateAwaitingHumanReview:
				return "Next: your review (automatic review done)."
			case reviewpass.StateFailed, reviewpass.StateBlocked, reviewpass.StateStopped:
				return "Next: your review (automatic review " + string(pass.State) + ")."
			default:
				return "Next: automatic review status unknown; your review is still required."
			}
		}
		switch pass.State {
		case reviewpass.StateAwaitingHumanReview, reviewpass.StateFailed, reviewpass.StateBlocked:
			return "Automatic review " + string(pass.State) + "; your review next."
		}
		return "Next: one " + providerName(pass.Snapshot.Policy.Reviewer.Provider) + " review, fixes, checks, then your review."
	}
	if a.State == launch.StateAwaitingReview {
		return "Next: your review."
	}
	return "Next: your review when the implementation ends."
}

// launchShownRun is the run the attempt detail describes: the one picked with [ and ], else the newest. Only the
// attempt being viewed honors the pick, so the overview always describes the newest run.
func (m Model) launchShownRun(a launch.Attempt) (run launch.Run, ok, picked bool) {
	l := m.garden.launch
	if cur, has := l.current(); has && cur.ID == a.ID && l.run >= 0 && l.run < len(a.Runs) {
		return a.Runs[l.run], true, true
	}
	run, ok = newestRun(a)
	return run, ok, false
}

func (m Model) launchSupervision(a launch.Attempt, now time.Time) string {
	who := launchWho(a.Provider, a.AccountName, a.Options.Effort)
	run, haveRun, picked := m.launchShownRun(a)
	if haveRun && run.StartedAt != nil && !run.StartedAt.IsZero() {
		end := now
		if run.EndedAt != nil {
			end = *run.EndedAt
		}
		elapsed := max(int(end.Sub(*run.StartedAt).Seconds()), 0)
		who += fmt.Sprintf(" · %02d:%02d elapsed", elapsed/60, elapsed%60)
	}
	newest, _ := newestRun(a)
	older := picked && run.Number != newest.Number
	if picked {
		who += "\nSelected " + launchRunText(run) + " ([ ] changes it; Open session attaches to it)"
	}
	var activity string
	switch {
	case older:
		activity = fmt.Sprintf("Activity is observed for the newest run (run %d) only", newest.Number)
	case !a.State.Active():
		// The holder is gone: a retained observation would describe a process that no longer runs.
	default:
		activity = m.launchActivityText(a, now)
	}
	text := strings.ReplaceAll(launchSupervisionHeading(a), "▶", m.pgG("▶", ">")) + "\n\n" + who
	if activity != "" {
		text += "\n\n" + activity
	}
	if m.garden.launch.reviewUnknown[a.ID] {
		text += "\nAutomatic review status unknown"
	}
	if pass, ok := m.garden.launch.reviews[a.ID]; ok {
		for _, result := range pass.Checks {
			name := "unknown"
			for _, c := range pass.Snapshot.Policy.Checks {
				if c.ID == result.CheckID {
					name = c.Name
				}
			}
			outcome := "unknown"
			switch result.Outcome {
			case reviewpass.OutcomePassed:
				outcome = "✓ passed"
			case reviewpass.OutcomeFailed:
				outcome = "✗ failed"
			case reviewpass.OutcomeSkipped:
				outcome = "skipped"
			}
			text += "\nCheck: " + name + " · " + outcome
		}
	}
	stage := "unknown"
	if rec, _, ok := m.garden.board.collection.Find(m.garden.board.opened); ok {
		stage = launchStageName(rec.Item.Classification.Stage)
	}
	sync := "sync time unknown"
	if at := m.garden.board.snapshot.SuccessfulAt; !at.IsZero() {
		sync = "synced " + compactAge(now.Sub(at)) + " ago"
	}
	return text + "\n\nGitHub (" + sync + "): " + stage + " · " + launchMirrorText(a)
}

func launchStageName(stage garden.Stage) string {
	if stage == "" {
		return "unknown"
	}
	return strings.ToUpper(string(stage[:1])) + strings.ReplaceAll(string(stage[1:]), "-", " ")
}

func (l *gardenLaunchState) setReviewUnknown(id string, unknown bool) {
	if l.reviewUnknown == nil {
		l.reviewUnknown = map[string]bool{}
	}
	l.reviewUnknown[id] = unknown
}

func (l *gardenLaunchState) staleActivity() {
	if l.activityStale == nil {
		l.activityStale = map[string]bool{}
	}
	for id := range l.activity {
		l.activityStale[id] = true
	}
}

type gardenLaunchOverviewTickMsg struct {
	gen   uint64
	issue uint64
}

func launchOverviewTickCmd(msg gardenLaunchOverviewTickMsg) tea.Cmd {
	return func() tea.Msg { return tea.Tick(5*time.Second, func(time.Time) tea.Msg { return msg })() }
}

func (m Model) launchOverviewWatching() bool {
	a, ok := m.launchOverviewAttempt()
	return m.gardenOverlayOpen() && m.garden.board.section == 0 && ok && a.State.Active()
}

func (m *Model) launchOverviewNext() tea.Cmd {
	l := &m.garden.launch
	if !m.launchOverviewWatching() {
		l.overviewGen++
		l.overviewPending = false
		return nil
	}
	if l.overviewPending {
		return nil
	}
	l.overviewGen++
	l.overviewPending = true
	return launchOverviewTickCmd(gardenLaunchOverviewTickMsg{gen: l.overviewGen, issue: uint64(m.garden.board.opened)})
}

func (m Model) onGardenLaunchOverviewTick(msg gardenLaunchOverviewTickMsg) (Model, tea.Cmd) {
	l := &m.garden.launch
	if msg.gen != l.overviewGen || msg.issue != uint64(m.garden.board.opened) || !l.overviewPending || !m.launchOverviewWatching() {
		return m, nil
	}
	l.overviewPending = false
	if l.inflight {
		cmd := m.launchOverviewNext()
		return m, cmd
	}
	m, cmd := m.launchLoad(msg.issue)
	if cmd == nil {
		return m, nil
	}
	return m, func() tea.Msg {
		result := cmd().(gardenLaunchAttemptsMsg)
		result.overviewGen = msg.gen
		return result
	}
}

type gardenLaunchStopTickMsg struct {
	gen     uint64
	issue   uint64
	attempt string
}

func launchStopTickCmd(msg gardenLaunchStopTickMsg) tea.Cmd {
	return func() tea.Msg { return tea.Tick(time.Second, func(time.Time) tea.Msg { return msg })() }
}

func (m *Model) launchStopNext() tea.Cmd {
	l := &m.garden.launch
	if l.stopAttempt == "" || l.stopPending || m.workspace != wsGarden {
		return nil
	}
	for _, a := range l.attempts {
		if a.ID == l.stopAttempt && (!a.State.Active() || runClosed(a)) {
			l.stopGen++
			l.stopAttempt, l.note, l.stopPending = "", "", false
			return nil
		}
	}
	if l.stopPolls >= 60 {
		l.note, l.stopAttempt = "Stop requested; closure not yet proven", ""
		return nil
	}
	l.stopPending = true
	return launchStopTickCmd(gardenLaunchStopTickMsg{gen: l.stopGen, issue: uint64(m.garden.board.opened), attempt: l.stopAttempt})
}

func (m Model) onGardenLaunchStopTick(msg gardenLaunchStopTickMsg) (Model, tea.Cmd) {
	l := &m.garden.launch
	if msg.gen != l.stopGen || msg.issue != uint64(m.garden.board.opened) || msg.attempt != l.stopAttempt || m.workspace != wsGarden || m.garden.board.opened == 0 {
		return m, nil
	}
	l.stopPending = false
	l.stopPolls++
	m, cmd := m.launchLoad(msg.issue)
	if cmd == nil {
		return m, nil
	}
	return m, func() tea.Msg {
		result := cmd().(gardenLaunchAttemptsMsg)
		result.stopGen = msg.gen
		return result
	}
}

func (m Model) launchSequence(v gardenapi.LaunchPreviewView) string {
	r := v.Review
	if r.Hash != "" {
		reviewer := r.Policy.Reviewer
		var checks []string
		for _, c := range r.Policy.Checks {
			checks = append(checks, c.Name)
		}
		names := "no checks"
		if len(checks) > 0 {
			names = strings.Join(checks, ", ")
		}
		implementer := v.Preview.Account
		return "Confirm authorizes: implementation by " + launchWho(implementer.Provider, implementer.AccountName, implementer.Launch.Effort) + ", then one review by " +
			providerName(reviewer.Provider) + " / " + orDefault(reviewer.AccountName, "unknown") + " · " + orDefault(reviewer.Model, "unknown") + " · " + orDefault(reviewer.Effort, "unknown") +
			" with fixes and checks (" + names + "), then a stop for your review."
	}
	if r.Enabled {
		return "Automatic review unavailable: " + orDefault(r.Reason, "unknown") + ". The implementation stops at your review."
	}
	return "Automatic review is off. The implementation stops at your review."
}

// launchActivityText words the last observation. "Process still running" is only said while the lifecycle itself
// says the holder is alive; a retained or contradicted observation is reported as what it was.
func (m Model) launchActivityText(a launch.Attempt, now time.Time) string {
	obs, ok := m.garden.launch.activity[a.ID]
	if !ok {
		return "Activity unknown"
	}
	stale := m.garden.launch.activityStale[a.ID]
	alive := a.State == launch.StateRunning && !runClosed(a) && !a.StopRequested
	age := ""
	if !obs.ObservedAt.IsZero() {
		age = compactAge(now.Sub(obs.ObservedAt)) + " ago"
	}
	activity, label := "Activity unknown", "observed"
	switch obs.Activity {
	case "working":
		activity = "Working"
	case "idle":
		if alive && !stale {
			activity = "Idle · process still running (idle is not completion)"
		} else {
			if age != "" {
				return "Last observed: idle · " + age
			}
			return "Last observed: idle"
		}
	case "waiting-permission":
		activity = "Waiting for permission in the session"
	}
	if age != "" {
		if stale {
			label = "last observed"
		}
		activity += " · " + label + " " + age
	}
	return activity
}
