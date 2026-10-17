package accounts

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi/fake"
)

func quickClock() time.Time { return testNow }

// quickFake adds a second, stale Antigravity profile to the usage list.
func quickFake() *fake.Accounts {
	api := mockupFake()
	api.Items = append(api.Items, agentsapi.Account{Account: agents.Account{ID: "a2", Provider: "antigravity", Name: "work"}})
	api.Usages["a2"] = agentsapi.Usage{
		Snapshot: agents.UsageSnapshot{Limits: []agents.UsageLimit{
			{ID: "pro", Group: "Gemini", Label: "Gemini Pro", RemainingFraction: ptr(0.9)},
			{ID: "flash", Group: "Gemini", Label: "Flash", RemainingFraction: ptr(0.7)},
		}},
		FetchedAt: testNow.Add(-time.Hour), Stale: true, Err: "rate limited",
	}
	return api
}

func newQuick(api agentsapi.Accounts, focus agents.AccountID, ascii bool, w, h int) Quick {
	q := NewQuick(api, focus, ascii).WithClock(quickClock)
	q.SetSize(w, h)
	return q
}

// quickDrain runs cmd and feeds every message back, like drain does for Model.
func quickDrain(q Quick, cmd tea.Cmd) (Quick, []tea.Msg) {
	var seen []tea.Msg
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg := c()
		if batch, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		seen = append(seen, msg)
		var next tea.Cmd
		q, next = q.Update(msg)
		queue = append(queue, next)
	}
	return q, seen
}

func quickKey(q Quick, k string) (Quick, []tea.Msg) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	if k == "esc" {
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	}
	var cmd tea.Cmd
	q, cmd = q.Update(msg)
	return quickDrain(q, cmd)
}

func quickPlain(q Quick) string { return ansi.Strip(q.View()) }

func opennedQuick(t *testing.T, api agentsapi.Accounts, focus agents.AccountID, ascii bool) Quick {
	t.Helper()
	q := newQuick(api, focus, ascii, 68, 20)
	q, _ = quickDrain(q, q.Init())
	return q
}

func order(t *testing.T, out string, parts ...string) {
	t.Helper()
	at := -1
	for _, p := range parts {
		i := strings.Index(out, p)
		if i < 0 || i < at {
			t.Fatalf("%q missing or out of order in:\n%s", p, out)
		}
		at = i
	}
}

func TestQuickLoadsAndOrders(t *testing.T) {
	api := quickFake()
	q := opennedQuick(t, api, "c1", false)
	out := quickPlain(q)
	for _, want := range []string{"USAGE", "Claude · work", "5-hour", "███████░░░ · Resets in 2h14m", "Weekly", "41% remaining",
		"Updated 35 seconds ago", "↑↓ Profile · r Refresh · m Profiles · Esc Close", "Gemini Pro 12% · Flash 80%", "stale"} {
		if !strings.Contains(out, want) {
			t.Errorf("view lacks %q:\n%s", want, out)
		}
	}
	// The list always contains all profiles in provider/name order.
	order(t, out, "Claude · work", "personal  5h ███████░░░", "personal  5h ██░░░░░░░░", "work      5h █████████░", "personal  Gemini", "work      Gemini")
	if strings.Count(out, "work") < 3 {
		t.Fatalf("unexpected rows:\n%s", out)
	}
	for _, c := range api.UsageCalls() {
		if c.Refresh {
			t.Errorf("initial load refreshed %s", c.ID)
		}
	}
	if got := len(api.UsageCalls()); got != 6 {
		t.Errorf("usage calls = %d, want one per account (6)", got)
	}
}

// gateAccounts blocks every Usage call until released and counts how many are
// in flight at once.
type gateAccounts struct {
	*fake.Accounts
	mu      sync.Mutex
	cur     int
	peak    int
	gates   map[agents.AccountID]chan struct{}
	started chan agents.AccountID
}

func newGate(a *fake.Accounts) *gateAccounts {
	g := &gateAccounts{Accounts: a, gates: map[agents.AccountID]chan struct{}{}, started: make(chan agents.AccountID, 32)}
	for _, it := range a.Items {
		g.gates[it.ID] = make(chan struct{})
	}
	return g
}

