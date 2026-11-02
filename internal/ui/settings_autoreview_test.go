package ui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/reviewpass"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	agentfake "github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi/fake"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
)

type autoReviewAccountsFake struct {
	*agentfake.Accounts
	lists         int
	defaultsReads []agents.AccountID
	liveReads     []agents.AccountID
	resolves      int
	resolveErr    error
	inheritedFull bool
	deadlines     bool
}

func (f *autoReviewAccountsFake) List(ctx context.Context) ([]agentsapi.Account, error) {
	f.lists++
	_, deadline := ctx.Deadline()
	f.deadlines = f.deadlines && deadline
	return f.Accounts.List(ctx)
}

func (f *autoReviewAccountsFake) Defaults(ctx context.Context, id agents.AccountID) (agentsapi.Defaults, error) {
	f.liveReads = append(f.liveReads, id)
	return f.Accounts.Defaults(ctx, id)
}

func (f *autoReviewAccountsFake) CachedDefaults(ctx context.Context, id agents.AccountID) (agentsapi.Defaults, error) {
	f.defaultsReads = append(f.defaultsReads, id)
	_, deadline := ctx.Deadline()
	f.deadlines = f.deadlines && deadline
	return f.Accounts.Defaults(ctx, id)
}

func (f *autoReviewAccountsFake) ResolveLaunch(ctx context.Context, id agents.AccountID, l agents.LaunchOptions) (agents.LaunchResolution, error) {
	f.resolves++
	_, deadline := ctx.Deadline()
	f.deadlines = f.deadlines && deadline
	if f.resolveErr != nil {
		return agents.LaunchResolution{}, f.resolveErr
	}
	inherited := map[string]bool{}
	if l.Sandbox == "" {
		l.Sandbox = "workspace-write"
		if f.inheritedFull {
			l.Sandbox = "danger-full-access"
		}
		inherited["sandbox"] = true
	}
	if l.Approval == "" && l.Sandbox != "bypass" {
		l.Approval = "on-request"
		inherited["approval"] = true
	}
	return agents.LaunchResolution{AccountID: id, AccountName: "Work", Provider: "codex", Launch: l, Inherited: inherited}, nil
}

func autoReviewTestModel(t *testing.T) (Model, *autoReviewAccountsFake) {
	t.Helper()
	e := newSettingsEnv(t)
	m := e.model(t, 120, 35)
	acc := &autoReviewAccountsFake{Accounts: agentfake.New().Accounts, deadlines: true}
	for _, a := range []agents.Account{{ID: "one", Name: "Work", Provider: "codex"}, {ID: "two", Name: "Personal", Provider: "claude"}} {
		acc.Items = append(acc.Items, agentsapi.Account{Account: a})
	}
	acc.LaunchDefaults["one"] = agentsapi.Defaults{
		LaunchFields: []agents.LaunchField{
			{ID: "permissions", Kind: agents.LaunchFieldSelect, Options: []agents.FieldOption{
				{Value: "auto", Label: "Auto", Set: map[string]string{"sandbox": "workspace-write", "approval": "on-request"}},
				{Value: "full", Label: "Unrestricted", Tone: agents.ToneDanger, Help: "Can change any file without asking.", Set: map[string]string{"sandbox": "danger-full-access", "approval": "never"}},
				{Value: "bypass", Label: "Bypass", Tone: agents.ToneDanger, Set: map[string]string{"sandbox": "bypass"}},
			}},
			{ID: "effort", Kind: agents.LaunchFieldSelect, PerModel: true, Options: []agents.FieldOption{{Value: "", Label: "Profile default"}, {Value: "medium", Label: "Medium"}, {Value: "high", Label: "High"}}},
		},
		Models: []agents.ModelOption{{ID: "smart", Default: true, Efforts: []string{"low", "high"}}, {ID: "fixed", NoEffort: true}},
		Values: map[string]string{"model": "smart", "permissions": "full", "effort": "high"},
	}
	acc.LaunchDefaults["two"] = agentsapi.Defaults{LaunchFields: []agents.LaunchField{{ID: "permission_mode", Kind: agents.LaunchFieldSelect, Options: []agents.FieldOption{{Value: "default", Label: "Default"}}}}}
	m.agents.api.Accounts = acc
	m.workspace, m.shellFocus = wsSettings, shellBody
	m.set.cat, m.set.sec, m.set.level = catGarden, len(gardenSections())-1, levelField
	return m, acc
}

