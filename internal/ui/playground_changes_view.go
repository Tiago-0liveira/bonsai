package ui

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/ui/playgroundapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// pgChgFixed are the summary rows that are always drawn: header, facts, chips and the CI line.
const pgChgFixed = 4

// pgG picks a glyph; the ASCII preference gets a one-cell stand-in.
func (m Model) pgG(unicode, ascii string) string {
	if m.agentGlyphsASCII() {
		return ascii
	}
	return unicode
}

// pgSince is how long ago a read landed, in the units a person reads at a glance.
func pgSince(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := max(agentNow().Sub(t), 0)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d/time.Hour))
	}
	return fmt.Sprintf("%dd ago", int(d/(24*time.Hour)))
}

// pgSafe makes diff and path text safe to draw: no escapes or control bytes, spacing kept.
func pgSafe(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, ansi.Strip(s))
}

// pgShortSHA is the first seven characters of a commit ID, with no ellipsis: an object name is never abbreviated
// with one.
func pgShortSHA(s string) string {
	s = strings.Join(strings.Fields(pgSafe(s)), "")
	if len(s) > 7 {
		s = s[:7]
	}
	return s
}

func pgShort(c playgroundapi.Commit) string {
	if c.Short != "" {
		return pgShortSHA(c.Short)
	}
	return pgShortSHA(c.SHA)
}

// --- geometry ---

func (m Model) pgChgBodyRows() int {
	return max(m.height-shellHeaderRows-shellFooterRows-settingsSubmenuRows, 1)
}

// pgChgListRows is how many files the list shows.
func (m Model) pgChgListRows() int {
	return max(m.pgChgBodyRows()-len(m.pgChgSummary(m.width, m.pgChgBodyRows()))-1, 1)
}

// pgChgDiffRows is how many diff rows are shown under the two head rows of the diff.
func (m Model) pgChgDiffRows() int {
	rows := m.pgChgBodyRows()
	if m.pgWide() {
		rows -= len(m.pgChgSummary(m.width, rows))
	}
	return max(rows-2, 1)
}

// --- the screen ---

// pgChangesLines draws exactly rows lines of width cells (the caller pads and cuts).
func (m Model) pgChangesLines(width, rows int) []string {
	p := theme.Current
	c := m.pg.chg
	if !m.pgWide() {
		if c.diffOpen {
			return m.pgChgDiffLines(width, rows)
		}
		out := m.pgChgSummary(width, rows)
		out = append(out, m.pgChgFiles(width, max(rows-len(out), 1), true)...)
		return out
	}
	out := m.pgChgSummary(width, rows)
	avail := max(rows-len(out), 1)
	left := pgListWidth(width)
	list := m.pgChgFiles(left, avail, false)
	var right []string
	if c.diffOpen {
		right = m.pgChgDiffLines(width-left-1, avail)
	} else {
		right = m.pgChgDetail(width-left-1, avail)
	}
	div := seg(p.Border, p.Background, false).Render("│")
	for i := 0; i < avail; i++ {
		out = append(out, list[i]+div+right[i])
	}
	return out
}

type pgNotice struct {
	text string
	fg   lipgloss.Color
}

