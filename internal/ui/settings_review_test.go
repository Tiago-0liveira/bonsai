package ui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
)

func TestSettingsReviewLocalConsentAndPersistence(t *testing.T) {
	e := newSettingsEnv(t)
	m := openSettings(t, e.model(t, 120, 35), catAgents, 1, 0)
	if m.reviewPolicy().Enabled {
		t.Fatal("review is not off by default")
	}
	// No account, or a provider that cannot enforce the role, cannot enable it.
	nm, cmd := m.reviewApply(func(p *config.GardenReviewPrefs) { p.Enabled = true })
	m = settle(t, nm.(Model), cmd)
	if m.modal != nil || m.work.GardenReview != nil || !strings.Contains(m.status, "Choose an account") {
		t.Fatal("enabled without an account")
	}
	a := agentsapi.Account{Account: agents.Account{ID: "reviewer", Name: "work", Provider: "claude"}, ReadOnly: agents.ReadOnlySupport{Supported: true, PlanFile: true}}
	m.set.rev = settingsReview{listed: true, items: []agentsapi.Account{a}}
	nm, cmd = m.reviewApply(func(p *config.GardenReviewPrefs) {
		p.AccountID = "reviewer"
		p.Trigger = config.GardenReviewTriggerAwaitingReview
		p.Enabled = true
	})
	m = settle(t, nm.(Model), cmd)
	if m.modal == nil || m.modal.Kind() != modals.KindGardenReviewEnable || diskPrefs(t).GardenReview != nil {
		t.Fatal("enabled before consent")
	}
	mustContain(t, "review consent", strings.Join(strings.Fields(view(m)), " "), "work", "read-only", "When awaiting review", "Bonsai-launched Garden attempts", "remote Issue or PR", "edit cannot enable this.")
	m, _ = send(t, m, modals.CancelMsg{Kind: modals.KindGardenReviewEnable})
	if m.set.revPending != nil || m.work.GardenReview != nil {
		t.Fatal("cancel retained pending consent")
	}
	nm, cmd = m.reviewApply(func(p *config.GardenReviewPrefs) {
		p.AccountID = "reviewer"
		p.Trigger = config.GardenReviewTriggerAwaitingReview
		p.Enabled = true
	})
	m = settle(t, nm.(Model), cmd)
	m, cmd = send(t, m, modals.SubmitMsg{Kind: modals.KindGardenReviewEnable})
	m = settle(t, m, cmd)
	if !diskPrefs(t).GardenReview.Enabled {
		t.Fatal("local consent was not saved")
	}
	before := m.work.Clone()
	nm, _ = m.reviewApply(func(p *config.GardenReviewPrefs) { p.Trigger = config.GardenReviewTriggerNewCommit })
	m = nm.(Model)
	if m.modal == nil || !reflect.DeepEqual(before, m.work) {
		t.Fatal("changing an active trigger skipped consent")
	}
	m, _ = send(t, m, modals.CancelMsg{Kind: modals.KindGardenReviewEnable})
	nm, cmd = m.reviewApply(func(p *config.GardenReviewPrefs) { p.Enabled = false })
	m = settle(t, nm.(Model), cmd)
	if m.modal != nil || diskPrefs(t).GardenReview.Enabled {
		t.Fatal("disabling required consent or did not persist")
	}
}

func TestSettingsReviewAccountsMessageAndEligibility(t *testing.T) {
	e := newPgEnv(t, 80, 24)
	supported := agentsapi.Account{Account: agents.Account{ID: "ok", Name: "work", Provider: "claude"}, ReadOnly: agents.ReadOnlySupport{Supported: true, PlanFile: true}}
	unsupported := agentsapi.Account{Account: agents.Account{ID: "no", Name: "personal", Provider: "codex"}, ReadOnly: agents.ReadOnlySupport{Reason: "no enforced report-file scope"}}
	e.fb.Accounts.Items = []agentsapi.Account{unsupported, supported}
	e.m.workspace = wsSettings
	e.m.set.cat = catAgents
	m, cmd := e.m.settingsReviewWarm()
	m = pgDrain(t, m, cmd)
	if !m.set.rev.listed || len(m.reviewChoices()) != 2 || m.reviewEligible() != 1 || m.reviewChoices()[0].off == "" {
		t.Fatal("account results were not routed or unsupported account was selectable")
	}
	gen := m.set.rev.gen
	m, _ = send(t, m, settingsReviewAccountsMsg{gen: gen - 1, items: nil})
	if len(m.set.rev.items) != 2 {
		t.Fatal("late account list replaced current choices")
	}
	m.work.GardenReview = &config.GardenReviewPrefs{AccountID: "no"}
	nm, _ := m.reviewApply(func(p *config.GardenReviewPrefs) { p.Enabled = true })
	m = nm.(Model)
	if m.modal != nil || m.reviewPolicy().Enabled {
		t.Fatal("unsupported reviewer was enabled")
	}
	// Revalidate at confirmation if the chosen account disappears.
	m.work.GardenReview = &config.GardenReviewPrefs{AccountID: "ok"}
	nm, _ = m.reviewApply(func(p *config.GardenReviewPrefs) { p.Enabled = true })
	m = nm.(Model)
	m.set.rev.items = nil
	m, _ = send(t, m, modals.SubmitMsg{Kind: modals.KindGardenReviewEnable})
	if m.reviewPolicy().Enabled || m.set.revPending != nil {
		t.Fatal("stale account consent was accepted")
	}
}
