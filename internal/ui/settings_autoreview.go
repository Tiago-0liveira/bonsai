package ui

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/reviewpass"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
)

const autoReviewPreservedReason = "Saved by a newer Bonsai; edit it there or restore defaults"

// Account reads belong to a category visit; defaults also belong to an account.
// Resolution has its own generation because every edit invalidates its result.
type settingsAutoReview struct {
	accounts        settingsReview
	defaultsGen     uint64
	defaultsAccount string
	defaults        agentsapi.Defaults
	defaultsLoaded  bool
	defaultsErr     string
	gen             uint64
	pending         bool
	resolved        reviewpass.Resolved
}

type settingsAutoReviewAccountsMsg struct {
	gen   uint64
	items []agentsapi.Account
	err   error
}
type settingsAutoReviewDefaultsMsg struct {
	gen      uint64
	account  string
	defaults agentsapi.Defaults
	err      error
}
type settingsAutoReviewResolvedMsg struct {
	gen      uint64
	resolved reviewpass.Resolved
	err      error
}

func (m Model) autoReviewPreserved() bool {
	return m.state != nil && m.state.GardenAutoReviewPreserved()
}

func (m Model) autoReviewPolicy() config.GardenAutoReviewPrefs {
	if p := m.work.GardenAutoReview; p != nil {
		return *p.Clone()
	}
	return config.GardenAutoReviewPrefs{Version: config.GardenAutoReviewVersion}
}

func (m Model) settingsAutoReviewLeave() Model {
	r := &m.set.autoReview
	r.accounts.asked = false
	r.accounts.gen++
	r.defaultsGen++
	r.gen++
	r.pending = false
	return m
}

func (m Model) settingsAutoReviewWarm() (Model, tea.Cmd) {
	if m.workspace != wsSettings || m.curCategory().id != "garden" {
		if m.set.autoReview.accounts.asked {
			m = m.settingsAutoReviewLeave()
		}
		return m, nil
	}
	r := &m.set.autoReview
	if r.accounts.asked {
		return m, nil
	}
	r.accounts.asked = true
	// Keep backend-free Settings navigation synchronous when the built-in
	// policy is off. There is no account state to resolve in this case.
	if m.agents.api.Accounts == nil && m.work.GardenAutoReview == nil {
		r.resolved = reviewpass.Resolved{Reason: "Automatic review is off."}
		return m, nil
	}
	r.accounts.gen++
	gen, acc, ctx := r.accounts.gen, m.agents.api.Accounts, m.pgCtx()
	var list tea.Cmd
	if acc != nil {
		list = func() tea.Msg {
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			items, err := acc.List(ctx)
			return settingsAutoReviewAccountsMsg{gen: gen, items: items, err: err}
		}
	}
	m, defaults := m.autoReviewLoadDefaults()
	m, resolve := m.autoReviewResolve()
	return m, tea.Batch(list, defaults, resolve)
}

func (m Model) onSettingsAutoReviewAccounts(msg settingsAutoReviewAccountsMsg) Model {
	r := &m.set.autoReview.accounts
	if msg.gen != r.gen {
		return m
	}
	if msg.err != nil {
		r.err = "The accounts could not be read."
	} else {
		r.items, r.listed, r.err = msg.items, true, ""
	}
	return m
}

func (m Model) autoReviewLoadDefaults() (Model, tea.Cmd) {
	r := &m.set.autoReview
	r.defaultsGen++
	r.defaultsAccount = m.autoReviewPolicy().Account
	r.defaults, r.defaultsLoaded, r.defaultsErr = agentsapi.Defaults{}, false, ""
	gen, account, acc, ctx := r.defaultsGen, r.defaultsAccount, m.agents.api.Accounts, m.pgCtx()
	if account == "" || acc == nil || m.autoReviewPreserved() {
		return m, nil
	}
	cached, ok := acc.(agentsapi.CachedDefaulter)
	if !ok {
		r.defaultsErr = "Launch options are not cached for this account yet."
		return m, nil
	}
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		d, err := cached.CachedDefaults(ctx, agents.AccountID(account))
		return settingsAutoReviewDefaultsMsg{gen: gen, account: account, defaults: d, err: err}
	}
}

