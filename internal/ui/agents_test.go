package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	git "github.com/Tiago-0liveira/bonsai/internal/git/local"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi/fake"
)

type agentsEnv struct {
	repo    string
	wtA     string // plain linked worktree
	wtB     string // worktree reached through a symlink
	linkB   string
	project string
	fb      *fake.Backend
	m       Model
}

func newAgentsEnv(t *testing.T, opts ...Option) *agentsEnv {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &agentsEnv{repo: filepath.Join(root, "repo"), wtA: filepath.Join(root, "wt-a"), wtB: filepath.Join(root, "wt-b"), linkB: filepath.Join(root, "link-b")}
	for _, d := range []string{e.repo, e.wtA, e.wtB} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(e.wtB, e.linkB); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	e.project = config.ProjectID(e.repo)
	e.fb = fake.New()
	if len(opts) == 0 {
		opts = []Option{WithAgents(e.fb.API())}
	}
	e.m = New(e.repo, &config.Config{}, &config.State{}, opts...)
	e.m.width, e.m.height = 100, 30
	e.m.ready = true
	e.m.worktrees = []git.Worktree{
		{Path: e.repo, Branch: "main", IsMain: true},
		{Path: e.wtA, Branch: "feat-a"},
		{Path: e.wtB, Branch: "feat-b"},
	}
	e.m.rebuildItems()
	e.m.list.SetSize(60, 30)
	return e
}

// relist runs one list request to completion, as the runtime would.
func (e *agentsEnv) relist(t *testing.T) {
	t.Helper()
	e.apply(t, e.m.relistAgents())
}

func (e *agentsEnv) apply(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	nm, _ := e.m.Update(cmd())
	e.m = nm.(Model)
}

func (e *agentsEnv) session(id, provider, state, dir string) agentsapi.Session {
	return agentsapi.Session{
		Summary: agentterminal.Summary{ID: id, ProjectID: e.project, Provider: agents.ProviderID(provider), State: state},
		WorkDir: dir,
	}
}

// rowOf returns the first line of the worktree row naming branch.
func (e *agentsEnv) rowOf(branch string) string {
	for _, l := range strings.Split(ansi.Strip(e.m.list.View()), "\n") {
		if strings.Contains(l, branch) {
			return l
		}
	}
	return ""
}

func TestAgentsIndexMatchesWorktrees(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(e *agentsEnv) agentsapi.Session
		branch  string
		present bool
	}{
		{"by work dir", func(e *agentsEnv) agentsapi.Session { return e.session("s1", "claude", "running", e.wtA) }, "feat-a", true},
		{"by symlinked work dir", func(e *agentsEnv) agentsapi.Session { return e.session("s1", "claude", "running", e.linkB) }, "feat-b", true},
		{"by worktree id fallback", func(e *agentsEnv) agentsapi.Session {
			s := e.session("s1", "claude", "running", "/gone/elsewhere")
			s.WorktreeID = git.ID("local", e.wtA)
			return s
		}, "feat-a", true},
		{"other project ignored", func(e *agentsEnv) agentsapi.Session {
			s := e.session("s1", "claude", "running", e.wtA)
			s.ProjectID = "someone-else"
			return s
		}, "feat-a", false},
		{"ended session shows nothing", func(e *agentsEnv) agentsapi.Session { return e.session("s1", "claude", "exited", e.wtA) }, "feat-a", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newAgentsEnv(t)
			e.fb.Sessions.Add(tc.mutate(e))
			e.relist(t)
			row := e.rowOf(tc.branch)
			if got := strings.Contains(row, "✳"); got != tc.present {
				t.Errorf("glyph on %s row = %v, want %v: %q", tc.branch, got, tc.present, row)
			}
			if other := e.rowOf("main"); strings.Contains(other, "✳") {
				t.Errorf("glyph leaked onto main: %q", other)
			}
		})
	}
}

