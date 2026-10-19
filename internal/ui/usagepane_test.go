package ui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/usagestore"
)

func TestAgeLabel(t *testing.T) {
	now := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name       string
		age        time.Duration
		zero       bool
		refreshing bool
		want       string
	}{
		{name: "refreshing wins", age: time.Hour, refreshing: true, want: "refreshing…"},
		{name: "never fetched", zero: true, want: "not fetched"},
		{name: "fresh", age: 0, want: "<30s"},
		{name: "just under 30s", age: 29 * time.Second, want: "<30s"},
		{name: "30s", age: 30 * time.Second, want: "<1m"},
		{name: "just under 1m", age: 59 * time.Second, want: "<1m"},
		{name: "1m", age: time.Minute, want: "1m"},
		{name: "just under 2m", age: 119 * time.Second, want: "1m"},
		{name: "2m", age: 2 * time.Minute, want: "2m"},
		{name: "3m", age: 3 * time.Minute, want: "3m"},
		{name: "just under 4m", age: 239 * time.Second, want: "3m"},
		{name: "4m", age: 4 * time.Minute, want: "4m+"},
		{name: "long overdue", age: 2 * time.Hour, want: "4m+"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fetched := now.Add(-tc.age)
			if tc.zero {
				fetched = time.Time{}
			}
			if got := ageLabel(fetched, now, tc.refreshing); got != tc.want {
				t.Errorf("ageLabel = %q, want %q", got, tc.want)
			}
		})
	}
}

func paneLimit(window string, frac float64, reset time.Duration) *agents.UsageLimit {
	l := &agents.UsageLimit{ID: window, Window: window, RemainingFraction: &frac}
	r := t0.Add(reset)
	l.ResetsAt = &r
	return l
}

// paneUsage is a usage fetched age ago, as of the pinned clock.
func paneUsage(age time.Duration, five, week float64) agentsapi.Usage {
	return agentsapi.Usage{
		Headline:  agents.NewUsageHeadline(paneLimit("5h", five, 2*time.Hour+14*time.Minute), paneLimit("weekly", week, 76*time.Hour), nil),
		FetchedAt: t0.Add(-age),
	}
}

func paneAccount(id, provider, name string) agentsapi.Account {
	return agentsapi.Account{Account: agents.Account{ID: agents.AccountID(id), Provider: agents.ProviderID(provider), Name: name}}
}

// paneEnv is an agents env at w x h with a pinned clock and the given accounts.
func paneEnv(t *testing.T, w, h int, accounts ...agentsapi.Account) *agentsEnv {
	t.Helper()
	old := agentNow
	agentNow = func() time.Time { return t0 }
	t.Cleanup(func() { agentNow = old })
	e := newAgentsEnv(t)
	e.fb.Accounts.Now = func() time.Time { return t0 }
	e.fb.Accounts.Items = accounts
	e.fb.Accounts.Stored = map[agents.AccountID]agentsapi.Usage{}
	e.m.width, e.m.height = w, h
	e.m.layout()
	return e
}

// settlePane feeds cmd's messages, and those of the commands they return,
// back into the model until nothing is left to deliver.
func (e *agentsEnv) settlePane(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	queue := flatten(cmd)
	for len(queue) > 0 {
		nm, c := e.m.Update(queue[0])
		e.m = nm.(Model)
		queue = append(queue[1:], flatten(c)...)
	}
}

func (e *agentsEnv) startPane(t *testing.T) {
	t.Helper()
	e.settlePane(t, e.m.initPane())
}

func TestPaneStartupWithFreshStoredMakesNoProviderCall(t *testing.T) {
	e := paneEnv(t, 120, 40, paneAccount("c1", "claude", "work"), paneAccount("x1", "codex", "main"))
	e.fb.Accounts.Stored["c1"] = paneUsage(2*time.Minute, 0.72, 0.41)
	e.fb.Accounts.Stored["x1"] = paneUsage(time.Minute, 0.9, 0.5)
	e.startPane(t)

	if f := e.fb.Accounts.Fetches(); len(f) != 0 {
		t.Errorf("provider fetches = %v, want a cache answer inside %v", f, agents.UsageMaxAge)
	}
	for _, c := range e.fb.Accounts.UsageCalls() {
		if c.Refresh {
			t.Errorf("pane refreshed %s; only the manual key may", c.ID)
		}
	}
	for _, id := range []agents.AccountID{"c1", "x1"} {
		en := e.m.agents.usage[id]
		if !en.Have || en.Stale() || en.Refreshing {
			t.Errorf("entry %s = %+v, want painted and settled", id, en)
		}
	}
	if got := e.m.agents.usage["c1"].Usage.FetchedAt; !got.Equal(t0.Add(-2 * time.Minute)) {
		t.Errorf("age not preserved: FetchedAt = %v", got)
	}
}