func (m Model) onSettingsAutoReviewDefaults(msg settingsAutoReviewDefaultsMsg) Model {
	r := &m.set.autoReview
	if msg.gen != r.defaultsGen || msg.account != r.defaultsAccount || msg.account != m.autoReviewPolicy().Account {
		return m
	}
	if msg.err != nil {
		r.defaultsErr = "The account's launch options could not be read."
		return m
	}
	r.defaults, r.defaultsLoaded, r.defaultsErr = msg.defaults, true, ""
	return m
}

func (m Model) autoReviewResolve() (Model, tea.Cmd) {
	r := &m.set.autoReview
	r.gen++
	r.pending = true
	gen, ctx := r.gen, m.pgCtx()
	p := m.work.GardenAutoReview.Clone()
	if m.autoReviewPreserved() {
		p = nil
	}
	resolver, _ := m.agents.api.Accounts.(agentsapi.LaunchResolver)
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		resolved, err := reviewpass.Resolve(ctx, p, nil, resolver)
		return settingsAutoReviewResolvedMsg{gen: gen, resolved: resolved, err: err}
	}
}

func (m Model) onSettingsAutoReviewResolved(msg settingsAutoReviewResolvedMsg) Model {
	r := &m.set.autoReview
	if msg.gen != r.gen {
		return m
	}
	r.pending = false
	r.resolved = msg.resolved
	if msg.err != nil {
		r.resolved = reviewpass.Resolved{Reason: "The reviewer availability could not be checked."}
	}
	return m
}

func (m Model) autoReviewAvailability() string {
	r := m.set.autoReview
	if m.autoReviewPreserved() {
		return "Automatic review is off."
	}
	if r.pending || r.resolved.Reason == "" && !r.resolved.Available {
		return "checking…"
	}
	if !r.resolved.Available {
		return r.resolved.Reason
	}
	if r.resolved.Policy.Reviewer.Escalated {
		return "Available · needs escalation acknowledgment at start"
	}
	return "Available"
}

func autoReviewRefusal(err error) string {
	return strings.TrimPrefix(err.Error(), config.ErrInvalidGardenAutoReview.Error()+": ")
}

func (m Model) autoReviewApply(change func(*config.GardenAutoReviewPrefs)) (tea.Model, tea.Cmd) {
	if m.autoReviewPreserved() {
		m.status = autoReviewPreservedReason
		return m, nil
	}
	cur, next := m.autoReviewPolicy(), m.autoReviewPolicy()
	change(&next)
	if next.Enabled && next.Account == "" {
		m.status = "Choose a reviewer account before turning automatic review on."
		m.set.editErr = m.status
		return m, nil
	}
	if err := next.Validate(); err != nil {
		m.status = autoReviewRefusal(err)
		m.set.editErr = m.status
		return m, nil
	}
	note := ""
	if next.Account != cur.Account {
		next.Model, next.Effort = "", ""
		next.PermissionMode, next.Sandbox, next.Approval, next.FullAccess = "", "", "", nil
		note = "Reviewer account changed; model, effort and permissions reset to profile defaults."
	}
	return m.autoReviewSave(&next, note, next.Account != cur.Account)
}

func (m Model) autoReviewSave(next *config.GardenAutoReviewPrefs, note string, accountChanged bool) (tea.Model, tea.Cmd) {
	nm, save := m.editPrefs(func(p *config.Prefs) { p.GardenAutoReview = next.Clone() })
	m = nm.(Model)
	if note != "" {
		m.status = note
		persist := save
		save = func() tea.Msg {
			msg := persist().(settingsSavedMsg)
			msg.note = note
			return msg
		}
	}
	var defaults tea.Cmd
	if accountChanged {
		m, defaults = m.autoReviewLoadDefaults()
	}
	m, resolve := m.autoReviewResolve()
	return m, tea.Batch(save, defaults, resolve)
}

func (m Model) autoReviewAccountChoices() []settingChoice {
	var out []settingChoice
	for _, a := range m.set.autoReview.accounts.items {
		out = append(out, settingChoice{label: reviewAccountName(a), value: string(a.ID)})
	}
	return out
}

func (m Model) autoReviewOptionsHelp() string {
	switch {
	case m.agents.api.Accounts == nil:
		return "Agent accounts are unavailable here."
	case m.autoReviewPolicy().Account == "":
		return "Choose a reviewer account first."
	case m.set.autoReview.defaultsErr != "":
		return m.set.autoReview.defaultsErr
	case !m.set.autoReview.defaultsLoaded:
		return "Reading the account's launch options…"
	}
	return "Empty inherits the account profile default. No account profile is changed."
}