func TestAgentsBadgeContent(t *testing.T) {
	tests := []struct {
		name     string
		prefs    config.Prefs
		sessions func(e *agentsEnv) []agentsapi.Session
		want     string
	}{
		{"two providers in order", config.Prefs{}, func(e *agentsEnv) []agentsapi.Session {
			return []agentsapi.Session{e.session("a", "codex", "running", e.wtA), e.session("b", "claude", "starting", e.wtA)}
		}, "✳ ◎"},
		{"count", config.Prefs{}, func(e *agentsEnv) []agentsapi.Session {
			return []agentsapi.Session{e.session("a", "claude", "running", e.wtA), e.session("b", "claude", "running", e.wtA)}
		}, "✳2"},
		{"failed unacknowledged", config.Prefs{}, func(e *agentsEnv) []agentsapi.Session {
			return []agentsapi.Session{e.session("a", "claude", "running", e.wtA), e.session("b", "codex", "failed", e.wtA)}
		}, "✳ !"},
		{"ascii", config.Prefs{AgentGlyphs: "ascii"}, func(e *agentsEnv) []agentsapi.Session {
			return []agentsapi.Session{e.session("a", "antigravity", "running", e.wtA)}
		}, "A"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newAgentsEnv(t)
			e.m.state.Prefs = tc.prefs
			e.fb.Sessions.Add(tc.sessions(e)...)
			e.relist(t)
			row := strings.TrimRight(e.rowOf("feat-a"), " ")
			if !strings.HasSuffix(row, tc.want) {
				t.Errorf("row %q should end with %q", row, tc.want)
			}
		})
	}

	t.Run("acknowledged failure is hidden", func(t *testing.T) {
		e := newAgentsEnv(t)
		e.m.agents.ack = map[string]bool{"b": true}
		e.fb.Sessions.Add(e.session("b", "codex", "failed", e.wtA))
		e.relist(t)
		if row := e.rowOf("feat-a"); strings.Contains(row, "!") {
			t.Errorf("acknowledged failure still flagged: %q", row)
		}
	})
}

func TestAgentsEmitRelists(t *testing.T) {
	e := newAgentsEnv(t)
	t.Cleanup(e.m.agents.stop)
	e.relist(t)

	nm, cmd := e.m.Update(subscribeAgentsCmd(e.m.agents.ctx, e.m.agents.api.Sessions)())
	e.m = nm.(Model)
	waited := make(chan tea.Msg, 1)
	go func() { waited <- cmd() }()

	e.fb.Sessions.Add(e.session("s1", "claude", "running", e.wtA))
	e.fb.Emit()
	changed := <-waited
	if _, ok := changed.(agentsChangedMsg); !ok {
		t.Fatalf("Emit produced %T, want agentsChangedMsg", changed)
	}

	// The handler re-lists and re-arms the wait: a batch of both.
	nm, cmd = e.m.Update(changed)
	e.m = nm.(Model)
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("changed handler must batch list + wait, got %T", batch)
	}
	// The wait cmd blocks until the next Emit or ctx end, so run both cmds
	// concurrently and take the list result.
	out := make(chan tea.Msg, len(batch))
	for _, c := range batch {
		go func() { out <- c() }()
	}
	var list tea.Msg
	deadline := time.After(10 * time.Second)
	for list == nil {
		select {
		case msg := <-out:
			if _, ok := msg.(agentsListMsg); ok {
				list = msg
			}
		case <-deadline:
			t.Fatal("no list result after a change")
		}
	}
	nm, _ = e.m.Update(list)
	e.m = nm.(Model)
	if row := e.rowOf("feat-a"); !strings.Contains(row, "✳") {
		t.Errorf("new session not shown after change: %q", row)
	}
}

func TestAgentsStaleListDropped(t *testing.T) {
	e := newAgentsEnv(t)
	e.fb.Sessions.Add(e.session("s1", "claude", "running", e.wtA))
	older := e.m.relistAgents() // seq N
	newer := e.m.relistAgents() // seq N+1

	e.apply(t, newer)
	if len(e.m.agents.sessions) != 1 {
		t.Fatal("newer result not applied")
	}
	// A late older result carrying an empty list must not clobber the newer one.
	nm, _ := e.m.Update(agentsListMsg{seq: e.m.agents.applied - 1, byTree: map[string][]int{}})
	e.m = nm.(Model)
	if len(e.m.agents.sessions) != 1 {
		t.Errorf("stale result overwrote newer state: %d sessions", len(e.m.agents.sessions))
	}
	e.apply(t, older)
	if len(e.m.agents.sessions) != 1 {
		t.Errorf("late older cmd overwrote newer state")
	}
}

func TestAgentsListErrorKeepsLastGood(t *testing.T) {
	e := newAgentsEnv(t)
	e.fb.Sessions.Add(e.session("s1", "claude", "running", e.wtA))
	e.relist(t)

	e.fb.Sessions.ListErr = errors.New("holder dir unreadable")
	e.relist(t)
	if len(e.m.agents.sessions) != 1 || e.m.agents.err == "" {
		t.Fatalf("error must keep the last list and set err: n=%d err=%q", len(e.m.agents.sessions), e.m.agents.err)
	}
	if row := e.rowOf("feat-a"); !strings.Contains(row, "✳") {
		t.Errorf("glyph lost on list error: %q", row)
	}

	e.fb.Sessions.ListErr = nil
	e.fb.Sessions.Set(e.session("s1", "claude", "exited", e.wtA))
	e.relist(t)
	if e.m.agents.err != "" {
		t.Errorf("err not cleared by a good list: %q", e.m.agents.err)
	}
}

