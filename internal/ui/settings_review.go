package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
	"github.com/Tiago-0liveira/bonsai/internal/ui/playgroundapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// The Garden review policy (plan 10.4): the local consent that lets Bonsai start bounded, read-only reviewer
// runs on this machine. It lives only in the personal preferences. Nothing read from Garden, GitHub or a
// reviewer's report ever writes it, and turning it on always goes through a confirmation that names what is
// authorized.

const reviewRoleSentence = "the reviewer can read the repository and write only its own report file"

// reviewConsentSentence is shown verbatim in the confirmation of enabling.
const reviewConsentSentence = "Authorizes bounded automatic read-only reviewer runs on this machine. A remote Issue or PR edit cannot enable this."

// settingsReview is the account list the section chooses from, read through a command.
type settingsReview struct {
	asked  bool
	gen    uint64
	listed bool
	items  []agentsapi.Account
	err    string
}

type settingsReviewAccountsMsg struct {
	gen   uint64
	items []agentsapi.Account
	err   error
}

var reviewTriggerLabels = map[string]string{
	config.GardenReviewTriggerManual:         "Manual",
	config.GardenReviewTriggerAwaitingReview: "When awaiting review",
	config.GardenReviewTriggerNewCommit:      "On each new commit",
}

var reviewTriggerHelp = map[string]string{
	config.GardenReviewTriggerManual:         "Manual: a reviewer runs only when you choose Retry review in the Playground.",
	config.GardenReviewTriggerAwaitingReview: "Awaiting review: one run when a Bonsai-launched attempt reaches human review.",
	config.GardenReviewTriggerNewCommit:      "New commit: one run for each new commit of an attempt, up to the run limit.",
}

// settingsWarm reads Garden status and the personal review options for the current category.
func (m Model) settingsWarm() (Model, tea.Cmd) {
	m, g := m.gardenWarm()
	m, r := m.settingsReviewWarm()
	m, ar := m.settingsAutoReviewWarm()
	return m, tea.Batch(g, r, ar)
}

// settingsReviewWarm reads the accounts once per visit to the Agents category. Without an account backend there is
// nothing to read and the section says so.
func (m Model) settingsReviewWarm() (Model, tea.Cmd) {
	if m.workspace != wsSettings || m.curCategory().id != "agents" {
		m.set.rev.asked = false
		return m, nil
	}
	rv := &m.set.rev
	if rv.asked || m.agents.api.Accounts == nil {
		return m, nil
	}
	rv.asked = true
	rv.gen++
	gen, acc := rv.gen, m.agents.api.Accounts
	ctx := m.pgCtx()
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		items, err := acc.List(ctx)
		return settingsReviewAccountsMsg{gen: gen, items: items, err: err}
	}
}

// onSettingsReviewAccounts keeps the newest list; a failed read keeps the last good one.
func (m Model) onSettingsReviewAccounts(msg settingsReviewAccountsMsg) Model {
	rv := &m.set.rev
	if msg.gen != rv.gen {
		return m
	}
	if msg.err != nil {
		rv.err = "the accounts could not be read"
		return m
	}
	rv.items, rv.listed, rv.err = msg.items, true, ""
	return m
}

// reviewAccountReason is why an account cannot be a reviewer; empty when it can. Only the provider's own
// verdict counts: an enforced read-only session that may still write the one report file.
func reviewAccountReason(a agentsapi.Account) string {
	switch {
	case !a.ReadOnly.Supported:
		if r := pgClean(a.ReadOnly.Reason, 100); r != "" {
			return r
		}
		return "the provider has no enforced read-only mode"
	case !a.ReadOnly.PlanFile:
		return "the provider cannot limit writes to the single report file"
	}
	return ""
}

func reviewAccountName(a agentsapi.Account) string {
	return boardTitle(a.Name) + " (" + providerName(a.Provider) + ")"
}

