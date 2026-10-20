package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/planrev"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

const (
	planHeadRows  = 3 // title, facts and revisions above the notices and the document
	planTailRows  = 1 // position and search under it
	planNoticeMax = 6
	// planContext is how many unchanged lines frame a change in a comparison.
	planContext = 3
)

// gsPlan draws the plan screens of the opened Issue.
func (m Model) gsPlanRev(width, rows int) []string {
	switch m.garden.planrev.stage {
	case planValidate:
		return m.gsValidate(width, rows)
	case planCompose:
		return m.gsPlanCompose(width, rows)
	case planConfirm:
		return m.gsPlanConfirm(width, rows)
	case planDecide:
		return m.gsPlanDecide(width, rows)
	}
	return m.gsPlanReview(width, rows)
}

func (m Model) gardenPlanFocusLabel() string {
	switch m.garden.planrev.stage {
	case planValidate:
		return "Plan / Validate specification"
	case planCompose:
		return "Plan / Text"
	case planConfirm:
		return "Plan / Publish"
	case planDecide:
		return "Plan / Decision"
	}
	if m.garden.planrev.searching {
		return "Plan / Search"
	}
	return "Plan / Review"
}

func stateWords(s planrev.State) string {
	switch s {
	case planrev.StateChangesRequested:
		return "changes requested"
	case planrev.StateRejected:
		return "rejected"
	case planrev.StateApproved:
		return "approved"
	}
	return "awaiting decision"
}

func stateColor(s planrev.State) lipgloss.Color {
	p := theme.Current
	switch s {
	case planrev.StateRejected:
		return p.Danger
	case planrev.StateApproved:
		return p.Success
	case planrev.StateChangesRequested:
		return p.Warning
	}
	return p.Text
}

// revWord is how a revision is named on this screen.
func revWord(r *planrev.Revision) string {
	if r == nil {
		return "none"
	}
	return fmt.Sprintf("r%d", r.Ordinal)
}

// --- document rows ---

// planRows is the body of the review: the rows to scroll, where each section starts and which rows hold a search hit.
type planRows struct {
	rows   []string
	starts []int
	hits   []int
}

// planDocument lays the revision out as one scrolling document with a heading per section.
func (m Model) planDocument(r planrev.Revision, width int) planRows {
	p := theme.Current
	st := m.garden.planrev
	out := planRows{}
	found := map[[2]int]bool{}
	for _, h := range planrev.Find(r.Sections, st.query) {
		found[[2]int{h.Section, h.Line}] = true
	}
	for si, sec := range r.Sections {
		out.starts = append(out.starts, len(out.rows))
		out.rows = append(out.rows, gText(width, p.Accent, true, "§ "+boardTitle(sec.Title)))
		for li, para := range strings.Split(boardSafe(sec.Text), "\n") {
			hit := found[[2]int{si, li}]
			if hit {
				out.hits = append(out.hits, len(out.rows))
			}
			current := hit && st.hit == len(out.hits)-1
			for _, l := range strings.Split(ansi.Wrap(para, max(width-3, 1), ""), "\n") {
				fg := p.Text
				if hit {
					fg = p.Warning
				}
				out.rows = append(out.rows, gText(width, fg, current, "  "+l))
			}
		}
		out.rows = append(out.rows, blankRow(width, p.Background))
	}
	if len(out.starts) == 0 {
		out.starts = []int{0}
	}
	return out
}

// planComparison lays out the changes between two texts: unified, or two columns when wide and the width allows.
func (m Model) planComparison(oldText, newText string, width int, wide bool) []string {
	p := theme.Current
	lines := planrev.Collapse(planrev.Diff(oldText, newText), planContext)
	if len(lines) == 0 {
		return []string{gText(width, p.Muted, false, "The two revisions have the same text.")}
	}
	if wide && width >= planWideMin {
		return planSideBySide(width, lines)
	}
	var out []string
	for _, l := range lines {
		if l.Skipped > 0 {
			out = append(out, gText(width, p.Muted, false, fmt.Sprintf("  … %d unchanged %s", l.Skipped, lineWord(l.Skipped))))
			continue
		}
		fg, mark := p.Muted, "  "
		switch l.Op {
		case planrev.OpAdd:
			fg, mark = p.Success, "+ "
		case planrev.OpDel:
			fg, mark = p.Danger, "- "
		}
		wrapped := strings.Split(ansi.Wrap(boardSafe(l.Text), max(width-4, 1), ""), "\n")
		for i, w := range wrapped {
			prefix := mark
			if i > 0 {
				prefix = "  "
			}
			out = append(out, gText(width, fg, false, prefix+w))
		}
	}
	return out
}

