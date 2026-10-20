package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/spec"
	gardensync "github.com/Tiago-0liveira/bonsai/internal/core/garden/sync"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// newTextarea builds a multi-line input drawn in the theme. The style is set
// before Focus/Blur because the widget points at its own style fields.
func newTextarea() textarea.Model {
	p := theme.Current
	base := lipgloss.NewStyle().Background(p.Background).Foreground(p.Text)
	st := textarea.Style{Base: base, CursorLine: base, CursorLineNumber: base, EndOfBuffer: base, LineNumber: base,
		Placeholder: base.Foreground(p.Muted), Prompt: base, Text: base}
	t := textarea.New()
	t.FocusedStyle, t.BlurredStyle = st, st
	t.Cursor.SetMode(cursor.CursorStatic) // keys only, so no blink messages
	t.ShowLineNumbers = false
	t.Prompt = ""
	t.MaxHeight = 0 // a specification may be long
	t.CharLimit = 0
	t.Blur()
	return t
}

// textareaTop puts the cursor on the first line.
func textareaTop(t *textarea.Model) {
	for i := 0; i < len(t.Value())+1 && t.Line() > 0; i++ {
		t.CursorUp()
	}
	for i := 0; i < len(t.Value())+1; i++ {
		before := t.Line()
		t.CursorUp()
		if t.Line() == before && before == 0 {
			break
		}
	}
	t.CursorStart()
}

// textareaLine puts the cursor on logical line n.
func textareaLine(t *textarea.Model, n int) {
	textareaTop(t)
	for i := 0; i < len(t.Value())+1 && t.Line() < n; i++ {
		t.CursorDown()
	}
}

// editKey identifies the document a dirty editor belongs to.
type editKey struct {
	garden uint64
	issue  garden.IssueID
}

type edSection struct{ title, text string }

type editMatch struct{ sec, line int }

// editorDoc is the specification being edited. Its text lives in gardenState
// keyed by (Garden, Issue), in memory only, until it is saved or discarded.
type editorDoc struct {
	key      editKey
	number   int64
	title    string
	base     gardenapi.DocumentBase
	secs     []edSection
	orig     []string // section texts when the editor was opened or last saved
	origHash string
	cur      int
	ta       textarea.Model
	text     bool // the text has the keyboard; otherwise the section rail does

	searching bool
	si        textinput.Model
	query     string
	matches   []editMatch
	mi        int

	help    bool
	discard bool
	btn     int
	notice  string
	// remote is the newest revision someone else wrote since the base, when it differs.
	remote *gardenapi.RemoteRevision
}

func sectionsOf(s spec.Spec) []edSection {
	var out []edSection
	for i, t := range s.Titles() {
		out = append(out, edSection{title: t, text: s.Text(i)})
	}
	return out
}

func newEditorDoc(key editKey, doc gardenapi.DocumentView) *editorDoc {
	e := &editorDoc{key: key, number: doc.Number, title: doc.Title}
	e.reseed(doc)
	return e
}

// reseed replaces the text with the document's seed. Only a clean editor or an explicit
// reload may do this: dirty text is never overwritten.
func (e *editorDoc) reseed(doc gardenapi.DocumentView) {
	e.base = doc.Base
	s := spec.Parse(doc.Seed)
	e.secs = sectionsOf(s)
	e.orig = make([]string, len(e.secs))
	for i, sec := range e.secs {
		e.orig[i] = sec.text
	}
	e.origHash = garden.ContentHash(spec.Render(s))
	e.ta = newTextarea()
	e.cur = 0
	e.ta.SetValue(e.secs[0].text)
	textareaTop(&e.ta)
	e.remote, e.notice = nil, ""
	e.setText(e.text)
}

func (e *editorDoc) setText(on bool) {
	e.text = on
	if on {
		e.ta.Focus()
		return
	}
	e.ta.Blur()
}

// flush stores the visible text into its section.
func (e *editorDoc) flush() { e.secs[e.cur].text = e.ta.Value() }

