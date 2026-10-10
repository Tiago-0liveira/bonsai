package websetup

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	setup "github.com/Tiago-0liveira/bonsai/internal/websetup"
	"github.com/Tiago-0liveira/bonsai/internal/websetup/checks"
)

func itoa(n int) string { return strconv.Itoa(n) }

func (m Model) move(s screen, delta, rows int) int {
	c := m.cursor[s] + delta
	if c < 0 {
		c = 0
	}
	if c > rows-1 {
		c = rows - 1
	}
	return c
}

// marker is the focus arrow: focus never depends on color alone.
func (m Model) marker(focused bool) string {
	if focused {
		return " " + m.st.focus.Render("›") + " "
	}
	return "   "
}

// ── 1. Welcome ──────────────────────────────────────────────────────────────

func (m Model) welcomeView() string {
	body := "\n" + indent(m.st.text.Render(setup.WelcomeLead), 2) + "\n\n" + indent(m.st.dim.Render(setup.WelcomeTime), 2)
	return m.frame("bonsai web · setup", m.counter(), body, []hint{{"enter", "start"}, {"d", "use defaults and go"}, {"q", "quit"}})
}

func (m Model) onWelcome(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "enter":
		return m.next()
	case "d":
		return m.push(screenReview), nil
	case "q", "esc":
		return m.requestQuit()
	}
	return m, nil
}

// ── 2. Projects ─────────────────────────────────────────────────────────────

func (m Model) projectsView() string {
	var b strings.Builder
	b.WriteString("\n")
	folders := m.draft.Folders
	width := 12
	for _, f := range folders {
		if n := lipgloss.Width(setup.TildePath(m.info.Home, f.Path)); n > width {
			width = n
		}
	}
	if width > 40 {
		width = 40
	}
	cursor := m.cursor[screenProjects]
	for i, f := range folders {
		box := "[ ]"
		if f.Checked {
			box = "[x]"
		}
		path := setup.TildePath(m.info.Home, f.Path)
		row := m.marker(i == cursor && !m.adding) + m.st.text.Render(box) + " " + padRight(m.st.text.Render(path), width) + "  " + m.folderStatus(f)
		b.WriteString(row + "\n")
	}
	addRow := m.marker(cursor == len(folders) && !m.adding) + m.st.dim.Render("[+]") + " " + m.st.text.Render("Add another folder…")
	b.WriteString(addRow + "\n")
	if m.adding {
		b.WriteString("\n  " + m.folderIn.View() + "\n")
		if m.inputErr != "" {
			b.WriteString("  " + m.st.fail.Render("✗") + " " + m.st.text.Render(m.inputErr) + "\n")
			b.WriteString("    " + m.st.dim.Render("fix  type a folder that exists, for example ~/code") + "\n")
		}
	}
	w, _ := m.size()
	b.WriteString("\n" + indent(m.st.dim.Render(m.wrap("Bonsai looks up to "+itoa(m.info.DiscoveryDepth)+" folders deep. Nothing is moved or changed.", w-4)), 2))
	b.WriteString("\n" + indent(m.st.dim.Render(m.wrap(setup.ProjectsPickTip, w-4)), 2))
	hs := []hint{{"space", "toggle"}, {"a", "add folder"}, {"enter", "next"}, {"esc", "back"}}
	if m.mode == Edit {
		hs[2] = hint{"enter", "done"}
	}
	if m.adding {
		hs = []hint{{"enter", "add"}, {"esc", "cancel"}}
	}
	return m.frame("Where are your repositories?", m.counter(), b.String(), hs)
}

func (m Model) folderStatus(f setup.Folder) string {
	var parts []string
	if f.ThisRepo {
		parts = append(parts, "this repo")
	}
	switch {
	case f.Err != "":
		return m.st.warn.Render("!") + " " + m.st.dim.Render(f.Err)
	case !f.Scanned:
		parts = append(parts, "scanning…")
	case len(f.Repos) == 0:
		parts = append(parts, "no repos found")
	case len(f.Repos) == 1 && f.ThisRepo:
	case len(f.Repos) == 1:
		parts = append(parts, "1 repo found")
	default:
		parts = append(parts, itoa(len(f.Repos))+" repos found")
	}
	return m.st.dim.Render(strings.Join(parts, " · "))
}

