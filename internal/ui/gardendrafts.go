package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	gardensync "github.com/Tiago-0liveira/bonsai/internal/core/garden/sync"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/work"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// draftsStage is what the Drafts submenu shows. The capture stages (form,
// preview, result) stack on the list, and Esc always returns to the one before.
type draftsStage int

const (
	draftsList draftsStage = iota
	draftsDetail
	draftsForm
	draftsPreview
	draftsResult
	draftsEditor   // the structured specification editor
	draftsSave     // the preview of the revision a save or resolution appends
	draftsConflict // Base / Yours / Remote / Result
	draftsValidate // gate one: the preview and result of validating a specification
	draftsAssist   // AI elaboration: setup, questions and the proposal (gardenassist.go)
)

// captureTypes are the type choices in the order the form offers them.
var captureTypes = []garden.ItemType{garden.TypeFeature, garden.TypeBug, garden.TypeChore, garden.TypeResearch}

type captureField int

const (
	fieldTitle captureField = iota
	fieldDesc
	fieldType
	fieldPreview
	fieldCount
)

// captureForm is the unfinished idea. It lives in gardenState, not in a screen,
// so errors, resizes, workspace switches and refreshes never lose the text.
type captureForm struct {
	title   textinput.Model
	desc    textarea.Model
	typ     int
	focus   captureField
	discard bool // the "discard this idea?" question is showing
}

func newCaptureForm() captureForm {
	t := textinput.New()
	t.Cursor.SetMode(cursor.CursorStatic) // keys only, so no blink messages
	t.CharLimit = 256
	t.Prompt = ""
	t.Focus()
	d := textarea.New()
	d.Cursor.SetMode(cursor.CursorStatic)
	d.ShowLineNumbers = false
	d.Prompt = ""
	d.CharLimit = garden.MaxDescriptionBytes
	d.MaxHeight = 0
	return captureForm{title: t, desc: d}
}

func (f captureForm) dirty() bool {
	return strings.TrimSpace(f.title.Value()) != "" || strings.TrimSpace(f.desc.Value()) != ""
}

func (f captureForm) request() gardenapi.CaptureRequest {
	return gardenapi.CaptureRequest{Title: strings.TrimSpace(f.title.Value()), Description: f.desc.Value(), Type: captureTypes[f.typ]}
}

// captureFieldWidth is the bracketed width of the inputs: the label column, the
// marker and the margin come off the width, and a wide terminal keeps a readable line.
func captureFieldWidth(width int) int { return max(12, min(width-18, 88)) }

func (f *captureForm) setFocus(c captureField) {
	f.focus = c
	f.title.Blur()
	f.desc.Blur()
	switch c {
	case fieldTitle:
		f.title.Focus()
	case fieldDesc:
		f.desc.Focus()
	}
}

// gardenDraftsState is the Drafts inbox: the selection by Issue ID, the search,
// the open detail and the capture flow. The data comes from the one Work read
// the Board shares; Drafts only filters it differently.
type gardenDraftsState struct {
	stage      draftsStage
	collection work.DraftCollection
	query      work.Query
	selected   garden.IssueID
	row        int // deletion anchor: the selection's last index
	scroll     int
	docScroll  int
	opened     garden.IssueID

	searching      bool
	search         textinput.Model
	beforeSearch   work.Query
	beforeSelected garden.IssueID

	notice     string
	pending    []gardenapi.CaptureView // captures that may exist on GitHub, from the last read
	pendingErr *gardenapi.Error

	// selectAfter is the Issue a finished capture created; the next read selects it.
	selectAfter garden.IssueID

	// editors holds every unsaved specification by (Garden, Issue), in memory only. Text survives errors,
	// resizes, workspace switches, profile changes and refreshes until it is saved or explicitly discarded.
	editors     map[editKey]*editorDoc
	edit        *editorDoc
	conflict    *conflictState
	savePreview *gardenapi.SavePreviewView
	saveView    *gardenapi.SaveView

	validate  *gardenapi.ValidatePreviewView
	validated *gardenapi.ValidateView

	// idea is the save of an AI-drafted new idea in progress: Capture, then the first specification revision.
	idea ideaSave

	form     captureForm
	formOpen bool // unfinished text exists, shown or not
	preview  *gardenapi.CapturePreview
	result   *gardenapi.CaptureView
	btn      int // focus among the buttons of the preview, result and discard screens
}

