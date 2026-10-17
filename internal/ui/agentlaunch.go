package ui

import (
	"context"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/accounts"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/launchform"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// launchCallTimeout bounds the read-only backend calls of the dialog so a hung
// provider cannot leave it loading forever. Start has no timeout of its own:
// the user can always Esc out of a start that is taking long.
const launchCallTimeout = 15 * time.Second

const launchNameLen = 60

type launchFocus int

const (
	launchFocusAccount launchFocus = iota
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
	launchListMsg struct {
		dlg      uint64
		accounts []agentsapi.Account
		err      error
	}
	launchUsageMsg struct {
		dlg   uint64
		id    agents.AccountID
		usage agentsapi.Usage
		err   error
	}
	launchDescribeMsg struct {
		dlg  uint64
		id   agents.AccountID
		info agents.AccountInfo
		err  error
	}
	launchDefaultsMsg struct {
		dlg uint64
		seq int
		id  agents.AccountID
		d   agentsapi.Defaults
		err error
	}
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
// modal; backend calls run as commands and carry the dialog ID and a sequence
// so an answer for a closed dialog or a left-behind account is dropped.
type launchDialog struct {
	id     uint64
	api    agentsapi.Accounts
	ascii  bool
	target launchTarget
	now    func() time.Time
	w, h   int

	accounts []agentsapi.Account
	listed   bool
	listErr  string
	selected agents.AccountID
	initial  agents.AccountID

	usage     map[agents.AccountID]agentsapi.Usage
	described map[agents.AccountID]bool

	focus       launchFocus
	name        textinput.Model
	nameTouched bool

	defaults   agentsapi.Defaults
	form       launchform.Form
	formLoaded bool
	formErr    string
	defSeq     int
	save       bool
	button     int // 0 Cancel, 1 Start / Sign in
	starting   bool
	err        string
}

func newLaunchDialog(id uint64, api agentsapi.Accounts, target launchTarget, initial agents.AccountID, usage map[agents.AccountID]agentsapi.Usage, ascii bool) launchDialog {
	ti := textinput.New()
	ti.Prompt = "> "
	ti.CharLimit = launchNameLen
	// A steady cursor needs no blink ticks fed back through Update.
	ti.Cursor.SetMode(cursor.CursorStatic)
	seeded := make(map[agents.AccountID]agentsapi.Usage, len(usage))
	for k, v := range usage {
		seeded[k] = v
	}
	return launchDialog{
		id: id, api: api, ascii: ascii, target: target, initial: initial,
		now: time.Now, usage: seeded, described: map[agents.AccountID]bool{}, name: ti, button: 1,
	}
}

func (d *launchDialog) SetSize(w, h int) { d.w, d.h = w, h }

func (d launchDialog) Init() tea.Cmd { return d.listCmd() }

// Capturing reports that a text input or dropdown has the keys.
func (d launchDialog) Capturing() bool {
	return d.focus == launchFocusName || (d.focus == launchFocusForm && d.form.Editing())
}

func launchCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), launchCallTimeout)
}

func (d launchDialog) listCmd() tea.Cmd {
	api, id := d.api, d.id
	return func() tea.Msg {
		ctx, cancel := launchCtx()
		defer cancel()
		list, err := api.List(ctx)
		return launchListMsg{dlg: id, accounts: list, err: err}
	}
}

func (d launchDialog) usageCmd(acct agents.AccountID) tea.Cmd {
	api, id := d.api, d.id
	return func() tea.Msg {
		ctx, cancel := launchCtx()
		defer cancel()
		u, err := api.Usage(ctx, acct, false)
		return launchUsageMsg{dlg: id, id: acct, usage: u, err: err}
	}
}

func (d launchDialog) describeCmd(acct agents.AccountID) tea.Cmd {
	api, id := d.api, d.id
	return func() tea.Msg {
		ctx, cancel := launchCtx()
		defer cancel()
		info, err := api.Describe(ctx, acct)
		return launchDescribeMsg{dlg: id, id: acct, info: info, err: err}
	}
}

