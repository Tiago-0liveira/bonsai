package accounts

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// Tab is a section of the right column.
type Tab int

const (
	TabOverview Tab = iota
	TabUsage
	TabDefaults
	tabCount
)

const (
	// maxW and maxH cap the modal on big terminals.
	maxW, maxH = 100, 32
	// margin is the empty border kept around the modal on every side.
	margin = 2
)

// Options configures a new modal.
type Options struct {
	ASCII   bool
	Initial agents.AccountID // account to select; the first one when empty or unknown
	Tab     Tab
	// Now is the clock for "Updated .. ago" and reset countdowns; time.Now when nil.
	Now func() time.Time
}

// CloseMsg asks the parent to close the modal.
type CloseMsg struct{}

// ExecMsg asks the parent to run Cmd in the real terminal (tea.ExecProcess)
// and then send ReloadMsg.
type ExecMsg struct{ Cmd *exec.Cmd }

// ReloadMsg tells the modal to re-list accounts after a terminal flow ended.
type ReloadMsg struct{}

// seqs holds the sequence of the latest request of each kind.
type seqs struct{ list, describe, usage, defaults, save, remove int }

// Model is the agent accounts modal. It is a value type; backend calls run as
// commands and their results are ordered by sequence number before they are
// applied.
type Model struct {
	api   agentsapi.Accounts
	ascii bool
	now   func() time.Time
	w, h  int // screen size; the frame is derived by FrameSize
	tab   Tab

	accounts []agentsapi.Account
	listed   bool
	listErr  string
	selected agents.AccountID

	n      int // request counter
	latest seqs

	usage        map[agents.AccountID]agentsapi.Usage
	usageLoading bool
	usageErr     string // set only while there is no earlier data to keep showing

	describing  bool
	describeErr string

	ov  overviewState
	df  defaultsState
	add addState
	dlg *dialog

	// pendingAdd remembers the account the terminal flow is creating so the
	// next list selects it.
	pendingAdd *newAccount
}

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
	m := Model{
		api:      api,
		ascii:    opts.ASCII,
		now:      now,
		tab:      min(max(opts.Tab, TabOverview), tabCount-1),
		selected: opts.Initial,
		usage:    map[agents.AccountID]agentsapi.Usage{},
		add:      newAddState(),
	}
	m.n = 1
	m.latest.list = 1
	return m
}

// Init lists accounts; the selected account's details load once the list is in.
func (m Model) Init() tea.Cmd { return listCmd(m.api, m.latest.list, m.selected) }

// SetSize gives the modal the screen size; it sizes itself inside it.
func (m *Model) SetSize(w, h int) { m.w, m.h = w, h }

// FrameSize is the modal size for a screen: a margin on every side, capped.
// The new-agent dialog uses it too so both modals line up.
func FrameSize(screenW, screenH int) (w, h int) {
	return max(min(screenW-2*margin, maxW), 0), max(min(screenH-2*margin, maxH), 0)
}

// Capturing reports that keys must all reach the modal: a text input, a
// dropdown or a dialog is active.
func (m Model) Capturing() bool {
	return m.dlg != nil || (m.add.active && m.add.stage == stageName) || m.formEditing()
}

func (m Model) formEditing() bool {
	return m.tab == TabDefaults && m.df.loaded && m.df.form.Editing()
}

// View renders the modal frame, with a dialog over it when one is open.
func (m Model) View() string {
	w, h := FrameSize(m.w, m.h)
	if w == 0 || h == 0 {
		return ""
	}
	leftW := LeftWidth(w)
	rightW := max(w-leftW-7, 0)
	rows := BodyRows(h)

	side := Sidebar{
		Accounts:    m.accounts,
		Selected:    m.selected,
		AddSelected: m.add.active,
		Usage:       m.usage,
		ASCII:       m.ascii,
	}
	out := Frame("Agent accounts", side.View(leftW, rows), m.right(rightW, rows), m.footer(), w, h)
	if m.dlg != nil {
		out = overlay(out, m.dlg.view(w), w)
	}
	return out
}