func (d *gardenDraftsState) rowIndex() int {
	if d.selected == 0 {
		return -1
	}
	_, i, ok := d.collection.Find(d.selected)
	if !ok {
		return -1
	}
	return i
}

// reselect rebuilds the inbox from a read. The selection is the Issue ID; when
// that Issue left, the row now at its index takes over and a notice says so.
func (d *gardenDraftsState) reselect(snap gardensync.Snapshot) {
	d.collection = work.Drafts(snap.Items, d.query, snap.Findings...)
	recs := d.collection.Records
	if d.selectAfter != 0 {
		if _, i, ok := d.collection.Find(d.selectAfter); ok {
			d.selected, d.row = d.selectAfter, i
			d.selectAfter = 0
			return
		}
		if snap.Source == gardensync.SourceLive && snap.Complete {
			d.selectAfter = 0
		}
	}
	if d.opened != 0 {
		if _, _, ok := d.collection.Find(d.opened); !ok {
			d.opened, d.stage, d.docScroll = 0, draftsList, 0
			d.notice = "Opened draft left the inbox; returned to Drafts."
		}
	}
	if i := d.rowIndex(); i >= 0 {
		d.row = i
		return
	}
	left := d.selected != 0
	d.selected = 0
	if len(recs) == 0 {
		if left && d.notice == "" {
			d.notice = "Selected draft left the inbox; no drafts remain."
		}
		return
	}
	d.row = max(0, min(d.row, len(recs)-1))
	d.selected = recs[d.row].Item.Issue.ID
	if left && d.notice == "" {
		d.notice = "Selected draft left the inbox; selected the row now in its place."
	}
}

func (d *gardenDraftsState) move(delta, page int, k string) {
	n := len(d.collection.Records)
	if n == 0 {
		return
	}
	i := max(0, d.rowIndex())
	switch k {
	case "up", "k":
		i--
	case "down", "j":
		i++
	case "pgup":
		i -= page
	case "pgdown":
		i += page
	case "home":
		i = 0
	case "end":
		i = n - 1
	default:
		return
	}
	i = max(0, min(i, n-1))
	d.row, d.selected = i, d.collection.Records[i].Item.Issue.ID
}

// gardenTyping reports that the Drafts body owns the keyboard for text.
func (m Model) gardenTyping() bool {
	if m.workspace == wsGarden && m.gardenScreen() == scrBoard && m.plannerActive() && m.garden.planner.stage == plannerChoose {
		return true // the planner's account choice has a name field
	}
	if m.workspace != wsGarden || m.gardenScreen() != scrDrafts {
		return false
	}
	d := m.garden.drafts
	return d.searching || d.stage == draftsForm || d.stage == draftsEditor || d.stage == draftsAssist || (d.stage == draftsConflict && d.conflict != nil && d.conflict.editing)
}

func (m Model) gardenDraftsCapacity() int { return gardenTableCapacity(m.gardenBoardRows()) }

// gardenDraftsVisible keeps the selected row inside the window.
func (m *Model) gardenDraftsVisible() {
	d := &m.garden.drafts
	d.scroll = tableScrollFor(d.scroll, d.rowIndex(), m.gardenDraftsCapacity(), len(d.collection.Records))
}

