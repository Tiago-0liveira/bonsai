package accounts

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
)

// activeTab names the tab the underline sits under.
func activeTab(out string) string {
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		col := strings.IndexAny(l, "━=")
		if i == 0 || col < 0 || strings.Trim(l, " ━=") != "" {
			continue
		}
		prev := lines[i-1]
		for _, name := range []string{"Overview", "Usage", "Defaults"} {
			if at := strings.Index(prev, name); at == col {
				return name
			}
		}
	}
	return ""
}

// hasRow reports a line that mentions every word.
func hasRow(out string, words ...string) bool {
	for _, l := range strings.Split(out, "\n") {
		ok := true
		for _, w := range words {
			ok = ok && strings.Contains(l, w)
		}
		if ok {
			return true
		}
	}
	return false
}

func tableFixture(n int) []agentsapi.Account {
	providers := []agents.ProviderID{"claude", "codex", "antigravity"}
	var out []agentsapi.Account
	for i := range n {
		out = append(out, acct(fmt.Sprintf("a%02d", i), string(providers[i%3]), fmt.Sprintf("acct%02d", i), "logged_in"))
	}
	return out
}

func testTable(accts []agentsapi.Account, sel agents.AccountID, ascii bool) Table {
	return Table{
		Accounts: accts, Selected: sel, ASCII: ascii, Focused: true,
		Columns: []Column{
			{Title: "PLAN", Width: 9, Optional: true, Cell: func(agentsapi.Account) string { return "Pro" }},
			{Title: "CONNECTION", Width: 15, Under: true, Cell: func(agentsapi.Account) string { return "● Connected" }},
		},
	}
}

func TestTableShapesAcrossSizes(t *testing.T) {
	accts := tableFixture(9)
	for _, tc := range []struct{ w, h int }{{76, 13}, {136, 29}, {76, 6}, {76, 3}, {76, 1}, {40, 9}, {26, 12}} {
		tb := testTable(accts, accts[4].ID, false)
		lines := tb.View(tc.w, tc.h)
		if len(lines) == 0 || len(lines) > tc.h {
			t.Fatalf("%dx%d: %d lines", tc.w, tc.h, len(lines))
		}
		for _, l := range lines {
			if ansi.StringWidth(l) != tc.w {
				t.Fatalf("%dx%d: line is %d cells: %q", tc.w, tc.h, ansi.StringWidth(l), l)
			}
		}
		if out := ansi.Strip(strings.Join(lines, "\n")); !strings.Contains(out, "acct04") {
			t.Fatalf("%dx%d: selected row scrolled out:\n%s", tc.w, tc.h, out)
		}
	}
}

func TestTableWideShowsEveryRowWithoutIndicator(t *testing.T) {
	accts := tableFixture(9)
	out := ansi.Strip(strings.Join(testTable(accts, accts[0].ID, false).View(136, 29), "\n"))
	for _, want := range []string{"ACCOUNT", "PROVIDER", "PLAN", "CONNECTION", "▸ acct00", "acct08"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, " of ") {
		t.Fatalf("no rows hidden, no indicator wanted:\n%s", out)
	}
}

func TestTableScrollsWithRangeIndicator(t *testing.T) {
	accts := tableFixture(12)
	for _, sel := range []int{0, 5, 11} {
		out := ansi.Strip(strings.Join(testTable(accts, accts[sel].ID, false).View(76, 8), "\n"))
		if !strings.Contains(out, fmt.Sprintf("▸ acct%02d", sel)) {
			t.Fatalf("selected row %d not visible:\n%s", sel, out)
		}
		if !strings.Contains(out, "of 12") {
			t.Fatalf("hidden rows need a range indicator:\n%s", out)
		}
		if len(strings.Split(out, "\n")) > 8 {
			t.Fatalf("table exceeds its height:\n%s", out)
		}
	}
}

func TestTableVeryShortShowsOnlySelectedRow(t *testing.T) {
	accts := tableFixture(9)
	out := ansi.Strip(strings.Join(testTable(accts, accts[3].ID, false).View(76, 1), "\n"))
	if !strings.Contains(out, "acct03") || strings.Contains(out, "ACCOUNT") {
		t.Fatalf("one line must be the selected row:\n%s", out)
	}
}

func TestTableNarrowDropsPlanThenFoldsConnection(t *testing.T) {
	accts := tableFixture(3)
	tb := testTable(accts, accts[0].ID, false)

	// Wide enough for every column.
	if out := ansi.Strip(strings.Join(tb.View(60, 8), "\n")); !strings.Contains(out, "PLAN") {
		t.Fatalf("plan expected at 60:\n%s", out)
	}
	// Plan goes first, connection stays in the row.
	out := ansi.Strip(strings.Join(tb.View(50, 8), "\n"))
	if strings.Contains(out, "PLAN") || !strings.Contains(out, "CONNECTION") {
		t.Fatalf("plan must drop before connection at 50:\n%s", out)
	}
	// Connection then moves under the name: two lines per row, no heading.
	out = ansi.Strip(strings.Join(tb.View(34, 12), "\n"))
	if strings.Contains(out, "CONNECTION") || !strings.Contains(out, "● Connected") || !strings.Contains(out, "acct00") {
		t.Fatalf("connection must fold under the name at 34:\n%s", out)
	}
	if got := strings.Count(out, "Connected"); got != 3 {
		t.Fatalf("one folded connection per row, got %d:\n%s", got, out)
	}
	if tb.Fits(20) {
		t.Fatal("20 cells cannot hold a readable row")
	}
}