func (m Model) autoReviewEfforts() []settingChoice {
	out := []settingChoice{{label: "Profile default", value: ""}}
	r, p := m.set.autoReview, m.autoReviewPolicy()
	if !r.defaultsLoaded || r.defaultsAccount != p.Account {
		return out
	}
	model := orDefault(p.Model, r.defaults.Values["model"])
	for _, opt := range r.defaults.Models {
		if opt.ID == model || model == "" && opt.Default {
			if opt.NoEffort {
				return out
			}
			if len(opt.Efforts) > 0 {
				for _, v := range opt.Efforts {
					if v != "" {
						out = append(out, settingChoice{label: v, value: v})
					}
				}
				return out
			}
		}
	}
	for _, f := range r.defaults.LaunchFields {
		if f.ID == "effort" {
			for _, o := range f.Options {
				if o.Value != "" {
					out = append(out, settingChoice{label: o.Label, value: o.Value})
				}
			}
			break
		}
	}
	return out
}

func (m Model) autoReviewPermissionField() (agents.LaunchField, bool) {
	r := m.set.autoReview
	if !r.defaultsLoaded || r.defaultsAccount != m.autoReviewPolicy().Account {
		return agents.LaunchField{}, false
	}
	for _, f := range r.defaults.LaunchFields {
		switch f.ID {
		case "permissions", "permission_mode", "full_access":
			return f, true
		}
	}
	return agents.LaunchField{}, false
}

func (m Model) autoReviewPermissions() []settingChoice {
	out := []settingChoice{{label: "Profile default", value: ""}}
	f, ok := m.autoReviewPermissionField()
	if !ok {
		return out
	}
	for _, o := range f.Options {
		if o.Value == "" {
			continue
		}
		label := o.Label
		if o.Tone == agents.ToneDanger {
			label += " (full access)"
		}
		out = append(out, settingChoice{label: label, value: o.Value})
	}
	return out
}

func autoReviewNativeSet(p config.GardenAutoReviewPrefs) bool {
	return p.PermissionMode != "" || p.Sandbox != "" || p.Approval != "" || p.FullAccess != nil
}

func (m Model) autoReviewPermissionValue() string {
	p := m.autoReviewPolicy()
	if !autoReviewNativeSet(p) {
		return ""
	}
	if f, ok := m.autoReviewPermissionField(); ok {
		for _, o := range f.Options {
			if o.Value == "" {
				continue
			}
			l := agents.LaunchOptionsFrom([]agents.LaunchField{f}, "", map[string]string{f.ID: o.Value})
			fullEqual := l.FullAccess == nil && p.FullAccess == nil || l.FullAccess != nil && p.FullAccess != nil && *l.FullAccess == *p.FullAccess
			if l.PermissionMode == p.PermissionMode && l.Sandbox == p.Sandbox && l.Approval == p.Approval && fullEqual {
				return o.Value
			}
		}
	}
	return "custom"
}

func autoReviewChoiceLabel(choices []settingChoice, value string) string {
	for _, c := range choices {
		if c.value == value {
			return c.label
		}
	}
	return value
}

func (m Model) autoReviewPermissionHelp() string {
	help := m.autoReviewOptionsHelp()
	if f, ok := m.autoReviewPermissionField(); ok {
		for _, o := range f.Options {
			if o.Value != "" && o.Value == m.autoReviewPermissionValue() && o.Tone == agents.ToneDanger {
				return strings.TrimSpace(help + " " + f.Help + " " + o.Help)
			}
		}
	}
	return help
}

func (m Model) confirmAutoReviewRestore() (Model, tea.Cmd) {
	if m.autoReviewPreserved() {
		m.status = autoReviewPreservedReason
		return m, nil
	}
	modal := modals.NewConfirm(modals.KindGardenAutoReviewRestore, "Restore automatic review defaults?")
	modal.SetBody("Clear the saved reviewer, permissions and checks. Automatic review will be off.")
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m, nil
}

func (m Model) autoReviewRestore() (tea.Model, tea.Cmd) {
	if m.autoReviewPreserved() {
		m.status = autoReviewPreservedReason
		return m, nil
	}
	return m.autoReviewSave(nil, "", true)
}