func (g *gateAccounts) Usage(ctx context.Context, id agents.AccountID, refresh bool) (agentsapi.Usage, error) {
	g.mu.Lock()
	g.cur++
	g.peak = max(g.peak, g.cur)
	g.mu.Unlock()
	g.started <- id
	select {
	case <-g.gates[id]:
	case <-ctx.Done():
	}
	g.mu.Lock()
	g.cur--
	g.mu.Unlock()
	return g.Accounts.Usage(ctx, id, refresh)
}

func (g *gateAccounts) inflight() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.cur
}

// runner executes commands on goroutines, like the Bubble Tea runtime, and
// hands the messages back one at a time.
type runner struct{ out chan tea.Msg }

func newRunner() *runner { return &runner{out: make(chan tea.Msg, 64)} }

func (r *runner) spawn(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		msg := cmd()
		if b, ok := msg.(tea.BatchMsg); ok {
			for _, c := range b {
				r.spawn(c)
			}
			return
		}
		r.out <- msg
	}()
}

func (r *runner) next(t *testing.T) tea.Msg {
	t.Helper()
	select {
	case m := <-r.out:
		return m
	case <-time.After(2 * time.Second):
		t.Fatal("no message arrived")
	}
	return nil
}

func nothingStarted(t *testing.T, g *gateAccounts) {
	t.Helper()
	select {
	case id := <-g.started:
		t.Fatalf("usage call for %s started above the cap", id)
	case <-time.After(60 * time.Millisecond):
	}
}

func TestQuickConcurrencyCap(t *testing.T) {
	g := newGate(quickFake())
	r := newRunner()
	q := newQuick(g, "c1", false, 68, 20)
	r.spawn(q.Init())
	var cmd tea.Cmd
	q, cmd = q.Update(r.next(t)) // the list
	r.spawn(cmd)

	// 6 accounts, 3 slots: the focused one is among the first three.
	first := make([]agents.AccountID, 0, 3)
	for range quickInflight {
		first = append(first, <-g.started)
	}
	// Goroutines start in any order; the sequence numbers show dispatch order.
	for id, seq := range q.usageSeq {
		if id != "c1" && seq < q.usageSeq["c1"] {
			t.Errorf("%s dispatched before the focused account c1", id)
		}
	}
	nothingStarted(t, g)

	// A refresh while the slots are full queues work; it must not exceed the cap.
	q, cmd = q.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	r.spawn(cmd)
	nothingStarted(t, g)
	if got := quickPlain(q); !strings.Contains(got, "Refreshing…") {
		t.Errorf("busy overlay should say Refreshing…:\n%s", got)
	}

	// Release one call at a time; every result frees exactly one slot.
	released := map[agents.AccountID]bool{}
	release := func(id agents.AccountID) {
		if !released[id] {
			released[id] = true
			close(g.gates[id])
		}
	}
	for _, id := range first {
		release(id)
	}
	pending := len(first)
	for pending > 0 {
		var next tea.Cmd
		q, next = q.Update(r.next(t))
		pending--
		r.spawn(next)
		// Each new dispatch shows up as a started call; collect and release.
		for {
			select {
			case id := <-g.started:
				release(id)
				pending++
				continue
			case <-time.After(60 * time.Millisecond):
			}
			break
		}
		if g.inflight() > quickInflight {
			t.Fatalf("in flight = %d, cap %d", g.inflight(), quickInflight)
		}
	}
	if g.peak > quickInflight {
		t.Errorf("peak in flight = %d, cap %d", g.peak, quickInflight)
	}
	refreshed := map[agents.AccountID]bool{}
	for _, c := range g.UsageCalls() {
		if c.Refresh {
			refreshed[c.ID] = true
		}
	}
	if len(refreshed) != 6 {
		t.Errorf("refreshed accounts = %v, want all 6", refreshed)
	}
	if got := quickPlain(q); strings.Contains(got, "Refreshing…") || !strings.Contains(got, "Updated 35 seconds ago") {
		t.Errorf("idle overlay:\n%s", got)
	}
}

