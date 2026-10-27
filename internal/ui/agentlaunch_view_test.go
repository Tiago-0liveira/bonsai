package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
)

var launchSizes = [][2]int{{80, 24}, {140, 40}}

func (e *agentsEnv) launchAt(t *testing.T, w, h int) {
	t.Helper()
	e.m.width, e.m.height = w, h
	e.openLaunch(t)
}

// checkLaunchBounds fails on rows that overflow the screen and on a missing
// Start action or footer: both must stay reachable at every size.
func checkLaunchBounds(t *testing.T, e *agentsEnv, w, h int, wantStart string) string {
	t.Helper()
	view := e.overlayView()
	lines := strings.Split(view, "\n")
	if len(lines) > h {
		t.Errorf("%dx%d: %d rows exceed the screen", w, h, len(lines))
	}
	for _, l := range lines {
		if ansi.StringWidth(l) > w {
			t.Errorf("%dx%d: row wider than the screen: %q", w, h, l)
		}
	}
	for _, want := range []string{wantStart, "Esc "} {
		if !strings.Contains(view, want) {
			t.Errorf("%dx%d: %q must stay reachable:\n%s", w, h, want, view)
		}
	}
	return view
}

func usageWithFiveHour(frac float64, in time.Duration, stale bool) agentsapi.Usage {
	reset := time.Now().Add(in)
	l := &agents.UsageLimit{ID: "five_hour", Window: "5h", RemainingFraction: &frac, ResetsAt: &reset}
	return agentsapi.Usage{Headline: agents.UsageHeadline{Primary: l}, FetchedAt: time.Now(), Stale: stale}
}

func TestLaunchViewShowsFiveHourCapacityPerAccount(t *testing.T) {
	for _, size := range launchSizes {
		e := accountsEnv(t)
		e.fb.Accounts.Usages["claude:work"] = usageWithFiveHour(0.72, 2*time.Hour+10*time.Minute+20*time.Second, false)
		e.fb.Accounts.Usages["claude:personal"] = usageWithFiveHour(0.30, 4*time.Hour, true)
		weekly := usageWithFiveHour(0.5, time.Hour, false)
		weekly.Headline.Primary.ID, weekly.Headline.Primary.Window = "weekly", "weekly"
		e.fb.Accounts.Usages["codex:work"] = weekly
		e.launchAt(t, size[0], size[1])
		view := checkLaunchBounds(t, e, size[0], size[1], "Start session")
		for _, want := range []string{"New session", "72%", "2h10m", "30%", "Stale", "Not reported"} {
			if !strings.Contains(view, want) {
				t.Errorf("%v: missing %q:\n%s", size, want, view)
			}
		}
		if size[0] > 100 && !strings.Contains(view, "5H LEFT") {
			t.Errorf("%v: wide view keeps the 5H LEFT heading:\n%s", size, view)
		}
		if got := strings.Count(view, "Ctrl+S") + strings.Count(view, "^S"); got != 1 {
			t.Errorf("%v: exactly one footer hint expected, got %d:\n%s", size, got, view)
		}
	}
}

func TestLaunchFooterFollowsFocusAndStatusBarStaysQuiet(t *testing.T) {
	e := accountsEnv(t)
	e.launchAt(t, 140, 40)
	if v := e.overlayView(); !strings.Contains(v, "↑↓ Account  Enter Options  Ctrl+S Start  Esc Back") {
		t.Errorf("table focus footer:\n%s", v)
	}
	e.sendKey(t, tea.KeyEnter)
	if v := e.overlayView(); !strings.Contains(v, "Tab Next field  Shift+Tab Previous  Ctrl+S Start  Esc Back") || strings.Contains(v, "↑↓ Account") {
		t.Errorf("field focus footer:\n%s", v)
	}
	if got := ansi.Strip(e.m.contextHelp()); got != "" {
		t.Errorf("status bar must not repeat the pane footer, got %q", got)
	}
}