func (m Model) onProjects(key string) (tea.Model, tea.Cmd) {
	rows := len(m.draft.Folders) + 1
	cursor := m.cursor[screenProjects]
	onAdd := cursor == len(m.draft.Folders)
	switch key {
	case "up", "k":
		m.cursor[screenProjects] = m.move(screenProjects, -1, rows)
	case "down", "j":
		m.cursor[screenProjects] = m.move(screenProjects, 1, rows)
	case " ", "x":
		if onAdd {
			return m.openFolderInput()
		}
		m.draft = m.draft.Clone()
		m.draft.Toggle(cursor)
	case "a":
		return m.openFolderInput()
	case "enter":
		if onAdd {
			return m.openFolderInput()
		}
		return m.next()
	case "esc":
		return m.pop(), nil
	case "q":
		return m.requestQuit()
	}
	return m, nil
}

func (m Model) openFolderInput() (tea.Model, tea.Cmd) {
	m.adding, m.inputErr = true, ""
	m.folderIn.Reset()
	return m, m.folderIn.Focus()
}

func (m Model) onFolderInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.adding, m.inputErr = false, ""
		m.folderIn.Blur()
		return m, nil
	case "enter":
		value := strings.TrimSpace(m.folderIn.Value())
		if value == "" {
			m.adding, m.inputErr = false, ""
			m.folderIn.Blur()
			return m, nil
		}
		m.inputErr = ""
		return m, m.addFolder(value)
	}
	var cmd tea.Cmd
	m.folderIn, cmd = m.folderIn.Update(msg)
	return m, cmd
}

// ── 3. Where to open ────────────────────────────────────────────────────────

func (m Model) openChoices() []setup.Choice {
	return setup.OpenChoices("http://127.0.0.1:"+itoa(m.draft.Config.APIPort), strings.TrimSuffix(m.info.HostedURL, "/app"))
}

func (m Model) openIndex() int {
	if m.draft.Config.Interfaces.Hosted {
		return 1
	}
	return 0
}

func (m Model) openInView() string {
	choices := m.openChoices()
	selected := m.openIndex()
	cursor := m.cursor[screenOpenIn]
	var left strings.Builder
	for i, c := range choices {
		radio := "○"
		if i == selected {
			radio = "●"
		}
		left.WriteString(m.marker(cursor == i) + m.st.text.Render(radio+" "+c.Label) + "\n")
	}
	box := "[ ]"
	if m.draft.Config.OpenBrowser {
		box = "[x]"
	}
	left.WriteString("\n" + m.marker(cursor == 2) + m.st.text.Render(box+" Open the browser when") + "\n")
	left.WriteString("      " + m.st.text.Render("bonsai web starts"))
	body := "\n" + m.panes(left.String(), func(w int) string { return m.choicePanel(choices[selected], "", w) })
	hs := []hint{{"↑↓", "choose"}, {"space", "toggle"}, {"enter", "next"}, {"esc", "back"}}
	if m.mode == Edit {
		hs[2] = hint{"enter", "done"}
	}
	return m.frame("Where do you want to open Bonsai?", m.counter(), body, hs)
}