func lineWord(n int) string {
	if n == 1 {
		return "line"
	}
	return "lines"
}

// planSideBySide pairs the removals of a change with the additions that replace them.
func planSideBySide(width int, lines []planrev.Line) []string {
	p := theme.Current
	col := (width - 4) / 2
	cell := func(s string, fg lipgloss.Color) string {
		return seg(fg, p.Background, false).Render(padRight(ansi.Truncate(boardSafe(s), col, "…"), col))
	}
	row := func(l, r string) string {
		return shellRow(width, p.Background, seg("", p.Background, false).Render(" ")+l+seg(p.Muted, p.Background, false).Render(" │ ")+r, "")
	}
	var out []string
	for i := 0; i < len(lines); {
		l := lines[i]
		switch {
		case l.Skipped > 0:
			out = append(out, gText(width, p.Muted, false, fmt.Sprintf("… %d unchanged %s", l.Skipped, lineWord(l.Skipped))))
			i++
		case l.Op == planrev.OpSame:
			out = append(out, row(cell(l.Text, p.Muted), cell(l.Text, p.Muted)))
			i++
		default:
			var dels, adds []string
			for ; i < len(lines) && lines[i].Op == planrev.OpDel; i++ {
				dels = append(dels, lines[i].Text)
			}
			for ; i < len(lines) && lines[i].Op == planrev.OpAdd; i++ {
				adds = append(adds, lines[i].Text)
			}
			for k := 0; k < max(len(dels), len(adds)); k++ {
				a, b := "", ""
				if k < len(dels) {
					a = dels[k]
				}
				if k < len(adds) {
					b = adds[k]
				}
				out = append(out, row(cell(a, p.Danger), cell(b, p.Success)))
			}
		}
	}
	return out
}

// planBody is what the review scrolls: the document, or the comparison with the previous revision.
func (m Model) planBody(r planrev.Revision, width int) planRows {
	st := m.garden.planrev
	if st.mode != planDoc {
		if prev, ok := st.previous(r); ok {
			return planRows{rows: m.planComparison(prev.Content, r.Content, width, st.mode == planWide), starts: []int{0}}
		}
	}
	return m.planDocument(r, width)
}

// planContentRows is how many rows the revision in view scrolls through.
func (m Model) planContentRows(r planrev.Revision) int {
	return len(m.planBody(r, max(m.width, 1)).rows)
}

// --- the review screen ---

func (m Model) gsPlanReview(width, rows int) []string {
	p := theme.Current
	st := m.garden.planrev
	if st.view == nil {
		return m.settingsASCII(gCompose(width, rows, blocksWithNotices(m, width, gblock{lines: []string{gText(width, p.Muted, false, "Reading the plan revisions and verifying them…")}}), nil))
	}
	rv := st.view.Review
	r, have := st.current()
	title := fmt.Sprintf("◇ PLAN REVIEW / #%d", rv.Number)
	if have {
		title = fmt.Sprintf("◇ PLAN REVIEW / r%d / specification s%d", r.Ordinal, rv.Gate.Ordinal)
	}
	if rv.Ready {
		title += " / READY"
	}
	if rv.StaleApproval {
		title = "! APPROVAL STALE / " + title
	}
	out := []string{gHead(width, title, m.planModeLabel(r, have)), m.planFacts(width, rv), m.planStrip(width, rv, r, have)}
	notices := m.planNotices(width, rv, r, have)
	out = append(out, notices...)

	bodyRows := max(rows-planHeadRows-len(notices)-planTailRows, 0)
	var body []string
	if !have {
		body = gCompose(width, bodyRows, []gblock{{lines: gWrap(width, p.Muted, false,
			"No plan revision has been published for this Issue.\n\nRequest a read-only plan with p, let the planner answer in its session, then press u on that attempt to publish the plan you chose. Nothing is published unless you do.")}}, nil)
	} else {
		pr := m.planBody(r, width)
		start := max(0, min(st.scroll, max(len(pr.rows)-bodyRows, 0)))
		for i := 0; i < bodyRows; i++ {
			if start+i < len(pr.rows) {
				body = append(body, pr.rows[start+i])
			} else {
				body = append(body, blankRow(width, p.Background))
			}
		}
	}
	out = append(out, body...)
	out = append(out, m.planTail(width, r, have))
	for len(out) < rows {
		out = append(out, blankRow(width, p.Background))
	}
	return m.settingsASCII(out[:rows])
}

