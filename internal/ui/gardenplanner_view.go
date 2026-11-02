package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/plan"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// gsPlanner draws the planner screen of the opened Issue.
func (m Model) gsPlanner(width, rows int) []string {
	switch m.garden.planner.stage {
	case plannerChoose:
		return m.gsPlannerChoose(width, rows)
	case plannerSession:
		return m.gsPlannerSession(width, rows)
	}
	return m.gsPlannerHub(width, rows)
}

func (m Model) gardenPlannerFocusLabel() string {
	switch {
	case m.plannerTerminalMode():
		return "Read-only planning / Terminal (focused)"
	case m.garden.planner.stage == plannerSession:
		return "Read-only planning / Session"
	case m.garden.planner.stage == plannerChoose:
		return "Read-only planning / Account"
	}
	return "Read-only planning"
}

// attemptStateText is one attempt's state in words, with the reason of a failed or cancelled one.
func attemptStateText(a plan.Attempt) string {
	s := string(a.State)
	if a.Reason != "" && a.State != plan.StateCancelled {
		s += " (" + a.Reason + ")"
	}
	return s
}

// shortCommit is a commit as people read it.
func shortCommit(c string) string {
	if len(c) > 9 {
		return c[:9]
	}
	return c
}

func baseText(b plan.Base, include bool) string {
	where := shortCommit(b.Commit)
	if b.Branch != "" {
		where += " on " + b.Branch
	} else {
		where += " (detached)"
	}
	switch {
	case !b.Dirty:
		return where + " / clean tree"
	case include:
		return fmt.Sprintf("%s / %d uncommitted %s, included", where, b.DirtyFiles, changeWord(b.DirtyFiles))
	}
	return fmt.Sprintf("%s / %d uncommitted %s, not included", where, b.DirtyFiles, changeWord(b.DirtyFiles))
}

func changeWord(n int) string {
	if n == 1 {
		return "change"
	}
	return "changes"
}

func (m Model) gsPlannerHub(width, rows int) []string {
	p := theme.Current
	st := m.garden.planner
	v := st.preview
	if v == nil {
		return gCompose(width, rows, []gblock{{lines: []string{gText(width, p.Muted, false, "Reading the specification and the checkout…")}}}, nil)
	}
	blocks := []gblock{{lines: []string{gHeadChip(width, "▣ LOCAL READ-ONLY PLANNER FOR #"+fmt.Sprint(v.Number), p.Success)}}}
	if !v.CanRequest() {
		blocks[0] = gblock{lines: []string{gHeadChip(width, "! NO PLANNER FOR #"+fmt.Sprint(v.Number)+" YET", p.Warning)}}
		blocks = append(blocks, gblock{prio: 1, lines: gWrap(width, p.Text, false, boardSafe(v.Unavailable))})
		if v.Gate.Reason != "" {
			blocks = append(blocks, gblock{prio: 2, lines: gWrap(width, p.Warning, false, "Gate: "+gateLabel(v.Gate.State)+" — "+v.Gate.Reason+".")})
		}
	} else {
		facts := []string{
			gField(width, 14, "Issue", grun{fmt.Sprintf("#%d %s", v.Number, boardTitle(v.Title)), p.Text, true}),
			gField(width, 14, "Specification", grun{fmt.Sprintf("s%d / revision %s / fingerprint %s…", v.Spec.Ordinal, shortHash(v.Spec.RevisionHash), shortHash(v.Spec.Fingerprint)), p.Muted, false}),
			gField(width, 14, "Repository", grun{baseText(v.Base, st.dirtyDecision()), p.Muted, false}),
			gField(width, 14, "Role", grun{"planner / read-only / no worktree is created", p.Muted, false}),
		}
		blocks = append(blocks, gSpaced(1, width, facts))
		if v.Base.Dirty {
			box := "[ ]"
			if st.dirtyDecision() {
				box = "[x]"
			}
			text := fmt.Sprintf("%s Let the planner read the %d uncommitted %s (x). Without it no planner starts.", box, v.Base.DirtyFiles, changeWord(v.Base.DirtyFiles))
			blocks = append(blocks, gblock{prio: 1, lines: gWrap(width, p.Warning, false, text)})
		}
		blocks = append(blocks, gblock{prio: 3, lines: gWrap(width, p.Muted, false,
			"The account you choose next decides the permission. Claude and Codex enforce read-only below the model; Antigravity cannot, so it cannot plan. Nothing falls back to unrestricted access.")})
	}
	blocks = append(blocks, m.plannerAttemptsBlock(width, st, v))
	if st.confirmCancel {
		blocks = append(blocks, gblock{lines: []string{gText(width, p.Warning, true, "Cancel this attempt? y cancels it and keeps its history; any other key keeps it running.")}})
	}
	if st.busyNote(m) != "" {
		blocks = append(blocks, gblock{lines: []string{gText(width, p.Warning, true, st.busyNote(m))}})
	}
	blocks = blocksWithNotices(m, width, blocks...)
	buttons := []string{gButton("Back", st.btn == 0)}
	if v.CanRequest() {
		buttons = append(buttons, gButton("Request AI plan", st.btn == 1))
	}
	actions := []string{gButtons(width, "  ", buttons...),
		gText(width, p.Muted, false, "Only this button starts a planner. Looking at an Issue, refreshing it or editing it on GitHub never does.")}
	return m.settingsASCII(gCompose(width, rows, blocks, actions))
}

