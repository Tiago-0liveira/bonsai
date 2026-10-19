package accounts

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/usagestore"
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
}

func TestSidebarSelector(t *testing.T) {
	s := Sidebar{Accounts: fixture(), Selected: "x1", Busy: map[agents.AccountID]bool{"x1": true}}
	got := ansi.Strip(s.Selector(false))
	if got != "❃ Codex · work (3/4) · busy" {
		t.Fatalf("selector: %q", got)
	}
	if got := ansi.Strip((Sidebar{Accounts: fixture(), Selected: "none"}).Selector(true)); got != "↑↓ + Add account" {
		t.Fatalf("unselected: %q", got)
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

// storeOf is a usage store holding the given snapshots as loaded data.
func storeOf(m map[agents.AccountID]agentsapi.Usage) usagestore.Store {
	out := usagestore.Store{}
	for id, u := range m {
		out[id] = usagestore.Entry{Usage: u, Have: true}
	}
	return out
}