func (m Model) planModeLabel(r planrev.Revision, have bool) string {
	st := m.garden.planrev
	switch {
	case !have || st.mode == planDoc:
		return "document"
	case st.mode == planWide:
		return fmt.Sprintf("r%d ↔ r%d side by side", r.Ordinal-1, r.Ordinal)
	}
	return fmt.Sprintf("r%d → r%d unified", r.Ordinal-1, r.Ordinal)
}

// planFacts shows the current, latest and approved revisions as three separate facts: they are three different
// questions, and a decision names a revision, never "whichever is latest".
func (m Model) planFacts(width int, rv planrev.Review) string {
	p := theme.Current
	cur := grun{"none", p.Muted, false}
	if rv.Current != nil {
		cur = grun{fmt.Sprintf("r%d %s", rv.Current.Ordinal, stateWords(rv.Current.State)), stateColor(rv.Current.State), true}
	}
	latest := grun{revWord(rv.Latest), p.Text, false}
	if rv.Latest != nil && rv.Latest.Stale {
		latest.text += " (older specification)"
	}
	approved := grun{revWord(rv.Approved), p.Muted, false}
	if rv.Approved != nil {
		approved = grun{revWord(rv.Approved), p.Success, true}
	}
	return gRuns(width, p.Background, grun{"Current ", p.Muted, false}, cur, grun{" · Latest ", p.Muted, false}, latest, grun{" · Approved ", p.Muted, false}, approved)
}

// planStrip lists every revision with its state, the one in view marked.
func (m Model) planStrip(width int, rv planrev.Review, shown planrev.Revision, have bool) string {
	p := theme.Current
	if len(rv.Revisions) == 0 {
		return gText(width, p.Muted, false, "No revisions yet.")
	}
	var runs []grun
	for i, r := range rv.Revisions {
		mark := " "
		if have && r.Hash == shown.Hash {
			mark = "▸"
		}
		note := stateWords(r.State)
		switch {
		case r.State == planrev.StateAwaiting && r.Superseded:
			note = "superseded"
		case !r.Trusted:
			note = "other author"
		case !r.Reviewable():
			note = "unverifiable"
		case r.Stale:
			note += ", older spec"
		}
		fg := p.Muted
		if have && r.Hash == shown.Hash {
			fg = p.Text
		}
		sep := "  "
		if i == 0 {
			sep = ""
		}
		runs = append(runs, grun{sep + mark + fmt.Sprintf("r%d %s", r.Ordinal, note), fg, have && r.Hash == shown.Hash})
	}
	return gRuns(width, p.Background, runs...)
}

