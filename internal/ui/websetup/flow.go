package websetup

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	setup "github.com/Tiago-0liveira/bonsai/internal/websetup"
	"github.com/Tiago-0liveira/bonsai/internal/websetup/checks"
)

func (m Model) row(label, value string) string {
	return "  " + padRight(m.st.dim.Render(label), 13) + m.st.text.Render(value)
}

func (m Model) githubSummary() string {
	if m.checksLoading && len(m.checks) == 0 {
		return "checking…"
	}
	installed, ok := checks.Find(m.checks, checks.IDGHInstalled)
	if !ok {
		return "checking…"
	}
	if installed.State != checks.OK {
		return "gh not installed · local features only"
	}
	auth, _ := checks.Find(m.checks, checks.IDGHAuth)
	if auth.State != checks.OK {
		return "gh not logged in · local features only"
	}
	host, login, found := strings.Cut(auth.Detail, " · ")
	if !found {
		return "via gh (" + host + ")"
	}
	return "via gh (" + login + ")"
}

// ── 6. Review ───────────────────────────────────────────────────────────────

func (m Model) reviewView() string {
	w, _ := m.size()
	plan := m.plan()
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(m.row("Projects", setup.ProjectsSummary(m.draft, m.info.Home)) + "\n")
	b.WriteString(m.row("Open in", setup.OpenInSummary(m.draft.Config)) + "\n")
	b.WriteString(m.row("GitHub", m.githubSummary()) + "\n")
	b.WriteString(m.row("Updates", setup.UpdatesSummary(m.draft.Config, m.info.StandardInterval)) + "\n")

	if m.mode == Edit {
		changes := setup.Diff(m.initial, m.draft, m.info.Home)
		b.WriteString("\n  " + m.st.title.Render("Changes") + "\n")
		if len(changes) == 0 {
			b.WriteString("  " + m.st.dim.Render("No changes yet.") + "\n")
		}
		for _, c := range changes {
			value := c.To
			if c.From != "" {
				value = c.From + " → " + c.To
			}
			b.WriteString(m.row(c.Label, value) + "\n")
		}
	}

	b.WriteString("\n  " + m.st.text.Render(setup.NothingExternal) + "\n")
	var effect []string
	switch {
	case m.mode == Wizard:
		effect = append(effect, "Bonsai saves your settings to "+setup.TildePath(m.info.Home, m.info.ConfigPath)+" and starts in the background.")
		if m.draft.Config.OpenBrowser {
			effect = append(effect, "Your browser opens when it is ready.")
		}
	case m.info.Running == nil:
		effect = append(effect, "bonsai web is not running; your changes apply the next time it starts.")
	default:
		if plan.RestartAPI {
			effect = append(effect, "Restarts the Bonsai API ("+plan.RestartReason+").")
			if m.draft.Config.APIPort != m.info.Running.APIPort {
				effect = append(effect, "Open pages stop working; use the new address: http://127.0.0.1:"+itoa(m.draft.Config.APIPort)+"/app")
			} else {
				effect = append(effect, "Open pages reconnect by themselves.")
			}
		}
		if plan.ProjectsChanged() {
			effect = append(effect, "Bonsai picks up project changes within 30 s; nothing restarts for them.")
		}
	}
	for _, e := range effect {
		b.WriteString(indent(m.st.dim.Render(m.wrap(e, w-4)), 2) + "\n")
	}
	return m.frame("Review", m.counter(), b.String(), []hint{{"enter", "apply"}, {"esc", "go back"}})
}

func (m Model) onReview(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "enter":
		if m.mode == Edit && !m.dirty() {
			m.flash = "Nothing to apply yet."
			return m, nil
		}
		if err := m.plan().Config.Validate(); err != nil {
			m.flash = "These settings cannot be saved: " + err.Error()
			return m, nil
		}
		m.applying, m.steps, m.result = true, nil, nil
		m = m.push(screenApply)
		return m, m.startApply()
	case "esc":
		return m.pop(), nil
	case "q":
		return m.requestQuit()
	}
	return m, nil
}

// ── 7. Apply ────────────────────────────────────────────────────────────────

func (m Model) applyView() string {
	var b strings.Builder
	b.WriteString("\n")
	if len(m.steps) == 0 {
		b.WriteString("  " + m.st.stepGlyph(StepRunning) + " " + m.st.text.Render("Starting") + "\n")
	}
	w, _ := m.size()
	for _, s := range m.steps {
		label := "  " + m.st.stepGlyph(s.State) + " " + m.st.text.Render(s.Label)
		b.WriteString(m.hang(label, 33, m.st.dim.Render(s.Detail), w-1) + "\n")
		if s.State == StepFailed && s.Fix != "" {
			b.WriteString(m.hang("    "+m.st.dim.Render("fix"), 9, s.Fix, w-1) + "\n")
		}
	}
	hs := []hint{{"…", "applying"}}
	if !m.applying {
		hs = []hint{{"esc", "back to review"}, {"q", "quit"}}
	}
	return m.frame("Applying your settings", m.counter(), b.String(), hs)
}

