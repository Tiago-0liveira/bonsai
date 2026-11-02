package accounts

import (
	"fmt"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi/fake"
)

func embedded(t *testing.T, api *fake.Accounts, w, h int) Model {
	t.Helper()
	m := New(api, Options{Embedded: true, Now: func() time.Time { return testNow }})
	m.SetSize(w, h)
	m, _ = drain(m, m.Init())
	return m
}

func TestEmbeddedHasNoFooterAndFillsHeight(t *testing.T) {
	for _, size := range [][2]int{{80, 18}, {120, 29}} {
		m := embedded(t, newFake(), size[0], size[1])
		out := plain(m)
		if strings.Contains(out, "Esc Back") || strings.Contains(out, "Tab Section") {
			t.Errorf("%v: embedded view drew its own footer:\n%s", size, out)
		}
		if got := strings.Count(out, "\n") + 1; got != size[1] {
			t.Errorf("%v: height %d", size, got)
		}
	}
}

func TestEmbeddedEmptyDoesNotOpenAddForm(t *testing.T) {
	m := embedded(t, &fake.Accounts{}, 80, 18)
	if m.add.active {
		t.Fatal("embedded empty list opened the add form")
	}
	if out := plain(m); !strings.Contains(out, "Press n to add") {
		t.Errorf("empty reason missing:\n%s", out)
	}
	if h := m.Hints(); h[0].Key != "n" {
		t.Errorf("hints = %v", h)
	}
}

func TestEmbeddedEscLeavesWithoutDiscardPrompt(t *testing.T) {
	m := embedded(t, newFake(), 100, 30)
	m, _ = press(m, "enter", "3", "enter", "down", "enter")
	if !m.Dirty() {
		t.Fatal("no draft")
	}
	m, _ = press(m, "esc")
	m, msgs := press(m, "esc")
	if m.dlg != nil {
		t.Error("esc asked to discard although the draft stays on the model")
	}
	if !m.Dirty() {
		t.Error("draft lost")
	}
	found := false
	for _, msg := range msgs {
		_, ok := msg.(CloseMsg)
		found = found || ok
	}
	if !found {
		t.Error("esc from the list did not ask the parent to leave")
	}
}

func TestShowAsksBeforeDroppingADraft(t *testing.T) {
	m := embedded(t, newFake(), 100, 30)
	m, _ = press(m, "enter", "3", "enter", "down", "enter", "esc")
	m, _ = m.Show("c2", TabOverview)
	if m.Selected() != "c1" || m.dlg == nil {
		t.Fatalf("selection %q dlg=%v: a jump must confirm before leaving a draft", m.Selected(), m.dlg != nil)
	}
	m, _ = m.Show("", TabUsage)
	if m.Section() != "Usage" || !m.Dirty() {
		t.Errorf("section %s dirty=%v: a section jump keeps the draft", m.Section(), m.Dirty())
	}
}

func TestShowBeforeFirstListSelectsLater(t *testing.T) {
	m := New(newFake(), Options{Embedded: true})
	m, _ = m.Show("c2", TabDefaults)
	m, _ = drain(m, m.Init())
	if m.Selected() != "c2" || m.Section() != "Defaults" {
		t.Errorf("selected %q section %s", m.Selected(), m.Section())
	}
}

func TestEmbeddedUsageScrollsWhenContentOverflows(t *testing.T) {
	api := newFake()
	var extra []agents.UsageLimit
	for i := range 8 {
		f := 0.5
		extra = append(extra, agents.UsageLimit{ID: fmt.Sprintf("pool%d", i), Label: fmt.Sprintf("Pool %d", i), RemainingFraction: &f})
	}
	u := api.Usages["c1"]
	u.Headline.Extra = extra
	api.Usages["c1"] = u
	m := embedded(t, api, 80, 12)
	m, _ = press(m, "enter", "2")
	out := plain(m)
	if strings.Contains(out, "Pool 7") || !strings.Contains(out, "more below") {
		t.Fatalf("overflow must be announced, not silently cut:\n%s", out)
	}
	hinted := false
	for _, h := range m.Hints() {
		hinted = hinted || h.Desc == "scroll"
	}
	if !hinted {
		t.Error("scroll hint missing while content overflows")
	}
	m, _ = press(m, "down", "down", "down", "down", "down", "down", "down", "down", "down", "down")
	if out := plain(m); !strings.Contains(out, "Pool 7") || strings.Contains(out, "more below") {
		t.Errorf("scrolling to the end must reveal the last pool:\n%s", out)
	}
	m, _ = press(m, "up")
	if out := plain(m); !strings.Contains(out, "above") {
		t.Errorf("no indicator for hidden rows above:\n%s", out)
	}
	m, _ = press(m, "esc", "down")
	if m.usageOff != 0 {
		t.Error("account change kept the scroll offset")
	}
}
