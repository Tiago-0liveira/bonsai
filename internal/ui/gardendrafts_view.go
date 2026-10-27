package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/drafts"
	gardensync "github.com/Tiago-0liveira/bonsai/internal/core/garden/sync"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/work"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// typeWord is the type as a word, never color alone.
func typeWord(t garden.ItemType) string {
	if t == "" {
		return "untyped"
	}
	return titleCase(string(t))
}

// draftStatus is the state column: a word and a symbol, with the reason kept
// for the inspector and the detail.
func draftStatus(d work.Draft) (string, lipgloss.Color) {
	p := theme.Current
	if d.Attention != "" {
		return "! attention", p.Danger
	}
	return "✎ draft", p.Warning
}

// gdHead is the title row of the inbox in the amber the Drafts use, with the
// filter chip on the right, followed by the feedback row.
func (m Model) gardenDraftsHead(width int) []string {
	p := theme.Current
	d := m.garden.drafts
	typeName := "Any"
	if d.query.Type != "" {
		typeName = typeWord(d.query.Type)
	}
	nLocal := len(d.locals())
	count := fmt.Sprintf("✎ DRAFT IDEAS / NOT YET VALIDATED  %d", len(d.collection.Records)+nLocal)
	if d.query.Active() {
		count += fmt.Sprintf(" filtered / %d", d.collection.Total+nLocal)
	}
	if width < 80 {
		count = fmt.Sprintf("✎ DRAFT IDEAS  %d", len(d.collection.Records)+nLocal)
	}
	chip := "Type: " + typeName
	left := seg(p.Warning, p.Background, true).Render(" " + count)
	right := seg(p.Muted, p.Raised, false).Render(" "+chip+" ") + seg("", p.Background, false).Render(" ")
	head := shellRow(width, p.Background, left, right)

	b := m.garden.board
	feedback, fg := "Drafts stay separate from approved work. Nothing here starts a process.", p.Muted
	switch {
	case b.snapshot.Source == gardensync.SourceStale:
		feedback, fg = "Refresh incomplete; showing the last good read.", p.Warning
	case d.query.Text != "":
		feedback = "Search: " + boardSafe(d.query.Text)
	}
	if g := m.garden.assist; g.sessions[m.editKeyFor(0)] != nil {
		feedback, fg = "AI idea in progress. Press i to resume it.", p.Warning
	}
	if len(d.pending) > 0 {
		feedback, fg = fmt.Sprintf("%d capture%s may already exist on GitHub. Press r to reconcile.", len(d.pending), plural(len(d.pending))), p.Warning
	}
	if d.privErr != nil {
		feedback, fg = "Private drafts could not be read; showing the last good list.", p.Warning
	}
	if d.notice != "" {
		feedback, fg = d.notice, p.Warning
	}
	if d.pendingErr != nil {
		feedback, fg = d.pendingErr.Message+" "+d.pendingErr.Next, p.Danger
	}
	if e := m.garden.err; e != nil {
		feedback, fg = e.Message+" Last good read retained.", p.Danger
	}
	if !b.loaded {
		feedback, fg = "Loading drafts…", p.Muted
		if m.garden.busy != busyWork {
			feedback = "No successful read yet; refresh to load drafts."
		}
	}
	if d.searching {
		d.search.Width = max(1, width-12)
		feedback, fg = "Search: "+d.search.View()+"  Enter apply / Esc cancel", p.Muted
	}
	return []string{head, boardLine(width, p.Background, fg, false, " "+feedback)}
}

func (m Model) gsDrafts(width, rows int) []string {
	switch m.garden.drafts.stage {
	case draftsDetail:
		return m.gsDraftDetail(width, rows)
	case draftsForm:
		return m.gsCaptureForm(width, rows)
	case draftsPreview:
		return m.gsCapturePreview(width, rows)
	case draftsResult:
		return m.gsCaptureResult(width, rows)
	case draftsEditor:
		return m.gsEditor(width, rows)
	case draftsSave:
		return m.gsSavePreview(width, rows)
	case draftsValidate:
		return m.gsValidate(width, rows)
	case draftsConflict:
		return m.gsConflict(width, rows)
	case draftsAssist:
		return m.gsAssist(width, rows)
	case draftsPrivate:
		return m.gsPrivate(width, rows)
	}
	return m.gsDraftList(width, rows)
}