// Execute batches synchronously: the messages may still be delivered in any
// order by individual tests, without timers, providers or background goroutines.
func autoReviewDrain(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			m = autoReviewDrain(t, m, c)
		}
		return m
	}
	if msg == nil {
		return m
	}
	m, cmd = send(t, m, msg)
	return autoReviewDrain(t, m, cmd)
}

func autoReviewApplyField(t *testing.T, m Model, id, value string) Model {
	t.Helper()
	for _, f := range gardenAutoReviewSection().fields {
		if f.id == id {
			if f.apply == nil {
				t.Fatalf("%s has no apply hook", id)
			}
			nm, cmd := f.apply(m, value)
			return autoReviewDrain(t, nm.(Model), cmd)
		}
	}
	t.Fatalf("field %s missing", id)
	return m
}

func autoReviewField(t *testing.T, id string) settingField {
	t.Helper()
	for _, f := range gardenAutoReviewSection().fields {
		if f.id == id {
			return f
		}
	}
	t.Fatalf("field %s missing", id)
	return settingField{}
}

func TestSettingsAutoReviewRendersBounded(t *testing.T) {
	m, _ := autoReviewTestModel(t)
	nm, cmd := m.settingsAutoReviewWarm()
	m = autoReviewDrain(t, nm, cmd)
	m = autoReviewApplyField(t, m, "auto_review_account", "one")
	m = autoReviewApplyField(t, m, "auto_review_permissions", "full")
	fields := gardenAutoReviewSection().fields
	for _, f := range fields {
		if f.scope != scopeMachine {
			t.Fatalf("%s has wrong scope", f.id)
		}
	}
	for _, sz := range [][2]int{{80, 24}, {120, 35}, {160, 48}} {
		for _, ascii := range []bool{false, true} {
			n := sized(t, m, sz[0], sz[1])
			if ascii {
				n.work.AgentGlyphs = "ascii"
			} else {
				n.work.AgentGlyphs = ""
			}
			for i := range fields {
				n.set.field = i
				assertSettingsView(t, fmt.Sprintf("auto review %dx%d ascii=%v field=%d", sz[0], sz[1], ascii, i), n, sz[0], sz[1], ascii)
			}
		}
	}
	if gardenSections()[len(gardenSections())-2].id != "assist" {
		t.Fatal("section does not follow AI drafting")
	}
}

func TestSettingsAutoReviewRefusalAndAccountIsolation(t *testing.T) {
	m, acc := autoReviewTestModel(t)
	m = autoReviewApplyField(t, m, "auto_review_enabled", "true")
	if m.work.GardenAutoReview != nil || diskPrefs(t).GardenAutoReview != nil || !strings.Contains(m.status, "Choose a reviewer account") {
		t.Fatal("enabled without account or missing refusal", m.status)
	}
	nm, cmd := m.settingsAutoReviewWarm()
	m = autoReviewDrain(t, nm, cmd)
	m = autoReviewApplyField(t, m, "auto_review_account", "one")
	m = autoReviewApplyField(t, m, "auto_review_enabled", "true")
	m = autoReviewApplyField(t, m, "auto_review_model", "smart")
	m = autoReviewApplyField(t, m, "auto_review_effort", "high")
	m = autoReviewApplyField(t, m, "auto_review_permissions", "full")
	m = autoReviewApplyField(t, m, "auto_review_checks", "go test ./...; go vet ./...")
	// Unrelated preferences and account defaults must survive both a change and restore.
	nm2, save := m.editPrefs(func(p *config.Prefs) {
		p.GardenReview = &config.GardenReviewPrefs{AccountID: "playground"}
		p.GardenAssist = &config.GardenAssistPrefs{Account: "assistant"}
	})
	m = autoReviewDrain(t, nm2.(Model), save)
	before, profile := m.work.Clone(), acc.LaunchDefaults["one"]
	m = autoReviewApplyField(t, m, "auto_review_account", "two")
	p := m.autoReviewPolicy()
	if p.Model != "" || p.Effort != "" || autoReviewNativeSet(p) || p.Account != "two" || !p.Enabled || len(p.Checks) != 2 {
		t.Fatalf("account change did not reset only provider options: %+v", p)
	}
	if !strings.Contains(m.status, "model, effort and permissions reset") {
		t.Fatal("account change message lost after save", m.status)
	}
	if !reflect.DeepEqual(before.GardenReview, m.work.GardenReview) || !reflect.DeepEqual(before.GardenAssist, m.work.GardenAssist) || !reflect.DeepEqual(profile, acc.LaunchDefaults["one"]) {
		t.Fatal("unrelated defaults changed")
	}
	if len(acc.Updates()) != 0 {
		t.Fatal("account defaults were written")
	}
	if !acc.deadlines {
		t.Fatal("account command missing timeout")
	}
	if !reflect.DeepEqual(diskPrefs(t), m.work) {
		t.Fatal("working preferences differ from saved preferences")
	}
}