func (e *editorDoc) spec() spec.Spec {
	e.flush()
	var s spec.Spec
	for i, sec := range e.secs {
		if i < spec.FixedCount {
			*s.Field(i) = sec.text
			continue
		}
		s.Extra = append(s.Extra, spec.Section{Title: sec.title, Text: sec.text})
	}
	return s
}

// content is the canonical text a save would send.
func (e *editorDoc) content() string { return spec.Render(e.spec()) }

func (e *editorDoc) dirty() bool { return garden.ContentHash(e.content()) != e.origHash }

func (e *editorDoc) modified(i int) bool {
	text := e.secs[i].text
	if i == e.cur {
		text = e.ta.Value()
	}
	return garden.NormalizeContent(text) != garden.NormalizeContent(e.orig[i])
}

func (e *editorDoc) selectSection(i int) {
	i = max(0, min(i, len(e.secs)-1))
	if i == e.cur {
		return
	}
	e.flush()
	e.cur = i
	e.ta.SetValue(e.secs[i].text)
	textareaTop(&e.ta)
}

// search finds every line containing q, case-insensitively, across the document.
func (e *editorDoc) search(q string) {
	e.flush()
	e.query, e.matches, e.mi = q, nil, 0
	if strings.TrimSpace(q) == "" {
		return
	}
	needle := strings.ToLower(q)
	for si, sec := range e.secs {
		for li, line := range strings.Split(sec.text, "\n") {
			if strings.Contains(strings.ToLower(line), needle) {
				e.matches = append(e.matches, editMatch{si, li})
			}
		}
	}
}

// gotoMatch moves to match i (wrapping), switching section as needed.
func (e *editorDoc) gotoMatch(i int) {
	if len(e.matches) == 0 {
		return
	}
	e.mi = (i%len(e.matches) + len(e.matches)) % len(e.matches)
	mt := e.matches[e.mi]
	e.selectSection(mt.sec)
	textareaLine(&e.ta, mt.line)
}

// position is "§ Requirements · 12/40": the section and the cursor line within it.
func (e *editorDoc) position() string {
	return fmt.Sprintf("§ %s · %d/%d", e.secs[e.cur].title, e.ta.Line()+1, max(1, e.ta.LineCount()))
}

// Messages -------------------------------------------------------------------

type docThen int

const (
	docOpen    docThen = iota // open the editor
	docCompare                // open the comparison against the editor's text
	docReload                 // replace a clean editor's text
	docAssist                 // make sure an editor exists, then open AI elaboration
	docApply                  // make sure an editor exists, then apply the review's decisions
	docIdea                   // the Issue a new idea just became: re-key its editor and preview the first save
)

type (
	gardenDocumentMsg struct {
		seq  uint64
		then docThen
		view gardenapi.DocumentView
		err  error
	}
	gardenSavePreviewMsg struct {
		seq     uint64
		resolve bool
		view    gardenapi.SavePreviewView
		err     error
	}
	gardenSaveMsg struct {
		seq     uint64
		resolve bool
		view    gardenapi.SaveView
		err     error
	}
)

func (m Model) gardenLoadDocument(id garden.IssueID, then docThen) (Model, tea.Cmd) {
	ctx, seq := m.garden.begin(busyDocument)
	api := m.garden.api
	return m, func() tea.Msg {
		v, err := api.Document(ctx, uint64(id))
		return gardenDocumentMsg{seq: seq, then: then, view: v, err: err}
	}
}

func (m Model) gardenPreviewSave(req gardenapi.SaveRequest) (Model, tea.Cmd) {
	ctx, seq := m.garden.begin(busyPreviewSave)
	api := m.garden.api
	return m, func() tea.Msg {
		v, err := api.PreviewSave(ctx, req)
		return gardenSavePreviewMsg{seq: seq, view: v, err: err}
	}
}

func (m Model) gardenPreviewResolve(req gardenapi.ResolveRequest) (Model, tea.Cmd) {
	ctx, seq := m.garden.begin(busyPreviewSave)
	api := m.garden.api
	return m, func() tea.Msg {
		v, err := api.PreviewResolve(ctx, req)
		return gardenSavePreviewMsg{seq: seq, resolve: true, view: v, err: err}
	}
}

