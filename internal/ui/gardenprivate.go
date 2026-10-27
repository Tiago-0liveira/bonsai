package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/drafts"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/spec"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// The private drafting workspace: capture an idea, shape it section by section, save it
// locally and reopen it, all without GitHub or an assistant. It is separate from editorDoc,
// which edits GitHub drafts, so that path stays untouched. Saves run in tea.Cmds that are
// ordered per draft: one in flight, later edits coalesce into one follow-up.

type privStage int

const (
	privCapture privStage = iota // one description field and two ways forward
	privEditor                   // the structured specification
	privReview                   // read-only preview of what an Issue would contain
	privPublish                  // confirm, then publish; also the screen of a draft that is publishing (gardenpublish.go)
)

type saveState int

const (
	saveClean saveState = iota
	saveDirty
	saveSaving
	saveFailed
	saveConflict
)

// privDebounce is how long edits settle before an autosave starts.
const privDebounce = 600 * time.Millisecond

// Capture focus.
const (
	capDesc = iota
	capContinue
	capDiscuss
	capCount
)

// privSection is one editable part of the specification. field is the fixed spec index,
// or -1 for an Extra section that is kept by title.
type privSection struct {
	title string
	field int
	text  string
}

var privPrimary = []privSection{{"Problem", 1, ""}, {"Outcome", 2, ""}, {"Requirements", 3, ""}, {"Criteria", 4, ""}, {"Questions", 6, ""}}
var privMore = []privSection{{"Summary", 0, ""}, {"Constraints", 5, ""}}

// privSectionsOf lists the five main sections, then Summary, Constraints and every extra section in order.
func privSectionsOf(s spec.Spec) []privSection {
	out := make([]privSection, 0, len(privPrimary)+len(privMore)+len(s.Extra))
	for _, group := range [][]privSection{privPrimary, privMore} {
		for _, e := range group {
			e.text = *s.Field(e.field)
			out = append(out, e)
		}
	}
	for _, x := range s.Extra {
		out = append(out, privSection{x.Title, -1, x.Text})
	}
	return out
}

// privSpecOf writes every entry back to its field, and extras to Extra in their original order.
func privSpecOf(secs []privSection) spec.Spec {
	var s spec.Spec
	for _, e := range secs {
		if e.field >= 0 {
			*s.Field(e.field) = e.text
			continue
		}
		s.Extra = append(s.Extra, spec.Section{Title: e.title, Text: e.text})
	}
	return s
}

type privateDraft struct {
	token uint64
	repo  garden.RepoID
	draft drafts.Draft // the last durable version; ID is empty until the first save
	stage privStage

	desc         textarea.Model
	title        textinput.Model
	typ          int
	secs         []privSection
	cur          int
	ta           textarea.Model
	taRef        string // the text the textarea was loaded with: only an actual edit is written back
	text         bool   // the section text has the keyboard
	foc          int    // capture: capDesc..; editor rail: sections, then title, type, invite, review
	titleEditing bool
	scroll       int // review

	// confirmed and decisions are the working copies of what the person settled with the assistant; they
	// are written with every save so a restart brings them back. pa is never persisted.
	confirmed []drafts.Answer
	decisions []drafts.Decision
	changed   map[int]bool // sections an apply just rewrote, until the person edits them or asks again
	pa        *privAssistant
	paGen     uint64 // the last request generation issued for this draft; it only grows
	pane      bool   // the discussion pane has the keyboard

	pub          *privPub
	pubGen       uint64 // the last publish request counter; like paGen it only grows
	previewing   bool   // a publish preview is being requested
	publishAfter bool   // publish once the text on screen is stored

	gen, savedGen uint64
	inflight      bool
	state         saveState
	err           *gardenapi.Error
	conflict      *drafts.ConflictError
	notice        string
	leaveAfter    bool
	quitAfter     bool
	seeded        bool
}

func newPrivateDraft(repo garden.RepoID, token uint64, d drafts.Draft, stage privStage) *privateDraft {
	p := &privateDraft{token: token, repo: repo, draft: d, stage: stage}
	p.desc = newTextarea()
	p.desc.SetValue(d.Description)
	p.title = textinput.New()
	p.title.Cursor.SetMode(cursor.CursorStatic)
	p.title.Prompt = ""
	p.title.CharLimit = 256
	p.title.SetValue(d.Title)
	for i, t := range captureTypes {
		if t == d.Type {
			p.typ = i
		}
	}
	p.secs = privSectionsOf(d.Spec)
	p.confirmed = append([]drafts.Answer(nil), d.ConfirmedAnswers...)
	p.decisions = append([]drafts.Decision(nil), d.Decisions...)
	p.ta = newTextarea()
	p.load(0)
	if stage == privCapture {
		p.desc.Focus()
	}
	if d.Publication.State != drafts.Unpublished && stage != privCapture {
		// Content is frozen while publishing and after: the draft opens on its publication, not the editor.
		p.stage, p.pub = privPublish, &privPub{}
	} else if p.stage == privPublish {
		p.stage = privReview // the publication was undone elsewhere: nothing to confirm yet
	}
	return p
}