func TestSettingsAutoReviewNativeOptionsAndEfforts(t *testing.T) {
	m, acc := autoReviewTestModel(t)
	m = autoReviewApplyField(t, m, "auto_review_account", "one")
	permissions := autoReviewField(t, "auto_review_permissions")
	mustContain(t, "danger label", permissions.dyn(m)[2].label, "(full access)")
	m = autoReviewApplyField(t, m, permissions.id, "full")
	p := m.autoReviewPolicy()
	if p.Sandbox != "danger-full-access" || p.Approval != "never" || p.PermissionMode != "" || p.FullAccess != nil {
		t.Fatalf("preset not expanded: %+v", p)
	}
	mustContain(t, "danger help", permissions.helpFn(m), "Can change any file without asking.")
	m = autoReviewApplyField(t, m, permissions.id, "bypass")
	if p := m.autoReviewPolicy(); p.Sandbox != "bypass" || p.Approval != "" {
		t.Fatal("old preset approval retained", p)
	}
	m = autoReviewApplyField(t, m, permissions.id, "")
	if autoReviewNativeSet(m.autoReviewPolicy()) {
		t.Fatal("profile default retained native options")
	}
	if got := m.autoReviewEfforts(); len(got) != 3 || got[1].value != "low" {
		t.Fatal("model efforts not used", got)
	}
	m = autoReviewApplyField(t, m, "auto_review_model", "typed-model")
	if got := m.autoReviewEfforts(); len(got) != 3 || got[1].value != "medium" {
		t.Fatal("field efforts not used as fallback", got)
	}
	m = autoReviewApplyField(t, m, "auto_review_model", "fixed")
	if len(m.autoReviewEfforts()) != 1 {
		t.Fatal("efforts offered for model without effort support")
	}
	for _, tc := range []struct {
		provider agents.ProviderID
		field    agents.LaunchField
		value    string
	}{
		{"claude", agents.LaunchField{ID: "permission_mode", Kind: agents.LaunchFieldSelect, Options: []agents.FieldOption{{Value: "bypassPermissions", Label: "Bypass", Tone: agents.ToneDanger, Help: "Tools run without asking."}}}, "bypassPermissions"},
		{"antigravity", agents.LaunchField{ID: "full_access", Kind: agents.LaunchFieldToggle, Help: "Allow tools without prompts.", Options: []agents.FieldOption{{Value: "false", Label: "Ask"}, {Value: "true", Label: "Full", Tone: agents.ToneDanger}}}, "true"},
	} {
		t.Run(string(tc.provider), func(t *testing.T) {
			acc.LaunchDefaults["two"] = agentsapi.Defaults{LaunchFields: []agents.LaunchField{tc.field}}
			n := autoReviewApplyField(t, m, "auto_review_account", "two")
			n = autoReviewApplyField(t, n, permissions.id, tc.value)
			p := n.autoReviewPolicy()
			if p.Sandbox != "" || p.Approval != "" {
				t.Fatal("codex options leaked to other provider")
			}
			if tc.provider == "claude" && p.PermissionMode != tc.value {
				t.Fatal("Claude permission mode not stored")
			}
			if tc.provider == "antigravity" {
				if p.FullAccess == nil || !*p.FullAccess {
					t.Fatal("Antigravity toggle not stored")
				}
				mustContain(t, "toggle help", permissions.helpFn(n), tc.field.Help)
				n = autoReviewApplyField(t, n, permissions.id, "false")
				if n.autoReviewPolicy().FullAccess == nil || *n.autoReviewPolicy().FullAccess {
					t.Fatal("explicit false lost")
				}
			}
			n = autoReviewApplyField(t, n, permissions.id, "")
			if autoReviewNativeSet(n.autoReviewPolicy()) {
				t.Fatal("native options not cleared")
			}
			m = autoReviewApplyField(t, n, "auto_review_account", "one")
		})
	}
	if len(acc.Updates()) != 0 {
		t.Fatal("account defaults written")
	}
}