func TestQuickLateResultsAreDropped(t *testing.T) {
	q := opennedQuick(t, quickFake(), "c1", false)
	cur := q.usageSeq["x2"]
	if cur == 0 {
		t.Fatal("no request recorded for x2")
	}
	old := agentsapi.Usage{Headline: agents.NewUsageHeadline(&agents.UsageLimit{ID: "5h", Window: "5h", RemainingFraction: ptr(0.01)}, nil, nil), FetchedAt: testNow}

	// An answer older than the latest request for that account changes nothing.
	q2, _ := q.Update(quickUsageMsg{id: "x2", seq: cur - 1, usage: old})
	if quickPlain(q2) != quickPlain(q) {
		t.Errorf("late result changed the view:\n%s", quickPlain(q2))
	}
	// A late answer for one account does not touch another.
	q2, _ = q.Update(quickUsageMsg{id: "x1", seq: q.usageSeq["x1"] - 1, usage: old})
	if quickPlain(q2) != quickPlain(q) {
		t.Errorf("late result for x1 changed the view")
	}
	// The current one applies.
	q2, _ = q.Update(quickUsageMsg{id: "x2", seq: cur, usage: old})
	if !strings.Contains(quickPlain(q2), "5h ░░░░░░░░░░") {
		t.Errorf("current result not applied:\n%s", quickPlain(q2))
	}
	// A late list is dropped too.
	q2, _ = q.Update(quickListMsg{seq: q.listSeq - 1, accounts: nil})
	if len(q2.accounts) != len(q.accounts) {
		t.Error("late list replaced the accounts")
	}
}

func TestQuickLateResultFreesSlot(t *testing.T) {
	q := opennedQuick(t, quickFake(), "c1", false)
	q.inflight = 3
	q.queue = []usageJob{{id: "c2"}}
	q, cmd := q.Update(quickUsageMsg{id: "x2", seq: -1}) // dropped, but its slot frees
	if cmd == nil || q.inflight != 3 {
		t.Errorf("dropped result must free its slot for the queue: inflight=%d cmd=%v", q.inflight, cmd != nil)
	}
}

