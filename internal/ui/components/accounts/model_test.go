package accounts

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi/fake"
)

var testNow = time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)

func newFake() *fake.Accounts {
	pct := 0.72
	reset := testNow.Add(2*time.Hour + 14*time.Minute)
	limit := &agents.UsageLimit{ID: "five_hour", Window: "5h", RemainingFraction: &pct, ResetsAt: &reset}
	return &fake.Accounts{
		Items: []agentsapi.Account{
			acct("c1", "claude", "work", "logged_in"),
			acct("c2", "claude", "personal", "logged_in"),
			acct("x1", "codex", "work", "logged_out"),
		},
		Infos: map[agents.AccountID]agents.AccountInfo{
			"c1": {Identity: "work@example.com", Plan: "Max", AuthMode: "login", Options: map[string]any{"auth_status": "logged_in"}},
			"c2": {Identity: "me@example.com"},
			"x1": {},
		},
		Usages: map[agents.AccountID]agentsapi.Usage{
			"c1": {Headline: agents.UsageHeadline{Primary: limit}, FetchedAt: testNow.Add(-35 * time.Second)},
			"c2": {Headline: agents.UsageHeadline{Primary: limit}, FetchedAt: testNow},
			"x1": {Headline: agents.UsageHeadline{Primary: limit}, FetchedAt: testNow},
		},
		LaunchDefaults: map[agents.AccountID]agentsapi.Defaults{
			"c1": {Fields: fake.SampleLaunchFields(), Values: map[string]string{}},
			"c2": {Fields: fake.SampleLaunchFields(), Values: map[string]string{}},
			"x1": {Fields: fake.SampleLaunchFields(), Values: map[string]string{}},
		},
	}
}

// drain runs cmd and feeds every message it produces back to the model.
// Slow commands (the "Saved" tick) are skipped instead of waited for.
func drain(m Model, cmd tea.Cmd) (Model, []tea.Msg) {
	var seen []tea.Msg
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		done := make(chan tea.Msg, 1)
		go func() { done <- c() }()
		var msg tea.Msg
		select {
		case msg = <-done:
		case <-time.After(200 * time.Millisecond):
			continue
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		seen = append(seen, msg)
		var next tea.Cmd
		m, next = m.Update(msg)
		queue = append(queue, next)
	}
	return m, seen
}

func press(m Model, keys ...string) (Model, []tea.Msg) {
	var all []tea.Msg
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "up":
			msg = tea.KeyMsg{Type: tea.KeyUp}
		case "left":
			msg = tea.KeyMsg{Type: tea.KeyLeft}
		case "right":
			msg = tea.KeyMsg{Type: tea.KeyRight}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "shift+tab":
			msg = tea.KeyMsg{Type: tea.KeyShiftTab}
		case "ctrl+s":
			msg = tea.KeyMsg{Type: tea.KeyCtrlS}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		var cmd tea.Cmd
		m, cmd = m.Update(msg)
		var seen []tea.Msg
		m, seen = drain(m, cmd)
		all = append(all, seen...)
	}
	return m, all
}

func opened(t *testing.T, api *fake.Accounts, w, h int) Model {
	t.Helper()
	m := New(api, Options{Now: func() time.Time { return testNow }})
	m.SetSize(w, h)
	m, _ = drain(m, m.Init())
	return m
}

func plain(m Model) string { return ansi.Strip(m.View()) }

func TestInitLoadsSelectedAccount(t *testing.T) {
	api := newFake()
	m := opened(t, api, 104, 34)
	out := plain(m)
	for _, want := range []string{"AGENT PROFILES", "Claude · work", "[Overview]", "Connected", "work@example.com", "Max", "Browser login", "0 running"} {
		if !strings.Contains(out, want) {
			t.Errorf("view lacks %q:\n%s", want, out)
		}
	}
	calls := api.UsageCalls()
	if len(calls) != 1 || calls[0].ID != "c1" || calls[0].Refresh {
		t.Fatalf("usage calls = %+v", calls)
	}
}

func TestTabsAndFrameSizes(t *testing.T) {
	for _, size := range [][2]int{{104, 34}, {80, 24}} {
		m := opened(t, newFake(), size[0], size[1])
		for key, want := range map[string]string{"2": "72% remaining", "3": "[Defaults]", "1": "[Overview]"} {
			m, _ = press(m, key)
			out := plain(m)
			if !strings.Contains(out, want) {
				t.Errorf("%dx%d key %s: view lacks %q:\n%s", size[0], size[1], key, want, out)
			}
			fw, fh := FrameSize(size[0], size[1])
			lines := strings.Split(out, "\n")
			if len(lines) != fh {
				t.Errorf("%dx%d: %d lines, want %d", size[0], size[1], len(lines), fh)
			}
			for _, l := range lines {
				if ansi.StringWidth(l) != fw {
					t.Fatalf("%dx%d: line width %d, want %d: %q", size[0], size[1], ansi.StringWidth(l), fw, l)
				}
			}
		}
	}
}

func TestUsageShowsAgeAndRefreshes(t *testing.T) {
	api := newFake()
	m := opened(t, api, 104, 34)
	m, _ = press(m, "2")
	if out := plain(m); !strings.Contains(out, "Updated 35 seconds ago") || !strings.Contains(out, "Resets in 2h14m") {
		t.Fatalf("usage view:\n%s", out)
	}
	m, _ = press(m, "r")
	calls := api.UsageCalls()
	if last := calls[len(calls)-1]; !last.Refresh || last.ID != "c1" {
		t.Fatalf("usage calls = %+v", calls)
	}
}

func TestEscClosesAndDirtyDefaultsAsk(t *testing.T) {
	m := opened(t, newFake(), 104, 34)
	if _, msgs := press(m, "esc"); len(msgs) != 1 || msgs[0] != (CloseMsg{}) {
		t.Fatalf("esc messages = %#v", msgs)
	}

	m, _ = press(m, "3", "enter", "down", "enter")
	if !strings.Contains(plain(m), "● unsaved") {
		t.Fatalf("no unsaved marker:\n%s", plain(m))
	}
	m, msgs := press(m, "esc", "esc")
	if len(msgs) != 0 || !m.Capturing() || !strings.Contains(plain(m), "Discard unsaved defaults?") {
		t.Fatalf("esc on dirty defaults must ask, msgs=%#v\n%s", msgs, plain(m))
	}
	if _, msgs := press(m, "enter"); len(msgs) != 0 {
		t.Fatalf("default dialog choice must keep editing, got %#v", msgs)
	}
}

func TestSaveSendsOnlyChangedFields(t *testing.T) {
	api := newFake()
	m := opened(t, api, 104, 34)
	m, _ = press(m, "3", "enter", "down", "enter", "ctrl+s")

	updates := api.Updates()
	if len(updates) != 1 || updates[0].ID != "c1" || len(updates[0].Values) != 1 {
		t.Fatalf("updates = %+v", updates)
	}
	if v := updates[0].Values["permissions"]; v == nil || *v != "full-access" {
		t.Fatalf("permissions = %v", v)
	}
	if out := plain(m); !strings.Contains(out, "Saved") || strings.Contains(out, "● unsaved") {
		t.Fatalf("after save:\n%s", out)
	}
}
