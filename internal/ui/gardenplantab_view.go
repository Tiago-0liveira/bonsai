package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/plan"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/planrev"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/work"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// planTabKind is which of the Plan tab's states the Issue is in. It is derived from verified board data, the
// local planner summary and the revise preview; nothing is guessed from terminal output.
type planTabKind int

const (
	tabNone        planTabKind = iota // approved spec, no plan revision yet
	tabUnavailable                    // the specification is not approved (or no longer): no planning
	tabLive                           // a planner attempt is running
	tabPublish                        // an ended attempt left a ready plan file nobody published
	tabAwaiting                       // the current revision awaits a decision
	tabPending                        // changes were requested and no revision attempt answers them
	tabChanges                        // changes were requested; the revise state is not known
	tabApproved                       // the exact pair is approved
	tabStale                          // an approval no longer matches the current plan
)

const (
	// planTabChangeLines caps the "changed since" excerpt; the review screen has the whole comparison.
	planTabChangeLines = 14
	// planTabSectionLines caps each excerpted plan section.
	planTabSectionLines = 5
	planTabHistoryRows  = 8
)

type planTabData struct {
	kind planTabKind
	rec  work.Record
	rv   planrev.Review
	run  *gardenapi.LocalRunSummary
	pv   *gardenapi.PlanRevisePreviewView
	rev  *planrev.Revision // the revision the tab is about: Current, else Latest
}

func (m Model) planTabData() (planTabData, bool) {
	b := m.garden.board
	r, _, ok := b.collection.Find(b.opened)
	if !ok {
		return planTabData{}, false
	}
	d := planTabData{rec: r, rv: planrev.FromVerification(r.Item.Issue, r.Item.Verification), pv: m.planTabPreview()}
	if s, ok := m.planTabRun(b.opened); ok {
		d.run = &s
	}
	switch {
	case d.rv.Current != nil:
		d.rev = d.rv.Current
	case d.rv.Latest != nil:
		d.rev = d.rv.Latest
	}
	cur := d.rv.Current
	switch {
	case d.run != nil && d.run.Live:
		d.kind = tabLive
	case !d.rv.Gate.DownstreamAvailable():
		d.kind = tabUnavailable
	case d.pv != nil && d.pv.Pending != nil:
		d.kind = tabPending
	case d.run != nil && d.run.PlanFile == plan.PlanFileReady:
		d.kind = tabPublish
	case d.rv.Ready:
		d.kind = tabApproved
	case cur != nil && cur.Reviewable() && cur.State == planrev.StateAwaiting:
		d.kind = tabAwaiting
	case d.rv.StaleApproval:
		d.kind = tabStale
	case cur != nil && cur.State == planrev.StateChangesRequested:
		d.kind = tabChanges
	}
	return d, true
}

// planTabRetryableState: a failed or cancelled revision attempt leaves its feedback unanswered, so the explicit
// retry is allowed; starting, running and ended ones already answer it.
func planTabRetryableState(s plan.State) bool {
	return s == plan.StateFailed || s == plan.StateCancelled
}

// planTabAttemptRetryable reports whether the pending decision may be started again: it has no attempt, or only a
// failed one. Anything else already answers it.
func planTabAttemptRetryable(pv *gardenapi.PlanRevisePreviewView) bool {
	return pv != nil && pv.Pending != nil && (pv.Pending.Attempt == nil || planTabRetryableState(pv.Pending.Attempt.State))
}

// --- actions ---