func TestSettingsAutoReviewChecksParseAndRefuse(t *testing.T) {
	m, acc := autoReviewTestModel(t)
	f := autoReviewField(t, "auto_review_checks")
	m.set.field = 6
	m, _ = f.open(m)
	if !m.set.editing {
		t.Fatal("lossless checks editor did not open")
	}
	m.set.input.SetValue("go test ./...; go vet ./...")
	nm, cmd := m.commitEdit()
	m = autoReviewDrain(t, nm.(Model), cmd)
	if f.get(m) != "go test ./...; go vet ./..." || f.display(m) != "go test ./... · go vet ./..." {
		t.Fatal("checks not formatted", f.get(m), f.display(m))
	}
	before := m.work.Clone()
	for _, text := range []string{"sh -c test", "go test | cat", "go test $HOME", "sudo go test"} {
		m, _ = f.open(m)
		m.set.input.SetValue(text)
		nm, cmd = m.commitEdit()
		m = autoReviewDrain(t, nm.(Model), cmd)
		if !m.set.editing || m.set.editErr == "" || strings.Contains(m.set.editErr, config.ErrInvalidGardenAutoReview.Error()) || !reflect.DeepEqual(m.work, before) {
			t.Fatalf("invalid checks %q not refused: %s", text, m.set.editErr)
		}
	}
	// A direct apply must also refuse malformed text.
	m = autoReviewApplyField(t, m, f.id, "env go test")
	if !reflect.DeepEqual(before, m.work) {
		t.Fatal("direct checks apply accepted shell wrapper")
	}
	m = autoReviewApplyField(t, m, f.id, "")
	if f.display(m) != "none" || f.get(m) != "" {
		t.Fatal("empty checks not cleared")
	}
	if len(acc.Updates()) != 0 {
		t.Fatal("account defaults written")
	}
}

func TestSettingsAutoReviewAvailabilityAndStaleResolution(t *testing.T) {
	m, acc := autoReviewTestModel(t)
	for _, tc := range []struct {
		name                         string
		enabled, inherited, explicit bool
		err                          error
		want                         string
	}{
		{name: "off", want: "Automatic review is off."},
		{name: "missing account", enabled: true, err: agents.ErrAccountNotFound, want: "The reviewer account is not set up on this machine."},
		{name: "inherited full access", enabled: true, inherited: true, want: "The reviewer profile defaults to full access; choose its permissions explicitly."},
		{name: "explicit escalation", enabled: true, inherited: true, explicit: true, want: "Available · needs escalation acknowledgment at start"},
		{name: "available", enabled: true, want: "Available"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			acc.resolveErr, acc.inheritedFull = tc.err, tc.inherited
			m.work.GardenAutoReview = &config.GardenAutoReviewPrefs{Enabled: tc.enabled, Account: "one"}
			if tc.explicit {
				m.work.GardenAutoReview.Sandbox, m.work.GardenAutoReview.Approval = "danger-full-access", "never"
			}
			n, cmd := m.autoReviewResolve()
			if n.autoReviewAvailability() != "checking…" {
				t.Fatal("pending status missing")
			}
			current := cmd().(settingsAutoReviewResolvedMsg)
			n, _ = send(t, n, current)
			if n.autoReviewAvailability() != tc.want {
				t.Fatalf("got %q want %q", n.autoReviewAvailability(), tc.want)
			}
			n, _ = send(t, n, settingsAutoReviewResolvedMsg{gen: current.gen - 1, resolved: reviewpass.Resolved{Reason: "stale"}})
			if n.autoReviewAvailability() != tc.want {
				t.Fatal("stale resolution replaced current result")
			}
			// Also reject an old result while the next check is still pending.
			n, _ = n.autoReviewResolve()
			n, _ = send(t, n, current)
			if n.autoReviewAvailability() != "checking…" {
				t.Fatal("stale reply cleared pending")
			}
		})
	}
	m.agents.api.Accounts = acc.Accounts // no optional LaunchResolver
	m.work.GardenAutoReview = &config.GardenAutoReviewPrefs{Enabled: true, Account: "one"}
	m, cmd := m.autoReviewResolve()
	m = autoReviewDrain(t, m, cmd)
	if m.autoReviewAvailability() != "Agent accounts are unavailable here." {
		t.Fatal("resolver unavailability not shown", m.autoReviewAvailability())
	}
	if !acc.deadlines {
		t.Fatal("resolution missing timeout")
	}
}