func TestTableLongNamesAreElided(t *testing.T) {
	long := agentsapi.Account{Account: agents.Account{ID: "l", Provider: "codex", Name: strings.Repeat("verylongname", 8)}}
	tb := testTable([]agentsapi.Account{long}, "l", false)
	for _, w := range []int{76, 136, 50} {
		lines := tb.View(w, 4)
		out := ansi.Strip(strings.Join(lines, "\n"))
		if !strings.Contains(out, "…") || !strings.Contains(out, "Codex") {
			t.Fatalf("w=%d: long name must elide and keep the provider:\n%s", w, out)
		}
		for _, l := range lines {
			if ansi.StringWidth(l) != w {
				t.Fatalf("w=%d: line is %d cells", w, ansi.StringWidth(l))
			}
		}
	}
}

func TestTableEmptyAndASCII(t *testing.T) {
	out := ansi.Strip(strings.Join(testTable(nil, "", false).View(76, 5), "\n"))
	if !strings.Contains(out, "No accounts yet.") {
		t.Fatalf("empty table:\n%s", out)
	}
	accts := tableFixture(2)
	out = ansi.Strip(strings.Join(testTable(accts, accts[0].ID, true).View(76, 5), "\n"))
	if !strings.Contains(out, "> acct00") || strings.Contains(out, "▸") {
		t.Fatalf("ascii marker:\n%s", out)
	}
}

func TestViewAtTargetSizes(t *testing.T) {
	api := mockupFake()
	for i := range 6 {
		api.Items = append(api.Items, acct(fmt.Sprintf("n%d", i), "antigravity", fmt.Sprintf("extra%d", i), "logged_in"))
	}
	for _, size := range [][2]int{{76, 24}, {136, 40}, {60, 24}, {40, 20}, {76, 12}} {
		for _, tab := range []Tab{TabOverview, TabUsage, TabDefaults} {
			m := New(api, Options{Initial: "c2", Tab: tab, Now: func() time.Time { return testNow }})
			m.SetSize(size[0], size[1])
			m, _ = drain(m, m.Init())
			out := plain(m)
			assertBox(t, strings.Split(out, "\n"), size[0], size[1])
			if !strings.Contains(out, "▸ personal") && !strings.Contains(out, "personal") {
				t.Fatalf("%v tab %d: selection missing:\n%s", size, tab, out)
			}
			if !strings.Contains(out, "Accounts") {
				t.Fatalf("%v tab %d: no title:\n%s", size, tab, out)
			}
			if size[0] >= 60 && activeTab(out) == "" && size[1] >= 20 {
				t.Fatalf("%v tab %d: no underline:\n%s", size, tab, out)
			}
		}
	}
}

func TestViewManyAccountsKeepsSelectionAndInspector(t *testing.T) {
	api := mockupFake()
	for i := range 12 {
		api.Items = append(api.Items, acct(fmt.Sprintf("n%d", i), "antigravity", fmt.Sprintf("extra%d", i), "logged_in"))
	}
	m := opened(t, api, 76, 24)
	for range 16 {
		m, _ = press(m, "j")
	}
	out := plain(m)
	for _, want := range []string{"▸ extra11", "of 17", "Antigravity / extra11", "Identity", "Sign in again"} {
		if !strings.Contains(out, want) {
			t.Fatalf("view lacks %q:\n%s", want, out)
		}
	}
}

func TestViewNarrowFallsBackToSelector(t *testing.T) {
	m := opened(t, newFake(), 18, 24)
	out := plain(m)
	if strings.Contains(out, "ACCOUNT") || !strings.Contains(out, "↑↓ ✹ Claude") {
		t.Fatalf("expected the single-account selector:\n%s", out)
	}
	assertBox(t, strings.Split(out, "\n"), 18, 24)
}

func TestViewEmptyList(t *testing.T) {
	api := newFake()
	api.Items = nil
	m := opened(t, api, 76, 24)
	assertBox(t, strings.Split(plain(m), "\n"), 76, 24)
}

func TestFooterFollowsFocus(t *testing.T) {
	m := opened(t, newFake(), 100, 30)
	if out := plain(m); !strings.Contains(out, "Enter Details") || strings.Contains(out, "Esc Accounts") {
		t.Fatalf("table focus footer:\n%s", out)
	}
	m, _ = press(m, "enter", "2")
	if out := plain(m); !strings.Contains(out, "r Refresh") || strings.Contains(out, "Enter Details") || strings.Count(out, "Refresh") != 1 {
		t.Fatalf("inspector focus footer must hold the only refresh action:\n%s", out)
	}
}