// planTabActions are the Plan tab's actions for its state, in focus order. A disabled one stays, with its reason.
func (m Model) planTabActions(idle bool) []issueControl {
	d, ok := m.planTabData()
	if !ok {
		return nil
	}
	write := m.garden.status.Access >= gardenapi.AccessWrite
	noWrite := "Needs Write access to " + m.garden.status.Target.FullName + "."
	gate := func(c issueControl) issueControl {
		switch {
		case !write && c.enabled:
			c.enabled, c.reason = false, noWrite
		}
		return c
	}
	rev := ""
	if d.rev != nil {
		rev = fmt.Sprintf(" r%d", d.rev.Ordinal)
	}
	review := issueControl{id: "review", key: "r", long: "Open review", short: "Review", enabled: idle}
	request := issueControl{id: "plan", key: "p", long: "Request plan", short: "Plan", enabled: idle}
	open := issueControl{id: "open-planner", key: "p", long: "Open planner", short: "Planner", enabled: idle}
	switch d.kind {
	case tabLive:
		return []issueControl{open, {id: "stop", key: "c", long: "Stop attempt…", short: "Stop…", enabled: idle && d.run.AttemptID != ""}}
	case tabUnavailable:
		request.enabled, request.reason = false, "The specification is not approved, so no plan can be requested."
		out := []issueControl{request}
		if len(d.rv.Revisions) > 0 {
			out = append(out, review)
		}
		return out
	case tabPublish:
		return []issueControl{gate(issueControl{id: "publish", key: "u", long: "Publish plan…", short: "Publish…", enabled: idle}), open}
	case tabAwaiting:
		approve := gate(issueControl{id: "approve", key: "a", long: "Approve plan" + rev + "…", short: "Approve…", enabled: idle && d.rv.CanDecide(garden.DecisionApproved)})
		changes := gate(issueControl{id: "changes", key: "c", long: "Request changes…", short: "Changes…", enabled: idle && d.rv.CanDecide(garden.DecisionChangesRequested)})
		switch {
		case changes.enabled && d.pv == nil:
			changes.enabled, changes.reason = false, "Plan details are still loading."
		case changes.enabled && d.pv.Unavailable != "":
			changes.enabled, changes.reason = false, d.pv.Unavailable
		}
		return []issueControl{approve, changes, review}
	case tabPending:
		retry := gate(issueControl{id: "retry", key: "b", long: "Start revision attempt…", short: "Revise…", enabled: idle && planTabAttemptRetryable(d.pv)})
		switch {
		case retry.enabled && d.pv.Unavailable != "":
			retry.enabled, retry.reason = false, d.pv.Unavailable
		case !planTabAttemptRetryable(d.pv):
			retry.enabled, retry.reason = false, "A revision attempt already answers this feedback."
		}
		out := []issueControl{retry, review}
		if d.pv != nil && d.pv.Pending != nil && d.pv.Pending.Attempt != nil && d.pv.Pending.Attempt.SessionID != "" {
			out = append(out, open)
		}
		return out
	case tabApproved, tabStale, tabChanges:
		return []issueControl{review, request}
	}
	out := []issueControl{request}
	if len(d.rv.Revisions) > 0 {
		out = append(out, review)
	}
	return out
}

// planTabModeControls are the actions of a Plan tab sub-state that replaces the usual bar: the approval
// confirmation, the stop confirmation and the request-changes form. ok is false in the plain tab.
func (m Model) planTabModeControls() ([]issueControl, bool) {
	f := m.planForm()
	idle := m.garden.api != nil && !m.garden.busy.writes()
	switch {
	case m.garden.planrev.fromOverlay && m.garden.planrev.stage == planDecide:
		rev := "plan"
		if r, ok := m.garden.planrev.current(); ok {
			rev = fmt.Sprintf("plan r%d", r.Ordinal)
		}
		return []issueControl{{id: "decide-cancel", key: "esc", long: "Cancel", short: "Cancel", enabled: true},
			{id: "decide-approve", key: "enter", long: "Approve " + rev, short: "Approve", enabled: idle}}, true
	case m.planStopAsked():
		return []issueControl{{id: "stop-keep", key: "n", long: "Keep it running", short: "Keep", enabled: true},
			{id: "stop-yes", key: "y", long: "Stop the attempt", short: "Stop", enabled: idle}}, true
	case f != nil:
		out := []issueControl{{id: "form-back", key: "esc", long: "Back", short: "Back", enabled: true}}
		if f.base.Dirty {
			out = append(out, issueControl{id: "form-dirty", key: "ctrl+y", long: "Read uncommitted changes", short: "Dirty", enabled: idle})
		}
		if f.settings == nil {
			out = append(out, issueControl{id: "form-account", key: "ctrl+g", long: "Choose account…", short: "Account…", enabled: idle})
		}
		send := issueControl{id: "form-send", key: "ctrl+s", long: "Send feedback & revise plan", short: "Send", enabled: idle}
		if f.retry {
			send.long, send.short = "Start revision attempt", "Start"
		}
		return append(out, send), true
	}
	return nil, false
}