// adopt carries the discussion over to a draft re-read from the store: the same conversation, the newer text.
func (p *privateDraft) adopt(old *privateDraft) {
	// The counter never goes back, so no later request can share an identity with an earlier reply.
	p.paGen, p.pubGen = old.paGen, old.pubGen
	p.pa, p.pane = old.pa, old.pane
	if p.pa != nil {
		p.pa.s.private = p
	}
}

// load shows section i in the text area.
func (p *privateDraft) load(i int) {
	p.cur = max(0, min(i, len(p.secs)-1))
	p.ta.SetValue(p.secs[p.cur].text)
	textareaTop(&p.ta)
	p.taRef = p.ta.Value()
}

// flush stores the visible text into its section when the user changed it. A section the user never
// touched keeps its exact stored text, so nothing is rewritten by merely viewing it.
func (p *privateDraft) flush() {
	if v := p.ta.Value(); v != p.taRef {
		p.secs[p.cur].text, p.taRef = v, v
	}
}

func (p *privateDraft) selectSection(i int) {
	if i = max(0, min(i, len(p.secs)-1)); i == p.cur {
		return
	}
	p.flush()
	p.load(i)
}

// build is the draft a save would write.
func (p *privateDraft) build() drafts.Draft {
	p.flush()
	d := p.draft
	d.Description = p.desc.Value()
	d.Title = strings.TrimSpace(p.title.Value())
	d.Type = captureTypes[p.typ]
	d.Spec = privSpecOf(p.secs)
	d.ConfirmedAnswers = append([]drafts.Answer(nil), p.confirmed...)
	d.Decisions = append([]drafts.Decision(nil), p.decisions...)
	return d
}

func (p *privateDraft) blank() bool {
	d := p.build()
	if strings.TrimSpace(d.Description) != "" || d.Title != "" {
		return false
	}
	for _, e := range p.secs {
		if strings.TrimSpace(e.text) != "" {
			return false
		}
	}
	return true
}

func (p *privateDraft) assess() drafts.Readiness { return drafts.Assess(p.build()) }

// unwritten says edits exist that no durable save holds.
func (p *privateDraft) unwritten() bool {
	if p.state == saveFailed || p.state == saveConflict {
		return true
	}
	return p.gen != p.savedGen
}

func (p *privateDraft) label() string {
	if p.draft.ID == "" {
		return "New draft"
	}
	return drafts.DraftLabel(p.draft.ID)
}

// typing says a text field owns the keyboard.
func (p *privateDraft) typing() bool {
	if p.state == saveConflict {
		return false
	}
	switch p.stage {
	case privCapture:
		return p.foc == capDesc
	case privEditor:
		// The invitation and the discussion pane own every key while they are in front.
		return p.text || p.titleEditing || (p.pa != nil && (p.pa.stage == paInvite || p.pane))
	}
	return false
}

func (p *privateDraft) status() string {
	switch p.state {
	case saveSaving:
		return "Saving…"
	case saveDirty:
		return "Unsaved changes"
	case saveFailed:
		return "Save failed — text kept"
	case saveConflict:
		return "Conflict — choose a version"
	}
	if p.draft.ID == "" {
		return "Nothing to save yet"
	}
	return "✓ Saved locally"
}

// Messages ---------------------------------------------------------------------

type (
	privTickMsg  struct{ token, gen uint64 }
	privSavedMsg struct {
		repo  garden.RepoID
		token uint64
		gen   uint64
		draft drafts.Draft
		err   error
	}
	privListMsg struct {
		repo   garden.RepoID
		seq    uint64
		all    bool
		drafts []drafts.Draft
		err    error
	}
	privOpenMsg struct {
		repo  garden.RepoID
		seq   uint64
		id    drafts.ID
		draft drafts.Draft
		err   error
	}
	privArchiveMsg struct {
		repo      garden.RepoID
		unarchive bool
		draft     drafts.Draft
		err       error
	}
	privRememberedMsg struct{ err error }
)

// privTickCmd is the autosave debounce. Tests skip it by name and deliver the tick themselves.
func privTickCmd(token, gen uint64) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(privDebounce)
		return privTickMsg{token, gen}
	}
}

// privateDrafts is the optional private store, only while a repository is chosen.
func (m Model) privateDrafts() (gardenapi.PrivateDrafts, bool) {
	g := m.garden
	if g.api == nil || g.status.Target.RepoID == 0 || g.drafts.privUnavailable {
		return nil, false
	}
	api, ok := g.api.(gardenapi.PrivateDrafts)
	return api, ok
}

func (m Model) privRepo() garden.RepoID { return garden.RepoID(m.garden.status.Target.RepoID) }

