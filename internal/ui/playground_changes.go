package ui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
	"github.com/Tiago-0liveira/bonsai/internal/ui/playgroundapi"
)

// The Changes screen of the Playground (plan 10.3). Four reads (changes, pull request, checks and one file's
// diff) each keep their own last good answer, age, in-flight flag and error. Only a successful reply replaces a
// call's data; a failure sets that call's error and leaves everything else alone. Reads start when the screen is
// entered and on r, never while moving through the list or the files.

// pgCall is the state of one kind of read.
type pgCall[T any] struct {
	// key is what the data is for: the session, or the session and file of a diff. A new key drops the data.
	key  string
	gen  uint64 // the newest request; an older reply is dropped
	gate playgroundapi.Gate
	val  T
	have bool
	// bad: val is itself an "unavailable" verdict (nothing better was ever read), not a last good answer.
	bad  bool
	at   time.Time // FetchedAt of val
	busy bool
	err  *playgroundapi.Error // the newest failure; cleared by the next success
}

// begin starts a request and returns its generation.
func (c *pgCall[T]) begin(key string) uint64 {
	if c.key != key {
		var zero T
		c.val, c.have, c.bad, c.at, c.err = zero, false, false, time.Time{}, nil
		c.gate.Reset()
		c.key = key
	}
	c.gen++
	c.busy = true
	return c.gen
}

// fail records a failed request. The last good data stays.
func (c *pgCall[T]) fail(key string, gen uint64, err error) bool {
	if key != c.key || gen != c.gen {
		return false
	}
	c.busy = false
	c.err = pgAPIError(err)
	return true
}

// ok records a reply. unavailable is set when the reply says the read did not complete: it then only replaces
// data that was never good. It reports whether the reply was for the newest request of this call.
func (c *pgCall[T]) ok(key string, gen uint64, st playgroundapi.Stamp, v T, unavailable *playgroundapi.Error) bool {
	if key != c.key || gen != c.gen {
		return false
	}
	c.busy = false
	if !c.gate.Accept(st) {
		return false
	}
	if unavailable != nil && c.have && !c.bad {
		c.err = unavailable
		return true
	}
	c.val, c.have, c.bad, c.at, c.err = v, true, unavailable != nil, st.FetchedAt, unavailable
	return true
}

// stale: there is last good data and the newest read failed.
func (c *pgCall[T]) stale() bool { return c.err != nil && c.have && !c.bad }

func pgAPIError(err error) *playgroundapi.Error {
	var e *playgroundapi.Error
	if errors.As(err, &e) && e != nil {
		return e
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &playgroundapi.Error{Code: playgroundapi.CodeTimeout}
	}
	return &playgroundapi.Error{Code: playgroundapi.CodeFailed}
}

// pgReasonText is the fixed wording of a code. Nothing from the backend's own message is shown.
func pgReasonText(c playgroundapi.Code) string {
	switch c {
	case playgroundapi.CodeUnavailable:
		return "backend unavailable"
	case playgroundapi.CodeTimeout:
		return "timed out"
	case playgroundapi.CodeStale:
		return "the list changed since it was read"
	case playgroundapi.CodeRateLimited:
		return "rate limited"
	case playgroundapi.CodeOffline:
		return "offline"
	case playgroundapi.CodeAuthRequired:
		return "GitHub sign-in required"
	case playgroundapi.CodePermission:
		return "permission denied"
	case playgroundapi.CodeNoGitHub:
		return "GitHub is not configured"
	case playgroundapi.CodeNoGitHubRemote:
		return "no GitHub remote"
	case playgroundapi.CodeNoBranch:
		return "no branch"
	case playgroundapi.CodeNotFound:
		return "not found"
	case playgroundapi.CodeSessionNotFound:
		return "session not found"
	case playgroundapi.CodeNotInRepository:
		return "session is outside this repository"
	case playgroundapi.CodeFileNotChanged:
		return "file is not among the changed files"
	case playgroundapi.CodeStore:
		return "link store unavailable"
	case playgroundapi.CodeGardenUnavailable:
		return "Garden unavailable"
	}
	return "read failed"
}

