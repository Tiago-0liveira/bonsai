package accounts

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
	"github.com/Tiago-0liveira/bonsai/internal/ui/usagestore"
)

// Tab is a section of the right column.
type Tab int

const (
	TabOverview Tab = iota
	TabUsage
	TabDefaults
	tabCount
)

// Options configures the profile view.
type Options struct {
	ASCII   bool
	Initial agents.AccountID // account to select; the first one when empty or unknown
	Tab     Tab
	// Now is the clock for "Updated .. ago" and reset countdowns; time.Now when nil.
	Now func() time.Time
	// Usage seeds the modal with what the root model already knows.
	Usage usagestore.Store
}

// CloseMsg asks the parent to close the modal.
type CloseMsg struct{}

// ExecMsg asks the parent to run Cmd in the real terminal (tea.ExecProcess)
// and then send ReloadMsg.
type ExecMsg struct{ Cmd *exec.Cmd }

// ReloadMsg tells the modal to re-list accounts after a terminal flow ended.
type ReloadMsg struct{}

// seqs holds the sequence of the latest request of each kind.
type seqs struct{ list, describe, defaults, save, remove int }

// Model is the agent profile view. It is a value type; backend calls run as
// commands and their results are ordered by sequence number before they are
// applied.
type Model struct {
	// Browser holds the account list, the selection and the details loaded for
	// it, shared with the new-agent dialog.
	Browser
	ascii bool
	now   func() time.Time
	w, h  int // screen size; the frame is derived by FrameSize
	tab   Tab
	focus accountFocus

	ov  overviewState
	df  defaultsState
	add addState
	dlg *dialog
}

type accountFocus int

const (
	focusProfiles accountFocus = iota
	focusContent
)

type newAccount struct {
	provider agents.ProviderID
	name     string
}

// New builds the modal. Init starts loading.
func New(api agentsapi.Accounts, opts Options) Model {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return Model{
		Browser: NewBrowser(api, opts.Initial, opts.Usage, false),
		ascii:   opts.ASCII,
		now:     now,
		tab:     min(max(opts.Tab, TabOverview), tabCount-1),
		add:     newAddState(),
	}
}

// SetSize gives the view the available pane size.
func (m *Model) SetSize(w, h int) { m.w, m.h = w, h }

// FrameSize uses the entire available Agents pane.
func FrameSize(screenW, screenH int) (w, h int) {
	return max(screenW, 0), max(screenH, 0)
}

// Capturing reports that keys must all reach the modal: a text input, a
// dropdown or a dialog is active.
func (m Model) Capturing() bool {
	return m.dlg != nil || (m.add.active && m.add.stage == stageName) || m.formEditing()
}

func (m Model) formEditing() bool {
	return m.tab == TabDefaults && m.df.loaded && m.df.form.Editing()
}

// View renders the accounts table over the selected account's inspector, with
// a dialog over it when one is open. The surrounding Agents pane draws the
// border, so the view only adds its title and rules.
func (m Model) View() string {
	w, h := FrameSize(m.w, m.h)
	if w == 0 || h == 0 {
		return ""
	}
	if m.dlg != nil {
		a, _ := m.Current()
		return m.dlg.view(theme.ProviderGlyph(a.Provider, m.ascii)+" "+providerName(a.Provider)+" · "+a.Name, w, h)
	}
	iw := ContentWidth(w)
	lines := m.compose(iw, h)
	for i := range lines {
		lines[i] = fit(" "+fit(lines[i], iw), w)
	}
	return strings.Join(lines, "\n")
}

// Help describes the keys available in the current profile view.
func (m Model) Help() string { return m.footer() }

func (m Model) footer() string {
	text := m.footerText()
	if m.ascii {
		text = strings.ReplaceAll(text, "↑↓", "j/k")
	}
	return text
}

