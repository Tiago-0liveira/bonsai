package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/accounts"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/launchform"
	"github.com/Tiago-0liveira/bonsai/internal/ui/usagestore"
)

// launchCallTimeout bounds the read-only backend calls of the dialog so a hung
// provider cannot leave it loading forever. Start has no timeout of its own:
// the user can always Esc out of a start that is taking long.
const launchCallTimeout = 15 * time.Second

const launchNameLen = 60

type launchFocus int

const (
	launchFocusAccount launchFocus = iota
	launchFocusTask
	launchFocusName
	launchFocusForm
	launchFocusSave
	launchFocusButtons
	launchFocusCount
)

// launchTarget is where a new agent starts: the same identities the web sends
// (project = config.ProjectID of the main root, worktree = local.ID with the
// web's repository ID), so both clients see one session under one worktree.
type launchTarget struct {
	worktree, workDir, branch string
}

// launchSubmitMsg asks the model to start an agent. The model adds the size of
// the terminal area, which only it knows.
type launchSubmitMsg struct {
	dlg     uint64
	req     agentsapi.StartRequest
	account agents.AccountID
	// save holds the form changes to persist as the account's defaults after a
	// successful start; nil when the box was not ticked or nothing changed.
	save map[string]*string
}

type (
	// launchStartedMsg is the answer to a start request.
	launchStartedMsg struct {
		dlg     uint64
		session agentsapi.Session
		account agents.AccountID
		save    map[string]*string
		workDir string
		err     error
	}
	launchSavedMsg struct{ err error }
)

// launchDialog is the new-agent dialog. It is a value type like the accounts
// modal and embeds the same accounts.Browser, so the account list, selection,
// describe info, defaults and usage load exactly as they do there. The start
// answer carries the dialog ID so one for a closed dialog is dropped.
type launchDialog struct {
	accounts.Browser
	id             uint64
	ascii          bool
	target         launchTarget
	targets        []launchTarget
	choosing       bool
	targetIndex    int
	changeWorktree key.Binding
	task           textarea.Model
	// planner turns the dialog into the Garden planner's account choice: only model and effort can be set,
	// the launch is read-only, and an account whose provider cannot enforce that cannot be chosen.
	planner bool
	// revise makes the planner choice the one behind Send feedback & revise plan: it accepts the read-only
	// contract for a revision attempt instead of a new plan.
	revise bool
	// issue is the opened Issue's "#n title" the planner's choice is headed with; empty keeps the plain heading.
	issue string
	now   func() time.Time
	w, h  int

	focus         launchFocus
	name          textinput.Model
	nameTouched   bool
	sessions      []agentsapi.Session
	accountPinned bool
	suggested     agents.AccountID
	seed          *agentsapi.Session

	form       launchform.Form
	formLoaded bool
	formErr    string
	save       bool
	button     int // 0 Cancel, 1 Start / Sign in
	starting   bool
	err        string
}

func newLaunchDialog(id uint64, api agentsapi.Accounts, target launchTarget, initial agents.AccountID, usage usagestore.Store, ascii bool) launchDialog {
	ti := textinput.New()
	ti.Prompt = "> "
	ti.CharLimit = launchNameLen
	// A steady cursor needs no blink ticks fed back through Update.
	ti.Cursor.SetMode(cursor.CursorStatic)
	ta := textarea.New()
	ta.CharLimit = 8192
	ta.SetHeight(3)
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.Placeholder = "Optional task for the agent"
	ta.Cursor.SetMode(cursor.CursorStatic)
	return launchDialog{
		task: ta, changeWorktree: binding("launch_change_worktree", nil),
		Browser: accounts.NewBrowser(api, initial, usage, true),
		id:      id, ascii: ascii, target: target,
		now: time.Now, name: ti, button: 1, accountPinned: initial != "",
	}
}

func (d *launchDialog) SetSize(w, h int) { d.w, d.h = w, h; d.task.SetWidth(max(w-8, 1)) }

