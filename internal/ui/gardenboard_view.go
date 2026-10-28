package ui

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/planrev"
	gardensync "github.com/Tiago-0liveira/bonsai/internal/core/garden/sync"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/work"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// Remote documents are plain text: terminal control sequences must not escape
// a measured region or change the terminal's state.
func boardSafe(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if r == '\t' {
			return ' '
		}
		// Keep the emoji joiner so grapheme widths and ZWJ sequences survive.
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) && r != '\u200d' {
			return -1
		}
		return r
	}, ansi.Strip(s))
}
func boardTitle(s string) string { return strings.Join(strings.Fields(boardSafe(s)), " ") }

func boardLine(width int, bg, fg lipgloss.Color, bold bool, s string) string {
	return shellRow(width, bg, seg(fg, bg, bold).Render(ansi.Truncate(s, width, "…")), "")
}

func boardPhaseColor(phase work.Phase) lipgloss.Color {
	p := theme.Current
	switch phase {
	case work.Backlog:
		return p.PhaseBacklog
	case work.PlanningPhase:
		return p.PhasePlanning
	case work.ReadyPhase:
		return p.PhaseReady
	case work.Building:
		return p.PhaseBuilding
	case work.Review:
		return p.PhaseReview
	case work.DonePhase:
		return p.PhaseDone
	}
	return p.Danger
}

// gardenWorkHead is the count row and feedback row both layouts share, so a
// filter, notice or stale read reads the same on Board and Table.
func (m Model) gardenWorkHead(width int, title, sort, view string) []string {
	p := theme.Current
	b := m.garden.board
	typeName, priority := "Any", "Any"
	if b.query.Type != "" {
		typeName = string(b.query.Type)
	}
	if b.query.Priority != "" {
		priority = b.query.Priority
	}
	n := len(b.collection.Records)
	if b.layout == layoutAttention {
		n = b.collection.Summarize().NeedsYou
	}
	count := fmt.Sprintf("%s  %d", title, n)
	if b.query.Active() {
		if b.layout == layoutAttention {
			count += " · filtered"
		} else {
			count += fmt.Sprintf(" filtered / %d", b.collection.Total)
		}
	}
	chip := "Type: " + typeName + " / Priority: " + priority
	if sort != "" {
		chip += " / Sort: " + sort
	}
	if view != "" {
		chip += " · " + view
	}
	if b.layout != layoutBoard && ansi.StringWidth(count+chip)+5 > width {
		chip = "Type: " + typeName + " / Priority: " + priority + " · " + view
		if ansi.StringWidth(count+chip)+5 > width {
			chip = view
		}
	}
	feedback := "Drafts and unverified acceptance stay off this board."
	if b.query.Text != "" {
		feedback = "Search: " + boardSafe(b.query.Text)
	}
	if b.collection.Excluded > 0 {
		excluded := fmt.Sprintf("%d draft, unverified or invalid items excluded.", b.collection.Excluded)
		if b.query.Text != "" {
			feedback += " · " + excluded
		} else {
			feedback = excluded + " " + feedback
		}
	}
	if b.notice != "" {
		feedback = b.notice
	}
	if m.garden.err != nil {
		feedback = m.garden.err.Message + " Last good board retained."
	}
	if !b.loaded {
		feedback = "Loading approved work…"
		if m.garden.busy != busyWork {
			feedback = "No successful read yet; refresh to load approved work."
		}
	}
	if b.searching {
		b.search.Width = max(1, width-12)
		feedback = "Search: " + b.search.View() + "  Enter apply / Esc cancel"
	}
	return []string{gHead(width, count, chip), boardLine(width, p.Background, p.Muted, false, " "+feedback)}
}