// choicePanel is the explanation of one option, wrapped to width. need, when
// set, replaces the first "You need" value (it carries the detected tool
// state).
func (m Model) choicePanel(c setup.Choice, need string, width int) string {
	var b strings.Builder
	b.WriteString(m.st.title.Render(c.Title) + "\n")
	b.WriteString(m.st.text.Render(strings.TrimRight(m.wrap(c.Lead, width), " ")) + "\n")
	if len(c.Pros) > 0 {
		b.WriteString("\n")
		for _, p := range c.Pros {
			b.WriteString(m.st.ok.Render("✓") + " " + m.st.text.Render(p) + "\n")
		}
	}
	if len(c.Facts) > 0 {
		b.WriteString("\n")
		for i, f := range c.Facts {
			value := f[1]
			if i == 0 && need != "" {
				value = need
			}
			b.WriteString(m.hang(m.st.dim.Render(f[0]), 11, value, width) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) onOpenIn(key string) (tea.Model, tea.Cmd) {
	cursor := m.cursor[screenOpenIn]
	pick := func(i int) {
		switch i {
		case 0:
			m.draft.Config.Interfaces = config.WebInterfaces{Local: true}
		case 1:
			m.draft.Config.Interfaces = config.WebInterfaces{Local: true, Hosted: true}
		}
	}
	switch key {
	case "up", "k":
		m.cursor[screenOpenIn] = m.move(screenOpenIn, -1, 3)
		pick(m.cursor[screenOpenIn])
	case "down", "j":
		m.cursor[screenOpenIn] = m.move(screenOpenIn, 1, 3)
		pick(m.cursor[screenOpenIn])
	case " ", "x":
		if cursor == 2 {
			m.draft.Config.OpenBrowser = !m.draft.Config.OpenBrowser
		} else {
			pick(cursor)
		}
	case "enter":
		return m.next()
	case "esc":
		return m.pop(), nil
	case "q":
		return m.requestQuit()
	}
	return m, nil
}

// ── 5. Updates ──────────────────────────────────────────────────────────────

func (m Model) updateChoices() []setup.Choice {
	return setup.UpdateChoices(m.info.StandardInterval)
}

// toolState is the detected state of the tunnel tool an option needs, for
// its "You need" line ("cloudflared  ✓ installed (2026.9.3)").
func (m Model) toolState(choiceID string) string {
	ids := map[string]string{
		setup.UpdatesCloudflaredQuick: checks.IDTunnelCloudflared,
		setup.UpdatesCloudflaredNamed: checks.IDTunnelCloudflared,
		setup.UpdatesNgrok:            checks.IDTunnelNgrok,
		setup.UpdatesTailscale:        checks.IDTunnelTailscale,
	}
	id, ok := ids[choiceID]
	if !ok {
		return ""
	}
	c, found := checks.Find(m.checks, id)
	if !found {
		return ""
	}
	state := strings.SplitN(c.Detail, " · ", 2)[0]
	glyph := m.st.ok.Render("✓")
	if strings.HasPrefix(state, "not installed") {
		glyph = m.st.fail.Render("✗")
	}
	need := ""
	for _, choice := range m.updateChoices() {
		if choice.ID == choiceID {
			need = choice.Facts[0][1]
		}
	}
	return need + "   " + glyph + " " + state
}

func (m Model) updatesView() string {
	if m.compare {
		return m.compareView()
	}
	choices := m.updateChoices()
	cursor := m.cursor[screenUpdates]
	var left strings.Builder
	for i, c := range choices {
		radio := "○"
		if c.ID == m.draft.Config.Updates.Mode || (c.ID == setup.UpdatesStandard && m.draft.Config.Updates.Mode != config.WebUpdatesLive) {
			radio = "●"
		}
		label := m.st.text.Render(radio + " " + c.Label)
		if !c.Available {
			label = m.st.dim.Render(radio + " " + c.Label)
		}
		left.WriteString(m.marker(cursor == i) + label + "\n")
	}
	left.WriteString("   " + m.st.dim.Render("Live: "+setup.LaterVersion))
	c := choices[cursor]
	body := ""
	if w, _ := m.size(); w >= twoPaneMinWidth {
		body = "\n" // the stacked layout needs every line at 24 rows
	}
	body += m.panes(left.String(), func(w int) string { return m.choicePanel(c, m.toolState(c.ID), w) })
	hs := []hint{{"↑↓", "choose"}, {"?", "compare all"}, {"enter", "next"}, {"esc", "back"}}
	if m.mode == Edit {
		hs[2] = hint{"enter", "done"}
	}
	return m.frame("How should GitHub changes reach you?", m.counter(), body, hs)
}

func (m Model) compareView() string {
	rows := setup.CompareMatrix(m.info.StandardInterval)
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for i, cell := range row {
			if n := lipgloss.Width(cell); n > widths[i] {
				widths[i] = n
			}
		}
	}
	var b strings.Builder
	b.WriteString("\n")
	for r, row := range rows {
		var line strings.Builder
		for i, cell := range row {
			style := m.st.text
			if r == 0 {
				style = m.st.dim
			}
			line.WriteString(padRight(style.Render(cell), widths[i]+2))
		}
		b.WriteString("  " + strings.TrimRight(line.String(), " ") + "\n")
	}
	b.WriteString("\n  " + m.st.dim.Render("Live options: "+setup.LaterVersion+"."))
	return m.frame("Compare update options", m.counter(), b.String(), []hint{{"esc", "close"}})
}

func (m Model) onUpdates(key string) (tea.Model, tea.Cmd) {
	choices := m.updateChoices()
	switch key {
	case "up", "k":
		m.cursor[screenUpdates] = m.move(screenUpdates, -1, len(choices))
	case "down", "j":
		m.cursor[screenUpdates] = m.move(screenUpdates, 1, len(choices))
	case "?":
		m.compare = true
	case "enter", " ":
		c := choices[m.cursor[screenUpdates]]
		if !c.Available {
			m.flash = c.Label + " is " + setup.LaterVersion + ". Standard stays on for now."
			return m, nil
		}
		m.draft.Config.Updates.Mode = config.WebUpdatesStandard
		if key == "enter" {
			return m.next()
		}
	case "esc":
		return m.pop(), nil
	case "q":
		return m.requestQuit()
	}
	return m, nil
}
