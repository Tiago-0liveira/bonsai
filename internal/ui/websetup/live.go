package websetup

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	setup "github.com/Tiago-0liveira/bonsai/internal/websetup"
	"github.com/Tiago-0liveira/bonsai/internal/websetup/checks"
	"github.com/Tiago-0liveira/bonsai/internal/webtunnel"
)

// Live updates take two more steps after the Updates screen: the tunnel's
// own settings (only for tunnels that need some) and the repositories that
// get a webhook. Both only edit the draft; Review lists what changes on
// GitHub before anything does.

type liveReposMsg struct {
	gen   int
	repos []setup.LiveRepo
}

// tunnelTool is the check of the program a tunnel preset runs, "" for none.
func tunnelTool(preset string) string {
	switch preset {
	case setup.UpdatesCloudflaredQuick, setup.UpdatesCloudflaredNamed:
		return checks.IDTunnelCloudflared
	case setup.UpdatesNgrok:
		return checks.IDTunnelNgrok
	case setup.UpdatesTailscale:
		return checks.IDTunnelTailscale
	}
	return ""
}

// missingTool returns the install fix when the tool preset runs is known
// not to be installed.
func (m Model) missingTool(preset string) (string, string, bool) {
	c, ok := checks.Find(m.checks, tunnelTool(preset))
	if !ok || !strings.HasPrefix(c.Detail, "not installed") {
		return "", "", false
	}
	fix := ""
	if c.Fix != nil {
		fix = c.Fix.Command
	}
	return c.Title, fix, true
}

// chooseLive selects a live tunnel on the Updates screen. With enter it
// moves on to the tunnel settings, or straight to the repositories.
func (m Model) chooseLive(c setup.Choice, open bool) (tea.Model, tea.Cmd) {
	if tool, fix, missing := m.missingTool(c.ID); missing {
		m.flash = tool + " is not installed. Install it (" + fix + "), then press r to check again."
		return m, nil
	}
	changed := m.draft.Config.Updates.Mode != config.WebUpdatesLive || m.draft.Config.Updates.Live.Tunnel != c.ID
	m.draft.Config.Updates.Mode = config.WebUpdatesLive
	m.draft.Config.Updates.Live.Tunnel = c.ID
	var cmds []tea.Cmd
	if changed {
		// The chosen tunnel decides which tool checks count.
		cmds = append(cmds, m.startChecks())
	}
	if !open {
		return m, tea.Batch(cmds...)
	}
	if fields := setup.LiveFields(c.ID, m.draft.Config.Updates.Live.WebhookPort); len(fields) > 0 {
		m = m.openTunnelSettings(fields)
		m = m.push(screenLiveTunnel)
		return m, tea.Batch(append(cmds, m.liveIn[0].Focus())...)
	}
	m, cmd := m.openLiveRepos()
	return m, tea.Batch(append(cmds, cmd)...)
}

// finishUpdates leaves the live steps: to Review in the wizard, back to the
// dashboard in edit mode.
func (m Model) finishUpdates() (tea.Model, tea.Cmd) {
	if m.mode == Edit {
		m.stack = []screen{screenDashboard}
		m.flash = ""
		return m, nil
	}
	return m.push(screenReview), nil
}

// ── 5a. Tunnel settings ─────────────────────────────────────────────────────

func (m Model) openTunnelSettings(fields []setup.LiveField) Model {
	m.liveFields = fields
	m.liveIn = make([]textinput.Model, len(fields))
	for i, f := range fields {
		in := textinput.New()
		in.Cursor.SetMode(cursor.CursorStatic)
		in.Prompt = "› "
		in.Placeholder = f.Placeholder
		in.CharLimit = 1024
		in.SetValue(setup.LiveFieldValue(m.draft.Config.Updates.Live, f.ID))
		in.CursorEnd()
		m.liveIn[i] = in
	}
	m.liveFocus, m.inputErr = 0, ""
	return m
}

