package ui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// Every function here returns lines of exactly the width it was given. The body
// is built from blocks: gCompose drops the lowest-priority ones when the height
// runs out, so a small terminal loses explanation before it loses the table or
// the action.

// gblock is one stretch of the body. prio 0 is never dropped; a higher number
// is dropped first. A block with flex grows into spare rows: other blocks are
// dropped to give it want rows, and it shrinks to min only when nothing is left
// to drop.
type gblock struct {
	prio  int
	lines []string
	min   int
	want  int
	flex  func(avail int) []string
}

func (b gblock) rows(want bool) int {
	switch {
	case b.flex == nil:
		return len(b.lines)
	case want:
		return max(b.want, b.min)
	}
	return b.min
}

// gCompose stacks blocks separated by one blank row, pins actions to the bottom
// and fills exactly rows lines.
func gCompose(width, rows int, blocks []gblock, actions []string) []string {
	blank := blankRow(width, theme.Current.Background)
	inc := make([]bool, len(blocks))
	for i := range inc {
		inc[i] = true
	}
	cost := func(want bool) int {
		n, c := 0, 0
		for i, b := range blocks {
			if inc[i] {
				n++
				c += b.rows(want)
			}
		}
		if n > 1 {
			c += n - 1
		}
		if len(actions) > 0 {
			c += len(actions)
			if n > 0 {
				c++
			}
		}
		return c
	}
	for cost(true) > rows {
		drop, best := -1, 0
		for i, b := range blocks {
			if inc[i] && b.prio > best {
				drop, best = i, b.prio
			}
		}
		if drop < 0 {
			break
		}
		inc[drop] = false
	}
	spare := max(rows-cost(false), 0)
	var out []string
	first := true
	for i, b := range blocks {
		if !inc[i] {
			continue
		}
		if !first {
			out = append(out, blank)
		}
		first = false
		lines := b.lines
		if b.flex != nil {
			lines = b.flex(b.min + spare)
			spare -= max(len(lines)-b.min, 0)
		}
		out = append(out, lines...)
	}
	room := max(rows-len(actions), 0)
	if len(out) > room {
		out = out[:room]
	}
	for len(out) < room {
		out = append(out, blank)
	}
	return append(out, actions...)[:rows]
}

// grun is one styled stretch of a line.
type grun struct {
	text string
	fg   lipgloss.Color
	bold bool
}

// gRuns draws runs after a one-cell margin on bg, cut with an ellipsis at width.
func gRuns(width int, bg lipgloss.Color, runs ...grun) string {
	var b strings.Builder
	b.WriteString(seg("", bg, false).Render(" "))
	left := width - 1
	for _, r := range runs {
		if left <= 0 {
			break
		}
		t := ansi.Truncate(r.text, left, "…")
		b.WriteString(seg(r.fg, bg, r.bold).Render(t))
		left -= lipgloss.Width(t)
	}
	return shellRow(width, bg, b.String(), "")
}

// gText is a single-run line on the base background.
func gText(width int, fg lipgloss.Color, bold bool, s string) string {
	return gRuns(width, theme.Current.Background, grun{s, fg, bold})
}

// gWrap draws a paragraph wrapped to the width; "\n" starts a new line.
func gWrap(width int, fg lipgloss.Color, bold bool, text string) []string {
	var out []string
	for _, para := range strings.Split(text, "\n") {
		for _, l := range strings.Split(ansi.Wrap(para, max(width-2, 1), ""), "\n") {
			out = append(out, gText(width, fg, bold, l))
		}
	}
	return out
}

// gHead is the title row: the title on the left and an optional scope chip on
// the right.
func gHead(width int, title, chip string) string {
	p := theme.Current
	left := seg(p.Text, p.Background, true).Render(" " + title)
	right := ""
	if chip != "" {
		right = seg(p.Muted, p.Raised, false).Render(" "+chip+" ") + seg("", p.Background, false).Render(" ")
	}
	return shellRow(width, p.Background, left, right)
}

// gHeadChip is a title drawn as a chip in a state color (incomplete, read-only).
func gHeadChip(width int, title string, fg lipgloss.Color) string {
	p := theme.Current
	return shellRow(width, p.Background, seg("", p.Background, false).Render(" ")+seg(fg, p.Raised, true).Render(" "+title+" "), "")
}

// gChip is a command or summary drawn on a raised chip.
func gChip(width int, fg lipgloss.Color, s string) string {
	p := theme.Current
	chip := seg(fg, p.Raised, false).Render(" " + ansi.Truncate(s, max(width-4, 1), "…") + " ")
	return shellRow(width, p.Background, seg("", p.Background, false).Render(" ")+chip, "")
}

func gButton(label string, focused bool) string {
	p := theme.Current
	if focused {
		return seg(p.Accent, p.Raised, true).Render(" " + label + " ")
	}
	return seg(p.Muted, p.Raised, false).Render(" " + label + " ")
}

// gButtons lays buttons on one row.
func gButtons(width int, gap string, buttons ...string) string {
	p := theme.Current
	return shellRow(width, p.Background, seg("", p.Background, false).Render(" ")+strings.Join(buttons, seg("", p.Background, false).Render(gap)), "")
}

// gField is a label and value row of the facts lists.
func gField(width, labelW int, label string, runs ...grun) string {
	p := theme.Current
	return gRuns(width, p.Background, append([]grun{{padRight(label, labelW), p.Text, false}}, runs...)...)
}

// gSpaced is a list of lines that takes a blank row between lines when the
// body has room, as the mockups do, and packs when it does not.
func gSpaced(prio int, width int, lines []string) gblock {
	blank := blankRow(width, theme.Current.Background)
	return gblock{prio: prio, min: len(lines), want: len(lines), flex: func(avail int) []string {
		if len(lines)*2-1 > avail {
			return lines
		}
		var out []string
		for i, l := range lines {
			if i > 0 {
				out = append(out, blank)
			}
			out = append(out, l)
		}
		return out
	}}
}

type gcol struct {
	head           string
	min, max, grow int
}