// Capturing reports that a text input or dropdown has the keys.
func (d launchDialog) Capturing() bool {
	return d.focus == launchFocusTask || d.focus == launchFocusName || (d.focus == launchFocusForm && d.form.Editing())
}

// launchCtx bounds the dialog's own backend calls (saving defaults).
func launchCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), launchCallTimeout)
}

func emitMsg(m tea.Msg) tea.Cmd { return func() tea.Msg { return m } }

// sidebar renders the shared account list; the same value orders j/k.
func (d launchDialog) sidebar() accounts.Sidebar {
	busy := map[agents.AccountID]bool{}
	for _, a := range d.Accounts() {
		// Only a hint: admission decides whether the account is really in use.
		busy[a.ID] = a.Running > 0 && !a.ConcurrentSameAccount
	}
	return accounts.Sidebar{
		Accounts: d.Accounts(), Selected: d.Selected(), Busy: busy, ASCII: d.ascii,
	}
}

// Update handles keys and results.
func (d launchDialog) Update(msg tea.Msg) (launchDialog, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		return d.onKey(k)
	}
	prev := d.Selected()
	var cmd tea.Cmd
	var ch accounts.Change
	d.Browser, cmd, ch = d.Browser.Update(msg)
	d.suggested = d.suggestedAccount()
	if !d.planner && !d.accountPinned && d.suggested != "" && d.suggested != d.Selected() {
		var pick tea.Cmd
		d.Browser, pick = d.Browser.Select(d.suggested)
		cmd = tea.Batch(cmd, pick)
	}
	// Re-selecting after a reload re-reads the form: a sign-in changes nothing
	// in it, but a defaults edit made in the accounts modal would.
	if d.Selected() != prev || (ch&accounts.ChangedList != 0 && d.ListErr() == "") {
		d = d.resetForm()
	}
	if ch&accounts.ChangedDefaults != 0 {
		d = d.onDefaults()
	}
	return d, cmd
}

// resetForm clears what belongs to the previous account's launch options.
func (d launchDialog) resetForm() launchDialog {
	d.formLoaded, d.formErr, d.save, d.err = false, "", false, ""
	if a, ok := d.Current(); ok && !d.nameTouched {
		d.name.SetValue(d.defaultName(a))
	}
	return d
}

func (d launchDialog) onDefaults() launchDialog {
	if msg := d.DefaultsErr(); msg != "" {
		d.formErr = msg
		return d
	}
	d.formErr = ""
	def := d.Defaults()
	// Changes here apply to this launch only: the provider's own launch fields
	// seeded from the account's defaults.
	fields := launchDialogFields(def.LaunchFields)
	if d.planner {
		fields = plannerDialogFields(fields)
	}
	values := def.Values
	if d.seed != nil {
		values = make(map[string]string, len(def.Values)+len(d.seed.Options)+1)
		for _, field := range fields {
			values[field.ID] = ""
		}
		for k, v := range d.seed.Options {
			values[k] = v
		}
		for _, field := range fields {
			for _, option := range field.Options {
				if len(option.Set) == 0 {
					continue
				}
				matches := true
				for k, v := range option.Set {
					if d.seed.Options[k] != v {
						matches = false
					}
				}
				if matches {
					values[field.ID] = option.Value
					break
				}
			}
		}
		values["model"] = d.seed.Model
	}
	d.form = launchform.New(fields, def.Models, values, d.ascii).Focus(d.focus == launchFocusForm)
	d.formLoaded = true
	return d
}

// The launch schema omits model because it is normally a top-level request
// field. Put it and effort first so one-session overrides are easy to find.
func launchDialogFields(fields []agents.LaunchField) []agents.LaunchField {
	model := agents.ProfileModelField()
	model.Options = []agents.FieldOption{{Value: "", Label: "Profile default"}}
	out := []agents.LaunchField{model}
	for _, f := range fields {
		if f.ID == "effort" {
			out = append(out, f)
		}
	}
	for _, f := range fields {
		if f.ID != "model" && f.ID != "effort" {
			out = append(out, f)
		}
	}
	return out
}