// pgChgSummary is the header, the facts, the totals and PR chips, the CI line and the notices. Every row says what
// it knows and what it does not: a failed read is named next to the last good data of the same call.
func (m Model) pgChgSummary(width, rows int) []string {
	p := theme.Current
	c := m.pg.chg
	cv := c.changes.val
	// Header.
	head := []grun{{m.pgG("▤", "=") + " CHANGES", p.Text, true}}
	switch {
	case !c.changes.have && c.changes.busy:
		head = append(head, grun{" / reading…", p.Muted, false})
	case !c.changes.have && c.changes.err != nil:
		head = append(head, grun{" / unavailable (" + pgReasonText(c.changes.err.Code) + ")", p.Warning, false})
	case !c.changes.have:
		head = append(head, grun{" / not read", p.Muted, false})
	default:
		branch := pgClean(cv.Branch, 60)
		switch {
		case cv.State == playgroundapi.WorktreeUnavailable:
			branch = "(unknown)"
		case cv.State == playgroundapi.BranchMissing:
			branch = "(branch missing)"
		case cv.State == playgroundapi.WorktreeMissing:
			branch = "(worktree missing)"
		case branch == "":
			branch = "(detached HEAD)"
		}
		head = append(head, grun{" / " + branch, p.Text, true})
		if cv.State != playgroundapi.WorktreeMissing && cv.State != playgroundapi.WorktreeUnavailable {
			if cv.Base.Source == playgroundapi.BaseNone {
				head = append(head, grun{" / no base — showing uncommitted changes only", p.Warning, false})
			} else {
				head = append(head, grun{" / base: " + pgClean(cv.Base.Ref, 60) + " (" + string(cv.Base.Source) + ")", p.Muted, false})
			}
		}
	}
	// Facts.
	var facts []string
	if c.changes.have && !c.changes.bad {
		if h := pgShort(cv.Head); h != "" {
			facts = append(facts, "HEAD "+h)
		}
		if cv.Base.MergeBase != "" {
			facts = append(facts, "merge-base "+pgShortSHA(cv.Base.MergeBase))
		}
		if cv.CommitsErr == nil {
			facts = append(facts, fmt.Sprintf("%d of %d commits", len(cv.Commits), cv.CommitsTotal))
		}
		age := "synced " + pgSince(c.changes.at)
		if c.changes.stale() {
			age = "last good " + pgSince(c.changes.at)
		}
		facts = append(facts, age)
	}
	factsLine := gText(width, p.Muted, false, strings.Join(facts, " · "))

	out := []string{gRuns(width, p.Background, head...), factsLine, m.pgChgChips(width), m.pgChgCI(width)}

	// Notices, worst first.
	notices := m.pgChgNotices()
	room := max(rows-pgChgFixed-4, 0)
	if len(notices) > room {
		hidden := len(notices) - max(room-1, 0)
		notices = notices[:max(room-1, 0)]
		if room > 0 {
			notices = append(notices, pgNotice{fmt.Sprintf("+%d more notices; widen or enlarge the terminal", hidden), p.Muted})
		}
	}
	for _, n := range notices {
		out = append(out, gText(width, n.fg, false, n.text))
	}
	return out
}

func (m Model) pgChgNotices() []pgNotice {
	p := theme.Current
	c := m.pg.chg
	cv := c.changes.val
	var out []pgNotice
	add := func(fg lipgloss.Color, format string, a ...any) {
		out = append(out, pgNotice{fmt.Sprintf(format, a...), fg})
	}
	if c.note != "" {
		add(p.Text, "%s", c.note)
	}
	if c.changes.have {
		switch cv.State {
		case playgroundapi.WorktreeMissing:
			add(p.Warning, "Worktree is missing (verified): its directory is gone, so there are no files or commits to show.")
		case playgroundapi.BranchMissing:
			add(p.Warning, "Branch is missing (verified): the worktree exists but its branch ref is gone.")
		case playgroundapi.WorktreeDetached:
			add(p.Muted, "Detached HEAD: files and commits are read; no pull request can be looked up.")
		case playgroundapi.WorktreeUnavailable:
			reason := playgroundapi.CodeUnavailable
			if cv.Err != nil {
				reason = cv.Err.Code
			}
			add(p.Warning, "Worktree could not be read (%s): nothing is known about it.", pgReasonText(reason))
		}
		if c.changes.stale() {
			add(p.Warning, "stale: worktree read failed (%s); showing the last good changes, read %s", pgReasonText(c.changes.err.Code), pgSince(c.changes.at))
		}
		if cv.FilesErr != nil {
			add(p.Warning, "Changed files could not be read (%s): the list is unknown.", pgReasonText(cv.FilesErr.Code))
		}
		if cv.FilesTrunc {
			add(p.Warning, "File list truncated: showing %d of %d files; totals are complete.", len(cv.Files), cv.Totals.Files)
		}
		if cv.CommitsErr != nil {
			add(p.Warning, "Commits could not be read (%s).", pgReasonText(cv.CommitsErr.Code))
		}
		if cv.Base.Note != "" {
			add(p.Muted, "%s", pgClean(cv.Base.Note, 160))
		}
		switch {
		case cv.LinkErr != nil:
			add(p.Warning, "Task link could not be read (%s).", pgReasonText(cv.LinkErr.Code))
		case cv.State == playgroundapi.WorktreeUnavailable:
		case cv.Task.Source == playgroundapi.TaskNone:
			add(p.Muted, "No Garden task (a neutral fact, not a failure). L links this session to an Issue.")
		case cv.Task.Source == playgroundapi.TaskManual:
			add(p.Muted, "Task: Issue %s, linked manually. No approved plan.", pgIssueNum(cv.Task.IssueNumber))
		default:
			add(p.Muted, "Task: Issue %s, launched attempt (the approved plan is on the Issue).", pgIssueNum(cv.Task.IssueNumber))
		}
	}
	if c.pr.stale() {
		add(p.Warning, "stale: PR lookup failed (%s); showing the last read, %s", pgReasonText(c.pr.err.Code), pgSince(c.pr.at))
	}
	if c.pr.have && c.pr.val.State == playgroundapi.PRFound {
		pr := c.pr.val.PR
		if c.pr.val.DetailErr != nil {
			add(p.Warning, "PR details unavailable (%s): mergeability and reviews are unknown.", pgReasonText(c.pr.val.DetailErr.Code))
		}
		if pr != nil && !pr.HeadMatchesLocal {
			add(p.Warning, "PR head differs from local HEAD.")
		}
	}
	if c.checks.stale() {
		add(p.Warning, "stale: CI lookup failed (%s); showing the last read, %s", pgReasonText(c.checks.err.Code), pgSince(c.checks.at))
	}
	if k := c.checks.val; c.checks.have && !c.checks.bad {
		if k.HeadSHA != "" && !k.IsLocal {
			add(p.Warning, "Checks are for an older commit (%s); local HEAD is %s.", pgShortSHA(k.HeadSHA), pgShortSHA(k.LocalHead))
		}
		if k.Truncated {
			add(p.Muted, "Check list truncated; the counts are complete.")
		}
	}
	return out
}