// --- rows ---

// ovDoc builds overlay body rows on the panel's surface.
type ovDoc struct {
	w    int
	rows []string
}

func (d *ovDoc) line(fg lipgloss.Color, bold bool, s string) {
	p := theme.Current
	d.rows = append(d.rows, shellRow(d.w, p.Surface, seg(fg, p.Surface, bold).Render(ansi.Truncate(s, d.w, "…")), ""))
}

// wrap adds a paragraph wrapped to the width; "\n" starts a new line. indent prefixes every row.
func (d *ovDoc) wrap(fg lipgloss.Color, bold bool, indent, s string) {
	for _, para := range strings.Split(s, "\n") {
		for _, l := range strings.Split(ansi.Wrap(para, max(d.w-ansi.StringWidth(indent), 1), ""), "\n") {
			d.line(fg, bold, indent+l)
		}
	}
}

func (d *ovDoc) blank() {
	d.rows = append(d.rows, blankRow(d.w, theme.Current.Surface))
}

// gap adds one blank row unless the document is empty or already ends in one.
func (d *ovDoc) gap() {
	if len(d.rows) > 0 && d.rows[len(d.rows)-1] != blankRow(d.w, theme.Current.Surface) {
		d.blank()
	}
}

// excerpt adds at most limit wrapped rows of s, saying how many more there are.
func (d *ovDoc) excerpt(fg lipgloss.Color, indent, s string, limit int) {
	var all []string
	for _, para := range strings.Split(strings.TrimSpace(boardSafe(s)), "\n") {
		for _, l := range strings.Split(ansi.Wrap(para, max(d.w-ansi.StringWidth(indent), 1), ""), "\n") {
			all = append(all, l)
		}
	}
	for i, l := range all {
		if i == limit {
			d.line(theme.Current.Muted, false, indent+fmt.Sprintf("… %d more lines in Open review", len(all)-limit))
			return
		}
		d.line(fg, false, indent+l)
	}
}

// elapsedText is a duration as a person reads it, measured when the view is drawn.
func elapsedText(d time.Duration) string {
	d = max(d, 0).Round(time.Second)
	h, mi, s := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh %02dm", h, mi)
	case mi > 0:
		return fmt.Sprintf("%dm %02ds", mi, s)
	}
	return fmt.Sprintf("%ds", s)
}

// planTabDocument is the body of the Plan tab.
func (m Model) planTabDocument(width int) []string {
	p := theme.Current
	d := &ovDoc{w: width}
	_, bodyRows := m.gardenOverlayBody()
	data, ok := m.planTabData()
	if n := m.garden.notice; n != "" && ok {
		// The reason row under the actions is one line; the whole notice is readable here.
		d.wrap(p.Warning, true, "", boardSafe(n))
		d.blank()
	}
	switch {
	case !ok:
		return nil
	case m.garden.planrev.fromOverlay && m.garden.planrev.stage == planDecide:
		m.planTabApproveRows(d)
	case m.planStopAsked():
		m.planTabStopRows(d, data)
	case m.planForm() != nil:
		m.planFormRows(d, bodyRows)
	default:
		m.planTabStateRows(d, data)
		m.planTabHistoryRows(d, data)
	}
	for _, problem := range data.rec.Item.Problems {
		d.gap()
		d.wrap(p.Warning, false, "", "Attention: "+boardSafe(problem.Message))
	}
	return m.settingsASCII(d.rows)
}