// canCapture says whether n may start: it needs Write access and no capture
// whose outcome is still unknown. The reason is shown instead of the action.
func (m Model) canCapture() (bool, string) {
	g := m.garden
	switch {
	case g.api == nil || !g.hasStatus:
		return false, "Garden is not loaded yet."
	case g.status.Access < gardenapi.AccessWrite:
		return false, "Capturing an idea needs Write access to " + g.status.Target.FullName + "."
	case g.drafts.pendingErr != nil:
		return false, g.drafts.pendingErr.Message + " " + g.drafts.pendingErr.Next
	case len(g.drafts.pending) > 0 || (g.drafts.result != nil && g.drafts.result.State == gardenapi.CaptureUnknown):
		return false, "A capture may already exist on GitHub. Reconcile it first (r)."
	}
	return true, ""
}

// Replies of the capture calls. Each carries the sequence number of its request.
type (
	gardenCapturePreviewMsg struct {
		seq  uint64
		view gardenapi.CapturePreview
		err  error
	}
	gardenCaptureMsg struct {
		seq       uint64
		reconcile bool
		view      gardenapi.CaptureView
		err       error
	}
)

func (m Model) gardenPreviewCapture(req gardenapi.CaptureRequest) (Model, tea.Cmd) {
	ctx, seq := m.garden.begin(busyPreviewCapture)
	api := m.garden.api
	return m, func() tea.Msg {
		v, err := api.PreviewCapture(ctx, req)
		return gardenCapturePreviewMsg{seq: seq, view: v, err: err}
	}
}

// gardenRunCapture creates the previewed Issue. It is never cancelled: the
// backend's operation record decides what a retry may do.
func (m Model) gardenRunCapture(p gardenapi.CapturePreview) (Model, tea.Cmd) {
	if m.garden.drafts.idea.active {
		m.garden.drafts.idea.op = p.OperationID
	}
	_, seq := m.garden.begin(busyCapture)
	api := m.garden.api
	return m, func() tea.Msg {
		v, err := api.Capture(context.Background(), p)
		return gardenCaptureMsg{seq: seq, view: v, err: err}
	}
}

func (m Model) gardenReconcile(opID string) (Model, tea.Cmd) {
	_, seq := m.garden.begin(busyReconcile)
	api := m.garden.api
	return m, func() tea.Msg {
		v, err := api.ReconcileCapture(context.Background(), opID)
		return gardenCaptureMsg{seq: seq, reconcile: true, view: v, err: err}
	}
}

func (m Model) onGardenCapturePreview(msg gardenCapturePreviewMsg) (Model, tea.Cmd) {
	g := &m.garden
	fail, ok := g.settle(msg.seq, msg.err)
	if !ok {
		return m, nil
	}
	if fail != nil {
		return m.gardenFailed(busyPreviewCapture, fail)
	}
	if !g.gate.Accept(msg.view.Stamp) {
		g.err = staleReply
		return m, nil
	}
	p := msg.view
	g.drafts.preview, g.drafts.stage, g.drafts.btn = &p, draftsPreview, 0
	m.shellFocus = shellBody
	return m, nil
}

// closeCapture drops the unfinished idea: it was created, or the user discarded it.
func (d *gardenDraftsState) closeCapture() {
	d.form, d.formOpen, d.preview, d.result, d.btn = captureForm{}, false, nil, nil, 0
	d.stage = draftsList
}

func (d *gardenDraftsState) trackPending(v gardenapi.CaptureView) {
	for i := range d.pending {
		if d.pending[i].OperationID == v.OperationID {
			d.pending[i] = v
			return
		}
	}
	d.pending = append(d.pending, v)
}

func (d *gardenDraftsState) untrackPending(opID string) {
	kept := d.pending[:0:0]
	for _, p := range d.pending {
		if p.OperationID != opID {
			kept = append(kept, p)
		}
	}
	d.pending = kept
}