// planNotices is up to three rows: why something is unavailable, what a decision said, and what GitHub shows that
// Bonsai cannot read as a record. A comment written elsewhere is a hint here, never an instruction.
func (m Model) planNotices(width int, rv planrev.Review, r planrev.Revision, have bool) []string {
	p := theme.Current
	g := m.garden
	parts := []string{"Launch unavailable (plan 09)."}
	fg := p.Warning
	if g.err != nil {
		parts = append(parts, "! "+g.err.Message+" "+g.err.Next)
		fg = p.Danger
	}
	if g.notice != "" {
		parts = append(parts, "! "+g.notice)
	}
	audit := rv.Approved
	if have && r.State == planrev.StateApproved {
		audit = &r
	}
	if audit != nil {
		a := audit
		for i := len(a.Decisions) - 1; i >= 0; i-- {
			d := a.Decisions[i]
			if d.Revoked || d.Decision != garden.DecisionApproved {
				continue
			}
			source := d.Source
			if source == "" {
				source = "external"
			}
			parts = append(parts, fmt.Sprintf("Approved r%d %s by %s (%s); verified %s. Recorded %s.", a.Ordinal, shortHash(a.Hash), boardTitle(d.Author), source, rv.VerifiedAt.UTC().Format("2006-01-02 15:04:05Z"), d.CreatedAt.UTC().Format("2006-01-02 15:04:05Z")))
			break
		}
	}
	if rv.StaleApproval {
		parts = append(parts, "Approval stale: the approved snapshot is retained; current plan/spec pair differs or cannot verify. Downstream disabled.")
	}
	if rv.Unavailable != "" {
		parts = append(parts, "! "+rv.Unavailable)
	}
	if have {
		switch {
		case !r.Trusted:
			parts = append(parts, "This revision was written by another account: it is shown, never reviewable.")
		case !r.Consistent || len(r.Missing) > 0 || r.BaseCommit == "":
			parts = append(parts, "This revision does not verify as a complete plan (inspected base, source specification and every required section): it cannot be decided.")
		case r.Stale:
			parts = append(parts, "This plan answers an older specification.")
		case len(r.Decisions) > 0 && r.Decisions[len(r.Decisions)-1].Revoked:
			parts = append(parts, fmt.Sprintf("The latest decision on r%d was revoked; review it again before approving.", r.Ordinal))
		case r.State == planrev.StateChangesRequested:
			parts = append(parts, fmt.Sprintf("Changes requested on r%d: %s A new run needs your confirmation: press p.", r.Ordinal, reasonText(r.Reason())))
		case r.State == planrev.StateRejected:
			parts = append(parts, fmt.Sprintf("r%d was rejected: no plan is accepted and the Issue stays Approved. Publish a new revision to continue.", r.Ordinal))
		}
	}
	if rv.Competing {
		parts = append(parts, "Several revisions compete: a newly published one supersedes them all.")
	}
	if rv.NewerComments > 0 {
		parts = append(parts, fmt.Sprintf("Newer on GitHub: %d %s that Bonsai cannot read as records. They change nothing here; read them on the Issue.", rv.NewerComments, commentWord(rv.NewerComments)))
	}
	if len(parts) == 0 {
		return nil
	}
	lines := gWrap(width, fg, false, strings.Join(parts, " "))
	if len(lines) > planNoticeMax {
		lines = lines[:planNoticeMax]
		lines[planNoticeMax-1] = gText(width, fg, false, "…")
	}
	return lines
}

func commentWord(n int) string {
	if n == 1 {
		return "comment"
	}
	return "comments"
}

func reasonText(s string) string {
	if s == "" {
		return "(no reason recorded)."
	}
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return boardTitle(s)
}

// planTail is the row under the document: where the view is, and the search in progress.
func (m Model) planTail(width int, r planrev.Revision, have bool) string {
	p := theme.Current
	st := m.garden.planrev
	if st.searching {
		return shellRow(width, p.Background, ansi.Truncate(" Search: "+st.si.View(), width, ""), "")
	}
	if !have {
		return gText(width, p.Muted, false, "p planner")
	}
	pr := m.planBody(r, width)
	rows := m.planBodyRows()
	text := fmt.Sprintf("rows %d-%d of %d", min(st.scroll+1, len(pr.rows)), min(st.scroll+rows, len(pr.rows)), len(pr.rows))
	if st.mode == planDoc {
		sec := 0
		for i, s := range pr.starts {
			if s <= st.scroll {
				sec = i
			}
		}
		if sec < len(r.Sections) {
			text = "§ " + boardTitle(r.Sections[sec].Title) + " · " + text
		}
	}
	if q := strings.TrimSpace(st.query); q != "" {
		text += fmt.Sprintf(" · search %q: ", q)
		switch {
		case st.mode != planDoc:
			text += "document only"
		case len(pr.hits) == 0:
			text += "no match"
		default:
			text += fmt.Sprintf("%d/%d (n/N)", st.hit+1, len(pr.hits))
		}
	}
	if !st.searching {
		if st.query == "" && width >= 100 {
			text += " · launch unavailable (plan 09)"
		}
	}
	return gText(width, p.Muted, false, text)
}

// --- compose, confirm, decide ---