// pgReadMsg is a finished read. key says what it was for and gen which request; both are checked on arrival.
type pgReadMsg[T any] struct {
	key string
	gen uint64
	v   T
	err error
}

// pgLinkMsg is a finished LinkSession write.
type pgLinkMsg struct {
	session string
	issue   int64
	replace bool
	err     error
}

// pgChanges is the Changes screen. The four calls are independent.
type pgChanges struct {
	open    bool
	session string // the session the screen is for

	changes pgCall[playgroundapi.ChangesView]
	pr      pgCall[playgroundapi.PRView]
	checks  pgCall[playgroundapi.ChecksView]
	diff    pgCall[playgroundapi.FileDiffView]

	sel  string // selected file path: the cursor follows it across refreshes
	idx  int    // where it was, for the neighbor when it leaves
	top  int    // first file in view
	note string // result of the last link attempt

	diffOpen bool // the detail (the diff) has the keyboard
	hunk     int
	scroll   int // first diff row in view
}

// pgLinkFlow is an explicit link in progress: the session and Issue the user confirmed, and nothing else.
type pgLinkFlow struct {
	session string
	issue   int64
	busy    bool
}

func (m Model) pgCtx() context.Context {
	if m.agents.ctx != nil {
		return m.agents.ctx
	}
	return context.Background()
}

func pgDiffKey(session, path string) string { return session + "\x00" + path }

// pgChangesOpen enters the Changes screen of the selected session and reads it. It never touches the terminal.
func (m Model) pgChangesOpen() (tea.Model, tea.Cmd) {
	r, ok := m.playgroundSelected()
	switch {
	case m.pg.api == nil:
		m.status = "Changes are not available on this host"
		return m, nil
	case !ok || m.pg.sub == pgSubUsage:
		return m, nil
	}
	c := &m.pg.chg
	c.open, c.session, c.diffOpen, c.note = true, r.ID, false, ""
	cmd := m.pgChangesRead()
	return m, cmd
}

// pgChangesClose leaves the screen; the last good data stays for the next time.
func (m *Model) pgChangesClose() {
	m.pg.chg.open, m.pg.chg.diffOpen = false, false
}

// pgChangesRead requests changes, pull request and checks of the screen's session. The reads are independent and
// run together; a diff that is open is read again once the new file list lands, since it is bound to its generation.
func (m *Model) pgChangesRead() tea.Cmd {
	c := &m.pg.chg
	api, ctx, id := m.pg.api, m.pgCtx(), c.session
	if api == nil || id == "" {
		return nil
	}
	cg := c.changes.begin(id)
	pg := c.pr.begin(id)
	kg := c.checks.begin(id)
	return tea.Batch(
		func() tea.Msg {
			v, err := api.Changes(ctx, playgroundapi.ChangesRequest{SessionID: id})
			return pgReadMsg[playgroundapi.ChangesView]{key: id, gen: cg, v: v, err: err}
		},
		func() tea.Msg {
			v, err := api.PullRequest(ctx, playgroundapi.PRRequest{SessionID: id})
			return pgReadMsg[playgroundapi.PRView]{key: id, gen: pg, v: v, err: err}
		},
		func() tea.Msg {
			v, err := api.Checks(ctx, playgroundapi.ChecksRequest{SessionID: id})
			return pgReadMsg[playgroundapi.ChecksView]{key: id, gen: kg, v: v, err: err}
		},
	)
}

// pgDiffRead reads the diff of the selected file of the current file list.
func (m *Model) pgDiffRead() tea.Cmd {
	c := &m.pg.chg
	api, ctx, id := m.pg.api, m.pgCtx(), c.session
	if api == nil || !c.changes.have || c.changes.bad || c.sel == "" {
		return nil
	}
	req := playgroundapi.FileDiffRequest{SessionID: id, Path: c.sel, Base: c.changes.val.Base.Ref, Generation: c.changes.val.Stamp.Generation}
	key := pgDiffKey(id, c.sel)
	gen := c.diff.begin(key)
	return func() tea.Msg {
		v, err := api.FileDiff(ctx, req)
		return pgReadMsg[playgroundapi.FileDiffView]{key: key, gen: gen, v: v, err: err}
	}
}