func pgIssueNum(n int64) string {
	if n <= 0 {
		return "(number unknown)"
	}
	return fmt.Sprintf("#%d", n)
}

// pgChgChips is the totals chip and the pull request chip.
func (m Model) pgChgChips(width int) string {
	p := theme.Current
	c := m.pg.chg
	chip := func(fg lipgloss.Color, s string) string { return seg(fg, p.Raised, false).Render(" " + s + " ") }
	gap := seg("", p.Background, false).Render("  ")

	totals := chip(p.Muted, "files: unknown")
	if c.changes.have && !c.changes.bad {
		t := c.changes.val.Totals
		noun := "files"
		if t.Files == 1 {
			noun = "file"
		}
		totals = chip(p.Text, fmt.Sprintf("%d %s / +%d -%d", t.Files, noun, t.Additions, t.Deletions))
	}

	var pr string
	pv := c.pr.val
	switch {
	case !c.pr.have && c.pr.busy:
		pr = chip(p.Muted, "PR: reading…")
	case !c.pr.have && c.pr.err != nil:
		pr = chip(p.Warning, "PR unavailable: "+pgReasonText(c.pr.err.Code))
	case !c.pr.have:
		pr = chip(p.Muted, "PR: not read")
	case pv.State == playgroundapi.PRNone:
		pr = chip(p.Muted, "No pull request (verified)")
	case pv.State == playgroundapi.PRUnavailable:
		pr = chip(p.Warning, "PR unavailable: "+pgReasonText(pv.Code))
	case pv.PR != nil:
		label, fg := "Open", p.Success
		switch {
		case pv.PR.Status == playgroundapi.PRMerged:
			label, fg = "Merged", p.Accent
		case pv.PR.Status == playgroundapi.PRClosed:
			label, fg = "Closed", p.Danger
		case pv.PR.Draft:
			label, fg = "Draft", p.Warning
		}
		pr = chip(fg, fmt.Sprintf("PR #%d / %s", pv.PR.Number, label))
		if pv.PR.Mergeable == playgroundapi.MergeConflicting {
			pr += gap + chip(p.Danger, "conflicts")
		}
	}
	return shellRow(width, p.Background, seg("", p.Background, false).Render(" ")+totals+gap+pr, "")
}