func (m Model) gsPlanCompose(width, rows int) []string {
	p := theme.Current
	st := m.garden.planrev
	n := int64(0)
	if st.view != nil {
		n = st.view.Review.Number
	} else if pv := m.garden.planner.preview; pv != nil {
		n = pv.Number
	}
	out := []string{gHead(width, fmt.Sprintf("▣ PUBLISH PLAN FOR #%d / attempt %s", n, strings.TrimPrefix(st.attempt, "plan-")), "text you chose")}
	out = append(out, gWrap(width, p.Muted, false, "Bonsai adds the source specification and the inspected base from the attempt. It reads nothing from the planner's terminal.")...)
	lines := strings.Split(st.ta.View(), "\n")
	room := max(rows-len(out)-1, 0)
	for i := 0; i < room; i++ {
		l := ""
		if i < len(lines) {
			l = lines[i]
		}
		out = append(out, shellRow(width, p.Background, ansi.Truncate(" "+l, width, ""), ""))
	}
	note := fmt.Sprintf("%d lines · ctrl+s previews exactly what would be published · esc keeps this draft in memory", max(st.ta.LineCount(), 1))
	if m.garden.err != nil {
		note = "! " + m.garden.err.Message + " " + m.garden.err.Next
	} else if m.garden.notice != "" {
		note = "! " + m.garden.notice
	}
	out = append(out, gText(width, p.Muted, false, note))
	for len(out) < rows {
		out = append(out, blankRow(width, p.Background))
	}
	return m.settingsASCII(out[:rows])
}

func (m Model) gsPlanConfirm(width, rows int) []string {
	p := theme.Current
	st := m.garden.planrev
	v := st.preview
	if v == nil {
		return gCompose(width, rows, []gblock{{lines: []string{gText(width, p.Muted, false, "Nothing to publish.")}}}, nil)
	}
	pv := v.Preview
	facts := []string{
		gField(width, 16, "Revision", grun{fmt.Sprintf("r%d, revision %s", pv.Ordinal, shortHash(pv.Hash)), p.Text, true}),
		gField(width, 16, "Specification", grun{fmt.Sprintf("s%d / fingerprint %s…", pv.Spec.Ordinal, shortHash(pv.Spec.Fingerprint)), p.Muted, false}),
		gField(width, 16, "Inspected base", grun{shortCommit(pv.Base.Commit) + branchText(pv.Base.Branch), p.Muted, false}),
		gField(width, 16, "Supersedes", grun{supersedesText(pv), p.Muted, false}),
		gField(width, 16, "Author", grun{v.Actor + " (GitHub records it; a prose claim is never proof)", p.Muted, false}),
	}
	blocks := []gblock{{lines: []string{gHeadChip(width, fmt.Sprintf("◇ PUBLISH r%d FOR #%d", pv.Ordinal, v.Number), p.Accent)}}, gSpaced(2, width, facts)}
	if pv.Replay != 0 {
		blocks = append(blocks, gblock{prio: 1, lines: gWrap(width, p.Warning, false, "This exact plan is already published: confirming finds it and writes nothing.")})
	}
	for _, b := range pv.Blockers {
		blocks = append(blocks, gblock{prio: 1, lines: gWrap(width, p.Danger, true, "! "+b+" Go back and complete the text.")})
	}
	var diff []string
	if pv.Against != nil {
		diff = m.planComparison(pv.Against.Content, pv.Content, width, false)
		added, removed := planrev.Changed(planrev.Diff(pv.Against.Content, pv.Content))
		blocks = append(blocks, gblock{prio: 3, min: 3, want: len(diff) + 1, flex: func(avail int) []string {
			head := gText(width, p.Text, true, fmt.Sprintf("Changes since r%d: %d added, %d removed", pv.Against.Ordinal, added, removed))
			start := max(0, min(st.scroll, max(len(diff)-(avail-1), 0)))
			return append([]string{head}, diff[start:min(start+max(avail-1, 0), len(diff))]...)
		}})
	} else {
		blocks = append(blocks, gblock{prio: 3, lines: gWrap(width, p.Muted, false, fmt.Sprintf("First revision: %d sections, %d lines. Nothing to compare it with.", len(pv.Sections), strings.Count(pv.Content, "\n")+1))})
	}
	blocks = blocksWithNotices(m, width, blocks...)
	buttons := []string{gButton("Back to edit", st.btn == 0)}
	if v.CanPublish() {
		buttons = append(buttons, gButton(fmt.Sprintf("Publish r%d", pv.Ordinal), st.btn == 1))
	}
	actions := []string{gButtons(width, "  ", buttons...), gText(width, p.Muted, false, "Publishing appends one comment. It starts no run and approves nothing.")}
	if m.garden.busy == busyPlanPublish {
		actions[1] = gText(width, p.Warning, true, "Publishing… keys are ignored until GitHub answers.")
	}
	return m.settingsASCII(gCompose(width, rows, blocks, actions))
}