func (m Model) gsDraftList(width, rows int) []string {
	p := theme.Current
	d := m.garden.drafts
	b := m.garden.board
	recs := d.collection.Records
	flat := m.gardenInboxRows()
	hasLocal := len(d.locals()) > 0
	inspector := width >= 120
	listW, inspW := width, 0
	if inspector {
		inspW = max(tableInspectorMin, width/3)
		listW = width - inspW - 3
	}
	issueW := 5
	for _, r := range recs {
		issueW = max(issueW, len(fmt.Sprint(r.Item.Issue.Number))+1)
	}
	if hasLocal {
		issueW = max(issueW, len("D-1A2B3C"))
	}
	cols := tableCols{issue: issueW, typ: 9, status: 12}
	if hasLocal && listW >= 90 {
		cols.status = 22
	}
	if listW < 64 {
		cols.status = 0
	}
	cols.title = max(12, listW-(2+cols.issue+tableGap*2+cols.typ+cols.status+tableGap*btoi(cols.status > 0)))
	capacity := gardenTableCapacity(rows)
	row := m.gardenInboxFlatIndex()
	scroll := tableScrollFor(d.scroll, row, capacity, len(flat))

	out := m.gardenDraftsHead(width)
	body := max(0, rows-3)
	region := make([]string, 0, body)
	head := [6]string{"ISSUE", "TITLE", "TYPE", "", "", "STATE"}
	if hasLocal {
		head[0] = "DRAFT"
	}
	muted := [6]lipgloss.Color{p.Muted, p.Muted, p.Muted, p.Muted, p.Muted, p.Muted}
	region = append(region, m.gardenTableLine(listW, p.Raised, cols, " ", p.Muted, head, muted, true))
	empty := ""
	switch {
	case (!b.loaded || b.snapshot.SuccessfulAt.IsZero()) && !hasLocal:
		empty = "(unknown until a successful read)"
	case len(flat) == 0 && d.query.Active():
		empty = "(no drafts match this filter)"
	case len(flat) == 0:
		empty = "(no draft ideas yet; press n to capture one)"
	}
	if empty != "" {
		region = append(region, boardLine(listW, p.Background, p.Muted, false, " "+empty))
	}
	for n := scroll; n < len(flat) && n < scroll+capacity; n++ {
		fr := flat[n]
		if fr.head != "" {
			region = append(region, boardLine(listW, p.Background, p.Warning, true, " "+fr.head))
			continue
		}
		selected := n == row
		bg, fg, marker := p.Surface, p.Text, " "
		if selected {
			bg, fg, marker = p.Raised, p.Warning, "▌"
			if m.agentGlyphsASCII() {
				marker = ">"
			}
		}
		if l := fr.local; l != nil {
			state, statusFg := localState(*l)
			if cols.status >= 22 {
				state += " · " + ago(l.UpdatedAt)
			}
			texts := [6]string{drafts.DraftLabel(l.ID), localTitle(*l), typeWord(l.Type), "", "", state}
			fgs := [6]lipgloss.Color{fg, fg, p.Warning, p.Muted, p.Muted, statusFg}
			region = append(region, m.gardenTableLine(listW, bg, cols, marker, p.Warning, texts, fgs, selected))
			continue
		}
		r := *fr.gh
		status, statusFg := draftStatus(r)
		texts := [6]string{fmt.Sprintf("#%d", r.Item.Issue.Number), boardTitle(r.Item.Issue.Title), typeWord(r.Item.Classification.Type), "", "", status}
		fgs := [6]lipgloss.Color{fg, fg, p.Warning, p.Muted, p.Muted, statusFg}
		region = append(region, m.gardenTableLine(listW, bg, cols, marker, p.Warning, texts, fgs, selected))
	}
	for len(region) < body {
		region = append(region, blankRow(listW, p.Background))
	}
	region = region[:body]
	if inspector {
		var side []string
		if l, ok := d.selectedLocalDraft(); ok {
			side = m.gardenLocalInspector(inspW, len(region), l)
		} else {
			side = m.gardenDraftInspector(inspW, len(region), recs, d.rowIndex())
		}
		sep := seg(p.Border, p.Background, false).Render(" " + m.chrome("│") + " ")
		for i := range region {
			region[i] += sep + side[i]
		}
	}
	out = append(out, region...)
	footer := "No rows"
	if n := len(d.locals()) + len(recs); n > 0 {
		first := scroll + 1
		footer = fmt.Sprintf("Rows %d–%d of %d", first, min(scroll+capacity, len(flat)), len(flat))
		if len(flat) != n {
			footer += fmt.Sprintf(" (%d drafts)", n)
		}
	}
	out = append(out, gText(width, p.Muted, false, footer))
	return m.settingsASCII(out[:rows])
}