// privUnavailable records that the store cannot be used: private rows disappear and n is the
// legacy capture again. It says so once.
func (m *Model) privUnavailable() {
	d := &m.garden.drafts
	if d.privUnavailable {
		return
	}
	d.privUnavailable = true
	d.privList = nil
	d.notice = "Private drafts are unavailable on this machine; capturing through GitHub instead."
}

// Lists ------------------------------------------------------------------------

// privListCmd reads the private drafts. A newer request supersedes an older reply.
func (m *Model) privListCmd() tea.Cmd {
	api, ok := m.privateDrafts()
	if !ok || m.garden.sub != subDrafts {
		return nil
	}
	d := &m.garden.drafts
	if !d.restoreRead {
		d.restoreRead = true
		if m.state != nil {
			d.restoreID, d.restoreSection = m.state.GardenDraft(m.repoDir)
			d.rememberedID, d.rememberedSection = d.restoreID, d.restoreSection
		}
	}
	d.privSeq++
	seq, repo, all := d.privSeq, m.privRepo(), d.showArchived
	return func() tea.Msg {
		list, err := api.ListPrivateDrafts(context.Background(), repo, all)
		return privListMsg{repo: repo, seq: seq, all: all, drafts: list, err: err}
	}
}

func (m Model) onPrivList(msg privListMsg) (Model, tea.Cmd) {
	d := &m.garden.drafts
	if msg.seq != d.privSeq || msg.repo != m.privRepo() {
		return m, nil
	}
	if errors.Is(msg.err, drafts.ErrUnavailable) {
		m.privUnavailable()
		return m, nil
	}
	if msg.err != nil {
		// The last good list stays; only a successful read, even an empty one, replaces it.
		d.privErr = safeGardenError(msg.err)
		return m, nil
	}
	d.privErr = nil
	first := !d.privLoaded
	d.privList = msg.drafts
	d.privLoaded = true
	if !d.restoreDone {
		d.restoreDone = true
		if id := drafts.ID(d.restoreID); id != "" && d.localIndex(id) >= 0 {
			d.selectedLocal, d.selected = id, 0
			first = false
		}
	}
	if first && d.selectedLocal == "" {
		// The first read lists local drafts above the GitHub ones: start on the top row.
		if locals := d.locals(); len(locals) > 0 {
			d.selectedLocal, d.selected = locals[0].ID, 0
		}
	}
	d.reselect(m.garden.board.snapshot)
	m.gardenDraftsVisible()
	return m.privCheckRemote()
}

// privCheckRemote compares the open draft with the list: another process wrote a newer revision.
// A clean editor reloads silently; unsaved text is saved against its old revision, which makes the
// store keep it as a recovery draft and puts the editor in the conflict state.
func (m Model) privCheckRemote() (Model, tea.Cmd) {
	d := &m.garden.drafts
	p := d.private
	if p == nil || p.draft.ID == "" || p.inflight || p.state == saveConflict || (p.pub != nil && p.pub.running) {
		return m, nil
	}
	for _, cur := range d.privList {
		if cur.ID != p.draft.ID || cur.Revision <= p.draft.Revision {
			continue
		}
		if p.stage == privPublish && p.pub != nil && p.pub.result != nil {
			// What the publication reported stays on screen; only the stored copy moves on.
			p.draft = cur
			return m, nil
		}
		p.dropPreview("The draft changed; review it again.")
		if !p.unwritten() {
			sec := p.cur
			np := newPrivateDraft(p.repo, p.token, cur, p.stage)
			np.load(sec)
			np.foc, np.text, np.notice = p.foc, p.text, p.notice
			np.adopt(p)
			d.private = np
			return m.privFocusSync(), nil
		}
		return m.privStartSave(p)
	}
	return m, nil
}

// Opening ----------------------------------------------------------------------

func (m Model) privNew() (tea.Model, tea.Cmd) {
	d := &m.garden.drafts
	d.notice = ""
	if p := d.private; p != nil && (p.draft.ID == "" || p.unwritten()) {
		// Unwritten text is never replaced by a fresh draft.
		d.stage = draftsPrivate
		return m.privFocusSync(), nil
	}
	d.privToken++
	d.private = newPrivateDraft(m.privRepo(), d.privToken, drafts.Draft{Type: garden.TypeFeature}, privCapture)
	d.stage = draftsPrivate
	return m.privFocusSync(), nil
}

// privOpen reads the draft again so another process's newer revision shows.
func (m Model) privOpen(id drafts.ID) (tea.Model, tea.Cmd) {
	api, ok := m.privateDrafts()
	if !ok {
		return m, nil
	}
	d := &m.garden.drafts
	d.openSeq++
	seq, repo := d.openSeq, m.privRepo()
	return m, func() tea.Msg {
		cur, err := api.ReadPrivateDraft(context.Background(), repo, id)
		return privOpenMsg{repo: repo, seq: seq, id: id, draft: cur, err: err}
	}
}