func (m Model) onApplyKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		return m.pop(), nil
	case "q":
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

// ── 8. Done ─────────────────────────────────────────────────────────────────

func (m Model) doneView() string {
	var b strings.Builder
	b.WriteString("\n")
	title := "Bonsai is ready"
	if m.mode == Edit {
		title = "Settings saved"
		b.WriteString("  " + m.st.ok.Render("✓") + " " + m.st.text.Render("Your settings are saved.") + "\n")
	}
	if r := m.result; r != nil && r.URL != "" {
		b.WriteString("  " + m.st.ok.Render("✓") + " " + m.st.text.Render("bonsai web is running at ") + m.st.accent.Render(r.URL) + "\n")
		if r.Opened {
			b.WriteString("    " + m.st.dim.Render("Your browser opened it.") + "\n")
		} else {
			b.WriteString("    " + m.st.dim.Render("Press o to open it in your browser.") + "\n")
		}
	}
	if m.result != nil {
		for _, n := range m.result.Notes {
			b.WriteString("    " + m.st.dim.Render(n) + "\n")
		}
	}
	b.WriteString("\n")
	for _, line := range [][2]string{
		{"bonsai web status", "what is running"},
		{"bonsai web logs", "recent output"},
		{"bonsai web setup", "change these settings"},
		{"bonsai web stop", "stop everything"},
	} {
		b.WriteString("  " + padRight(m.st.text.Render(line[0]), 21) + m.st.dim.Render(line[1]) + "\n")
	}
	hs := []hint{{"o", "open browser"}, {"enter", "close"}}
	if m.mode == Edit {
		hs = []hint{{"o", "open browser"}, {"enter", "back to settings"}, {"q", "quit"}}
	}
	return m.frame(title, "", b.String(), hs)
}