func (m Model) gsBoard(width, rows int) []string {
	b := m.garden.board
	if b.opened != 0 && m.garden.planrev.stage != planNone {
		return m.gsPlanRev(width, rows)
	}
	if b.opened != 0 && m.garden.launch.stage != launchStageNone {
		return m.gsLaunch(width, rows)
	}
	if b.opened != 0 && m.garden.planner.stage != plannerNone {
		return m.gsPlanner(width, rows)
	}
	if b.layout != layoutBoard {
		return m.gsTable(width, rows)
	}

	p := theme.Current
	s := b.selection
	lanes, titles := b.collection.Lanes(s.Mode), b.collection.LaneTitles(s.Mode)
	columns := min(gardenBoardColumns(width, s.Mode), len(lanes))
	capacity := gardenBoardCapacity(rows)
	s.EnsureVisible(b.collection, capacity, columns)
	view := fmt.Sprintf("View: Phases %d", len(lanes))
	if s.Mode == work.ModeDetailed {
		view = fmt.Sprintf("View: Detailed %d", len(lanes))
	}
	out := m.gardenWorkHead(width, "APPROVED WORK", "", view)
	gridRows := max(0, rows-3)
	grid := make([][]string, columns)
	widths := boardColumnWidths(width, columns)
	rail := "▌"
	if m.agentGlyphsASCII() {
		rail = "|"
	}
	for j, w := range widths {
		lane := s.Window + j
		phase := work.Phase(lane)
		if s.Mode == work.ModeDetailed {
			phase = work.Column(lane).Phase()
		}
		color := boardPhaseColor(phase)
		marker := rail
		if lane == int(s.Column) && m.shellFocus != shellNav {
			marker = ">"
		}
		grid[j] = append(grid[j], boardLine(w, p.Raised, color, true, marker+" "+titles[lane]))
		items := lanes[lane]
		start := min(s.Scroll[lane], max(0, len(items)-1))
		grid[j] = append(grid[j], boardLine(w, p.Background, p.Muted, false, " "+localPlural(len(items), "item", "items")))
		up := ""
		if start > 0 {
			up = fmt.Sprintf(" ↑ %d more", start)
		}
		grid[j] = append(grid[j], boardLine(w, p.Background, p.Muted, false, up))
		if len(items) == 0 {
			grid[j] = append(grid[j], boardLine(w, p.Background, p.Muted, false, " (empty)"))
		}
		for n := start; n < len(items) && n < start+capacity; n++ {
			grid[j] = append(grid[j], m.boardCard(w, items[n], items[n].Item.Issue.ID == s.ID)...)
		}
		remaining := len(items) - min(len(items), start+capacity)
		if remaining > 0 {
			grid[j] = append(grid[j], boardLine(w, p.Background, p.Muted, false, " ↓ "+localPlural(remaining, "more item", "more items")))
		}
		for len(grid[j]) < gridRows {
			grid[j] = append(grid[j], boardLine(w, p.Background, p.Muted, false, ""))
		}
	}
	for row := 0; row < gridRows; row++ {
		parts := make([]string, columns)
		for j := range parts {
			parts[j] = grid[j][row]
		}
		out = append(out, shellRow(width, p.Background, " "+strings.Join(parts, seg(p.Border, p.Background, false).Render(m.chrome("│"))), ""))
	}
	window := "All six phases · empty columns remain navigable"
	if s.Mode == work.ModeDetailed {
		window = "All nine stages · empty columns remain navigable"
	} else if len(lanes) > 6 {
		window = "All six phases + Unplaced · empty columns remain navigable"
	}
	if columns < len(lanes) {
		window = "Showing " + titles[s.Window] + " → " + titles[s.Window+columns-1]
		if s.Window > 0 {
			window = "‹ " + strings.Join(titles[:s.Window], " / ") + "   " + window
		}
		if s.Window+columns < len(lanes) {
			window += "   " + strings.Join(titles[s.Window+columns:], " / ") + " ›"
		}
	}
	out = append(out, gText(width, p.Muted, false, window))
	return m.settingsASCII(out[:rows])
}