// pgLinksRead reads the manual links once; rows use them to show their Issue.
func (m *Model) pgLinksRead() tea.Cmd {
	api, ctx := m.pg.api, m.pgCtx()
	if api == nil {
		return nil
	}
	gen := m.pg.links.begin("links")
	return func() tea.Msg {
		v, err := api.Links(ctx)
		return pgReadMsg[playgroundapi.LinksView]{key: "links", gen: gen, v: v, err: err}
	}
}

// pgChangesMsg applies a finished read. Replies of another session, an older request or an older backend
// generation are dropped; a failure only marks its own call.
func (m Model) pgChangesMsg(msg tea.Msg) (Model, tea.Cmd) {
	c := &m.pg.chg
	switch msg := msg.(type) {
	case pgReadMsg[playgroundapi.ChangesView]:
		if msg.err != nil {
			c.changes.fail(msg.key, msg.gen, msg.err)
			return m, nil
		}
		if msg.key != m.pg.sel || msg.v.SessionID != msg.key {
			return m, nil
		}
		var unavailable *playgroundapi.Error
		if msg.v.State == playgroundapi.WorktreeUnavailable {
			unavailable = msg.v.Err
			if unavailable == nil {
				unavailable = &playgroundapi.Error{Code: playgroundapi.CodeUnavailable}
			}
		}
		if !c.changes.ok(msg.key, msg.gen, msg.v.Stamp, msg.v, unavailable) {
			return m, nil
		}
		if unavailable != nil && c.changes.val.Stamp != msg.v.Stamp {
			return m, nil // the last good view stays
		}
		m.pgChangesReconcile()
		if c.open && c.diffOpen {
			cmd := m.pgDiffRead()
			return m, cmd
		}
	case pgReadMsg[playgroundapi.PRView]:
		if msg.err != nil {
			c.pr.fail(msg.key, msg.gen, msg.err)
			return m, nil
		}
		if msg.key != m.pg.sel || msg.v.SessionID != msg.key {
			return m, nil
		}
		var unavailable *playgroundapi.Error
		if msg.v.State == playgroundapi.PRUnavailable {
			unavailable = &playgroundapi.Error{Code: msg.v.Code}
		}
		c.pr.ok(msg.key, msg.gen, msg.v.Stamp, msg.v, unavailable)
	case pgReadMsg[playgroundapi.ChecksView]:
		if msg.err != nil {
			c.checks.fail(msg.key, msg.gen, msg.err)
			return m, nil
		}
		if msg.key != m.pg.sel || msg.v.SessionID != msg.key {
			return m, nil
		}
		var unavailable *playgroundapi.Error
		if msg.v.State == playgroundapi.ChecksUnavailable {
			unavailable = &playgroundapi.Error{Code: msg.v.Code}
		}
		c.checks.ok(msg.key, msg.gen, msg.v.Stamp, msg.v, unavailable)
	case pgReadMsg[playgroundapi.FileDiffView]:
		if msg.err != nil {
			c.diff.fail(msg.key, msg.gen, msg.err)
			return m, nil
		}
		if msg.key != pgDiffKey(m.pg.sel, c.sel) || msg.v.SessionID != m.pg.sel || msg.v.Path != c.sel {
			return m, nil
		}
		if c.diff.ok(msg.key, msg.gen, msg.v.Stamp, msg.v, nil) {
			m.pgDiffClamp()
		}
	case pgReadMsg[playgroundapi.LinksView]:
		p := &m.pg
		if msg.err != nil {
			p.links.fail(msg.key, msg.gen, msg.err)
			return m, nil
		}
		if p.links.ok(msg.key, msg.gen, msg.v.Stamp, msg.v, nil) {
			p.linkMap = map[string]playgroundapi.Link{}
			for _, l := range msg.v.Links {
				p.linkMap[l.SessionID] = l // oldest first: the newest link of a session wins
			}
			m.playgroundReconcile()
		}
	case pgLinkMsg:
		return m.pgLinkDone(msg)
	}
	return m, nil
}

