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
func boardColumnColor(c work.Column) lipgloss.Color {
	p := theme.Current
	switch c {
	case work.PlanReview, work.HumanReview:
		return p.Warning
	case work.Blocked:
		return p.Danger
	case work.Approved, work.Ready, work.Running:
		return p.Success
	default:
		return p.Muted
	}
}

// gardenWorkHead is the count row and feedback row both layouts share, so a
// filter, notice or stale read reads the same on Board and Table.
func (m Model) gardenWorkHead(width int, title, sort string) []string {
	p := theme.Current
	b := m.garden.board
	typeName, priority := "Any", "Any"
	if b.query.Type != "" {
		typeName = string(b.query.Type)
	}
	if b.query.Priority != "" {
		priority = b.query.Priority
	}
	count := fmt.Sprintf("%s  %d", title, len(b.collection.Records))
	if b.query.Active() {
		count += fmt.Sprintf(" filtered / %d", b.collection.Total)
	}
	chip := "Type: " + typeName + " / Priority: " + priority
	if sort != "" {
		chip += " / Sort: " + sort
	}
	feedback := "Drafts and unverified acceptance stay off this board."
	if b.collection.Excluded > 0 {
		feedback = fmt.Sprintf("%d draft, unverified or invalid items excluded. ", b.collection.Excluded) + feedback
	}
	if b.query.Text != "" {
		feedback = "Search: " + boardSafe(b.query.Text)
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
	if b.opened != 0 && m.garden.planner.stage != plannerNone {
		return m.gsPlanner(width, rows)
	}
	if b.opened != 0 {
		return m.gardenDetail(width, rows)
	}
	if b.layout == layoutTable {
		return m.gsTable(width, rows)
	}
	p := theme.Current
	s := b.selection
	columns := gardenBoardColumns(width)
	capacity := gardenBoardCapacity(rows)
	s.EnsureVisible(b.collection, capacity, columns)
	out := m.gardenWorkHead(width, "APPROVED WORK", "")
	gridRows := max(0, rows-3)
	grid := make([][]string, columns)
	usable := max(1, width-2-(columns-1))
	base := usable / columns
	remainder := usable % columns
	for j := 0; j < columns; j++ {
		c := work.Column(s.Window + j)
		w := base
		if j < remainder {
			w++
		}
		fg := boardColumnColor(c)
		marker := " "
		if c == s.Column && m.shellFocus != shellNav {
			marker = ">"
		}
		title := c.String()
		if w < 14 {
			if c == work.PlanReview {
				title = "Plan rev"
			}
			if c == work.HumanReview {
				title = "Human rev"
			}
		}
		grid[j] = append(grid[j], boardLine(w, p.Raised, fg, true, marker+title))
		items := b.collection.Columns[c]
		start := min(s.Scroll[c], max(0, len(items)-1))
		grid[j] = append(grid[j], boardLine(w, p.Background, p.Muted, false, fmt.Sprintf(" %d items", len(items))))
		if len(items) == 0 {
			grid[j] = append(grid[j], boardLine(w, p.Background, p.Muted, false, " (empty)"))
		}
		for n := start; n < len(items) && n < start+capacity; n++ {
			r := items[n]
			selected := r.Item.Issue.ID == s.ID
			bg, fg := p.Surface, p.Text
			if selected {
				bg = p.Raised
				fg = p.Success
			}
			mark := " "
			if selected {
				mark = "▌"
			}
			if m.agentGlyphsASCII() && selected {
				mark = ">"
			}
			grid[j] = append(grid[j], boardLine(w, bg, fg, true, fmt.Sprintf("%s#%d", mark, r.Item.Issue.Number)))
			title := boardTitle(r.Item.Issue.Title)
			wrapped := strings.Split(ansi.Wrap(title, max(1, w-2), ""), "\n")
			for line := 0; line < 2; line++ {
				text := ""
				if line < len(wrapped) {
					text = wrapped[line]
				}
				if line == 1 && len(wrapped) > 2 {
					text = ansi.Truncate(text, max(1, w-3), "") + "…"
				}
				grid[j] = append(grid[j], boardLine(w, bg, fg, false, " "+text))
			}
			typ := string(r.Item.Classification.Type)
			if w < 16 {
				typ = map[garden.ItemType]string{garden.TypeFeature: "Ftr", garden.TypeBug: "Bug", garden.TypeChore: "Chr", garden.TypeResearch: "Res"}[r.Item.Classification.Type]
			}
			grid[j] = append(grid[j], boardLine(w, bg, p.Muted, false, " "+typ+" / "+r.Priority))
			status, statusColor := gardenApprovalIndicator(r)
			grid[j] = append(grid[j], boardLine(w, bg, statusColor, false, " "+status), boardLine(w, p.Background, p.Muted, false, ""))
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
	window := fmt.Sprintf("Columns %d–%d of 9 · ←/→ reveals states", s.Window+1, s.Window+columns)
	if columns == 9 {
		window = "All nine states · empty columns remain navigable"
	}
	out = append(out, gText(width, p.Muted, false, window))
	return m.settingsASCII(out[:rows])
}

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
	var text strings.Builder
	switch b.section {
	case 0:
		fmt.Fprintf(&text, "#%d %s\n\n%s / %s / %s\n\n", r.Item.Issue.Number, boardSafe(r.Item.Issue.Title), r.Column, r.Item.Classification.Type, r.Priority)
		status, _ := gardenApprovalIndicator(r)
		text.WriteString(status + "\n\n")
		body := gardenIssueBody(r.Item.Issue.Body)
		if body == "" {
			body = "The canonical issue body is empty."
		}
		text.WriteString(body)
		text.WriteString("\n\nSpecifications are validated from Drafts. Press p to request a read-only AI plan for an approved specification (a local, explicit action) and r to review its published plan revisions.\nPress a in plan review to approve the exact pair to Ready. Press v there to revalidate a changed specification. Start implementation locally: unavailable until plan 09.")
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
	case 3:
		text.WriteString("PLANS\n\n")
		rv := planrev.FromVerification(r.Item.Issue, r.Item.Verification)
		fmt.Fprintf(&text, "Current %s / Latest %s / Approved %s\n", revWord(rv.Current), revWord(rv.Latest), revWord(rv.Approved))
		for _, rev := range rv.Revisions {
			fmt.Fprintf(&text, "\nr%d / %s / %s\nSource specification: %s\n", rev.Ordinal, shortHash(rev.Hash), stateWords(rev.State), shortHash(rev.SourceSpecFingerprint))
			if rev.Stale {
				text.WriteString("Older specification; downstream disabled.\n")
			}
			if rev.Superseded {
				text.WriteString("Superseded; snapshot retained.\n")
			}
		}
		if len(rv.Revisions) == 0 {
			text.WriteString("No verified plan revisions.\n")
		}
		text.WriteString("\nPress r to review with comparison, search and exact approval. Press p to request a read-only AI plan. Launch unavailable (plan 09).")
	case 4:
		fmt.Fprintf(&text, "EXECUTION\n\nCanonical stage: %s\nBoard state: %s\n\nNo local execution context is available in this read projection.\nLocal implementation launch is unavailable until plan 09.", r.Item.Classification.Stage, r.Column)
	}
	for _, problem := range r.Item.Problems {
		fmt.Fprintf(&text, "\n\nAttention: %s", boardSafe(problem.Message))
	}
	return m.settingsASCII(gWrap(width, p.Text, false, text.String()))
}

func (m Model) gardenDetail(width, rows int) []string {
	b := m.garden.board
	p := theme.Current
	out := []string{gText(width, p.Success, true, "‹ "+b.layoutName()+" (Esc)   Issue document"), gText(width, p.Muted, false, m.gardenBoardStatus())}
	contentWidth := width
	if width >= 120 {
		contentWidth -= 26
	}
	doc := m.gardenDocument(contentWidth)
	avail := max(0, rows-3)
	start := max(0, min(b.docScroll, max(0, len(doc)-avail)))
	for i := 0; i < avail; i++ {
		line := blankRow(contentWidth, p.Background)
		if start+i < len(doc) {
			line = doc[start+i]
		}
		if width >= 120 {
			rail := ""
			fg, bg := p.Muted, p.Surface
			if i == 0 {
				rail = "IN THIS ISSUE"
			}
			if i >= 2 && i < 2+len(gardenDocumentSections)*2 && i%2 == 0 {
				n := (i - 2) / 2
				rail = "  " + gardenDocumentSections[n]
				if n == b.section {
					rail = "> " + gardenDocumentSections[n]
					fg, bg = p.Success, p.Raised
				}
			}
			line = boardLine(24, bg, fg, i == 2+b.section*2, rail) + "  " + line
		}
		out = append(out, shellRow(width, p.Background, line, ""))
	}
	tabs := []string{}
	for i, s := range gardenDocumentSections {
		if i == b.section {
			s = "[" + s + "]"
		}
		tabs = append(tabs, s)
	}
	out = append(out, gText(width, p.Success, false, strings.Join(tabs, " / ")))
	return m.settingsASCII(out[:rows])
}
