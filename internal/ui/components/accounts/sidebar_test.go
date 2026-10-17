package accounts

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
)

func acct(id, provider, name, auth string) agentsapi.Account {
	a := agentsapi.Account{Account: agents.Account{
		ID: agents.AccountID(id), Provider: agents.ProviderID(provider), Name: name,
	}}
	if auth != "" {
		a.Info = agents.AccountInfo{Options: map[string]any{"auth_status": auth}}
	}
	return a
}

func usageOf(remaining float64, stale bool) agentsapi.Usage {
	return agentsapi.Usage{
		Headline: agents.UsageHeadline{Primary: &agents.UsageLimit{Window: "5h", RemainingFraction: &remaining}},
		Stale:    stale,
	}
}

func fixture() []agentsapi.Account {
	// Deliberately interleaved: grouping must not depend on input order.
	return []agentsapi.Account{
		acct("c1", "claude", "work", "logged_in"),
		acct("x1", "codex", "work", "logged_out"),
		acct("c2", "claude", "personal", ""),
		acct("a1", "antigravity", "personal", "logged_in"),
	}
}

func TestSidebarGroupOrderAndLayout(t *testing.T) {
	s := Sidebar{Accounts: fixture(), Selected: "c1"}
	lines := s.View(24, 20)
	assertBox(t, lines, 24, 20)

	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = strings.TrimRight(stripLine(l), " ")
	}
	// The dot is right-aligned in the last of the 24 cells.
	row := func(left string) string {
		return left + strings.Repeat(" ", 23-ansi.StringWidth(left)) + "●"
	}
	want := []string{
		"ACCOUNTS",
		"",
		"CLAUDE",
		row("> ✳ work"),
		row("  ✳ personal"),
		"",
		"CODEX",
		row("  ◎ work"),
		"",
		"ANTIGRAVITY",
		row("  ✦ personal"),
	}
	for i, w := range want {
		if plain[i] != w {
			t.Fatalf("line %d: got %q want %q", i, plain[i], w)
		}
	}
	if plain[len(plain)-1] != "+ Add account" {
		t.Fatalf("add row not pinned: %q", plain[len(plain)-1])
	}
}

func TestSidebarOrdered(t *testing.T) {
	var ids []string
	for _, a := range (Sidebar{Accounts: fixture()}).Ordered() {
		ids = append(ids, string(a.ID))
	}
	if got := strings.Join(ids, ","); got != "c1,c2,x1,a1" {
		t.Fatalf("order: %s", got)
	}
}

func TestSidebarUnknownProviderLast(t *testing.T) {
	s := Sidebar{Accounts: []agentsapi.Account{acct("z", "zeta", "n", ""), acct("c", "claude", "n", "")}}
	got := s.Ordered()
	if got[0].ID != "c" || got[1].ID != "z" {
		t.Fatalf("order: %v %v", got[0].ID, got[1].ID)
	}
	if !strings.Contains(strings.Join(s.View(24, 12), "\n"), "ZETA") {
		t.Fatal("unknown provider group missing")
	}
}

func TestSidebarAddSelected(t *testing.T) {
	s := Sidebar{Accounts: fixture(), Selected: "c1", AddSelected: true}
	lines := s.View(24, 20)
	if got := strings.TrimSpace(stripLine(lines[len(lines)-1])); got != "> + Add account" {
		t.Fatalf("add row: %q", got)
	}
	for _, l := range lines {
		if strings.Contains(stripLine(l), "> ✳") {
			t.Fatalf("account marker still shown: %q", l)
		}
	}
}

func TestSidebarUsageOnlyWhenWide(t *testing.T) {
	usage := map[agents.AccountID]agentsapi.Usage{"c1": usageOf(0.72, false)}
	s := Sidebar{Accounts: fixture(), Selected: "c1", Usage: usage}

	narrow := strings.Join(s.View(25, 20), "\n")
	if strings.Contains(narrow, "███░") {
		t.Fatalf("usage shown at w=25:\n%s", narrow)
	}
	lines := s.View(26, 20)
	assertBox(t, lines, 26, 20)
	row := stripLine(lines[3])
	if !strings.Contains(row, " ███░ ●") || !strings.HasSuffix(row, "●") {
		t.Fatalf("usage row at w=26: %q", row)
	}
	if strings.Contains(stripLine(lines[4]), "%") {
		t.Fatalf("account without usage shows a percentage: %q", lines[4])
	}
}

func TestSidebarStaleUsageStillShowsNumbers(t *testing.T) {
	s := Sidebar{
		Accounts: fixture(),
		Usage:    map[agents.AccountID]agentsapi.Usage{"c1": usageOf(1, true)},
	}
	if row := stripLine(s.View(26, 20)[3]); !strings.Contains(row, "████") {
		t.Fatalf("stale usage dropped: %q", row)
	}
}

func TestSidebarASCII(t *testing.T) {
	s := Sidebar{Accounts: fixture(), Selected: "c1", ASCII: true}
	lines := s.View(24, 20)
	assertBox(t, lines, 24, 20)
	got := stripLine(strings.Join(lines, "\n"))
	for _, want := range []string{"> C work", "  C personal", "  X work", "  A personal"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if strings.ContainsAny(got, "✳◎✦●") {
		t.Fatalf("unicode glyphs in ascii mode:\n%s", got)
	}
	// Connected is "*"; signed-out and unknown are "o".
	if !strings.Contains(stripLine(lines[3]), "*") || !strings.Contains(stripLine(lines[4]), "o") || !strings.Contains(stripLine(lines[7]), "o") {
		t.Fatalf("ascii dots wrong:\n%s", got)
	}
}

func TestSidebarSizes(t *testing.T) {
	usage := map[agents.AccountID]agentsapi.Usage{"c1": usageOf(0.4, false)}
	long := append(fixture(), acct("l", "claude", strings.Repeat("verylongname", 5), "logged_in"))
	for _, w := range []int{12, 24, 26, 40} {
		for _, h := range []int{1, 2, 4, 10, 30} {
			s := Sidebar{Accounts: long, Selected: "l", Usage: usage}
			assertBox(t, s.View(w, h), w, h)
		}
	}
}

func TestSidebarScrollsToSelection(t *testing.T) {
	var accts []agentsapi.Account
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		accts = append(accts, acct(n, "claude", "acct-"+n, ""))
	}
	s := Sidebar{Accounts: accts, Selected: "h"}
	lines := s.View(24, 8)
	assertBox(t, lines, 24, 8)
	joined := stripLine(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "> ✳ acct-h") || !strings.Contains(joined, "+ Add account") {
		t.Fatalf("selection scrolled out:\n%s", joined)
	}
}

func TestAuthOf(t *testing.T) {
	tests := map[string]authState{
		"logged_in": authConnected, "token": authConnected,
		"logged_out": authNeedsSignIn, "missing": authNeedsSignIn, "expired": authNeedsSignIn,
		"unknown": authUnknown, "": authUnknown,
	}
	for status, want := range tests {
		if got := authOf(agents.AccountInfo{Options: map[string]any{"auth_status": status}}); got != want {
			t.Errorf("%q: got %d want %d", status, got, want)
		}
	}
	if authOf(agents.AccountInfo{}) != authUnknown {
		t.Error("zero info should be unknown")
	}
}