// pgChgCI is the CI line. It reads only the checks call: not the pull request, not a later review.
func (m Model) pgChgCI(width int) string {
	p := theme.Current
	k := m.pg.chg.checks
	switch {
	case !k.have && k.busy:
		return gText(width, p.Muted, false, "CI: reading…")
	case !k.have && k.err != nil:
		return gText(width, p.Warning, false, "CI unavailable: "+pgReasonText(k.err.Code)+" (not a pass or a failure)")
	case !k.have:
		return gText(width, p.Muted, false, "CI: not read")
	}
	v := k.val
	switch v.State {
	case playgroundapi.ChecksUnavailable:
		return gText(width, p.Warning, false, "CI unavailable: "+pgReasonText(v.Code)+" (not a pass or a failure)")
	case playgroundapi.ChecksNone:
		return gText(width, p.Muted, false, "No checks reported for this commit (verified)")
	}
	var runs []grun
	add := func(fg lipgloss.Color, s string) {
		if len(runs) > 0 {
			s = "   " + s
		}
		runs = append(runs, grun{s, fg, false})
	}
	if n := v.Counts.Passed; n > 0 {
		add(p.Success, fmt.Sprintf("%s %d %s passed", m.pgG("✓", "+"), n, pgPlural(n, "check", "checks")))
	}
	if n := v.Counts.Failed; n > 0 {
		s := fmt.Sprintf("! %d failed", n)
		var names []string
		for _, ch := range v.Checks {
			if ch.Result == playgroundapi.ChecksFailed {
				names = append(names, pgClean(ch.Name, 30))
			}
		}
		if len(names) > 2 {
			names = append(names[:2], fmt.Sprintf("+%d", len(names)-2))
		}
		if len(names) > 0 {
			s += ": " + strings.Join(names, ", ")
		}
		add(p.Danger, s)
	}
	if n := v.Counts.Pending; n > 0 {
		add(p.Warning, fmt.Sprintf("%s %d pending", m.pgG("◷", "o"), n))
	}
	if len(runs) == 0 {
		return gText(width, p.Muted, false, "CI: no check counts reported")
	}
	return gRuns(width, p.Background, runs...)
}

func pgPlural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// --- files ---

func pgStatusLetter(f playgroundapi.FileChange) string {
	switch f.Status {
	case playgroundapi.FileAdded:
		return "A"
	case playgroundapi.FileDeleted:
		return "D"
	case playgroundapi.FileRenamed:
		return "R"
	}
	return "M"
}

func pgStatusWord(f playgroundapi.FileChange) string {
	if f.Status == "" {
		return "modified"
	}
	return string(f.Status)
}

func pgCounts(f playgroundapi.FileChange) string {
	switch {
	case f.Binary:
		return "binary"
	case !f.Counted:
		return "uncounted"
	case f.Additions == 0 && f.Deletions == 0:
		return "+0 -0"
	case f.Deletions == 0:
		return fmt.Sprintf("+%d", f.Additions)
	case f.Additions == 0:
		return fmt.Sprintf("-%d", f.Deletions)
	}
	return fmt.Sprintf("+%d -%d", f.Additions, f.Deletions)
}

// pgTags are the markers of a file; words when there is room, letters otherwise.
func pgTags(f playgroundapi.FileChange, words bool) string {
	var t []string
	add := func(on bool, word, letter string) {
		if !on {
			return
		}
		if words {
			t = append(t, word)
		} else {
			t = append(t, letter)
		}
	}
	add(f.Uncommitted, "uncommitted", "U")
	add(f.Untracked, "untracked", "?")
	add(f.Binary, "binary", "B")
	add(!f.Counted && !f.Binary, "uncounted", "~")
	return strings.Join(t, " ")
}