// gColWidths gives each column its minimum, then shares the spare width by
// grow weight up to max. A deficit comes out of the widest growing column.
func gColWidths(avail, gap int, cols []gcol) []int {
	w := make([]int, len(cols))
	spare := avail - gap*(len(cols)-1)
	for i, c := range cols {
		w[i] = c.min
		spare -= c.min
	}
	for spare > 0 {
		weight := 0
		for i, c := range cols {
			if c.grow > 0 && w[i] < c.max {
				weight += c.grow
			}
		}
		if weight == 0 {
			break
		}
		given := 0
		for i, c := range cols {
			if c.grow > 0 && w[i] < c.max {
				add := min(max(spare*c.grow/weight, 1), c.max-w[i], spare-given)
				w[i] += add
				given += add
			}
		}
		if given == 0 {
			break
		}
		spare -= given
	}
	for spare < 0 {
		widest := -1
		for i, c := range cols {
			if c.grow > 0 && w[i] > 4 && (widest < 0 || w[i] > w[widest]) {
				widest = i
			}
		}
		if widest < 0 {
			break
		}
		w[widest]--
		spare++
	}
	return w
}

type gcell struct {
	text string
	fg   lipgloss.Color
	bold bool
}

// gTableRow draws one row: the green marker on the selected one, then cells
// cut and padded to their column widths.
func gTableRow(width, gap int, widths []int, sel bool, cells []gcell) string {
	p := theme.Current
	bg := p.Background
	marker := seg("", bg, false).Render(" ")
	if sel {
		bg = p.Raised
		marker = seg(p.Accent, bg, true).Render("▌")
	}
	var b strings.Builder
	b.WriteString(marker + seg("", bg, false).Render(" "))
	for i, c := range cells {
		if i > 0 {
			b.WriteString(seg("", bg, false).Render(strings.Repeat(" ", gap)))
		}
		b.WriteString(seg(c.fg, bg, c.bold).Render(padRight(ansi.Truncate(c.text, widths[i], "…"), widths[i])))
	}
	return shellRow(width, bg, b.String(), "")
}

// gTable is a header, a rule and selectable rows that scroll to keep the
// selection visible. roomy tables take a blank row between rows when they fit.
func (m Model) gTable(width, gap int, cols []gcol, rows [][]gcell, sel int, roomy bool) gblock {
	p := theme.Current
	widths := gColWidths(width-3, gap, cols)
	head := make([]gcell, len(cols))
	for i, c := range cols {
		head[i] = gcell{c.head, p.Muted, true}
	}
	blank := blankRow(width, p.Background)
	rule := seg(p.Border, p.Background, false).Render(" " + strings.Repeat("─", max(width-2, 0)))
	return gblock{min: 2 + min(len(rows), 3), want: 2 + len(rows), flex: func(avail int) []string {
		out := []string{gTableRow(width, gap, widths, false, head)}
		listRows := avail - 1
		if avail >= 4 {
			out = append(out, shellRow(width, p.Background, rule, ""))
			listRows--
		}
		pitch := 1
		if roomy {
			pitch = m.pitchFor(len(rows), listRows)
		}
		visible := (listRows + pitch - 1) / pitch
		start := window(len(rows), visible, sel)
		for i := start; i < len(rows) && len(out) < avail; i++ {
			if pitch == 2 && i > start {
				out = append(out, blank)
			}
			out = append(out, gTableRow(width, gap, widths, i == sel, rows[i]))
		}
		return out[:min(len(out), avail)]
	}}
}

// gardenBody renders exactly rows lines of exactly width cells.
func (m Model) gardenBody(width, rows int) []string {
	var lines []string
	switch m.gardenScreen() {
	case scrUnavailable:
		lines = m.gsUnavailable(width, rows)
	case scrLoading:
		lines = m.gsLoading(width, rows)
	case scrError:
		lines = m.gsError(width, rows)
	case scrStart:
		lines = m.gsStart(width, rows)
	case scrTarget:
		lines = m.gsTarget(width, rows)
	case scrProjects:
		lines = m.gsProjects(width, rows)
	case scrPlan:
		lines = m.gsPlan(width, rows)
	case scrConfirm:
		lines = m.gsConfirm(width, rows)
	case scrIncomplete:
		lines = m.gsIncomplete(width, rows)
	case scrReadOnly:
		lines = m.gsReadOnly(width, rows)
	case scrOff:
		lines = m.gsOff(width, rows)
	case scrBoard:
		lines = m.gsBoard(width, rows)
	case scrDrafts:
		lines = m.gsDrafts(width, rows)
	}
	for len(lines) < rows {
		lines = append(lines, blankRow(width, theme.Current.Background))
	}
	return m.settingsASCII(lines[:rows])
}

// gardenNotices are the lines that explain a change under the user or a
// failure: they stay on the screen they happened on.
func (m Model) gardenNotices(width int) (gblock, bool) {
	p := theme.Current
	g := m.garden
	var lines []string
	warn := func(s string) { lines = append(lines, gWrap(width, p.Warning, true, "! "+s)...) }
	if g.notice != "" {
		warn(g.notice)
	}
	if g.err != nil {
		lines = append(lines, m.gErrorLines(width, g.err)...)
	}
	if g.hasStatus {
		if e := g.status.RefreshError; e != nil && (g.err == nil || g.err.Code != e.Code) {
			lines = append(lines, m.gErrorLines(width, e)...)
		}
		if g.status.RemoteChanged {
			warn("The remotes now point at another repository than this Garden. Setup is paused until you reconnect.")
		}
	}
	return gblock{prio: 1, lines: lines}, len(lines) > 0
}

// gErrorLines shows an error as the backend wrote it. Fix is a command for the
// user to run: Bonsai never runs it and never refreshes a login.
func (m Model) gErrorLines(width int, e *gardenapi.Error) []string {
	p := theme.Current
	fg := p.Danger
	switch e.Code {
	case gardenapi.CodeAuthRequired, gardenapi.CodeRateLimited, gardenapi.CodeOffline, gardenapi.CodeNoGitHubRemote,
		gardenapi.CodeScopeMissing, gardenapi.CodeReadOnly, gardenapi.CodePermissionDenied:
		fg = p.Warning // the user can act on these
	}
	lines := gWrap(width, fg, true, "! "+e.Message)
	if e.Next != "" {
		lines = append(lines, gWrap(width, p.Muted, false, e.Next)...)
	}
	if e.Fix != "" {
		lines = append(lines, gText(width, p.Muted, false, "Run this yourself:"), gChip(width, p.Text, e.Fix))
	}
	return lines
}

func blocksWithNotices(m Model, width int, blocks ...gblock) []gblock {
	if n, ok := m.gardenNotices(width); ok {
		blocks = append(blocks, n)
	}
	return blocks
}