// footerText is the single footer: the keys of the area that has focus.
func (m Model) footerText() string {
	switch {
	case m.dlg != nil:
		return "←→ Choice · Enter Confirm · Esc Cancel"
	case m.add.active:
		return m.addFooter()
	case m.formEditing():
		return "↑↓ Choice · Enter Select · Tab Section · Esc Cancel"
	}
	switch m.focus {
	case focusProfiles:
		return "↑↓ Account · Tab Section · Enter Details · n Add account · Esc Back"
	default:
		if m.tab == TabDefaults {
			return "↑↓ Field · Enter Edit · Ctrl+S Save · Tab Section · Esc Accounts"
		}
		if m.tab == TabUsage {
			return "r Refresh · Tab Section · Esc Accounts"
		}
		return "↑↓ Action · Enter Select · Tab Section · Esc Accounts"
	}
}

// shortenFooter drops the least useful words until the hints fit w, so a narrow
// pane never cuts a key mid-word.
func shortenFooter(s string, w int) string {
	for _, r := range [][2]string{{"n Add account", "n Add"}, {"Tab Section", "Tab"}, {"Enter Details", "Enter"}, {"Esc Accounts", "Esc"}} {
		if ansi.StringWidth(s) <= w {
			break
		}
		s = strings.Replace(s, r[0], r[1], 1)
	}
	return s
}

// ruleGlyph is the horizontal rule character.
func (m Model) ruleGlyph() string {
	if m.ascii {
		return "-"
	}
	return "─"
}

// compose lays out exactly h lines of iw cells: title, accounts table,
// divider, tabs, inspector and footer. Spare height goes to the inspector once
// the table shows every account; when height is short the table gives up rows
// before the inspector gives up content.
func (m Model) compose(iw, h int) []string {
	th := theme.Current
	dim := lipgloss.NewStyle().Foreground(th.Dim)
	rule := dim.Render(strings.Repeat(m.ruleGlyph(), iw))

	title := lipgloss.NewStyle().Bold(true).Foreground(th.Text).Render("Accounts")
	if m.listed && len(m.accounts) > 0 && !m.add.active {
		title = twoCol(title, dim.Render(plural(len(m.accounts), "account")), iw)
	}
	// Blank spacer lines are the first thing to go in a short pane.
	var gap []string
	if h >= 20 {
		gap = []string{""}
	}
	top := append([]string{title, rule}, gap...)
	bottom := []string{rule, dim.Render(shortenFooter(m.footer(), iw))}
	free := max(h-len(top)-len(bottom), 0)

	var body []string
	switch {
	case m.add.active:
		body = m.addView(iw)
	case !m.listed:
		body = []string{dim.Render("Loading accounts…")}
	default:
		if _, ok := m.Current(); ok {
			body = m.inspect(iw, free, gap)
			break
		}
		if m.listErr != "" {
			body = []string{lipgloss.NewStyle().Foreground(th.Danger).Render("Couldn't load accounts"), dim.Render(m.listErr)}
		} else {
			body = []string{dim.Render("No accounts yet.")}
		}
	}

	lines := append(top, padTo(body, free)...)
	lines = append(lines, bottom...)
	if len(lines) > h {
		lines = append(lines[:h-1], lines[len(lines)-1])
	}
	return lines
}

// padTo cuts or extends lines with blanks to exactly n.
func padTo(lines []string, n int) []string {
	out := append([]string{}, lines[:min(len(lines), n)]...)
	for len(out) < n {
		out = append(out, "")
	}
	return out
}

