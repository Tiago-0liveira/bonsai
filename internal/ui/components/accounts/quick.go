package accounts

import (
	"maps"
	"slices"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents/usagefmt"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

const (
	// quickInflight bounds concurrent Usage calls so opening the overlay does
	// not hit every provider at once.
	quickInflight = 3
	// quickFocusRows is the focused block: header, two meters, rule. It is
	// fixed so the overlay does not jump while data loads.
	quickFocusRows = 4
	// quickTailRows is the blank row and the "Updated" row under the list.
	quickTailRows = 2
)

// OpenAccountsMsg asks the parent to replace the overlay with the accounts
// modal selected on Account.
type OpenAccountsMsg struct{ Account agents.AccountID }

type (
	quickListMsg struct {
		seq      int
		accounts []agentsapi.Account
		err      error
	}
	quickUsageMsg struct {
		id    agents.AccountID
		seq   int
		usage agentsapi.Usage
		err   error
	}
)

type usageJob struct {
	id      agents.AccountID
	refresh bool
}

// Quick shows the focused account in detail and a stable list of all accounts. It is a value type like Model; results are ordered by
// per-account sequence numbers so a late answer never overwrites newer data.
type Quick struct {
	api   agentsapi.Accounts
	ascii bool
	now   func() time.Time
	w, h  int // screen size; the frame is derived by quickSize

	accounts []agentsapi.Account
	listed   bool
	listErr  string
	focus    agents.AccountID

	n        int // request counter
	listSeq  int
	usageSeq map[agents.AccountID]int // latest dispatched request per account
	queue    []usageJob               // waiting for a free slot
	inflight int                      // dispatched calls whose result has not arrived

	usage   map[agents.AccountID]agentsapi.Usage
	errs    map[agents.AccountID]string // failures with no earlier data to keep
	loading map[agents.AccountID]bool
}

// NewQuick builds the overlay focused on an account (the first one when
// focus is empty or unknown). Init starts loading.
func NewQuick(api agentsapi.Accounts, focus agents.AccountID, ascii bool) Quick {
	return Quick{
		api:      api,
		ascii:    ascii,
		now:      time.Now,
		focus:    focus,
		n:        1,
		listSeq:  1,
		usageSeq: map[agents.AccountID]int{},
		usage:    map[agents.AccountID]agentsapi.Usage{},
		errs:     map[agents.AccountID]string{},
		loading:  map[agents.AccountID]bool{},
	}
}

// WithClock sets the clock used for "Updated .. ago" and reset countdowns.
func (q Quick) WithClock(now func() time.Time) Quick {
	if now != nil {
		q.now = now
	}
	return q
}

// Init lists accounts; usage loads once the list is in.
func (q Quick) Init() tea.Cmd { return quickListCmd(q.api, q.listSeq) }

// SetSize gives the view the available pane size.
func (q *Quick) SetSize(w, h int) { q.w, q.h = w, h }

func quickListCmd(api agentsapi.Accounts, seq int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := callCtx()
		defer cancel()
		accounts, err := api.List(ctx)
		return quickListMsg{seq: seq, accounts: accounts, err: err}
	}
}

func quickUsageCmd(api agentsapi.Accounts, id agents.AccountID, seq int, refresh bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := callCtx()
		defer cancel()
		u, err := api.Usage(ctx, id, refresh)
		return quickUsageMsg{id: id, seq: seq, usage: u, err: err}
	}
}

// ordered is the display and j/k order: provider group, then name.
func (q Quick) ordered() []agentsapi.Account {
	out := Sidebar{Accounts: q.accounts}.Ordered()
	rank := func(p agents.ProviderID) int {
		if i := slices.Index(groupOrder, p); i >= 0 {
			return i
		}
		return len(groupOrder)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if ra, rb := rank(a.Provider), rank(b.Provider); ra != rb {
			return ra < rb
		}
		if a.Provider != b.Provider {
			return a.Provider < b.Provider
		}
		return a.Name < b.Name
	})
	return out
}

// Update handles keys and backend results.
func (q Quick) Update(msg tea.Msg) (Quick, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return q.onKey(msg)
	case quickListMsg:
		return q.onList(msg)
	case quickUsageMsg:
		return q.onUsage(msg)
	}
	return q, nil
}

func (q Quick) onKey(k tea.KeyMsg) (Quick, tea.Cmd) {
	switch k.String() {
	case "esc", "u":
		return q, emit(CloseMsg{})
	case "m":
		return q, emit(OpenAccountsMsg{Account: q.focus})
	case "j", "down":
		return q.move(1), nil
	case "k", "up":
		return q.move(-1), nil
	case "r":
		return q.refresh()
	}
	return q, nil
}

