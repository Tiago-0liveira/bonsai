package websetup

import (
	"context"
	"reflect"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
	setup "github.com/Tiago-0liveira/bonsai/internal/websetup"
	"github.com/Tiago-0liveira/bonsai/internal/websetup/checks"
)

type Mode int

const (
	// Wizard is the first run: Welcome through Done.
	Wizard Mode = iota
	// Edit is `bonsai web setup` after the first run: a dashboard.
	Edit
)

type screen int

const (
	screenWelcome screen = iota
	screenProjects
	screenOpenIn
	screenGitHub
	screenUpdates
	screenReview
	screenApply
	screenDone
	screenDashboard
	screenAdvanced
	screenDoctor
)

// wizardSteps is the step counter: Welcome to Apply. Done is not counted.
const wizardSteps = 7

func (s screen) step() int {
	if s <= screenApply {
		return int(s) + 1
	}
	return 0
}

// Model is the setup TUI. Build it with New and run it with Bubble Tea.
type Model struct {
	backend Backend
	info    Info
	mode    Mode
	st      styles

	initial setup.Draft // what is on disk now
	draft   setup.Draft // what the user is choosing

	stack         []screen
	width, height int
	cursor        map[screen]int

	checks        []checks.Check
	checksLoading bool
	checksGen     int

	adding     bool // Projects: typing a folder
	folderIn   textinput.Model
	portIn     textinput.Model
	inputErr   string
	compare    bool // Updates: comparison matrix open
	confirming bool // Ctrl+C: discard changes?
	flash      string

	applying bool
	steps    []Step
	result   *Result
	applied  bool // at least one Apply succeeded
	quitting bool
}

// New builds the model. initial is the draft read from disk (setup.NewDraft).
func New(backend Backend, info Info, mode Mode, initial setup.Draft, r *lipgloss.Renderer) Model {
	if r == nil {
		r = lipgloss.DefaultRenderer()
	}
	// A steady cursor: no blink ticks, so the screen only changes on input.
	folder := textinput.New()
	folder.Cursor.SetMode(cursor.CursorStatic)
	folder.Placeholder = "~/code"
	folder.Prompt = "folder › "
	folder.CharLimit = 4096
	port := textinput.New()
	port.Cursor.SetMode(cursor.CursorStatic)
	port.Prompt = "API port › "
	port.CharLimit = 5
	start := screenWelcome
	if mode == Edit {
		start = screenDashboard
	}
	m := Model{
		backend:  backend,
		info:     info,
		mode:     mode,
		st:       newStyles(r, theme.Current),
		initial:  initial.Clone(),
		draft:    initial.Clone(),
		stack:    []screen{start},
		cursor:   map[screen]int{},
		folderIn: folder,
		portIn:   port,
	}
	return m
}

// Applied reports whether the user confirmed and Apply succeeded at least
// once; when false on exit, nothing was written.
func (m Model) Applied() bool { return m.applied }

// Result is the last Apply result, or nil.
func (m Model) Result() *Result { return m.result }

func (m Model) current() screen { return m.stack[len(m.stack)-1] }

func (m Model) push(s screen) Model {
	m.stack = append(append([]screen{}, m.stack...), s)
	m.flash = ""
	return m
}

func (m Model) pop() Model {
	if len(m.stack) > 1 {
		m.stack = m.stack[:len(m.stack)-1]
	}
	m.flash = ""
	return m
}

// dirty reports unapplied edits.
func (m Model) dirty() bool {
	if !reflect.DeepEqual(m.initial.Config, m.draft.Config) {
		return true
	}
	checked := map[string]bool{}
	for _, f := range m.initial.Folders {
		checked[f.Path] = f.Checked
	}
	for _, f := range m.draft.Folders {
		if f.Checked != checked[f.Path] {
			return true
		}
	}
	return false
}

func (m Model) firstRun() bool { return m.mode == Wizard }

func (m Model) plan() setup.Plan {
	return setup.BuildPlan(m.initial, m.draft, m.info.Running, m.firstRun())
}

