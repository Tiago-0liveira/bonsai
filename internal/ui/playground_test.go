package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	gardensync "github.com/Tiago-0liveira/bonsai/internal/core/garden/sync"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/work"
	git "github.com/Tiago-0liveira/bonsai/internal/git/local"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi/fake"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// --- pure model -------------------------------------------------------------

func pgS(id, project, dir string, created int) agentsapi.Session {
	return agentsapi.Session{
		Summary: agentterminal.Summary{ID: id, ProjectID: project, State: "running", CreatedAt: t0.Add(time.Duration(created) * time.Minute)},
		WorkDir: dir,
	}
}

func TestPgScopeKeepsOnlyThisRepository(t *testing.T) {
	trees := []pgTree{
		{ID: "wt-main", Path: "/r", Resolved: "/r", Branch: "main"},
		{ID: "wt-feat", Path: "/r-wt", Resolved: "/real/r-wt", Branch: "feat/x"},
	}
	resolve := func(p string) string {
		if p == "/link/r-wt" {
			return "/real/r-wt"
		}
		return p
	}
	byID := pgS("byid", "other", "/elsewhere", 5)
	byID.WorktreeID = "wt-feat"
	all := []agentsapi.Session{
		pgS("late", "mine", "/nowhere", 9),
		pgS("foreign", "other", "/other-repo", 1),             // another repository: out
		pgS("launch", "other", "/link/r-wt", 3),               // worktree-owned through a symlink: in
		pgS("manual", "mine", "", 2),                          // project match without a worktree: in
		byID,                                                  // stored worktree ID of this repository: in
		pgS("manual", "mine", "", 2),                          // duplicate record: once
		pgS("samebranch", "other", "/other-repo-checkout", 4), // never matched by branch name
	}
	all[6].Name = "feat/x"
	got := pgScope(all, "mine", trees, resolve)
	var ids []string
	for _, r := range got {
		ids = append(ids, r.Session.ID)
	}
	if want := "manual launch byid late"; strings.Join(ids, " ") != want {
		t.Fatalf("scope = %v, want %q (creation order, each once)", ids, want)
	}
	if got[1].Branch != "feat/x" || !got[1].InTree {
		t.Errorf("launch session lost its worktree facts: %+v", got[1])
	}
	if got[0].InTree || got[3].InTree {
		t.Errorf("sessions outside any worktree claim one: %+v %+v", got[0], got[3])
	}
}

func TestPgFactKeepsLifecycleAndActivitySeparate(t *testing.T) {
	code := 0
	ended := agentactivity.Activity{State: agentactivity.Waiting, Reason: agentactivity.ReasonPermission}
	tests := []struct {
		name      string
		mut       func(*agentsapi.Session)
		acked     bool
		label     string
		attention bool
	}{
		{"waiting names its reason", func(s *agentsapi.Session) {
			s.Activity = act(agentactivity.Waiting, agentactivity.ReasonPermission, "")
		}, false, "Needs input (permission)", true},
		{"idle is not completion", func(s *agentsapi.Session) { s.Activity = act(agentactivity.Idle, agentactivity.ReasonTurnComplete, "") }, false, "Idle", false},
		{"no activity is unknown, not working", func(s *agentsapi.Session) {}, false, "Running · activity unknown", false},
		{"unknown state is unknown", func(s *agentsapi.Session) { s.Activity = act(agentactivity.Unknown, "", "") }, false, "Running · activity unknown", false},
		{"stalled is not failure", func(s *agentsapi.Session) {
			a := act(agentactivity.Working, "", "")
			a.Stalled = true
			s.Activity = a
		}, false, "Stalled", false},
		{"inferred is marked", func(s *agentsapi.Session) {
			a := act(agentactivity.Working, "", "")
			a.Inferred = true
			s.Activity = a
		}, false, "Working (inferred)", false},
		{"exited shows the code and needs nobody", func(s *agentsapi.Session) { s.State, s.ExitCode = "exited", &code }, false, "Exited (code 0)", false},
		{"stale activity on an ended session is ignored", func(s *agentsapi.Session) { s.State, s.Activity = "exited", &ended }, false, "Exited", false},
		{"failed reason is sanitized", func(s *agentsapi.Session) { s.State, s.Error = "failed", "\x1b[31mboom\x07\nnow" }, false, "Failed: boom now", true},
		{"acknowledged failure stops asking", func(s *agentsapi.Session) { s.State, s.Error = "failed", "boom" }, true, "Failed: boom", false},
		{"failure without reason", func(s *agentsapi.Session) { s.State = "failed" }, true, "Failed: unknown error", false},
		{"starting", func(s *agentsapi.Session) { s.State = "starting" }, false, "Starting", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := pgS("a", "p", "", 0)
			tc.mut(&s)
			f := pgFactOf(s, tc.acked)
			if f.Label != tc.label || f.Attention != tc.attention {
				t.Errorf("fact = %q attention=%v, want %q attention=%v", f.Label, f.Attention, tc.label, tc.attention)
			}
			if low := strings.ToLower(f.Label); strings.Contains(low, "complete") || strings.Contains(low, "done") || strings.Contains(low, "finished") {
				t.Errorf("label %q claims completion", f.Label)
			}
		})
	}
}