func TestLaunchManyAccountsShowRangeAndKeepActionsAndSelection(t *testing.T) {
	for _, size := range launchSizes {
		e := newAgentsEnv(t)
		var items []agentsapi.Account
		for i := range 15 {
			items = append(items, acct("claude", fmt.Sprintf("acct%02d", i), 0))
		}
		e.setAccounts(items...)
		e.openTab(t)
		e.launchAt(t, size[0], size[1])
		view := checkLaunchBounds(t, e, size[0], size[1], "Start session")
		if size[0] == 80 && !strings.Contains(view, " of 15") {
			t.Errorf("%v: hidden rows need a range indicator:\n%s", size, view)
		}
		for range 14 {
			e.sendKey(t, tea.KeyDown)
		}
		view = checkLaunchBounds(t, e, size[0], size[1], "Start session")
		if !strings.Contains(view, "▸ acct14") {
			t.Errorf("%v: selected row must stay visible:\n%s", size, view)
		}
	}
}

func TestLaunchLongNamesAreElided(t *testing.T) {
	for _, size := range launchSizes {
		e := newAgentsEnv(t)
		long := strings.Repeat("verylongaccountname", 4)
		e.setAccounts(acct("claude", long, 0), acct("codex", "short", 0))
		e.openTab(t)
		e.launchAt(t, size[0], size[1])
		view := checkLaunchBounds(t, e, size[0], size[1], "Start session")
		if !strings.Contains(view, "…") || !strings.Contains(view, "Claude") {
			t.Errorf("%v: long name must be elided beside its provider:\n%s", size, view)
		}
	}
}

func TestLaunchExtraProviderFieldsScrollWithoutLosingActions(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {80, 14}, {140, 40}} {
		e := accountsEnv(t)
		var fields []agents.LaunchField
		for i := range 8 {
			fields = append(fields, agents.LaunchField{ID: fmt.Sprintf("extra%d", i), Label: fmt.Sprintf("Extra %d", i), Kind: agents.LaunchFieldSelect,
				Options: []agents.FieldOption{{Value: "a", Label: "A"}, {Value: "b", Label: "B"}}})
		}
		e.fb.Accounts.LaunchDefaults["claude:work"] = agentsapi.Defaults{LaunchFields: fields, Values: map[string]string{}}
		e.launchAt(t, size[0], size[1])
		e.sendKey(t, tea.KeyEnter) // Name
		for range 9 {
			e.sendKey(t, tea.KeyTab)
		}
		view := checkLaunchBounds(t, e, size[0], size[1], "Start session")
		if !strings.Contains(view, "> Extra 7") {
			t.Errorf("%v: the focused field must be visible:\n%s", size, view)
		}
		if !strings.Contains(view, "Save as account defaults") {
			t.Errorf("%v: save box must stay reachable:\n%s", size, view)
		}
	}
}

func TestLaunchSignInRequiredAndEmptyList(t *testing.T) {
	for _, size := range launchSizes {
		e := accountsEnv(t)
		e.fb.Accounts.Infos["claude:work"] = agents.AccountInfo{Options: map[string]any{"auth_status": "logged_out"}}
		e.launchAt(t, size[0], size[1])
		checkLaunchBounds(t, e, size[0], size[1], "Sign in first")

		e = newAgentsEnv(t)
		e.setAccounts()
		e.openTab(t)
		e.launchAt(t, size[0], size[1])
		view := checkLaunchBounds(t, e, size[0], size[1], "No accounts yet.")
		if strings.Contains(view, "Start session") {
			t.Errorf("%v: nothing to start with an empty list:\n%s", size, view)
		}
	}
}

func TestLaunchStartErrorStaysBesideActions(t *testing.T) {
	e := accountsEnv(t)
	e.launchAt(t, 80, 24)
	d := e.dialog().failed(fmt.Errorf("account is busy"))
	e.m.agents.overlay.launch = &d
	view := checkLaunchBounds(t, e, 80, 24, "Start session")
	if !strings.Contains(view, "account is busy") {
		t.Errorf("start error missing:\n%s", view)
	}
}

func TestLaunchViewKeepsStartReachableAtShortHeights(t *testing.T) {
	for h := 10; h <= 14; h++ {
		e := accountsEnv(t)
		e.launchAt(t, 80, h)
		checkLaunchBounds(t, e, 80, h, "Start")
	}
}

// Below the reachable range the pane may drop content but must not panic.
func TestLaunchViewSurvivesTinyHeights(t *testing.T) {
	for h := 1; h < 10; h++ {
		e := accountsEnv(t)
		e.launchAt(t, 80, h)
		_ = e.m.View()
	}
}