// pgChgFiles draws the file list: a header row and the visible window of files, padded to avail+1 rows. In the
// narrow layout the commits fill the rows the files leave free.
func (m Model) pgChgFiles(w, avail int, commits bool) []string {
	p := theme.Current
	c := m.pg.chg
	cv := c.changes.val
	blank := blankRow(w, p.Background)
	rows := max(avail-1, 0)
	var out []string
	title := "FILES"
	if c.changes.have && !c.changes.bad && cv.FilesErr == nil {
		title = fmt.Sprintf("FILES %d", len(cv.Files))
		if cv.FilesTrunc {
			title = fmt.Sprintf("FILES %d of %d (list truncated)", len(cv.Files), cv.Totals.Files)
		}
	}
	out = append(out, gText(w, p.Text, true, title))
	empty := ""
	switch {
	case !c.changes.have && c.changes.busy:
		empty = "Reading changes…"
	case !c.changes.have:
		empty = "No changes read yet."
	case cv.State == playgroundapi.WorktreeMissing:
		empty = "No files: the worktree is missing."
	case cv.State == playgroundapi.WorktreeUnavailable:
		empty = "No files known: the worktree could not be read."
	case cv.FilesErr != nil:
		empty = "The file list is unknown."
	case len(cv.Files) == 0 && cv.Base.Source == playgroundapi.BaseNone:
		empty = "No uncommitted changes (verified)."
	case len(cv.Files) == 0:
		empty = "No changed files (verified)."
	}
	if empty != "" {
		out = append(out, gText(w, p.Muted, false, empty))
	}
	if empty == "" || len(cv.Files) > 0 {
		top := pgChgTop(c.top, c.idx, len(cv.Files), rows)
		for i := top; i < len(cv.Files) && len(out) < rows+1; i++ {
			out = append(out, m.pgChgFileRow(w, cv.Files[i], cv.Files[i].Path == c.sel))
		}
	}
	if commits && len(out) < avail-2 && cv.CommitsErr == nil && len(cv.Commits) > 0 {
		out = append(out, blank)
		out = append(out, m.pgChgCommits(w, avail-len(out))...)
	}
	for len(out) < avail {
		out = append(out, blank)
	}
	return out[:avail]
}

func (m Model) pgChgFileRow(w int, f playgroundapi.FileChange, selected bool) string {
	p := theme.Current
	bg, mark := p.Background, "  "
	fg := p.Text
	if selected {
		bg, mark = p.Raised, m.pgG("▌", ">")+" "
	}
	counts := pgCounts(f)
	words := w >= 70
	tagW := 5 // the widest letter markers, "U ? ~"
	if words {
		tagW = 11 // "uncommitted"
	}
	tags := pgTags(f, words)
	// The tag column has a fixed width so the counts line up from row to row.
	pw := w - 1 - 2 - 2 - 1 - 11 - 1 - tagW
	if pw < 12 {
		tags = ""
		pw = w - 1 - 2 - 2 - 1 - 11
	}
	pw = max(pw, 4)
	path := ansi.Truncate(pgSafe(f.Path), pw, "…")
	pad := strings.Repeat(" ", max(pw-ansi.StringWidth(path), 0))
	statusFg := p.Muted
	switch f.Status {
	case playgroundapi.FileAdded:
		statusFg = p.Success
	case playgroundapi.FileDeleted:
		statusFg = p.Danger
	}
	runs := []grun{
		{mark, p.Accent, true},
		{pgStatusLetter(f) + " ", statusFg, true},
		{path + pad + " ", fg, selected},
		{fmt.Sprintf("%-11s", counts), p.Muted, false},
	}
	if tags != "" {
		runs = append(runs, grun{" " + tags, p.Warning, false})
	}
	return gRuns(w, bg, runs...)
}

func (m Model) pgChgCommits(w, rows int) []string {
	p := theme.Current
	cv := m.pg.chg.changes.val
	if rows < 1 {
		return nil
	}
	title := fmt.Sprintf("COMMITS %d of %d", len(cv.Commits), cv.CommitsTotal)
	if cv.CommitsTrunc {
		title += " (newest first, list truncated)"
	}
	out := []string{gText(w, p.Text, true, title)}
	for _, cm := range cv.Commits {
		if len(out) >= rows {
			break
		}
		who := ""
		if cm.Author != "" {
			who = "  " + pgClean(cm.Author, 24)
		}
		out = append(out, gRuns(w, p.Background, grun{pgShort(cm) + " ", p.Accent, false}, grun{pgClean(cm.Subject, 200), p.Text, false}, grun{who, p.Dim, false}))
	}
	return out
}

// --- the wide detail without a diff ---

