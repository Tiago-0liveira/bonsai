// Package launchform is the generic editor for provider-declared fields
// (agents.LaunchField): the Defaults tab of the accounts modal edits profile
// fields with it and the new-agent dialog edits launch fields. It knows no
// provider; a field kind it does not understand is shown read-only so a
// provider adding one never breaks the TUI.
package launchform

import (
	"maps"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

const (
	modelKey = "model"
	// primaryFields is how many leading fields are the headline options; the
	// rest are provider-specific extras under their own sub-heading.
	primaryFields = 3
	maxListRows   = 8
	otherHeading  = "Other provider-specific options"
)

// choice is one entry of a dropdown, resolved from a field option, a model or
// a per-model effort.
type choice struct {
	value, label, desc, tone, help string
}

// Form edits a list of fields. It is a value type: Update returns the next
// Form and shares no mutable state with the previous one.
type Form struct {
	fields  []agents.LaunchField
	models  []agents.ModelOption
	initial map[string]string
	values  map[string]string
	cursor  int // index into visible()
	open    bool
	pick    int // highlighted choice of the open list
	ascii   bool
	blurred bool
}

// New builds a form over fields. values are the current settings keyed by
// field ID; the model, when the fields do not include a model field of their
// own, is read from values["model"] to resolve per-model options.
func New(fields []agents.LaunchField, models []agents.ModelOption, values map[string]string, ascii bool) Form {
	return Form{
		fields:  fields,
		models:  models,
		initial: maps.Clone(values),
		values:  maps.Clone(values),
		ascii:   ascii,
	}
}

// Focus shows or hides the focus highlight and decides whether keys are
// handled, so a parent can move focus to its own buttons.
func (f Form) Focus(on bool) Form {
	f.blurred = !on
	if !on {
		f.open = false
	}
	return f
}

// Editing reports that an inline list is open: the parent must not treat esc
// as "close" while it is.
func (f Form) Editing() bool { return f.open }

// AtFirst reports whether moving up would leave the form.
func (f Form) AtFirst() bool { return f.cursor == 0 }

// AtLast reports that the cursor is on the last shown field, so a parent can
// hand "down" to its own buttons.
func (f Form) AtLast() bool { return f.cursor >= len(f.visible())-1 }

// Dirty reports whether any value differs from the initial one.
func (f Form) Dirty() bool { return len(f.Changes()) > 0 }

// Values returns the current value of every shown field, defaults included
// (callers build launch options from it and expect them seeded), plus keys
// that are not fields, such as the model.
func (f Form) Values() map[string]string {
	out := maps.Clone(f.values)
	if out == nil {
		out = map[string]string{}
	}
	for _, fd := range f.fields {
		delete(out, fd.ID)
	}
	for _, i := range f.visible() {
		fd := f.fields[i]
		if v := f.effective(f.values, fd); v != "" {
			out[fd.ID] = v
		}
	}
	return out
}

// Changes lists the fields whose value differs from the initial one. A nil
// value means "back to the provider default".
func (f Form) Changes() map[string]*string {
	out := map[string]*string{}
	for _, fd := range f.fields {
		cur := f.effective(f.values, fd)
		if cur == f.effective(f.initial, fd) {
			continue
		}
		if _, set := f.values[fd.ID]; !set || cur == "" {
			out[fd.ID] = nil
			continue
		}
		out[fd.ID] = &cur
	}
	return out
}

// ResetAll puts every editable field back to the provider default.
func (f Form) ResetAll() Form {
	f.open = false
	for _, fd := range f.fields {
		if editable(fd) {
			f = f.reset(fd)
		}
	}
	return f.revalidate()
}

func editable(fd agents.LaunchField) bool {
	switch fd.Kind {
	case agents.LaunchFieldModel, agents.LaunchFieldSelect, agents.LaunchFieldToggle:
		return true
	}
	return false
}

// visible lists the indexes of the fields to show: a per-model field is hidden
// when the selected model has no such control.
func (f Form) visible() []int {
	noEffort := false
	if m := f.selectedModel(); m != nil {
		noEffort = m.NoEffort
	}
	var out []int
	for i, fd := range f.fields {
		if fd.PerModel && noEffort {
			continue
		}
		out = append(out, i)
	}
	return out
}

func (f Form) selectedModel() *agents.ModelOption {
	id := f.values[modelKey]
	for i := range f.models {
		m := &f.models[i]
		if (id != "" && m.ID == id) || (id == "" && m.Default) {
			return m
		}
	}
	return nil
}

// effective is the value a field shows for a values map: its own entry, else
// its default, else its first option.
func (f Form) effective(values map[string]string, fd agents.LaunchField) string {
	if v, ok := values[fd.ID]; ok {
		return v
	}
	if fd.Default != "" {
		return fd.Default
	}
	if fd.Kind != agents.LaunchFieldModel && len(fd.Options) > 0 {
		return fd.Options[0].Value
	}
	return ""
}

func (f Form) choices(fd agents.LaunchField) []choice {
	if fd.Kind == agents.LaunchFieldModel {
		label := agents.ProviderDefaultLabel
		for _, o := range fd.Options {
			if o.Value == "" && o.Label != "" {
				label = o.Label
				break
			}
		}
		out := []choice{{label: label}}
		for _, m := range f.models {
			label := m.Label
			if label == "" {
				label = m.ID
			}
			if m.Default {
				label += " (default)"
			}
			out = append(out, choice{value: m.ID, label: label, desc: m.Description})
		}
		return out
	}
	var out []choice
	if m := f.selectedModel(); fd.PerModel && m != nil && len(m.Efforts) > 0 {
		for _, o := range fd.Options {
			if o.Value == "" {
				out = append(out, optionChoice(o))
				break
			}
		}
		for _, e := range m.Efforts {
			label := effortLabel(fd, e)
			if e == m.DefaultEffort {
				label += " (default)"
			}
			out = append(out, choice{value: e, label: label})
		}
		return out
	}
	for _, o := range fd.Options {
		out = append(out, optionChoice(o))
	}
	return out
}

// effortLabel prefers the field's own option label so a per-model effort reads
// like the profile-level one ("High", not "high").
func effortLabel(fd agents.LaunchField, effort string) string {
	for _, o := range fd.Options {
		if o.Value == effort && o.Label != "" {
			return o.Label
		}
	}
	if effort == "" {
		return effort
	}
	return strings.ToUpper(effort[:1]) + effort[1:]
}

func optionChoice(o agents.FieldOption) choice {
	label := o.Label
	if label == "" {
		label = o.Value
	}
	return choice{value: o.Value, label: label, tone: o.Tone, help: o.Help}
}

func indexOf(cs []choice, value string) int {
	for i, c := range cs {
		if c.value == value {
			return i
		}
	}
	return -1
}

func (f Form) set(id, value string) Form {
	f.values = maps.Clone(f.values)
	if f.values == nil {
		f.values = map[string]string{}
	}
	f.values[id] = value
	return f
}

func (f Form) reset(fd agents.LaunchField) Form {
	f.values = maps.Clone(f.values)
	delete(f.values, fd.ID)
	return f
}

// apply stores a picked value and, for the model, re-validates what depends on it.
func (f Form) apply(fd agents.LaunchField, value string) Form {
	f = f.set(fd.ID, value)
	if fd.ID == modelKey {
		f = f.revalidate()
	}
	return f.clamp()
}

// revalidate drops per-model values the selected model does not accept,
// falling back to its default effort.
func (f Form) revalidate() Form {
	m := f.selectedModel()
	for _, fd := range f.fields {
		v, ok := f.values[fd.ID]
		if !fd.PerModel || !ok || v == "" {
			continue
		}
		switch {
		case m != nil && m.NoEffort:
			f = f.reset(fd)
		case indexOf(f.choices(fd), v) >= 0:
		case m != nil && indexOf(f.choices(fd), m.DefaultEffort) >= 0:
			f = f.set(fd.ID, m.DefaultEffort)
		default:
			f = f.reset(fd)
		}
	}
	return f.clamp()
}

func (f Form) clamp() Form {
	n := len(f.visible())
	if f.cursor >= n {
		f.cursor = max(n-1, 0)
	}
	return f
}

// Update handles keys; every other message is ignored.
func (f Form) Update(msg tea.Msg) (Form, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	vis := f.visible()
	if !ok || f.blurred || len(vis) == 0 {
		return f, nil
	}
	f = f.clamp()
	fd := f.fields[vis[f.cursor]]
	if f.open {
		return f.updateOpen(km, fd), nil
	}
	key := km.String()
	switch key {
	case "j", "down":
		f.cursor = min(f.cursor+1, len(vis)-1)
	case "k", "up":
		f.cursor = max(f.cursor-1, 0)
	case "enter":
		switch {
		case fd.Kind == agents.LaunchFieldToggle:
			f = f.flip(fd)
		case editable(fd) && len(f.choices(fd)) > 0:
			f.open = true
			f.pick = max(indexOf(f.choices(fd), f.effective(f.values, fd)), 0)
		}
	case " ":
		if fd.Kind == agents.LaunchFieldToggle {
			f = f.flip(fd)
		}
	case "left", "right":
		if editable(fd) {
			delta := 1
			if key == "left" {
				delta = -1
			}
			f = f.cycle(fd, delta)
		}
	case "r":
		if editable(fd) {
			f = f.reset(fd).revalidate()
		}
	}
	return f, nil
}

func (f Form) updateOpen(km tea.KeyMsg, fd agents.LaunchField) Form {
	cs := f.choices(fd)
	switch km.String() {
	case "j", "down":
		f.pick = min(f.pick+1, len(cs)-1)
	case "k", "up":
		f.pick = max(f.pick-1, 0)
	case "enter", "tab":
		f.open = false
		if f.pick < len(cs) {
			f = f.apply(fd, cs[f.pick].value)
		}
	case "esc":
		f.open = false
	}
	return f
}

func (f Form) flip(fd agents.LaunchField) Form {
	if f.effective(f.values, fd) == "true" {
		return f.apply(fd, "false")
	}
	return f.apply(fd, "true")
}

func (f Form) cycle(fd agents.LaunchField, delta int) Form {
	if fd.Kind == agents.LaunchFieldToggle {
		return f.flip(fd)
	}
	cs := f.choices(fd)
	if len(cs) == 0 {
		return f
	}
	i := indexOf(cs, f.effective(f.values, fd))
	if i < 0 {
		i = 0
		if delta < 0 {
			i = len(cs) - 1
		}
	} else {
		i = (i + delta + len(cs)) % len(cs)
	}
	return f.apply(fd, cs[i].value)
}

type styles struct {
	label, focusLabel, dim, accent, danger, heading lipgloss.Style
	border                                          lipgloss.Style
}

func newStyles() styles {
	p := theme.Current
	return styles{
		label:      lipgloss.NewStyle().Foreground(p.Text),
		focusLabel: lipgloss.NewStyle().Foreground(p.Accent).Bold(true),
		dim:        lipgloss.NewStyle().Foreground(p.Dim),
		accent:     lipgloss.NewStyle().Foreground(p.Accent).Bold(true),
		danger:     lipgloss.NewStyle().Foreground(p.Danger),
		heading:    lipgloss.NewStyle().Foreground(p.Dim).Bold(true),
		border:     lipgloss.NewStyle().Foreground(p.Accent),
	}
}

// View renders exactly h lines of at most w cells, scrolled so the focused
// field (and its open list) is visible.
func (f Form) View(w, h int) string {
	if h <= 0 {
		return ""
	}
	lines, from, to := f.render(max(w, 1), newStyles())
	top := 0
	if to > h {
		top = to - h
	}
	if from < top {
		top = from
	}
	out := make([]string, h)
	for i := range out {
		line := ""
		if top+i < len(lines) {
			line = lines[top+i]
		}
		out[i] = fit(line, w)
	}
	return strings.Join(out, "\n")
}

// fit truncates or pads s to exactly w cells.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) > w {
		s = ansi.Truncate(s, w, "")
	}
	if pad := w - ansi.StringWidth(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

// render lays out every shown field and returns the line range [from, to) of
// the focused one.
func (f Form) render(w int, st styles) (lines []string, from, to int) {
	vis := f.visible()
	// Every value box is as wide as the widest value, not the pane, so the
	// closing bracket stays next to the text.
	boxW := 0
	for _, i := range vis {
		boxW = max(boxW, f.valueTextWidth(f.fields[i]))
	}

	headed := false
	for n, i := range vis {
		fd := f.fields[i]
		focused := n == f.cursor && !f.blurred
		start := len(lines)
		if i >= primaryFields && !headed {
			headed = true
			lines = append(lines, st.heading.Render(otherHeading))
			if focused {
				start = len(lines) - 1
			}
		}
		lines = append(lines, f.renderField(fd, w, st, focused)...)
		if focused {
			from, to = start, len(lines)
		}
		lines = append(lines, "")
	}
	return lines, from, to
}

func (f Form) renderField(fd agents.LaunchField, w int, st styles, focused bool) []string {
	label := st.label
	if focused {
		label = st.focusLabel
	}
	lines := []string{label.Render(ansi.Truncate(fd.Label, w, "…"))}

	cur := f.effective(f.values, fd)
	cs := f.choices(fd)
	ci := indexOf(cs, cur)
	tone, help := "", ""
	text, marker := "", ""
	switch {
	case fd.Kind == agents.LaunchFieldToggle:
		text, marker = "Off", "[space]"
		if cur == "true" {
			text = "On"
		}
		if ci >= 0 {
			tone, help = cs[ci].tone, cs[ci].help
		}
	case editable(fd):
		text, marker = cur, "["+f.arrow()+"]"
		if ci >= 0 {
			text, tone, help = cs[ci].label, cs[ci].tone, cs[ci].help
		} else if cur == "" {
			text = agents.ProviderDefaultLabel
		}
	default:
		text = cur
		if text == "" {
			text = "-"
		}
	}

	row := f.valueRow(text, marker, w)
	switch {
	case !editable(fd):
		row = st.dim.Render(row)
	case focused:
		style := lipgloss.NewStyle().Reverse(true)
		if tone == agents.ToneDanger {
			style = style.Foreground(theme.Current.Danger)
		}
		row = style.Render(row)
	case tone == agents.ToneDanger:
		row = st.danger.Render(row)
	}
	lines = append(lines, row)

	if focused && f.open {
		lines = append(lines, f.renderList(cs, w, st)...)
		if c := cs[min(f.pick, len(cs)-1)]; c.tone == agents.ToneDanger && c.help != "" {
			lines = append(lines, wrapIndented(c.help, w, st.danger)...)
		}
		return lines
	}
	if focused {
		switch {
		case tone == agents.ToneDanger && help != "":
			lines = append(lines, wrapIndented(help, w, st.danger)...)
		case fd.Help != "":
			lines = append(lines, wrapIndented(fd.Help, w, st.dim)...)
		}
	}
	return lines
}

func (f Form) arrow() string {
	if f.ascii {
		return "v"
	}
	return "▼"
}

// valueRow is "> value" with the marker right-aligned, within w cells.
func (f Form) valueRow(text, marker string, w int) string {
	mw := ansi.StringWidth(marker)
	if marker != "" && mw+4 > w {
		marker, mw = "", 0
	}
	left := ansi.Truncate("> "+text, max(w-mw-1, 1), "…")
	gap := max(w-ansi.StringWidth(left)-mw, 0)
	return left + strings.Repeat(" ", gap) + marker
}

func wrapIndented(s string, w int, st lipgloss.Style) []string {
	var out []string
	for _, line := range strings.Split(ansi.Wrap(s, max(w-2, 1), ""), "\n") {
		out = append(out, st.Render("  "+line))
	}
	return out
}

// renderList draws the bordered inline dropdown under the value row.
func (f Form) renderList(cs []choice, w int, st styles) []string {
	const indent = 2
	inner := max(w-indent-2, 1)
	rows := min(len(cs), maxListRows)
	start := max(0, min(f.pick-rows/2, len(cs)-rows))
	b := lipgloss.RoundedBorder()
	pad := strings.Repeat(" ", indent)
	edge := func(l, r string) string {
		return pad + st.border.Render(l+strings.Repeat(b.Top, inner)+r)
	}
	out := []string{edge(b.TopLeft, b.TopRight)}
	for i := start; i < start+rows; i++ {
		out = append(out, pad+st.border.Render(b.Left)+f.listRow(cs[i], i == f.pick, inner, st)+st.border.Render(b.Right))
	}
	return append(out, edge(b.BottomLeft, b.BottomRight))
}

func (f Form) listRow(c choice, picked bool, inner int, st styles) string {
	mark := "  "
	if picked {
		mark = "> "
	}
	avail := max(inner-ansi.StringWidth(mark), 0)
	label := ansi.Truncate(c.label, avail, "…")
	desc := ""
	if rest := avail - ansi.StringWidth(label) - 2; c.desc != "" && rest > 0 {
		desc = "  " + ansi.Truncate(c.desc, rest, "…")
	}
	ls := st.label
	switch {
	case c.tone == agents.ToneDanger:
		ls = st.danger
	case picked:
		ls = st.accent
	}
	row := st.accent.Render(mark) + ls.Render(label) + st.dim.Render(desc)
	return fit(row, inner)
}

// CompactLines lays the fields out as one "label  [ value  v ]" row each, with
// ">" on the focused one, and returns the line range [from, to) that must stay
// visible when the caller scrolls. Lines are at most w cells wide.
func (f Form) CompactLines(w int) (lines []string, from, to int) {
	st := newStyles()
	vis := f.visible()
	labelW := f.CompactLabelWidth(w)

	// Every value box is as wide as the widest value, not the pane, so the
	// closing bracket stays next to the text.
	boxW := 0
	for _, i := range vis {
		boxW = max(boxW, f.valueTextWidth(f.fields[i]))
	}

	headed := false
	for n, i := range vis {
		fd := f.fields[i]
		focused := n == f.cursor && !f.blurred
		start := len(lines)
		if i >= primaryFields && !headed {
			headed = true
			lines = append(lines, st.heading.Render(ansi.Truncate(otherHeading, w, "…")))
			if focused {
				start = len(lines) - 1
			}
		}
		lines = append(lines, f.compactField(fd, w, labelW, boxW, st, focused)...)
		if focused {
			from, to = start, len(lines)
		}
	}
	return lines, from, to
}

// CompactLabelWidth is the label column of CompactLines at width w, so a row
// the caller draws above the fields can line up with them.
func (f Form) CompactLabelWidth(w int) int {
	labelW := 0
	for _, i := range f.visible() {
		labelW = max(labelW, ansi.StringWidth(f.fields[i].Label))
	}
	// Labels yield first when the row is narrow; the value keeps at least a
	// readable cell.
	return max(min(labelW, w/3), 4)
}

func (f Form) compactField(fd agents.LaunchField, w, labelW, boxW int, st styles, focused bool) []string {
	cur := f.effective(f.values, fd)
	cs := f.choices(fd)
	ci := indexOf(cs, cur)
	tone, help := "", ""
	text, chevron := cur, " "
	switch {
	case fd.Kind == agents.LaunchFieldToggle:
		text = "Off"
		if cur == "true" {
			text = "On"
		}
		if ci >= 0 {
			tone, help = cs[ci].tone, cs[ci].help
		}
	case editable(fd):
		chevron = f.compactArrow()
		if ci >= 0 {
			text, tone, help = cs[ci].label, cs[ci].tone, cs[ci].help
		} else if cur == "" {
			text = agents.ProviderDefaultLabel
		}
	case text == "":
		text = "-"
	}

	marker, label := "  ", st.label
	if focused {
		marker, label = st.accent.Render("> "), st.focusLabel
	}
	name := ansi.Truncate(fd.Label, labelW, "…")
	head := marker + label.Render(name) + strings.Repeat(" ", labelW-ansi.StringWidth(name)+2)

	// "[ " + value + " " + chevron + " ]" around the value.
	valueW := min(w-ansi.StringWidth(head)-6, boxW)
	var control string
	switch {
	case !editable(fd):
		control = st.dim.Render(ansi.Truncate(text, max(w-ansi.StringWidth(head), 1), "…"))
	case valueW < 3:
		control = ansi.Truncate(text, max(w-ansi.StringWidth(head), 1), "…")
	default:
		body := "[ " + fit(ansi.Truncate(text, valueW, "…"), valueW) + " " + chevron + " ]"
		switch {
		case focused:
			style := lipgloss.NewStyle().Reverse(true)
			if tone == agents.ToneDanger {
				style = style.Foreground(theme.Current.Danger)
			}
			control = style.Render(body)
		case tone == agents.ToneDanger:
			control = st.danger.Render(body)
		default:
			control = body
		}
	}
	lines := []string{head + control}

	if focused && f.open {
		lines = append(lines, f.renderList(cs, w, st)...)
		if c := cs[min(f.pick, len(cs)-1)]; c.tone == agents.ToneDanger && c.help != "" {
			lines = append(lines, wrapIndented(c.help, w, st.danger)...)
		}
		return lines
	}
	if focused {
		switch {
		case tone == agents.ToneDanger && help != "":
			lines = append(lines, wrapIndented(help, w, st.danger)...)
		case fd.Help != "":
			lines = append(lines, wrapIndented(fd.Help, w, st.dim)...)
		}
	}
	return lines
}

// valueTextWidth is the widest text a compact row of fd can show: the current
// value and every choice of its dropdown.
func (f Form) valueTextWidth(fd agents.LaunchField) int {
	cur := f.effective(f.values, fd)
	w := ansi.StringWidth(cur)
	switch {
	case fd.Kind == agents.LaunchFieldToggle:
		w = 3 // "Off"
	case cur == "":
		w = max(w, ansi.StringWidth(agents.ProviderDefaultLabel))
	}
	for _, c := range f.choices(fd) {
		w = max(w, ansi.StringWidth(c.label))
	}
	return max(w, 1)
}

// compactArrow is the quiet dropdown chevron of the compact rows.
func (f Form) compactArrow() string {
	if f.ascii {
		return "v"
	}
	return "▾"
}
