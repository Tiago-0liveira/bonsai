package accounts

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

type addStage int

const (
	stageProvider addStage = iota
	stageName
	stageContinue
)

const maxNameLen = 40

// addState is the "+ Add account" form that replaces the right column.
type addState struct {
	active   bool
	stage    addStage
	provider int // index into groupOrder
	input    textinput.Model
	err      string
}

func newAddState() addState {
	ti := textinput.New()
	ti.Placeholder = "account name"
	ti.CharLimit = maxNameLen
	// A steady cursor needs no blink ticks to be fed back through Update.
	ti.Cursor.SetMode(cursor.CursorStatic)
	return addState{input: ti}
}

func (m Model) openAdd() Model {
	m.add = newAddState()
	m.add.active = true
	return m
}

func (m Model) addFooter() string {
	switch m.add.stage {
	case stageName:
		return "Type a name  ·  Enter Continue  ·  Esc Back"
	case stageContinue:
		return "Enter Continue in terminal  ·  Esc Back"
	}
	return "j/k Provider  ·  Enter Continue  ·  Esc Back"
}

func (m Model) addKey(k tea.KeyMsg) (Model, tea.Cmd) {
	a := m.add
	key := k.String()
	switch a.stage {
	case stageProvider:
		switch key {
		case "esc":
			if m.selected == "" {
				return m, emit(CloseMsg{})
			}
			a.active = false
		case "j", "down":
			a.provider = min(a.provider+1, len(groupOrder)-1)
		case "k", "up":
			a.provider = max(a.provider-1, 0)
		case "enter", "tab":
			a.stage = stageName
			m.add = a
			return m.focusName()
		}
	case stageName:
		switch key {
		case "esc", "up", "shift+tab":
			a.stage = stageProvider
			a.input.Blur()
		case "enter", "tab", "down":
			if a.err = m.nameError(); a.err == "" {
				a.stage = stageContinue
				a.input.Blur()
			}
		default:
			var cmd tea.Cmd
			a.input, cmd = a.input.Update(k)
			a.err = ""
			m.add = a
			return m, cmd
		}
	case stageContinue:
		switch key {
		case "esc", "up", "shift+tab", "k":
			a.stage = stageName
			m.add = a
			return m.focusName()
		case "enter":
			return m.startAdd()
		}
	}
	m.add = a
	return m, nil
}

func (m Model) focusName() (Model, tea.Cmd) {
	cmd := m.add.input.Focus()
	return m, cmd
}

// nameError validates the typed name; "" means it is usable.
func (m Model) nameError() string {
	name := strings.TrimSpace(m.add.input.Value())
	p := groupOrder[m.add.provider]
	switch {
	case name == "":
		return "Name is required"
	case strings.Contains(name, "/"):
		return "Name cannot contain /"
	}
	for _, a := range m.accounts {
		if a.Provider == p && strings.EqualFold(a.Name, name) {
			return fmt.Sprintf("%s already has an account named %s", providerName(p), name)
		}
	}
	return ""
}

// startAdd hands the sign-in flow to the terminal; the list reloads when it ends.
func (m Model) startAdd() (Model, tea.Cmd) {
	p := groupOrder[m.add.provider]
	name := strings.TrimSpace(m.add.input.Value())
	m.pendingAdd = &newAccount{provider: p, name: name}
	m.add.active = false
	return m, emit(ExecMsg{Cmd: m.api.AddCommand(p, name)})
}

func (m Model) addView(rw int) []string {
	th := theme.Current
	dim := lipgloss.NewStyle().Foreground(th.Dim)
	accent := lipgloss.NewStyle().Bold(true).Foreground(th.Accent)
	a := m.add

	lines := []string{lipgloss.NewStyle().Bold(true).Render("ADD ACCOUNT"), "", dim.Render("Provider")}
	for i, p := range groupOrder {
		marker, label := "  ", providerLabel(p)
		if i == a.provider {
			marker = "> "
			if a.stage == stageProvider {
				marker, label = accent.Render("> "), accent.Render(label)
			}
		}
		lines = append(lines, marker+theme.ProviderStyle(p).Render(theme.ProviderGlyph(p, m.ascii))+" "+label)
	}

	in := a.input
	in.Width = max(rw-4, 1)
	lines = append(lines, "", dim.Render("Name"), in.View())
	if a.err != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(th.Danger).Render(a.err))
	} else {
		lines = append(lines, "")
	}
	return append(lines, "", button("Continue in terminal", a.stage == stageContinue, false))
}
