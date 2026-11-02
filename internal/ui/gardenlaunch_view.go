package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

const launchFactLabel = 16

// gsLaunch draws the launch screens of the opened Issue.
func (m Model) gsLaunch(width, rows int) []string {
	switch m.garden.launch.stage {
	case launchStageChoose:
		return m.gsLaunchChoose(width, rows)
	case launchStageIsolation:
		return m.gsLaunchIsolation(width, rows)
	case launchStageConfirm:
		return m.gsLaunchConfirm(width, rows)
	case launchStageAttempt:
		return m.gsLaunchAttempt(width, rows)
	case launchStageSession:
		return m.gsLaunchSession(width, rows)
	}
	return m.gsLaunchHub(width, rows)
}

func (m Model) gardenLaunchFocusLabel() string {
	switch {
	case m.plannerTerminalMode():
		return "Implementation / Terminal (focused)"
	}
	switch m.garden.launch.stage {
	case launchStageChoose:
		return "Implementation / Account"
	case launchStageIsolation:
		return "Implementation / Branch and base"
	case launchStageConfirm:
		return "Implementation / Confirmation"
	case launchStageAttempt:
		return "Implementation / Attempt"
	case launchStageSession:
		return "Implementation / Session"
	}
	return "Implementation / Attempts"
}

func (m Model) gardenLaunchCrumb() string { return "Implementation" }

// launchIssueNumber is the number of the opened Issue.
func (m Model) launchIssueNumber() int64 {
	if r, _, ok := m.garden.board.collection.Find(m.garden.board.opened); ok {
		return r.Item.Issue.Number
	}
	return 0
}

// --- facts ---

type launchFact struct {
	label string
	value string
	fg    lipgloss.Color
	bold  bool
}

func plainFact(label, value string) launchFact {
	return launchFact{label: label, value: value, fg: theme.Current.Muted}
}

// launchFactLines draws facts with the value wrapped under itself, so a long path or hash is never cut: at a
// narrow width an identity stays readable, in full, by scrolling.
func launchFactLines(width int, facts []launchFact) []string {
	p := theme.Current
	var out []string
	room := max(width-launchFactLabel-2, 8)
	for _, f := range facts {
		fg := f.fg
		if fg == "" {
			fg = p.Muted
		}
		lines := strings.Split(ansi.Wrap(boardSafe(f.value), room, ""), "\n")
		for i, l := range lines {
			label := ""
			if i == 0 {
				label = f.label
			}
			out = append(out, gRuns(width, p.Background, grun{padRight(label, launchFactLabel), p.Text, false}, grun{l, fg, f.bold}))
		}
	}
	return out
}

// launchScroll windows scrolling lines into the rows a block was given.
func launchScroll(lines []string, scroll int) func(avail int) []string {
	return func(avail int) []string {
		avail = max(avail, 0)
		start := max(0, min(scroll, max(len(lines)-avail, 0)))
		return lines[start:min(start+avail, len(lines))]
	}
}

// nativeOptions are the effective model, effort and native permission values, whichever provider they belong to.
type nativeOptions struct {
	provider       agents.ProviderID
	model, effort  string
	permissionMode string
	sandbox        string
	approval       string
	fullAccess     *bool
	inherited      map[string]bool
}

func nativeFromLaunch(provider agents.ProviderID, o agents.LaunchOptions, inherited map[string]bool) nativeOptions {
	return nativeOptions{provider: provider, model: o.Model, effort: o.Effort, permissionMode: o.PermissionMode, sandbox: o.Sandbox,
		approval: o.Approval, fullAccess: o.FullAccess, inherited: inherited}
}

func nativeFromAttempt(a launch.Attempt) nativeOptions {
	o := a.Options
	return nativeOptions{provider: a.Provider, model: o.Model, effort: o.Effort, permissionMode: o.PermissionMode, sandbox: o.Sandbox,
		approval: o.Approval, fullAccess: o.FullAccess}
}

func (n nativeOptions) source(key, value string) string {
	switch {
	case value == "":
		return ""
	case n.inherited[key]:
		return " (from profile)"
	case n.inherited != nil:
		return " (chosen here)"
	}
	return ""
}