// emptiable lets a test turn the backend's list into a successful empty one.
type emptiable struct {
	agentsapi.Sessions
	empty bool
}

func (s *emptiable) List(ctx context.Context) ([]agentsapi.Session, error) {
	if s.empty {
		return []agentsapi.Session{}, nil
	}
	return s.Sessions.List(ctx)
}

func TestAgentsEmptyListClears(t *testing.T) {
	fb := fake.New()
	wrap := &emptiable{Sessions: fb.Sessions}
	e := newAgentsEnv(t, WithAgents(agentsapi.Backend{Sessions: wrap}))
	fb.Sessions.Add(e.session("s1", "claude", "running", e.wtA))
	e.relist(t)
	if !strings.Contains(e.rowOf("feat-a"), "✳") {
		t.Fatal("setup: glyph missing")
	}
	wrap.empty = true
	e.relist(t)
	if row := e.rowOf("feat-a"); strings.Contains(row, "✳") || len(e.m.agents.sessions) != 0 {
		t.Errorf("successful empty list must clear: %q", row)
	}
}

func TestAgentsFeatureOff(t *testing.T) {
	tests := []struct {
		name string
		opts func(e *fake.Backend) []Option
	}{
		{"no option", func(*fake.Backend) []Option { return []Option{func(*Model) {}} }},
		{"nil sessions", func(*fake.Backend) []Option { return []Option{WithAgents(agentsapi.Backend{})} }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fb := fake.New()
			e := newAgentsEnv(t, tc.opts(fb)...)
			if e.m.agents.enabled() {
				t.Fatal("agents must be off")
			}
			if e.m.initAgents() != nil || e.m.relistAgents() != nil {
				t.Error("no commands expected when off")
			}
			e.m.refreshAgentBadges()
			if row := e.rowOf("feat-a"); strings.ContainsAny(row, "✳◎✦!") {
				t.Errorf("glyphs rendered with agents off: %q", row)
			}
		})
	}
}

func TestAgentsUnsupportedHostKeepsBackendButIsIdle(t *testing.T) {
	fb := fake.New()
	fb.Sessions.Supports = false
	fb.Sessions.Add(agentsapi.Session{})
	e := newAgentsEnv(t, WithAgents(fb.API()))
	if !e.m.agents.enabled() || e.m.agents.active() {
		t.Fatalf("want enabled && !active, got enabled=%v active=%v", e.m.agents.enabled(), e.m.agents.active())
	}
	if e.m.initAgents() != nil || e.m.relistAgents() != nil || e.m.resubscribeAgents() != nil {
		t.Error("unsupported host must not subscribe or list")
	}
	e.m.refreshAgentBadges()
	if row := e.rowOf("feat-a"); strings.ContainsAny(row, "✳◎✦!") {
		t.Errorf("glyphs on unsupported host: %q", row)
	}
}

type failingChanges struct {
	agentsapi.Sessions
	err error
}

func (f *failingChanges) Changes(context.Context) (<-chan struct{}, error) { return nil, f.err }

func TestAgentsResubscribeAfterChangesError(t *testing.T) {
	fb := fake.New()
	fail := &failingChanges{Sessions: fb.Sessions, err: errors.New("watch failed")}
	e := newAgentsEnv(t, WithAgents(agentsapi.Backend{Sessions: fail}))
	t.Cleanup(e.m.agents.stop)
	if e.m.resubscribeAgents() != nil {
		t.Fatal("already subscribed by Init: no second subscription")
	}
	nm, cmd := e.m.Update(subscribeAgentsCmd(e.m.agents.ctx, fail)())
	e.m = nm.(Model)
	if cmd != nil || e.m.agents.err == "" {
		t.Fatalf("subscribe failure must set err and stop: cmd=%v err=%q", cmd, e.m.agents.err)
	}
	if e.m.resubscribeAgents() == nil {
		t.Error("next worktree refresh must retry the subscription")
	}
}

func TestAgentsNarrowRows(t *testing.T) {
	e := newAgentsEnv(t)
	e.fb.Sessions.Add(
		e.session("a", "claude", "running", e.wtA),
		e.session("b", "codex", "running", e.wtA),
	)
	e.relist(t)
	for _, w := range []int{30, 40, 80} {
		e.m.list.SetSize(w, 30)
		for _, l := range strings.Split(e.m.list.View(), "\n") {
			if got := ansi.StringWidth(l); got > w {
				t.Errorf("width %d: line is %d cells: %q", w, got, l)
			}
		}
		if row := e.rowOf("feat-a"); !strings.Contains(row, "✳ ◎") {
			t.Errorf("width %d: badges missing: %q", w, row)
		}
	}
}