func (m Model) gsUnavailable(width, rows int) []string {
	p := theme.Current
	return gCompose(width, rows, []gblock{
		{lines: []string{gHead(width, "♧ GARDEN", "")}},
		{prio: 1, lines: gWrap(width, p.Muted, false, "Garden is unavailable: no Garden backend is running in this session.\nOther workspaces are not affected.")},
	}, nil)
}

func (m Model) gsLoading(width, rows int) []string {
	p := theme.Current
	return gCompose(width, rows, []gblock{
		{lines: []string{gHead(width, "♧ GARDEN SETUP", "")}},
		{lines: []string{gText(width, p.Muted, false, "Reading GitHub for this repository…")}},
	}, nil)
}

func (m Model) gsError(width, rows int) []string {
	p := theme.Current
	e := m.garden.err
	if e == nil {
		e = &gardenapi.Error{Code: gardenapi.CodeFailed, Message: "Garden reported a state this version does not know.", Next: "Update bonsai."}
	}
	return gCompose(width, rows, []gblock{
		{lines: []string{gHeadChip(width, "! CANNOT READ GARDEN", p.Warning)}},
		{lines: m.gErrorLines(width, e)},
	}, []string{gButtons(width, "", gButton("Retry", true))})
}

// facts ---------------------------------------------------------------------

func accessWord(a gardenapi.Access) string {
	if a == gardenapi.AccessNone {
		return "no access"
	}
	return titleCase(a.String()) + " access"
}

func (m Model) repoRuns(t gardenapi.Target) []grun {
	p := theme.Current
	if t.RepoID == 0 {
		return []grun{{"not detected", p.Muted, false}}
	}
	runs := []grun{{t.FullName, p.Text, true}}
	if len(t.Aliases) > 0 {
		runs = append(runs, grun{" · " + strings.Join(t.Aliases, ", "), p.Muted, false})
	}
	return runs
}

func (m Model) identityRuns() []grun {
	p := theme.Current
	st := m.garden.status
	login := grun{"unknown", p.Muted, false}
	if st.Identity.Login != "" {
		login = grun{st.Identity.Login, p.Text, true}
	}
	access := grun{" · " + accessWord(st.Access), p.Muted, false}
	if st.Access == gardenapi.AccessRead {
		access.fg = p.Warning
	}
	return []grun{login, access}
}

func (m Model) scopeRuns() []grun {
	p := theme.Current
	switch m.garden.status.ProjectScope {
	case gardenapi.PresencePresent:
		return []grun{{"✓ project", p.Success, false}}
	case gardenapi.PresenceAbsent:
		return []grun{{"! absent", p.Warning, true}, {" · Project steps need it", p.Muted, false}}
	}
	return []grun{{"? unknown", p.Muted, false}, {" · checked when a Project step runs", p.Muted, false}}
}

// GS-01 ---------------------------------------------------------------------

var gardenModeNotes = map[gardenapi.Mode]string{
	gardenapi.ModeCreate:          "new labels and a Project",
	gardenapi.ModeConnectExisting: "reuse a Project you own",
	gardenapi.ModeSkip:            "stay off; reversible",
}

func (m Model) gsStart(width, rows int) []string {
	p := theme.Current
	modes := m.gardenModes()
	head := gHead(width, "♧ GARDEN SETUP", "Scope: Repository")
	if len(modes) == 0 {
		blocks := blocksWithNotices(m, width,
			gblock{lines: []string{head}},
			gblock{lines: gWrap(width, p.Muted, false, "No setup action is available for this repository right now.")})
		return gCompose(width, rows, blocks, nil)
	}
	sel := m.gardenRail()
	mode := modes[sel]
	t := m.gardenTarget()

	if width >= settingsWideMin && rows >= 10 {
		railW := settingsPanelW
		rightW := width - railW - settingsGap
		h := rows - 2
		facts := []string{
			gField(rightW, 24, "Repository", m.repoRuns(t)...),
			gField(rightW, 24, "GitHub identity", m.identityRuns()...),
			gField(rightW, 24, "Project scope", m.scopeRuns()...),
			gField(rightW, 24, "Existing issues", grun{"0 imported", p.Success, true}, grun{" · import one by one later", p.Muted, false}),
			gField(rightW, 24, modeCreatesLabel(mode), grun{modeCreates(mode), p.Text, false}),
		}
		blocks := blocksWithNotices(m, rightW,
			gblock{lines: []string{seg(p.Text, p.Background, true).Render(" " + strings.ToUpper(gardenRailNames[mode]))}},
			gblock{prio: 2, lines: gWrap(rightW, p.Text, false, modeIntro(mode))},
			gSpaced(1, rightW, facts),
			gblock{prio: 3, lines: gWrap(rightW, p.Muted, false, "Your GitHub identity is not a coding-agent account. Agents live in Accounts.")},
		)
		right := gCompose(rightW, h, blocks, []string{gButtons(rightW, "", gButton(modeButton(mode), true))})
		return joinColumns(width, railW, rightW, head, m.gRail(railW, h, modes, sel), right)
	}

	choices := make([]string, len(modes))
	for i, md := range modes {
		bg, marker, nameFg, bold := p.Background, " ", p.Text, false
		if i == sel {
			bg, marker, nameFg, bold = p.Raised, seg(p.Accent, p.Raised, true).Render("▌"), p.Text, true
		} else {
			marker = seg("", bg, false).Render(marker)
		}
		nameW := 26
		left := marker + seg("", bg, false).Render(" ") + seg(nameFg, bg, bold).Render(padRight(gardenRailNames[md], nameW)) +
			seg(p.Muted, bg, false).Render(" "+gardenModeNotes[md])
		choices[i] = shellRow(width, bg, left, "")
	}
	repo := "not detected"
	if t.RepoID != 0 {
		repo = t.FullName
	}
	st := m.garden.status
	login := st.Identity.Login
	if login == "" {
		login = "unknown"
	}
	scope := map[gardenapi.Presence]string{gardenapi.PresencePresent: "project scope ✓", gardenapi.PresenceAbsent: "project scope !", gardenapi.PresenceUnknown: "project scope ?"}[st.ProjectScope]
	facts := []string{
		gText(width, p.Muted, false, repo+" · "+login+" · "+titleCase(st.Access.String())+" · "+scope),
		gRuns(width, p.Background, grun{"Existing issues imported: ", p.Muted, false}, grun{"0", p.Success, true}),
		gText(width, p.Muted, false, "Coding-agent accounts are separate (Accounts)."),
	}
	blocks := blocksWithNotices(m, width,
		gblock{lines: []string{head}},
		gblock{prio: 3, lines: []string{gText(width, p.Text, false, "Garden is not set up for this repository.")}},
		gblock{lines: choices},
		gblock{prio: 2, lines: facts},
	)
	return gCompose(width, rows, blocks, []string{gButtons(width, "", gButton(modeButton(mode), true))})
}