// plannerDialogFields keeps the model and the effort. Permission, sandbox, approval and full-access choices
// do not exist for a read-only role: the provider's enforced restriction is the only permission it runs with.
func plannerDialogFields(fields []agents.LaunchField) []agents.LaunchField {
	var out []agents.LaunchField
	for _, f := range fields {
		if f.Kind == agents.LaunchFieldModel || f.ID == "model" || f.ID == "effort" {
			out = append(out, f)
		}
	}
	return out
}

func (d launchDialog) setFocus(f launchFocus) launchDialog {
	d.focus = (f + launchFocusCount) % launchFocusCount
	if d.planner && d.focus == launchFocusTask {
		if f < launchFocusTask {
			d.focus = launchFocusAccount
		} else {
			d.focus = launchFocusName
		}
	}
	if d.focus == launchFocusTask {
		d.task.Focus()
	} else {
		d.task.Blur()
	}
	if d.focus == launchFocusName {
		d.name.Focus()
	} else {
		d.name.Blur()
	}
	d.form = d.form.Focus(d.focus == launchFocusForm)
	return d
}

func (d launchDialog) onKey(k tea.KeyMsg) (launchDialog, tea.Cmd) {
	s := k.String()
	close := emitMsg(accounts.CloseMsg{})
	switch s {
	case "ctrl+c":
		return d, close
	case "ctrl+s":
		if d.choosing {
			return d, nil
		}
		return d.submit()
	}
	if d.starting {
		if s == "esc" {
			return d, close
		}
		return d, nil
	}
	if d.choosing {
		switch s {
		case "esc":
			d.choosing = false
		case "up", "k":
			d.targetIndex = max(d.targetIndex-1, 0)
		case "down", "j":
			d.targetIndex = min(d.targetIndex+1, len(d.targets)-1)
		case "enter":
			if len(d.targets) > 0 {
				d.target = d.targets[d.targetIndex]
			}
			d.choosing = false
			d = d.updateName()
		}
		return d, nil
	}
	if !d.planner && key.Matches(k, d.changeWorktree) && len(d.targets) > 0 {
		d.choosing = true
		for i, t := range d.targets {
			if t.workDir == d.target.workDir {
				d.targetIndex = i
			}
		}
		return d, nil
	}
	editing := d.focus == launchFocusForm && d.form.Editing()
	if !editing {
		switch s {
		case "esc":
			return d, close
		case "tab":
			if d.focus == launchFocusForm && !d.form.AtLast() {
				d.form, _ = d.form.Update(tea.KeyMsg{Type: tea.KeyDown})
				return d, nil
			}
			return d.setFocus(d.focus + 1), nil
		case "shift+tab":
			if d.focus == launchFocusForm && !d.form.AtFirst() {
				d.form, _ = d.form.Update(tea.KeyMsg{Type: tea.KeyUp})
				return d, nil
			}
			return d.setFocus(d.focus - 1), nil
		}
	}
	switch d.focus {
	case launchFocusAccount:
		return d.accountKey(s)
	case launchFocusTask:
		var cmd tea.Cmd
		d.task, cmd = d.task.Update(k)
		d = d.updateName()
		return d, cmd
	case launchFocusName:
		if s == "enter" || s == "down" {
			return d.setFocus(launchFocusForm), nil
		}
		if s == "up" {
			return d.setFocus(launchFocusTask), nil
		}
		before := d.name.Value()
		var cmd tea.Cmd
		d.name, cmd = d.name.Update(k)
		d.nameTouched = d.nameTouched || before != d.name.Value()
		return d, cmd
	case launchFocusForm:
		if !editing && s == "down" && d.form.AtLast() {
			return d.setFocus(launchFocusSave), nil
		}
		if !editing && s == "up" && d.form.AtFirst() {
			return d.setFocus(launchFocusName), nil
		}
		if d.formLoaded {
			d.form, _ = d.form.Update(k)
		}
	case launchFocusSave:
		if s == "up" {
			return d.setFocus(launchFocusForm), nil
		}
		if s == "down" {
			return d.setFocus(launchFocusButtons), nil
		}
		if s == "enter" || s == " " {
			d.save = !d.save
		}
	case launchFocusButtons:
		switch s {
		case "up":
			return d.setFocus(launchFocusSave), nil
		case "left", "h":
			d.button = 0
		case "right", "l":
			d.button = 1
		case "enter":
			if d.button == 0 {
				return d, close
			}
			return d.submit()
		}
	}
	return d, nil
}