func (m Model) onGardenCapture(msg gardenCaptureMsg) (Model, tea.Cmd) {
	g := &m.garden
	d := &g.drafts
	kind := busyCapture
	if msg.reconcile {
		kind = busyReconcile
	}
	fail, ok := g.settle(msg.seq, msg.err)
	if !ok {
		return m, nil
	}
	if fail != nil {
		return m.gardenFailed(kind, fail)
	}
	if !g.gate.Accept(msg.view.Stamp) {
		g.err = staleReply
		return m, nil
	}
	v := msg.view
	mine := d.preview != nil && d.preview.OperationID == v.OperationID
	switch v.State {
	case gardenapi.CaptureDone:
		d.untrackPending(v.OperationID)
		if d.idea.active && d.idea.issue == 0 && d.idea.op == v.OperationID && v.Issue != nil {
			return m.gardenIdeaCaptured(v)
		}
		if v.Issue != nil {
			d.selectAfter = garden.IssueID(v.Issue.ID)
			d.notice = fmt.Sprintf("Created #%d as a Draft.", v.Issue.Number)
		}
		if mine || !msg.reconcile {
			d.closeCapture()
		}
		d.stage = draftsList
		m.shellFocus = shellBody
		return m.gardenLoadWork()
	case gardenapi.CaptureFailed:
		d.untrackPending(v.OperationID)
		if mine {
			// Nothing exists on GitHub: the same operation may be retried.
			d.result, d.stage, d.btn = &v, draftsResult, 1
			m.shellFocus = shellBody
			return m, nil
		}
		d.notice = "Nothing was created on GitHub for that capture. Capture the idea again with n."
		return m.gardenLoadWork()
	}
	// Unknown: keep everything and say what to do. The form stays so the text is not lost,
	// and n stays refused until the capture is settled, even after the result screen closes.
	d.trackPending(v)
	if mine || !msg.reconcile {
		d.result, d.stage, d.btn = &v, draftsResult, 0
		m.shellFocus = shellBody
	}
	if v.Err != nil {
		g.err = v.Err
	}
	return m, nil
}

// gardenDraftsKey handles a key while the Drafts submenu owns the keyboard.
func (m Model) gardenDraftsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	g := &m.garden
	d := &g.drafts
	k := msg.String()
	if g.busy == busyPreviewCapture || g.busy == busyDocument || g.busy == busyPreviewSave || g.busy == busyPreviewValidate {
		if k == "esc" {
			g.abandon()
		}
		return m, nil
	}
	if d.searching {
		return m.gardenDraftsSearchKey(msg)
	}
	switch d.stage {
	case draftsForm:
		return m.gardenFormKey(msg)
	case draftsPreview:
		return m.gardenPreviewKey(msg)
	case draftsResult:
		return m.gardenResultKey(msg)
	case draftsDetail:
		return m.gardenDraftDetailKey(msg)
	case draftsEditor:
		return m.gardenEditorKey(msg)
	case draftsSave:
		return m.gardenSaveKey(msg)
	case draftsConflict:
		return m.gardenConflictKey(msg)
	case draftsValidate:
		return m.gardenValidateKey(msg)
	case draftsAssist:
		return m.gardenAssistKey(msg)
	}
	if key.Matches(msg, m.keys.Refresh) {
		return m.gardenLoadWork()
	}
	if key.Matches(msg, m.keys.GardenView) {
		g.sub = subBoard
		cmd := m.gardenRememberView()
		return m, cmd
	}
	switch k {
	case "esc":
		g.err, g.notice, d.notice = nil, "", ""
		g.sub = subBoard
		return m, nil
	case "enter":
		if d.selected != 0 {
			d.opened, d.stage, d.docScroll = d.selected, draftsDetail, 0
		}
	case "/":
		return m.gardenDraftsStartSearch()
	case "t":
		types := append([]garden.ItemType{""}, captureTypes...)
		for i, v := range types {
			if v == d.query.Type {
				d.query.Type = types[(i+1)%len(types)]
				break
			}
		}
		d.reselect(g.board.snapshot)
	case "x":
		d.query = work.Query{}
		d.reselect(g.board.snapshot)
	case "n":
		return m.gardenNewCapture()
	case "i":
		return m.gardenAssistNew()
	case "r":
		if len(d.pending) > 0 {
			return m.gardenReconcile(d.pending[0].OperationID)
		}
	default:
		if key.Matches(msg, m.keys.Tab) && m.canToggleSub() {
			m = m.gardenToggleSub()
			return m.gardenLoadWork()
		}
		d.move(0, m.gardenDraftsCapacity(), k)
	}
	m.gardenDraftsVisible()
	return m, nil
}