// Messages.
type (
	checksMsg struct {
		gen    int
		checks []checks.Check
	}
	scanMsg struct {
		path  string
		repos []setup.Repo
		err   error
	}
	addedMsg struct {
		canonical string
		repos     []setup.Repo
		err       error
	}
	fixDoneMsg struct{ err error }
	applyMsg   struct {
		step   *Step
		result *Result
		events chan applyMsg
	}
)

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.runChecks()}
	for _, f := range m.draft.Folders {
		cmds = append(cmds, m.scan(f.Path))
	}
	return tea.Batch(cmds...)
}

func (m *Model) startChecks() tea.Cmd {
	m.checksGen++
	m.checksLoading = true
	return m.runChecks()
}

func (m Model) runChecks() tea.Cmd {
	gen, cfg, backend := m.checksGen, m.draft.Config, m.backend
	return func() tea.Msg {
		return checksMsg{gen: gen, checks: backend.Checks(context.Background(), cfg)}
	}
}

func (m Model) scan(path string) tea.Cmd {
	backend := m.backend
	return func() tea.Msg {
		_, repos, err := backend.ScanFolder(context.Background(), path)
		return scanMsg{path: path, repos: repos, err: err}
	}
}

func (m Model) addFolder(path string) tea.Cmd {
	backend := m.backend
	return func() tea.Msg {
		canonical, repos, err := backend.ScanFolder(context.Background(), path)
		return addedMsg{canonical: canonical, repos: repos, err: err}
	}
}

func (m Model) startApply() tea.Cmd {
	backend, plan := m.backend, m.plan()
	return func() tea.Msg {
		events := make(chan applyMsg, 32)
		go func() {
			result := backend.Apply(context.Background(), plan, func(s Step) {
				step := s
				events <- applyMsg{step: &step}
			})
			events <- applyMsg{result: &result}
			close(events)
		}()
		return nextApply(events)()
	}
}

func nextApply(events chan applyMsg) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-events
		if !ok {
			return nil
		}
		ev.events = events
		return ev
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case checksMsg:
		if msg.gen == m.checksGen {
			m.checks, m.checksLoading = msg.checks, false
			m.placeGitHubCursor()
		}
		return m, nil
	case scanMsg:
		for i, f := range m.draft.Folders {
			if f.Path == msg.path {
				m.draft.SetScan(i, msg.repos, errText(msg.err))
			}
		}
		for i, f := range m.initial.Folders {
			if f.Path == msg.path {
				m.initial.SetScan(i, msg.repos, errText(msg.err))
			}
		}
		return m, nil
	case addedMsg:
		if msg.err != nil {
			m.adding, m.inputErr = true, msg.err.Error()
			return m, nil
		}
		i := m.draft.AddFolder(msg.canonical)
		m.draft.SetScan(i, msg.repos, "")
		m.adding, m.inputErr = false, ""
		m.folderIn.Reset()
		m.folderIn.Blur()
		m.cursor[screenProjects] = i
		return m, nil
	case fixDoneMsg:
		if msg.err != nil {
			m.flash = "The fix did not finish: " + msg.err.Error()
		}
		return m, m.startChecks()
	case applyMsg:
		return m.onApply(msg)
	case tea.KeyMsg:
		return m.onKey(msg)
	}
	return m, nil
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (m Model) onApply(msg applyMsg) (tea.Model, tea.Cmd) {
	if msg.step != nil {
		replaced := false
		for i, s := range m.steps {
			if s.ID == msg.step.ID {
				m.steps[i], replaced = *msg.step, true
			}
		}
		if !replaced {
			m.steps = append(m.steps, *msg.step)
		}
		return m, nextApply(msg.events)
	}
	if msg.result != nil {
		m.applying = false
		m.result = msg.result
		if msg.result.OK {
			m.applied = true
			m.draft = m.draft.AfterApply()
			m.initial = m.draft.Clone()
			m.info.Running = runningAfter(m.draft, m.info.Running, msg.result)
			m.stack = append(m.stack[:len(m.stack)-1], screenDone)
			return m, m.startChecks()
		}
	}
	return m, nil
}