func (m Model) tunnelSettingsView() string {
	w, _ := m.size()
	var b strings.Builder
	b.WriteString("\n")
	labels := make([]string, len(m.liveFields))
	width := 0
	for i, f := range m.liveFields {
		labels[i] = f.Label
		if f.Optional {
			labels[i] += " (optional)"
		}
		width = max(width, len([]rune(labels[i]))+2)
	}
	for i, f := range m.liveFields {
		b.WriteString(m.marker(i == m.liveFocus) + padRight(m.st.text.Render(labels[i]), width) + m.liveIn[i].View() + "\n")
		b.WriteString(indent(m.st.dim.Render(m.wrap(f.Help, w-8)), 5) + "\n\n")
	}
	if m.inputErr != "" {
		b.WriteString(m.hang("  "+m.st.fail.Render("✗"), 4, m.inputErr, w-1) + "\n")
	}
	hs := []hint{{"tab", "next field"}, {"enter", "next"}, {"esc", "back"}}
	return m.frame("Set up the "+setup.TunnelText(m.draft.Config.Updates.Live.Tunnel), m.counter(), b.String(), hs)
}

func (m Model) focusLiveField(i int) (Model, tea.Cmd) {
	if i < 0 || i >= len(m.liveIn) || i == m.liveFocus {
		return m, nil
	}
	m.liveIn[m.liveFocus].Blur()
	m.liveFocus = i
	return m, m.liveIn[i].Focus()
}

func (m Model) onTunnelSettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.inputErr = ""
		return m.pop(), nil
	case "tab", "down":
		return m.focusLiveField((m.liveFocus + 1) % len(m.liveIn))
	case "shift+tab", "up":
		return m.focusLiveField((m.liveFocus + len(m.liveIn) - 1) % len(m.liveIn))
	case "enter":
		if m.liveFocus < len(m.liveIn)-1 {
			return m.focusLiveField(m.liveFocus + 1)
		}
		live := m.draft.Config.Updates.Live
		live.Command = append([]string{}, live.Command...)
		for i, f := range m.liveFields {
			if problem := setup.CheckLiveField(f, m.liveIn[i].Value()); problem != "" {
				m.inputErr = problem
				return m.focusLiveField(i)
			}
			setup.SetLiveField(&live, f.ID, m.liveIn[i].Value())
		}
		if err := validateTunnel(live, m.draft.Config.APIPort); err != nil {
			m.inputErr = err.Error()
			return m, nil
		}
		m.draft.Config.Updates.Live = live
		m.inputErr = ""
		m.liveIn[m.liveFocus].Blur()
		return m.openLiveRepos()
	}
	var cmd tea.Cmd
	m.liveIn[m.liveFocus], cmd = m.liveIn[m.liveFocus].Update(msg)
	return m, cmd
}

// validateTunnel runs the same checks bonsai web does before it starts the
// tunnel, so a command aimed at the API port never gets past this screen.
func validateTunnel(live config.WebLiveUpdates, apiPort int) error {
	argv, err := webtunnel.Argv(live.TunnelOptions())
	if err != nil {
		return err
	}
	if argv != nil {
		return webtunnel.ValidateArgv(argv, live.WebhookPort, apiPort)
	}
	return nil
}

// ── 5b. Live repositories ───────────────────────────────────────────────────

// openLiveRepos shows the picker and asks GitHub which repositories the gh
// login administers.
func (m Model) openLiveRepos() (Model, tea.Cmd) {
	m.livePicked = map[string]bool{}
	for _, repo := range m.draft.Config.Updates.Live.Repositories {
		m.livePicked[strings.ToLower(repo)] = true
	}
	m.cursor[screenLiveRepos] = 0
	m = m.push(screenLiveRepos)
	return m, m.loadLiveRepos()
}

func (m *Model) loadLiveRepos() tea.Cmd {
	m.liveGen++
	m.liveLoading = true
	gen, backend := m.liveGen, m.backend
	configured := append([]string{}, m.draft.Config.Updates.Live.Repositories...)
	var repos []setup.Repo
	for _, f := range m.draft.CheckedFolders() {
		repos = append(repos, f.Repos...)
	}
	return func() tea.Msg {
		return liveReposMsg{gen: gen, repos: backend.LiveRepositories(context.Background(), repos, configured)}
	}
}

func (m Model) pickable(r setup.LiveRepo) bool { return r.Admin && r.Err == "" }

func (m Model) livePickedRepos() []string {
	var out []string
	for _, r := range m.liveRepos {
		if m.livePicked[strings.ToLower(r.FullName)] {
			out = append(out, r.FullName)
		}
	}
	return out
}