func (m Model) pgChgDetail(w, rows int) []string {
	p := theme.Current
	c := m.pg.chg
	blank := blankRow(w, p.Background)
	var out []string
	if f, ok := m.pgChgSelectedFile(); ok {
		out = append(out, gText(w, p.Text, true, pgSafe(f.Path)))
		line := pgStatusWord(f)
		if f.OldPath != "" {
			line += ", was " + pgSafe(f.OldPath)
		}
		out = append(out, gText(w, p.Muted, false, line+" / "+pgCounts(f)))
		if t := pgTags(f, true); t != "" {
			out = append(out, gText(w, p.Warning, false, t))
		}
		out = append(out, gText(w, p.Accent, false, "Enter opens the bounded diff"), blank)
	} else {
		out = append(out, gText(w, p.Muted, false, "No file selected."), blank)
	}
	if c.pr.have && c.pr.val.State == playgroundapi.PRFound && c.pr.val.PR != nil {
		pr := c.pr.val.PR
		out = append(out, gText(w, p.Text, true, "PR #"+fmt.Sprint(pr.Number)+" "+pgClean(pr.Title, 120)))
		out = append(out, gText(w, p.Muted, false, pgClean(pr.Head, 50)+" -> "+pgClean(pr.Base, 50)))
		if pr.ReviewKnown {
			out = append(out, gText(w, p.Muted, false, fmt.Sprintf("approvals %d / changes requested %d / review requests %d", pr.Approvals, pr.ChangesRequested, pr.RequestedReviews)))
		} else {
			out = append(out, gText(w, p.Muted, false, "reviews unknown"))
		}
		out = append(out, blank)
	}
	if k := c.checks; k.have && !k.bad && len(k.val.Checks) > 0 {
		out = append(out, gText(w, p.Text, true, fmt.Sprintf("CHECKS %d", len(k.val.Checks))))
		for i, ch := range k.val.Checks {
			if i >= 6 {
				out = append(out, gText(w, p.Muted, false, fmt.Sprintf("+%d more", len(k.val.Checks)-i)))
				break
			}
			glyph, fg := m.pgG("◷", "o"), p.Warning
			switch ch.Result {
			case playgroundapi.ChecksPassed:
				glyph, fg = m.pgG("✓", "+"), p.Success
			case playgroundapi.ChecksFailed:
				glyph, fg = "!", p.Danger
			}
			out = append(out, gRuns(w, p.Background, grun{glyph + " ", fg, false}, grun{pgClean(ch.Name, 80), p.Text, false}))
		}
		out = append(out, blank)
	}
	if c.changes.have && !c.changes.bad && c.changes.val.CommitsErr == nil && len(c.changes.val.Commits) > 0 {
		out = append(out, m.pgChgCommits(w, rows-len(out))...)
	}
	for len(out) < rows {
		out = append(out, blank)
	}
	return out[:rows]
}

func (m Model) pgChgSelectedFile() (playgroundapi.FileChange, bool) {
	c := m.pg.chg
	for _, f := range c.changes.val.Files {
		if f.Path == c.sel {
			return f, c.changes.have
		}
	}
	return playgroundapi.FileChange{}, false
}

// --- the diff ---

// pgChgDiffLines draws the diff of the selected file: two head rows and the visible rows.
func (m Model) pgChgDiffLines(w, rows int) []string {
	p := theme.Current
	c := m.pg.chg
	blank := blankRow(w, p.Background)
	f, _ := m.pgChgSelectedFile()
	path := pgSafe(c.sel)
	if f.Path == "" && path == "" {
		path = "(no file)"
	}
	head1 := []grun{{path, p.Text, true}, {"  " + pgStatusWord(f), p.Muted, false}, {"  " + pgCounts(f), p.Muted, false}}
	var out []string
	var body []string
	ready := m.pgDiffReady()
	var info []string
	switch {
	case ready:
		v := c.diff.val
		allRows, starts := pgDiffFlatten(v)
		// What matters when the width runs out goes first: a failed read and a cut-off diff.
		if c.diff.stale() {
			info = append(info, "stale: diff read failed ("+pgReasonText(c.diff.err.Code)+")")
		}
		if v.Truncated {
			switch v.TruncReason {
			case playgroundapi.TruncBytes:
				info = append(info, fmt.Sprintf("TRUNCATED at %d KiB", playgroundapi.MaxDiffBytes>>10))
			default:
				info = append(info, fmt.Sprintf("TRUNCATED at %d lines", playgroundapi.MaxDiffLines))
			}
		}
		if len(starts) > 0 {
			info = append(info, fmt.Sprintf("hunk %d/%d", min(c.hunk+1, len(starts)), len(starts)))
		}
		if len(allRows) > 0 {
			first := min(c.scroll, len(allRows)-1)
			last := min(first+max(rows-2, 1), len(allRows))
			info = append(info, fmt.Sprintf("rows %d-%d of %d", first+1, last, len(allRows)))
		}
		info = append(info, "read "+pgSince(c.diff.at))
		body = m.pgChgDiffBody(w, rows-2, v, allRows)
	case c.diff.busy:
		body = []string{gText(w, p.Muted, false, "Reading the diff…")}
	case c.diff.err != nil:
		info = append(info, "diff unavailable ("+pgReasonText(c.diff.err.Code)+")")
		if c.diff.err.Code == playgroundapi.CodeStale {
			body = []string{gText(w, p.Warning, false, "The file list moved since it was read. Press r to refresh.")}
		} else {
			body = []string{gText(w, p.Warning, false, "The diff could not be read. Press r to try again.")}
		}
	default:
		body = []string{gText(w, p.Muted, false, "Press Enter on a file to read its diff.")}
	}
	infoFg := p.Muted
	if ready && (c.diff.val.Truncated || c.diff.stale()) {
		infoFg = p.Warning
	}
	if len(info) > 0 && !ready {
		infoFg = p.Warning
	}
	out = append(out, gRuns(w, p.Background, head1...), gText(w, infoFg, false, strings.Join(info, " · ")))
	out = append(out, body...)
	for len(out) < rows {
		out = append(out, blank)
	}
	return out[:rows]
}