func TestPaneStartupWithOldStoredFetchesOnce(t *testing.T) {
	e := paneEnv(t, 120, 40, paneAccount("c1", "claude", "work"))
	e.fb.Accounts.Stored["c1"] = paneUsage(5*time.Minute, 0.1, 0.1)
	e.fb.Accounts.Usages["c1"] = paneUsage(0, 0.8, 0.6)
	e.startPane(t)

	if f := e.fb.Accounts.Fetches(); len(f) != 1 {
		t.Fatalf("fetches = %v, want one", f)
	}
	en := e.m.agents.usage["c1"]
	if f, _ := agents.RemainingFraction(en.Usage.Headline.Primary); !en.Have || f != 0.8 {
		t.Errorf("entry = %+v, want the fetched numbers", en)
	}
}

func TestPaneFailureKeepsLastGoodAndMarksStale(t *testing.T) {
	e := paneEnv(t, 120, 40, paneAccount("c1", "claude", "work"))
	e.fb.Accounts.Stored["c1"] = paneUsage(30*time.Second, 0.72, 0.41)
	e.startPane(t)

	var seq uint64
	e.m.agents.usage, seq = e.m.agents.usage.Begin("c1")
	nm, _ := e.m.Update(usagestore.Result{Seq: seq, Account: "c1", Err: errors.New("usage unavailable")})
	e.m = nm.(Model)

	en := e.m.agents.usage["c1"]
	if !en.Have || !en.Stale() || en.Message() != "usage unavailable" || en.Refreshing {
		t.Fatalf("entry = %+v, want last good data marked stale", en)
	}
	if f, _ := agents.RemainingFraction(en.Usage.Headline.Primary); f != 0.72 {
		t.Errorf("numbers changed on failure: %v", f)
	}
}

func TestPaneLateResultIsDropped(t *testing.T) {
	e := paneEnv(t, 120, 40, paneAccount("c1", "claude", "work"))
	e.fb.Accounts.Stored["c1"] = paneUsage(30*time.Second, 0.72, 0.41)
	e.startPane(t)

	cur := e.m.agents.usage["c1"].Seq
	for _, msg := range []usagestore.Result{
		{Seq: cur - 1, Account: "c1", Usage: paneUsage(0, 0.01, 0.01)},
		{Seq: cur - 1, Account: "c1", Err: errors.New("late failure")},
	} {
		nm, _ := e.m.Update(msg)
		e.m = nm.(Model)
	}
	en := e.m.agents.usage["c1"]
	if f, _ := agents.RemainingFraction(en.Usage.Headline.Primary); f != 0.72 || en.Stale() || en.Message() != "" {
		t.Fatalf("late result changed state: %+v", en)
	}
	// A stored answer arriving after a fetch must not replace it either.
	nm, _ := e.m.Update(usagestore.Result{Account: "c1", Stored: true, Found: true, Usage: paneUsage(time.Hour, 0.01, 0.01)})
	e.m = nm.(Model)
	if f, _ := agents.RemainingFraction(e.m.agents.usage["c1"].Usage.Headline.Primary); f != 0.72 {
		t.Errorf("stored answer overwrote a fetched one: %v", f)
	}
}

func TestPaneTickRefetchesOnlyPastTheCacheWindow(t *testing.T) {
	e := paneEnv(t, 120, 40, paneAccount("c1", "claude", "work"))
	e.fb.Accounts.Stored["c1"] = paneUsage(2*time.Minute, 0.72, 0.41)
	e.fb.Accounts.Usages["c1"] = paneUsage(0, 0.5, 0.5)
	e.startPane(t)
	tick := func() (fetched bool) {
		before := len(e.fb.Accounts.Fetches())
		gen := e.m.agents.pane.tickGen
		nm, cmd := e.m.Update(paneTickMsg{gen: gen})
		e.m = nm.(Model)
		e.settlePane(t, cmd)
		return len(e.fb.Accounts.Fetches()) > before
	}
	// Inside the 3 minute window the minute check is answered from storage.
	if tick() {
		t.Fatalf("early tick fetched from the provider, want a cache answer inside %v", agents.UsageMaxAge)
	}
	// One more minute and the snapshot is 3m+ old: the check refetches.
	e.fb.Accounts.Now = func() time.Time { return t0.Add(90 * time.Second) }
	if !tick() {
		t.Fatal("late tick did not refetch a snapshot past the window")
	}
	// A duplicate timer from an earlier generation does nothing.
	if _, cmd := e.m.Update(paneTickMsg{gen: e.m.agents.pane.tickGen - 1}); cmd != nil {
		t.Error("stale tick re-armed")
	}
}