func modeIntro(mode gardenapi.Mode) string {
	switch mode {
	case gardenapi.ModeConnectExisting:
		return "Reuse a Project you own. Garden fields are added by name where they are missing."
	case gardenapi.ModeSkip:
		return "Garden stays off for this repository. Nothing is created on GitHub, and you can turn it on later."
	}
	return "Garden is not set up for this repository.\nWork is tracked as GitHub Issues you create here or import one by one."
}

func modeCreatesLabel(mode gardenapi.Mode) string {
	if mode == gardenapi.ModeConnectExisting {
		return "Uses"
	}
	return "Creates"
}

func modeCreates(mode gardenapi.Mode) string {
	switch mode {
	case gardenapi.ModeConnectExisting:
		return "bonsai: labels, and the Stage and Bonsai Type fields in a Project you pick"
	case gardenapi.ModeSkip:
		return "nothing on GitHub"
	}
	return "bonsai: labels, one Project, Stage and Bonsai Type fields"
}

func modeButton(mode gardenapi.Mode) string {
	switch mode {
	case gardenapi.ModeConnectExisting:
		return "Choose a Project…"
	case gardenapi.ModeSkip:
		return "Skip for this repository"
	}
	return "Preview setup plan…"
}

// gRail is the left list of setup modes. On GS-03 it only shows where the user
// came from.
func (m Model) gRail(width, rows int, modes []gardenapi.Mode, sel int) []string {
	p := theme.Current
	panel := func(left string) string { return shellRow(width, p.Surface, left, "") }
	out := []string{panel(seg(p.Muted, p.Surface, true).Render(" SET UP"))}
	if rows > 4 {
		out = append(out, panel(""))
	}
	pitch := 1
	if m.work.Density != "compact" && len(out)+len(modes)*2-1 <= rows {
		pitch = 2
	}
	for i, md := range modes {
		name := gardenRailNames[md]
		if i == sel {
			out = append(out, shellRow(width, p.Raised, seg(p.Accent, p.Raised, true).Render("▌")+seg(p.Text, p.Raised, true).Render(" "+name), ""))
		} else {
			out = append(out, panel(seg(p.Text, p.Surface, false).Render("  "+name)))
		}
		if pitch == 2 && i < len(modes)-1 {
			out = append(out, panel(""))
		}
	}
	for len(out) < rows {
		out = append(out, panel(""))
	}
	return out[:rows]
}

// joinColumns puts a title row, a blank row and two panels together.
func joinColumns(width, leftW, rightW int, head string, left, right []string) []string {
	p := theme.Current
	gap := seg("", p.Background, false).Render(strings.Repeat(" ", width-leftW-rightW))
	out := []string{head, blankRow(width, p.Background)}
	for i := range left {
		out = append(out, left[i]+gap+right[i])
	}
	return out
}

// GS-02 ---------------------------------------------------------------------

func (m Model) gsTarget(width, rows int) []string {
	p := theme.Current
	g := m.garden
	cs := g.cands.Candidates
	sel := max(min(g.candSel, len(cs)-1), 0)
	accessText := func(a gardenapi.Access) string { return titleCase(a.String()) }
	kind := func(t gardenapi.Target) string {
		switch t.Kind {
		case gardenapi.KindFork:
			return "fork of " + t.ParentFullName
		case gardenapi.KindUpstream:
			return "upstream"
		}
		return "source"
	}
	var cols []gcol
	var cells [][]gcell
	wide := width >= settingsWideMin
	if wide {
		cols = []gcol{{"REPOSITORY", 16, 40, 3}, {"REMOTES", 10, 24, 2}, {"ACCESS", 7, 7, 0}, {"KIND", 12, 32, 2}, {"ID", 10, 12, 0}}
	} else {
		cols = []gcol{{"REPOSITORY", 16, 60, 1}, {"ACCESS", 7, 7, 0}, {"ID", 10, 12, 0}}
	}
	for i, c := range cs {
		name := gcell{c.FullName, p.Text, i == sel}
		id := gcell{strconv.FormatUint(c.RepoID, 10), p.Muted, false}
		if wide {
			cells = append(cells, []gcell{name, {strings.Join(c.Aliases, ", "), p.Text, false}, {accessText(c.Access), p.Text, false}, {kind(c), p.Text, false}, id})
		} else {
			cells = append(cells, []gcell{name, {accessText(c.Access), p.Text, false}, id})
		}
	}
	notes := gWrap(width, p.Muted, false, "Remotes that resolve to the same repository ID are one choice.\nThe target is identified by its numeric ID; partial names never match.")
	for i, n := range g.cands.Notes {
		if i < 3 {
			notes = append(notes, gText(width, p.Muted, false, "Ignored: "+n))
		}
	}
	blocks := blocksWithNotices(m, width,
		gblock{lines: []string{gHead(width, "CHOOSE THE GITHUB REPOSITORY", "Scope: Repository")}},
		gblock{prio: 3, lines: gWrap(width, p.Text, false, "Your remotes point at different GitHub repositories. Pick the one this Garden tracks.")},
		m.gTable(width, 2, cols, cells, sel, true),
		gblock{prio: 2, lines: notes},
	)
	if !wide && len(cs) > 0 {
		c := cs[sel]
		detail := "Remotes: " + strings.Join(c.Aliases, ", ") + " · " + kind(c)
		blocks = append(blocks, gblock{prio: 2, lines: []string{gText(width, p.Muted, false, detail)}})
	}
	var actions []string
	if len(cs) > 0 {
		actions = []string{gButtons(width, "", gButton("Use "+cs[sel].FullName+" ›", true))}
	}
	return gCompose(width, rows, blocks, actions)
}

// GS-03 ---------------------------------------------------------------------