func (m Model) liveReposView() string {
	w, _ := m.size()
	var b strings.Builder
	b.WriteString("\n" + indent(m.st.dim.Render(m.wrap(setup.LiveReposLead, w-4)), 2) + "\n\n")
	switch {
	case m.liveLoading && len(m.liveRepos) == 0:
		b.WriteString("  " + m.st.accent.Render("…") + " " + m.st.dim.Render("checking your access on GitHub") + "\n")
	case len(m.liveRepos) == 0:
		b.WriteString("  " + m.st.warn.Render("!") + " " + m.st.text.Render("No GitHub repositories in your project folders.") + "\n")
		b.WriteString("    " + m.st.dim.Render("fix  add the folder of a GitHub repository in Projects, or choose Standard") + "\n")
	}
	width := 12
	for _, r := range m.liveRepos {
		if n := len([]rune(r.FullName)); n > width {
			width = n
		}
	}
	if width > 40 {
		width = 40
	}
	cursor := m.cursor[screenLiveRepos]
	for i, r := range m.liveRepos {
		picked := m.livePicked[strings.ToLower(r.FullName)]
		box := "[ ]"
		if picked {
			box = "[x]"
		}
		name := m.st.text.Render(padRight(r.FullName, width))
		status := m.st.dim.Render(r.Local)
		switch {
		case r.Err != "":
			box = m.st.warn.Render("!") + "  "
			if picked {
				box = "[x]"
			}
			status = m.st.warn.Render("!") + " " + m.st.dim.Render(r.Err)
		case !r.Admin:
			box = m.st.dim.Render("–") + "  "
			name = m.st.dim.Render(padRight(r.FullName, width))
			status = m.st.dim.Render(setup.NeedsAdmin)
		}
		b.WriteString(m.marker(i == cursor) + box + " " + name + "  " + status + "\n")
	}
	if len(m.liveRepos) > 0 {
		picked := len(m.livePickedRepos())
		summary := plural(picked, "repo") + " picked"
		if m.liveLoading {
			summary += " · checking again…"
		}
		b.WriteString("\n  " + m.st.dim.Render(summary) + "\n")
	}
	hs := []hint{{"space", "toggle"}, {"enter", "next"}, {"r", "re-check"}, {"esc", "back"}}
	if m.mode == Edit {
		hs[1] = hint{"enter", "done"}
	}
	return m.frame("Which repos get live updates?", m.counter(), b.String(), hs)
}

func (m Model) onLiveRepos(key string) (tea.Model, tea.Cmd) {
	rows := len(m.liveRepos)
	cursor := m.cursor[screenLiveRepos]
	switch key {
	case "up", "k":
		m.cursor[screenLiveRepos] = m.move(screenLiveRepos, -1, rows)
	case "down", "j":
		m.cursor[screenLiveRepos] = m.move(screenLiveRepos, 1, rows)
	case " ", "x":
		if cursor >= rows {
			return m, nil
		}
		r := m.liveRepos[cursor]
		k := strings.ToLower(r.FullName)
		switch {
		case m.livePicked[k]:
			delete(m.livePicked, k)
		case r.Err != "":
			m.flash = "Bonsai cannot check " + r.FullName + " right now: " + r.Err
		case !r.Admin:
			m.flash = "You need admin rights on " + r.FullName + " to add a webhook; it stays on Standard."
		default:
			m.livePicked[k] = true
		}
	case "r":
		return m, m.loadLiveRepos()
	case "enter":
		if m.liveLoading && len(m.liveRepos) == 0 {
			m.flash = "Still checking your access on GitHub; press enter again in a moment."
			return m, nil
		}
		picked := m.livePickedRepos()
		if len(picked) == 0 {
			m.flash = "Pick at least one repository, or go back and choose Standard."
			return m, nil
		}
		m.draft.Config.Updates.Live.Repositories = picked
		return m.finishUpdates()
	case "esc":
		return m.pop(), nil
	case "q":
		return m.requestQuit()
	}
	return m, nil
}

// onLiveReposLoaded shows the backend's answer. It lists the configured
// repositories too, even those no project folder has any more, so they can
// still be unpicked.
func (m Model) onLiveReposLoaded(msg liveReposMsg) Model {
	if msg.gen != m.liveGen {
		return m
	}
	m.liveLoading = false
	m.liveRepos = msg.repos
	if c := m.cursor[screenLiveRepos]; c >= len(m.liveRepos) {
		m.cursor[screenLiveRepos] = 0
	}
	return m
}