func pgCollection() work.Collection {
	issue := func(id, number int64, title string) work.Record {
		return work.Record{Item: gardensync.Item{Issue: garden.Issue{Repo: 1, ID: garden.IssueID(id), Number: number, Title: title}}}
	}
	return work.Collection{Records: []work.Record{issue(7, 151, "Broken usage refresh"), issue(9, 174, "Reconnect")}}
}

func pgRecs() []pgRec {
	mk := func(id, provider, account, state string, created int, branch string, task *agents.TaskRef) pgRec {
		s := pgS(id, "p", "/wt", created)
		s.Provider, s.AccountID, s.ProfileName, s.State, s.Task, s.Name = agents.ProviderID(provider), agents.AccountID(account), account, state, task, "name-"+id
		return pgRec{Session: s, Branch: branch, InTree: branch != ""}
	}
	return []pgRec{
		mk("a1", "claude", "work", "running", 1, "feat/151-usage", &agents.TaskRef{RepositoryID: 1, IssueID: 7, AttemptID: "att1"}),
		mk("m1", "codex", "personal", "running", 2, "", nil),
		mk("a2", "codex", "personal", "running", 3, "feat/151-retry", &agents.TaskRef{RepositoryID: 1, IssueID: 7, AttemptID: "att2"}),
		mk("x1", "claude", "work", "exited", 4, "feat/old", &agents.TaskRef{RepositoryID: 1, IssueID: 9}),
		mk("f1", "codex", "personal", "failed", 5, "feat/boom", nil),
		mk("u1", "claude", "work", "running", 6, "main", &agents.TaskRef{RepositoryID: 1, IssueID: 404}),
	}
}

func pgRowIDs(rows []pgRow) string {
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return strings.Join(ids, " ")
}