func TestQuickFailureKeepsStaleData(t *testing.T) {
	api := quickFake()
	q := opennedQuick(t, api, "c1", false)
	before := quickPlain(q)
	api.UsageErr = errString("boom at /home/me/.claude/secret.json")
	q, _ = quickKey(q, "r")
	out := quickPlain(q)
	if !strings.Contains(out, "███████░░░") || !strings.Contains(out, "5h ██░░░░░░░░") {
		t.Errorf("failed refresh dropped the last good numbers:\n%s", out)
	}
	if strings.Count(out, "stale") < 6 { // header marker + 5 rows? every account is stale
		t.Errorf("every account should be stale:\n%s", out)
	}
	if strings.Contains(out, "/home/me") || strings.Contains(out, "secret.json") {
		t.Errorf("error leaked a path:\n%s", out)
	}
	if before == out {
		t.Error("view did not change after a failed refresh")
	}
	// A later success clears the stale marks.
	api.UsageErr = nil
	q, _ = quickKey(q, "r")
	if strings.Contains(quickPlain(q), "boom") {
		t.Error("error survived a good refresh")
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestQuickStaleRowsAreDimmedAndLabelled(t *testing.T) {
	q := opennedQuick(t, quickFake(), "c1", false)
	var staleRow, goodRow string
	for _, l := range strings.Split(q.View(), "\n") {
		switch {
		case strings.Contains(ansi.Strip(l), "work      Gemini"):
			staleRow = l
		case strings.Contains(ansi.Strip(l), "personal  Gemini"):
			goodRow = l
		}
	}
	if !strings.Contains(ansi.Strip(staleRow), "stale") || strings.Contains(ansi.Strip(goodRow), "stale") {
		t.Fatalf("stale label: stale=%q good=%q", ansi.Strip(staleRow), ansi.Strip(goodRow))
	}
}

func TestQuickFirstFailureWithoutData(t *testing.T) {
	api := quickFake()
	delete(api.Usages, "x1")
	q := opennedQuick(t, api, "c1", false)
	if out := quickPlain(q); !strings.Contains(out, "unavailable") {
		t.Errorf("account without data should say unavailable:\n%s", out)
	}
}

func TestQuickKeys(t *testing.T) {
	q := opennedQuick(t, quickFake(), "c1", false)

	_, msgs := quickKey(q, "m")
	if len(msgs) != 1 || msgs[0] != (OpenAccountsMsg{Account: "c1"}) {
		t.Fatalf("m = %#v", msgs)
	}
	// j moves the focus along the display order: provider, then name, so
	// claude personal (c2) < claude work (c1) < codex personal (x2).
	q, _ = quickKey(q, "j")
	if q.focus != "x2" {
		t.Fatalf("focus after j = %s, want x2", q.focus)
	}
	if out := quickPlain(q); !strings.Contains(out, "Codex · personal") {
		t.Errorf("focused account not on top:\n%s", out)
	}
	_, msgs = quickKey(q, "m")
	if len(msgs) != 1 || msgs[0] != (OpenAccountsMsg{Account: "x2"}) {
		t.Fatalf("m after j = %#v", msgs)
	}
	q, _ = quickKey(q, "k")
	q, _ = quickKey(q, "k")
	q, _ = quickKey(q, "k") // clamped at the first account
	if q.focus != "c2" {
		t.Errorf("focus after k,k,k = %s", q.focus)
	}
	for range 10 {
		q, _ = quickKey(q, "j")
	}
	if q.focus != "a2" {
		t.Errorf("focus clamps at the last account, got %s", q.focus)
	}

	for _, k := range []string{"esc", "u"} {
		if _, msgs := quickKey(q, k); len(msgs) != 1 || msgs[0] != (CloseMsg{}) {
			t.Errorf("%s = %#v, want CloseMsg", k, msgs)
		}
	}
}

func TestQuickRefreshAsksProvider(t *testing.T) {
	api := quickFake()
	q := opennedQuick(t, api, "c1", false)
	n := len(api.UsageCalls())
	q, _ = quickKey(q, "r")
	calls := api.UsageCalls()[n:]
	if len(calls) != 6 {
		t.Fatalf("refresh calls = %d, want 6", len(calls))
	}
	for _, c := range calls {
		if !c.Refresh {
			t.Errorf("%s refreshed without refresh=true", c.ID)
		}
	}
	_ = q
}

func TestQuickListErrorAndEmpty(t *testing.T) {
	api := quickFake()
	api.ListErr = errString("down at /var/lib/x/y")
	q := opennedQuick(t, api, "c1", false)
	out := quickPlain(q)
	if !strings.Contains(out, "Couldn't load accounts") || strings.Contains(out, "/var/lib") {
		t.Errorf("list error view:\n%s", out)
	}
	api.ListErr = nil
	q, _ = quickKey(q, "r") // retries the list
	if out := quickPlain(q); !strings.Contains(out, "Claude · work") {
		t.Errorf("r should retry the list:\n%s", out)
	}

	empty := opennedQuick(t, &fake.Accounts{}, "", false)
	if out := quickPlain(empty); !strings.Contains(out, "No accounts yet.") {
		t.Errorf("empty:\n%s", out)
	}
}

func TestQuickUnknownFocusFallsBack(t *testing.T) {
	q := opennedQuick(t, quickFake(), "nope", false)
	if q.focus != "c2" { // claude personal sorts first
		t.Errorf("focus = %s, want first account in display order", q.focus)
	}
}

func TestQuickFocusedAntigravityShowsPools(t *testing.T) {
	q := opennedQuick(t, quickFake(), "a1", false)
	out := quickPlain(q)
	if !strings.Contains(out, "Antigravity · personal") || !strings.Contains(out, "Gemini Pro") {
		t.Errorf("pool meters missing:\n%s", out)
	}
}

func TestQuickASCII(t *testing.T) {
	q := opennedQuick(t, quickFake(), "c1", true)
	out := quickPlain(q)
	for _, r := range []string{"█", "░", "✳", "◎", "✦"} {
		if strings.Contains(out, r) {
			t.Errorf("ascii view has %q", r)
		}
	}
	if !strings.Contains(out, "#") || !strings.Contains(out, ".") {
		t.Errorf("ascii bar missing:\n%s", out)
	}
}

func TestQuickNoSecretsRendered(t *testing.T) {
	api := quickFake()
	api.Items[0].Info = agents.AccountInfo{Identity: "work@example.com", Options: map[string]any{"token": "sk-secret"}}
	q := opennedQuick(t, api, "c1", false)
	out := quickPlain(q)
	for _, s := range []string{"sk-secret", "work@example.com"} {
		if strings.Contains(out, s) {
			t.Errorf("view shows %q", s)
		}
	}
}

func TestQuickSizeInvariants(t *testing.T) {
	loaded := opennedQuick(t, quickFake(), "c1", false)
	fresh := newQuick(quickFake(), "c1", false, 0, 0)
	for _, base := range []Quick{fresh, loaded} {
		for _, sz := range [][2]int{{80, 24}, {120, 40}, {68, 20}, {44, 14}, {30, 12}, {20, 10}, {12, 8}, {6, 6}, {200, 60}, {80, 12}} {
			q := base
			q.SetSize(sz[0], sz[1])
			out := q.View()
			lines := strings.Split(out, "\n")
			if out == "" {
				continue
			}
			if len(lines) != sz[1] {
				t.Errorf("%v: %d lines, want full pane height", sz, len(lines))
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w != sz[0] {
					t.Errorf("%v line %d width %d: %q", sz, i, w, ansi.Strip(l))
				}
			}
		}
	}
	q := loaded
	q.SetSize(80, 24)
	lines := strings.Split(q.View(), "\n")
	if w := ansi.StringWidth(lines[0]); w != 80 {
		t.Errorf("wide width = %d, want %d", w, 80)
	}
	if len(lines) != 24 {
		t.Errorf("height = %d, want 24 for the full pane", len(lines))
	}
}

func TestQuickTruncatesListWhenShort(t *testing.T) {
	q := opennedQuick(t, quickFake(), "c1", false)
	q.SetSize(64, 10) // frame of 10 rows: 5 body rows, fewer than the list needs
	out := quickPlain(q)
	if got := len(strings.Split(out, "\n")); got > 10 {
		t.Errorf("lines = %d, want <= 10", got)
	}
}

func TestGoldenQuickUsage(t *testing.T) {
	q := newQuick(quickFake(), "c1", false, 64, 16) // a 64-wide frame
	q, _ = quickDrain(q, q.Init())
	checkGolden(t, "quick-usage-64x16", quickPlain(q))
}

func TestQuickNavigationKeepsRowsStableAndSelectionVisible(t *testing.T) {
	q := opennedQuick(t, quickFake(), "c1", false)
	before := strings.Split(quickPlain(q), "\n")
	for _, key := range []tea.KeyType{tea.KeyDown, tea.KeyUp, tea.KeyUp, tea.KeyDown} {
		q, _ = q.Update(tea.KeyMsg{Type: key})
		after := strings.Split(quickPlain(q), "\n")
		// The title/spacer and fixed detail block occupy six rows. Every
		// profile keeps its position; only its selection marker changes.
		for i := 6; i < 12; i++ {
			if strings.TrimLeft(before[i], " >") != strings.TrimLeft(after[i], " >") {
				t.Fatalf("profile row %d moved on %v:\nbefore: %q\nafter: %q", i, key, before[i], after[i])
			}
		}
		if strings.Count(strings.Join(after, "\n"), "> ") != 1 {
			t.Fatal("usage list must show exactly one selection marker")
		}
		before = after
	}

	q.SetSize(64, 11) // two profile rows fit between details and footer
	for _, key := range []tea.KeyType{tea.KeyDown, tea.KeyUp} {
		for range len(q.accounts) {
			q, _ = q.Update(tea.KeyMsg{Type: key})
			out := quickPlain(q)
			if !strings.Contains(out, "> ") {
				t.Fatalf("selected profile %s scrolled out:\n%s", q.focus, out)
			}
		}
	}
}
