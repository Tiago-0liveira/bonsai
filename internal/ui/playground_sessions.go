package ui

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/work"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/playgroundapi"
)

// The Playground session model is pure: it takes lists and already-loaded Garden
// data and returns rows. Anything that touches the filesystem happens in the
// command that builds the input (see playground.go), never here.

// pgSub is a Playground submenu entry.
type pgSub int

const (
	pgSubSessions pgSub = iota
	pgSubAttention
	pgSubHistory
	pgSubUsage
	pgSubCount
)

var pgSubNames = [pgSubCount]string{"Sessions", "Attention", "History", "Usage"}

// pgTree is one worktree of this repository with its path already resolved
// through symlinks.
type pgTree struct {
	ID, Path, Resolved, Branch string
}

// pgRec is a session that belongs to this repository, with the worktree facts
// resolved when the list was read.
type pgRec struct {
	Session agentsapi.Session
	Branch  string // branch of the matching worktree
	InTree  bool   // WorkDir or WorktreeID matched a worktree of this repository
}

// pgScope keeps the sessions that belong to this repository: those recorded
// under its project ID, plus those that run in (or are recorded for) one of its
// worktrees, such as a Garden launch worktree. Another repository's session is
// never kept, and a branch name alone never matches. resolve maps a directory
// to its real path. The result is in a stable order (creation time, then ID)
// with every session once.
func pgScope(all []agentsapi.Session, project string, trees []pgTree, resolve func(string) string) []pgRec {
	seen := map[string]bool{}
	var out []pgRec
	for _, s := range all {
		if seen[s.ID] {
			continue
		}
		dir := ""
		if s.WorkDir != "" {
			dir = resolve(s.WorkDir)
		}
		rec := pgRec{Session: s}
		for _, t := range trees {
			if (dir != "" && dir == t.Resolved) || (s.WorktreeID != "" && s.WorktreeID == t.ID) {
				rec.Branch, rec.InTree = t.Branch, true
				break
			}
		}
		if s.ProjectID != project && !rec.InTree {
			continue
		}
		seen[s.ID] = true
		out = append(out, rec)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Session, out[j].Session
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.Before(b.CreatedAt)
		}
		return a.ID < b.ID
	})
	return out
}

// pgFact is what a session is doing as separate facts: lifecycle and activity
// never merge into a guess. Key is a stable style identifier.
type pgFact struct {
	Key, Label string
	// Attention is set for a session that waits for the user, or one that failed
	// and has not been acknowledged. It marks a row; it never moves it.
	Attention bool
}

// pgFactOf derives the display facts from the provider's own lifecycle and
// activity. Silence is not failure, an idle turn is not a finished task, and a
// running session without reported activity says so instead of guessing.
func pgFactOf(s agentsapi.Session, acked bool) pgFact {
	key, label := agentactivity.Display(s.State, s.Activity)
	f := pgFact{Key: key, Label: label}
	switch s.State {
	case "exited":
		if s.ExitCode != nil {
			f.Label = fmt.Sprintf("Exited (code %d)", *s.ExitCode)
		}
	case "failed":
		reason := pgClean(s.Error, 120)
		if reason == "" {
			reason = "unknown error"
		}
		f.Label = "Failed: " + reason
		f.Attention = !acked
	case "starting", "stopping":
	default:
		// Display reports "running" for missing and unrecognized activity.
		if key == "running" {
			f.Key, f.Label = "unknown", "Running · activity unknown"
			break
		}
		if s.Activity != nil && s.Activity.Inferred {
			f.Label += " (inferred)"
		}
		if key == "waiting" {
			f.Attention = true
		}
	}
	return f
}

// pgClean strips escapes and control bytes from provider free text.
func pgClean(s string, limit int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, ansi.Strip(s))
	return ansi.Truncate(strings.Join(strings.Fields(s), " "), limit, "…")
}

// pgTask resolves a session's task link against the Garden collection that is
// already loaded. ok is false when the link is absent or the Issue is not in
// that collection; nothing is fetched.
func pgTask(c work.Collection, t *agents.TaskRef) (rec work.Record, ok bool) {
	if t == nil || t.IssueID == 0 {
		return work.Record{}, false
	}
	for _, r := range c.Records {
		if uint64(r.Item.Issue.ID) == t.IssueID && (t.RepositoryID == 0 || uint64(r.Item.Issue.Repo) == t.RepositoryID) {
			return r, true
		}
	}
	return work.Record{}, false
}

// pgRow is one session as the list shows it.
type pgRow struct {
	ID      string
	Rec     pgRec
	Title   string
	Linked  bool            // the session has a Task link
	Manual  bool            // the link was made by hand: it carries no plan or attempt
	Ref     *agents.TaskRef // the effective link: the launch record, else the manual one
	Issue   uint64          // the linked Issue's ID, 0 when unlinked
	Number  int64           // the Issue number when it is known
	Task    bool            // the Issue resolved in the loaded Garden collection
	Fact    pgFact
	Account string
	Group   string // grouping key: "issue:<id>" or "" for unlinked sessions
}

func pgAccount(s agentsapi.Session) string {
	if n := pgClean(s.ProfileName, 40); n != "" {
		return n
	}
	return pgClean(string(s.AccountID), 40)
}