func TestPaneListedAccountsAreOrderedAndPruned(t *testing.T) {
	e := paneEnv(t, 120, 40, paneAccount("g1", "antigravity", "a"), paneAccount("x1", "codex", "b"), paneAccount("c2", "claude", "z"), paneAccount("c1", "claude", "a"))
	e.startPane(t)
	var got []string
	for _, a := range e.m.agents.pane.accounts {
		got = append(got, string(a.ID))
	}
	if strings.Join(got, ",") != "c1,c2,x1,g1" {
		t.Fatalf("order = %v", got)
	}
	e.fb.Accounts.Items = e.fb.Accounts.Items[:1] // g1 only
	e.m.agents.pane.listSeq++
	e.settlePane(t, listPaneAccountsCmd(e.m.agents.ctx, e.m.agents.api.Accounts, e.m.agents.pane.listSeq))
	if len(e.m.agents.usage) != 1 {
		t.Fatalf("entries = %v, want removed accounts pruned", e.m.agents.usage)
	}
}

func TestPaneVisibilityThreshold(t *testing.T) {
	for _, tc := range []struct {
		h    int
		want bool
	}{{24, false}, {29, false}, {30, true}, {40, true}} {
		t.Run(fmt.Sprint(tc.h), func(t *testing.T) {
			e := paneEnv(t, 120, tc.h, paneAccount("c1", "claude", "work"))
			e.fb.Accounts.Stored["c1"] = paneUsage(time.Minute, 0.72, 0.41)
			e.startPane(t)
			if got := strings.Contains(ansi.Strip(e.m.View()), "Usage"); got != tc.want {
				t.Errorf("pane shown = %v at %d rows, want %v", got, tc.h, tc.want)
			}
		})
	}
}

func TestPaneHiddenWithoutAccounts(t *testing.T) {
	e := paneEnv(t, 120, 40)
	e.startPane(t)
	if rows := e.m.usagePaneRows(30); rows != 0 {
		t.Errorf("rows = %d, want 0", rows)
	}
}

func TestPaneLeftColumnHeightIsExact(t *testing.T) {
	for _, size := range [][2]int{{120, 30}, {120, 31}, {100, 40}, {160, 60}, {80, 30}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			e := paneEnv(t, size[0], size[1], paneAccount("c1", "claude", "work"), paneAccount("x1", "codex", "main"), paneAccount("x2", "codex", "side"))
			for _, id := range []agents.AccountID{"c1", "x1", "x2"} {
				e.fb.Accounts.Stored[id] = paneUsage(time.Minute, 0.72, 0.41)
			}
			e.startPane(t)
			_, _, innerH := e.m.dims()
			rows := e.m.usagePaneRows(innerH)
			if rows == 0 {
				t.Fatal("pane hidden")
			}
			lines := strings.Split(ansi.Strip(e.m.View()), "\n")
			if len(lines) != e.m.height {
				t.Fatalf("view rows = %d, want %d", len(lines), e.m.height)
			}
			// Left column: list box + pane must span innerH+2 rows with the
			// pane's bottom edge level with the right pane's.
			bottom := lines[innerH+1]
			if !strings.HasPrefix(bottom, "╰") || strings.Count(bottom, "╰") != 2 {
				t.Errorf("bottom row %q: borders disagree", bottom)
			}
			if w := lipgloss.Width(lines[0]); w > e.m.width {
				t.Errorf("row wider than terminal: %d", w)
			}
		})
	}
}

func TestPaneHiddenViewMatchesNoAccounts(t *testing.T) {
	with := paneEnv(t, 120, 24, paneAccount("c1", "claude", "work"))
	with.fb.Accounts.Stored["c1"] = paneUsage(time.Minute, 0.72, 0.41)
	with.startPane(t)
	without := paneEnv(t, 120, 24)
	without.startPane(t)
	if a, b := with.m.View(), without.m.View(); a != b {
		t.Errorf("view differs below the threshold:\n%s\n---\n%s", a, b)
	}
}