// twoCol puts right at the far edge of a w-cell line when both fit.
func twoCol(left, right string, w int) string {
	gap := w - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 2 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

// accountTable is the table of every account, columns PLAN and CONNECTION.
func (m Model) accountTable() Table {
	dim := lipgloss.NewStyle().Foreground(theme.Current.Dim)
	side := Sidebar{ASCII: m.ascii}
	none := "—"
	if m.ascii {
		none = "-"
	}
	return Table{
		Accounts: m.Ordered(),
		Selected: m.selected,
		ASCII:    m.ascii,
		Focused:  m.focus == focusProfiles,
		Columns: []Column{
			{Title: "PLAN", Width: 9, Optional: true, Cell: func(a agentsapi.Account) string {
				if plan := planOf(a, m.usage); plan != "" {
					return plan
				}
				return dim.Render(none)
			}},
			{Title: "CONNECTION", Width: 15, Under: true, Cell: func(a agentsapi.Account) string {
				return side.dot(a) + " " + connectionLabel(a)
			}},
		},
	}
}

// connectionLabel is the sign-in state next to the account's dot.
func connectionLabel(a agentsapi.Account) string {
	th := theme.Current
	switch authOf(a.Info) {
	case authConnected:
		return lipgloss.NewStyle().Foreground(th.Success).Render("Connected")
	case authNeedsSignIn:
		return lipgloss.NewStyle().Foreground(th.Warning).Render("Needs sign-in")
	}
	return lipgloss.NewStyle().Foreground(th.Dim).Render("Unknown")
}

// inspect builds the lines between the title and the footer for the selected
// account, filling rows lines.
func (m Model) inspect(iw, rows int, gap []string) []string {
	a, _ := m.Current()
	tbl := m.accountTable()
	useTable := tbl.Fits(iw)

	var mid []string
	if useTable {
		mid = append(append(append([]string{}, gap...), m.divider(a, iw)), gap...)
	} else {
		// Too narrow for the table: the single-account selector above the same
		// tabs.
		side := Sidebar{Accounts: m.accounts, Selected: m.selected, Usage: m.usage, ShowPlan: true, ASCII: m.ascii}
		mid = []string{side.Selector(m.focus == focusProfiles)}
	}
	tabs := append(m.tabRows(iw), gap...)
	insp := m.inspector(a, iw, !useTable)

	avail := rows - len(mid) - len(tabs)
	var out []string
	if useTable {
		// Table rows yield before inspector content does, but never below the
		// selected row.
		tableH := min(max(avail-insp.height(), 1), tbl.Height(iw))
		out = tbl.View(iw, tableH)
		avail -= len(out)
	}
	out = append(out, mid...)
	out = append(out, tabs...)
	return append(out, insp.render(avail)...)
}

// divider is the single rule between the table and the tabs, naming the
// account the inspector shows.
func (m Model) divider(a agentsapi.Account, w int) string {
	th := theme.Current
	dim := lipgloss.NewStyle().Foreground(th.Dim)
	rule := m.ruleGlyph()
	name := ansi.Truncate(a.Name, max(w-ansi.StringWidth(providerName(a.Provider))-11, 1), "…")
	label := " " + providerMark(a.Provider, m.ascii) + dim.Render(" / ") +
		lipgloss.NewStyle().Foreground(th.Text).Render(name) + " "
	left := dim.Render(strings.Repeat(rule, 2))
	return left + label + dim.Render(strings.Repeat(rule, max(w-2-ansi.StringWidth(label), 0)))
}

// tabRows is the tab row and the underline marking the active tab. Save state
// sits at the right edge of the tab row so the table never shifts.
func (m Model) tabRows(w int) []string {
	th := theme.Current
	names := [tabCount]string{"Overview", "Usage", "Defaults"}
	sep := 5
	if w < 36 {
		sep = 2
	}
	var row, under string
	col := 0
	underline := "━"
	if m.ascii {
		underline = "="
	}
	for t := range tabCount {
		if t > 0 {
			row += strings.Repeat(" ", sep)
			col += sep
		}
		if t == m.tab {
			row += lipgloss.NewStyle().Bold(true).Foreground(th.Accent).Render(names[t])
			under = strings.Repeat(" ", col) + lipgloss.NewStyle().Foreground(th.Accent).Render(strings.Repeat(underline, len(names[t])))
		} else {
			row += lipgloss.NewStyle().Foreground(th.Dim).Render(names[t])
		}
		col += len(names[t])
	}
	switch {
	case m.df.saved:
		row = twoCol(row, lipgloss.NewStyle().Foreground(th.Success).Render("Saved"), w)
	case m.dirty():
		dot := "●"
		if m.ascii {
			dot = "*"
		}
		row = twoCol(row, lipgloss.NewStyle().Foreground(th.Warning).Render(dot+" unsaved"), w)
	}
	return []string{row, under}
}

// resetAccount drops the view state of the previous account when the
// selection moves.
func (m *Model) resetAccount() {
	m.ov = overviewState{}
	m.df = defaultsState{}
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}
