package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/plan"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// Local work. The board flags the Issues that have a planner running on this machine, or one whose plan file is
// ready and not published. It is a local overlay: it never moves an Issue between the canonical stages, and a
// failed read keeps the last good summaries, marked stale.

const (
	// localFastEvery applies while a planner is live or its screen is open; localSlowEvery otherwise, so a planner
	// started from another terminal still shows up. The timer runs only while the Garden workspace is in view.
	localFastEvery = 4 * time.Second
	localSlowEvery = 15 * time.Second
	localTimeout   = 15 * time.Second
)

type gardenLocalState struct {
	issues map[uint64]gardenapi.IssueLocalWork
	loaded bool
	// stale is true while the newest read failed: issues is the last good one.
	stale bool
	// gate orders LocalWork replies among themselves. It is not the shared gate: a local read racing the work
	// read must not make the board's own reply look old.
	gate gardenapi.Gate
	seq  uint64 // newest request; only its failure may mark the summaries stale

	tickOn  bool
	tickGen uint64
}

type (
	gardenLocalMsg struct {
		seq  uint64
		view gardenapi.LocalWorkView
		err  error
	}
	gardenLocalTickMsg struct{ gen uint64 }
	// gardenPlannerPollMsg is the hub's own refresh of its attempts and their live overlay.
	gardenPlannerPollMsg struct {
		issue uint64
		view  gardenapi.PlannerAttemptsView
		err   error
	}
	// gardenPlannerPlanMsg carries the plan text of a ready plan file for the publish text box.
	gardenPlannerPlanMsg struct {
		seq     uint64
		attempt string
		view    gardenapi.PlannerPlanView
		err     error
	}
)

// gardenLocalTickCmd is the one timer of the local overlay. Tests skip it by name.
func gardenLocalTickCmd(gen uint64, d time.Duration) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(d)
		return gardenLocalTickMsg{gen: gen}
	}
}

func (m Model) gardenLoadLocal() (Model, tea.Cmd) {
	g := &m.garden
	if g.api == nil || !g.hasStatus {
		return m, nil
	}
	g.local.seq++
	seq, api := g.local.seq, g.api
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), localTimeout)
		defer cancel()
		v, err := api.LocalWork(ctx)
		return gardenLocalMsg{seq: seq, view: v, err: err}
	}
}

func (m Model) onGardenLocal(msg gardenLocalMsg) (Model, tea.Cmd) {
	l := &m.garden.local
	if msg.err != nil {
		if msg.seq == l.seq && l.loaded {
			l.stale = true
		}
		return m, nil
	}
	s := msg.view.Stamp
	if s.GardenID != m.garden.status.Target.RepoID || !l.gate.Accept(s) {
		return m, nil
	}
	// A successful read, including an empty one, replaces the list.
	l.issues, l.loaded = msg.view.Issues, true
	if msg.seq == l.seq {
		l.stale = false
	}
	return m, nil
}

// gardenLocalStart begins the timer once, when the Garden has data to flag.
func (m *Model) gardenLocalStart() tea.Cmd {
	l := &m.garden.local
	if l.tickOn || m.garden.api == nil {
		return nil
	}
	l.tickOn = true
	l.tickGen++
	return gardenLocalTickCmd(l.tickGen, m.localEvery())
}

func (m Model) localEvery() time.Duration {
	if m.garden.planner.stage != plannerNone || m.garden.local.anyLive() {
		return localFastEvery
	}
	return localSlowEvery
}

func (l gardenLocalState) anyLive() bool {
	for _, w := range l.issues {
		if w.Planner != nil && w.Planner.Live {
			return true
		}
	}
	return false
}

func (m Model) onGardenLocalTick(msg gardenLocalTickMsg) (Model, tea.Cmd) {
	l := &m.garden.local
	if msg.gen != l.tickGen || !l.tickOn {
		return m, nil
	}
	if m.workspace != wsGarden || m.garden.api == nil {
		l.tickOn = false // re-armed when the Garden is entered again
		return m, nil
	}
	l.tickGen++
	cmds := []tea.Cmd{gardenLocalTickCmd(l.tickGen, m.localEvery())}
	var read tea.Cmd
	m, read = m.gardenLoadLocal()
	cmds = append(cmds, read)
	if m.garden.planner.stage == plannerHub && m.garden.board.opened != 0 {
		cmds = append(cmds, m.gardenPlannerPoll())
	}
	return m, tea.Batch(cmds...)
}