func gardenAutoReviewSection() settingSection {
	origin := func(set func(config.GardenAutoReviewPrefs) bool) func(Model) string {
		return func(m Model) string {
			return personalOrDefault(m.work.GardenAutoReview != nil && set(m.autoReviewPolicy()))
		}
	}
	section := settingSection{id: "auto_review", name: "Automatic review", fields: []settingField{
		{id: "auto_review_enabled", label: "Review after implementation", kind: kindToggle, choices: boolChoices(),
			help:   "Default for new implementation starts. Each start still asks you to authorize one review, permitted fixes and checks; nothing runs from this setting alone.",
			get:    func(m Model) string { return strconv.FormatBool(m.autoReviewPolicy().Enabled) },
			origin: origin(func(p config.GardenAutoReviewPrefs) bool { return p.Enabled }),
			apply: func(m Model, v string) (tea.Model, tea.Cmd) {
				return m.autoReviewApply(func(p *config.GardenAutoReviewPrefs) { p.Enabled = v == "true" })
			},
		},
		{id: "auto_review_account", label: "Reviewer account", kind: kindChoice,
			helpFn: func(m Model) string {
				r := m.set.autoReview.accounts
				if r.err != "" {
					return r.err
				}
				if m.agents.api.Accounts == nil {
					return "Agent accounts are unavailable here."
				}
				if !r.listed {
					return "Reading accounts…"
				}
				if len(r.items) == 0 {
					return "No agent account exists yet; add one in Accounts."
				}
				return "Choose the reviewer for new implementation starts. Changing account clears model, effort and permissions."
			},
			dyn: func(m Model) []settingChoice { return m.autoReviewAccountChoices() },
			get: func(m Model) string { return m.autoReviewPolicy().Account },
			display: func(m Model) string {
				return autoReviewChoiceLabel(m.autoReviewAccountChoices(), orDefault(m.autoReviewPolicy().Account, "Choose an account"))
			},
			origin: origin(func(p config.GardenAutoReviewPrefs) bool { return p.Account != "" }),
			apply: func(m Model, v string) (tea.Model, tea.Cmd) {
				if v == "" {
					m.status = "Choose a reviewer account first."
					return m, nil
				}
				return m.autoReviewApply(func(p *config.GardenAutoReviewPrefs) { p.Account = v })
			},
		},
		{id: "auto_review_model", label: "Reviewer model", kind: kindText,
			help:    "Empty inherits the account profile default.",
			get:     func(m Model) string { return m.autoReviewPolicy().Model },
			display: func(m Model) string { return orDefault(m.autoReviewPolicy().Model, "profile default") },
			origin:  origin(func(p config.GardenAutoReviewPrefs) bool { return p.Model != "" }),
			apply: func(m Model, v string) (tea.Model, tea.Cmd) {
				return m.autoReviewApply(func(p *config.GardenAutoReviewPrefs) { p.Model = v })
			},
		},
		{id: "auto_review_effort", label: "Reviewer effort", kind: kindChoice,
			helpFn:  func(m Model) string { return m.autoReviewOptionsHelp() },
			dyn:     func(m Model) []settingChoice { return m.autoReviewEfforts() },
			get:     func(m Model) string { return m.autoReviewPolicy().Effort },
			display: func(m Model) string { return autoReviewChoiceLabel(m.autoReviewEfforts(), m.autoReviewPolicy().Effort) },
			origin:  origin(func(p config.GardenAutoReviewPrefs) bool { return p.Effort != "" }),
			apply: func(m Model, v string) (tea.Model, tea.Cmd) {
				return m.autoReviewApply(func(p *config.GardenAutoReviewPrefs) { p.Effort = v })
			},
		},
		{id: "auto_review_permissions", label: "Reviewer permissions", kind: kindChoice,
			helpFn: func(m Model) string { return m.autoReviewPermissionHelp() },
			dyn:    func(m Model) []settingChoice { return m.autoReviewPermissions() },
			get:    func(m Model) string { return m.autoReviewPermissionValue() },
			display: func(m Model) string {
				return autoReviewChoiceLabel(m.autoReviewPermissions(), m.autoReviewPermissionValue())
			},
			origin: origin(autoReviewNativeSet),
			apply: func(m Model, v string) (tea.Model, tea.Cmd) {
				var l agents.LaunchOptions
				if v != "" {
					f, ok := m.autoReviewPermissionField()
					if !ok {
						m.status = m.autoReviewOptionsHelp()
						return m, nil
					}
					l = agents.LaunchOptionsFrom([]agents.LaunchField{f}, "", map[string]string{f.ID: v})
				}
				return m.autoReviewApply(func(p *config.GardenAutoReviewPrefs) {
					p.PermissionMode, p.Sandbox, p.Approval, p.FullAccess = l.PermissionMode, l.Sandbox, l.Approval, l.FullAccess
				})
			},
		},
		{id: "auto_review_limit", label: "Review limit", kind: kindReadonly,
			display: func(Model) string { return "One review + fixes + checks" }, origin: func(Model) string { return "fixed" },
		},
		{id: "auto_review_checks", label: "Checks", kind: kindLink,
			help: "Typed commands run in the worktree after the review, separated by ';'. Words are split on spaces; no shell, quoting, pipes or variables.",
			get:  func(m Model) string { return config.FormatGardenChecks(m.autoReviewPolicy().Checks) },
			display: func(m Model) string {
				var names []string
				for _, c := range m.autoReviewPolicy().Checks {
					names = append(names, c.Name)
				}
				if len(names) == 0 {
					return "none"
				}
				return strings.Join(names, " · ")
			},
			origin: origin(func(p config.GardenAutoReviewPrefs) bool { return p.Checks != nil }),
			validate: func(v string) error {
				_, err := config.ParseGardenChecks(v)
				if err != nil {
					return errors.New(autoReviewRefusal(err))
				}
				return nil
			},
			apply: func(m Model, v string) (tea.Model, tea.Cmd) {
				checks, err := config.ParseGardenChecks(v)
				if err != nil {
					m.status = autoReviewRefusal(err)
					return m, nil
				}
				return m.autoReviewApply(func(p *config.GardenAutoReviewPrefs) { p.Checks = checks })
			},
		},
		{id: "auto_review_implementer", label: "Implementer", kind: kindReadonly,
			display: func(Model) string { return "Chosen at each start from the account's launch defaults" }, origin: func(Model) string { return "per start" },
		},
		{id: "auto_review_status", label: "Availability", kind: kindReadonly,
			display: func(m Model) string { return m.autoReviewAvailability() },
		},
		{id: "auto_review_restore", label: "Restore defaults", kind: kindLink,
			help:    "Clear personal automatic review defaults after confirmation.",
			display: func(Model) string { return "Restore defaults ›" },
			open:    func(m Model) (Model, tea.Cmd) { return m.confirmAutoReviewRestore() },
		},
	}}
	for i := range section.fields {
		f := &section.fields[i]
		f.scope = scopeMachine
		if f.kind == kindReadonly {
			continue
		}
		display, help, helpFn, apply := f.display, f.help, f.helpFn, f.apply
		get := f.get
		f.display = func(m Model) string {
			if m.autoReviewPreserved() {
				return autoReviewPreservedReason
			}
			if display != nil {
				return display(m)
			}
			return autoReviewChoiceLabel(boolChoices(), get(m))
		}
		f.helpFn = func(m Model) string {
			if m.autoReviewPreserved() {
				return autoReviewPreservedReason
			}
			if helpFn != nil {
				return helpFn(m)
			}
			return help
		}
		if apply != nil {
			f.apply = func(m Model, v string) (tea.Model, tea.Cmd) {
				if m.autoReviewPreserved() {
					m.status = autoReviewPreservedReason
					return m, nil
				}
				return apply(m, v)
			}
		}
		if f.id == "auto_review_checks" {
			field := *f
			f.open = func(m Model) (Model, tea.Cmd) {
				if m.autoReviewPreserved() {
					m.status = autoReviewPreservedReason
					return m, nil
				}
				checks := m.autoReviewPolicy().Checks
				parsed, err := config.ParseGardenChecks(config.FormatGardenChecks(checks))
				if err != nil || !reflect.DeepEqual(parsed, checks) {
					m.status = "These checks have a folder, custom name or spaced argument; edit them in state.json or restore defaults."
					return m, nil
				}
				return m.beginEdit(field), nil
			}
		}
	}
	return section
}