func (m Model) onPrivOpen(msg privOpenMsg) (Model, tea.Cmd) {
	d := &m.garden.drafts
	if msg.seq != d.openSeq || msg.repo != m.privRepo() {
		return m, nil
	}
	if msg.err != nil {
		if errors.Is(msg.err, drafts.ErrUnavailable) {
			m.privUnavailable()
			return m, nil
		}
		d.notice = "Could not open that draft: " + safeGardenError(msg.err).Message
		return m, nil
	}
	cur := msg.draft
	if p := d.private; p != nil && p.draft.ID == cur.ID {
		d.stage = draftsPrivate
		if cur.Revision > p.draft.Revision && !p.inflight && !(p.pub != nil && p.pub.running) {
			if p.unwritten() {
				m, cmd := m.privStartSave(p)
				return m.privFocusSync(), cmd
			}
			// A clean editor reloads in place, keeping the section and focus the person was on.
			old := p
			p = newPrivateDraft(p.repo, p.token, cur, old.stage)
			p.load(old.cur)
			p.foc, p.text = old.foc, old.text
			p.adopt(old)
			d.private = p
		}
		return m.privFocusSync(), nil
	}
	if p := d.private; p != nil && p.unwritten() {
		d.notice = "Finish saving " + p.label() + " before opening another draft."
		d.stage = draftsPrivate
		return m.privFocusSync(), nil
	}
	d.privToken++
	p := newPrivateDraft(m.privRepo(), d.privToken, cur, privEditor)
	if string(cur.ID) == d.restoreID && !d.sectionUsed {
		d.sectionUsed = true
		p.load(d.restoreSection)
		p.foc = p.cur
	}
	d.private = p
	d.stage = draftsPrivate
	rem := m.privRemember() // first: it records what was remembered on m
	return m.privFocusSync(), rem
}

// privRemember persists which draft and section are open, never any text.
func (m *Model) privRemember() tea.Cmd {
	d := &m.garden.drafts
	p := d.private
	if m.state == nil || p == nil || p.draft.ID == "" {
		return nil
	}
	id := string(p.draft.ID)
	if d.rememberedID == id && d.rememberedSection == p.cur {
		return nil
	}
	d.rememberedID, d.rememberedSection = id, p.cur
	d.saveGen++
	gen, st, dir, sec := d.saveGen, m.state, m.repoDir, p.cur
	return func() tea.Msg { return privRememberedMsg{st.PersistGardenDraft(gen, dir, id, sec)} }
}

// privFocusSync gives text the keyboard exactly while a text field is focused, so letters such
// as q, n and ? are text there and shell shortcuts everywhere else.
func (m Model) privFocusSync() Model {
	d := &m.garden.drafts
	if d.stage != draftsPrivate || d.private == nil {
		return m
	}
	p := d.private
	if p.typing() {
		m.shellFocus = shellInput
	} else {
		m.shellFocus = shellBody
	}
	return m
}

func (m Model) privSize() {
	p := m.garden.drafts.private
	if p == nil {
		return
	}
	w, h := privCaptureBox(m.width, m.gardenBoardRows())
	p.desc.SetWidth(w)
	p.desc.SetHeight(h)
	tw, th := m.privEditorText()
	p.ta.SetWidth(tw)
	p.ta.SetHeight(th)
	p.title.Width = max(8, min(m.width-30, 80))
	if p.paActive() {
		// The answer field takes the height the pane's fixed parts leave.
		pw, ph := m.paDims()
		l := m.paLayoutOf(p, pw-2, ph)
		g := p.pa.s.panelGeometry(pw-2, l.panelRows)
		p.pa.s.qScroll = g.scroll
		p.pa.s.ta.SetWidth(max(4, pw-4))
		p.pa.s.ta.SetHeight(max(1, g.taH))
		p.pa.s.ta.Placeholder = "Your answer"
		if q, ok := p.pa.s.current(); ok {
			if sel := p.pa.s.answer(q).sel; sel == selChoice || sel == selUnknown {
				p.pa.s.ta.Placeholder = "Add notes"
			}
		}
	}
}

// Saving -----------------------------------------------------------------------

// privEdited records an edit: the text is dirty until a save of this generation lands.
func (m Model) privEdited(p *privateDraft) (Model, tea.Cmd) {
	p.gen++
	p.dropPreview("The draft changed; review it again.")
	p.err = nil
	if p.draft.ID == "" && !p.inflight && p.blank() {
		// Nothing worth keeping yet: the first non-empty edit creates the draft.
		p.savedGen, p.state = p.gen, saveClean
		return m, nil
	}
	if p.state != saveSaving {
		p.state = saveDirty
	}
	return m, privTickCmd(p.token, p.gen)
}