// facts lists the values the holder runs with as distinct fields: the Codex sandbox and approval, the Claude
// permission mode and the Antigravity full access are never flattened into one flag.
func (n nativeOptions) facts() []launchFact {
	p := theme.Current
	value := func(label, key, v string) launchFact {
		if v == "" {
			return launchFact{label: label, value: "unresolved", fg: p.Danger, bold: true}
		}
		return launchFact{label: label, value: v + n.source(key, v), fg: p.Text}
	}
	model := launchFact{label: "Model", value: "Provider default (not pinned)", fg: p.Muted}
	if n.model != "" {
		model = launchFact{label: "Model", value: n.model + n.source("model", n.model), fg: p.Text}
	}
	effort := launchFact{label: "Effort", value: "Provider default (not pinned)", fg: p.Muted}
	if n.effort != "" {
		effort = launchFact{label: "Effort", value: n.effort + n.source("effort", n.effort), fg: p.Text}
	}
	out := []launchFact{model, effort}
	switch string(n.provider) {
	case "claude":
		out = append(out, value("Permission mode", "permission_mode", n.permissionMode))
	case "codex":
		out = append(out, value("Sandbox", "sandbox", n.sandbox))
		if n.sandbox == "bypass" {
			out = append(out, launchFact{label: "Approval", value: "approval bypassed", fg: p.Warning})
		} else {
			out = append(out, value("Approval", "approval", n.approval))
		}
	case "antigravity":
		switch {
		case n.fullAccess == nil:
			out = append(out, launchFact{label: "Full access", value: "unresolved", fg: p.Danger, bold: true})
		case *n.fullAccess:
			out = append(out, launchFact{label: "Full access", value: "yes" + n.source("full_access", "yes"), fg: p.Warning})
		default:
			out = append(out, launchFact{label: "Full access", value: "no" + n.source("full_access", "no"), fg: p.Text})
		}
	default:
		out = append(out, plainFact("Native options", "not known to this version"))
	}
	return out
}

// inputFacts are the exact approved inputs a launch is bound to.
func launchInputFacts(in launch.Inputs) []launchFact {
	return []launchFact{
		plainFact("Specification", fmt.Sprintf("s%d / revision %s / comment %d / approval %d", in.Spec.Ordinal, in.Spec.RevisionHash, in.Spec.CommentID, in.SpecApprovalID)),
		plainFact("Spec fingerprint", in.Spec.Fingerprint),
		plainFact("Plan", fmt.Sprintf("r%d / revision %s / comment %d / decision %d", in.Plan.Ordinal, in.Plan.RevisionHash, in.Plan.CommentID, in.Plan.DecisionID)),
		plainFact("Plan base", in.Plan.BaseCommit),
	}
}

func (m Model) startFacts(v gardenapi.LaunchPreviewView) []launchFact {
	p := theme.Current
	pv := v.Preview
	prop := pv.Proposal
	acct := pv.Account
	facts := []launchFact{
		{label: "Issue", value: fmt.Sprintf("#%d %s", pv.Remote.Issue.Number, boardTitle(pv.Remote.Issue.Title)), fg: p.Text, bold: true},
		{label: "Branch", value: prop.Branch + " (a new branch Bonsai creates)", fg: p.Text},
		{label: "Worktree", value: prop.Path + " (created for this attempt)", fg: p.Text},
		{label: "Base", value: prop.BaseRef + " @ " + prop.BaseSHA, fg: p.Text},
		plainFact("Remote", prop.RemoteName+" / "+prop.Repository),
		{label: "Account", value: providerName(acct.Provider) + " / " + boardTitle(acct.AccountName), fg: p.Text},
	}
	facts = append(facts, nativeFromLaunch(acct.Provider, acct.Launch, acct.Inherited).facts()...)
	facts = append(facts,
		plainFact("Terminal", fmt.Sprintf("%d x %d, frozen with this confirmation", pv.Request.Cols, pv.Request.Rows)),
		plainFact("Repository", fmt.Sprintf("%s / Garden ID %d", pv.Remote.FullName, pv.Request.Target.Repo)),
		plainFact("Approved by", fmt.Sprintf("%s (GitHub ID %s)", boardTitle(pv.Request.Target.Login), pv.Request.Target.Viewer)),
	)
	facts = append(facts, launchInputFacts(pv.Inputs)...)
	facts = append(facts,
		plainFact("Hooks", "the configured lifecycle hooks run inside the daemon (config digest "+shortHash(prop.ConfigHash)+"…)"),
		plainFact("Source checkout", "Uncommitted changes in your checkout are excluded: the worktree starts from the committed base."),
	)
	if n := len(pv.Attempts); n > 0 {
		facts = append(facts, plainFact("Earlier attempts", fmt.Sprintf("%d retained for this Issue; none is running", n)))
	}
	return facts
}