func (m Model) gardenNewCapture() (tea.Model, tea.Cmd) {
	g := &m.garden
	d := &g.drafts
	if ok, why := m.canCapture(); !ok {
		d.notice = why
		return m, nil
	}
	d.notice = ""
	if !d.formOpen {
		d.form, d.formOpen = newCaptureForm(), true
	}
	d.stage = draftsForm
	m.shellFocus = shellInput
	m.gardenSizeForm()
	return m, nil
}

// gardenSizeForm gives the inputs the width and height the form view will draw
// them at, so wrapping and the cursor agree with what is on screen.
func (m *Model) gardenSizeForm() {
	f := &m.garden.drafts.form
	w := captureFieldWidth(m.width)
	f.title.Width = w - 2
	f.desc.SetWidth(w - 2)
	f.desc.SetHeight(max(3, min(8, m.gardenBoardRows()-12)))
}

func (m Model) gardenFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	g := &m.garden
	d := &g.drafts
	f := &d.form
	k := msg.String()
	m.gardenSizeForm()
	if f.discard {
		switch k {
		case "left", "right", "h", "l", "tab", "shift+tab":
			d.btn = 1 - d.btn
		case "enter":
			if d.btn == 1 {
				d.closeCapture()
				m.shellFocus = shellBody
				return m, nil
			}
			f.discard = false
		case "esc":
			f.discard = false
		}
		return m, nil
	}
	switch k {
	case "esc":
		g.err = nil
		if d.idea.active {
			return m.gardenIdeaBack(), nil
		}
		if !f.dirty() {
			d.closeCapture()
			m.shellFocus = shellBody
			return m, nil
		}
		f.discard, d.btn = true, 0
		return m, nil
	case "tab", "shift+tab":
		next := (int(f.focus) + 1) % int(fieldCount)
		if k == "shift+tab" {
			next = (int(f.focus) + int(fieldCount) - 1) % int(fieldCount)
		}
		f.setFocus(captureField(next))
		return m, nil
	}
	switch f.focus {
	case fieldTitle:
		if k == "enter" {
			f.setFocus(fieldDesc)
			return m, nil
		}
		var cmd tea.Cmd
		f.title, cmd = f.title.Update(msg)
		return m, cmd
	case fieldDesc:
		var cmd tea.Cmd
		f.desc, cmd = f.desc.Update(msg)
		return m, cmd
	case fieldType:
		switch k {
		case "left", "h", "up":
			f.typ = (f.typ + len(captureTypes) - 1) % len(captureTypes)
		case "right", "l", "down":
			f.typ = (f.typ + 1) % len(captureTypes)
		case "enter":
			f.setFocus(fieldPreview)
		}
		return m, nil
	}
	if k == "enter" {
		if strings.TrimSpace(f.title.Value()) == "" {
			g.err = &gardenapi.Error{Code: gardenapi.CodeInvalidInput, Message: "Give the idea a title."}
			f.setFocus(fieldTitle)
			return m, nil
		}
		return m.gardenPreviewCapture(f.request())
	}
	return m, nil
}