func (d launchDialog) accountKey(s string) (launchDialog, tea.Cmd) {
	delta := 0
	switch s {
	case "j", "down":
		delta = 1
	case "k", "up":
		delta = -1
	case "enter":
		return d.setFocus(launchFocusTask), nil
	}
	ordered := d.Ordered()
	if delta == 0 || len(ordered) == 0 {
		return d, nil
	}
	i := 0
	for n, a := range ordered {
		if a.ID == d.Selected() {
			i = n
		}
	}
	next := ordered[min(max(i+delta, 0), len(ordered)-1)].ID
	if next == d.Selected() {
		return d, nil
	}
	d.accountPinned = true
	d.seed = nil
	var cmd tea.Cmd
	d.Browser, cmd = d.Browser.Select(next)
	return d.resetForm(), cmd
}

// submit starts the agent, or opens the sign-in flow for an account that has
// no login.
func (d launchDialog) submit() (launchDialog, tea.Cmd) {
	if d.starting {
		return d, nil
	}
	a, ok := d.Current()
	if !ok {
		d.err = "Choose an account first."
		return d, nil
	}
	if accounts.NeedsSignIn(a.Info) {
		if c := d.LoginCommand(a.ID); c != nil {
			return d, emitMsg(accounts.ExecMsg{Cmd: c})
		}
		return d, nil
	}
	if d.planner && !a.ReadOnly.Supported {
		// Refused here and again by the backend: there is no unrestricted fallback.
		d.err = plannerUnavailableText(a)
		return d, nil
	}
	if !d.formLoaded {
		d.err = "Options are still loading."
		return d, nil
	}
	name := strings.TrimSpace(d.name.Value())
	if name == "" {
		name = d.defaultName(a)
	}
	values := d.form.Values()
	req := agentsapi.StartRequest{
		WorktreeID: d.target.worktree,
		WorkDir:    d.target.workDir,
		AccountID:  a.ID,
		Name:       name,
		Launch:     agents.LaunchOptionsFrom(d.Defaults().LaunchFields, values["model"], values),
	}
	req.Launch.Prompt = sanitizeLaunchTask(d.task.Value())
	if d.planner {
		// Only what the form can set survives; the role fixes the rest.
		req.Launch = agents.LaunchOptions{Model: req.Launch.Model, Effort: req.Launch.Effort, ReadOnly: true}
	}
	sub := launchSubmitMsg{dlg: d.id, req: req, account: a.ID}
	if d.save {
		if ch := d.form.Changes(); len(ch) > 0 {
			sub.save = ch
		}
	}
	d.starting, d.err = true, ""
	return d, emitMsg(sub)
}

// plannerUnavailableText is why an account cannot take the planner role.
func plannerUnavailableText(a agentsapi.Account) string {
	reason := a.ReadOnly.Reason
	if reason == "" {
		reason = "no enforced read-only mode"
	}
	return providerName(a.Provider) + " cannot plan: " + reason + ". Choose another account."
}

// failed shows a start error inline and lets the user try again.
func (d launchDialog) failed(err error) launchDialog {
	d.starting = false
	d.err = sanitizeAgentText(err.Error())
	if d.err == "" {
		d.err = "Could not start the agent."
	}
	return d
}