func (m Model) onPrivTick(msg privTickMsg) (Model, tea.Cmd) {
	p := m.garden.drafts.private
	if p == nil || p.token != msg.token || p.gen != msg.gen || p.inflight || p.state == saveConflict || p.gen == p.savedGen {
		return m, nil
	}
	return m.privStartSave(p)
}

// privStartSave writes the current text with the revision the editor last saw. Only one save per
// draft is ever in flight; the reply starts the next one if more edits arrived.
func (m Model) privStartSave(p *privateDraft) (Model, tea.Cmd) {
	api, ok := m.privateDrafts()
	if !ok || p.inflight {
		return m, nil
	}
	if p.draft.ID == "" && p.blank() {
		p.savedGen, p.state = p.gen, saveClean
		return m, nil
	}
	d, expected, gen, repo, token := p.build(), p.draft.Revision, p.gen, p.repo, p.token
	p.inflight, p.state, p.err = true, saveSaving, nil
	return m, func() tea.Msg {
		out, err := api.SavePrivateDraft(context.Background(), repo, d, expected)
		return privSavedMsg{repo: repo, token: token, gen: gen, draft: out, err: err}
	}
}

func (m Model) onPrivSaved(msg privSavedMsg) (Model, tea.Cmd) {
	d := &m.garden.drafts
	p := d.private
	if p == nil || p.token != msg.token || p.repo != msg.repo {
		return m, nil
	}
	p.inflight = false
	if msg.err != nil {
		p.leaveAfter, p.publishAfter = false, false
		var ce *drafts.ConflictError
		switch {
		case errors.As(msg.err, &ce):
			if p.quitAfter {
				p.quitAfter = false
				p.notice = "Another window changed this draft, so Bonsai did not quit. Choose a version first."
			}
			p.state, p.conflict, p.text, p.titleEditing = saveConflict, ce, false, false
			d.privList = privUpsert(d.privList, ce.Incoming)
			m = m.privFocusSync()
			return m, nil
		case errors.Is(msg.err, drafts.ErrUnavailable):
			m.privUnavailable()
		}
		p.state, p.err = saveFailed, privSaveError(msg.err)
		if p.pa != nil {
			p.pa.startNext = false
		}
		if p.quitAfter {
			p.quitAfter = false
			p.notice = "Could not save, so Bonsai did not quit. Your text is kept."
		}
		return m, nil
	}
	p.draft, p.savedGen = msg.draft, msg.gen
	d.privList = privUpsert(d.privList, msg.draft)
	if p.gen != p.savedGen {
		return m.privStartSave(p) // edits arrived while this save ran: exactly one follow-up
	}
	p.state, p.err = saveClean, nil
	cmd := m.privRemember()
	if pa := p.pa; pa != nil && pa.startNext {
		// The invitation was waiting for a stored draft to scope its request to.
		pa.startNext = false
		m, start := m.paStart(p)
		return m, tea.Batch(cmd, start)
	}
	switch {
	case p.publishAfter:
		p.publishAfter = false
		m, pcmd := m.privPublishStart(p)
		return m, tea.Batch(cmd, pcmd)
	case p.quitAfter:
		p.quitAfter = false
		nm, qcmd := m.quit()
		return nm.(Model), tea.Batch(cmd, qcmd)
	case p.leaveAfter:
		p.leaveAfter = false
		m, lcmd := m.privLeave()
		return m, tea.Batch(cmd, lcmd)
	}
	return m, cmd
}

// privSaveError keeps a safe backend message and otherwise names the real problem: the file is on this machine.
func privSaveError(err error) *gardenapi.Error {
	var ge *gardenapi.Error
	if errors.As(err, &ge) && ge != nil {
		return ge
	}
	return &gardenapi.Error{Code: gardenapi.CodeFailed, Message: "Could not write the draft to this machine."}
}

// privUpsert replaces the draft with the same ID, or adds it.
func privUpsert(list []drafts.Draft, d drafts.Draft) []drafts.Draft {
	if d.ID == "" {
		return list
	}
	for i := range list {
		if list[i].ID == d.ID {
			list[i] = d
			return list
		}
	}
	return append(list, d)
}

// privFlush is Ctrl+S: save now, whatever the debounce says.
func (m Model) privFlush(p *privateDraft) (Model, tea.Cmd) {
	switch {
	case p.state == saveConflict:
		p.notice = "Choose a version first: k keeps yours, o opens theirs."
		return m, nil
	case p.inflight:
		return m, nil // the reply starts a follow-up when more edits exist
	case p.gen == p.savedGen:
		if p.draft.ID != "" {
			p.notice = "Already saved locally."
		}
		return m, nil
	}
	return m.privStartSave(p)
}