// gardenPlannerPoll re-reads the hub's attempts without touching the busy track, the notice or the shared gate:
// it is a background refresh, ordered by the stamp of what the screen already shows.
func (m Model) gardenPlannerPoll() tea.Cmd {
	g := &m.garden
	if g.api == nil || g.busy.writes() {
		return nil
	}
	issue, api := uint64(g.board.opened), g.api
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), localTimeout)
		defer cancel()
		v, err := api.PlannerAttempts(ctx, issue)
		return gardenPlannerPollMsg{issue: issue, view: v, err: err}
	}
}

func (m Model) onGardenPlannerPoll(msg gardenPlannerPollMsg) (Model, tea.Cmd) {
	p := &m.garden.planner
	if msg.err != nil || p.preview == nil || msg.issue != uint64(m.garden.board.opened) || msg.view.IssueID != msg.issue {
		return m, nil // a failed poll keeps what the screen shows
	}
	if p.stage != plannerHub || !p.fresh(msg.view.Stamp) {
		return m, nil
	}
	p.replaceAttempts(msg.view.Attempts)
	p.preview.Overlay = msg.view.Overlay
	return m, nil
}

// fresh reports whether a reply is newer than what the planner screen already took in, and remembers it.
func (p *gardenPlannerState) fresh(s gardenapi.Stamp) bool {
	if s.LocalRepoID == "" {
		return false
	}
	if p.seen.LocalRepoID == s.LocalRepoID && (s.Epoch < p.seen.Epoch || s.Epoch == p.seen.Epoch && s.Generation <= p.seen.Generation) {
		return false
	}
	p.seen = s
	return true
}

// --- what the board shows ---

type localKind int

const (
	localEnded localKind = iota
	localIdle
	localWorking
	localNeeds
	localReady
	localInvalid
)

// localBadge is the compact flag of one local run. long is for roomy cells, short for tight ones.
type localBadge struct {
	kind        localKind
	long, short string
	fg          lipgloss.Color
}

// activityKey is the agent-status key of a summary, in the vocabulary of the Agents tab.
func activityKey(a *agentactivity.Activity) string {
	key, _ := agentactivity.Display("running", a)
	return key
}

func localBadgeOf(s gardenapi.LocalRunSummary, stale bool) localBadge {
	p := theme.Current
	b := localBadge{localEnded, "planner ended", "ended", p.Muted}
	key := activityKey(s.Activity)
	switch {
	case s.PlanFile == plan.PlanFileInvalid:
		b = localBadge{localInvalid, "plan file invalid", "invalid", p.Danger}
	case s.PlanFile == plan.PlanFileReady && (!s.Live || key != "waiting"):
		b = localBadge{localReady, "plan ready", "ready", p.Success}
	case !s.Live:
	case key == "waiting":
		b = localBadge{localNeeds, "planner needs you", "needs you", p.Warning}
	case key == "idle":
		b = localBadge{localIdle, "planner idle", "idle", p.Muted}
	default:
		b = localBadge{localWorking, "planner working", "working", p.Warning}
	}
	if stale {
		b.long, b.short = b.long+"?", b.short+"?"
	}
	return b
}

// localFor is the summary to flag on an Issue.
func (m Model) localFor(issue garden.IssueID) (gardenapi.LocalRunSummary, bool) {
	w, ok := m.garden.local.issues[uint64(issue)]
	if !ok || w.Planner == nil {
		return gardenapi.LocalRunSummary{}, false
	}
	return *w.Planner, true
}

func (m Model) localBadgeFor(issue garden.IssueID) (localBadge, bool) {
	s, ok := m.localFor(issue)
	if !ok {
		return localBadge{}, false
	}
	return localBadgeOf(s, m.garden.local.stale), true
}

// localTotals counts the flagged Issues by kind.
func (m Model) localTotals() (counts [localInvalid + 1]int) {
	for _, w := range m.garden.local.issues {
		if w.Planner != nil {
			counts[localBadgeOf(*w.Planner, false).kind]++
		}
	}
	return counts
}