// reviewNoneReason is the reason no account can be chosen at all.
func (m Model) reviewNoneReason() string {
	rv := m.set.rev
	switch {
	case m.agents.api.Accounts == nil:
		return "agent accounts are unavailable here"
	case rv.err != "" && !rv.listed:
		return rv.err
	case !rv.listed:
		return "the accounts are still being read"
	case len(rv.items) == 0:
		return "no agent account exists yet; add one in Accounts"
	}
	for _, a := range rv.items {
		if r := reviewAccountReason(a); r != "" {
			return reviewAccountName(a) + ": " + r
		}
	}
	return ""
}

func (m Model) reviewEligible() int {
	n := 0
	for _, a := range m.set.rev.items {
		if reviewAccountReason(a) == "" {
			n++
		}
	}
	return n
}

func (m Model) reviewChoices() []settingChoice {
	var out []settingChoice
	for _, a := range m.set.rev.items {
		out = append(out, settingChoice{label: reviewAccountName(a), value: string(a.ID), off: reviewAccountReason(a)})
	}
	return out
}

func (m Model) reviewAccountByID(id string) (agentsapi.Account, bool) {
	for _, a := range m.set.rev.items {
		if string(a.ID) == id {
			return a, true
		}
	}
	return agentsapi.Account{}, false
}

// reviewPrefsOf allocates the policy only when a preference is edited.
func reviewPrefsOf(p *config.Prefs) *config.GardenReviewPrefs {
	if p.GardenReview == nil {
		p.GardenReview = &config.GardenReviewPrefs{}
	}
	return p.GardenReview
}

func (m Model) reviewPolicy() config.GardenReviewPrefs { return m.work.GardenReview.Effective() }

// reviewRefusal is why a policy cannot be saved, in words; empty when it can.
func (m Model) reviewRefusal(next config.GardenReviewPrefs) string {
	if next.Enabled {
		if next.AccountID == "" {
			return "Choose an account that can run a read-only reviewer before turning review on."
		}
		a, ok := m.reviewAccountByID(next.AccountID)
		switch {
		case !m.set.rev.listed:
			return "No account can run a read-only reviewer: " + m.reviewNoneReason() + "."
		case !ok:
			return "The chosen account no longer exists. Choose another before turning review on."
		}
		if r := reviewAccountReason(a); r != "" {
			return reviewAccountName(a) + " cannot run a read-only reviewer: " + r + "."
		}
	}
	if err := next.Validate(); err != nil {
		return "This policy cannot be saved (" + strings.TrimPrefix(err.Error(), config.ErrInvalidGardenReview.Error()+": ") + ")."
	}
	return ""
}

// reviewApply is the one write path of the section. A change that turns review on, or changes who runs it or when
// while it is on, asks first; everything else (turning it off, the limits) saves at once. An incomplete or invalid
// policy is refused with the reason and nothing changes.
func (m Model) reviewApply(change func(*config.GardenReviewPrefs)) (tea.Model, tea.Cmd) {
	cur := m.reviewPolicy()
	next := cur
	change(&next)
	if next == cur {
		return m, nil
	}
	if why := m.reviewRefusal(next); why != "" {
		m.set.editErr, m.status = why, why
		return m, nil
	}
	if next.Enabled && (!cur.Enabled || next.AccountID != cur.AccountID || next.Trigger != cur.Trigger) {
		return m.reviewConsent(next, cur.Enabled), nil
	}
	return m.editPrefs(func(p *config.Prefs) { *reviewPrefsOf(p) = next })
}

// reviewConsent names account, role, trigger and scope, and what the consent authorizes.
func (m Model) reviewConsent(next config.GardenReviewPrefs, changing bool) Model {
	name := next.AccountID
	if a, ok := m.reviewAccountByID(next.AccountID); ok {
		name = reviewAccountName(a)
	}
	title := "Turn on automatic read-only review?"
	if changing {
		title = "Change who runs the review or when?"
	}
	width := max(min(m.width-14, 92), 24)
	body := "Account: " + pgClean(name, 80) + "\n" +
		"Role: " + playgroundapi.ReviewRole + ": " + reviewRoleSentence + "\n" +
		"Trigger: " + reviewTriggerLabels[next.Trigger] + "\n" +
		"Scope: Bonsai-launched Garden attempts\n" +
		fmt.Sprintf("Limit: at most %d automatic runs per attempt\n\n", next.MaxAutoRuns) +
		reviewConsentSentence
	m.set.revPending = &next
	modal := modals.NewConfirm(modals.KindGardenReviewEnable, title)
	modal.SetBody(wrapParagraphs(body, width))
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m
}