func branchText(b string) string {
	if b == "" {
		return " (detached)"
	}
	return " on " + b
}

func supersedesText(pv planrev.PublishPreview) string {
	if len(pv.Parents) == 0 {
		return "nothing: this is the first revision"
	}
	var names []string
	for _, h := range pv.Parents {
		names = append(names, shortHash(h))
	}
	return strings.Join(names, ", ")
}

func (m Model) gsPlanDecide(width, rows int) []string {
	p := theme.Current
	st := m.garden.planrev
	r, ok := st.current()
	if !ok || st.view == nil {
		return gCompose(width, rows, []gblock{{lines: []string{gText(width, p.Muted, false, "Nothing to decide.")}}}, nil)
	}
	if st.decision == garden.DecisionApproved {
		blocks := []gblock{{lines: []string{gHeadChip(width, fmt.Sprintf("◇ APPROVE PLAN r%d?", r.Ordinal), p.Warning)}}}
		facts := fmt.Sprintf("Plan revision: %s\nSource spec fingerprint: %s\nSpecification head: %s\nActor: %s (GitHub ID %s)", r.Hash, st.view.Review.Gate.Fingerprint, st.view.Review.Gate.Head.Hash, boardTitle(st.view.Actor), st.view.ActorID)
		blocks = append(blocks, gblock{lines: gWrap(width, p.Text, false, facts)}, gblock{prio: 1, lines: gWrap(width, p.Muted, false, "Records approval of this exact pair → Ready. No agent, worktree or PR starts. Both heads and your authority are verified again before writing. Launch unavailable (plan 09).")})
		blocks = blocksWithNotices(m, width, blocks...)
		actions := []string{gButtons(width, "  ", gButton("Cancel", st.btn == 0), gButton(fmt.Sprintf("Approve plan r%d", r.Ordinal), st.btn == 1))}
		return m.settingsASCII(gCompose(width, rows, blocks, actions))
	}
	head, consequence := "REQUEST CHANGES ON", "Records your reason on the Issue and keeps this revision in the history. It starts nothing: another read-only run needs your own confirmation from the planner screen, even if a comment elsewhere asked for it."
	if st.decision == garden.DecisionRejected {
		head, consequence = "REJECT", "Records that no plan is accepted. The Issue stays Approved and every revision stays in the history. It starts nothing."
	}
	facts := []string{
		gField(width, 16, "Revision", grun{fmt.Sprintf("r%d, revision %s", r.Ordinal, shortHash(r.Hash)), p.Text, true}),
		gField(width, 16, "Specification", grun{fmt.Sprintf("s%d / fingerprint %s…", st.view.Review.Gate.Ordinal, shortHash(r.SourceSpecFingerprint)), p.Muted, false}),
		gField(width, 16, "Decided by", grun{st.view.Actor, p.Muted, false}),
	}
	label := "Reason"
	if st.decision == garden.DecisionRejected {
		label = "Reason (optional)"
	}
	input := shellRow(width, p.Background, ansi.Truncate(" "+label+": "+st.reason.View(), width, ""), "")
	blocks := []gblock{{lines: []string{gHeadChip(width, fmt.Sprintf("◇ %s r%d", head, r.Ordinal), p.Warning)}}, gSpaced(2, width, facts),
		{lines: []string{input}}, {prio: 1, lines: gWrap(width, p.Muted, false, consequence)}}
	blocks = blocksWithNotices(m, width, blocks...)
	actions := []string{gText(width, p.Muted, false, "Enter records the decision · Esc cancels")}
	if m.garden.busy == busyPlanDecide {
		actions[0] = gText(width, p.Warning, true, "Recording… keys are ignored until GitHub answers.")
	}
	return m.settingsASCII(gCompose(width, rows, blocks, actions))
}