func (m Model) gardenDraftInspector(w, n int, recs []work.Draft, row int) []string {
	p := theme.Current
	var lines []string
	line := func(bg, fg lipgloss.Color, bold bool, s string) {
		lines = append(lines, boardLine(w, bg, fg, bold, " "+s))
	}
	blank := func() { line(p.Background, p.Muted, false, "") }
	if row < 0 {
		line(p.Background, p.Muted, false, "No draft selected")
	} else {
		r := recs[row]
		line(p.Background, p.Text, true, fmt.Sprintf("#%d / %s", r.Item.Issue.Number, typeWord(r.Item.Classification.Type)))
		blank()
		title := strings.Split(ansi.Wrap(boardTitle(r.Item.Issue.Title), max(1, w-2), ""), "\n")
		for i := 0; i < min(3, len(title)); i++ {
			text := title[i]
			if i == 2 && len(title) > 3 {
				text = ansi.Truncate(text, max(1, w-3), "") + "…"
			}
			line(p.Background, p.Text, false, text)
		}
		blank()
		status, fg := draftStatus(r)
		line(p.Raised, fg, true, " "+status)
		if r.Attention != "" {
			for _, l := range strings.Split(ansi.Wrap(boardSafe(r.Attention), max(1, w-2), ""), "\n") {
				line(p.Background, p.Danger, false, l)
			}
		}
		blank()
		line(p.Background, p.Muted, true, "NEXT HUMAN GATE")
		gate := approve.Assess(r.Item.Issue, r.Item.Verification)
		if gate.CanValidate() {
			line(p.Background, p.Text, false, "Validate specification")
			line(p.Background, p.Muted, false, "(Enter to open, then v)")
		} else {
			line(p.Background, p.Text, false, "Resolve before validating")
			for _, b := range gate.Blockers[:min(len(gate.Blockers), 2)] {
				for _, l := range strings.Split(ansi.Wrap(boardSafe(b.Message), max(1, w-2), ""), "\n") {
					line(p.Background, p.Muted, false, l)
				}
			}
		}
		blank()
		line(p.Raised, p.Warning, true, " Enter open item")
	}
	for len(lines) < n {
		blank()
	}
	return lines[:n]
}

// Detail -----------------------------------------------------------------------

// gardenDraftDocument is the read-only text of a draft: the body the user owns
// and the current specification head, wrapped to width.
func (m Model) gardenDraftDocument(width int) []string {
	d := m.garden.drafts
	r, _, ok := d.collection.Find(d.opened)
	if !ok {
		return nil
	}
	p := theme.Current
	var text strings.Builder
	status, _ := draftStatus(r)
	fmt.Fprintf(&text, "#%d %s\n\n%s / %s\n", r.Item.Issue.Number, boardSafe(r.Item.Issue.Title), typeWord(r.Item.Classification.Type), status)
	if r.Attention != "" {
		fmt.Fprintf(&text, "Attention: %s\n", boardSafe(r.Attention))
	}
	body := strings.TrimSpace(boardSafe(garden.DisplayBody(r.Item.Issue.Body)))
	if body == "" {
		body = "The Issue body is empty."
	}
	text.WriteString("\nISSUE BODY\n" + body + "\n")
	heads := r.Item.Verification.Heads
	var specs []garden.Revision
	for _, h := range heads {
		if h.Kind == garden.RevisionSpec {
			specs = append(specs, h)
		}
	}
	switch {
	case len(specs) == 0:
		text.WriteString("\nSPECIFICATION\nNo specification revision yet. Refine it after capture.\n")
	default:
		for _, h := range specs {
			label := "CURRENT SPECIFICATION"
			if len(specs) > 1 {
				label = "COMPETING SPECIFICATION HEAD"
			}
			fmt.Fprintf(&text, "\n%s (revision %d, %s)\n%s\n", label, h.CommentID, shortHash(h.Hash), boardSafe(h.Content))
		}
	}
	text.WriteString("\n" + gateSummary(approve.Assess(r.Item.Issue, r.Item.Verification)))
	return m.settingsASCII(gWrap(width, p.Text, false, text.String()))
}