// verbatim is the exact approved text a launch gives the agent, inspectable before confirming.
func verbatim(width int, title, text string) []string {
	p := theme.Current
	out := []string{gText(width, p.Accent, true, title)}
	return append(out, gWrap(width, p.Text, false, boardSafe(text))...)
}

// --- the form ---

func (m Model) gsLaunchChoose(width, rows int) []string {
	p := theme.Current
	d := m.garden.launch.dlg
	if d == nil {
		return gCompose(width, rows, []gblock{{lines: []string{gText(width, p.Muted, false, "Nothing to choose.")}}}, nil)
	}
	d2 := *d
	d2.SetSize(width, rows)
	// The shared dialog says "session"; here the primary action only asks for the review, and defaults are not
	// edited in this flow.
	text := strings.NewReplacer("New session", "Start implementation locally", "Start session", "Review launch",
		"Ctrl+S Start", "Ctrl+S Review", "^S Start", "^S Review").Replace(d2.View())
	lines := strings.Split(text, "\n")
	out := make([]string, 0, rows)
	for _, l := range lines {
		if strings.Contains(ansi.Strip(l), "Save as account defaults") {
			l = "  " + lipgloss.NewStyle().Foreground(p.Dim).Render("Defaults are edited in Accounts. Nothing here changes them.")
		}
		out = append(out, shellRow(width, p.Background, ansi.Truncate(l, width, ""), ""))
	}
	for len(out) < rows {
		out = append(out, blankRow(width, p.Background))
	}
	return m.settingsASCII(out[:rows])
}

func (m Model) gsLaunchIsolation(width, rows int) []string {
	p := theme.Current
	l := m.garden.launch
	labels := [3]string{"Branch", "Base ref", "Remote"}
	var lines []string
	for i := range l.iso {
		in := l.iso[i]
		in.Width = max(width-launchFactLabel-6, 10)
		mark := "  "
		if i == l.isoFocus {
			mark = "> "
		}
		lines = append(lines, shellRow(width, p.Background, ansi.Truncate(mark+padRight(labels[i], launchFactLabel-2)+in.View(), width, ""), ""))
	}
	blocks := []gblock{
		{lines: []string{gHeadChip(width, "◇ BRANCH AND BASE", p.Accent)}},
		{lines: lines},
		{prio: 1, lines: gWrap(width, p.Muted, false, "The base ref must still resolve to the approved base commit; moving the planned base needs a new plan and approval. A remote is only taken when it points at this Garden's repository. Applying reads a fresh preview and asks again.")},
	}
	return m.settingsASCII(gCompose(width, rows, blocks, []string{gText(width, p.Muted, false, "Enter applies · Tab next field · Esc keeps what it was")}))
}

// --- confirmation ---