// pgChangesReconcile keeps the file selection on its path; a file that left the list hands the cursor to the
// one now at its position, and closes a diff that was open on it.
func (m *Model) pgChangesReconcile() {
	c := &m.pg.chg
	files := c.changes.val.Files
	for i, f := range files {
		if f.Path == c.sel {
			c.idx = i
			c.top = pgChgTop(c.top, i, len(files), m.pgChgListRows())
			return
		}
	}
	if len(files) == 0 {
		c.sel, c.idx, c.top, c.diffOpen = "", 0, 0, false
		return
	}
	c.idx = max(0, min(c.idx, len(files)-1))
	if c.sel != "" {
		c.diffOpen = false // the file the diff showed is gone
	}
	c.sel = files[c.idx].Path
	c.top = pgChgTop(c.top, c.idx, len(files), m.pgChgListRows())
}

// pgChgTop keeps the selected file inside a window of avail rows, moving it as little as possible.
func pgChgTop(top, sel, n, avail int) int {
	avail = max(avail, 1)
	top = max(0, min(top, max(n-avail, 0)))
	if sel < top {
		top = sel
	}
	if sel >= top+avail {
		top = sel - avail + 1
	}
	return max(top, 0)
}

// --- keys ---

func (m Model) pgChangesKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	c := &m.pg.chg
	k := msg.String()
	if c.diffOpen {
		switch k {
		case "esc", "left", "h":
			c.diffOpen = false
		case "up", "k":
			m.pgDiffScroll(-1)
		case "down", "j":
			m.pgDiffScroll(1)
		case "pgup":
			m.pgDiffScroll(-m.pgChgDiffRows())
		case "pgdown", " ":
			m.pgDiffScroll(m.pgChgDiffRows())
		case "n", "]":
			m.pgDiffHunk(1)
		case "p", "[":
			m.pgDiffHunk(-1)
		case "r":
			cmd := m.pgChangesRead()
			return m, cmd
		}
		return m, nil
	}
	switch k {
	case "esc":
		m.pgChangesClose()
	case "up", "k":
		m.pgChangesMove(-1)
	case "down", "j":
		m.pgChangesMove(1)
	case "enter", "right", "l":
		return m.pgDiffOpen()
	case "r":
		cmd := m.pgChangesRead()
		return m, cmd
	case "L":
		return m.pgLinkStart(), nil
	}
	return m, nil
}

func (m *Model) pgChangesMove(d int) {
	c := &m.pg.chg
	files := c.changes.val.Files
	if len(files) == 0 {
		return
	}
	c.idx = max(0, min(c.idx+d, len(files)-1))
	c.sel = files[c.idx].Path
	c.top = pgChgTop(c.top, c.idx, len(files), m.pgChgListRows())
}

// pgDiffOpen opens the bounded diff of the selected file.
func (m Model) pgDiffOpen() (tea.Model, tea.Cmd) {
	c := &m.pg.chg
	if c.sel == "" || !c.changes.have || c.changes.bad {
		return m, nil
	}
	c.diffOpen, c.hunk, c.scroll = true, 0, 0
	cmd := m.pgDiffRead()
	return m, cmd
}

// pgDiffRows flattens the hunks into display rows: one header row per hunk, then its lines.
type pgDiffRow struct {
	hunk   int
	header bool
	note   bool // the closing row of a truncated diff
	text   string
	line   playgroundapi.DiffLine
}

func pgDiffFlatten(v playgroundapi.FileDiffView) (rows []pgDiffRow, starts []int) {
	for i, h := range v.Hunks {
		starts = append(starts, len(rows))
		rows = append(rows, pgDiffRow{hunk: i, header: true, text: h.Header})
		for _, l := range h.Lines {
			rows = append(rows, pgDiffRow{hunk: i, line: l})
		}
	}
	if v.Truncated {
		rows = append(rows, pgDiffRow{hunk: len(v.Hunks), note: true, text: "diff truncated here: the rest of this file is not shown"})
	}
	return rows, starts
}