func (m Model) planTabFacts(data planTabData) string {
	gate := data.rv.Gate
	spec := "no specification head"
	if gate.Ordinal > 0 {
		spec = fmt.Sprintf("s%d %s", gate.Ordinal, gate.State)
	}
	return fmt.Sprintf("Spec %s · Latest %s · Current %s · Approved %s", spec, revWord(data.rv.Latest), revWord(data.rv.Current), revWord(data.rv.Approved))
}

func (m Model) planTabStateRows(d *ovDoc, data planTabData) {
	p := theme.Current
	if m.garden.plantab.readErr {
		d.wrap(p.Warning, false, "", "Plan details could not be read just now; showing the last known state. R refreshes.")
		d.blank()
	}
	switch data.kind {
	case tabLive:
		m.planTabLiveRows(d, data)
	case tabUnavailable:
		d.line(p.Warning, true, "PLANNING UNAVAILABLE")
		reason := "The specification is not approved."
		if data.rv.Gate.Reason != "" {
			reason = "The specification is not approved: " + boardSafe(data.rv.Gate.Reason) + "."
		}
		d.wrap(p.Text, false, "", reason)
		if data.rv.Approved != nil {
			d.wrap(p.Warning, false, "", fmt.Sprintf("r%d was approved against an earlier specification; that approval does not carry over.", data.rv.Approved.Ordinal))
		}
		d.wrap(p.Muted, false, "", "A plan can be requested once the exact specification is approved. History below is kept.")
		d.wrap(p.Muted, false, "", m.planTabFacts(data))
	case tabPublish:
		d.line(p.Success, true, "PLAN FILE READY TO PUBLISH")
		who := planTabWho(*data.run)
		d.wrap(p.Text, false, "", who+" ended and left a ready plan file. It is not a plan revision until you publish it.")
		d.wrap(p.Muted, false, "", "Publish plan… shows the exact text and its source specification first; nothing is written until you confirm.")
		d.wrap(p.Muted, false, "", m.planTabFacts(data))
	case tabAwaiting:
		m.planTabAwaitingRows(d, data)
	case tabPending, tabChanges:
		m.planTabPendingRows(d, data)
	case tabApproved:
		d.line(p.Success, true, fmt.Sprintf("APPROVED r%d against s%d", data.rv.Approved.Ordinal, data.rv.Gate.Ordinal))
		d.wrap(p.Text, false, "", "Ready; coding starts only when you choose.")
		d.wrap(p.Muted, false, "", m.planTabFacts(data))
		d.wrap(p.Muted, false, "", m.launchPlansLine())
	case tabStale:
		m.planTabStaleRows(d, data)
	default:
		d.line(p.Text, true, "NO PLAN YET")
		d.wrap(p.Text, false, "", "No plan revision has been published for this Issue.")
		d.wrap(p.Muted, false, "", "Request plan starts a local read-only planner after its own confirmation. Reading this tab starts nothing.")
		d.wrap(p.Muted, false, "", m.planTabFacts(data))
		if data.rev != nil && data.rev.State == planrev.StateRejected {
			d.wrap(p.Warning, false, "", fmt.Sprintf("r%d was rejected; no plan is accepted.", data.rev.Ordinal))
		}
	}
}

func planTabWho(s gardenapi.LocalRunSummary) string {
	who := strings.TrimSpace(boardTitle(s.Provider) + " / " + boardTitle(s.AccountName))
	if strings.Trim(who, " /") == "" {
		return "The planner"
	}
	return who
}