func shortHash(h string) string {
	if len(h) > 8 {
		return h[:8]
	}
	return h
}

func (m Model) gsDraftDetail(width, rows int) []string {
	p := theme.Current
	d := m.garden.drafts
	second := gText(width, p.Muted, false, m.gardenBoardStatus())
	if d.notice != "" {
		second = gText(width, p.Warning, false, d.notice)
	} else if ok, why := m.canEdit(); !ok {
		second = gText(width, p.Warning, false, why)
	}
	out := []string{gText(width, p.Warning, true, "‹ Drafts (Esc)   Draft document"), second}
	doc := m.gardenDraftDocument(width)
	avail := max(0, rows-4)
	start := max(0, min(d.docScroll, max(0, len(doc)-avail)))
	for i := 0; i < avail; i++ {
		if start+i < len(doc) {
			out = append(out, doc[start+i])
		} else {
			out = append(out, blankRow(width, p.Background))
		}
	}
	pos := "Empty"
	if len(doc) > 0 {
		pos = fmt.Sprintf("Lines %d–%d of %d", start+1, min(start+avail, len(doc)), len(doc))
	}
	out = append(out, gText(width, p.Muted, false, pos))
	out = append(out, m.gardenDraftActions(width))
	return m.settingsASCII(out[:rows])
}

// gateSummary is the gate-one section of the Issue document: the verified state, why it needs attention
// and every approval record retained for audit.
func gateSummary(a approve.Assessment) string {
	var b strings.Builder
	b.WriteString("SPECIFICATION GATE\nState: " + gateLabel(a.State) + "\n")
	if a.Reason != "" {
		b.WriteString("Attention: " + boardSafe(a.Reason) + "\n")
	}
	if a.Head != nil {
		fmt.Fprintf(&b, "Revision s%d, fingerprint %s\n", a.Ordinal, shortHash(a.Fingerprint))
	}
	for _, r := range a.Records {
		fmt.Fprintf(&b, "Approval %d by %s on %s for revision %s: %s\n", r.CommentID, boardSafe(orDefault(r.Author, "unknown")), r.At.UTC().Format("2006-01-02 15:04Z"), shortHash(r.RevisionHash), r.State)
	}
	if a.CanValidate() {
		b.WriteString("Press v to validate this specification.\n")
	}
	for _, bl := range a.Blockers {
		b.WriteString("Blocked: " + boardSafe(bl.Message) + "\n")
	}
	return b.String()
}

// gardenDraftActions is the action bar of the detail: Edit and Validate when Write access allows them,
// with the reason shown otherwise.
func (m Model) gardenDraftActions(width int) string {
	p := theme.Current
	edit := seg(p.Warning, p.Raised, true).Render(" e Edit specification ")
	if ok, _ := m.canEdit(); !ok {
		edit = seg(p.Dim, p.Raised, false).Render(" e Edit specification (unavailable) ")
	}
	ai := seg(p.Warning, p.Raised, true).Render(" i Elaborate with AI ")
	if ok, _ := m.canEdit(); !ok {
		ai = seg(p.Dim, p.Raised, false).Render(" i Elaborate with AI (unavailable) ")
	}
	validate := seg(p.Warning, p.Raised, true).Render(" v Validate specification ")
	if ok, _ := m.canValidate(); !ok {
		validate = seg(p.Dim, p.Raised, false).Render(" v Validate specification (unavailable) ")
	}
	gap := seg("", p.Background, false).Render("  ")
	line := seg("", p.Background, false).Render(" ") + edit + gap + ai + gap + validate
	return shellRow(width, p.Background, line, "")
}