func (d launchDialog) defaultsCmd(acct agents.AccountID) tea.Cmd {
	api, id, seq := d.api, d.id, d.defSeq
	return func() tea.Msg {
		ctx, cancel := launchCtx()
		defer cancel()
		def, err := api.Defaults(ctx, acct)
		return launchDefaultsMsg{dlg: id, seq: seq, id: acct, d: def, err: err}
	}
}

func emitMsg(m tea.Msg) tea.Cmd { return func() tea.Msg { return m } }

// sidebar renders the shared account list; the same value orders j/k.
func (d launchDialog) sidebar() accounts.Sidebar {
	busy := map[agents.AccountID]bool{}
	for _, a := range d.accounts {
		// Only a hint: admission decides whether the account is really in use.
		busy[a.ID] = a.Running > 0 && !a.ConcurrentSameAccount
	}
	return accounts.Sidebar{
		Accounts: d.accounts, Selected: d.selected, Usage: d.usage, Busy: busy,
		ASCII: d.ascii, HideAdd: true, Focused: d.focus == launchFocusAccount,
	}
}

func (d launchDialog) current() (agentsapi.Account, bool) {
	for _, a := range d.accounts {
		if a.ID == d.selected {
			return a, true
		}
	}
	return agentsapi.Account{}, false
}

// Update handles keys and results.
func (d launchDialog) Update(msg tea.Msg) (launchDialog, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return d.onKey(msg)
	case accounts.ReloadMsg:
		// A sign-in ran in the terminal: re-read the accounts and their status.
		d.described = map[agents.AccountID]bool{}
		return d, d.listCmd()
	case launchListMsg:
		return d.onList(msg)
	case launchUsageMsg:
		if msg.dlg == d.id && msg.err == nil {
			d.usage[msg.id] = msg.usage
		}
	case launchDescribeMsg:
		if msg.dlg == d.id && msg.err == nil {
			d.accounts = append([]agentsapi.Account(nil), d.accounts...)
			for i := range d.accounts {
				if d.accounts[i].ID == msg.id {
					d.accounts[i].Info = msg.info
				}
			}
		}
	case launchDefaultsMsg:
		return d.onDefaults(msg), nil
	}
	return d, nil
}

func (d launchDialog) onList(msg launchListMsg) (launchDialog, tea.Cmd) {
	if msg.dlg != d.id {
		return d, nil
	}
	d.listed = true
	if msg.err != nil {
		d.listErr = sanitizeAgentText(msg.err.Error())
		return d, nil
	}
	d.listErr = ""
	d.accounts = msg.accounts
	ordered := d.sidebar().Ordered()
	pick := agents.AccountID("")
	has := func(id agents.AccountID) bool {
		for _, a := range ordered {
			if a.ID == id {
				return id != ""
			}
		}
		return false
	}
	switch {
	case has(d.selected):
		pick = d.selected
	case has(d.initial):
		pick = d.initial
	case len(ordered) > 0:
		pick = ordered[0].ID
	}
	var cmds []tea.Cmd
	for _, a := range ordered {
		if _, ok := d.usage[a.ID]; !ok {
			cmds = append(cmds, d.usageCmd(a.ID))
		}
	}
	if pick == "" {
		d.selected, d.formLoaded = "", false
		return d, tea.Batch(cmds...)
	}
	// Re-selecting after a reload re-reads the form: a sign-in changes nothing
	// in it, but a defaults edit made in the accounts modal would.
	nd, c := d.selectAccount(pick)
	return nd, tea.Batch(append(cmds, c)...)
}

// selectAccount switches the right column to an account and reloads its form.
func (d launchDialog) selectAccount(id agents.AccountID) (launchDialog, tea.Cmd) {
	d.selected = id
	d.formLoaded, d.formErr, d.save, d.err = false, "", false, ""
	d.defSeq++
	a, _ := d.current()
	if !d.nameTouched {
		d.name.SetValue(a.Name)
	}
	cmds := []tea.Cmd{d.defaultsCmd(id)}
	if !d.described[id] {
		d.described = copyBoolMap(d.described)
		d.described[id] = true
		cmds = append(cmds, d.describeCmd(id))
	}
	return d, tea.Batch(cmds...)
}