func (m Model) onDone(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "o":
		if m.result == nil || m.result.URL == "" {
			m.flash = "bonsai web is not running. Start it with: bonsai web"
			return m, nil
		}
		if err := m.backend.Open(m.result.URL); err != nil {
			m.flash = "Could not open a browser (" + err.Error() + "). Open " + m.result.URL + " yourself."
		} else {
			m.result.Opened = true
		}
	case "enter", "esc":
		if m.mode == Edit {
			m.stack = []screen{screenDashboard}
			return m, nil
		}
		m.quitting = true
		return m, tea.Quit
	case "q":
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

// ── Edit mode: dashboard ────────────────────────────────────────────────────

type section struct {
	label, value string
	state        checks.State
	target       screen
	edited       bool
}

func (m Model) worst(ids ...string) checks.State {
	state := checks.OK
	rank := map[checks.State]int{checks.Skip: 0, checks.OK: 0, checks.Warn: 1, checks.Fail: 2}
	found := false
	for _, id := range ids {
		if c, ok := checks.Find(m.checks, id); ok {
			found = true
			if rank[c.State] > rank[state] {
				state = c.State
			}
		}
	}
	if !found {
		return checks.Skip
	}
	return state
}

func (m Model) updatesHealth() checks.State {
	if m.draft.Config.Updates.Mode == config.WebUpdatesLive {
		return checks.Warn
	}
	return checks.OK
}

func (m Model) sections() []section {
	b, a := m.initial.Config, m.draft.Config
	foldersEdited := false
	checked := map[string]bool{}
	for _, f := range m.initial.Folders {
		checked[f.Path] = f.Checked
	}
	for _, f := range m.draft.Folders {
		foldersEdited = foldersEdited || f.Checked != checked[f.Path]
	}
	return []section{
		{"Projects", setup.ProjectsSummary(m.draft, m.info.Home), m.worst(checks.IDProjects), screenProjects, foldersEdited},
		{"Open in", setup.InterfacesText(a.Interfaces) + " · auto-open " + onOff(a.OpenBrowser), checks.OK, screenOpenIn, a.Interfaces != b.Interfaces || a.OpenBrowser != b.OpenBrowser},
		{"GitHub", m.githubSummary(), m.worst(checks.IDGHInstalled, checks.IDGHAuth), screenGitHub, false},
		{"Updates", setup.UpdatesSummary(a, m.info.StandardInterval), m.updatesHealth(), screenUpdates, a.Updates.Mode != b.Updates.Mode},
		{"Advanced", "port " + itoa(a.APIPort), m.worst(checks.IDPort), screenAdvanced, a.APIPort != b.APIPort},
	}
}

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

func (m Model) dashboardView() string {
	w, _ := m.size()
	cursor := m.cursor[screenDashboard]
	var b strings.Builder
	b.WriteString("\n")
	valueWidth := w - 2 - 2 - 16 - 12
	for i, s := range m.sections() {
		value := s.value
		if s.edited {
			value = "edited · " + value
		}
		value = truncate(value, valueWidth)
		health := m.st.glyph(s.state)
		if s.state == checks.Skip {
			health = " "
		}
		b.WriteString(m.marker(i == cursor) + padRight(m.st.text.Render(s.label), 16) + padRight(m.st.text.Render(value), valueWidth) + "  " + health + "\n")
	}
	if m.dirty() {
		b.WriteString("\n  " + m.st.warn.Render("!") + " " + m.st.text.Render("Changes not applied yet") + m.st.dim.Render(" · a to review and apply") + "\n")
	}
	if failed, warned := checks.Counts(m.checks); failed+warned > 0 {
		b.WriteString("\n  " + m.st.dim.Render(problems(failed, warned)+" · d to see them in the doctor") + "\n")
	}
	return m.frame("bonsai web · settings", m.counter(), b.String(), []hint{{"enter", "edit"}, {"a", "apply"}, {"d", "doctor"}, {"q", "quit"}})
}

func truncate(s string, width int) string {
	if width < 4 {
		width = 4
	}
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	return string(r[:width-1]) + "…"
}

func (m Model) onDashboard(key string) (tea.Model, tea.Cmd) {
	sections := m.sections()
	switch key {
	case "up", "k":
		m.cursor[screenDashboard] = m.move(screenDashboard, -1, len(sections))
	case "down", "j":
		m.cursor[screenDashboard] = m.move(screenDashboard, 1, len(sections))
	case "enter":
		target := sections[m.cursor[screenDashboard]].target
		if target == screenAdvanced {
			m.portIn.SetValue(itoa(m.draft.Config.APIPort))
			m.portIn.CursorEnd()
			m.inputErr = ""
			m = m.push(target)
			return m, m.portIn.Focus()
		}
		if target == screenOpenIn {
			m.cursor[screenOpenIn] = m.openIndex()
		}
		return m.push(target), nil
	case "a":
		if !m.dirty() {
			m.flash = "Nothing to apply yet. Press enter on a section to change it."
			return m, nil
		}
		return m.push(screenReview), nil
	case "d":
		return m.push(screenDoctor), m.startChecks()
	case "q", "esc":
		return m.requestQuit()
	}
	return m, nil
}

// ── Edit mode: advanced ─────────────────────────────────────────────────────

func (m Model) advancedView() string {
	w, _ := m.size()
	var b strings.Builder
	b.WriteString("\n  " + m.portIn.View() + "\n")
	if m.inputErr != "" {
		b.WriteString("  " + m.st.fail.Render("✗") + " " + m.st.text.Render(m.inputErr) + "\n")
		b.WriteString("    " + m.st.dim.Render("fix  type a number from 1024 to 65535 that no other program uses") + "\n")
	}
	b.WriteString("\n" + indent(m.st.dim.Render(m.wrap("Your browser connects to http://127.0.0.1:<port>. Applying a new port restarts the Bonsai API; open pages need the new address.", w-4)), 2) + "\n")
	b.WriteString("\n" + m.row("Webhook port", itoa(m.draft.Config.Updates.Live.WebhookPort)+" · "+setup.LaterVersion) + "\n")
	b.WriteString(m.row("Secret", "rotate webhook secret · "+setup.LaterVersion) + "\n")
	return m.frame("Advanced", m.counter(), b.String(), []hint{{"enter", "done"}, {"esc", "back"}})
}

func (m Model) onAdvanced(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.portIn.Blur()
		m.inputErr = ""
		return m.pop(), nil
	case "enter":
		port, err := strconv.Atoi(strings.TrimSpace(m.portIn.Value()))
		if err != nil || port < 1 || port > 65535 {
			m.inputErr = "“" + m.portIn.Value() + "” is not a port number"
			return m, nil
		}
		next := m.draft.Config
		next.APIPort = port
		if err := next.Validate(); err != nil {
			m.inputErr = err.Error()
			return m, nil
		}
		m.draft.Config = next
		m.portIn.Blur()
		m.inputErr = ""
		return m.pop(), nil
	}
	var cmd tea.Cmd
	m.portIn, cmd = m.portIn.Update(msg)
	return m, cmd
}