// privLeave returns to the inbox. Unsaved text is saved first; text that cannot be saved stays
// parked in memory, visible to the quit guard.
func (m Model) privLeave() (Model, tea.Cmd) {
	d := &m.garden.drafts
	p := d.private
	if p == nil {
		d.stage = draftsList
		return m, nil
	}
	if p.gen != p.savedGen && p.state != saveFailed && p.state != saveConflict {
		p.leaveAfter = true
		if p.inflight {
			return m, nil
		}
		return m.privStartSave(p)
	}
	d.stage = draftsList
	m = m.paClose(p) // leaving ends any request: its reply would have nowhere to land
	if p.draft.ID != "" {
		d.selectedLocal, d.selected = p.draft.ID, 0 // the inbox opens on the draft just left
	}
	if p.state == saveFailed || p.state == saveConflict {
		d.notice = fmt.Sprintf("%s has edits that are not saved. Reopen it to keep going.", p.label())
	} else {
		d.private = nil
	}
	m.shellFocus = shellBody
	list := m.privListCmd()
	return m, list
}

// Keys -------------------------------------------------------------------------

func isReviewKey(k string) bool { return k == "ctrl+enter" || k == "ctrl+r" }

func (m Model) privKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	d := &m.garden.drafts
	p := d.private
	if p == nil {
		d.stage = draftsList
		m.shellFocus = shellBody
		return m, nil
	}
	m.privSize()
	k := msg.String()
	var nm Model
	var cmd tea.Cmd
	switch {
	case p.state == saveConflict:
		nm, cmd = m.privConflictKey(p, k)
	case k == "ctrl+s":
		p.notice = ""
		nm, cmd = m.privFlush(p)
	case p.stage == privCapture:
		nm, cmd = m.privCaptureKey(p, msg)
	case p.stage == privReview:
		nm, cmd = m.privReviewKey(p, k)
	case p.stage == privPublish:
		nm, cmd = m.privPublishKey(p, k)
	default:
		nm, cmd = m.privEditorKey(p, msg)
	}
	nm = nm.privFocusSync()
	nm.privSize()
	rem := nm.privRemember()
	return nm, tea.Batch(cmd, rem)
}

func (m Model) privConflictKey(p *privateDraft, k string) (Model, tea.Cmd) {
	switch k {
	case "k":
		// Keep mine: write over what is stored now.
		p.draft = p.conflict.Current
		p.conflict, p.state = nil, saveDirty
		return m.privStartSave(p)
	case "o":
		cur := p.conflict.Current
		m = m.paClose(p) // a discussion about the text that was set aside has nothing to say about theirs
		np := newPrivateDraft(p.repo, p.token, cur, privEditor)
		np.adopt(p) // the discussion is closed above; only the request counter carries over
		np.load(p.cur)
		np.foc = np.cur
		np.notice = "Opened the other version. Your text stays in the inbox as " + drafts.DraftLabel(p.conflict.Incoming.ID) + "."
		m.garden.drafts.private = np
		list := m.privListCmd()
		return m, list
	case "esc":
		return m.privLeave()
	}
	return m, nil
}

func (m Model) privCaptureKey(p *privateDraft, msg tea.KeyMsg) (Model, tea.Cmd) {
	k := msg.String()
	switch k {
	case "esc":
		return m.privLeave()
	case "tab", "shift+tab":
		step := 1
		if k == "shift+tab" {
			step = capCount - 1
		}
		p.foc = (p.foc + step) % capCount
		if p.foc == capDesc {
			p.desc.Focus()
		} else {
			p.desc.Blur()
		}
		return m, nil
	}
	if p.foc != capDesc {
		if k == "enter" {
			if p.foc == capContinue {
				return m.privContinue(p)
			}
			// Discuss with AI: the same draft in the editor, with the invitation in front.
			m, cmd := m.privContinue(p)
			m, inv := m.privInvite(p)
			return m, tea.Batch(cmd, inv)
		}
		return m, nil
	}
	p.notice = ""
	before := p.desc.Value()
	var cmd tea.Cmd
	p.desc, cmd = p.desc.Update(msg)
	if p.desc.Value() == before {
		return m, cmd
	}
	m, tick := m.privEdited(p)
	return m, tea.Batch(cmd, tick)
}

// privContinue turns the capture into the structured editor of the same draft. The description
// seeds the Problem section once, when that section is still empty.
func (m Model) privContinue(p *privateDraft) (Model, tea.Cmd) {
	p.stage, p.foc, p.text = privEditor, 0, false
	p.desc.Blur()
	var cmd tea.Cmd
	if !p.seeded && strings.TrimSpace(p.secs[0].text) == "" && strings.TrimSpace(p.desc.Value()) != "" {
		p.seeded = true
		p.flush()
		p.secs[0].text = p.desc.Value()
		p.load(0)
		m, cmd = m.privEdited(p)
	}
	p.seeded = true
	return m, cmd
}

// Editor focus: sections first, then these.
func (p *privateDraft) focTitle() int  { return len(p.secs) }
func (p *privateDraft) focType() int   { return len(p.secs) + 1 }
func (p *privateDraft) focInvite() int { return len(p.secs) + 2 }
func (p *privateDraft) focReview() int { return len(p.secs) + 3 }