func TestPgRowsTitlesFiltersAndSearch(t *testing.T) {
	c := pgCollection()
	recs := pgRecs()
	recs[0].Session.Activity = act(agentactivity.Waiting, agentactivity.ReasonQuestion, "")
	ack := map[string]bool{}
	build := func(o pgOpts) []pgRow { return pgBuild(recs, c, ack, o) }

	rows := build(pgOpts{sub: pgSubSessions, all: true})
	if got := pgRowIDs(rows); got != "a1 m1 a2 x1 f1 u1" {
		t.Fatalf("all rows = %q: every session once, in creation order", got)
	}
	titles := map[string]string{}
	for _, r := range rows {
		titles[r.ID] = r.Title
	}
	for id, want := range map[string]string{
		"a1": "#151 Broken usage refresh", "a2": "#151 Broken usage refresh", "x1": "#174 Reconnect",
		"m1": "Unlinked session", "f1": "Unlinked session",
		"u1": "name-u1", // linked, but the Issue is not in the loaded collection: the session's own name
	} {
		if titles[id] != want {
			t.Errorf("title(%s) = %q, want %q", id, titles[id], want)
		}
	}
	for _, tc := range []struct {
		name string
		o    pgOpts
		want string
	}{
		{"active", pgOpts{sub: pgSubSessions}, "a1 m1 a2 u1"},
		{"attention is waiting plus unacknowledged failure", pgOpts{sub: pgSubAttention}, "a1 f1"},
		{"history is ended and failed", pgOpts{sub: pgSubHistory}, "x1 f1"},
		{"search by task number", pgOpts{sub: pgSubSessions, all: true, text: "151"}, "a1 a2"},
		{"search by hash number", pgOpts{sub: pgSubSessions, all: true, text: "#174"}, "x1"},
		{"search by title, any case", pgOpts{sub: pgSubSessions, all: true, text: "BROKEN usage"}, "a1 a2"},
		{"search by provider", pgOpts{sub: pgSubSessions, all: true, text: "codex"}, "m1 a2 f1"},
		{"search by account", pgOpts{sub: pgSubSessions, all: true, text: "Personal"}, "m1 a2 f1"},
		{"search by branch", pgOpts{sub: pgSubSessions, all: true, text: "feat/boom"}, "f1"},
		{"search with no match", pgOpts{sub: pgSubSessions, all: true, text: "zzz"}, ""},
		{"search composes with a filter", pgOpts{sub: pgSubHistory, text: "claude"}, "x1"},
	} {
		if got := pgRowIDs(build(tc.o)); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
	ack["f1"] = true
	if got := pgRowIDs(build(pgOpts{sub: pgSubAttention})); got != "a1" {
		t.Errorf("acknowledged failure still needs attention: %q", got)
	}
	if got := pgAttentionCount(recs, ack); got != 1 {
		t.Errorf("attention count = %d, want 1", got)
	}
}

func TestPgGroupingNestsWithoutMergingStates(t *testing.T) {
	c := pgCollection()
	recs := pgRecs()
	recs[0].Session.Activity = act(agentactivity.Waiting, agentactivity.ReasonQuestion, "")
	recs[2].Session.Activity = act(agentactivity.Working, "", "")
	flat := pgBuild(recs, c, nil, pgOpts{sub: pgSubSessions, all: true})
	grouped := pgBuild(recs, c, nil, pgOpts{sub: pgSubSessions, all: true, group: true})
	if len(grouped) != len(flat) {
		t.Fatalf("grouping changed the row count: %d vs %d", len(grouped), len(flat))
	}
	if got := pgRowIDs(grouped); got != "a1 a2 x1 u1 m1 f1" {
		t.Fatalf("grouped order = %q: issue groups by first session, unlinked last", got)
	}
	if title, n := pgGroupTitle(grouped, grouped[0].Group); title != "#151 Broken usage refresh" || n != 2 {
		t.Errorf("group = %q (%d sessions)", title, n)
	}
	if grouped[0].Fact.Label == grouped[1].Fact.Label {
		t.Errorf("sessions of one Issue must keep separate states: %q", grouped[0].Fact.Label)
	}
	if title, _ := pgGroupTitle(grouped, "issue:404"); !strings.Contains(title, "not loaded") {
		t.Errorf("unresolved group title = %q", title)
	}
}

func TestPgReconcileFollowsIDThenNeighbor(t *testing.T) {
	rows := func(ids ...string) []pgRow {
		var out []pgRow
		for _, id := range ids {
			out = append(out, pgRow{ID: id})
		}
		return out
	}
	if id, i := pgReconcile(rows("a", "b", "c"), "b", 0); id != "b" || i != 1 {
		t.Errorf("follows the ID: %s %d", id, i)
	}
	if id, i := pgReconcile(rows("a", "c", "d"), "b", 1); id != "c" || i != 1 {
		t.Errorf("a vanished row hands over to the one that took its place: %s %d", id, i)
	}
	if id, i := pgReconcile(rows("a", "b"), "c", 2); id != "b" || i != 1 {
		t.Errorf("past the end falls back to the last: %s %d", id, i)
	}
	if id, _ := pgReconcile(nil, "c", 2); id != "" {
		t.Errorf("empty list selects nothing: %q", id)
	}
}

func TestPgTopKeepsSelectionVisible(t *testing.T) {
	units := []pgUnit{{id: "a"}, {id: "b"}, {id: "c"}, {id: "d"}}
	if got := pgTop(units, 2, 0, "d", 4); got != 2 {
		t.Errorf("top = %d, want 2", got)
	}
	if got := pgTop(units, 2, 2, "b", 4); got != 1 {
		t.Errorf("scrolling back up moves the window just enough: %d", got)
	}
	if got := pgTop(units, 2, 1, "c", 4); got != 1 {
		t.Errorf("a visible selection does not move the window: %d", got)
	}
}

// --- model level --------------------------------------------------------------

type pgEnv struct {
	repo, tree, other string
	fb                *fake.Backend
	m                 Model
}

func newPgEnv(t *testing.T, w, h int, opts ...Option) *pgEnv {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &pgEnv{repo: filepath.Join(root, "repo"), tree: filepath.Join(root, "wt"), other: filepath.Join(root, "other"), fb: fake.New()}
	for _, d := range []string{e.repo, e.tree, e.other} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	st, err := config.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	opts = append([]Option{WithLayout(LayoutRedesign), WithAgents(e.fb.API())}, opts...)
	m := sized(t, New(e.repo, &config.Config{}, st, opts...), w, h)
	m.pg.trees = func(context.Context, string) ([]git.Worktree, error) {
		return []git.Worktree{{Path: e.repo, Branch: "main", IsMain: true}, {Path: e.tree, Branch: "feat/151-usage"}}, nil
	}
	e.m = m
	return e
}

// sess builds a session of this repository running in its linked worktree.
func (e *pgEnv) sess(id, provider, account, state string, created int) agentsapi.Session {
	return agentsapi.Session{
		Summary: agentterminal.Summary{
			ID: id, ProjectID: config.ProjectID(e.repo), Provider: agents.ProviderID(provider), AccountID: agents.AccountID(account),
			ProfileName: account, State: state, CreatedAt: t0.Add(time.Duration(created) * time.Minute),
		},
		WorkDir: e.tree,
	}
}

// pgDrain feeds a command's messages back. Blocking subscriptions are skipped
// before execution, since tests deliver their messages themselves.
func pgDrain(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		if name := runtime.FuncForPC(reflect.ValueOf(c).Pointer()).Name(); strings.Contains(name, "worktreeRead") || strings.Contains(name, "indexFiles") || strings.Contains(name, "tickProc") || strings.Contains(name, "tickPRs") {
			continue
		}
		if name := runtime.FuncForPC(reflect.ValueOf(c).Pointer()).Name(); strings.Contains(name, "waitInputCmd") || strings.Contains(name, "waitFramesCmd") || strings.Contains(name, "pgWaitCmd") {
			continue // blocking waits; tests deliver their messages themselves
		}
		// Terminal fixtures opt into short timers. List tests do not need a
		// live preview, and debounce tests deliver their ticks explicitly.
		if name := runtime.FuncForPC(reflect.ValueOf(c).Pointer()).Name(); strings.Contains(name, "bubbletea.Tick") && m.pgDebounce() >= pgAttachDebounce {
			continue
		}
		done := make(chan tea.Msg, 1)
		go func() { done <- c() }()
		var msg tea.Msg
		select {
		case msg = <-done:
		case <-time.After(150 * time.Millisecond):
			continue
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		if msg == nil {
			continue
		}
		var next tea.Cmd
		m, next = send(t, m, msg)
		queue = append(queue, next)
	}
	return m
}

func (e *pgEnv) key(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		var cmd tea.Cmd
		e.m, cmd = send(t, e.m, keyOf(k))
		e.m = pgDrain(t, e.m, cmd)
	}
}