func (m Model) gsLaunchConfirm(width, rows int) []string {
	p := theme.Current
	l := m.garden.launch
	head, consequence, facts, blockers := m.confirmContent(width)
	blocks := []gblock{{lines: []string{head}}}
	var content []string
	if l.note != "" {
		content = append(content, gWrap(width, p.Warning, true, l.note)...)
	}
	content = append(content, blockers...)
	if len(content) > 0 {
		content = append(content, blankRow(width, p.Background))
	}
	content = append(content, gText(width, p.Text, true, "Review the consequence"))
	content = append(content, gWrap(width, p.Text, false, consequence)...)
	content = append(content, blankRow(width, p.Background))
	content = append(content, facts...)
	scroll := min(l.scroll, max(len(content)-3, 0))
	blocks = append(blocks, launchFactsBlock(content, scroll))
	label := m.confirmLabel()
	confirm := gButton(label, l.btn == 1)
	if !m.launchConfirmable() {
		confirm = gButton(label+" (unavailable)", false)
	}
	actions := []string{gButtons(width, "  ", gButton("Cancel", l.btn == 0), confirm)}
	switch {
	case l.inflight:
		actions = append(actions, gText(width, p.Warning, true, "Working… keys are ignored until it answers. Leaving this screen does not stop it."))
	case l.stale:
		actions = append(actions, gText(width, p.Warning, true, orDefault(l.note, "The terminal size changed: reading the confirmation again…")))
	default:
		actions = append(actions, gText(width, p.Muted, false, "Cancel is focused first. Tab moves to Confirm; Enter on Cancel changes nothing."))
	}
	return m.settingsASCII(gCompose(width, rows, blocksWithNotices(m, width, blocks...), actions))
}

func (m Model) confirmLabel() string {
	switch m.garden.launch.action {
	case launchActRetry:
		return "Confirm: retry"
	case launchActStop:
		return "Confirm: stop attempt"
	case launchActCleanup:
		return "Confirm: remove worktree"
	case launchActPublish:
		return "Confirm: push and open Draft PR"
	}
	return "Confirm: start locally"
}