func (m Model) pgChgDiffBody(w, rows int, v playgroundapi.FileDiffView, all []pgDiffRow) []string {
	p := theme.Current
	c := m.pg.chg
	switch {
	case v.Binary:
		return []string{gText(w, p.Muted, false, "Binary file: the content changed but there is no text diff.")}
	case v.Empty:
		return []string{gText(w, p.Muted, false, "No textual change (for example only the file mode changed).")}
	case len(all) == 0:
		return []string{gText(w, p.Muted, false, "The diff has no hunks.")}
	}
	numbers := w >= 48
	top := max(0, min(c.scroll, len(all)-1))
	var out []string
	for i := top; i < len(all) && len(out) < rows; i++ {
		r := all[i]
		if r.note {
			out = append(out, gText(w, p.Warning, true, m.pgG("…", "~")+" "+r.text))
			continue
		}
		if r.header {
			mark := "  "
			bold := false
			if r.hunk == c.hunk {
				mark, bold = m.pgG("▸", ">")+" ", true
			}
			out = append(out, gRuns(w, p.Background, grun{mark + pgSafe(r.text), p.Accent, bold}))
			continue
		}
		fg, sign := p.Muted, " "
		switch r.line.Kind {
		case playgroundapi.DiffAdd:
			fg, sign = p.Success, "+"
		case playgroundapi.DiffDel:
			fg, sign = p.Danger, "-"
		}
		gutter := ""
		if numbers {
			num := func(n int) string {
				if n <= 0 {
					return "    "
				}
				return fmt.Sprintf("%4d", n)
			}
			gutter = num(r.line.Old) + " " + num(r.line.New) + " "
		}
		out = append(out, gRuns(w, p.Background, grun{gutter, p.Dim, false}, grun{sign + " " + pgSafe(r.line.Text), fg, false}))
	}
	return out
}

// --- chrome hooks ---

// pgChgCrumb names the session in the breadcrumb: its Issue when known, else its name.
func (m Model) pgChgCrumb() string {
	c := m.pg.chg
	if n := c.changes.val.Task.IssueNumber; c.changes.have && n > 0 {
		return fmt.Sprintf("#%d", n)
	}
	if r, ok := m.playgroundSelected(); ok {
		if r.Number > 0 {
			return fmt.Sprintf("#%d", r.Number)
		}
		return pgClean(sessionName(r.Rec.Session), 28)
	}
	return "Session"
}

func (m Model) pgChangesHints() []shellHint {
	c := m.pg.chg
	esc := shellHint{"esc", "sessions"}
	if c.diffOpen {
		return []shellHint{{"↑/↓", "scroll"}, {"n/p", "hunk"}, {"r", "refresh"}, {"esc", "files"}}
	}
	h := []shellHint{{"↑/↓", "file"}, {"enter", "diff"}, {"r", "refresh"}}
	if m.pgLinkable() {
		h = append(h, shellHint{"L", "link to Issue"})
	}
	return append(h, esc)
}

func (m Model) pgChangesStatus() string {
	c := m.pg.chg
	switch {
	case c.changes.busy || c.pr.busy || c.checks.busy:
		return "Reading changes…"
	case c.changes.have:
		return "Changes " + pgSince(c.changes.at)
	}
	return "Changes / local machine"
}