// sanitizeAgentText makes provider and backend error text safe to draw: no
// escapes or control bytes that could move the cursor or recolor the screen.
func sanitizeAgentText(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, ansi.Strip(s))
	return ansi.Truncate(strings.TrimSpace(s), 200, "…")
}

// Task content stays in the transient dialog and start request, never account defaults.
func sanitizeLaunchTask(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, ansi.Strip(s))
	r := []rune(strings.TrimSpace(s))
	return string(r[:min(len(r), 8192)])
}

func launchShortPath(path string) string {
	if home := os.Getenv("HOME"); home != "" {
		if path == home {
			return "~"
		}
		if rel, err := filepath.Rel(home, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.Join("~", rel)
		}
	}
	// The last two components distinguish sibling worktrees without consuming the header.
	parent := filepath.Base(filepath.Dir(path))
	if parent == "." || parent == string(filepath.Separator) {
		return path
	}
	return "…/" + filepath.Join(parent, filepath.Base(path))
}

func (d launchDialog) targetLines() []string {
	lines := []string{"Choose worktree"}
	for i, t := range d.targets {
		marker := "  "
		if i == d.targetIndex {
			marker = "> "
		}
		lines = append(lines, fmt.Sprintf("%s%s (%s)", marker, t.branch, launchShortPath(t.workDir)))
	}
	lines = append(lines, "↑↓ worktree · enter select · esc back")
	return lines
}

func launchSlug(s string, words int) string {
	parts := strings.Fields(s)
	if words > 0 {
		parts = parts[:min(len(parts), words)]
	}
	var out strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.Join(parts, "-")) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if dash && out.Len() > 0 {
				out.WriteByte('-')
			}
			out.WriteRune(r)
			dash = false
		} else {
			dash = true
		}
	}
	r := []rune(out.String())
	return string(r[:min(len(r), launchNameLen)])
}

func (d launchDialog) defaultName(a agentsapi.Account) string {
	if d.planner {
		return a.Name
	}
	if task := launchSlug(sanitizeLaunchTask(d.task.Value()), 4); task != "" {
		return task
	}
	branch := []rune(launchSlug(d.target.branch, 0))
	prefix := string(branch[:min(len(branch), 35)]) + "-" + string(a.Provider) + "-"
	for n := 1; ; n++ {
		name := fmt.Sprintf("%s%d", prefix, n)
		used := false
		for _, s := range d.sessions {
			if cleanPath(s.WorkDir) == cleanPath(d.target.workDir) && s.Name == name {
				used = true
				break
			}
		}
		if !used {
			return name
		}
	}
}

func (d launchDialog) updateName() launchDialog {
	if a, ok := d.Current(); ok && !d.nameTouched {
		d.name.SetValue(d.defaultName(a))
	}
	return d
}

// Quick launch starts in Task; forking reuses only accepted session settings.
func (m Model) openAgentTask(fork bool) (tea.Model, tea.Cmd) {
	var seed *agentsapi.Session
	initial := agents.AccountID("")
	if fork {
		wt, ok := m.selectedWorktree()
		if !ok {
			return m, nil
		}
		session, _, ok := m.selectedSession(cleanPath(wt.Path))
		if !ok {
			m.status = "agents: choose a session first"
			return m, nil
		}
		seed = &session
		initial = session.AccountID
	}
	nm, _ := m.openAgentLaunch()
	next := nm.(Model)
	if next.agents.overlay == nil || next.agents.overlay.launch == nil {
		return next, nil
	}
	d := *next.agents.overlay.launch
	d.Browser = accounts.NewBrowser(next.agents.api.Accounts, initial, next.agents.usage, true)
	d.accountPinned = initial != ""
	d.seed = seed
	if seed != nil {
		for _, target := range d.targets {
			if cleanPath(target.workDir) == cleanPath(seed.WorkDir) {
				d.target = target
				break
			}
		}
	}
	d = d.setFocus(launchFocusTask)
	next.setAgentOverlay(agentOverlay{launch: &d})
	return next, d.Init()
}