// confirmContent is the title, the consequence paragraphs, the facts and the acknowledgments of the confirmation
// in view. The facts are the frozen preview or the stored attempt, never something re-derived while it is open.
func (m Model) confirmContent(width int) (head string, consequence string, facts []string, blockers []string) {
	p := theme.Current
	l := m.garden.launch
	a, haveAttempt := l.current()
	switch l.action {
	case launchActStart:
		v := l.preview
		if v == nil {
			return gHeadChip(width, "Confirm the proposed change", p.Warning), "Nothing to confirm.", nil, nil
		}
		pv := v.Preview
		approved := ""
		if pv.Remote.Review.Approved != nil {
			approved = fmt.Sprintf("plan r%d / ", pv.Remote.Review.Approved.Ordinal)
		}
		chip := fmt.Sprintf("%sspec s%d / verified now", approved, pv.Inputs.Spec.Ordinal)
		head = gHead(width, fmt.Sprintf("◇ Confirm the proposed change / #%d", pv.Remote.Issue.Number), chip)
		consequence = "Create an isolated worktree and start the selected coding agent on this machine.\n" +
			"This starts a local holder. Leaving Bonsai detaches. Pushing and opening a Draft PR require a separate action."
		facts = launchFactLines(width, m.startFacts(*v))
		blockers = append(blockers, gText(width, p.Text, true, "Sequence"))
		blockers = append(blockers, gWrap(width, p.Text, false, m.launchSequence(*v))...)
		if g := pv.Remote.Review.Gate.Head; g != nil {
			facts = append(facts, verbatim(width, "SPECIFICATION (exact approved text)", g.Content)...)
		}
		if ap := pv.Remote.Review.Approved; ap != nil {
			facts = append(facts, verbatim(width, "PLAN (exact approved text)", ap.Content)...)
		}
		if b := launchPermissionBlocker(pv.Account); b != "" {
			blockers = append(blockers, gWrap(width, p.Danger, true, "! "+b+" Confirm stays off until it is resolved in the form.")...)
		}
		if !pv.Remote.Live {
			blockers = append(blockers, gWrap(width, p.Danger, true, "! The approvals were not verified live, so a start is not offered. Go back and review again.")...)
		}
		if m.launchReviewNeedsEscalation() {
			r := v.Review.Policy.Reviewer
			blockers = append(blockers, m.checkbox(width, l.reviewEscalate, "r", "I accept "+launchWho(r.Provider, r.AccountName, r.Effort)+" running with full access for the review")...)
		}
		if pv.Escalated {
			blockers = append(blockers, m.checkbox(width, l.escalate, "x", "I accept that this agent may act without asking (full access or a bypassed permission mode).")...)
		}
	case launchActRetry:
		head = gHeadChip(width, "◇ Confirm the retry", p.Warning)
		consequence = "Start a new holder run in the retained worktree. Edits already there are preserved and never reset; the agent is told to inspect them.\n" +
			"The same approved inputs and native options are used again. Earlier runs stay in the history."
		if haveAttempt {
			facts = launchFactLines(width, m.attemptFacts(a))
			if launchEscalated(a.Provider, a.Options) {
				blockers = append(blockers, m.checkbox(width, l.escalate, "x", "I accept that this agent may act without asking (full access or a bypassed permission mode).")...)
			}
			if hooksNeedAck(a) {
				blockers = append(blockers, m.checkbox(width, l.hooks, "y", "I know the earlier hooks may have run partly, and want them run again.")...)
			}
		}
	case launchActStop:
		head = gHeadChip(width, "◇ Confirm stopping the attempt", p.Warning)
		consequence = "Ask the running holder to end. The attempt shows stopped only once the holder is confirmed closed.\n" +
			"The worktree, branch and history remain, and nothing is removed."
		if haveAttempt {
			facts = launchFactLines(width, m.attemptFacts(a))
		}
	case launchActCleanup:
		head = gHeadChip(width, "◇ Confirm removing the retained worktree", p.Warning)
		consequence = "Remove the retained worktree without force. The branch and the attempt history remain.\n" +
			"Nothing with uncommitted, ignored, untracked or extra committed work is removed."
		if c := l.cleanup; c != nil {
			ck := c.Preview.Check
			facts = launchFactLines(width, []launchFact{
				{label: "Attempt", value: c.Preview.AttemptID, fg: p.Text},
				{label: "Worktree", value: ck.Proposal.Path, fg: p.Text},
				{label: "Branch", value: ck.Proposal.Branch + " (remains)", fg: p.Text},
				{label: "HEAD", value: ck.HeadSHA, fg: p.Text},
				{label: "Base", value: ck.Proposal.BaseSHA, fg: p.Text},
				plainFact("Checks", fmt.Sprintf("owned %s / clean %s / hooks %s", yesNo(ck.Owned), yesNo(ck.Clean), ck.Hooks)),
				plainFact("Fingerprint", ck.Fingerprint),
			})
			if !c.Preview.Allowed {
				text := c.Preview.Blocker
				if text == "" {
					text = "This worktree is not safe to remove."
				}
				blockers = append(blockers, gWrap(width, p.Danger, true, "! "+text)...)
			}
		}
	case launchActPublish:
		head = gHeadChip(width, "◇ Confirm pushing and the Draft PR", p.Warning)
		if haveAttempt {
			consequence = fmt.Sprintf("Push branch %s to %s, then open or link one Draft pull request into %s.\n", a.Proposal.Branch, a.Proposal.RemoteName, a.Proposal.BaseRef) +
				"Nothing is merged, no reviewer is requested, and an existing pull request is only linked, never changed."
			facts = launchFactLines(width, append([]launchFact{
				{label: "Remote", value: a.Proposal.RemoteName + " / " + a.Proposal.Repository, fg: p.Text},
				{label: "Branch", value: a.Proposal.Branch, fg: p.Text},
				{label: "Into", value: a.Proposal.BaseRef + " (Draft pull request)", fg: p.Text},
			}, m.attemptResourceFacts(a)...))
		}
	}
	return head, consequence, facts, blockers
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func (m Model) checkbox(width int, on bool, key, text string) []string {
	box := "[ ]"
	if on {
		box = "[x]"
	}
	return gWrap(width, theme.Current.Warning, true, fmt.Sprintf("%s %s: %s", box, key, text))
}

// --- attempt detail ---

func launchStateText(a launch.Attempt) string {
	s := string(a.State)
	switch a.State {
	case launch.StatePreparing:
		s += ": creating the worktree and running its hooks"
	case launch.StateStarting:
		s += ": the holder is starting"
	case launch.StateStartUnknown:
		s += ": whether the holder started is not known yet; it is being reconciled, never started twice"
	case launch.StateRecoverable:
		s += ": nothing is running; retry, or clean up"
	case launch.StateRunning:
		s += ": the holder is alive"
	case launch.StateAwaitingReview:
		s += ": the holder exited; no commits or pull request are implied"
	case launch.StateFailed:
		s += ": the holder failed or was lost"
	case launch.StateStopped:
		s += ": stopped on request"
	case launch.StateCleaned:
		s += ": the worktree was removed; the branch and this history remain"
	}
	return s
}

func launchMirrorText(a launch.Attempt) string {
	st := a.Steps[launch.StepMirror]
	// The outstanding target is the desired stage; the mirrored one is only what GitHub already shows.
	target := a.DesiredStage
	if target == "" {
		target = a.MirroredStage
	}
	stage := "In progress"
	if target != "" {
		stage = launchStageName(target)
	}
	prefix := "stage update to " + stage + ": "
	switch {
	case st.Code == "remote-conflict":
		return prefix + "failed (remote stage conflict: GitHub changed elsewhere and was not overwritten)"
	case st.State == launch.StepFailed:
		return prefix + "failed"
	case st.State == launch.StepDone && a.MirroredStage != "" && (a.DesiredStage == "" || a.DesiredStage == a.MirroredStage):
		return prefix + "written"
	}
	return prefix + "pending"
}

func launchPRText(pr launch.PRLink) string {
	switch pr.State {
	case launch.PRLinked:
		return fmt.Sprintf("linked: #%d %s", pr.Number, boardSafe(pr.URL))
	case launch.PRPending:
		return "pending: not pushed yet, or not ready"
	case launch.PRUnknown:
		return "unknown: a pull request may exist; it is looked up before any new one is opened"
	case launch.PRUnavailable:
		return "unavailable: no GitHub capability or access for it"
	}
	return "not-created: nothing was pushed and no Draft PR exists"
}

func launchStepsText(a launch.Attempt) string {
	var parts []string
	for _, s := range []launch.Step{launch.StepPreflight, launch.StepWorktree, launch.StepHolder, launch.StepMirror, launch.StepPush, launch.StepPR, launch.StepCleanup} {
		rec, ok := a.Steps[s]
		state := "pending"
		if ok {
			state = string(rec.State)
		}
		text := string(s) + " " + state
		if ok && rec.Code != "" {
			text += " (" + rec.Code + ")"
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, " · ")
}

func launchRunText(r launch.Run) string {
	text := fmt.Sprintf("run %d / %s / %s", r.Number, r.SessionID, r.State)
	if r.Acknowledged {
		text += " / started"
	} else {
		text += " / not started"
	}
	if r.ExitCode != nil {
		text += fmt.Sprintf(" / exit code %d", *r.ExitCode)
	}
	return text
}

// attemptResourceFacts is the resources and approved inputs of an attempt.
func (m Model) attemptResourceFacts(a launch.Attempt) []launchFact {
	prop := a.Proposal
	facts := []launchFact{
		plainFact("Worktree", prop.Path),
		plainFact("Base", prop.BaseRef+" @ "+prop.BaseSHA),
		plainFact("Account", providerName(a.Provider)+" / "+boardTitle(a.AccountName)),
	}
	facts = append(facts, nativeFromAttempt(a).facts()...)
	facts = append(facts, plainFact("Approved by", "GitHub ID "+a.ActorID+", Garden ID "+fmt.Sprint(a.Repo)))
	return append(facts, launchInputFacts(a.Inputs)...)
}

// attemptFacts is one attempt in full.
func (m Model) attemptFacts(a launch.Attempt) []launchFact {
	p := theme.Current
	fg := p.Text
	switch a.State {
	case launch.StateRunning, launch.StateStarting, launch.StatePreparing, launch.StateStartUnknown:
		fg = p.Warning
	case launch.StateFailed:
		fg = p.Danger
	}
	facts := []launchFact{
		{label: "State", value: launchStateText(a), fg: fg, bold: true},
		plainFact("Remote", launchMirrorText(a)),
		plainFact("Pull request", launchPRText(a.PR)),
	}
	if len(a.Runs) == 0 {
		facts = append(facts, plainFact("Runs", "none yet: no holder was started"))
	}
	for _, r := range a.Runs {
		facts = append(facts, plainFact("Run", launchRunText(r)))
	}
	facts = append(facts,
		plainFact("Steps", launchStepsText(a)),
		launchFact{label: "Branch", value: a.Proposal.Branch, fg: p.Text},
	)
	return append(facts, m.attemptResourceFacts(a)...)
}

func (m Model) gsLaunchAttempt(width, rows int) []string {
	p := theme.Current
	l := m.garden.launch
	a, ok := l.current()
	if !ok {
		return gCompose(width, rows, []gblock{{lines: []string{gText(width, p.Muted, false, "This attempt is no longer in the list. Esc returns to the attempts.")}}}, nil)
	}
	blocks := []gblock{{lines: gWrap(width, p.Accent, true, strings.ReplaceAll(launchSupervisionHeading(a), "▶", m.pgG("▶", ">")))}}
	detail := strings.SplitN(m.launchSupervision(a, time.Now()), "\n\n", 2)
	if len(detail) == 2 {
		facts := gWrap(width, p.Text, false, detail[1])
		blocks = append(blocks, launchFactsBlock(facts, l.scroll))
	}
	if l.note != "" {
		blocks = append(blocks, gblock{lines: gWrap(width, p.Warning, true, "! "+l.note)})
	}
	actions := gWrap(width, p.Muted, false, m.launchNext(a))
	buttons := []string{gButton("o Open session", true)}
	if a.State.Active() {
		buttons = append(buttons, gButton("x Stop run…", false))
	}
	actions = append(actions, gButtons(width, "  ", buttons...))
	return m.settingsASCII(gCompose(width, rows, blocksWithNotices(m, width, blocks...), actions))
}

// --- history ---

func (m Model) gsLaunchHub(width, rows int) []string {
	p := theme.Current
	l := m.garden.launch
	n := m.launchIssueNumber()
	blocks := []gblock{{lines: []string{gHead(width, fmt.Sprintf("▣ IMPLEMENTATION ATTEMPTS FOR #%d", n), "local execution")}}}
	list := l.display()
	var lines []string
	switch {
	case !m.launchSupported():
		lines = gWrap(width, p.Warning, false, launchUnavailableText)
	case !l.loaded && l.readErr == "":
		lines = []string{gText(width, p.Muted, false, "Reading the local attempts…")}
	case len(list) == 0:
		lines = gWrap(width, p.Muted, false, "No local implementation attempts for this Issue. A Ready Issue can start one with s; nothing starts until you confirm.")
	default:
		lines = append(lines, gText(width, p.Text, true, "Attempts (newest first)"))
		for i, a := range list {
			marker := "  "
			if i == l.sel {
				marker = "> "
			}
			who := providerName(a.Provider) + " / " + boardTitle(a.AccountName)
			row := fmt.Sprintf("%s#%d  %s  %s  %s  %s", marker, a.Number, a.ID, a.State, who, a.RequestedAt.Local().Format("Jan 2 15:04"))
			fg := p.Muted
			switch {
			case a.State.Active():
				fg = p.Warning
			case a.State == launch.StateFailed:
				fg = p.Danger
			case a.State == launch.StateAwaitingReview:
				fg = p.Success
			}
			if i == l.sel {
				fg = p.Text
			}
			lines = append(lines, gText(width, fg, i == l.sel, row))
			if i == l.sel {
				lines = append(lines, gText(width, p.Muted, false, "    "+a.Proposal.Branch+" / "+launchMirrorText(a)))
			}
		}
	}
	blocks = append(blocks, gblock{prio: 1, lines: lines})
	if l.readErr != "" {
		blocks = append(blocks, gblock{lines: gWrap(width, p.Warning, true, "! "+l.readErr+" The list above is the last one read.")})
	}
	actions := []string{gText(width, p.Muted, false, "Enter opens an attempt · o opens its terminal · s starts a new one · Esc back")}
	return m.settingsASCII(gCompose(width, rows, blocksWithNotices(m, width, blocks...), actions))
}

// --- terminal ---

func (m Model) launchSessionSel(a launch.Attempt) (sel agentsapi.Session) {
	if id, ok := m.garden.launch.runSession(a); ok {
		sel.ID = id
	}
	sel.Provider, sel.AccountID = a.Provider, a.AccountID
	sel.Name = fmt.Sprintf("Implement #%d", a.Number)
	return sel
}

func (m Model) gsLaunchSession(width, rows int) []string {
	p := theme.Current
	l := m.garden.launch
	a, ok := l.current()
	if !ok {
		return gCompose(width, rows, []gblock{{lines: []string{gText(width, p.Muted, false, "This attempt is no longer in the list. Esc returns to it.")}}}, nil)
	}
	state := string(a.State)
	if v := m.agents.view; v != nil && v.state == attachEnded && a.State.Active() {
		state = "ended"
	}
	title := gHead(width, fmt.Sprintf("▣ IMPLEMENTATION #%d / %s", a.Number, a.ID), state)
	facts := providerName(a.Provider) + " / " + boardTitle(a.AccountName) + " · " + a.Proposal.Branch
	if id, ok := l.runSession(a); ok {
		facts += " · " + id
	}
	head := []string{title, gText(width, p.Muted, false, facts)}

	areaRows := max(rows-plannerHeadRows-plannerTailRows, 0)
	var area []string
	switch {
	case areaRows < minTerminalRows:
		area = centered([]string{"The terminal needs more room.", "Enlarge the window, or press Esc for the attempt."}, width, areaRows)
	case m.agents.view == nil:
		area = centered([]string{"Terminal session unavailable."}, width, areaRows)
	case m.agents.view.err != "" && m.agents.view.term == nil:
		// A session the record names but the holder does not know: say so and never start a replacement.
		area = centered([]string{"This session is unavailable: it may have ended or been removed.", "Bonsai never starts a replacement. Esc returns to the attempt.", m.agents.view.err}, width, areaRows)
	default:
		area = m.agentTerminalArea(m.launchSessionSel(a), width, areaRows)
	}
	out := append([]string(nil), head...)
	for _, l := range area {
		out = append(out, fit(l, width))
	}
	for len(out) < rows-plannerTailRows {
		out = append(out, blankRow(width, p.Background))
	}
	out = out[:max(rows-plannerTailRows, 0)]
	out = append(out, m.launchTail(width))
	if len(out) > rows {
		out = out[:rows]
	}
	return m.settingsASCII(out)
}

// launchTail is the one hint row under the terminal.
func (m Model) launchTail(width int) string {
	p := theme.Current
	v := m.agents.view
	text, fg := "", p.Muted
	switch {
	case v == nil:
		text = "Esc attempt"
	case v.stream == nil && v.err == "":
		text, fg = hintConnecting, p.Warning
	case v.state == attachEnded:
		text = "Session ended · Esc attempt · " + m.keys.Refresh.Help().Key + " refresh"
	case v.control && v.state == attachObservingOther:
		text, fg = hintOtherControls, p.Warning
	case v.control:
		text = hintAgentFocused
	case v.state == attachObservingOther:
		text, fg = "Another client controls this session · Enter takes control · Esc attempt", p.Warning
	default:
		text = "Observing · Enter takes control · Esc detaches and leaves the holder running"
	}
	return gText(width, fg, false, m.chrome(text))
}

// launchFactsBlock is the scrolling facts: it is never dropped, takes every spare row, and the keys scroll the
// rest, so an exact identity stays reachable at a narrow width.
func launchFactsBlock(facts []string, scroll int) gblock {
	n := min(3, len(facts))
	return gblock{min: n, want: n, flex: launchScroll(facts, scroll)}
}