func (m Model) privCompact() bool { return m.width < 100 }

// privNext is the focus after tab (dir 1) or shift+tab (dir -1). While text is being written the
// walk stays inside the sections; on the rail it also visits Title, Type, the assistant and Review. The
// compact layout shows the assistant as a tab, so it is a stop there too.
func (m Model) privNext(p *privateDraft, dir int) int {
	n := p.focReview() + 1
	if p.text {
		return (p.cur + dir + len(p.secs)) % len(p.secs)
	}
	for i := 1; i <= n; i++ {
		f := ((p.foc+dir*i)%n + n) % n
		return f
	}
	return p.foc
}

func (m Model) privEditorKey(p *privateDraft, msg tea.KeyMsg) (Model, tea.Cmd) {
	k := msg.String()
	switch {
	case isReviewKey(k):
		return m.privOpenReview(p), nil
	case k == "ctrl+g":
		return m.privTogglePane(p)
	case p.pa != nil && (p.pa.stage == paInvite || p.pane):
		return m.paKey(p, msg)
	case k == "ctrl+t":
		p.titleEditing, p.text = true, false
		p.title.Focus()
		return m, nil
	case k == "ctrl+y":
		p.typ = (p.typ + 1) % len(captureTypes)
		return m.privEdited(p)
	}
	if p.titleEditing {
		switch k {
		case "enter", "esc", "tab":
			p.titleEditing = false
			p.title.Blur()
			return m, nil
		}
		before := p.title.Value()
		var cmd tea.Cmd
		p.title, cmd = p.title.Update(msg)
		if p.title.Value() == before {
			return m, cmd
		}
		m, tick := m.privEdited(p)
		return m, tea.Batch(cmd, tick)
	}
	p.notice = ""
	switch k {
	case "tab", "shift+tab":
		dir := 1
		if k == "shift+tab" {
			dir = -1
		}
		f := m.privNext(p, dir)
		p.foc = f
		if f == p.focInvite() && p.pa != nil {
			p.pa.unseen = false // the discussion is now in front (compact) or marked as the stop (side by side)
		}
		if f < len(p.secs) {
			p.selectSection(f)
		}
		return m, nil
	case "esc":
		if p.text {
			p.text = false
			p.ta.Blur()
			return m, nil
		}
		return m.privLeave()
	}
	if p.text {
		before := p.ta.Value()
		var cmd tea.Cmd
		p.ta, cmd = p.ta.Update(msg)
		if p.ta.Value() == before {
			return m, cmd
		}
		delete(p.changed, p.cur) // the person's own edit: no longer "just changed by the assistant"
		m, tick := m.privEdited(p)
		return m, tea.Batch(cmd, tick)
	}
	switch k {
	case "up", "k":
		if p.foc > 0 && p.foc <= len(p.secs) {
			p.foc--
			p.selectSection(p.foc)
		}
	case "down", "j":
		if p.foc < len(p.secs)-1 {
			p.foc++
			p.selectSection(p.foc)
		}
	case "enter", "right", "l":
		switch {
		case p.foc < len(p.secs):
			p.text = true
			p.ta.Focus()
		case p.foc == p.focTitle():
			p.titleEditing = true
			p.title.Focus()
		case p.foc == p.focType():
			p.typ = (p.typ + 1) % len(captureTypes)
			return m.privEdited(p)
		case p.foc == p.focInvite():
			return m.privInvite(p)
		default:
			return m.privOpenReview(p), nil
		}
	}
	return m, nil
}

// privTogglePane is Ctrl+G: from the text, the rail or the pane itself it moves the keyboard to the discussion
// and back. Without a discussion it opens the invitation.
func (m Model) privTogglePane(p *privateDraft) (Model, tea.Cmd) {
	if p.pa == nil {
		return m.privInvite(p)
	}
	if p.pa.stage == paInvite {
		return m, nil
	}
	if p.pane {
		p.pane = false
		return m, nil
	}
	p.text, p.titleEditing = false, false
	p.ta.Blur()
	p.pane, p.pa.unseen = true, false
	p.foc = p.focInvite()
	return m, nil
}

func (m Model) privOpenReview(p *privateDraft) Model {
	p.flush()
	p.stage, p.text, p.titleEditing, p.scroll = privReview, false, false, 0
	p.ta.Blur()
	return m
}

func (m Model) privReviewKey(p *privateDraft, k string) (Model, tea.Cmd) {
	page := max(1, m.gardenBoardRows()-8)
	switch k {
	case "esc":
		p.stage, p.notice = privEditor, ""
	case "up", "k":
		p.scroll = max(0, p.scroll-1)
	case "down", "j":
		p.scroll++
	case "pgup":
		p.scroll = max(0, p.scroll-page)
	case "pgdown":
		p.scroll += page
	case "home":
		p.scroll = 0
	case "end":
		p.scroll = 1 << 20
	case "enter":
		return m.privPublishStart(p)
	}
	return m, nil
}

