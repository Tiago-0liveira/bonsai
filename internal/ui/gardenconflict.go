package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/spec"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

type conflictPane int

const (
	paneBase conflictPane = iota
	paneYours
	paneRemote
	paneResult
	paneCount
)

func (p conflictPane) String() string { return [...]string{"Base", "Yours", "Remote", "Result"}[p] }

// secStatus is how one section differs between Yours and Remote, relative to Base.
type secStatus int

const (
	secSame   secStatus = iota // both sides agree
	secYours                   // only you changed it
	secRemote                  // only the remote side changed it
	secBoth                    // both changed it differently: needs an explicit choice
)

// conflictState is the Base / Yours / Remote / Result comparison. The Result is built
// section by section; nothing is published until every section changed on both sides
// has been chosen or edited, and then only after a preview.
type conflictState struct {
	view   gardenapi.ConflictView
	issue  garden.IssueID
	number int64
	title  string
	note   string

	titles []string
	base   spec.Spec
	yours  spec.Spec
	remote []spec.Spec // shown remote heads, oldest first
	shown  []gardenapi.RemoteRevision
	ri     int // selected remote head

	result   []string
	resolved []bool
	status   []secStatus

	intro   bool
	sel     int
	pane    conflictPane
	scroll  int
	editing bool
	ta      textarea.Model
	notice  string
}

func lookup(s spec.Spec, title string) string {
	for i, t := range s.Titles() {
		if strings.EqualFold(t, title) {
			return garden.NormalizeContent(s.Text(i))
		}
	}
	return ""
}

func newConflictState(v gardenapi.ConflictView, yours string, number int64, title, note string) *conflictState {
	c := &conflictState{view: v, issue: garden.IssueID(v.IssueID), number: number, title: title, note: note, intro: true}
	c.base, c.yours = spec.Parse(v.Base.Content), spec.Parse(yours)
	for _, r := range v.Remote {
		// After a lost race our own head is among the remote ones; it is Yours, not a competitor.
		if v.Saved && r.CommentID == v.SavedCommentID {
			continue
		}
		c.shown = append(c.shown, r)
		c.remote = append(c.remote, spec.Parse(r.Content))
	}
	if len(c.shown) == 0 {
		c.shown, c.remote = v.Remote, []spec.Spec{{}}
		if len(v.Remote) > 0 {
			c.remote = []spec.Spec{spec.Parse(v.Remote[0].Content)}
		}
	}
	c.ri = len(c.remote) - 1
	seen := map[string]bool{}
	add := func(t string) {
		if k := strings.ToLower(t); !seen[k] {
			seen[k] = true
			c.titles = append(c.titles, t)
		}
	}
	for _, n := range spec.Names {
		add(n)
	}
	for _, s := range append([]spec.Spec{c.base, c.yours}, c.remote...) {
		for _, t := range s.Titles()[spec.FixedCount:] {
			add(t)
		}
	}
	c.result = make([]string, len(c.titles))
	c.resolved = make([]bool, len(c.titles))
	c.status = make([]secStatus, len(c.titles))
	latest := c.remote[len(c.remote)-1]
	for i, t := range c.titles {
		b, y, r := lookup(c.base, t), lookup(c.yours, t), lookup(latest, t)
		switch {
		case y == r:
			c.status[i], c.result[i], c.resolved[i] = secSame, y, true
		case y == b:
			c.status[i], c.result[i], c.resolved[i] = secRemote, r, true
		case r == b:
			c.status[i], c.result[i], c.resolved[i] = secYours, y, true
		default:
			// Remote text is the safe default: nothing of theirs is lost, and it must still be confirmed.
			c.status[i], c.result[i] = secBoth, r
		}
	}
	return c
}

func (c *conflictState) unresolved() int {
	n := 0
	for i, s := range c.status {
		if s == secBoth && !c.resolved[i] {
			n++
		}
	}
	return n
}

func (c *conflictState) texts(pane conflictPane, i int) string {
	t := c.titles[i]
	switch pane {
	case paneBase:
		return lookup(c.base, t)
	case paneYours:
		return lookup(c.yours, t)
	case paneRemote:
		return lookup(c.remote[c.ri], t)
	}
	return c.result[i]
}