// pgDiffReady: a diff of the selected file is loaded.
func (m Model) pgDiffReady() bool {
	c := m.pg.chg
	return c.diff.have && c.diff.key == pgDiffKey(c.session, c.sel)
}

func (m *Model) pgDiffClamp() {
	c := &m.pg.chg
	if !m.pgDiffReady() {
		return
	}
	rows, starts := pgDiffFlatten(c.diff.val)
	c.scroll = max(0, min(c.scroll, max(len(rows)-m.pgChgDiffRows(), 0)))
	c.hunk = pgHunkAt(starts, c.scroll)
}

func pgHunkAt(starts []int, top int) int {
	h := 0
	for i, s := range starts {
		if s <= top {
			h = i
		}
	}
	return h
}

func (m *Model) pgDiffScroll(d int) {
	c := &m.pg.chg
	if !m.pgDiffReady() {
		return
	}
	c.scroll += d
	m.pgDiffClamp()
}

// pgDiffHunk jumps to the next or previous hunk.
func (m *Model) pgDiffHunk(d int) {
	c := &m.pg.chg
	if !m.pgDiffReady() {
		return
	}
	rows, starts := pgDiffFlatten(c.diff.val)
	if len(starts) == 0 {
		return
	}
	h := max(0, min(c.hunk+d, len(starts)-1))
	c.hunk = h
	c.scroll = max(0, min(starts[h], max(len(rows)-m.pgChgDiffRows(), 0)))
}

// --- linking a session to an Issue ---

// pgLinkable: the session has no task and the changes that say so were read.
func (m Model) pgLinkable() bool {
	c := m.pg.chg
	return m.pg.api != nil && c.open && c.changes.have && !c.changes.bad && c.changes.val.Task.Source == playgroundapi.TaskNone && !m.pg.link.busy
}

// pgLinkStart asks for the Issue number in a small text dialog; while it is open the dialog owns every key.
func (m Model) pgLinkStart() Model {
	switch {
	case m.pg.link.busy:
		return m
	case !m.pg.chg.changes.have || m.pg.chg.changes.bad:
		m.status = "Read the changes first; the link depends on whether the session already has a task"
		return m
	case !m.pgLinkable():
		m.status = "This session already has a task; it cannot be linked again here"
		return m
	}
	m.pg.link = pgLinkFlow{session: m.pg.chg.session}
	return m.pgLinkAsk("Link to Issue")
}

func (m Model) pgLinkAsk(title string) Model {
	modal := modals.NewInput(modals.KindPlaygroundLinkIssue, title, "Issue number, for example 151")
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m
}

// pgLinkIssueEntered checks the typed number and asks for confirmation. Nothing is written yet.
func (m Model) pgLinkIssueEntered(value string) Model {
	n, err := strconv.ParseInt(strings.TrimPrefix(strings.TrimSpace(value), "#"), 10, 64)
	if err != nil || n <= 0 {
		return m.pgLinkAsk("Link to Issue: enter a positive whole number")
	}
	if m.pg.link.session == "" || m.pg.link.session != m.pg.sel || !m.pg.chg.open {
		m.pg.link = pgLinkFlow{}
		return m
	}
	m.pg.link.issue = n
	return m.pgLinkConfirm(false)
}

func (m Model) pgIssueTitle(n int64) string {
	for _, r := range m.garden.board.collection.Records {
		if r.Item.Issue.Number == n {
			return boardTitle(r.Item.Issue.Title)
		}
	}
	return ""
}