func (q Quick) move(delta int) Quick {
	ids := q.ordered()
	i := slices.IndexFunc(ids, func(a agentsapi.Account) bool { return a.ID == q.focus })
	if len(ids) == 0 || i < 0 {
		return q
	}
	q.focus = ids[min(max(i+delta, 0), len(ids)-1)].ID
	return q
}

// refresh re-reads every account from its provider. Without a list yet it
// retries the list instead.
func (q Quick) refresh() (Quick, tea.Cmd) {
	if !q.listed || q.listErr != "" {
		q.n++
		q.listSeq = q.n
		return q, quickListCmd(q.api, q.listSeq)
	}
	return q.enqueue(true)
}

// enqueue replaces the waiting jobs with one per account and starts as many
// as the cap allows. Calls already in flight keep counting against the cap;
// their results are dropped once a newer request for the account is sent.
func (q Quick) enqueue(refresh bool) (Quick, tea.Cmd) {
	q.queue = q.queue[:0:0]
	q.loading = maps.Clone(q.loading)
	accts := q.ordered()
	// The focused account goes first so the part that matters fills in first.
	sort.SliceStable(accts, func(i, j int) bool { return accts[i].ID == q.focus && accts[j].ID != q.focus })
	for _, a := range accts {
		q.queue = append(q.queue, usageJob{a.ID, refresh})
		q.loading[a.ID] = true
	}
	return q.pump()
}

// pump dispatches queued jobs up to the in-flight cap.
func (q Quick) pump() (Quick, tea.Cmd) {
	var cmds []tea.Cmd
	for q.inflight < quickInflight && len(q.queue) > 0 {
		job := q.queue[0]
		q.queue = q.queue[1:]
		q.n++
		q.usageSeq = maps.Clone(q.usageSeq)
		q.usageSeq[job.id] = q.n
		q.inflight++
		cmds = append(cmds, quickUsageCmd(q.api, job.id, q.n, job.refresh))
	}
	return q, tea.Batch(cmds...)
}

func (q Quick) onList(msg quickListMsg) (Quick, tea.Cmd) {
	if msg.seq != q.listSeq {
		return q, nil
	}
	q.listed = true
	if msg.err != nil {
		q.listErr = sanitize(msg.err)
		return q, nil
	}
	q.listErr = ""
	q.accounts = msg.accounts
	ids := q.ordered()
	if !slices.ContainsFunc(ids, func(a agentsapi.Account) bool { return a.ID == q.focus }) {
		q.focus = ""
		if len(ids) > 0 {
			q.focus = ids[0].ID
		}
	}
	return q.enqueue(false)
}

func (q Quick) onUsage(msg quickUsageMsg) (Quick, tea.Cmd) {
	// Every dispatched call frees its slot, even when its answer is dropped.
	q.inflight = max(q.inflight-1, 0)
	if msg.seq != q.usageSeq[msg.id] {
		return q.pump()
	}
	q.loading = maps.Clone(q.loading)
	delete(q.loading, msg.id)
	prev, have := q.usage[msg.id]
	if msg.err != nil {
		if !have {
			q.errs = maps.Clone(q.errs)
			q.errs[msg.id] = sanitize(msg.err)
			return q.pump()
		}
		// Provider failure keeps the last good numbers, marked stale.
		prev.Stale, prev.Err = true, sanitize(msg.err)
		msg.usage = prev
	}
	q.errs = maps.Clone(q.errs)
	delete(q.errs, msg.id)
	q.usage = maps.Clone(q.usage)
	q.usage[msg.id] = msg.usage
	return q.pump()
}

func (q Quick) busy() bool { return q.inflight > 0 || len(q.queue) > 0 }

// View fills the Agents pane and keeps the profile list in a stable order.
func (q Quick) View() string {
	if q.w <= 0 || q.h <= 0 {
		return ""
	}
	th := theme.Current
	dim := lipgloss.NewStyle().Foreground(th.Dim)
	w, h := FrameSize(q.w, q.h)
	inner := ContentWidth(w)

	footer := "↑↓ Profile · r Refresh · m Profiles · Esc Close"
	if inner < ansi.StringWidth(footer) {
		footer = "↑↓ Profile · r Refresh · m More · Esc"
	}

	var body []string
	switch {
	case !q.listed:
		body = []string{dim.Render("Loading accounts…")}
	case q.listErr != "":
		body = []string{lipgloss.NewStyle().Foreground(th.Danger).Render("Couldn't load accounts"), dim.Render(q.listErr)}
	case len(q.accounts) == 0:
		body = []string{dim.Render("No accounts yet.")}
	default:
		body = q.body(inner, max(BodyRows(h), 0))
	}
	return FrameSingle("Usage", body, footer, w, h)
}