// leftColumn is the screen's left pane, styling stripped.
func (e *agentsEnv) leftColumn() string {
	leftW, _, _ := e.m.dims()
	var out []string
	// The symlink-resolved root: os.TempDir() differs from it on macOS
	// (/var -> /private/var) and Windows (8.3 short names).
	root := filepath.Dir(e.repo)
	for _, l := range strings.Split(e.m.View(), "\n") {
		l = ansi.Strip(ansi.Truncate(l, leftW, ""))
		// The pane truncates long paths, so the root may be cut short.
		if p := strings.Trim(l, "│ …"); len(p) > 3 && (strings.HasPrefix(p, root) || strings.HasPrefix(root, p)) {
			// Worktree paths are per-run temp dirs.
			l = "│  <path>" + strings.Repeat(" ", max(leftW-ansi.StringWidth("│  <path>")-1, 0)) + "│"
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

func TestPaneGoldens(t *testing.T) {
	two := func(t *testing.T) *agentsEnv {
		e := paneEnv(t, 120, 40, paneAccount("c1", "claude", "work-claude"), paneAccount("x1", "codex", "codex-main"))
		e.fb.Accounts.Stored["c1"] = paneUsage(20*time.Second, 0.72, 0.41)
		e.fb.Accounts.Stored["x1"] = paneUsage(90*time.Second, 0.98, 0.12)
		e.startPane(t)
		return e
	}
	for _, tc := range []struct {
		name string
		make func(t *testing.T) *agentsEnv
	}{
		{"usagepane-two-accounts-120x40", two},
		{"usagepane-stale-loading-120x40", func(t *testing.T) *agentsEnv {
			e := paneEnv(t, 120, 40, paneAccount("c1", "claude", "work-claude"), paneAccount("x1", "codex", "codex-main"))
			e.fb.Accounts.Stored["c1"] = paneUsage(5*time.Minute, 0.72, 0.41)
			e.startPane(t)
			// c1 went stale on a failed refresh; x1 never loaded.
			en := e.m.agents.usage["c1"]
			en.Usage.Stale, en.Usage.Err = true, "usage unavailable"
			e.m.agents.usage["c1"] = en
			e.m.agents.usage["x1"] = usagestore.Entry{Refreshing: true, Seq: 99}
			e.m.layout()
			return e
		}},
		{"usagepane-degraded-120x30", func(t *testing.T) *agentsEnv {
			e := paneEnv(t, 120, 30, paneAccount("c1", "claude", "work-claude"), paneAccount("c2", "claude", "personal"), paneAccount("x1", "codex", "codex-main"), paneAccount("a1", "antigravity", "ag"))
			for _, id := range []agents.AccountID{"c1", "c2", "x1", "a1"} {
				e.fb.Accounts.Stored[id] = paneUsage(time.Minute, 0.72, 0.41)
			}
			e.startPane(t)
			return e
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compareGolden(t, tc.name, tc.make(t).leftColumn())
		})
	}
}

func TestPaneLeavesRightColumnUnchanged(t *testing.T) {
	for _, size := range [][2]int{{80, 30}, {120, 40}, {160, 60}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			e := paneEnv(t, size[0], size[1], paneAccount("c1", "claude", "personal"), paneAccount("x1", "codex", "work"))
			for _, id := range []agents.AccountID{"c1", "x1"} {
				e.fb.Accounts.Stored[id] = paneUsage(time.Minute, .72, .96)
			}
			e.startPane(t)
			leftW, _, innerH := e.m.dims()
			paneRows := e.m.usagePaneRows(innerH)
			if paneRows == 0 {
				t.Fatal("pane must be visible for layout comparison")
			}
			with := strings.Split(ansi.Strip(e.m.View()), "\n")
			if left := e.leftColumn(); strings.Index(left, "Usage") <= strings.Index(left, "Worktrees") {
				t.Fatalf("usage pane must follow Worktrees:\n%s", left)
			}
			e.m.agents.pane.accounts = nil
			e.m.layout()
			without := strings.Split(ansi.Strip(e.m.View()), "\n")
			for i := range innerH + 2 {
				if ansi.Cut(with[i], leftW, size[0]) != ansi.Cut(without[i], leftW, size[0]) {
					t.Errorf("right column changed on row %d:\nwith: %s\nwithout: %s", i, with[i], without[i])
				}
			}
			if strings.Contains(e.leftColumn(), "Usage") || e.m.usagePaneRows(innerH) != 0 ||
				!strings.HasPrefix(without[innerH+1], "╰") {
				t.Fatal("hidden pane must leave no chrome and return its space to Worktrees")
			}
		})
	}
}