func boardState(r work.Record) string {
	if r.Column == work.Blocked {
		return "! Blocked"
	}
	if r.Column == work.Planning {
		return "○ Planning · local planner"
	}
	glyph := "○"
	switch r.Column {
	case work.Ready, work.Done:
		glyph = "✓"
	case work.Running:
		glyph = ">"
	case work.PlanReview, work.Reviewing, work.HumanReview:
		glyph = "◇"
	}
	return glyph + " " + r.Column.String()
}
func boardActivity(r work.Record, detailed bool) string {
	if r.Column == work.Blocked {
		if !r.Placed {
			return "no proven phase"
		}
		if detailed {
			return "last proven: " + r.Phase.String() + ": " + r.Reason
		}
		return "Blocked in " + r.Phase.String() + ": " + r.Reason
	}
	run := r.Evidence.Implementation
	if run == nil {
		run = r.Evidence.Planner
	}
	if run == nil {
		return work.NextGate(r.Column)
	}
	name := "Planner"
	if run.Kind == "implementation" {
		name = "Implementation"
	}
	text := "Activity unknown"
	switch run.Activity {
	case work.ActivityWorking:
		text = name + " working"
		if run.Kind == "implementation" {
			text = name + " running"
		}
	case work.ActivityNeedsInput:
		text = name + " needs you"
	case work.ActivityIdle:
		text = name + " idle"
	case work.ActivityExited:
		text = name + " exited"
	case work.ActivityFailed:
		text = name + " failed"
	}
	if run.Kind == "implementation" {
		// Show the stored state verbatim: only "ended" is an exit.
		switch {
		case run.AwaitingReview:
			text = name + " awaiting review"
		case run.Activity == work.ActivityUnknown || run.Activity == work.ActivityExited && run.State != "ended":
			if run.State != "" {
				text = name + " " + run.State
			}
		}
	}
	if run.PlanReady {
		text = "Plan ready to publish"
	}
	// The marker precedes the timestamp so truncation drops the timestamp first.
	if r.Evidence.Stale {
		text += " (stale)"
	}
	if !run.Updated.IsZero() {
		text += " · updated " + run.Updated.Local().Format("Jan 2 15:04")
	}
	return text
}
func (m Model) boardCard(w int, r work.Record, selected bool) []string {
	p := theme.Current
	bg, fg := p.Surface, p.Text
	color := boardPhaseColor(r.Phase)
	if r.Column == work.Blocked {
		color = p.Danger
	}
	if selected {
		bg, fg = p.Raised, p.Success
	}
	typ := string(r.Item.Classification.Type)
	number := fmt.Sprintf("#%d", r.Item.Issue.Number)
	meta := typ + " · " + r.Priority
	if ansi.StringWidth(number+meta)+4 > w {
		typ = map[garden.ItemType]string{garden.TypeFeature: "Ftr", garden.TypeBug: "Bug", garden.TypeChore: "Chr", garden.TypeResearch: "Res"}[r.Item.Classification.Type]
		meta = typ + " · " + r.Priority
	}
	if ansi.StringWidth(number+meta)+4 > w {
		meta = r.Priority
	}
	rail := "▌"
	if m.agentGlyphsASCII() {
		rail = "|"
	}
	// Color alone vanishes under flat contrast and the mono preset.
	if selected {
		rail = ">"
	}
	first := shellRow(w, bg, seg(color, bg, true).Render(rail)+seg(fg, bg, true).Render(number), seg(p.Muted, bg, false).Render(m.chrome(meta)))
	out := []string{first}
	wrapped := strings.Split(ansi.Wrap(boardTitle(r.Item.Issue.Title), max(1, w-2), ""), "\n")
	for line := 0; line < 2; line++ {
		text := ""
		if line < len(wrapped) {
			text = wrapped[line]
		}
		if line == 1 && len(wrapped) > 2 {
			text = ansi.Truncate(text, max(1, w-3), "") + "…"
		}
		out = append(out, boardLine(w, bg, fg, false, " "+text))
	}
	out = append(out, boardLine(w, bg, color, true, " "+boardState(r)), boardLine(w, bg, p.Muted, false, " "+boardActivity(r, m.garden.board.selection.Mode == work.ModeDetailed)))
	warning, wc := gardenApprovalIndicator(r)
	if !strings.HasPrefix(warning, "!") {
		warning = ""
	}
	out = append(out, boardLine(w, bg, wc, false, " "+warning), boardLine(w, p.Background, p.Muted, false, ""))
	return out
}

// boardColumnWidths splits the width left after the margin and the column rules.
func boardColumnWidths(width, columns int) []int {
	usable := max(1, width-2-(columns-1))
	out := make([]int, columns)
	for j := range out {
		out[j] = usable / columns
		if j < usable%columns {
			out[j]++
		}
	}
	return out
}

// boardCardRows includes the trailing gap used by keyboard and mouse geometry.
func (m Model) boardCardRows(_ garden.IssueID) int { return 7 }

func gardenApprovalIndicator(r work.Record) (string, lipgloss.Color) {
	p := theme.Current
	it := r.Item
	rv := planrev.FromVerification(it.Issue, it.Verification)
	if rv.StaleApproval || rv.Gate.State == "stale" {
		return "! approval stale", p.Warning
	}
	if rv.Ready {
		return "✓ Ready: exact pair", p.Success
	}
	for _, f := range r.Findings {
		if f.Code == gardensync.FindingProjectionDrift || f.Code == gardensync.FindingStageConflict {
			return "! conflict", p.Danger
		}
	}
	if len(it.Problems) > 0 || len(it.Verification.ConflictingHeads) > 0 {
		return "! conflict", p.Danger
	}
	for _, h := range it.Verification.Heads {
		if h.Kind == garden.RevisionPlan && h.SourceSpecFingerprint != it.Verification.CurrentSpecFingerprint {
			return "! stale plan", p.Warning
		}
	}
	if len(it.Verification.RevokedApprovals) > 0 || len(it.Verification.SupersededApprovals) > 0 {
		return "! prior approval", p.Warning
	}
	return "✓ spec approved", p.Success
}