// Capture ----------------------------------------------------------------------

// formLine draws one row of the form with a label column.
func formLabel(width int, focused bool, label string) string {
	p := theme.Current
	fg := p.Muted
	if focused {
		fg = p.Warning
	}
	return seg(fg, p.Background, focused).Render(padRight(label, width))
}

func (m Model) gsCaptureForm(width, rows int) []string {
	p := theme.Current
	d := m.garden.drafts
	f := d.form
	m.gardenSizeForm()
	fw := captureFieldWidth(width)
	labelW := 13
	row := func(label string, focus captureField, content string) string {
		marker := " "
		if f.focus == focus {
			marker = "›"
		}
		line := seg(p.Warning, p.Background, true).Render(marker) + formLabel(labelW, f.focus == focus, label) + content
		return shellRow(width, p.Background, line, "")
	}
	box := func(s string, focused bool) string {
		fg := p.Text
		if focused {
			fg = p.Warning
		}
		return seg(fg, p.Background, false).Render("[") + s + seg(fg, p.Background, false).Render("]")
	}
	var lines []string
	lines = append(lines, gText(width, p.Warning, true, "✎ A NEW IDEA / ALWAYS STARTS AS DRAFT"), blankRow(width, p.Background))
	lines = append(lines, row("Title", fieldTitle, box(padRight(ansi.Truncate(f.title.View(), fw-2, ""), fw-2), f.focus == fieldTitle)), blankRow(width, p.Background))
	typ := string(captureTypes[f.typ])
	typeCell := "[" + titleCase(typ) + " v]"
	if f.focus == fieldType {
		typeCell = "‹ " + titleCase(typ) + " ›"
	}
	lines = append(lines, row("Type", fieldType, seg(p.Text, p.Background, f.focus == fieldType).Render(typeCell)), blankRow(width, p.Background))
	descLines := strings.Split(f.desc.View(), "\n")
	for i, l := range descLines {
		label := ""
		if i == 0 {
			label = "Description"
		}
		marker := " "
		if f.focus == fieldDesc && i == 0 {
			marker = "›"
		}
		boxFg := p.Text
		if f.focus == fieldDesc {
			boxFg = p.Warning
		}
		edge := seg(boxFg, p.Background, false)
		l = ansi.Truncate(l, fw-2, "")
		l += strings.Repeat(" ", max(0, fw-2-ansi.StringWidth(l)))
		cell := seg(p.Warning, p.Background, true).Render(marker) + formLabel(labelW, f.focus == fieldDesc, label) + edge.Render("[") + l + edge.Render("]")
		lines = append(lines, shellRow(width, p.Background, cell, ""))
	}
	lines = append(lines, gText(width, p.Muted, false, "Optional. Refine the specification after capture."), blankRow(width, p.Background))
	if e := m.garden.err; e != nil {
		lines = append(lines, m.gErrorLines(width, e)...)
	}
	if f.discard {
		lines = append(lines, gText(width, p.Warning, true, "Discard this idea? Nothing has been created on GitHub."))
		keep, drop := gButton("Keep editing", d.btn == 0), gButton("Discard", d.btn == 1)
		lines = append(lines, gButtons(width, "  ", keep, drop))
	} else {
		lines = append(lines, gButtons(width, "", gButton("Create GitHub Issue…", f.focus == fieldPreview)))
	}
	// A small terminal drops the explanation first, never the field in use.
	if len(lines) > rows {
		lines = lines[:rows]
	}
	for len(lines) < rows {
		lines = append(lines, blankRow(width, p.Background))
	}
	return m.settingsASCII(lines)
}