// gardenRunSave appends the previewed revision. Like every write it is never cancelled.
func (m Model) gardenRunSave(p gardenapi.SavePreviewView, resolve bool) (Model, tea.Cmd) {
	_, seq := m.garden.begin(busySave)
	api := m.garden.api
	return m, func() tea.Msg {
		var v gardenapi.SaveView
		var err error
		if resolve {
			v, err = api.Resolve(context.Background(), p)
		} else {
			v, err = api.Save(context.Background(), p)
		}
		return gardenSaveMsg{seq: seq, resolve: resolve, view: v, err: err}
	}
}

func (m Model) editKeyFor(id garden.IssueID) editKey {
	return editKey{garden: m.garden.status.Target.RepoID, issue: id}
}

// canEdit says whether e may start: editing appends a revision, which needs Write access.
func (m Model) canEdit() (bool, string) {
	g := m.garden
	switch {
	case g.api == nil || !g.hasStatus:
		return false, "Garden is not loaded yet."
	case g.status.Access < gardenapi.AccessWrite:
		return false, "Editing a specification needs Write access to " + g.status.Target.FullName + "."
	}
	return true, ""
}

func (m Model) onGardenDocument(msg gardenDocumentMsg) (Model, tea.Cmd) {
	g := &m.garden
	d := &g.drafts
	fail, ok := g.settle(msg.seq, msg.err)
	if !ok {
		return m, nil
	}
	if fail != nil {
		return m.gardenFailed(busyDocument, fail)
	}
	if !g.gate.Accept(msg.view.Stamp) {
		g.err = staleReply
		return m, nil
	}
	doc := msg.view
	key := m.editKeyFor(garden.IssueID(doc.IssueID))
	e := d.editors[key]
	if doc.Conflict != nil && msg.then == docAssist {
		return m.assistNotice("This specification has competing heads. Resolve them in the editor before asking AI to elaborate it."), nil
	}
	if doc.Conflict != nil && msg.then == docApply {
		if s := m.garden.assist.sessions[key]; s != nil {
			s.notice = "This specification now has competing heads. Nothing was applied; resolve them in the editor first."
		}
		return m, nil
	}
	if doc.Conflict != nil {
		// Several heads already exist: there is nothing to edit until they are resolved.
		yours := ""
		if e != nil {
			yours = e.content()
		}
		return m.gardenOpenConflict(*doc.Conflict, yours, doc.Number, doc.Title, ""), nil
	}
	switch msg.then {
	case docReload:
		if e != nil && !e.dirty() {
			e.reseed(doc)
			e.notice = "Reloaded the current specification from GitHub."
		}
		return m, nil
	case docCompare:
		if e == nil {
			return m, nil
		}
		return m.gardenOpenConflict(compareConflict(e, doc), e.content(), doc.Number, doc.Title, ""), nil
	case docAssist:
		if e == nil {
			e = newEditorDoc(key, doc)
			if d.editors == nil {
				d.editors = map[editKey]*editorDoc{}
			}
			d.editors[key] = e
		}
		return m.gardenAssistOpen(key, false, doc.Number, doc.Title, doc.Type)
	case docApply:
		s := m.garden.assist.sessions[key]
		if s == nil {
			return m, nil
		}
		if e == nil {
			e = newEditorDoc(key, doc)
			if d.editors == nil {
				d.editors = map[editKey]*editorDoc{}
			}
			d.editors[key] = e
		}
		return m.assistContinue(s, e, s.after, s.afterFeedback)
	case docIdea:
		return m.gardenIdeaRekey(doc)
	}
	if e == nil {
		e = newEditorDoc(key, doc)
		if d.editors == nil {
			d.editors = map[editKey]*editorDoc{}
		}
		d.editors[key] = e
	} else if e.base.HeadHash != doc.Base.HeadHash {
		e.remote = remoteHead(doc)
	}
	d.edit, d.stage = e, draftsEditor
	m.shellFocus = shellInput
	m.gardenSizeEditor()
	return m, nil
}