// planTabLiveRows shows the running planner from what it reported. Nothing is inferred: no checklist, no progress,
// and idle is never "finished".
func (m Model) planTabLiveRows(d *ovDoc, data planTabData) {
	p := theme.Current
	s := *data.run
	key, label := agentactivity.Display("running", s.Activity)
	title, fg := "PLANNER RUNNING", p.Warning
	switch key {
	case "waiting":
		title, fg = "PLANNER NEEDS YOU", p.Warning
	case "idle":
		title, fg = "PLANNER IDLE (NOT FINISHED)", p.Muted
	case "stalled":
		title = "PLANNER STALLED"
	case "working":
		title = "PLANNER WORKING"
	}
	d.line(fg, true, title)
	settings := planTabWho(s)
	if s.Effort != "" {
		settings += " · " + boardTitle(s.Effort)
	}
	if s.Model != "" {
		settings += " · " + boardTitle(s.Model)
	}
	d.wrap(p.Text, false, "", settings)
	now := agentNow()
	if s.RequestedAt.IsZero() {
		d.line(p.Text, false, "Elapsed: not reported")
	} else {
		d.line(p.Text, false, fmt.Sprintf("Elapsed: %s (requested %s)", elapsedText(now.Sub(s.RequestedAt)), s.RequestedAt.Local().Format("15:04")))
	}
	if s.Activity == nil {
		d.line(p.Text, false, "Activity: not reported")
	} else {
		act := label
		if det := strings.TrimSpace(boardTitle(s.Activity.Detail)); det != "" {
			act += " · " + det
		}
		if !s.Activity.Since.IsZero() {
			act += " · " + elapsedText(now.Sub(s.Activity.Since)) + " ago"
		}
		d.wrap(p.Text, false, "", "Activity: "+act)
	}
	d.line(p.Text, false, "Plan file: "+planFileText(s.PlanFile))
	d.line(p.Text, false, "Access: read-only, no worktree")
	if s.Revises != nil {
		d.wrap(p.Text, false, "", fmt.Sprintf("Revises plan r%d: your recorded feedback is part of this attempt.", s.Revises.Ordinal))
	}
	switch key {
	case "waiting":
		d.gap()
		d.wrap(p.Warning, false, "", "The planner is waiting for you. Open the planner to answer it.")
	case "idle":
		d.gap()
		d.wrap(p.Muted, false, "", "Idle means the planner is waiting for a prompt. It is not a finished plan; a plan counts only when its plan file is ready.")
	}
	if m.garden.local.stale {
		d.wrap(p.Warning, false, "", "The last read failed; this is the last known state.")
	}
	d.gap()
	d.wrap(p.Muted, false, "", "Leave this overlay and planning keeps running.")
}

func (m Model) planTabAwaitingRows(d *ovDoc, data planTabData) {
	p := theme.Current
	cur := data.rv.Current
	d.line(p.Warning, true, fmt.Sprintf("PLAN r%d / NEEDS YOUR APPROVAL", cur.Ordinal))
	d.wrap(p.Muted, false, "", m.planTabFacts(data))
	if data.rv.StaleApproval && data.rv.Approved != nil {
		d.wrap(p.Warning, false, "", fmt.Sprintf("The earlier approval of r%d does not carry over to this plan. Approve it only if you mean to.", data.rv.Approved.Ordinal))
	}
	secs := planrev.Parse(cur.Content)
	find := func(title string) (planrev.Section, bool) {
		for _, s := range secs {
			if strings.EqualFold(s.Title, title) {
				return s, true
			}
		}
		return planrev.Section{}, false
	}
	d.gap()
	if s, ok := find("Summary"); ok {
		d.line(p.Text, true, "SUMMARY")
		d.excerpt(p.Text, "", s.Text, planTabSectionLines)
	} else if s, ok := find("Steps"); ok {
		d.line(p.Text, true, "STEPS (FIRST LINES)")
		d.excerpt(p.Text, "", s.Text, planTabSectionLines)
	} else {
		d.line(p.Text, true, "SUMMARY")
		d.line(p.Muted, false, "This revision has no summary or steps.")
	}
	d.gap()
	if cur.Ordinal > 1 && cur.Ordinal-2 < len(data.rv.Revisions) {
		prev := data.rv.Revisions[cur.Ordinal-2]
		diff := planrev.Diff(prev.Content, cur.Content)
		added, removed := planrev.Changed(diff)
		d.line(p.Text, true, fmt.Sprintf("CHANGED SINCE r%d: %d added, %d removed", prev.Ordinal, added, removed))
		shown := 0
		for _, l := range planrev.Collapse(diff, 1) {
			if shown >= planTabChangeLines {
				d.line(p.Muted, false, "… more changes in Open review")
				break
			}
			if l.Skipped > 0 {
				d.line(p.Muted, false, fmt.Sprintf("  … %d unchanged %s", l.Skipped, lineWord(l.Skipped)))
				shown++
				continue
			}
			fg, mark := p.Muted, "  "
			switch l.Op {
			case planrev.OpAdd:
				fg, mark = p.Success, "+ "
			case planrev.OpDel:
				fg, mark = p.Danger, "- "
			}
			d.line(fg, false, mark+strings.TrimRight(boardSafe(l.Text), " "))
			shown++
		}
	} else {
		d.line(p.Text, true, "FIRST REVISION")
		d.line(p.Muted, false, "Nothing to compare it with.")
	}
	for _, title := range []string{"Risks", "Tests"} {
		if s, ok := find(title); ok {
			d.gap()
			d.line(p.Text, true, strings.ToUpper(title))
			d.excerpt(p.Text, "", s.Text, planTabSectionLines)
		}
	}
	d.gap()
	d.wrap(p.Muted, false, "", fmt.Sprintf("Approve plan r%d… asks you to confirm this exact plan and specification. Coding starts only when you choose.", cur.Ordinal))
}