func (m Model) footer() string {
	switch {
	case m.add.active:
		return m.addFooter()
	}
	switch m.tab {
	case TabUsage:
		return "j/k Account  ·  Tab Section  ·  r Refresh  ·  Esc Close"
	case TabDefaults:
		return "Tab Section  ·  Enter Edit  ·  Esc Back"
	}
	return "j/k Navigate  ·  Tab Section  ·  Enter Select  ·  Esc Close"
}

// right builds the right column's rows.
func (m Model) right(rw, rows int) []string {
	th := theme.Current
	dim := lipgloss.NewStyle().Foreground(th.Dim)
	switch {
	case m.add.active:
		return m.addView(rw)
	case !m.listed:
		return []string{dim.Render("Loading accounts…")}
	}
	a, ok := m.current()
	if !ok {
		if m.listErr != "" {
			return []string{lipgloss.NewStyle().Foreground(th.Danger).Render("Couldn't load accounts"), dim.Render(m.listErr)}
		}
		return []string{dim.Render("No accounts yet.")}
	}

	head := theme.ProviderStyle(a.Provider).Render(theme.ProviderGlyph(a.Provider, m.ascii)) +
		" " + providerName(a.Provider) + " · " + a.Name
	lines := []string{head, "", m.tabRow(), dim.Render(strings.Repeat("─", rw)), ""}
	body := max(rows-len(lines), 0)

	switch m.tab {
	case TabUsage:
		lines = append(lines, m.usageView(rw, body)...)
	case TabDefaults:
		lines = append(lines, m.defaultsView(rw, body)...)
	default:
		lines = append(lines, m.overviewView(rw, body)...)
	}
	return lines
}

func (m Model) tabRow() string {
	th := theme.Current
	names := [tabCount]string{"Overview", "Usage", "Defaults"}
	parts := make([]string, 0, tabCount)
	for t := range tabCount {
		if t == m.tab {
			parts = append(parts, lipgloss.NewStyle().Bold(true).Foreground(th.Accent).Render("["+names[t]+"]"))
			continue
		}
		parts = append(parts, lipgloss.NewStyle().Foreground(th.Dim).Render(names[t]))
	}
	row := strings.Join(parts, "  ")
	switch {
	case m.df.saved:
		row += "  " + lipgloss.NewStyle().Foreground(th.Success).Render("Saved")
	case m.dirty():
		dot := "●"
		if m.ascii {
			dot = "*"
		}
		row += "  " + lipgloss.NewStyle().Foreground(th.Warning).Render(dot+" unsaved")
	}
	return row
}

// current is the selected account, or false when none is.
func (m Model) current() (agentsapi.Account, bool) {
	for _, a := range m.accounts {
		if a.ID == m.selected {
			return a, true
		}
	}
	return agentsapi.Account{}, false
}

func (m Model) orderedIDs() []agents.AccountID {
	ordered := Sidebar{Accounts: m.accounts}.Ordered()
	ids := make([]agents.AccountID, len(ordered))
	for i, a := range ordered {
		ids[i] = a.ID
	}
	return ids
}

// next advances the request counter and records it as the latest of a kind.
func (m *Model) next(kind *int) int {
	m.n++
	*kind = m.n
	return m.n
}

// loadSelected requests everything the right column shows for the selected account.
func (m *Model) loadSelected() tea.Cmd {
	id := m.selected
	if id == "" {
		return nil
	}
	m.describing, m.describeErr = true, ""
	m.usageLoading = true
	return tea.Batch(
		describeCmd(m.api, id, m.next(&m.latest.describe)),
		usageCmd(m.api, id, m.next(&m.latest.usage), false),
		defaultsCmd(m.api, id, m.next(&m.latest.defaults)),
	)
}

// resetAccount drops the per-account view state when the selection moves, so
// nothing of the previous account shows under the new one.
func (m *Model) resetAccount() {
	m.describing, m.describeErr = false, ""
	m.usageLoading, m.usageErr = false, ""
	m.ov = overviewState{}
	m.df = defaultsState{}
}

func (m *Model) relist(want agents.AccountID) tea.Cmd {
	return listCmd(m.api, m.next(&m.latest.list), want)
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}