// resultContent is the canonical text of the Result pane.
func (c *conflictState) resultContent() string {
	var s spec.Spec
	for i, t := range c.titles {
		if i < spec.FixedCount {
			*s.Field(i) = c.result[i]
			continue
		}
		s.Extra = append(s.Extra, spec.Section{Title: t, Text: c.result[i]})
	}
	return spec.Render(s)
}

func (m Model) gardenOpenConflict(v gardenapi.ConflictView, yours string, number int64, title, note string) Model {
	d := &m.garden.drafts
	d.conflict = newConflictState(v, yours, number, title, note)
	d.stage = draftsConflict
	d.savePreview, d.saveView = nil, nil
	m.shellFocus = shellBody
	return m
}

func (m Model) gardenConflictKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	g := &m.garden
	d := &g.drafts
	c := d.conflict
	k := msg.String()
	if c == nil {
		d.stage = draftsDetail
		return m, nil
	}
	if c.editing {
		switch k {
		case "esc":
			c.result[c.sel] = garden.NormalizeContent(c.ta.Value())
			c.resolved[c.sel] = true
			c.editing = false
			m.shellFocus = shellBody
			return m, nil
		}
		var cmd tea.Cmd
		c.ta, cmd = c.ta.Update(msg)
		return m, cmd
	}
	c.notice = ""
	g.err = nil
	if c.intro {
		switch k {
		case "enter":
			c.intro = false
		case "esc":
			return m.gardenLeaveConflict(), nil
		}
		return m, nil
	}
	switch k {
	case "esc":
		return m.gardenLeaveConflict(), nil
	case "up", "k":
		c.sel, c.scroll = max(0, c.sel-1), 0
	case "down", "j":
		c.sel, c.scroll = min(len(c.titles)-1, c.sel+1), 0
	case "left", "h":
		c.pane = (c.pane + paneCount - 1) % paneCount
		c.scroll = 0
	case "right", "l", "tab":
		c.pane = (c.pane + 1) % paneCount
		c.scroll = 0
	case "shift+tab":
		c.pane = (c.pane + paneCount - 1) % paneCount
		c.scroll = 0
	case "1", "2", "3", "4":
		c.pane, c.scroll = conflictPane(k[0]-'1'), 0
	case "[":
		c.ri = (c.ri + len(c.remote) - 1) % len(c.remote)
	case "]":
		c.ri = (c.ri + 1) % len(c.remote)
	case "pgdown":
		c.scroll += max(1, m.height-14)
	case "pgup":
		c.scroll = max(0, c.scroll-max(1, m.height-14))
	case "y", "m", "b":
		src := map[string]conflictPane{"y": paneYours, "m": paneRemote, "b": paneBase}[k]
		c.result[c.sel], c.resolved[c.sel] = c.texts(src, c.sel), true
	case "e":
		c.ta = newTextarea()
		c.ta.SetValue(c.result[c.sel])
		textareaTop(&c.ta)
		c.ta.Focus()
		c.editing = true
		m.shellFocus = shellInput
		m.gardenSizeConflict()
	case "p", "ctrl+s":
		if ok, why := m.canEdit(); !ok {
			c.notice = why
			return m, nil
		}
		if n := c.unresolved(); n > 0 {
			c.notice = fmt.Sprintf("%d section%s changed on both sides: choose yours (y), remote (m), base (b) or edit the result (e) first.", n, plural(n))
			return m, nil
		}
		return m.gardenPreviewResolve(gardenapi.ResolveRequest{IssueID: uint64(c.issue), Conflict: c.view, Result: c.resultContent()})
	}
	return m, nil
}

func (m *Model) gardenSizeConflict() {
	c := m.garden.drafts.conflict
	if c == nil {
		return
	}
	w, h := m.gardenEditorText()
	c.ta.SetWidth(w)
	c.ta.SetHeight(h)
}

// gardenLeaveConflict returns to the editor with the text exactly as typed, or to the document.
func (m Model) gardenLeaveConflict() Model {
	d := &m.garden.drafts
	d.conflict = nil
	if d.edit != nil {
		d.stage = draftsEditor
		m.shellFocus = shellInput
		m.gardenSizeEditor()
		return m
	}
	d.stage = draftsDetail
	return m
}