func (m Model) pgLinkConfirm(replace bool) Model {
	l := m.pg.link
	name := "session"
	if r, ok := m.playgroundSelected(); ok {
		name = sessionName(r.Rec.Session)
	}
	issue := fmt.Sprintf("#%d", l.issue)
	if t := m.pgIssueTitle(l.issue); t != "" {
		issue += " " + t
	}
	kind, title := modals.KindPlaygroundLinkConfirm, fmt.Sprintf("Link this session to Issue #%d?", l.issue)
	var lead string
	if replace {
		kind, title = modals.KindPlaygroundLinkReplace, fmt.Sprintf("Replace the link with Issue #%d?", l.issue)
		lead = "This session is already linked to another Issue"
		if cur, ok := m.pg.linkMap[l.session]; ok && cur.IssueNumber > 0 {
			lead = fmt.Sprintf("This session is already linked to Issue #%d", cur.IssueNumber)
		}
		lead += ". Confirming replaces that link.\n\n"
	}
	width := max(min(m.width-14, 92), 24)
	body := lead + "Session: " + pgClean(name, 80) + "\nIssue: " + pgClean(issue, 120) + "\n\n" +
		"Links this session to the Issue for display only. It creates no plan, approval or attempt."
	var wrapped []string
	for _, para := range strings.Split(body, "\n") {
		wrapped = append(wrapped, strings.Split(ansi.Wrap(para, width, ""), "\n")...)
	}
	modal := modals.NewConfirm(kind, title)
	modal.SetBody(strings.Join(wrapped, "\n"))
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m
}

// pgLinkConfirmed writes the link, after the user said yes to exactly what the dialog showed.
func (m Model) pgLinkConfirmed(replace bool) (Model, tea.Cmd) {
	l := m.pg.link
	if l.session == "" || l.issue <= 0 || m.pg.api == nil || l.session != m.pg.sel || !m.pg.chg.open {
		m.pg.link = pgLinkFlow{}
		m.status = "Nothing was linked: the session changed"
		return m, nil
	}
	m.pg.link.busy = true
	api, ctx := m.pg.api, m.pgCtx()
	req := playgroundapi.LinkRequest{SessionID: l.session, IssueNumber: l.issue, Replace: replace}
	return m, func() tea.Msg {
		_, err := api.LinkSession(ctx, req)
		return pgLinkMsg{session: req.SessionID, issue: req.IssueNumber, replace: replace, err: err}
	}
}

// pgLinkDone handles the result of LinkSession. Every code has its own wording and a failure writes nothing.
func (m Model) pgLinkDone(msg pgLinkMsg) (Model, tea.Cmd) {
	m.pg.link.busy = false
	c := &m.pg.chg
	say := func(s string) {
		if c.session == msg.session {
			c.note = s
		}
		m.status = s
	}
	if msg.err == nil {
		say(fmt.Sprintf("Linked to Issue #%d (display only: no plan, approval or attempt)", msg.issue))
		m.pg.link = pgLinkFlow{}
		links := m.pgLinksRead()
		var read tea.Cmd
		if c.open && c.session == msg.session {
			read = m.pgChangesRead()
		}
		return m, tea.Batch(links, read)
	}
	e := pgAPIError(msg.err)
	switch e.Code {
	case playgroundapi.CodeLinkConflict:
		// The only code with a next step: an explicit replace, confirmed on its own.
		if m.pg.link.session == msg.session && !msg.replace && m.pg.sel == msg.session {
			return m.pgLinkConfirm(true), nil
		}
		say("This session is already linked to another Issue. Nothing was changed.")
	case playgroundapi.CodeHasTask:
		say("This session already has a task from a launch or a link. Nothing was linked.")
		m.pg.link = pgLinkFlow{}
		var read tea.Cmd
		if c.open && c.session == msg.session {
			read = m.pgChangesRead()
		}
		return m, read
	case playgroundapi.CodeGardenUnavailable:
		say("Garden is not available, so the Issue could not be checked. Nothing was linked.")
	case playgroundapi.CodeIssueNotFound:
		say(fmt.Sprintf("Issue #%d was not found in this Garden. Nothing was linked.", msg.issue))
	case playgroundapi.CodeStore:
		say("The link store is unavailable. Nothing was linked.")
	default:
		say("The link could not be saved (" + pgReasonText(e.Code) + "). Nothing was linked.")
	}
	m.pg.link = pgLinkFlow{}
	return m, nil
}