func copyBoolMap(m map[agents.AccountID]bool) map[agents.AccountID]bool {
	out := make(map[agents.AccountID]bool, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (d launchDialog) onDefaults(msg launchDefaultsMsg) launchDialog {
	if msg.dlg != d.id || msg.seq != d.defSeq || msg.id != d.selected {
		return d
	}
	if msg.err != nil {
		d.formErr = sanitizeAgentText(msg.err.Error())
		return d
	}
	d.formErr = ""
	d.defaults = msg.d
	// Changes here apply to this launch only: the provider's own launch fields
	// seeded from the account's defaults.
	d.form = launchform.New(launchDialogFields(msg.d.LaunchFields), msg.d.Models, msg.d.Values, d.ascii).Focus(d.focus == launchFocusForm)
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

func (d launchDialog) setFocus(f launchFocus) launchDialog {
	d.focus = (f + launchFocusCount) % launchFocusCount
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
		return d.submit()
	}
	if d.starting {
		if s == "esc" {
			return d, close
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
	case launchFocusName:
		if s == "enter" || s == "down" {
			return d.setFocus(launchFocusForm), nil
		}
		if s == "up" {
			return d.setFocus(launchFocusAccount), nil
		}
		d.nameTouched = true
		var cmd tea.Cmd
		d.name, cmd = d.name.Update(k)
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
		return d.setFocus(launchFocusName), nil
	}
	ordered := d.sidebar().Ordered()
	if delta == 0 || len(ordered) == 0 {
		return d, nil
	}
	i := 0
	for n, a := range ordered {
		if a.ID == d.selected {
			i = n
		}
	}
	next := ordered[min(max(i+delta, 0), len(ordered)-1)].ID
	if next == d.selected {
		return d, nil
	}
	return d.selectAccount(next)
}

// submit starts the agent, or opens the sign-in flow for an account that has
// no login.
func (d launchDialog) submit() (launchDialog, tea.Cmd) {
	if d.starting {
		return d, nil
	}
	a, ok := d.current()
	if !ok {
		d.err = "Choose an account first."
		return d, nil
	}
	if accounts.NeedsSignIn(a.Info) {
		if c := d.api.LoginCommand(a.ID); c != nil {
			return d, emitMsg(accounts.ExecMsg{Cmd: c})
		}
		return d, nil
	}
	if !d.formLoaded {
		d.err = "Options are still loading."
		return d, nil
	}
	name := strings.TrimSpace(d.name.Value())
	if name == "" {
		name = a.Name
	}
	values := d.form.Values()
	req := agentsapi.StartRequest{
		WorktreeID: d.target.worktree,
		WorkDir:    d.target.workDir,
		AccountID:  a.ID,
		Name:       name,
		Launch:     agents.LaunchOptionsFrom(d.defaults.LaunchFields, values["model"], values),
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

// --- view ---

func (d launchDialog) View() string {
	w, h := accounts.FrameSize(d.w, d.h)
	if w == 0 || h == 0 {
		return ""
	}
	leftW := accounts.LeftWidth(w)
	rw := accounts.RightWidth(w)
	rows := accounts.BodyRows(h)
	title := "New agent · " + d.target.branch
	if accounts.Compact(w) {
		body := []string{d.sidebar().Selector(d.focus == launchFocusAccount), ""}
		body = append(body, d.right(accounts.ContentWidth(w), max(rows-2, 0))...)
		return accounts.FrameSingle(title, body, d.footer(), w, h)
	}
	return accounts.Frame(title, d.sidebar().View(leftW, rows), d.right(rw, rows), d.footer(), w, h)
}

func (d launchDialog) footer() string {
	switch d.focus {
	case launchFocusAccount:
		return "↑↓ Profile · Tab Options · ^S Start · Esc"
	case launchFocusForm:
		return "↑↓ Field · ←→ Value · ^S Start · Esc"
	default:
		return "Tab Next · Enter Select · ^S Start · Esc"
	}
}

func (d launchDialog) right(rw, rows int) []string {
	th := theme.Current
	dim := lipgloss.NewStyle().Foreground(th.Dim)
	danger := lipgloss.NewStyle().Foreground(th.Danger)
	switch {
	case !d.listed:
		return []string{dim.Render("Loading accounts…")}
	case d.listErr != "":
		return []string{danger.Render("Couldn't load accounts"), dim.Render(d.listErr)}
	}
	a, ok := d.current()
	if !ok {
		return []string{dim.Render("No accounts yet."), dim.Render("Esc, then m to add one.")}
	}

	head := theme.ProviderStyle(a.Provider).Render(theme.ProviderGlyph(a.Provider, d.ascii)) + " " + providerName(a.Provider) + " · " + a.Name
	usage := dim.Render("usage loading…")
	if u, ok := d.usage[a.ID]; ok {
		if text := accounts.Headline(u.Headline, d.now(), d.ascii, false); text != "" {
			usage = text
			if u.Stale {
				usage = dim.Render(ansi.Strip(text) + " (stale)")
			}
		} else {
			usage = dim.Render("no usage data")
		}
	}
	label := func(text string, on bool) string {
		if on {
			return lipgloss.NewStyle().Bold(true).Foreground(th.Accent).Render(text)
		}
		return lipgloss.NewStyle().Foreground(th.Text).Render(text)
	}
	d.name.Width = max(rw-4, 1)
	lines := []string{head, usage, dim.Render(strings.Repeat("─", rw)), "",
		label("Name", d.focus == launchFocusName), d.name.View(), ""}
	if accounts.Compact(d.w) {
		lines = []string{label("Name", d.focus == launchFocusName) + " " + d.name.View()}
	}

	scope := "This agent only"
	if d.save {
		scope = "Also saves profile defaults"
	}
	tail := []string{dim.Render(scope), d.saveRow()}
	if d.err != "" {
		errLines := strings.Split(ansi.Wrap(d.err, max(rw, 1), ""), "\n")
		for _, l := range errLines[:min(len(errLines), 2)] {
			tail = append(tail, danger.Render(l))
		}
	}
	tail = append(tail, "")
	tail = append(tail, d.buttonsRow(a, rw))

	formRows := max(rows-len(lines)-len(tail), 1)
	switch {
	case d.formErr != "":
		lines = append(lines, danger.Render("Couldn't load options"), dim.Render(d.formErr))
	case !d.formLoaded:
		lines = append(lines, dim.Render("Loading options…"))
	default:
		lines = append(lines, strings.Split(d.form.View(rw, formRows), "\n")...)
	}
	for len(lines) < rows-len(tail) {
		lines = append(lines, "")
	}
	return append(lines[:max(rows-len(tail), 0)], tail...)
}

func (d launchDialog) saveRow() string {
	box := "[ ]"
	if d.save {
		box = "[x]"
	}
	row := box + " Save profile defaults"
	if d.focus == launchFocusSave {
		return lipgloss.NewStyle().Bold(true).Foreground(theme.Current.Accent).Render(row)
	}
	return row
}

func (d launchDialog) buttonsRow(a agentsapi.Account, rw int) string {
	focused := d.focus == launchFocusButtons
	start := "Start"
	disabled := false
	switch {
	case d.starting:
		start, disabled = "Starting…", true
	case accounts.NeedsSignIn(a.Info):
		start = "Sign in first"
	}
	btn := func(label string, on, off bool) string {
		st := lipgloss.NewStyle()
		switch {
		case on:
			st = st.Reverse(true).Bold(true)
		case off:
			st = st.Faint(true)
		}
		return st.Render("[" + label + "]")
	}
	row := btn("Cancel", focused && d.button == 0, false) + "  " + btn(start, focused && d.button == 1, disabled)
	pad := max(rw-ansi.StringWidth(row), 0)
	return strings.Repeat(" ", pad) + row
}