// busyNote tells what the screen is waiting for while keys are ignored.
func (st gardenPlannerState) busyNote(m Model) string {
	switch m.garden.busy {
	case busyPlannerStart:
		return "Starting the planner… keys are ignored until it answers."
	case busyPlannerCancel:
		return "Cancelling… keys are ignored until it answers."
	}
	return ""
}

// plannerAttemptsBlock lists the history, newest first. Superseded and failed attempts stay visible.
func (m Model) plannerAttemptsBlock(width int, st gardenPlannerState, v *gardenapi.PlannerPreviewView) gblock {
	p := theme.Current
	list := st.display()
	if len(list) == 0 {
		return gblock{prio: 2, lines: []string{gText(width, p.Muted, false, "No planner attempts yet.")}}
	}
	lines := []string{gText(width, p.Text, true, "Attempts (newest first)")}
	for i, a := range list {
		marker := "  "
		if i == st.sel {
			marker = "> "
		}
		spec := fmt.Sprintf("s%d", a.Spec.Ordinal)
		if a.Spec.Fingerprint != v.Spec.Fingerprint {
			spec += " (older specification)"
		}
		who := providerName(a.Provider) + " / " + boardTitle(a.AccountName)
		if a.Model != "" {
			who += " · " + boardTitle(a.Model)
		}
		ov := st.attemptOverlay(a.ID)
		row := fmt.Sprintf("%s#%d  %s%s  %s  for %s  %s", marker, len(list)-i, attemptStateText(a), attemptLiveText(a, ov), who, spec, a.RequestedAt.Local().Format("Jan 2 15:04"))
		fg := p.Muted
		switch {
		case a.State.Live():
			fg = p.Warning
		case a.State == plan.StateEnded:
			fg = p.Success
		case a.State == plan.StateFailed:
			fg = p.Danger
		}
		if i == st.sel {
			fg = p.Text
		}
		lines = append(lines, gText(width, fg, i == st.sel, row))
		if i == st.sel {
			detail := "read-only via " + a.Isolation.Mechanism + " / base " + shortCommit(a.Base.Commit)
			if a.Base.Dirty {
				detail += fmt.Sprintf(" + %d uncommitted", a.Base.DirtyFiles)
				if a.Isolation.DirtyAccepted {
					detail += " (accepted)"
				}
			}
			lines = append(lines, gText(width, p.Muted, false, "    "+detail))
			if f := planFileDetail(ov); f != "" {
				lines = append(lines, gText(width, p.Muted, false, "    "+f))
			}
		}
	}
	lines = append(lines, m.plannerHubNotes(width, st)...)
	return gblock{prio: 1, lines: lines}
}

// attemptLiveText is the agent status and plan file state appended to an attempt's state, in the Agents tab's
// words. A finished attempt has no agent status.
func attemptLiveText(a plan.Attempt, ov gardenapi.PlannerLiveView) string {
	var parts []string
	if a.State.Live() {
		_, label := agentactivity.Display("running", ov.Activity)
		parts = append(parts, strings.ToLower(label))
	}
	switch ov.PlanFile.State {
	case plan.PlanFileWriting:
		parts = append(parts, "plan writing")
	case plan.PlanFileReady:
		parts = append(parts, "plan ready")
	case plan.PlanFileInvalid:
		parts = append(parts, "plan file invalid")
	}
	if len(parts) == 0 {
		return ""
	}
	return " · " + strings.Join(parts, " · ")
}

// planFileDetail is the selected attempt's plan file line: state, size and when it last changed.
func planFileDetail(ov gardenapi.PlannerLiveView) string {
	f := ov.PlanFile
	if f.State == "" || f.State == plan.PlanFileNone {
		return ""
	}
	text := "plan file " + planFileText(f.State)
	if f.State == plan.PlanFileInvalid && f.Reason != "" {
		text += " (" + boardTitle(f.Reason) + ")"
	}
	if f.Size > 0 {
		text += fmt.Sprintf(" · %s", byteSize(f.Size))
	}
	if !f.ModTime.IsZero() {
		text += " · changed " + f.ModTime.Local().Format("Jan 2 15:04:05")
	}
	return text
}

func byteSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f KB", float64(n)/1024)
}