// remoteHead is the newest revision of a document, for the banner.
func remoteHead(doc gardenapi.DocumentView) *gardenapi.RemoteRevision {
	for i := len(doc.Revisions) - 1; i >= 0; i-- {
		if doc.Revisions[i].Hash == doc.Base.HeadHash {
			r := doc.Revisions[i]
			return &r
		}
	}
	return &gardenapi.RemoteRevision{Source: "Issue body", At: time.Time{}}
}

// compareConflict builds the Base/Yours/Remote comparison for an editor whose base moved.
func compareConflict(e *editorDoc, doc gardenapi.DocumentView) gardenapi.ConflictView {
	c := gardenapi.ConflictView{IssueID: doc.IssueID, Base: e.base, BodyHash: doc.Base.BodyHash}
	if r := remoteHead(doc); r != nil && r.Hash != "" {
		c.Remote = []gardenapi.RemoteRevision{*r}
		return c
	}
	c.Remote = []gardenapi.RemoteRevision{{Content: doc.Seed, Source: "Issue body edited on GitHub (not part of the specification)"}}
	return c
}

func (m Model) onGardenSavePreview(msg gardenSavePreviewMsg) (Model, tea.Cmd) {
	g := &m.garden
	d := &g.drafts
	fail, ok := g.settle(msg.seq, msg.err)
	if !ok {
		return m, nil
	}
	if fail != nil {
		if fail.Code == gardenapi.CodeNoChange && d.edit != nil {
			d.edit.notice = fail.Message
			return m, nil
		}
		return m.gardenFailed(busyPreviewSave, fail)
	}
	if !g.gate.Accept(msg.view.Stamp) {
		g.err = staleReply
		return m, nil
	}
	v := msg.view
	if v.Conflict != nil {
		yours := ""
		if d.edit != nil {
			yours = d.edit.content()
		}
		if msg.resolve && d.conflict != nil {
			yours = d.conflict.view.Yours
		}
		n, t := d.openNumberTitle()
		return m.gardenOpenConflict(*v.Conflict, yours, n, t, "The remote side changed again. Compare again before publishing."), nil
	}
	if d.idea.active && d.idea.issue != 0 && v.IssueID == uint64(d.idea.issue) && !v.BodyChanged && !msg.resolve {
		// The Issue was created a moment ago and nothing else touched it: the second write needs no second question.
		d.savePreview, d.saveView = &v, nil
		return m.gardenRunSave(v, false)
	}
	d.savePreview, d.saveView, d.stage, d.btn = &v, nil, draftsSave, 0
	m.shellFocus = shellBody
	return m, nil
}

// openNumberTitle is the Issue number and title of the document in view.
func (d *gardenDraftsState) openNumberTitle() (int64, string) {
	if d.edit != nil {
		return d.edit.number, d.edit.title
	}
	if d.conflict != nil {
		return d.conflict.number, d.conflict.title
	}
	return 0, ""
}