func (q Quick) body(inner, rows int) []string {
	th := theme.Current
	dim := lipgloss.NewStyle().Foreground(th.Dim)

	accts := q.ordered()
	selected := slices.IndexFunc(accts, func(a agentsapi.Account) bool { return a.ID == q.focus })
	if selected < 0 {
		return nil
	}
	out := q.focusBlock(accts[selected], inner)
	out = append(out, dim.Render(strings.Repeat("─", inner)))

	// Include the selected profile in the list. Removing it used to reshuffle
	// every row on each arrow key, making the selection hard to follow.
	space := max(rows-quickFocusRows-quickTailRows, 0)
	list := q.otherRows(accts, inner)
	start := max(selected-space+1, 0)
	end := min(start+space, len(list))
	out = append(out, list[start:end]...)
	for len(out) < rows-quickTailRows {
		out = append(out, "")
	}
	out = append(out, "", dim.Render(q.updated()))
	return out
}

func (q Quick) updated() string {
	if q.busy() {
		return "Refreshing…"
	}
	if u, ok := q.usage[q.focus]; ok && !u.FetchedAt.IsZero() {
		return updatedText(q.now(), u.FetchedAt)
	}
	return ""
}

// focusBlock is exactly quickFocusRows-1 lines: header and two meter rows.
func (q Quick) focusBlock(a agentsapi.Account, inner int) []string {
	th := theme.Current
	dim := lipgloss.NewStyle().Foreground(th.Dim)

	head := theme.ProviderStyle(a.Provider).Render(theme.ProviderGlyph(a.Provider, q.ascii)) +
		" " + providerName(a.Provider) + " · " + a.Name
	u, have := q.usage[a.ID]
	if have && u.Stale {
		head += dim.Render("  stale")
	}
	lines := []string{head}

	switch {
	case !have && q.loading[a.ID]:
		lines = append(lines, dim.Render("Loading usage…"))
	case !have:
		lines = append(lines, dim.Render("Usage unavailable"))
		if e := q.errs[a.ID]; e != "" {
			lines = append(lines, dim.Render(e))
		}
	default:
		for _, l := range q.meterRows(u, inner) {
			if u.Stale {
				l = dim.Render(ansi.Strip(l))
			}
			lines = append(lines, l)
		}
	}
	for len(lines) < quickFocusRows-1 {
		lines = append(lines, "")
	}
	return lines[:quickFocusRows-1]
}

type meterRow struct {
	name  string
	limit *agents.UsageLimit
}

// meterRows are the two meters of the focused account: the headline windows,
// or the first two pools for providers that report pools.
func (q Quick) meterRows(u agentsapi.Usage, inner int) []string {
	var rows []meterRow
	if usesPools(u) {
		for i := range u.Snapshot.Limits {
			l := &u.Snapshot.Limits[i]
			rows = append(rows, meterRow{name: limitLabel(*l), limit: l})
		}
	} else {
		for _, w := range []struct {
			l        *agents.UsageLimit
			fallback string
		}{{u.Headline.Primary, "Primary"}, {u.Headline.Secondary, "Secondary"}} {
			if w.l != nil {
				rows = append(rows, meterRow{name: quickWindowName(w.l, w.fallback), limit: w.l})
			}
		}
	}
	rows = rows[:min(len(rows), 2)]
	if len(rows) == 0 {
		return []string{lipgloss.NewStyle().Foreground(theme.Current.Dim).Render("No usage reported.")}
	}

	labelW := 0
	for _, r := range rows {
		labelW = max(labelW, ansi.StringWidth(r.name))
	}
	// Leave room for the meter and a percentage even on narrow frames.
	labelW = min(labelW, max(inner-barCells-6, 1))
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		label := ansi.Truncate(r.name, labelW, "…")
		line := label + strings.Repeat(" ", max(labelW-ansi.StringWidth(label), 0)) + "  "
		_, ok := agents.RemainingFraction(r.limit)
		if !ok {
			out = append(out, line+lipgloss.NewStyle().Foreground(theme.Current.Dim).Render("Unknown"))
			continue
		}
		long := line + limitValue(r.limit, q.ascii, true)
		short := line + limitValue(r.limit, q.ascii, false)
		if r.limit.ResetsAt != nil {
			long += " · " + resetsText(q.now(), r.limit.ResetsAt)
			short += " · " + usagefmt.ResetsIn(q.now(), r.limit.ResetsAt, true)
		}
		if ansi.StringWidth(long) > inner {
			long = short
		}
		out = append(out, long)
	}
	return out
}