func TestSettingsAutoReviewWarmAndDefaultsGenerations(t *testing.T) {
	m, acc := autoReviewTestModel(t)
	m.work.GardenAutoReview = &config.GardenAutoReviewPrefs{Account: "one"}
	m, cmd := m.settingsAutoReviewWarm()
	m = autoReviewDrain(t, m, cmd)
	for range 3 {
		m, cmd = m.settingsAutoReviewWarm()
		m = autoReviewDrain(t, m, cmd)
	}
	if acc.lists != 1 || !reflect.DeepEqual(acc.defaultsReads, []agents.AccountID{"one"}) {
		t.Fatal("reads repeated within visit", acc.lists, acc.defaultsReads)
	}
	oldListGen, oldDefaultsGen := m.set.autoReview.accounts.gen, m.set.autoReview.defaultsGen
	oldDefaults := m.set.autoReview.defaults
	m = autoReviewApplyField(t, m, "auto_review_account", "two")
	m, _ = send(t, m, settingsAutoReviewDefaultsMsg{gen: oldDefaultsGen, account: "one", defaults: oldDefaults})
	if m.set.autoReview.defaultsAccount != "two" || m.set.autoReview.defaults.LaunchFields[0].ID != "permission_mode" {
		t.Fatal("stale account defaults accepted")
	}
	gen := m.set.autoReview.defaultsGen
	m, _ = send(t, m, settingsAutoReviewDefaultsMsg{gen: gen, account: "one", defaults: oldDefaults})
	if m.set.autoReview.defaults.LaunchFields[0].ID != "permission_mode" {
		t.Fatal("wrong account accepted at current generation")
	}
	m.set.cat = catGeneral
	m, _ = m.settingsAutoReviewWarm()
	m, _ = send(t, m, settingsAutoReviewAccountsMsg{gen: oldListGen, items: nil})
	if len(m.set.autoReview.accounts.items) != 2 {
		t.Fatal("old visit list accepted")
	}
	acc.ListErr = errors.New("private provider error")
	m.set.cat = catGarden
	m, cmd = m.settingsAutoReviewWarm()
	m = autoReviewDrain(t, m, cmd)
	if acc.lists != 2 || len(m.set.autoReview.accounts.items) != 2 || m.set.autoReview.accounts.err == "" {
		t.Fatal("failed read cleared last good list")
	}
	if strings.Contains(autoReviewField(t, "auto_review_account").helpFn(m), "private provider") {
		t.Fatal("raw provider error shown")
	}
	m, _ = m.selectWorkspace(wsWorktrees)
	m, _ = m.selectWorkspace(wsSettings)
	m, cmd = m.settingsAutoReviewWarm()
	m = autoReviewDrain(t, m, cmd)
	if acc.lists != 3 {
		t.Fatal("workspace reentry did not start new visit")
	}
	if len(acc.Updates()) != 0 {
		t.Fatal("warm-up wrote account defaults")
	}
}