// plannerHubNotes are the lines under the attempts that say what to do next: review a ready plan, or that a live
// planner outlives this screen.
func (m Model) plannerHubNotes(width int, st gardenPlannerState) []string {
	p := theme.Current
	var out []string
	if idx, _, ok := st.readyAttempt(); ok {
		text := "Plan ready: press u to review and publish."
		if idx != st.sel {
			text = fmt.Sprintf("Plan ready in attempt #%d: select it, then press u to review and publish.", len(st.display())-idx)
		}
		out = append(out, gText(width, p.Success, true, text))
	}
	if st.liveAttempt() {
		out = append(out, gWrap(width, p.Warning, false, keepsRunning)...)
	}
	return out
}

func (m Model) gsPlannerChoose(width, rows int) []string {
	p := theme.Current
	d := m.garden.planner.dlg
	if d == nil {
		return gCompose(width, rows, []gblock{{lines: []string{gText(width, p.Muted, false, "Nothing to choose.")}}}, nil)
	}
	d2 := *d
	d2.SetSize(width, rows)
	if v := m.garden.planner.preview; v != nil {
		d2.issue = fmt.Sprintf("#%d %s", v.Number, boardTitle(v.Title))
	}
	lines := strings.Split(d2.View(), "\n")
	out := make([]string, 0, rows)
	for _, l := range lines {
		out = append(out, shellRow(width, p.Background, ansi.Truncate(l, width, ""), ""))
	}
	for len(out) < rows {
		out = append(out, blankRow(width, p.Background))
	}
	return m.settingsASCII(out[:rows])
}

// plannerSessionSel is the session the body shows, shaped for the terminal controller's own helpers.
func (m Model) plannerSessionSel(a plan.Attempt) agentsapi.Session {
	sel := agentsapi.Session{}
	sel.ID, sel.Provider, sel.AccountID = a.SessionID, a.Provider, a.AccountID
	sel.Name = fmt.Sprintf("Plan #%d", a.Number)
	return sel
}

func (m Model) gsPlannerSession(width, rows int) []string {
	p := theme.Current
	st := m.garden.planner
	a, ok := st.attemptByID(st.attempt)
	if !ok {
		return gCompose(width, rows, []gblock{{lines: []string{gText(width, p.Muted, false, "This attempt is no longer in the list. Esc returns to the attempts.")}}}, nil)
	}
	state := attemptStateText(a)
	if v := m.agents.view; v != nil && v.state == attachEnded && a.State.Live() {
		state = "ended"
	}
	title := gHead(width, fmt.Sprintf("▣ LOCAL READ-ONLY PLANNER / #%d / attempt %s", a.Number, strings.TrimPrefix(a.ID, "plan-")), state)
	facts := providerName(a.Provider) + " / " + boardTitle(a.AccountName)
	if a.Model != "" {
		facts += " · " + boardTitle(a.Model)
	}
	facts += " · read-only / no worktree · s" + fmt.Sprint(a.Spec.Ordinal) + " @ " + shortCommit(a.Base.Commit)
	head := []string{title, gText(width, p.Muted, false, facts)}

	areaRows := max(rows-plannerHeadRows-plannerTailRows, 0)
	var area []string
	switch {
	case areaRows < minTerminalRows:
		area = centered([]string{"The terminal needs more room.", "Enlarge the window, or press Esc for the attempts."}, width, areaRows)
	case m.agents.view == nil:
		area = centered([]string{"Terminal session unavailable."}, width, areaRows)
	default:
		area = m.agentTerminalArea(m.plannerSessionSel(a), width, areaRows)
	}
	out := append([]string(nil), head...)
	for _, l := range area {
		out = append(out, fit(l, width))
	}
	for len(out) < rows-plannerTailRows {
		out = append(out, blankRow(width, p.Background))
	}
	out = out[:max(rows-plannerTailRows, 0)]
	out = append(out, m.plannerTail(width))
	if len(out) > rows {
		out = out[:rows]
	}
	return m.settingsASCII(out)
}

// plannerTail is the one hint row under the terminal.
func (m Model) plannerTail(width int) string {
	p := theme.Current
	v := m.agents.view
	st := m.garden.planner
	text, fg := "", p.Muted
	switch {
	case st.confirmCancel:
		text, fg = "Cancel this attempt? y cancels it and keeps its history; any other key keeps it running.", p.Warning
	case m.garden.busy == busyPlannerCancel:
		text, fg = "Cancelling… keys are ignored until it answers.", p.Warning
	case v == nil:
		text = "Esc attempts"
	case v.stream == nil && v.err == "":
		text, fg = hintConnecting, p.Warning
	case v.state == attachEnded:
		text = "Session ended · Esc attempts · " + m.keys.Refresh.Help().Key + " refresh"
	case v.control && v.state == attachObservingOther:
		text, fg = hintOtherControls, p.Warning
	case v.control:
		text = hintAgentFocused
	case v.state == attachObservingOther:
		text, fg = "Another client controls this planner · Enter takes control · Esc attempts", p.Warning
	default:
		text = "Enter focuses the terminal · c cancels the attempt · Esc attempts"
	}
	return gText(width, fg, false, m.chrome(text))
}