func (m Model) gsProjects(width, rows int) []string {
	p := theme.Current
	g := m.garden
	ps := g.projects.Projects
	sel := max(min(g.projSel, len(ps)-1), 0)
	mark := func(s gardenapi.FieldState) string {
		if s == gardenapi.FieldPresent {
			return "✓"
		}
		return "+"
	}
	status := func(pr gardenapi.Project) gcell {
		switch {
		case pr.Conflict:
			return gcell{"! same name, unverified", p.Warning, false}
		case pr.Stage == gardenapi.FieldPresent || pr.Type == gardenapi.FieldPresent:
			return gcell{"compatible", p.Success, false}
		}
		return gcell{"no Garden fields", p.Muted, false}
	}
	// column is the Projects column of the screen, as wide as the space it gets.
	column := func(width, rows int, head []gblock) []string {
		showFields := width >= 74
		cols := []gcol{{"#", 3, 3, 0}, {"PROJECT", 14, 40, 2}, {"STATUS", 14, 30, 2}}
		if showFields {
			cols = []gcol{{"#", 3, 3, 0}, {"PROJECT", 14, 40, 2}, {"FIELDS", 22, 22, 0}, {"STATUS", 14, 30, 2}}
		}
		var cells [][]gcell
		for i, pr := range ps {
			row := []gcell{{pr.Number, p.Text, i == sel}, {pr.Title, p.Text, i == sel}}
			if showFields {
				fields := "—"
				if pr.Stage == gardenapi.FieldPresent || pr.Type == gardenapi.FieldPresent {
					fields = "Stage " + mark(pr.Stage) + "  Bonsai Type " + mark(pr.Type)
				}
				row = append(row, gcell{fields, p.Text, false})
			}
			cells = append(cells, append(row, status(pr)))
		}
		intro := fmt.Sprintf("Projects owned by %s. Pick one explicitly; fields are reused by name.", g.projects.Owner)
		blocks := append(head, gblock{prio: 3, lines: gWrap(width, p.Text, false, intro)})
		var actions []string
		if len(ps) == 0 {
			blocks = append(blocks, gblock{lines: gWrap(width, p.Muted, false, "No Projects found. Go back and choose Create Garden.")})
		} else {
			blocks = append(blocks, m.gTable(width, 2, cols, cells, sel, true))
			actions = []string{gButtons(width, "", gButton("Preview setup plan…", true))}
		}
		blocks = append(blocks,
			gblock{prio: 2, lines: gWrap(width, p.Muted, false, "A same-name Project is never adopted automatically.\nChoose it here only if it is yours; ownership is verified first.")},
			gblock{prio: 3, lines: gWrap(width, p.Muted, false, "Missing fields are added (+). Unrelated fields are untouched.")})
		return gCompose(width, rows, append(blocks, gblockNotices(m, width)...), actions)
	}

	if width >= settingsWideMin && rows >= 10 {
		railW := settingsPanelW
		rightW := width - railW - settingsGap
		h := rows - 2
		heading := gblock{lines: []string{seg(p.Text, p.Background, true).Render(" CONNECT EXISTING PROJECT")}}
		modes := m.gardenModes()
		railSel := 0
		for i, md := range modes {
			if md == gardenapi.ModeConnectExisting {
				railSel = i
			}
		}
		return joinColumns(width, railW, rightW, gHead(width, "♧ GARDEN SETUP", "Scope: Repository"),
			m.gRail(railW, h, modes, railSel), column(rightW, h, []gblock{heading}))
	}
	return column(width, rows, []gblock{{lines: []string{gHead(width, "CONNECT EXISTING PROJECT", "Scope: Repository")}}})
}

// gblockNotices is the notices block as a slice, empty when there is none.
func gblockNotices(m Model, width int) []gblock {
	if n, ok := m.gardenNotices(width); ok {
		return []gblock{n}
	}
	return nil
}

// GS-04 ---------------------------------------------------------------------

func changeCell(r gardenapi.PlanRow) gcell {
	p := theme.Current
	text, fg := string(r.Change), p.Success
	switch r.Change {
	case gardenapi.ChangeCreate, gardenapi.ChangeAdd:
		text = "+ " + text
	case gardenapi.ChangeReuse:
		text, fg = "= "+text, p.Muted
	case gardenapi.ChangeManual:
		text, fg = "◇ "+text, p.Warning
	}
	if r.Conflict {
		text, fg = "! "+string(r.Change), p.Danger
	}
	return gcell{text, fg, false}
}

func modeLabel(m gardenapi.Mode) string {
	switch m {
	case gardenapi.ModeCreate:
		return "Create"
	case gardenapi.ModeConnectExisting:
		return "Connect existing"
	}
	return titleCase(string(m))
}

