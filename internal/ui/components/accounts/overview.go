package accounts

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

const (
	btnLogin = iota
	btnRemove
)

type overviewState struct {
	focus    int
	msg      string // inline result of the last action, shown above the buttons
	removing bool
}

func (m Model) overviewKey(k tea.KeyMsg) (Model, tea.Cmd) {
	switch k.String() {
	case "up", "left", "h":
		m.ov.focus = btnLogin
		m.ov.msg = ""
	case "down", "right", "l":
		m.ov.focus = btnRemove
		m.ov.msg = ""
	case "enter":
		return m.activateOverview()
	}
	return m, nil
}

func (m Model) activateOverview() (Model, tea.Cmd) {
	a, ok := m.current()
	if !ok {
		return m, nil
	}
	m.ov.msg = ""
	if m.ov.focus == btnLogin {
		return m, emit(ExecMsg{Cmd: m.api.LoginCommand(a.ID)})
	}
	if a.Running > 0 {
		m.ov.msg = fmt.Sprintf("Stop %s first.", plural(a.Running, "running session"))
		return m, nil
	}
	if m.ov.removing {
		return m, nil
	}
	glyph := theme.ProviderStyle(a.Provider).Render(theme.ProviderGlyph(a.Provider, m.ascii))
	m.dlg = &dialog{
		title:   "Remove account",
		text:    fmt.Sprintf("Remove %s %s? Its Bonsai config dir is deleted.", glyph, a.Name),
		cancel:  "Cancel",
		confirm: "Remove",
		then: func(m Model) (Model, tea.Cmd) {
			m.ov.removing = true
			return m, removeCmd(m.api, a.ID, m.next(&m.latest.remove), m.neighbor(a.ID))
		},
	}
	return m, nil
}

// neighbor is the account to select once id is gone: the next one in display
// order, else the previous one, else none.
func (m Model) neighbor(id agents.AccountID) agents.AccountID {
	ids := m.orderedIDs()
	for i, other := range ids {
		if other != id {
			continue
		}
		switch {
		case i+1 < len(ids):
			return ids[i+1]
		case i > 0:
			return ids[i-1]
		}
	}
	return ""
}

func (m Model) overviewView(rw, rows int) []string {
	th := theme.Current
	dim := lipgloss.NewStyle().Foreground(th.Dim)
	a, _ := m.current()

	loading := dim.Render("Loading…")
	info := func(value string) string {
		switch {
		case m.describing:
			return loading
		case m.describeErr != "":
			return dim.Render("Unavailable")
		case value == "":
			return dim.Render("-")
		}
		return value
	}

	status := dim.Render("Unknown")
	switch authOf(a.Info) {
	case authConnected:
		status = lipgloss.NewStyle().Foreground(th.Success).Render("Connected")
	case authNeedsSignIn:
		status = lipgloss.NewStyle().Foreground(th.Warning).Render("Needs sign-in")
	}
	if m.describing {
		status = loading
	}
	identity := info(a.Info.Identity)
	if m.describeErr != "" && !m.describing {
		identity = dim.Render("Unavailable · " + m.describeErr)
	}

	const first, second = 13, 17
	lines := []string{
		kv("Status", status, first),
		kv("Identity", identity, first),
		kv("Provider", theme.ProviderStyle(a.Provider).Render(providerLabel(a.Provider)), first),
		kv("Plan", info(a.Info.Plan), first),
		kv("Auth", info(authLabel(a.Info.AuthMode)), first),
		"",
		kv("Default model", m.defaultValue("model"), second),
		kv("Default effort", m.defaultValue("effort"), second),
		"",
		kv("Sessions", fmt.Sprintf("%d running", a.Running), second),
	}

	slot := ""
	if m.ov.msg != "" {
		slot = lipgloss.NewStyle().Foreground(th.Warning).Render(m.ov.msg)
	}
	buttons := button("Sign in again", m.ov.focus == btnLogin, false) + "  " +
		button("Remove account", m.ov.focus == btnRemove, a.Running > 0)
	lines = append(lines, slot, buttons)
	return squeeze(lines, rows)
}

// defaultValue is the label of the account's default for a profile field.
func (m Model) defaultValue(id string) string {
	dim := lipgloss.NewStyle().Foreground(theme.Current.Dim)
	switch {
	case m.df.err != "":
		return dim.Render("Unavailable")
	case !m.df.loaded:
		return dim.Render("Loading…")
	}
	v := m.df.data.Values[id]
	if v == "" {
		return agents.ProviderDefaultLabel
	}
	if id == "model" {
		for _, mo := range m.df.data.Models {
			if mo.ID == v && mo.Label != "" {
				return mo.Label
			}
		}
		return v
	}
	for _, f := range m.df.data.Fields {
		if f.ID != id {
			continue
		}
		for _, o := range f.Options {
			if o.Value == v && o.Label != "" {
				return o.Label
			}
		}
	}
	return v
}

// kv is "label  value" with the value starting at column w.
func kv(label, value string, w int) string {
	dim := lipgloss.NewStyle().Foreground(theme.Current.Dim)
	pad := max(w-lipgloss.Width(label), 1)
	return dim.Render(label) + strings.Repeat(" ", pad) + value
}

// squeeze drops blank lines, last first, until lines fit rows so the gaps
// between sections survive longest; whatever still does not fit is cut from
// the bottom.
func squeeze(lines []string, rows int) []string {
	out := make([]string, 0, len(lines))
	over := len(lines) - rows
	for i := len(lines) - 1; i >= 0; i-- {
		if over > 0 && lines[i] == "" {
			over--
			continue
		}
		out = append(out, lines[i])
	}
	slices.Reverse(out)
	return out[:min(len(out), max(rows, 0))]
}

// providerName is the short provider name of the column header.
func providerName(p agents.ProviderID) string {
	s := string(p)
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// providerLabel is the provider's product name. The TUI cannot import the
// provider packages that define it, so the known ones are spelled out.
func providerLabel(p agents.ProviderID) string {
	if p == "claude" {
		return "Claude Code"
	}
	return providerName(p)
}

// authLabel names a provider's sign-in method for the Auth row. Unknown modes
// show as reported rather than hiding a method a provider added.
func authLabel(mode string) string {
	switch mode {
	case "login":
		return "Browser login"
	case "token":
		return "Access token"
	case "chatgpt":
		return "ChatGPT login"
	case "device":
		return "Device login"
	case "api-key":
		return "API key"
	}
	return mode
}