const privPublishUnavailable = "Creating the GitHub issue is not available yet. Your draft stays saved locally."

// Quit recovery ------------------------------------------------------------------

// privQuitGuard opens the dialog when unwritten edits would be lost by quitting.
func (m Model) privQuitGuard() (Model, bool) {
	p := m.garden.drafts.private
	if p == nil || !p.unwritten() || (p.draft.ID == "" && p.blank()) {
		return m, false
	}
	modal := modals.NewSelect(modals.KindGardenDraftQuit, "Unwritten draft edits", []string{"Save and quit", "Quit without saving", "Cancel"})
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m, true
}

func (m Model) privQuitChoice(choice string) (tea.Model, tea.Cmd) {
	d := &m.garden.drafts
	p := d.private
	switch choice {
	case "Quit without saving":
		d.private = nil
		return m.quit()
	case "Save and quit":
		if p == nil {
			return m.quit()
		}
		if p.state == saveConflict {
			p.notice = "Choose a version first: k keeps yours, o opens theirs."
			return m, nil
		}
		if _, ok := m.privateDrafts(); !ok {
			p.state, p.err = saveFailed, &gardenapi.Error{Code: gardenapi.CodeFailed, Message: "Private drafts are unavailable, so Bonsai did not quit."}
			return m, nil
		}
		p.quitAfter = true
		if p.inflight {
			return m, nil
		}
		return m.privStartSave(p)
	}
	return m, nil
}

// Archive ------------------------------------------------------------------------

func (m Model) privArchive(l drafts.Draft, unarchive bool) (tea.Model, tea.Cmd) {
	api, ok := m.privateDrafts()
	if !ok {
		return m, nil
	}
	repo := m.privRepo()
	return m, func() tea.Msg {
		var out drafts.Draft
		var err error
		if unarchive {
			out, err = api.UnarchivePrivateDraft(context.Background(), repo, l.ID, l.Revision)
		} else {
			out, err = api.ArchivePrivateDraft(context.Background(), repo, l.ID, l.Revision)
		}
		return privArchiveMsg{repo: repo, unarchive: unarchive, draft: out, err: err}
	}
}

func (m Model) onPrivArchive(msg privArchiveMsg) (Model, tea.Cmd) {
	d := &m.garden.drafts
	if msg.repo != m.privRepo() {
		return m, nil
	}
	var ce *drafts.ConflictError
	switch {
	case errors.As(msg.err, &ce):
		d.privList = privUpsert(d.privList, ce.Current)
		d.notice = "Changed in another Bonsai window; nothing was archived. Check " + drafts.DraftLabel(ce.Current.ID) + " and try again."
		return m, nil
	case msg.err != nil:
		d.notice = "Could not update that draft: " + safeGardenError(msg.err).Message
		return m, nil
	}
	for _, cur := range d.privList {
		if cur.ID == msg.draft.ID && cur.Revision >= msg.draft.Revision {
			return m, nil // a newer read already holds this draft: the reply is stale
		}
	}
	label := drafts.DraftLabel(msg.draft.ID)
	if msg.unarchive {
		d.lastArchived = nil
		d.notice = "Restored " + label + "."
	} else {
		a := msg.draft
		d.lastArchived = &a
		d.notice = "Archived " + label + ". u to undo."
	}
	d.privList = privUpsert(d.privList, msg.draft)
	if p := d.private; p != nil && p.draft.ID == msg.draft.ID && p.state == saveClean {
		p.draft = msg.draft
	}
	d.reselect(m.garden.board.snapshot)
	m.gardenDraftsVisible()
	return m, nil
}

// Dispatch -----------------------------------------------------------------------

// privMsg handles the private draft replies.
func (m Model) privMsg(msg tea.Msg) (handled bool, nm Model, cmd tea.Cmd) {
	switch msg := msg.(type) {
	case privTickMsg:
		nm, cmd = m.onPrivTick(msg)
	case privSavedMsg:
		nm, cmd = m.onPrivSaved(msg)
	case privListMsg:
		nm, cmd = m.onPrivList(msg)
	case privOpenMsg:
		nm, cmd = m.onPrivOpen(msg)
	case privArchiveMsg:
		nm, cmd = m.onPrivArchive(msg)
	case privAssistMsg:
		nm, cmd = m.onPrivAssist(msg)
	case privAssistTickMsg:
		nm, cmd = m.onPrivAssistTick(msg)
	case privPubPreviewMsg:
		nm, cmd = m.onPrivPubPreview(msg)
	case privPubDoneMsg:
		nm, cmd = m.onPrivPubDone(msg)
	case privRememberedMsg:
		return true, m, nil
	default:
		return false, m, nil
	}
	return true, nm, cmd
}