// open goes to the Playground and enters its list, with the sessions loaded.
func (e *pgEnv) open(t *testing.T) {
	t.Helper()
	var cmd tea.Cmd
	e.m, cmd = send(t, e.m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2"), Alt: true})
	e.m = pgDrain(t, e.m, cmd)
	if e.m.workspace != wsPlayground {
		t.Fatalf("workspace = %v", e.m.workspace)
	}
	e.key(t, "enter")
	if e.m.currentShellFocus() != shellBody {
		t.Fatalf("focus = %v", e.m.currentShellFocus())
	}
}

// relist delivers a change signal and applies the resulting list.
func (e *pgEnv) relist(t *testing.T) {
	t.Helper()
	var cmd tea.Cmd
	e.m, cmd = send(t, e.m, pgChangedMsg{ch: make(chan struct{})})
	e.m = pgDrain(t, e.m, cmd)
}

func (e *pgEnv) fixture() {
	waiting := e.sess("s1", "claude", "work", "running", 1)
	waiting.Activity = act(agentactivity.Waiting, agentactivity.ReasonPermission, "Bash")
	waiting.Task = &agents.TaskRef{RepositoryID: 1, IssueID: 7, AttemptID: "att1"}
	waiting.Name = "attempt one"
	second := e.sess("s2", "codex", "personal", "running", 2)
	second.Activity = act(agentactivity.Working, "", "")
	second.Task = &agents.TaskRef{RepositoryID: 1, IssueID: 7, AttemptID: "att2"}
	idle := e.sess("s3", "codex", "personal", "running", 3)
	idle.Activity = act(agentactivity.Idle, agentactivity.ReasonTurnComplete, "")
	idle.WorkDir = e.repo
	unknown := e.sess("s4", "claude", "work", "running", 4)
	ended := e.sess("s5", "claude", "work", "exited", 5)
	failed := e.sess("s6", "codex", "personal", "failed", 6)
	failed.Error = "\x1b[31mboom\x07"
	viaTree := e.sess("s7", "claude", "work", "running", 7) // another project id, but a worktree of this repository
	viaTree.ProjectID = "someone-else"
	foreign := e.sess("foreign", "claude", "work", "running", 8)
	foreign.ProjectID, foreign.WorkDir = "someone-else", e.other
	foreign.Name = "feat/151-usage" // the same branch name proves nothing
	e.fb.Sessions.Add(waiting, second, idle, unknown, ended, failed, viaTree, foreign)
}

func (e *pgEnv) ids() string {
	rows := e.m.playgroundRows()
	return pgRowIDs(rows)
}

func TestPlaygroundListsRepositorySessionsOnly(t *testing.T) {
	e := newPgEnv(t, 120, 35)
	e.fixture()
	e.open(t)
	if got := e.m.pg.sel; got != "s1" {
		t.Errorf("first row is not selected: %q", got)
	}
	var all []string
	for _, r := range e.m.pg.recs {
		all = append(all, r.Session.ID)
	}
	if got := strings.Join(all, " "); got != "s1 s2 s3 s4 s5 s6 s7" {
		t.Fatalf("scoped sessions = %q; the other repository's must stay out and a worktree-owned one stay in", got)
	}
	if got := e.ids(); got != "s1 s2 s3 s4 s7" {
		t.Errorf("active rows = %q", got)
	}
	out := view(e.m)
	for _, want := range []string{"ACTIVE SESSIONS 5", "Attention 2", "Unlinked session", "Claude / work / Needs input (permission)", "Codex / personal / Idle", "Claude / work / Running · activity unknown"} {
		if !strings.Contains(out, want) {
			t.Errorf("view lacks %q:\n%s", want, out)
		}
	}
	for _, bad := range []string{"foreign", "Open terminal", "open terminal", "complete"} {
		if strings.Contains(out, bad) {
			t.Errorf("view must not contain %q:\n%s", bad, out)
		}
	}
}

func TestPlaygroundTitlesUseLoadedGardenOnly(t *testing.T) {
	e := newPgEnv(t, 120, 35)
	e.m.garden.board.collection = pgCollection()
	e.fixture()
	e.open(t)
	out := view(e.m)
	if strings.Count(out, "#151 Broken usage refresh") < 2 {
		t.Errorf("two attempts on Issue #151 should both name it:\n%s", out)
	}
	e.key(t, "g")
	out = view(e.m)
	if !strings.Contains(out, "#151 Broken usage refresh · 2") || !strings.Contains(out, "Unlinked sessions") {
		t.Errorf("grouped view lacks the Issue group:\n%s", out)
	}
	if e.ids() != "s1 s2 s4 s7 s3" && e.ids() != "s1 s2 s3 s4 s7" {
		t.Errorf("grouped ids = %q", e.ids())
	}
}

func TestPlaygroundFiltersAndSubmenu(t *testing.T) {
	e := newPgEnv(t, 120, 35)
	e.fixture()
	e.open(t)
	e.key(t, "a")
	if got := e.ids(); got != "s1 s2 s3 s4 s5 s6 s7" {
		t.Errorf("all = %q", got)
	}
	if !strings.Contains(view(e.m), "ALL SESSIONS 7") {
		t.Errorf("title lacks the all-sessions count")
	}
	e.key(t, "tab") // Attention
	if e.m.pg.sub != pgSubAttention || e.ids() != "s1 s6" {
		t.Errorf("attention = %v %q", e.m.pg.sub, e.ids())
	}
	if e.m.pg.sel != "s1" {
		t.Errorf("selection survived the submenu change: %q", e.m.pg.sel)
	}
	e.key(t, "tab") // History
	if e.ids() != "s5 s6" {
		t.Errorf("history = %q", e.ids())
	}
	if e.m.pg.sel != "s5" {
		t.Errorf("selected session is not listed here, so it moves to the row in its place: %q", e.m.pg.sel)
	}
	e.key(t, "tab", "tab") // Usage, then Sessions again
	if e.m.pg.sub != pgSubSessions {
		t.Errorf("tab does not cycle: %v", e.m.pg.sub)
	}
	e.key(t, "left")
	if e.m.pg.sub != pgSubUsage {
		t.Errorf("left does not go back: %v", e.m.pg.sub)
	}
	e.key(t, "esc")
	if e.m.currentShellFocus() != shellNav {
		t.Errorf("esc must return to navigation")
	}
}

func TestPlaygroundSearchCapturesKeys(t *testing.T) {
	e := newPgEnv(t, 120, 35)
	e.fixture()
	e.open(t)
	e.key(t, "/")
	if e.m.currentShellFocus() != shellInput {
		t.Fatalf("search must own the keyboard, focus = %v", e.m.currentShellFocus())
	}
	for _, k := range []string{"q", "?", "a", "g", ",", "o"} {
		var cmd tea.Cmd
		e.m, cmd = send(t, e.m, runeKey(k))
		if cmd != nil && fmt.Sprintf("%T", cmd()) == "tea.quitMsg" {
			t.Fatalf("%q quit the program while typing", k)
		}
	}
	if e.m.modal != nil || e.m.workspace != wsPlayground || e.m.pg.all || e.m.pg.group {
		t.Fatalf("typed letters acted as commands: modal=%v ws=%v", e.m.modal != nil, e.m.workspace)
	}
	if e.m.pg.query != "q?ag,o" {
		t.Fatalf("query = %q", e.m.pg.query)
	}
	if hints := fmt.Sprint(e.m.shellHints()); !strings.Contains(hints, "apply search") {
		t.Errorf("hints while typing: %s", hints)
	}
	e.key(t, "esc")
	if e.m.pg.query != "" || e.m.currentShellFocus() != shellBody {
		t.Errorf("esc cancels the search: %q %v", e.m.pg.query, e.m.currentShellFocus())
	}
	e.key(t, "/")
	e.m, _ = send(t, e.m, runeKey("p"))
	e.m, _ = send(t, e.m, runeKey("e"))
	e.m, _ = send(t, e.m, runeKey("r"))
	if got := e.ids(); got != "s2 s3" {
		t.Errorf("search %q matched %q", e.m.pg.query, got)
	}
	e.key(t, "enter")
	if e.m.currentShellFocus() != shellBody || e.m.pg.query != "per" {
		t.Errorf("enter applies the search: %q %v", e.m.pg.query, e.m.currentShellFocus())
	}
	if !strings.Contains(view(e.m), "Search: per") {
		t.Errorf("applied search not shown")
	}
}

func TestPlaygroundSelectionIsStableAcrossUpdates(t *testing.T) {
	e := newPgEnv(t, 120, 35)
	e.fixture()
	e.open(t)
	e.key(t, "down", "down") // s3
	if e.m.pg.sel != "s3" {
		t.Fatalf("sel = %q", e.m.pg.sel)
	}
	before := e.ids()
	// An activity flip of another session, and of the selected one.
	other, _ := e.fb.Sessions.List(context.Background())
	for _, s := range other {
		if s.ID == "s2" {
			s.Activity = act(agentactivity.Waiting, agentactivity.ReasonQuestion, "")
			e.fb.Sessions.Set(s)
		}
		if s.ID == "s3" {
			s.Activity = act(agentactivity.Working, "", "")
			e.fb.Sessions.Set(s)
		}
	}
	e.relist(t)
	if e.ids() != before || e.m.pg.sel != "s3" {
		t.Errorf("activity reordered or moved the cursor: %q sel=%q (was %q)", e.ids(), e.m.pg.sel, before)
	}
	if got := pgAttentionCount(e.m.pg.recs, e.m.agents.ack); got != 3 {
		t.Errorf("attention count = %d, want 3 (the flip is a marker, not a move)", got)
	}
	// A new session arrives: appended, cursor stays on its session.
	e.fb.Sessions.Add(e.sess("s9", "codex", "personal", "running", 20))
	e.relist(t)
	if e.m.pg.sel != "s3" || !strings.HasSuffix(e.ids(), "s9") {
		t.Errorf("new session moved the row under the cursor: %q sel=%q", e.ids(), e.m.pg.sel)
	}
	// The selected session ends: it leaves Active, the neighbor takes over.
	for _, s := range other {
		if s.ID == "s3" {
			s.State, s.Activity = "exited", nil
			e.fb.Sessions.Set(s)
		}
	}
	e.relist(t)
	if e.m.pg.sel != "s4" {
		t.Errorf("selection after its session left = %q, want its neighbor s4 (ids %q)", e.m.pg.sel, e.ids())
	}
}

func TestPlaygroundListOrderingErrorsAndStaleMarker(t *testing.T) {
	e := newPgEnv(t, 120, 35)
	e.fixture()
	e.open(t)
	applied := e.m.agents.applied
	good := len(e.m.pg.recs)

	// An older answer than the one applied is dropped.
	nm, _ := send(t, e.m, pgListMsg{seq: applied})
	if len(nm.pg.recs) != good {
		t.Errorf("stale list replaced newer state")
	}
	nm, _ = send(t, e.m, pgListMsg{seq: applied - 1, recs: nil})
	if len(nm.pg.recs) != good {
		t.Errorf("older list cleared the list")
	}
	// A newer failure keeps the last good list and says so.
	e.fb.Sessions.ListErr = errors.New("boom at /home/me/secret")
	e.relist(t)
	if len(e.m.pg.recs) != good || !e.m.pg.stale {
		t.Fatalf("failure must keep the last good list: %d recs, stale=%v", len(e.m.pg.recs), e.m.pg.stale)
	}
	out := view(e.m)
	if !strings.Contains(out, "Session list stale") || !strings.Contains(out, "Sessions stale") {
		t.Errorf("stale marker missing:\n%s", out)
	}
	if !strings.Contains(out, "Needs input") {
		t.Errorf("last good rows vanished:\n%s", out)
	}
	// Recovery clears the marker; only a successful empty result empties the list.
	e.fb.Sessions.ListErr = nil
	e.relist(t)
	if e.m.pg.stale || len(e.m.pg.recs) != good {
		t.Errorf("recovery: stale=%v recs=%d", e.m.pg.stale, len(e.m.pg.recs))
	}
	empty := newPgEnv(t, 120, 35)
	empty.open(t)
	if !empty.m.pg.loaded || len(empty.m.pg.recs) != 0 || !strings.Contains(view(empty.m), "No active sessions") && !strings.Contains(view(empty.m), "No sessions for this repository yet.") {
		t.Errorf("an empty successful list is a loaded empty state:\n%s", view(empty.m))
	}
	e.fb.Sessions.Set(agentsapi.Session{}) // keep the fake non-empty for the next line
	e.m.pg.recs = e.m.pg.recs[:1]
	nm, _ = send(t, e.m, pgListMsg{seq: e.m.agents.seq + 5, recs: nil})
	if len(nm.pg.recs) != 0 {
		t.Errorf("a successful empty result must clear the list")
	}
	// Worktrees that cannot be verified keep the last good list too.
	nm, _ = send(t, e.m, pgListMsg{seq: e.m.agents.seq + 6, recs: nil, degraded: true})
	if len(nm.pg.recs) != 1 || !nm.pg.stale {
		t.Errorf("degraded list replaced the last good one: %d stale=%v", len(nm.pg.recs), nm.pg.stale)
	}
}

// Enter used to do nothing (10.1). Since 10.2 it requests control; what must still hold is that it never moves
// the selection, never starts a session and never advertises something it cannot do.
func TestPlaygroundEnterNeverStartsOrMovesSelection(t *testing.T) {
	e := newPgEnv(t, 120, 35)
	e.fixture()
	e.open(t)
	sel := e.m.pg.sel
	e.key(t, "enter")
	if e.m.pg.sel != sel || e.m.workspace != wsPlayground || e.m.modal != nil {
		t.Errorf("enter changed state")
	}
	if n := len(e.fb.Sessions.Starts()); n != 0 {
		t.Errorf("starts = %d", n)
	}
}

func TestPlaygroundNavigationNeverRefreshesUsage(t *testing.T) {
	e := newPgEnv(t, 120, 35)
	e.fb.Accounts.Stored = map[agents.AccountID]agentsapi.Usage{
		"work": {Snapshot: agents.UsageSnapshot{}, Headline: agents.NewUsageHeadline(&agents.UsageLimit{ID: "5h", Window: "5h", RemainingFraction: pgPtr(0.16)}, nil, nil), FetchedAt: time.Now()},
	}
	e.fixture()
	e.open(t)
	e.key(t, "down", "down", "up", "a", "g", "tab", "tab", "tab", "right", "left", "/")
	e.key(t, "esc")
	e.relist(t)
	e.m, _ = send(t, e.m, pgChangedMsg{ch: make(chan struct{})})
	if calls := e.fb.Accounts.UsageCalls(); len(calls) != 0 {
		t.Fatalf("Playground navigation called the provider: %+v", calls)
	}
	e.m = e.m.playgroundSetSub(pgSubUsage)
	out := view(e.m)
	for _, want := range []string{"ACCOUNT CAPACITY", "16% left", "▰▰▱▱▱▱▱▱▱▱", "capacity, not task progress", "no stored snapshot yet"} {
		if !strings.Contains(out, want) {
			t.Errorf("usage view lacks %q:\n%s", want, out)
		}
	}
	capacity, _ := e.m.pgCapacity("work")
	if !strings.Contains(ansi.Strip(capacity), "▰▰▱▱▱▱▱▱▱▱  16% left") || strings.Contains(out+capacity, "% used") {
		t.Fatalf("session and Usage meters must show the same remaining capacity: %s\n%s", capacity, out)
	}
	e.m.work.AgentGlyphs = "ascii"
	ascii := view(e.m)
	if !strings.Contains(ascii, "##........  16% left") || strings.ContainsAny(ascii, "▰▱█░") {
		t.Fatalf("usage meter lacks the Accounts ASCII fallback:\n%s", ascii)
	}
	for _, s := range []string{"token", "secret"} {
		if strings.Contains(strings.ToLower(out), s) {
			t.Errorf("usage view mentions %q", s)
		}
	}
	if calls := e.fb.Accounts.UsageCalls(); len(calls) != 0 {
		t.Fatalf("reading the usage view called the provider: %+v", calls)
	}
}

func pgPtr[T any](v T) *T { return &v }

func TestPlaygroundMarksFailureSeen(t *testing.T) {
	e := newPgEnv(t, 120, 35)
	e.fixture()
	e.open(t)
	e.key(t, "tab", "tab") // History: s5, s6
	e.key(t, "down")
	if e.m.pg.sel != "s6" {
		t.Fatalf("sel = %q", e.m.pg.sel)
	}
	if !strings.Contains(fmt.Sprint(e.m.shellHints()), "mark seen") {
		t.Errorf("an unacknowledged failure should offer x")
	}
	before := pgAttentionCount(e.m.pg.recs, e.m.agents.ack)
	e.key(t, "x")
	if got := pgAttentionCount(e.m.pg.recs, e.m.agents.ack); got != before-1 {
		t.Errorf("attention %d -> %d", before, got)
	}
	if strings.Contains(fmt.Sprint(e.m.shellHints()), "mark seen") {
		t.Errorf("hint stays after acknowledging")
	}
	if !strings.Contains(view(e.m), "Failed: boom") || strings.ContainsAny(view(e.m), "\x07") {
		t.Errorf("failure reason missing or not sanitized")
	}
}

func TestPlaygroundPaletteEntries(t *testing.T) {
	labels, _ := paletteTargets()
	for _, want := range []string{"Playground: Sessions", "Playground: Attention", "Playground: History", "Playground: Usage"} {
		found := false
		for _, l := range labels {
			found = found || l == want
		}
		if !found {
			t.Errorf("palette lacks %q", want)
		}
	}
	e := newPgEnv(t, 120, 35)
	e.fixture()
	tg, _ := paletteTargetForLabel("Playground: Attention")
	nm, cmd := e.m.goToPalette(tg)
	e.m = pgDrain(t, nm, cmd)
	if e.m.workspace != wsPlayground || e.m.pg.sub != pgSubAttention || e.m.currentShellFocus() != shellBody {
		t.Errorf("palette landed on ws=%v sub=%v focus=%v", e.m.workspace, e.m.pg.sub, e.m.currentShellFocus())
	}
	if e.ids() != "s1 s6" {
		t.Errorf("attention list after palette = %q", e.ids())
	}
}

func TestPlaygroundStartsOnPlaygroundLoadsFromInit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	repo := t.TempDir()
	fb := fake.New()
	st, err := config.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	st.Prefs.StartWorkspace = "playground"
	m := New(repo, &config.Config{}, st, WithLayout(LayoutRedesign), WithAgents(fb.API()))
	if m.workspace != wsPlayground {
		t.Fatalf("workspace = %v", m.workspace)
	}
	s := agentsapi.Session{Summary: agentterminal.Summary{ID: "x", ProjectID: config.ProjectID(repo), State: "running"}}
	fb.Sessions.Add(s)
	m = sized(t, m, 120, 35)
	m = pgDrain(t, m, m.Init())
	if !m.pg.loaded || len(m.pg.recs) != 1 || !m.pg.subscribed {
		t.Errorf("startup on Playground did not list and subscribe: loaded=%v recs=%d sub=%v", m.pg.loaded, len(m.pg.recs), m.pg.subscribed)
	}
}