func (m Model) planTabPendingRows(d *ovDoc, data planTabData) {
	p := theme.Current
	ord := 0
	switch {
	case data.pv != nil && data.pv.Revision.Ordinal > 0:
		ord = data.pv.Revision.Ordinal
	case data.rv.Current != nil:
		ord = data.rv.Current.Ordinal
	}
	d.line(p.Warning, true, fmt.Sprintf("CHANGES REQUESTED ON r%d", ord))
	reason := ""
	var attempt *plan.Attempt
	if data.pv != nil && data.pv.Pending != nil {
		reason, attempt = data.pv.Pending.Reason, data.pv.Pending.Attempt
	} else if data.rv.Current != nil {
		reason = data.rv.Current.Reason()
	}
	switch {
	case attempt == nil && data.kind == tabPending:
		d.wrap(p.Text, false, "", fmt.Sprintf("Feedback recorded on r%d. No revision attempt is running.", ord))
	case attempt != nil && planTabRetryableState(attempt.State):
		d.wrap(p.Text, false, "", fmt.Sprintf("Feedback recorded on r%d. The revision attempt did not finish (%s). No revision attempt is running.", ord, boardTitle(attemptStateText(*attempt))))
	case attempt != nil:
		d.wrap(p.Text, false, "", fmt.Sprintf("Feedback recorded on r%d. A revision attempt exists for it (%s); it cannot be started again.", ord, attempt.State))
	default:
		d.wrap(p.Text, false, "", fmt.Sprintf("Changes were requested on r%d. Plan details are still loading.", ord))
	}
	if t := m.garden.plantab; t.startErr != "" && data.pv != nil && data.pv.Pending != nil && t.startFor == data.pv.Pending.DecisionCommentID {
		d.wrap(p.Warning, false, "", "The start failed: "+boardSafe(t.startErr))
	}
	if reason != "" {
		d.gap()
		d.line(p.Text, true, "YOUR FEEDBACK")
		d.excerpt(p.Muted, "  ", reason, planTabSectionLines+1)
	}
	d.gap()
	d.wrap(p.Muted, false, "", m.planTabFacts(data))
	if data.kind == tabPending && planTabAttemptRetryable(data.pv) {
		d.wrap(p.Muted, false, "", m.planTabReviseWith(data.pv))
		d.wrap(p.Muted, false, "", "Start revision attempt… asks for one more confirmation. Reading this tab never starts it.")
	}
}