func (m Model) onGardenSave(msg gardenSaveMsg) (Model, tea.Cmd) {
	g := &m.garden
	d := &g.drafts
	fail, ok := g.settle(msg.seq, msg.err)
	if !ok {
		return m, nil
	}
	if fail != nil {
		return m.gardenFailed(busySave, fail)
	}
	if !g.gate.Accept(msg.view.Stamp) {
		g.err = staleReply
		return m, nil
	}
	v := msg.view
	switch v.State {
	case gardenapi.SaveConflict:
		if v.Conflict == nil {
			g.err = staleReply
			return m, nil
		}
		yours := v.Conflict.Yours
		n, t := d.openNumberTitle()
		note := "The remote side changed before saving. Nothing was written."
		if v.Conflict.Saved {
			note = "Your revision was saved, but another revision landed at the same time. Both heads are shown: publish a result to join them."
		}
		return m.gardenOpenConflict(*v.Conflict, yours, n, t, note), nil
	case gardenapi.SaveUnknown:
		d.saveView = &v
		return m, nil
	}
	if d.idea.active && d.savePreview != nil && d.edit != nil && !msg.resolve && d.edit.key.issue == d.idea.issue {
		return m.gardenIdeaDone()
	}
	// Done: the editor's base moves to the revision just written and its text stays as typed.
	d.notice = fmt.Sprintf("Saved revision %s.", shortHash(v.Hash))
	if d.savePreview != nil && d.edit != nil && !msg.resolve {
		e := d.edit
		e.base = gardenapi.DocumentBase{HeadHash: v.Hash, HeadCommentID: v.CommentID, BodyHash: d.savePreview.Base.BodyHash, Content: d.savePreview.Content}
		s := spec.Parse(d.savePreview.Content)
		e.secs = sectionsOf(s)
		e.orig = make([]string, len(e.secs))
		for i, sec := range e.secs {
			e.orig[i] = sec.text
		}
		e.origHash = garden.ContentHash(spec.Render(s))
		e.cur = min(e.cur, len(e.secs)-1)
		e.ta.SetValue(e.secs[e.cur].text)
		textareaTop(&e.ta)
		e.remote, e.notice = nil, d.notice
		m.garden.assist.discard(e.key) // a saved draft ends its assistant session
		d.stage = draftsEditor
		m.shellFocus = shellInput
		d.savePreview, d.saveView = nil, nil
		return m.gardenLoadWork()
	}
	// A resolution replaces whatever was being edited for this Issue.
	if d.conflict != nil {
		delete(d.editors, m.editKeyFor(d.conflict.issue))
		m.garden.assist.discard(m.editKeyFor(d.conflict.issue))
		if d.idea.active && d.conflict.issue == d.idea.issue {
			d.selectAfter = d.idea.issue
			d.idea = ideaSave{}
		}
		d.opened = d.conflict.issue
	}
	d.edit, d.conflict, d.savePreview, d.saveView = nil, nil, nil, nil
	d.stage = draftsDetail
	d.docScroll = 0
	m.shellFocus = shellBody
	return m.gardenLoadWork()
}

// checkRemote notes, for the editor in view, a specification head that differs from its base.
func (d *gardenDraftsState) checkRemote(snap gardensync.Snapshot) {
	e := d.edit
	if e == nil {
		return
	}
	for _, it := range snap.Items {
		if it.Issue.ID != e.key.issue {
			continue
		}
		var heads []garden.Revision
		for _, h := range it.Verification.Heads {
			if h.Kind == garden.RevisionSpec {
				heads = append(heads, h)
			}
		}
		if len(heads) == 0 || (len(heads) == 1 && heads[0].Hash == e.base.HeadHash) {
			return
		}
		h := heads[len(heads)-1]
		for _, c := range heads {
			if c.CommentID > h.CommentID {
				h = c
			}
		}
		if h.Hash == e.base.HeadHash {
			return
		}
		e.remote = &gardenapi.RemoteRevision{Hash: h.Hash, CommentID: h.CommentID, Author: h.Author, At: h.CreatedAt, Content: h.Content, Source: "Bonsai/GitHub comment"}
		return
	}
}

// gardenSizeEditor gives the text the width and height the view draws it at.
func (m *Model) gardenSizeEditor() {
	e := m.garden.drafts.edit
	if e == nil {
		return
	}
	w, h := m.gardenEditorText()
	e.ta.SetWidth(w)
	e.ta.SetHeight(h)
}

// gardenEditorText is the text region: a bounded reading width, and the rows between the header and the footer.
func (m Model) gardenEditorText() (w, h int) {
	rail := gardenEditorRail(m.width)
	w = max(10, min(88, m.width-rail-3))
	h = max(3, m.gardenBoardRows()-5)
	return w, h
}

func gardenEditorRail(width int) int {
	if width >= 100 {
		return 24
	}
	return 18
}