func TestSettingsAutoReviewRestoreOnlyPersonalPolicy(t *testing.T) {
	m, acc := autoReviewTestModel(t)
	nm, save := m.editPrefs(func(p *config.Prefs) {
		p.GardenAssist = &config.GardenAssistPrefs{Account: "assistant"}
		p.GardenReview = &config.GardenReviewPrefs{AccountID: "playground"}
	})
	m = autoReviewDrain(t, nm.(Model), save)
	m = autoReviewApplyField(t, m, "auto_review_account", "one")
	m = autoReviewApplyField(t, m, "auto_review_enabled", "true")
	before := m.work.Clone()
	m, cmd := m.confirmAutoReviewRestore()
	if cmd != nil || m.modal == nil || m.modal.Kind() != modals.KindGardenAutoReviewRestore || !reflect.DeepEqual(m.work, before) {
		t.Fatal("restore skipped confirmation")
	}
	m, _ = send(t, m, modals.CancelMsg{Kind: modals.KindGardenAutoReviewRestore})
	if !reflect.DeepEqual(diskPrefs(t), before) {
		t.Fatal("cancelled restore changed saved preferences")
	}
	m, _ = m.confirmAutoReviewRestore()
	m, cmd = send(t, m, modals.SubmitMsg{Kind: modals.KindGardenAutoReviewRestore})
	m = autoReviewDrain(t, m, cmd)
	before.GardenAutoReview = nil
	if !reflect.DeepEqual(m.work, before) || !reflect.DeepEqual(diskPrefs(t), before) {
		t.Fatal("restore changed unrelated preferences")
	}
	if m.autoReviewAvailability() != "Automatic review is off." || len(acc.Updates()) != 0 {
		t.Fatal("restore did not turn off or wrote account defaults")
	}
}

func TestSettingsAutoReviewPreservedNewerReadOnly(t *testing.T) {
	e := newSettingsEnv(t)
	e.writeState(t, `{"prefs":{"garden_auto_review":{"version":999,"enabled":true,"account":"future","extra":"keep"}}}`)
	m := e.model(t, 120, 35)
	m.workspace, m.shellFocus, m.set.cat, m.set.sec, m.set.level = wsSettings, shellBody, catGarden, len(gardenSections())-1, levelField
	if !m.state.GardenAutoReviewPreserved() {
		t.Fatal("test policy not preserved")
	}
	before := m.work.Clone()
	for i, f := range gardenAutoReviewSection().fields {
		if f.kind == kindReadonly {
			continue
		}
		m.set.field = i
		if f.display(m) != autoReviewPreservedReason || f.helpFn(m) != autoReviewPreservedReason {
			t.Fatalf("%s missing preserved reason", f.id)
		}
		nm, cmd := m.activateField()
		m = autoReviewDrain(t, nm.(Model), cmd)
		if m.set.editing || m.modal != nil || m.status != autoReviewPreservedReason || !reflect.DeepEqual(before, m.work) {
			t.Fatalf("%s remained editable", f.id)
		}
	}
	m, cmd := m.settingsAutoReviewWarm()
	m = autoReviewDrain(t, m, cmd)
	if m.autoReviewAvailability() != "Automatic review is off." {
		t.Fatal("preserved policy not off")
	}
	nm, cmd := m.autoReviewRestore()
	m = autoReviewDrain(t, nm.(Model), cmd)
	if !m.state.GardenAutoReviewPreserved() || m.status != autoReviewPreservedReason {
		t.Fatal("restore destroyed protected policy")
	}
	// Exercise the persistence error even if protection appeared after editing.
	nm, cmd = m.editPrefs(func(p *config.Prefs) { p.GardenAutoReview = &config.GardenAutoReviewPrefs{Account: "one"} })
	m = autoReviewDrain(t, nm.(Model), cmd)
	if m.status != autoReviewPreservedReason || m.set.saveErr != autoReviewPreservedReason || m.work.GardenAutoReview != nil {
		t.Fatal("newer policy save error not surfaced or rolled back", m.status)
	}
	// An unrelated save must retain the raw policy, including unknown fields.
	nm, cmd = m.editPrefs(func(p *config.Prefs) { p.Editor = "test-editor" })
	m = autoReviewDrain(t, nm.(Model), cmd)
	st, err := config.LoadState()
	if err != nil || !st.GardenAutoReviewPreserved() || st.PrefsSnapshot().Editor != "test-editor" {
		t.Fatal("unrelated save lost protected policy", err)
	}
}