// planTabReviseWith names the settings a revision attempt would use.
func (m Model) planTabReviseWith(pv *gardenapi.PlanRevisePreviewView) string {
	if pv.Settings == nil {
		text := "Revise with: choose an account when you start."
		if pv.SettingsUnavailable != "" {
			text += " " + boardSafe(pv.SettingsUnavailable)
		}
		return text
	}
	return "Revise with " + m.planTabSettingsText(*pv.Settings)
}

func (m Model) planTabSettingsText(s gardenapi.PlanReviseSettings) string {
	name := string(s.AccountID)
	if n, ok := m.garden.plantab.accountNames[s.AccountID]; ok {
		name = n
	}
	text := boardTitle(name)
	if s.Effort != "" {
		text += " · " + boardTitle(s.Effort)
	}
	if s.Model != "" {
		text += " · " + boardTitle(s.Model)
	}
	return text
}

func (m Model) planTabStaleRows(d *ovDoc, data planTabData) {
	p := theme.Current
	d.line(p.Warning, true, "APPROVAL IS STALE")
	switch {
	case data.rv.Approved != nil && data.rv.Current != nil && data.rv.Approved.Hash != data.rv.Current.Hash:
		d.wrap(p.Text, false, "", fmt.Sprintf("r%d was approved, but the plan under review is now r%d. The approval does not carry over.", data.rv.Approved.Ordinal, data.rv.Current.Ordinal))
	case data.rv.Approved != nil:
		d.wrap(p.Text, false, "", fmt.Sprintf("r%d was approved against an earlier specification (s%d is current). The approval does not carry over.", data.rv.Approved.Ordinal, data.rv.Gate.Ordinal))
	default:
		d.wrap(p.Text, false, "", "The earlier approval no longer matches the current plan and specification.")
	}
	d.wrap(p.Muted, false, "", "The approved snapshot stays in the history. Open review to decide again, or request a new plan. Nothing is approved again automatically.")
	d.wrap(p.Muted, false, "", m.planTabFacts(data))
}

func (m Model) planTabHistoryRows(d *ovDoc, data planTabData) {
	p := theme.Current
	revs := data.rv.Revisions
	if len(revs) == 0 {
		return
	}
	d.gap()
	d.line(p.Muted, true, "REVISIONS")
	for i, shown := len(revs)-1, 0; i >= 0; i-- {
		r := revs[i]
		if shown == planTabHistoryRows {
			d.line(p.Muted, false, fmt.Sprintf("… %d older in Open review", i+1))
			break
		}
		shown++
		tag := stateWords(r.State)
		switch {
		case r.Stale:
			tag += ", older specification"
		case r.Superseded:
			tag += ", superseded"
		}
		d.line(stateColor(r.State), false, fmt.Sprintf("r%d  %s  %s", r.Ordinal, tag, shortHash(r.Hash)))
	}
}

func (m Model) planTabStopRows(d *ovDoc, data planTabData) {
	p := theme.Current
	d.line(p.Warning, true, "STOP THE PLANNER ATTEMPT?")
	if data.run != nil {
		d.wrap(p.Text, false, "", planTabWho(*data.run)+" is asked to end. Whatever it wrote stays in its session; the attempt stays in the history.")
	}
	d.wrap(p.Muted, false, "", "y stops it. Any other key keeps it running.")
}

func (m Model) planTabApproveRows(d *ovDoc) {
	p := theme.Current
	st := m.garden.planrev
	r, ok := st.current()
	if !ok || st.view == nil || st.view.Review.Gate.Head == nil {
		d.line(p.Muted, false, "Nothing to decide.")
		return
	}
	d.line(p.Warning, true, fmt.Sprintf("APPROVE PLAN r%d?", r.Ordinal))
	d.gap()
	d.wrap(p.Text, false, "", fmt.Sprintf("Plan revision: %s\nSource spec fingerprint: %s\nSpecification head: %s s%d\nActor: %s (GitHub ID %s)",
		r.Hash, st.view.Review.Gate.Fingerprint, st.view.Review.Gate.Head.Hash, st.view.Review.Gate.Ordinal, boardTitle(st.view.Actor), st.view.ActorID))
	d.gap()
	d.wrap(p.Muted, false, "", "Records approval of this exact pair → Ready. No agent, worktree or PR starts. Both heads and your authority are verified again before writing. "+m.launchApprovalNote())
	if m.garden.busy == busyPlanDecide {
		d.gap()
		d.line(p.Warning, true, "Recording… keys are ignored until GitHub answers.")
	}
}