func (m Model) gardenDocument(width int) []string {
	b := m.garden.board
	r, _, ok := b.collection.Find(b.opened)
	if !ok {
		return nil
	}
	p := theme.Current
	if b.section == planTabSection {
		return m.planTabDocument(width)
	}
	var text strings.Builder
	switch b.section {
	case 0:
		status, _ := gardenApprovalIndicator(r)
		text.WriteString(status + "\n" + m.launchOverview(r.Item.Classification.Stage == garden.StageReady) + "\n\n")
		if fact := m.localFact(r.Item.Issue.ID); fact != "" {
			text.WriteString(fact + "\n\n")
		}
		body := gardenIssueBody(r.Item.Issue.Body)
		if body == "" {
			body = "The canonical issue body is empty."
		}
		text.WriteString(body)
		text.WriteString("\n\nSpecifications are validated from Drafts. Press p to request a read-only AI plan for an approved specification (a local, explicit action) and r to review its published plan revisions.\nPress a in plan review to approve the exact pair to Ready. Press v there to revalidate a changed specification. Approving starts nothing: a Ready Issue starts locally only with s and its own confirmation.")
	case 1:
		text.WriteString("SPECIFICATION\n\n")
		for _, rev := range r.Item.Verification.Revisions {
			if rev.Kind == garden.RevisionSpec {
				fmt.Fprintf(&text, "Revision %d / %s\n%s\n\n", rev.CommentID, rev.Hash, boardSafe(rev.Content))
			}
		}
	case 2:
		text.WriteString("QUESTIONS\n\n")
		body := gardenIssueBody(r.Item.Issue.Body)
		found := false
		active := false
		fence := ""
		for _, line := range strings.Split(body, "\n") {
			trimmed := strings.TrimLeft(line, " ")
			if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
				marker := string(trimmed[0])
				run := len(trimmed) - len(strings.TrimLeft(trimmed, marker))
				if fence == "" {
					fence = trimmed[:run]
				} else if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, marker+" ") == "" {
					fence = ""
				}
				if active {
					text.WriteString(line + "\n")
				}
				continue
			}
			if fence != "" {
				if active {
					text.WriteString(line + "\n")
				}
				continue
			}
			if strings.HasPrefix(line, "#") {
				heading := strings.ToLower(strings.TrimSpace(strings.TrimLeft(line, "#")))
				active = heading == "questions" || heading == "open questions"
				if active {
					found = true
				}
				continue
			}
			if active {
				text.WriteString(line + "\n")
			}
		}
		if !found {
			text.WriteString("No Questions section recorded in the canonical issue document.")
		}
		text.WriteString("\n\nQuestion authoring is unavailable until plan 08.")
	case 4:
		fmt.Fprintf(&text, "EXECUTION\n\nCanonical stage: %s\nBoard state: %s\n\nThis read projection holds no local execution.\n%s", r.Item.Classification.Stage, r.Column, m.launchExecutionLine())
	}
	for _, problem := range r.Item.Problems {
		fmt.Fprintf(&text, "\n\nAttention: %s", boardSafe(problem.Message))
	}
	var out []string
	for _, para := range strings.Split(text.String(), "\n") {
		for _, l := range strings.Split(ansi.Wrap(para, max(width, 1), ""), "\n") {
			out = append(out, shellRow(width, p.Surface, seg(p.Text, p.Surface, false).Render(l), ""))
		}
	}
	return m.settingsASCII(out)
}

// launchOverview is the launch actions of an opened Issue. Local execution is its own fact: it supplements the
// canonical stage and never rewrites the read projection.
func (m Model) launchOverview(ready bool) string {
	switch {
	case !m.launchSupported() && ready:
		return "Start implementation locally… is unavailable here: this Garden runtime has no local launch."
	case !m.launchSupported():
		return "Implementation attempts (i) are unavailable here: this Garden runtime has no local launch."
	case ready:
		return "Start implementation locally… (s)  ·  Implementation attempts (i)\nNothing starts until you confirm the exact approved pair, branch and permissions."
	}
	return "Implementation attempts (i): the local execution history of this Issue. A local start is offered once it is Ready."
}

func (m Model) launchPlansLine() string {
	if !m.launchSupported() {
		return "Launch unavailable in this runtime."
	}
	return "A Ready Issue starts locally with s, after its own confirmation."
}

func (m Model) launchExecutionLine() string {
	if !m.launchSupported() {
		return "Local implementation launch is unavailable in this runtime."
	}
	return "Press i for the local implementation attempts of this Issue. They are shown separately and never rewrite the canonical stage."
}