func (m Model) gardenPreviewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	g := &m.garden
	d := &g.drafts
	switch msg.String() {
	case "left", "right", "h", "l", "tab", "shift+tab":
		d.btn = 1 - d.btn
	case "esc":
		return m.gardenBackToForm(), nil
	case "enter":
		if d.btn == 0 {
			return m.gardenBackToForm(), nil
		}
		if d.preview != nil {
			return m.gardenRunCapture(*d.preview)
		}
	}
	return m, nil
}

// gardenBackToForm returns to editing. The preview is dropped: edited text is
// another operation, so it gets a fresh preview and operation ID.
func (m Model) gardenBackToForm() Model {
	d := &m.garden.drafts
	d.preview, d.result, d.stage = nil, nil, draftsForm
	m.garden.err = nil
	m.shellFocus = shellInput
	m.gardenSizeForm()
	return m
}

func (m Model) gardenResultKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	g := &m.garden
	d := &g.drafts
	if d.result == nil {
		d.stage = draftsList
		return m, nil
	}
	k := msg.String()
	unknown := d.result.State == gardenapi.CaptureUnknown
	switch k {
	case "left", "right", "h", "l", "tab", "shift+tab":
		d.btn = 1 - d.btn
	case "esc":
		if unknown {
			d.stage, d.result = draftsList, nil
			g.err = nil
			return m, nil
		}
		return m.gardenBackToForm(), nil
	case "enter":
		switch {
		case unknown && d.btn == 0:
			return m.gardenReconcile(d.result.OperationID)
		case unknown:
			d.stage, d.result = draftsList, nil
			g.err = nil
		case d.btn == 0:
			return m.gardenBackToForm(), nil
		case d.preview != nil:
			return m.gardenRunCapture(*d.preview)
		}
	}
	return m, nil
}

func (m Model) gardenDraftDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.gardenClampDraftDocument()
	d := &m.garden.drafts
	k := msg.String()
	if key.Matches(msg, m.keys.Refresh) {
		return m.gardenLoadWork()
	}
	switch k {
	case "esc", "backspace":
		d.opened, d.stage, d.docScroll = 0, draftsList, 0
	case "down", "j":
		d.docScroll++
	case "up", "k":
		d.docScroll = max(0, d.docScroll-1)
	case "pgdown":
		d.docScroll += max(1, m.gardenBoardRows()-4)
	case "pgup":
		d.docScroll = max(0, d.docScroll-max(1, m.gardenBoardRows()-4))
	case "home":
		d.docScroll = 0
	case "end":
		d.docScroll = m.gardenDraftDocumentMaxScroll()
	case "e":
		if ok, why := m.canEdit(); !ok {
			d.notice = why
			return m, nil
		}
		d.notice = ""
		return m.gardenLoadDocument(d.opened, docOpen)
	case "v":
		return m.gardenStartValidate()
	case "i":
		return m.gardenAssistExisting(d.opened)
	}
	m.gardenClampDraftDocument()
	return m, nil
}

func (m Model) gardenDraftDocumentMaxScroll() int {
	return max(0, len(m.gardenDraftDocument(m.width))-max(0, m.gardenBoardRows()-4))
}

func (m *Model) gardenClampDraftDocument() {
	if m.garden.drafts.stage == draftsDetail {
		m.garden.drafts.docScroll = max(0, min(m.garden.drafts.docScroll, m.gardenDraftDocumentMaxScroll()))
	}
}

func (m Model) gardenDraftsStartSearch() (tea.Model, tea.Cmd) {
	d := &m.garden.drafts
	d.beforeSearch, d.beforeSelected = d.query, d.selected
	d.search = textinput.New()
	// Search handles keys only, so a static cursor avoids scheduling unhandled blink messages.
	d.search.Cursor.SetMode(cursor.CursorStatic)
	d.search.SetValue(d.query.Text)
	d.search.CharLimit = 256
	d.search.Focus()
	d.searching = true
	m.shellFocus = shellInput
	return m, nil
}