// gardenEditorKey handles a key while the structured editor is open.
func (m Model) gardenEditorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	g := &m.garden
	d := &g.drafts
	e := d.edit
	k := msg.String()
	if e == nil {
		d.stage = draftsDetail
		return m, nil
	}
	m.gardenSizeEditor()
	switch {
	case e.discard:
		switch k {
		case "left", "right", "h", "l", "tab", "shift+tab":
			e.btn = 1 - e.btn
		case "esc":
			e.discard = false
		case "enter":
			if e.btn == 1 && (e.key.issue == 0 || e.key.issue == d.idea.issue) && d.idea.op != "" {
				// The Capture may already have created the Issue: discarding now would lose the specification.
				e.discard = false
				e.notice = "The Issue is being created. Discard after it finishes."
				return m, nil
			}
			if e.btn == 1 {
				delete(d.editors, e.key)
				m.garden.assist.discard(e.key)
				d.edit, d.stage = nil, draftsDetail
				if e.key.issue == 0 || e.key.issue == d.idea.issue {
					d.idea, d.stage = ideaSave{}, draftsList
				}
				m.shellFocus = shellBody
				return m, nil
			}
			e.discard = false
		}
		return m, nil
	case e.help:
		e.help = false
		return m, nil
	case e.searching:
		switch k {
		case "esc":
			e.searching = false
		case "enter":
			e.searching = false
			e.search(e.si.Value())
			e.gotoMatch(0)
		default:
			var cmd tea.Cmd
			e.si, cmd = e.si.Update(msg)
			return m, cmd
		}
		return m, nil
	}
	e.notice = ""
	g.err = nil
	switch k {
	case "ctrl+s":
		return m.gardenSaveEditor()
	case "tab", "shift+tab":
		e.setText(!e.text)
		return m, nil
	case "esc":
		if e.text {
			e.setText(false)
			return m, nil
		}
		if e.dirty() {
			e.discard, e.btn = true, 0
			return m, nil
		}
		delete(d.editors, e.key)
		d.edit, d.stage = nil, draftsDetail
		m.shellFocus = shellBody
		return m, nil
	}
	if e.text {
		var cmd tea.Cmd
		e.ta, cmd = e.ta.Update(msg)
		return m, cmd
	}
	switch k {
	case "up", "k":
		e.selectSection(e.cur - 1)
	case "down", "j":
		e.selectSection(e.cur + 1)
	case "enter", "right", "l":
		e.setText(true)
	case "/":
		e.si = textinput.New()
		e.si.Cursor.SetMode(cursor.CursorStatic)
		e.si.CharLimit = 256
		e.si.SetValue(e.query)
		e.si.Focus()
		e.searching = true
	case "n":
		e.gotoMatch(e.mi + 1)
	case "N":
		e.gotoMatch(e.mi - 1)
	case "?":
		e.help = true
	case "i":
		return m.gardenAssistExisting(e.key.issue)
	case "c":
		if e.remote != nil {
			if e.dirty() {
				return m.gardenLoadDocument(e.key.issue, docCompare)
			}
			return m.gardenLoadDocument(e.key.issue, docReload)
		}
	}
	return m, nil
}

func (m Model) gardenSaveEditor() (tea.Model, tea.Cmd) {
	e := m.garden.drafts.edit
	if ok, why := m.canEdit(); !ok {
		e.notice = why
		return m, nil
	}
	if e.key.issue == 0 {
		return m.gardenIdeaSave(e)
	}
	if !e.dirty() {
		e.notice = "There is nothing to save: the specification is unchanged."
		return m, nil
	}
	return m.gardenPreviewSave(gardenapi.SaveRequest{IssueID: uint64(e.key.issue), Base: e.base, Content: e.content()})
}

// gardenSaveKey handles the save preview: Back to edit has the first focus.
func (m Model) gardenSaveKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	g := &m.garden
	d := &g.drafts
	resolve := d.savePreview != nil && d.savePreview.Resolution != nil
	back := func() (tea.Model, tea.Cmd) {
		d.savePreview, d.saveView = nil, nil
		g.err = nil
		if resolve && d.conflict != nil {
			d.stage = draftsConflict
			return m, nil
		}
		d.stage = draftsEditor
		m.shellFocus = shellInput
		return m, nil
	}
	switch msg.String() {
	case "left", "right", "h", "l", "tab", "shift+tab":
		d.btn = 1 - d.btn
	case "esc":
		return back()
	case "enter":
		if d.btn == 0 {
			return back()
		}
		if d.savePreview != nil {
			return m.gardenRunSave(*d.savePreview, resolve)
		}
	}
	return m, nil
}