func pgRowOf(r pgRec, c work.Collection, ack map[string]bool, links map[string]playgroundapi.Link) pgRow {
	s := r.Session
	row := pgRow{ID: s.ID, Rec: r, Fact: pgFactOf(s, ack[s.ID]), Account: pgAccount(s)}
	ref := s.Task
	// A launch record wins over a manual link: it knows the plan and the attempt.
	if l, ok := links[s.ID]; ok && ref == nil && l.IssueID != 0 {
		ref = &agents.TaskRef{IssueID: l.IssueID}
		row.Manual, row.Number = true, l.IssueNumber
	}
	var rec work.Record
	found := false
	if ref != nil {
		rec, found = pgTask(c, ref)
		if !found && row.Manual {
			// The link names the Issue by number too, so a board that numbers it differently still resolves it.
			for _, cand := range c.Records {
				if cand.Item.Issue.Number == row.Number {
					rec, found = cand, true
					ref = &agents.TaskRef{IssueID: uint64(cand.Item.Issue.ID), RepositoryID: uint64(cand.Item.Issue.Repo)}
					break
				}
			}
		}
	}
	row.Ref = ref
	switch {
	case ref == nil:
		row.Title = "Unlinked session"
	default:
		row.Linked, row.Issue = true, ref.IssueID
		row.Group = fmt.Sprintf("issue:%d", ref.IssueID)
		switch {
		case found:
			row.Task, row.Number = true, rec.Item.Issue.Number
			row.Title = fmt.Sprintf("#%d %s", rec.Item.Issue.Number, boardTitle(rec.Item.Issue.Title))
		case row.Manual:
			row.Title = fmt.Sprintf("#%d", row.Number)
		case pgClean(s.Name, 80) != "":
			row.Title = pgClean(s.Name, 80)
		default:
			row.Title = "Task session"
		}
		if row.Manual {
			row.Title += " (linked manually)"
		}
	}
	return row
}

// pgOpts is what the list is built from. text is a case-insensitive substring
// over task title and number, provider, account name and branch.
type pgOpts struct {
	sub   pgSub
	all   bool // Sessions: include ended sessions too
	text  string
	group bool
	// links are the manual links read from the backend, by session ID.
	links map[string]playgroundapi.Link
}

func (r pgRow) matches(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return true
	}
	number := ""
	if r.Number != 0 {
		number = fmt.Sprintf("#%d %d", r.Number, r.Number)
	}
	hay := strings.ToLower(strings.Join([]string{
		r.Title, number, string(r.Rec.Session.Provider), providerName(r.Rec.Session.Provider),
		r.Account, string(r.Rec.Session.AccountID), r.Rec.Branch,
	}, "\n"))
	return strings.Contains(hay, text)
}

// pgBuild applies the submenu filter and the search to the scoped records. The
// order is the records' own (stable) order; with grouping the rows of one Issue
// are gathered under the position of its first session, unlinked ones last.
// Urgency never changes it.
func pgBuild(recs []pgRec, c work.Collection, ack map[string]bool, o pgOpts) []pgRow {
	var rows []pgRow
	for _, r := range recs {
		row := pgRowOf(r, c, ack, o.links)
		active := r.Session.Active()
		switch o.sub {
		case pgSubSessions:
			if !active && !o.all {
				continue
			}
		case pgSubAttention:
			if !row.Fact.Attention {
				continue
			}
		case pgSubHistory:
			if active {
				continue
			}
		}
		if row.matches(o.text) {
			rows = append(rows, row)
		}
	}
	if !o.group {
		return rows
	}
	var order []string
	byGroup := map[string][]pgRow{}
	for _, r := range rows {
		if _, ok := byGroup[r.Group]; !ok && r.Group != "" {
			order = append(order, r.Group)
		}
		byGroup[r.Group] = append(byGroup[r.Group], r)
	}
	grouped := make([]pgRow, 0, len(rows))
	for _, g := range order {
		grouped = append(grouped, byGroup[g]...)
	}
	return append(grouped, byGroup[""]...)
}

// pgAttentionCount counts the sessions that need the user across the whole
// scoped list, whatever filter or search is active.
func pgAttentionCount(recs []pgRec, ack map[string]bool) int {
	n := 0
	for _, r := range recs {
		if pgFactOf(r.Session, ack[r.Session.ID]).Attention {
			n++
		}
	}
	return n
}

// pgReconcile keeps the selection on the same session ID. A selection that left
// the list moves to the row now at its old position (the one that followed it,
// or the last). idx is the position the selection had last time.
func pgReconcile(rows []pgRow, sel string, idx int) (string, int) {
	if len(rows) == 0 {
		return "", 0
	}
	for i, r := range rows {
		if r.ID == sel {
			return sel, i
		}
	}
	idx = max(0, min(idx, len(rows)-1))
	return rows[idx].ID, idx
}

// pgGroupTitle names a group by its first resolved session, else says the Issue
// is not loaded. Members keep their own state.
func pgGroupTitle(rows []pgRow, group string) (string, int) {
	title, n := "", 0
	for _, r := range rows {
		if r.Group != group {
			continue
		}
		n++
		if title == "" && r.Task {
			title = r.Title
		}
	}
	if title == "" {
		title = "Issue (details not loaded)"
	}
	return title, n
}