func TestPlaygroundUnavailableWithoutAgents(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	st, _ := config.LoadState()
	m := sized(t, New(t.TempDir(), &config.Config{}, st, WithLayout(LayoutRedesign)), 80, 24)
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2"), Alt: true})
	m, _ = send(t, m, keyOf("enter"))
	if !strings.Contains(view(m), "not available on this host") {
		t.Errorf("missing explanation:\n%s", view(m))
	}
}

func TestPlaygroundOpensLinkedIssueFromLoadedBoard(t *testing.T) {
	fb := gardenFake(gardenapi.StateConnected)
	e := newPgEnv(t, 120, 35, WithGarden(fb))
	e.m.garden.hasStatus, e.m.garden.status = true, fb.StatusView
	e.m.garden.board.collection, e.m.garden.board.loaded = pgCollection(), true
	e.fixture()
	e.open(t)
	if !strings.Contains(fmt.Sprint(e.m.shellHints()), "issue") {
		t.Errorf("a resolved Issue should offer the i link: %v", e.m.shellHints())
	}
	e.m, _ = send(t, e.m, runeKey("i"))
	if e.m.workspace != wsGarden || e.m.garden.board.opened != 7 {
		t.Fatalf("i opened ws=%v opened=%d", e.m.workspace, e.m.garden.board.opened)
	}
	// Unresolved Issue: no jump.
	e2 := newPgEnv(t, 120, 35, WithGarden(fb))
	e2.m.garden.hasStatus, e2.m.garden.status = true, fb.StatusView
	e2.m.garden.board.loaded = true
	e2.fixture()
	e2.open(t)
	e2.m, _ = send(t, e2.m, runeKey("i"))
	if e2.m.workspace != wsPlayground {
		t.Errorf("i left the Playground without a resolved Issue")
	}
}