func (m Model) gsPlan(width, rows int) []string {
	p := theme.Current
	g := m.garden
	pl := g.plan
	sel := max(min(g.planSel, len(pl.Rows)-1), 0)
	wide := width >= settingsWideMin
	var cols []gcol
	if wide {
		cols = []gcol{{"#", 3, 3, 0}, {"CHANGE", 10, 10, 0}, {"KIND", 9, 9, 0}, {"TARGET", 20, 64, 3}, {"NOTE", 14, 40, 2}}
	} else {
		cols = []gcol{{"CHANGE", 10, 10, 0}, {"TARGET", 16, 90, 1}}
	}
	cells := make([][]gcell, len(pl.Rows))
	for i, r := range pl.Rows {
		target := gcell{r.Target, p.Text, i == sel}
		note := gcell{r.Note, p.Muted, false}
		switch {
		case r.Conflict:
			note.fg = p.Danger
		case r.Advisory:
			note.fg = p.Warning
		}
		if wide {
			cells[i] = []gcell{{strconv.Itoa(i + 1), p.Muted, i == sel}, changeCell(r), {string(r.Kind), p.Text, false}, target, note}
		} else {
			cells[i] = []gcell{changeCell(r), target}
		}
	}
	chip := modeLabel(pl.Request.Mode)
	if pl.Target.FullName != "" {
		chip += " · " + pl.Target.FullName
	}
	sum := pl.Summary
	summary := gChip(width, p.Success, fmt.Sprintf("✓ %d issues imported · 0 issue bodies edited · 0 labels renamed", sum.ImportCount))
	var notes []string
	if sum.NeedsProjectScope {
		notes = append(notes, "Project steps need the project scope; label steps still run without it.")
	}
	notes = append(notes, "Before each write Bonsai re-reads GitHub and reuses anything that appeared since.")
	blocks := blocksWithNotices(m, width,
		gblock{lines: []string{gHead(width, "SETUP PLAN", chip)}},
		gblock{prio: 3, lines: []string{gText(width, p.Text, false, "Preview only. Nothing has been written to GitHub.")}},
		m.gTable(width, 2, cols, cells, sel, false),
		gblock{prio: 2, lines: []string{summary}},
		gblock{prio: 4, lines: gWrap(width, p.Muted, false, strings.Join(notes, "\n"))},
	)
	button := gButton("Review and apply…", true)
	if !sum.CanApply {
		reason := "nothing to apply"
		if sum.Conflicts > 0 {
			reason = fmt.Sprintf("blocked by %d conflict%s on GitHub", sum.Conflicts, plural(sum.Conflicts))
		}
		button = seg(p.Dim, p.Surface, false).Render(" Review and apply… ") + seg(p.Warning, p.Background, false).Render("  "+reason)
	}
	return gCompose(width, rows, blocks, []string{gButtons(width, "", button)})
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// GS-05 ---------------------------------------------------------------------

// gardenConsequence is the exact sentence the user confirms, built only from
// the previewed plan's summary and rows.
func gardenConsequence(pl gardenapi.PlanView) string {
	s := pl.Summary
	var creates []string
	if s.CreateLabels > 0 {
		creates = append(creates, fmt.Sprintf("%d label%s", s.CreateLabels, plural(s.CreateLabels)))
	}
	if s.CreateProject {
		creates = append(creates, fmt.Sprintf("the Project %q in %s", s.ProjectName, s.ProjectOwner))
	}
	var parts []string
	if len(creates) > 0 {
		parts = append(parts, "create "+strings.Join(creates, " and "))
	}
	if !s.CreateProject && s.ProjectName != "" {
		parts = append(parts, fmt.Sprintf("use the existing Project %q", s.ProjectName))
	}
	if s.ReuseLabels > 0 {
		parts = append(parts, fmt.Sprintf("reuse %d label%s", s.ReuseLabels, plural(s.ReuseLabels)))
	}
	if s.AddFields > 0 {
		var names []string
		for _, r := range pl.Rows {
			if r.Kind == gardenapi.RowField && r.Change == gardenapi.ChangeAdd && !r.Conflict {
				name, _, _ := strings.Cut(r.Target, "  (")
				if name = strings.TrimSpace(name); name != "" {
					names = append(names, name)
				}
			}
		}
		if len(names) == s.AddFields {
			parts = append(parts, fmt.Sprintf("add the %s field%s to that Project", strings.Join(names, " and "), plural(len(names))))
		} else {
			parts = append(parts, fmt.Sprintf("add %d field%s to that Project", s.AddFields, plural(s.AddFields)))
		}
	}
	if len(parts) == 0 {
		return "Nothing to change."
	}
	text := parts[0]
	if len(parts) > 1 {
		text = strings.Join(parts[:len(parts)-1], ", ") + ", and " + parts[len(parts)-1]
	}
	return titleCase(text) + "."
}

func (m Model) gsConfirm(width, rows int) []string {
	p := theme.Current
	g := m.garden
	issues := "No Issue is imported, edited or relabeled."
	if n := g.plan.Summary.ImportCount; n > 0 {
		issues = fmt.Sprintf("%d Issue%s would be imported.", n, plural(n))
	}
	var action string
	if g.busy == busyApply {
		action = gText(width, p.Warning, true, "Applying setup… completed steps are kept if it stops.")
	} else {
		apply := "[Apply setup]"
		cancel := "[Cancel]"
		var cb, ab string
		if g.confirm == 0 {
			cb = seg(p.Accent, p.Raised, true).Render(cancel)
			ab = seg(p.Text, p.Background, false).Render(apply)
		} else {
			cb = seg(p.Text, p.Background, false).Render(cancel)
			ab = seg(p.Accent, p.Raised, true).Render(apply)
		}
		action = gButtons(width, "                    ", cb, ab)
	}
	blocks := blocksWithNotices(m, width,
		gblock{lines: []string{gText(width, p.Text, true, "REVIEW THE CONSEQUENCE")}},
		gblock{lines: gWrap(width, p.Text, false, gardenConsequence(g.plan))},
		gblock{prio: 2, lines: gWrap(width, p.Text, false, issues+" No label is renamed or deleted.\nIf a step fails, completed steps are kept and you can Resume.")},
		gblock{lines: []string{action}},
		gblock{prio: 3, lines: []string{gText(width, p.Muted, false, "Cancel has initial focus.")}},
	)
	return gCompose(width, rows, blocks, nil)
}

// GS-06 ---------------------------------------------------------------------

// stepReason words a safe step code. Unknown codes read as a remote failure,
// never as the raw text.
func stepReason(code string) string {
	switch code {
	case gardenapi.StepScopeMissing:
		return "the GitHub token lacks the project scope"
	case gardenapi.StepAuthRequired:
		return "GitHub sign-in is required"
	case gardenapi.StepRateLimited:
		return "GitHub is rate limiting requests"
	case gardenapi.StepOffline:
		return "GitHub could not be reached"
	case gardenapi.StepGraphQL:
		return "GitHub rejected Bonsai's GraphQL request"
	case gardenapi.StepValidation:
		return "GitHub rejected the field name or settings"
	case gardenapi.StepLabelConflict:
		return "a label already exists with different settings"
	case gardenapi.StepProjectConflict:
		return "a Project with that name already exists"
	}
	return "GitHub rejected the request"
}

func safeStepCode(code string) string {
	switch code {
	case gardenapi.StepScopeMissing, gardenapi.StepAuthRequired, gardenapi.StepRateLimited, gardenapi.StepOffline,
		gardenapi.StepLabelConflict, gardenapi.StepProjectConflict, gardenapi.StepRemoteFailure, gardenapi.StepGraphQL, gardenapi.StepValidation:
		return code
	}
	return gardenapi.StepRemoteFailure
}

func (m Model) gsIncomplete(width, rows int) []string {
	p := theme.Current
	g := m.garden
	op, hasOp := m.gardenOperation()
	state := g.status.State
	title, fg := "! SETUP INCOMPLETE", p.Warning
	switch state {
	case gardenapi.StateAwaitingManual:
		title = "! MANUAL STEP NEEDED"
	case gardenapi.StateOperationInProgress:
		title, fg = "… SETUP RUNNING", p.Muted
	}

	var reason string
	var failed *gardenapi.OperationStep
	for i := range op.Steps {
		if op.Steps[i].Status == gardenapi.StepFailed {
			failed = &op.Steps[i]
			break
		}
	}
	switch {
	case state == gardenapi.StateOperationInProgress:
		reason = fmt.Sprintf("Setup is running for this clone: %d of %d steps done.\nRefresh to see its progress. Nothing can be changed until it ends.", op.Done, op.Total)
	case failed != nil && op.Err != nil && op.Err.Message != "":
		reason = fmt.Sprintf("Setup stopped at %s. %s", failed.Title, strings.TrimSuffix(op.Err.Message, ".")+".")
	case failed != nil:
		reason = fmt.Sprintf("Setup stopped at %s: %s.", failed.Title, stepReason(safeStepCode(failed.ErrorCode)))
	case state == gardenapi.StateAwaitingManual:
		reason = "Setup needs a step you finish on GitHub in the browser, then refresh."
	default:
		reason = "Setup did not finish. Completed steps are kept."
	}

	var cells [][]gcell
	sel := max(min(g.opSel, len(op.Steps)+len(op.AwaitingManual)-1), 0)
	nextPending := true
	for i, s := range op.Steps {
		icon, word, wfg := gcell{"○", p.Muted, false}, "waiting", p.Muted
		switch s.Status {
		case gardenapi.StepDone:
			icon, word, wfg = gcell{"✓", p.Success, false}, "done", p.Success
		case gardenapi.StepFailed:
			icon, word, wfg = gcell{"!", p.Danger, true}, safeStepCode(s.ErrorCode), p.Danger
		case gardenapi.StepSkipped:
			word = "skipped"
		case gardenapi.StepPending:
			if op.Running && nextPending {
				icon, word, wfg = gcell{"…", p.Warning, false}, "running", p.Warning
			}
			nextPending = false
		}
		cells = append(cells, []gcell{icon, {s.Title, p.Text, i == sel}, {word, wfg, false}})
	}
	for i, s := range op.AwaitingManual {
		cells = append(cells, []gcell{{"◇", p.Warning, false}, {"Manual: " + s, p.Text, len(op.Steps)+i == sel}, {"waiting", p.Muted, false}})
	}
	cols := []gcol{{"", 1, 1, 0}, {"STEP", 20, 70, 3}, {"STATUS", 14, 18, 1}}

	var fix []string
	if op.Err != nil && (op.Err.Next != "" || op.Err.Fix != "") {
		next := op.Err.Next
		if next == "" {
			next = "Run this yourself, then Resume. Bonsai never refreshes or changes your login."
		}
		fix = gWrap(width, p.Text, false, next)
		if op.Err.Fix != "" {
			fix = append(fix, gChip(width, p.Text, op.Err.Fix))
		}
	}
	var infos []string
	if hasOp && op.ID != "" {
		infos = append(infos, "Operation "+op.ID)
	}
	infos = append(infos, "completed steps are kept")
	if state != gardenapi.StateOperationInProgress {
		infos = append(infos, "Resume re-reads GitHub first")
	}
	info := strings.Join(infos, " · ")

	blocks := []gblock{
		{lines: []string{gHeadChip(width, title, fg)}},
		{prio: 2, lines: gWrap(width, p.Text, false, reason)},
	}
	switch {
	case len(cells) > 0:
		blocks = append(blocks, m.gTable(width, 1, cols, cells, sel, false))
	case state == gardenapi.StateAwaitingManual:
		blocks = append(blocks, gblock{lines: gWrap(width, p.Muted, false, "No step is waiting to be listed yet.")})
	}
	if state == gardenapi.StateOperationInProgress && op.Total > 0 {
		blocks = append(blocks, gblock{prio: 1, lines: []string{gProgress(width, op.Done, op.Total)}})
	}
	if len(fix) > 0 {
		blocks = append(blocks, gblock{prio: 1, lines: fix})
	}
	blocks = append(blocks, gblock{prio: 3, lines: []string{gText(width, p.Muted, false, info)}})
	blocks = append(blocks, gblockNotices(m, width)...)

	var buttons []string
	for i, a := range m.gardenIncompleteActions() {
		buttons = append(buttons, gButton(a.label(), i == max(min(g.opBtn, len(m.gardenIncompleteActions())-1), 0)))
	}
	var actions []string
	if len(buttons) > 0 {
		actions = []string{gButtons(width, "  ", buttons...)}
	}
	return gCompose(width, rows, blocks, actions)
}

// gProgress is a bar of done over total steps.
func gProgress(width, done, total int) string {
	p := theme.Current
	barW := max(min(30, width-16), 8)
	filled := 0
	if total > 0 {
		filled = min(done*barW/total, barW)
	}
	return gRuns(width, p.Background,
		grun{strings.Repeat("█", filled), p.Accent, false},
		grun{strings.Repeat("░", barW-filled), p.Muted, false},
		grun{fmt.Sprintf("  %d/%d", done, total), p.Muted, false})
}

// GS-07, GS-08, board ---------------------------------------------------------

func (m Model) gsReadOnly(width, rows int) []string {
	p := theme.Current
	st := m.garden.status
	t := m.gardenTarget()
	garden := grun{"no Garden found on this repository", p.Muted, false}
	switch st.RemoteGarden {
	case gardenapi.PresencePresent:
		garden = grun{"set up by a collaborator", p.Text, false}
	case gardenapi.PresenceUnknown:
		garden = grun{"not checked", p.Muted, false}
	}
	garden2 := grun{}
	if st.RemoteGarden == gardenapi.PresencePresent && st.IssueCount > 0 {
		garden2 = grun{fmt.Sprintf(" · %d issue%s", st.IssueCount, plural(st.IssueCount)), p.Muted, false}
	}
	repoRuns := m.repoRuns(t)
	if t.RepoID != 0 {
		repoRuns = []grun{{t.FullName, p.Text, true}, {fmt.Sprintf(" · id %d", t.RepoID), p.Muted, false}}
	}
	facts := []string{
		gField(width, 20, "Repository", repoRuns...),
		gField(width, 20, "GitHub identity", m.identityRuns()...),
		gField(width, 20, "Garden", garden, garden2),
		gField(width, 20, "Setup actions", grun{"unavailable: needs Write or Admin", p.Muted, false}),
	}
	blocks := blocksWithNotices(m, width,
		gblock{lines: []string{gHeadChip(width, "◎ READ-ONLY CONNECTION", p.Text)}},
		gblock{prio: 2, lines: gWrap(width, p.Text, false, fmt.Sprintf("You can read this Garden. Changing its setup needs Write access to %s.", orDefault(t.FullName, "the repository")))},
		gSpaced(1, width, facts),
	)
	var actions []string
	if st.Caps.Read {
		blocks = append(blocks, gblock{prio: 3, lines: gWrap(width, p.Muted, false, "Board, Table and Drafts stay readable. Approvals and launches are unavailable.")})
		actions = []string{gButtons(width, "", gButton("Open Board ›", true))}
	}
	return gCompose(width, rows, blocks, actions)
}

func (m Model) gsOff(width, rows int) []string {
	p := theme.Current
	blocks := blocksWithNotices(m, width,
		gblock{lines: []string{gText(width, p.Text, true, "GARDEN IS OFF FOR THIS REPOSITORY")}},
		gblock{prio: 2, lines: gWrap(width, p.Text, false, "You chose Skip. Nothing was created on GitHub and no issues are tracked.\nWorktrees, Playground and Accounts work normally.")},
		gblock{prio: 3, lines: gWrap(width, p.Muted, false, "This choice is stored for this local clone and can be changed at any time.\nSettings › Garden shows the same state.")},
	)
	var actions []string
	if m.garden.status.Caps.Unskip {
		actions = []string{gButtons(width, "", gButton("Set up Garden…", true))}
	}
	return gCompose(width, rows, blocks, actions)
}

// chrome around the body ------------------------------------------------------

// gardenSubmenu is the breadcrumb, the submenu and a rule. Board, Table and
// Drafts stay muted until a Garden exists; Table and Drafts have no plan yet.
func (m Model) gardenSubmenu(width int) []string {
	p := theme.Current
	crumbs := m.gardenCrumbs()
	sep := seg(p.Muted, p.Surface, false).Render(" › ")
	var crumb strings.Builder
	crumb.WriteString(seg("", p.Surface, false).Render(" "))
	for i, c := range crumbs {
		if i > 0 {
			crumb.WriteString(sep)
		}
		if i == len(crumbs)-1 {
			crumb.WriteString(seg(p.Text, p.Surface, true).Render(c))
		} else {
			crumb.WriteString(seg(p.Muted, p.Surface, false).Render(c))
		}
	}
	active := subSetup
	switch m.gardenScreen() {
	case scrBoard:
		active = subBoard
	case scrDrafts:
		active = subDrafts
	}
	g := m.garden
	boardOn := g.hasStatus && (g.status.State == gardenapi.StateConnected || g.status.Caps.Read)
	setupOn := !g.hasStatus || g.status.State != gardenapi.StateConnected
	items := []struct {
		icon, name string
		on, active bool
	}{
		{"✚", "Setup", setupOn, active == subSetup},
		{"▦", "Board", boardOn, active == subBoard && g.board.layout == layoutBoard},
		{"▤", "Table", boardOn, active == subBoard && g.board.layout == layoutTable},
		{"✎", "Drafts", boardOn, active == subDrafts},
		{"◇", "Needs you", boardOn, active == subBoard && g.board.layout == layoutAttention},
	}
	var tabs strings.Builder
	tabs.WriteString(seg("", p.Surface, false).Render(" "))
	for _, it := range items {
		label := " " + it.icon + " " + it.name + " "
		if it.name == "Drafts" && it.on && len(g.drafts.collection.Records) > 0 {
			label = " " + it.icon + " " + it.name + " " + fmt.Sprint(g.drafts.collection.Total) + " "
		}
		if it.name == "Needs you" {
			label = " " + it.icon + " " + it.name + " " + fmt.Sprint(g.board.collection.Summarize().NeedsYou) + " "
		}
		switch {
		case it.active && (it.name == "Drafts" || it.name == "Needs you"):
			// Amber marks inboxes and decisions needing attention.
			tabs.WriteString(seg(p.Background, p.Warning, true).Render(label))
		case it.active:
			tabs.WriteString(seg(p.Background, p.Accent, true).Render(label))
		case it.on && it.name == "Needs you":
			tabs.WriteString(seg(p.Warning, p.Surface, false).Render(label))
		case it.on:
			tabs.WriteString(seg(p.Muted, p.Surface, false).Render(label))
		default:
			tabs.WriteString(seg(p.Dim, p.Surface, false).Render(label))
		}
		tabs.WriteString(seg("", p.Surface, false).Render(" "))
	}
	rule := seg(p.Border, p.Background, false).Render(strings.Repeat("─", width))
	return m.settingsASCII([]string{
		shellRow(width, p.Surface, crumb.String(), ""),
		shellRow(width, p.Surface, tabs.String(), ""),
		rule,
	})
}

// gardenHeaderRight is the header's right side: owner/repo and the state.
func (m Model) gardenHeaderRight() string {
	p := theme.Current
	g := m.garden
	repo := filepath.Base(m.repoDir)
	if t := m.gardenTarget(); t.RepoID != 0 {
		repo = t.FullName
	}
	icon, text, fg := "○", "unavailable", p.Muted
	switch {
	case g.api == nil:
	case !g.hasStatus && g.busy != busyNone:
		icon, text = "…", "reading"
	case !g.hasStatus:
		icon, text, fg = "!", "unavailable", p.Warning
	default:
		text = gardenStateLabel(g.status.State)
		switch g.status.State {
		case gardenapi.StateSkipped:
			icon = "—"
		case gardenapi.StateReadOnly:
			icon = "◎"
		case gardenapi.StateConnected:
			icon, fg = "●", p.Success
		case gardenapi.StatePartialSetup, gardenapi.StateAwaitingManual:
			icon, fg = "!", p.Warning
		case gardenapi.StateOperationInProgress:
			icon, fg = "…", p.Warning
		}
	}
	right := seg(p.Muted, p.Surface, false).Render(repo+" · ") + seg(fg, p.Surface, false).Render(icon+" "+text+" ")
	if m.agentGlyphsASCII() {
		right = m.chrome(settingsGlyphs.Replace(right))
	}
	return right
}
