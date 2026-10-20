package ui

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/assist"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
)

func assistSettings(t *testing.T, m Model, field int) Model {
	t.Helper()
	m, cmd := m.selectWorkspace(wsSettings)
	m = plannerRun(t, m, cmd)
	sec := -1
	for n, s := range settingCategories[catGarden].sections {
		if s.id == "assist" {
			sec = n
		}
	}
	if sec < 0 {
		t.Fatal("missing AI drafting section")
	}
	m.set.cat, m.set.sec, m.set.field, m.set.level = catGarden, sec, field, levelField
	m.shellFocus = shellBody
	m, cmd = m.gardenWarm()
	return plannerRun(t, m, cmd)
}

func TestSettingsAssistRestoreOnlyClearsGardenAssist(t *testing.T) {
	e := newSettingsEnv(t)
	e.writeState(t, `{"future":"keep","copy_counts":{"a":2},"prefs":{"theme":"nord","keys":{"quit":"x"},"garden_assist":{"account":"gone","questions":7,"guidance":"focus on UX"},"future_pref":"keep"}}`)
	m := assistSettings(t, e.model(t, 120, 35), 4)
	before := m.work.Clone()
	m = apress(t, m, "enter")
	if m.modal == nil || m.modal.Kind() != modals.KindGardenAssistRestore {
		t.Fatal("restore did not confirm")
	}
	m, _ = send(t, m, modals.CancelMsg{Kind: modals.KindGardenAssistRestore})
	if !reflect.DeepEqual(m.work, before) {
		t.Fatal("cancel changed preferences")
	}
	m = apress(t, m, "enter", "up", "enter")
	before.GardenAssist = nil
	if !reflect.DeepEqual(m.work, before) || !reflect.DeepEqual(diskPrefs(t), before) {
		t.Fatalf("restore changed unrelated prefs: %+v", m.work)
	}
	st, err := config.LoadState()
	if err != nil || st.CopyRank("a") != 2 {
		t.Fatalf("restore lost state: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(e.xdg, "bonsai", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"garden_assist"`) {
		t.Fatalf("restore left garden_assist: %s", data)
	}
	for _, want := range []string{`"future": "keep"`, `"future_pref": "keep"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("restore lost %s: %s", want, data)
		}
	}
	if got := m.assistDefaults(); got.Questions != assist.DefaultQuestions || got.Guidance != "" || got.AccountID != "" {
		t.Fatalf("restore defaults = %+v", got)
	}
}

func TestSettingsAssistMissingAccountNeverFallsBack(t *testing.T) {
	for _, id := range []string{"gone", "x1"} {
		t.Run(id, func(t *testing.T) {
			p := config.Prefs{GardenAssist: &config.GardenAssistPrefs{Account: id, Model: "opus", Effort: "high"}}
			m, rig := assistEnv(t, p, 120, 35, true)
			m = assistSettings(t, m, 0)
			if got := m.assistDefaultsSummary(); got != "missing: choose again" {
				t.Fatalf("summary = %q", got)
			}
			m = apress(t, m, "enter")
			o := m.set.assist
			if !m.set.assistOpen || o.Selected() != "" {
				t.Fatalf("silently selected %q", o.Selected())
			}
			mustContain(t, "missing account", view(m), "Saved default account is missing: choose again", "Save defaults", "Cancel")
			m = apress(t, m, "ctrl+s")
			if !m.set.assistOpen || m.prefsGen != 0 || m.work.GardenAssist.Account != id {
				t.Fatal("saved without an explicit choice")
			}
			m = apress(t, m, "esc")
			m, cmd := m.selectWorkspace(wsGarden)
			m = plannerRun(t, m, cmd)
			m = apress(t, m, "enter")
			m = newIdea(t, m, "Export is slow")
			o = assistSession0(m).opts
			if o.Selected() != "" {
				t.Fatalf("session silently selected %q", o.Selected())
			}
			m = apress(t, m, "ctrl+s")
			if rig.garden.Count("AssistQuestions") != 0 {
				t.Fatal("missing default made a request")
			}
			m = apress(t, m, "tab", "enter", "down")
			if o.Selected() == "" {
				t.Fatal("explicit account choice failed")
			}
			if m.work.GardenAssist.Account != id {
				t.Fatal("session changed prefs")
			}
		})
	}
}

func TestSettingsAssistOrigins(t *testing.T) {
	m, _ := assistEnv(t, config.Prefs{}, 120, 35, true)
	for _, id := range []string{"assist_account", "assist_questions", "assist_guidance"} {
		f, _ := fieldByID(t, id)
		if f.origin(m) != "default" {
			t.Fatalf("%s initial origin = %s", id, f.origin(m))
		}
	}
	m.work.GardenAssist = &config.GardenAssistPrefs{}
	q, _ := fieldByID(t, "assist_questions")
	g, _ := fieldByID(t, "assist_guidance")
	if q.get(m) != "5" || q.origin(m) != "default" || g.display(m) != "none" || g.origin(m) != "default" {
		t.Fatal("empty defaults changed origins")
	}
	m.work.GardenAssist = &config.GardenAssistPrefs{Account: "c1", Questions: 5, Guidance: "focus on UX"}
	m = assistSettings(t, m, 0)
	for _, id := range []string{"assist_account", "assist_questions", "assist_guidance"} {
		f, _ := fieldByID(t, id)
		if f.origin(m) != "personal" {
			t.Fatalf("%s origin = %s", id, f.origin(m))
		}
	}
	if m.assistDefaultsSummary() != "work · profile default · profile default" {
		t.Fatal(m.assistDefaultsSummary())
	}
	if got := m.categoryScope(settingCategories[catGarden]); got != "Mixed" {
		t.Fatalf("Garden scope = %q", got)
	}
}

func TestSettingsAssistRoundTripIntoRequest(t *testing.T) {
	m, rig := assistEnv(t, config.Prefs{}, 120, 35, true)
	m = assistSettings(t, m, 1)
	m = apress(t, m, "right", "down", "enter") // 6 questions, edit guidance
	guidance := strings.Repeat("é", assist.MaxGuidanceRunes)
	m = typed(t, m, guidance+"x")
	m = apress(t, m, "enter")
	if !m.set.editing || !strings.Contains(m.set.editErr, "300 runes") {
		t.Fatal("overlong guidance accepted")
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	m = apress(t, m, "enter")
	if m.set.editing {
		t.Fatal(m.set.editErr)
	}
	persisted := diskPrefs(t)
	if persisted.GardenAssist.Questions != 6 || persisted.GardenAssist.Guidance != guidance {
		t.Fatalf("prefs = %+v", persisted.GardenAssist)
	}
	// A new model reads the same persisted preferences.
	st, err := config.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	next := New(m.repoDir, m.cfg, st, WithLayout(LayoutRedesign), WithAgents(rig.agents.API()), WithGarden(rig.garden))
	next = sized(t, next, 120, 35)
	next.garden = m.garden
	next.workspace, next.shellFocus = wsGarden, shellBody
	next = newIdea(t, next, "Export is slow")
	next = askQuestions(t, next)
	reqs := rig.garden.AssistQuestionsRequests
	if len(reqs) != 1 || reqs[0].Settings.Questions != 6 || reqs[0].Settings.Guidance != guidance {
		t.Fatalf("requests = %+v", reqs)
	}
}

func TestSettingsAssistDefaultsSaveAndLateResults(t *testing.T) {
	m, rig := assistEnv(t, config.Prefs{GardenAssist: &config.GardenAssistPrefs{Account: "c1", Model: "opus", Effort: "high", Questions: 7, Guidance: "focus on UX"}}, 120, 35, true)
	m = assistSettings(t, m, 0)
	m = apress(t, m, "enter")
	o := m.set.assist
	if o.Selected() != "c1" || o.settings().Model != "opus" || o.settings().Effort != "high" {
		t.Fatalf("saved defaults not prefilled: %+v", o.settings())
	}
	mustContain(t, "defaults dialog", view(m), "Save defaults", "Cancel")
	if strings.Contains(view(m), "Questions  ") || strings.Contains(view(m), "Guidance   ") {
		t.Fatal("defaults mode exposed session fields")
	}
	// Clear the model override and change effort through the shared form.
	m = apress(t, m, "tab", "enter", "up", "enter", "tab", "enter", "up", "enter")
	if got := o.settings(); got.Model != "" || got.Effort != "low" {
		t.Fatalf("edited options = %+v", got)
	}
	// Save through the button, then make a newer question edit before it lands.
	o.setFocus(optSave)
	m, first := ahold(t, m, "enter")
	if first == nil || m.set.assistOpen {
		t.Fatal("Save defaults did not use prefs save flow")
	}
	m.set.field = 1
	m, second := ahold(t, m, "right")
	m = plannerRun(t, m, second)
	msg := first().(settingsSavedMsg)
	if !errors.Is(msg.err, config.ErrSuperseded) {
		t.Fatalf("old save error = %v", msg.err)
	}
	m, _ = send(t, m, msg)
	m, _ = send(t, m, settingsSavedMsg{gen: msg.gen, err: errors.New("late failure")})
	got := diskPrefs(t).GardenAssist
	if m.work.GardenAssist.Questions != 8 || got.Questions != 8 || got.Guidance != "focus on UX" || got.Account != "c1" || got.Model != "" || got.Effort != "low" || m.set.saveErr != "" {
		t.Fatalf("late save changed newer defaults: %+v, %s", got, m.set.saveErr)
	}
	if len(rig.agents.Accounts.Updates()) != 0 {
		t.Fatal("AI defaults changed account profile")
	}
}

func TestAssistDefaultsClampSanitizeAndSessionOverride(t *testing.T) {
	for _, tc := range []struct{ stored, want int }{{0, 5}, {-1, 1}, {11, 10}, {7, 7}} {
		m := Model{work: config.Prefs{GardenAssist: &config.GardenAssistPrefs{Questions: tc.stored, Guidance: strings.Repeat("é", 301)}}}
		got := m.assistDefaults()
		if got.Questions != tc.want || got.Guidance != assist.SanitizeGuidance(m.work.GardenAssist.Guidance) {
			t.Fatalf("defaults = %+v", got)
		}
	}
	prefs := config.Prefs{GardenAssist: &config.GardenAssistPrefs{Account: "c1", Model: "opus", Effort: "high", Questions: 7, Guidance: "focus on UX"}}
	m, rig := assistEnv(t, prefs, 120, 35, true)
	m = newIdea(t, m, "Export is slow")
	o := assistSession0(m).opts
	if o.Selected() != "c1" || o.settings().Model != "opus" || o.settings().Effort != "high" {
		t.Fatalf("session defaults = %+v", o.settings())
	}
	m = apress(t, m, "tab", "enter")
	o.setFocus(optQuestions)
	m = apress(t, m, "right")
	o.setFocus(optGuidance)
	o.guidance.SetValue("challenge scope")
	m = apress(t, m, "enter")
	m = askQuestions(t, m)
	req := rig.garden.AssistQuestionsRequests[0]
	if req.Settings.Questions != 8 || req.Settings.Guidance != "challenge scope" {
		t.Fatalf("session request = %+v", req.Settings)
	}
	if !reflect.DeepEqual(m.work, prefs) || m.prefsGen != 0 || m.set.inflight != 0 || m.state.PrefsSnapshot().GardenAssist.Questions != 7 {
		t.Fatal("session override wrote preferences")
	}
}

func TestSettingsAssistDialogFits(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 35}, {64, 10}} {
		m, _ := assistEnv(t, config.Prefs{GardenAssist: &config.GardenAssistPrefs{Account: "gone"}}, size[0], size[1], true)
		m = assistSettings(t, m, 0)
		m = apress(t, m, "enter")
		mustFit(t, "defaults dialog", m)
		m = apress(t, m, "esc")
		if m.set.assistOpen || m.prefsGen != 0 {
			t.Fatal("cancel saved defaults")
		}
	}
}

func TestSettingsAssistEmptyModelEffortInheritProfile(t *testing.T) {
	m, rig := assistEnv(t, config.Prefs{}, 120, 35, true)
	profile := rig.agents.Accounts.LaunchDefaults["c1"]
	profile.Values = map[string]string{"model": "opus", "effort": "high"}
	rig.agents.Accounts.LaunchDefaults["c1"] = profile
	m = assistSettings(t, m, 0)
	m = apress(t, m, "enter")
	if got := m.set.assist.settings(); got.Model != "" || got.Effort != "" {
		t.Fatalf("inherited profile values became explicit: %+v", got)
	}
	// Tab traverses only account, model, effort and the two buttons.
	m = apress(t, m, "tab", "tab", "tab")
	if m.set.assist.focus != optCancel {
		t.Fatalf("tab reached %v, want Cancel", m.set.assist.focus)
	}
	m = apress(t, m, "tab", "enter")
	p := diskPrefs(t).GardenAssist
	if m.set.assistOpen || p == nil || p.Account != "c1" || p.Model != "" || p.Effort != "" {
		t.Fatalf("saved defaults = %+v", p)
	}
	m, cmd := m.selectWorkspace(wsGarden)
	m = plannerRun(t, m, cmd)
	m = apress(t, m, "enter")
	m = newIdea(t, m, "Export is slow")
	m = askQuestions(t, m)
	got := rig.garden.AssistQuestionsRequests[0].Settings
	if got.Model != "" || got.Effort != "" || got.AccountID != "c1" {
		t.Fatalf("profile inheritance request = %+v", got)
	}
}

func TestAssistPreselectsSavedSupportedAccount(t *testing.T) {
	m, rig := assistEnv(t, config.Prefs{GardenAssist: &config.GardenAssistPrefs{Account: "c2"}}, 120, 35, true)
	rig.agents.Accounts.Items = append(rig.agents.Accounts.Items, assistAccount("c2", "claude", "second", "logged_in", agents.StructuredSupport{Supported: true}))
	rig.agents.Accounts.Infos["c2"] = rig.agents.Accounts.Infos["c1"]
	rig.agents.Accounts.LaunchDefaults["c2"] = rig.agents.Accounts.LaunchDefaults["c1"]
	m = assistSettings(t, m, 0)
	m = apress(t, m, "enter")
	if got := m.set.assist.Selected(); got != "c2" {
		t.Fatalf("defaults selected %q", got)
	}
	m = apress(t, m, "esc")
	m, cmd := m.selectWorkspace(wsGarden)
	m = plannerRun(t, m, cmd)
	m = apress(t, m, "enter")
	m = newIdea(t, m, "Export is slow")
	if got := assistSession0(m).opts.Selected(); got != "c2" {
		t.Fatalf("session selected %q", got)
	}
}