func (m Model) gardenDraftsSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	g := &m.garden
	d := &g.drafts
	switch msg.String() {
	case "esc":
		d.query, d.selected = d.beforeSearch, d.beforeSelected
		d.searching = false
		m.shellFocus = shellBody
		d.reselect(g.board.snapshot)
	case "enter":
		d.searching = false
		m.shellFocus = shellBody
	default:
		var cmd tea.Cmd
		d.search, cmd = d.search.Update(msg)
		d.query.Text = d.search.Value()
		d.reselect(g.board.snapshot)
		m.gardenDraftsVisible()
		return m, cmd
	}
	d.notice = ""
	m.gardenDraftsVisible()
	return m, nil
}

// Chrome of the Drafts screens -------------------------------------------------

func (m Model) gardenDraftsCrumbs() []string {
	d := m.garden.drafts
	switch d.stage {
	case draftsDetail:
		if r, _, ok := d.collection.Find(d.opened); ok {
			return []string{"Garden", "Drafts", fmt.Sprintf("#%d", r.Item.Issue.Number)}
		}
	case draftsEditor, draftsSave:
		if d.edit != nil || d.savePreview != nil {
			n, _ := d.openNumberTitle()
			if d.savePreview != nil {
				n = d.savePreview.Number
			}
			if n == 0 {
				return []string{"Garden", "Drafts", "New idea", "Specification"}
			}
			last := "Specification"
			if d.stage == draftsSave {
				last = "Review revision"
			}
			return []string{"Garden", "Drafts", fmt.Sprintf("#%d", n), last}
		}
	case draftsConflict:
		return []string{"Garden", "Remote edit conflict"}
	case draftsValidate:
		if d.validate != nil {
			return []string{"Garden", "Drafts", fmt.Sprintf("#%d", d.validate.Number), "Validate specification"}
		}
	case draftsAssist:
		return m.gardenAssistCrumbs()
	case draftsForm:
		return []string{"Garden", "Drafts", "Capture a draft"}
	case draftsPreview:
		return []string{"Garden", "Drafts", "Capture a draft", "Review"}
	case draftsResult:
		return []string{"Garden", "Drafts", "Capture a draft", "Result"}
	}
	return []string{"Garden", "Drafts"}
}

func (m Model) gardenDraftsIdentity() string {
	d := m.garden.drafts
	switch d.stage {
	case draftsForm, draftsPreview, draftsResult:
		return "Capture a draft"
	case draftsEditor, draftsSave:
		if n, t := d.openNumberTitle(); n != 0 {
			return fmt.Sprintf("#%d  %s", n, boardTitle(t))
		} else if d.edit != nil && d.edit.key.issue == 0 {
			return "New idea  " + boardTitle(t)
		}
	case draftsAssist:
		return m.gardenAssistIdentity()
	case draftsConflict:
		return "Remote edit conflict"
	case draftsValidate:
		if d.validate != nil {
			return fmt.Sprintf("#%d  %s", d.validate.Number, boardTitle(d.validate.Title))
		}
	}
	id := d.selected
	if d.opened != 0 {
		id = d.opened
	}
	if r, _, ok := d.collection.Find(id); ok {
		return fmt.Sprintf("#%d  %s", r.Item.Issue.Number, boardTitle(r.Item.Issue.Title))
	}
	return "Drafts / empty"
}

func (m Model) gardenDraftsFocusLabel() string {
	switch m.garden.drafts.stage {
	case draftsDetail:
		return "Draft / Issue"
	case draftsForm, draftsPreview, draftsResult:
		return "Submenu / Capture a draft"
	case draftsEditor:
		return "Draft / Specification editor"
	case draftsSave:
		return "Draft / Review revision"
	case draftsConflict:
		return "Submenu / Remote edit conflict"
	case draftsValidate:
		return "Submenu / Validate specification"
	case draftsAssist:
		return m.gardenAssistFocusLabel()
	}
	return "Draft inbox"
}

