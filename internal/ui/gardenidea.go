package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// The save of an AI-drafted new idea: one confirmation for two existing writes. Capture creates the Issue at Draft
// with the original description as its body; the first specification revision follows through the ordinary save
// preview, so a conflict stops on the standard screens. The editor of the idea has key Issue 0 until the Issue exists.

type ideaSave struct {
	active bool
	op     string         // the Capture operation this save is waiting for
	issue  garden.IssueID // the created Issue, 0 until Capture is done
	number int64
}

// gardenIdeaSave is ctrl+s on the editor of a new idea: the capture form, prefilled, comes first.
func (m Model) gardenIdeaSave(e *editorDoc) (tea.Model, tea.Cmd) {
	d := &m.garden.drafts
	if d.idea.issue != 0 {
		// The Issue exists already: continue from reading it.
		return m.gardenLoadDocument(d.idea.issue, docIdea)
	}
	if ok, why := m.canCapture(); !ok {
		e.notice = why
		return m, nil
	}
	if !d.idea.active {
		if d.formOpen && d.form.dirty() {
			e.notice = "Finish or discard the unfinished capture first (n on the Drafts list)."
			return m, nil
		}
		f := newCaptureForm()
		f.title.SetValue(e.title)
		s := m.garden.assist.sessions[e.key]
		if s != nil {
			f.desc.SetValue(s.desc.Value())
			if s.proposal != nil {
				for i, t := range captureTypes {
					if t == s.proposal.Type {
						f.typ = i
					}
				}
			}
		}
		f.setFocus(fieldPreview)
		d.form, d.formOpen = f, true
	}
	d.idea.active = true
	d.stage = draftsForm
	d.btn = 0
	m.shellFocus = shellInput
	m.gardenSizeForm()
	return m, nil
}

// gardenIdeaBack returns from the capture form to the editor; the form keeps what was typed.
func (m Model) gardenIdeaBack() Model {
	d := &m.garden.drafts
	d.stage = draftsEditor
	m.shellFocus = shellInput
	m.gardenSizeEditor()
	return m
}

// gardenIdeaCaptured continues after Capture: the new Issue's document is read, the editor re-keyed to it.
func (m Model) gardenIdeaCaptured(v gardenapi.CaptureView) (Model, tea.Cmd) {
	g := &m.garden
	d := &g.drafts
	id := garden.IssueID(v.Issue.ID)
	d.idea.issue, d.idea.number = id, v.Issue.Number
	d.selectAfter = id
	d.closeCapture()
	k0 := m.editKeyFor(0)
	if s := g.assist.sessions[k0]; s != nil {
		// The idea is an Issue now: its session follows it, so it ends with the save.
		delete(g.assist.sessions, k0)
		s.key, s.isNew, s.number, s.title = m.editKeyFor(id), false, v.Issue.Number, v.Title
		g.assist.sessions[s.key] = s
	}
	if e := d.editors[k0]; e != nil {
		d.edit, d.stage = e, draftsEditor
		m.shellFocus = shellInput
	}
	return m.gardenLoadDocument(id, docIdea)
}

// gardenIdeaRekey gives the idea's editor the new Issue and the base just read, and previews the first revision.
func (m Model) gardenIdeaRekey(doc gardenapi.DocumentView) (Model, tea.Cmd) {
	d := &m.garden.drafts
	key := m.editKeyFor(garden.IssueID(doc.IssueID))
	e := d.editors[m.editKeyFor(0)]
	if e == nil {
		e = d.editors[key]
	}
	if e == nil {
		return m, nil
	}
	delete(d.editors, m.editKeyFor(0))
	e.key, e.number, e.title, e.base = key, doc.Number, doc.Title, doc.Base
	d.editors[key] = e
	d.edit, d.stage = e, draftsEditor
	m.shellFocus = shellInput
	m.gardenSizeEditor()
	return m.gardenPreviewSave(gardenapi.SaveRequest{IssueID: doc.IssueID, Base: e.base, Content: e.content()})
}

// gardenIdeaDone ends the save: the Issue and its first revision exist, so the idea and its session are over.
func (m Model) gardenIdeaDone() (Model, tea.Cmd) {
	d := &m.garden.drafts
	key, num := d.edit.key, d.idea.number
	m.garden.assist.discard(key)
	delete(d.editors, key)
	d.selectAfter = d.idea.issue
	d.idea = ideaSave{}
	d.edit, d.savePreview, d.saveView = nil, nil, nil
	d.stage = draftsList
	d.notice = fmt.Sprintf("Created #%d as a Draft with its first specification.", num)
	m.shellFocus = shellBody
	return m.gardenLoadWork()
}

// ideaSections counts the specification sections of the idea's editor that have text.
func ideaSections(d gardenDraftsState, m Model) int {
	e := d.editors[m.editKeyFor(0)]
	if e == nil {
		return 0
	}
	e.flush()
	n := 0
	for _, sec := range e.secs {
		if sec.text != "" {
			n++
		}
	}
	return n
}