// wrapParagraphs wraps each "\n" separated paragraph to width.
func wrapParagraphs(text string, width int) string {
	var out []string
	for _, para := range strings.Split(text, "\n") {
		out = append(out, strings.Split(ansi.Wrap(para, width, ""), "\n")...)
	}
	return strings.Join(out, "\n")
}

// reviewConsentConfirmed saves exactly the policy the dialog showed, after checking it again against what is known
// now: an account that has gone, or a list that changed, never gets a stale consent.
func (m Model) reviewConsentConfirmed() (tea.Model, tea.Cmd) {
	p := m.set.revPending
	m.set.revPending = nil
	if p == nil {
		return m, nil
	}
	if why := m.reviewRefusal(*p); why != "" {
		m.set.editErr, m.status = why, why
		return m, nil
	}
	next := *p
	return m.editPrefs(func(pr *config.Prefs) { *reviewPrefsOf(pr) = next })
}

func (m Model) reviewHelpAccount() string {
	if m.reviewEligible() == 0 {
		return "No account can run a read-only reviewer: " + m.reviewNoneReason() + "."
	}
	return "Only accounts whose provider enforces read-only access can be chosen; others are listed with the reason."
}

func gardenReviewSection() settingSection {
	var runs []settingChoice
	for n := 1; n <= config.MaxGardenReviewMaxAutoRuns; n++ {
		v := strconv.Itoa(n)
		runs = append(runs, settingChoice{label: v, value: v})
	}
	var triggers []settingChoice
	for _, t := range []string{config.GardenReviewTriggerManual, config.GardenReviewTriggerAwaitingReview, config.GardenReviewTriggerNewCommit} {
		triggers = append(triggers, settingChoice{label: reviewTriggerLabels[t], value: t})
	}
	origin := func(m Model) string { return personalOrDefault(m.work.GardenReview != nil) }
	return settingSection{id: "review", name: "Garden review policy", extra: reviewAccountLines, fields: []settingField{
		{
			id: "review_enabled", label: "Review enabled", scope: scopeMachine, kind: kindToggle,
			help:    "Off by default. Turning it on asks first and names the account, role, trigger and scope.",
			choices: boolChoices(),
			get:     func(m Model) string { return strconv.FormatBool(m.reviewPolicy().Enabled) },
			origin:  origin,
			apply: func(m Model, v string) (tea.Model, tea.Cmd) {
				return m.reviewApply(func(p *config.GardenReviewPrefs) { p.Enabled = v == "true" })
			},
		},
		{
			id: "review_account", label: "Review account", scope: scopeMachine, kind: kindChoice,
			helpFn: func(m Model) string { return m.reviewHelpAccount() },
			dyn:    func(m Model) []settingChoice { return m.reviewChoices() },
			get:    func(m Model) string { return m.reviewPolicy().AccountID },
			display: func(m Model) string {
				id := m.reviewPolicy().AccountID
				if a, ok := m.reviewAccountByID(id); ok {
					return reviewAccountName(a)
				}
				switch {
				case id != "" && m.set.rev.listed:
					return "missing: choose again"
				case id != "":
					return "reading accounts…"
				case m.reviewEligible() == 0 && m.set.rev.listed:
					return "none can review"
				}
				return "none chosen"
			},
			origin: origin,
			apply: func(m Model, v string) (tea.Model, tea.Cmd) {
				if v == "" {
					why := "No account can run a read-only reviewer: " + m.reviewNoneReason() + "."
					m.set.editErr, m.status = why, why
					return m, nil
				}
				return m.reviewApply(func(p *config.GardenReviewPrefs) { p.AccountID = v })
			},
		},
		{
			id: "review_role", label: "Reviewer role", scope: scopeNone, kind: kindReadonly,
			help:    reviewRoleSentence,
			display: func(Model) string { return playgroundapi.ReviewRole },
			origin:  func(Model) string { return "fixed: not editable" },
		},
		{
			id: "review_trigger", label: "Trigger", scope: scopeMachine, kind: kindChoice,
			choices: triggers,
			helpFn:  func(m Model) string { return reviewTriggerHelp[m.reviewPolicy().Trigger] },
			get:     func(m Model) string { return m.reviewPolicy().Trigger },
			origin:  origin,
			apply: func(m Model, v string) (tea.Model, tea.Cmd) {
				return m.reviewApply(func(p *config.GardenReviewPrefs) { p.Trigger = v })
			},
		},
		{
			id: "review_scope", label: "Scope", scope: scopeNone, kind: kindReadonly,
			help:    "Reviews only cover attempts Bonsai launched on this machine, never manual sessions.",
			display: func(Model) string { return "Bonsai-launched Garden attempts" },
			origin:  func(Model) string { return "fixed" },
		},
		{
			id: "review_max_runs", label: "Automatic runs per attempt", scope: scopeMachine, kind: kindChoice,
			help:    "At most this many automatic reviewer runs for one attempt (1-10, default 3). A manual retry is not counted.",
			choices: runs,
			get:     func(m Model) string { return strconv.Itoa(m.reviewPolicy().MaxAutoRuns) },
			origin:  origin,
			apply: func(m Model, v string) (tea.Model, tea.Cmd) {
				n, _ := strconv.Atoi(v)
				return m.reviewApply(func(p *config.GardenReviewPrefs) { p.MaxAutoRuns = n })
			},
		},
		{
			id: "review_require_ci", label: "Require CI passed to accept", scope: scopeMachine, kind: kindToggle,
			help:    "Accepting while CI has not passed (or is unavailable) needs an extra acknowledgment.",
			choices: boolChoices(),
			get:     func(m Model) string { return strconv.FormatBool(m.reviewPolicy().RequireCI) },
			origin:  origin,
			apply: func(m Model, v string) (tea.Model, tea.Cmd) {
				return m.reviewApply(func(p *config.GardenReviewPrefs) { p.RequireCI = v == "true" })
			},
		},
		{
			id: "review_block_high", label: "Block accept on open high", scope: scopeMachine, kind: kindToggle,
			help:    "Accepting with open high findings needs an extra acknowledgment.",
			choices: boolChoices(),
			get:     func(m Model) string { return strconv.FormatBool(m.reviewPolicy().BlockOnHigh) },
			origin:  origin,
			apply: func(m Model, v string) (tea.Model, tea.Cmd) {
				return m.reviewApply(func(p *config.GardenReviewPrefs) { p.BlockOnHigh = v == "true" })
			},
		},
	}}
}

// reviewAccountLines list every account with whether it can be the reviewer; the others stay visible with the
// provider's reason and are never selectable.
func reviewAccountLines(m Model, width int) []string {
	p := theme.Current
	out := []string{seg(p.Muted, p.Background, true).Render(" REVIEW ACCOUNTS")}
	add := func(fg string, text string) {
		c := p.Muted
		if fg == "warn" {
			c = p.Warning
		}
		out = append(out, seg(c, p.Background, false).Render(ansi.Truncate(" "+text, max(width, 1), "…")))
	}
	if m.reviewEligible() == 0 {
		add("warn", "No account can run a read-only reviewer: "+m.reviewNoneReason()+".")
	}
	for _, a := range m.set.rev.items {
		if r := reviewAccountReason(a); r != "" {
			add("", "  "+reviewAccountName(a)+": not selectable: "+r)
		} else {
			add("", "  "+reviewAccountName(a)+": can run a read-only reviewer")
		}
	}
	return out
}