// planFormRows draws the request-changes form, or the confirmation of a revision attempt for recorded feedback.
func (m Model) planFormRows(d *ovDoc, bodyRows int) {
	p := theme.Current
	f := m.planForm()
	if f.retry {
		d.line(p.Warning, true, fmt.Sprintf("START A REVISION ATTEMPT FOR r%d", f.ordinal))
		d.line(p.Muted, false, "The feedback is already recorded on the Issue.")
		d.gap()
		d.excerpt(p.Text, "  ", f.reason, planTabSectionLines+1)
	} else {
		d.line(p.Warning, true, fmt.Sprintf("Request changes to plan r%d", f.ordinal))
		d.line(p.Muted, false, "Feedback stays attached to this exact plan revision.")
		d.gap()
		ta := f.ta
		w, rows := m.gardenOverlayBody()
		ta.SetWidth(max(w-2, 4))
		ta.SetHeight(max(2, min(rows-planFormFixedRows(f), 8)))
		for _, l := range strings.Split(ta.View(), "\n") {
			d.rows = append(d.rows, shellRow(d.w, p.Surface, seg(p.Text, p.Surface, false).Render(" ")+l, ""))
		}
	}
	d.gap()
	if f.settings != nil {
		d.wrap(p.Text, false, "", "Revise with "+m.planTabSettingsText(*f.settings))
	} else {
		text := "Revise with: no account chosen. Choose account… (ctrl+g)."
		if f.unavailable != "" {
			text += " " + boardSafe(f.unavailable)
		}
		d.wrap(p.Warning, false, "", text)
	}
	d.line(p.Text, false, "Access: read-only, no worktree")
	if f.base.Dirty {
		box := "[ ]"
		if f.includeDirty {
			box = "[x]"
		}
		d.line(p.Text, false, box+" Let the planner read uncommitted changes in the checkout (ctrl+y)")
	}
	d.wrap(p.Muted, false, "", fmt.Sprintf("The new attempt receives this feedback and plan r%d.", f.ordinal))
	if m.garden.busy == busyPlanRevise {
		d.line(p.Warning, true, "Sending… keys are ignored until it answers.")
	} else {
		d.wrap(p.Muted, false, "", "Starts one read-only revision attempt. Nothing starts coding.")
	}
}

// planTabRail adds the Plan tab's facts to the wide summary rail.
func (m Model) planTabRail(add func(fg lipgloss.Color, bold bool, s string)) {
	p := theme.Current
	data, ok := m.planTabData()
	if !ok {
		return
	}
	add(p.Muted, true, "PLAN")
	spec := "none"
	if data.rv.Gate.Ordinal > 0 {
		spec = fmt.Sprintf("s%d %s", data.rv.Gate.Ordinal, data.rv.Gate.State)
	}
	add(p.Text, false, "Spec "+spec)
	add(p.Text, false, "Latest "+revWord(data.rv.Latest))
	add(p.Text, false, "Current "+revWord(data.rv.Current))
	add(p.Text, false, "Approved "+revWord(data.rv.Approved))
	var chips []string
	for _, r := range data.rv.Revisions {
		chip := fmt.Sprintf("r%d", r.Ordinal)
		if data.rv.Current != nil && data.rv.Current.Hash == r.Hash {
			chip = "[" + chip + "]"
		}
		chips = append(chips, chip)
	}
	if len(chips) > 0 {
		add(p.Muted, false, strings.Join(chips, " "))
	}
}