func TestPlaygroundRenderBoundsAllSizes(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 35}, {160, 48}} {
		w, h := size[0], size[1]
		for _, ascii := range []bool{false, true} {
			t.Run(fmt.Sprintf("%dx%d/ascii=%v", w, h, ascii), func(t *testing.T) {
				e := newPgEnv(t, w, h)
				if ascii {
					e.m.work.AgentGlyphs = "ascii"
				}
				e.m.garden.board.collection = pgCollection()
				e.fixture()
				for i := 0; i < 12; i++ { // enough sessions to scroll
					s := e.sess(fmt.Sprintf("n%02d", i), "codex", "personal", "running", 30+i)
					s.Name = strings.Repeat("a very long session name 界 ", 6)
					s.Task = &agents.TaskRef{RepositoryID: 1, IssueID: 404}
					e.fb.Sessions.Add(s)
				}
				e.open(t)
				check := func(label string) {
					t.Helper()
					lines := strings.Split(e.m.View(), "\n")
					if len(lines) != h {
						t.Errorf("%s: %d lines, want %d", label, len(lines), h)
					}
					for i, l := range lines {
						if got := ansi.StringWidth(l); got > w {
							t.Errorf("%s: line %d is %d cells > %d: %q", label, i, got, w, ansi.Strip(l))
						}
					}
					if ascii {
						for _, r := range ansi.Strip(e.m.View()) {
							if r > 0x7e && r != '界' {
								t.Errorf("%s: non-ASCII glyph %q in ASCII mode", label, r)
								break
							}
						}
					}
				}
				check("list")
				for step := 0; step < 14; step++ {
					e.key(t, "down")
					check(fmt.Sprintf("down %d", step))
					rows := e.m.playgroundRows()
					idx := -1
					for i, r := range rows {
						if r.ID == e.m.pg.sel {
							idx = i
						}
					}
					if idx < 0 {
						t.Fatalf("selection %q left the list", e.m.pg.sel)
					}
				}
				e.key(t, "g")
				check("grouped")
				e.key(t, "a")
				check("all")
				e.key(t, "/")
				check("searching")
				e.key(t, "esc", "tab")
				check("attention")
				e.key(t, "tab")
				check("history")
				e.key(t, "tab")
				check("usage")
				e.m.shellFocus = shellNav
				check("nav")
				if w >= pgWideMin {
					e.m.shellFocus = shellBody
					e.m = e.m.playgroundSetSub(pgSubSessions)
					if !strings.Contains(view(e.m), "not progress of this task") {
						t.Errorf("wide layout lacks the detail pane")
					}
				} else if strings.Contains(view(e.m), "not progress of this task") {
					t.Errorf("compact layout shows the detail pane")
				}
			})
		}
	}
}

func TestPlaygroundSelectedRowStaysVisibleWhenScrolling(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 35}} {
		e := newPgEnv(t, size[0], size[1])
		for i := 0; i < 20; i++ {
			s := e.sess(fmt.Sprintf("n%02d", i), "codex", "personal", "running", i)
			s.Task = &agents.TaskRef{RepositoryID: 1, IssueID: uint64(1000 + i)}
			s.Name = fmt.Sprintf("session-%02d", i)
			e.fb.Sessions.Add(s)
		}
		e.open(t)
		for i := 0; i < 19; i++ {
			e.key(t, "down")
			if want := fmt.Sprintf("session-%02d", i+1); !strings.Contains(view(e.m), want) {
				t.Fatalf("%v: selected %s not on screen:\n%s", size, want, view(e.m))
			}
		}
		for i := 0; i < 19; i++ {
			e.key(t, "up")
		}
		if !strings.Contains(view(e.m), "session-00") {
			t.Errorf("%v: top of the list not shown after scrolling back", size)
		}
	}
}