func (m Model) gsCapturePreview(width, rows int) []string {
	p := theme.Current
	g := m.garden
	d := g.drafts
	if d.preview == nil {
		return gCompose(width, rows, []gblock{{lines: []string{gText(width, p.Muted, false, "Nothing to review.")}}}, nil)
	}
	pv := *d.preview
	desc := "none"
	if n := len(strings.Split(strings.TrimSpace(pv.Request.Description), "\n")); strings.TrimSpace(pv.Request.Description) != "" {
		desc = fmt.Sprintf("%d line%s", n, plural(n))
	}
	facts := []string{
		gField(width, 14, "Title", grun{boardTitle(pv.Request.Title), p.Text, true}),
		gField(width, 14, "Type", grun{typeWord(pv.Request.Type), p.Warning, false}),
		gField(width, 14, "Description", grun{desc, p.Muted, false}),
		gField(width, 14, "Labels", grun{strings.Join(pv.Labels, "  "), p.Muted, false}),
	}
	var action string
	create := "Create Draft Issue"
	if d.idea.active {
		create = "Create and save"
		if n := ideaSections(d, m); n > 0 {
			facts = append(facts, gField(width, 14, "Then", grun{fmt.Sprintf("append the first specification revision (%d section%s with text)", n, plural(n)), p.Text, false}))
		}
	}
	switch g.busy {
	case busyCapture:
		action = gText(width, p.Warning, true, "Creating the Issue… keys are ignored until GitHub answers.")
	default:
		action = gButtons(width, "                    ", gButton("Back to edit", d.btn == 0), gButton(create, d.btn == 1))
	}
	blocks := blocksWithNotices(m, width,
		gblock{lines: []string{gText(width, p.Warning, true, "✎ REVIEW BEFORE CREATING")}},
		gblock{prio: 1, lines: gWrap(width, p.Text, false, "Creates one GitHub Issue in "+orDefault(pv.Target.FullName, "the repository")+" at Draft"+map[bool]string{true: ", then saves your specification as its first revision (two writes, one confirmation)", false: ""}[d.idea.active]+".\nNothing is approved, planned or started.")},
		gSpaced(2, width, facts),
		gblock{lines: []string{action}},
		gblock{prio: 3, lines: []string{gText(width, p.Muted, false, "If GitHub does not answer, Bonsai checks before it ever creates this again.")}},
	)
	return m.settingsASCII(gCompose(width, rows, blocks, nil))
}

func (m Model) gsCaptureResult(width, rows int) []string {
	p := theme.Current
	g := m.garden
	d := g.drafts
	if d.result == nil {
		return gCompose(width, rows, []gblock{{lines: []string{gText(width, p.Muted, false, "No result.")}}}, nil)
	}
	v := *d.result
	var title, text string
	var buttons []string
	switch v.State {
	case gardenapi.CaptureUnknown:
		title = "! THE IDEA MAY ALREADY EXIST"
		text = "GitHub did not confirm the creation of \"" + boardTitle(v.Title) + "\".\nIt may exist. Bonsai will not create it again until you reconcile: that looks for it on GitHub and settles it."
		buttons = []string{gButton("Reconcile", d.btn == 0), gButton("Close", d.btn == 1)}
	default:
		title = "! NOTHING WAS CREATED"
		text = "GitHub has no Issue for \"" + boardTitle(v.Title) + "\", so retrying is safe. Your text is kept."
		buttons = []string{gButton("Back to edit", d.btn == 0), gButton("Retry", d.btn == 1)}
	}
	action := gButtons(width, "          ", buttons...)
	if g.busy == busyReconcile || g.busy == busyCapture {
		action = gText(width, p.Warning, true, "Working… keys are ignored until GitHub answers.")
	}
	blocks := []gblock{
		{lines: []string{gHeadChip(width, title, p.Warning)}},
		{prio: 1, lines: gWrap(width, p.Text, false, text)},
	}
	if v.Err != nil {
		blocks = append(blocks, gblock{prio: 2, lines: m.gErrorLines(width, v.Err)})
	}
	blocks = blocksWithNotices(m, width, append(blocks, gblock{lines: []string{action}})...)
	return m.settingsASCII(gCompose(width, rows, blocks, nil))
}