func TestSettingsAutoReviewSaveFailureRefreshesRolledBackAccount(t *testing.T) {
	m, acc := autoReviewTestModel(t)
	m, warm := m.settingsAutoReviewWarm()
	m = autoReviewDrain(t, m, warm)
	m = autoReviewApplyField(t, m, "auto_review_account", "one")
	m = autoReviewApplyField(t, m, "auto_review_enabled", "true")
	nm, _ := autoReviewField(t, "auto_review_account").apply(m, "two")
	m = nm.(Model)
	staleGen := m.set.autoReview.gen
	// The persist failed before any result from the optimistic account arrives.
	m, refresh := send(t, m, settingsSavedMsg{gen: m.prefsGen, err: errors.New("disk unavailable")})
	m = autoReviewDrain(t, m, refresh)
	if m.autoReviewPolicy().Account != "one" || m.set.autoReview.defaultsAccount != "one" || !m.set.autoReview.defaultsLoaded || m.autoReviewAvailability() != "Available" {
		t.Fatal("rollback left options or availability for the rejected account")
	}
	m, _ = send(t, m, settingsAutoReviewResolvedMsg{gen: staleGen, resolved: reviewpass.Resolved{Reason: "stale optimistic policy"}})
	if m.autoReviewAvailability() != "Available" || len(acc.Updates()) != 0 {
		t.Fatal("rollback accepted stale resolution or wrote account defaults")
	}
}

// Embedding the required interface deliberately hides optional CachedDefaulter.
type autoReviewUncachedAccounts struct{ agentsapi.Accounts }

func TestSettingsAutoReviewNeverDiscoversModels(t *testing.T) {
	for _, cached := range []bool{false, true} {
		t.Run(fmt.Sprintf("cached=%v", cached), func(t *testing.T) {
			m, acc := autoReviewTestModel(t)
			if !cached {
				m.agents.api.Accounts = autoReviewUncachedAccounts{acc}
			}
			m.work.GardenAutoReview = &config.GardenAutoReviewPrefs{Account: "one"}
			m, cmd := m.settingsAutoReviewWarm()
			m = autoReviewDrain(t, m, cmd)
			m = autoReviewApplyField(t, m, "auto_review_account", "two")
			m = autoReviewApplyField(t, m, "auto_review_model", "typed")
			m = autoReviewApplyField(t, m, "auto_review_effort", "")
			m = autoReviewApplyField(t, m, "auto_review_permissions", "")
			m.set.cat = catGeneral
			m, _ = m.settingsAutoReviewWarm()
			m.set.cat = catGarden
			m, cmd = m.settingsAutoReviewWarm()
			m = autoReviewDrain(t, m, cmd)
			if len(acc.liveReads) != 0 {
				t.Fatalf("Settings ran live Defaults: %v", acc.liveReads)
			}
			if !cached {
				want := []settingChoice{{label: "Profile default", value: ""}}
				if !reflect.DeepEqual(m.autoReviewEfforts(), want) || !reflect.DeepEqual(m.autoReviewPermissions(), want) || m.autoReviewOptionsHelp() != "Launch options are not cached for this account yet." {
					t.Fatal("uncached options did not fall back to profile default")
				}
			}
		})
	}
}

func TestSettingsAutoReviewChecksEditorProtectsTypedChecks(t *testing.T) {
	for _, tc := range []struct {
		name     string
		checks   []config.GardenCheck
		editable bool
	}{
		{"empty", nil, true},
		{"lossless", []config.GardenCheck{{Name: "go test", Executable: "go", Args: []string{"test"}}}, true},
		{"folder", []config.GardenCheck{{Name: "go test", Executable: "go", Args: []string{"test"}, Dir: "internal"}}, false},
		{"custom name", []config.GardenCheck{{Name: "Go tests", Executable: "go", Args: []string{"test"}}}, false},
		{"spaced argument", []config.GardenCheck{{Name: "go one argument", Executable: "go", Args: []string{"one argument"}}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := autoReviewTestModel(t)
			m.work.GardenAutoReview = &config.GardenAutoReviewPrefs{Checks: tc.checks}
			before := m.work.Clone()
			m.set.field = 6
			nm, cmd := m.activateField()
			m = autoReviewDrain(t, nm.(Model), cmd)
			if m.set.editing != tc.editable || !reflect.DeepEqual(before, m.work) {
				t.Fatal("editor availability or checks changed", m.set.editing)
			}
			if !tc.editable && m.status != "These checks have a folder, custom name or spaced argument; edit them in state.json or restore defaults." {
				t.Fatal("missing refusal", m.status)
			}
		})
	}
}