func (m Model) gardenDraftsHints() []shellHint {
	g := m.garden
	d := g.drafts
	refresh := shellHint{m.keys.Refresh.Help().Key, "refresh"}
	if g.busy == busyPreviewCapture || g.busy == busyDocument || g.busy == busyPreviewSave || g.busy == busyPreviewValidate {
		return []shellHint{{"esc", "cancel"}}
	}
	switch d.stage {
	case draftsAssist:
		return m.gardenAssistHints()
	case draftsEditor:
		e := d.edit
		switch {
		case e == nil:
		case e.discard:
			return []shellHint{{"←/→", "button"}, {"enter", "choose"}, {"esc", "keep editing"}}
		case e.searching:
			return []shellHint{{"enter", "find"}, {"esc", "cancel"}}
		case e.text:
			return []shellHint{{"ctrl+s", "save"}, {"tab", "sections"}, {"esc", "sections"}}
		}
		h := []shellHint{{"↑/↓", "section"}, {"enter", "edit"}, {"/", "search"}, {"n/N", "next/prev"}, {"ctrl+s", "save"}, {"i", "AI"}, {"?", "keys"}}
		if e != nil && e.remote != nil {
			h = append(h, shellHint{"c", "compare"})
		}
		return append(h, shellHint{"esc", "back"})
	case draftsSave:
		return []shellHint{{"←/→", "button"}, {"enter", "choose"}, {"esc", "back"}}
	case draftsValidate:
		if d.validate != nil && d.validate.CanValidate() {
			return []shellHint{{"←/→", "button"}, {"enter", "choose"}, {"esc", "back"}}
		}
		return []shellHint{{"enter", "back"}, {"esc", "back"}}
	case draftsConflict:
		c := d.conflict
		if c == nil || c.intro {
			return []shellHint{{"enter", "inspect"}, {"esc", "back"}}
		}
		if c.editing {
			return []shellHint{{"esc", "keep result"}}
		}
		return []shellHint{{"↑/↓", "section"}, {"←/→", "pane"}, {"y/m/b", "take"}, {"e", "edit result"}, {"p", "publish"}, {"esc", "back"}}
	case draftsForm:
		if d.form.discard {
			return []shellHint{{"←/→", "button"}, {"enter", "choose"}, {"esc", "keep editing"}}
		}
		h := []shellHint{{"tab", "next field"}, {"shift+tab", "previous"}}
		if d.form.focus == fieldType {
			h = append(h, shellHint{"←/→", "type"})
		}
		if d.form.focus == fieldPreview {
			h = append(h, shellHint{"enter", "preview"})
		}
		return append(h, shellHint{"esc", "close"})
	case draftsPreview:
		return []shellHint{{"←/→", "button"}, {"enter", "choose"}, {"esc", "edit"}}
	case draftsResult:
		return []shellHint{{"←/→", "button"}, {"enter", "choose"}, {"esc", "back"}}
	case draftsDetail:
		return []shellHint{{"esc", "Drafts"}, {"↑/↓", "scroll"}, {"pgup/dn", "page"}, {"e", "edit"}, {"i", "AI draft"}, {"v", "validate"}, refresh}
	}
	if d.searching {
		return []shellHint{{"enter", "apply search"}, {"esc", "cancel"}}
	}
	h := []shellHint{{"↑/↓", "select"}, {"enter", "open"}, {"n", "new draft"}, {"i", "new with AI"}, {"/", "search"}, {"t", "type"}, {"x", "clear"}}
	if len(d.pending) > 0 {
		h = append(h, shellHint{"r", "reconcile"})
	}
	h = append(h, shellHint{m.keys.GardenView.Help().Key, "board"}, refresh, shellHint{"esc", "parent"})
	if m.canToggleSub() {
		h = append(h, shellHint{m.keys.Tab.Help().Key, "submenu"})
	}
	return h
}