// runningAfter is what is running once Apply succeeded.
func runningAfter(d setup.Draft, before *setup.Running, r *Result) *setup.Running {
	if r.Port == 0 {
		return before
	}
	return &setup.Running{APIPort: r.Port, Hosted: d.Config.Interfaces.Hosted}
}

func (m Model) onKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.confirming {
		switch key {
		case "y", "Y":
			m.quitting = true
			return m, tea.Quit
		case "n", "N", "esc":
			m.confirming = false
		}
		return m, nil
	}
	if key == "ctrl+c" {
		return m.requestQuit()
	}
	if m.applying {
		m.flash = "Applying your settings; this takes a few seconds."
		return m, nil
	}
	if m.adding {
		return m.onFolderInput(msg)
	}
	if m.current() == screenAdvanced {
		return m.onAdvanced(msg)
	}
	if m.compare {
		if key == "esc" || key == "?" || key == "q" {
			m.compare = false
		}
		return m, nil
	}
	m.flash = ""
	switch m.current() {
	case screenWelcome:
		return m.onWelcome(key)
	case screenProjects:
		return m.onProjects(key)
	case screenOpenIn:
		return m.onOpenIn(key)
	case screenGitHub:
		return m.onChecks(key, githubChecks)
	case screenUpdates:
		return m.onUpdates(key)
	case screenReview:
		return m.onReview(key)
	case screenApply:
		return m.onApplyKey(key)
	case screenDone:
		return m.onDone(key)
	case screenDashboard:
		return m.onDashboard(key)
	case screenDoctor:
		return m.onChecks(key, nil)
	}
	return m, nil
}

// requestQuit quits at once when nothing would be lost, and asks otherwise.
func (m Model) requestQuit() (tea.Model, tea.Cmd) {
	if m.applying {
		m.flash = "Applying your settings; wait for it to finish."
		return m, nil
	}
	if m.dirty() {
		m.confirming = true
		return m, nil
	}
	m.quitting = true
	return m, tea.Quit
}

// next moves forward in the wizard, or back to the dashboard in edit mode.
func (m Model) next() (tea.Model, tea.Cmd) {
	if m.mode == Edit {
		return m.pop(), nil
	}
	return m.push(m.current() + 1), nil
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}
	if m.confirming {
		return m.confirmView()
	}
	switch m.current() {
	case screenWelcome:
		return m.welcomeView()
	case screenProjects:
		return m.projectsView()
	case screenOpenIn:
		return m.openInView()
	case screenGitHub:
		return m.githubView()
	case screenUpdates:
		return m.updatesView()
	case screenReview:
		return m.reviewView()
	case screenApply:
		return m.applyView()
	case screenDone:
		return m.doneView()
	case screenDashboard:
		return m.dashboardView()
	case screenAdvanced:
		return m.advancedView()
	case screenDoctor:
		return m.doctorView()
	}
	return ""
}

// counter is the title bar's right side: the wizard step, or bonsai web's
// state in edit mode.
func (m Model) counter() string {
	if m.mode == Edit {
		if m.info.Running != nil {
			return m.st.ok.Render("●") + m.st.dim.Render(" running · ") + m.st.text.Render(itoa(m.info.Running.APIPort))
		}
		return m.st.dim.Render("○ not running")
	}
	if step := m.current().step(); step > 0 {
		return m.st.dim.Render(itoa(step) + " / " + itoa(wizardSteps))
	}
	return ""
}

func (m Model) confirmView() string {
	body := "\n  " + m.st.warn.Render("!") + " " + m.st.text.Render("Discard your changes and quit?") + "\n\n" +
		"  " + m.st.dim.Render("Nothing has been saved yet. Your current settings stay as they are.")
	return m.frame("bonsai web · setup", "", body, []hint{{"y", "discard and quit"}, {"n", "keep editing"}})
}