func quickWindowName(l *agents.UsageLimit, fallback string) string {
	switch name := usagefmt.WindowName(l, fallback); name {
	case "5h":
		return "5-hour"
	case "Week":
		return "Weekly"
	default:
		return name
	}
}

type quickRow struct {
	glyph    string
	name     string
	body     string // styled
	reset    string // styled; "stale" replaces it on stale rows
	bold     bool
	selected bool
	bodyW    int
	resetW   int
}

func (q Quick) otherRows(accts []agentsapi.Account, inner int) []string {
	th := theme.Current
	dim := lipgloss.NewStyle().Foreground(th.Dim)

	rows := make([]quickRow, 0, len(accts))
	nameW, bodyW, resetW := 0, 0, 0
	for _, a := range accts {
		r := quickRow{
			glyph:    theme.ProviderStyle(a.Provider).Render(theme.ProviderGlyph(a.Provider, q.ascii)),
			name:     a.Name,
			selected: a.ID == q.focus,
		}
		u, have := q.usage[a.ID]
		switch {
		case !have && q.loading[a.ID]:
			r.body = dim.Render("loading…")
		case !have:
			r.body = dim.Render("unavailable")
		default:
			text, rank, ok := q.compact(u)
			switch {
			case !ok:
				r.body = dim.Render("no usage")
			case u.Stale:
				r.body = dim.Render(ansi.Strip(text))
			case rank >= usagefmt.RankLow:
				r.body, r.bold = text, true
			default:
				r.body = text
			}
			switch {
			case u.Stale:
				r.reset = dim.Render("stale")
			case u.Headline.NextReset != nil && !usesPools(u):
				r.reset = dim.Render("reset " + usagefmt.ResetsIn(q.now(), u.Headline.NextReset, true))
			}
		}
		r.bodyW, r.resetW = ansi.StringWidth(r.body), ansi.StringWidth(r.reset)
		nameW = max(nameW, ansi.StringWidth(r.name))
		bodyW = max(bodyW, r.bodyW)
		resetW = max(resetW, r.resetW)
		rows = append(rows, r)
	}

	// glyph + space + name + gap + body (+ gap + reset): drop the reset
	// column, then shrink the name, before anything gets cut mid-number.
	withReset := resetW > 0
	if 4+nameW+2+bodyW+2+resetW > inner {
		withReset = false
	}
	nameW = min(nameW, max(inner-4-2-bodyW, min(nameW, 10)))

	out := make([]string, 0, len(rows))
	for _, r := range rows {
		name := ansi.Truncate(r.name, nameW, "…")
		if r.bold || r.selected {
			name = lipgloss.NewStyle().Bold(true).Render(name)
		}
		marker := "  "
		if r.selected {
			marker = lipgloss.NewStyle().Foreground(th.Accent).Bold(true).Render("> ")
		}
		line := marker + r.glyph + " " + name + strings.Repeat(" ", max(nameW-ansi.StringWidth(r.name), 0)) + "  " + r.body
		if withReset && r.reset != "" {
			line += strings.Repeat(" ", bodyW-r.bodyW) + "  " + r.reset
		}
		out = append(out, line)
	}
	return out
}

// compact is the one-line reading of an account and its worst severity.
// Providers with headline windows share the same bars and percentages.
func (q Quick) compact(u agentsapi.Usage) (string, usagefmt.Rank, bool) {
	var parts []string
	worst, found := 1.0, false
	note := func(l *agents.UsageLimit) (float64, bool) {
		f, ok := agents.RemainingFraction(l)
		if ok {
			found = true
			worst = min(worst, f)
		}
		return f, ok
	}
	if usesPools(u) {
		for i := range u.Snapshot.Limits {
			l := &u.Snapshot.Limits[i]
			if _, ok := note(l); ok {
				parts = append(parts, limitLabel(*l)+" "+limitValue(l, q.ascii, false))
			}
		}
		if !found {
			return "", 0, false
		}
		return strings.Join(parts, " · "), usagefmt.RankOf(worst), true
	}
	note(u.Headline.Primary)
	note(u.Headline.Secondary)
	text := Headline(u.Headline, q.now(), q.ascii, true)
	if !found || text == "" {
		return "", 0, false
	}
	return text, usagefmt.RankOf(worst), true
}