func localPlural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// localSummaryText is the compact total for the footer: "1 planner working · 1 plan ready". Empty when nothing
// is flagged. compact drops the nouns for tight footers.
func (m Model) localSummaryText(compact bool) string {
	c := m.localTotals()
	var parts []string
	add := func(k localKind, long, short string) {
		switch {
		case c[k] == 0:
		case compact:
			parts = append(parts, fmt.Sprintf("%d %s", c[k], short))
		default:
			parts = append(parts, long)
		}
	}
	add(localWorking, localPlural(c[localWorking], "planner working", "planners working"), "working")
	add(localNeeds, localPlural(c[localNeeds], "planner needs you", "planners need you"), "need you")
	add(localIdle, localPlural(c[localIdle], "planner idle", "planners idle"), "idle")
	add(localReady, localPlural(c[localReady], "plan ready", "plans ready"), "ready")
	add(localInvalid, localPlural(c[localInvalid], "plan file invalid", "plan files invalid"), "invalid")
	if len(parts) == 0 {
		return ""
	}
	text := strings.Join(parts, " · ")
	if m.garden.local.stale {
		text += " (stale)"
	}
	return text
}

// withLocalSummary appends the local planner totals to the footer status. Tight footers drop the sync time for
// them: the totals are what changes while the user is looking.
func (m Model) withLocalSummary(status string) string {
	if m.width < 120 {
		if s := m.localSummaryText(true); s != "" {
			state, _, _ := strings.Cut(status, " · ")
			return state + " · " + s
		}
		return status
	}
	if s := m.localSummaryText(false); s != "" {
		return status + " · " + s
	}
	return status
}

// localAgentText is the agent status of a summary in the Agents tab's words.
func localAgentText(s gardenapi.LocalRunSummary) string {
	if !s.Live {
		return "not running"
	}
	_, label := agentactivity.Display("running", s.Activity)
	if activityKey(s.Activity) == "waiting" {
		label = "waiting for you" + strings.TrimPrefix(label, "Needs input")
	}
	return strings.ToLower(label[:1]) + label[1:]
}

func planFileText(s plan.PlanFileState) string {
	switch s {
	case plan.PlanFileWriting:
		return "writing"
	case plan.PlanFileReady:
		return "ready"
	case plan.PlanFileInvalid:
		return "invalid"
	}
	return "none yet"
}

// keepsRunning is the reminder shown whenever a planner may still be live after its screen was left.
const keepsRunning = "Planner keeps running in the background. Find it again: the badge on this Issue, or p."

// localFact is the "Local planner" line of an opened Issue; empty when it has no local planner work.
func (m Model) localFact(issue garden.IssueID) string {
	s, ok := m.localFor(issue)
	if !ok {
		return ""
	}
	state := s.State
	if state == "" {
		state = "unknown"
	}
	text := fmt.Sprintf("Local planner: %s · agent %s · plan file %s", state, localAgentText(s), planFileText(s.PlanFile))
	if !s.Updated.IsZero() {
		text += " · updated " + s.Updated.Local().Format("Jan 2 15:04")
	}
	if m.garden.local.stale {
		text += " · last read failed, showing the last known state"
	}
	switch {
	case s.PlanFile == plan.PlanFileReady:
		text += "\nPress p to open it, then u to review and publish the plan."
	case s.Live:
		text += "\nPress p to open it. " + keepsRunning
	default:
		text += "\nPress p to open it."
	}
	return text
}

// attemptOverlay is the live view of one attempt on the planner screen.
func (p gardenPlannerState) attemptOverlay(id string) gardenapi.PlannerLiveView {
	if p.preview == nil {
		return gardenapi.PlannerLiveView{}
	}
	return p.preview.Overlay[id]
}

// liveAttempt reports whether any attempt of the shown history can still change.
func (p gardenPlannerState) liveAttempt() bool {
	if p.preview == nil {
		return false
	}
	for _, a := range p.preview.Attempts {
		if a.State.Live() {
			return true
		}
	}
	return false
}

// readyAttempt is the first attempt in display order (newest first) whose plan file is ready.
func (p gardenPlannerState) readyAttempt() (idx int, a plan.Attempt, ok bool) {
	for i, a := range p.display() {
		if p.attemptOverlay(a.ID).PlanFile.State == plan.PlanFileReady {
			return i, a, true
		}
	}
	return 0, plan.Attempt{}, false
}

// cleanPlanText drops terminal control sequences from the planner's plan: it came from an agent.
func cleanPlanText(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, ansi.Strip(s))
}
