package ui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/drafts"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// Publishing a private draft: confirm the exact preview, then create the Issue and record the approval.
// The work is core/garden/publish; this file only asks for it and shows where it stands. Replies are
// scoped by draft token and request counter, so a late reply never lands on another draft or preview.

// privPub is the publish screen of one draft. A nil view means the draft was opened already
// publishing (or published): there is nothing to confirm, only to resume or read.
type privPub struct {
	view    *gardenapi.PublishPreviewView
	gen     uint64 // the edit generation the preview was made for
	running bool   // a publish or resume is in flight: input is locked and a second enter is ignored
	result  *gardenapi.PublishView
	scroll  int
}

type (
	privPubPreviewMsg struct {
		repo  garden.RepoID
		token uint64
		gen   uint64
		view  gardenapi.PublishPreviewView
		err   error
	}
	privPubDoneMsg struct {
		repo  garden.RepoID
		token uint64
		gen   uint64
		view  gardenapi.PublishView
		err   error
	}
)

func (m Model) privPublisher() (gardenapi.PrivatePublisher, bool) {
	if _, ok := m.privateDrafts(); !ok {
		return nil, false
	}
	api, ok := m.garden.api.(gardenapi.PrivatePublisher)
	return api, ok
}

// privPublishStart is enter on the review: save what is unsaved, then ask for the preview.
func (m Model) privPublishStart(p *privateDraft) (Model, tea.Cmd) {
	api, ok := m.privPublisher()
	switch {
	case !ok:
		p.notice = privPublishUnavailable
		return m, nil
	case p.state == saveConflict:
		p.notice = "Choose a version first: k keeps yours, o opens theirs."
		return m, nil
	case p.previewing:
		return m, nil
	case p.draft.ID == "" && p.blank():
		p.notice = "There is nothing to publish yet."
		return m, nil
	case p.draft.ID == "" || p.unwritten():
		// The preview is of the stored revision, so the text on screen is stored first.
		p.publishAfter = true
		if p.inflight {
			return m, nil
		}
		return m.privStartSave(p)
	}
	p.previewing, p.notice = true, ""
	p.pubGen++
	gen, token, repo, id := p.pubGen, p.token, p.repo, p.draft.ID
	return m, func() tea.Msg {
		v, err := api.PreviewPublish(context.Background(), repo, id)
		return privPubPreviewMsg{repo: repo, token: token, gen: gen, view: v, err: err}
	}
}

func (m Model) onPrivPubPreview(msg privPubPreviewMsg) (Model, tea.Cmd) {
	p := m.garden.drafts.private
	if p == nil || p.token != msg.token || p.repo != msg.repo || msg.gen != p.pubGen || !p.previewing {
		return m, nil
	}
	p.previewing = false
	if msg.err != nil {
		p.notice = safeGardenError(msg.err).Message
		if next := safeGardenError(msg.err).Next; next != "" {
			p.notice += " " + next
		}
		if safeGardenError(msg.err).Code == gardenapi.CodePublishResume {
			// Another window or an earlier run holds it: read the draft again so it opens as publishing.
			return m, m.privListCmd()
		}
		return m, nil
	}
	v := msg.view
	if p.gen != p.savedGen || v.DraftID != p.draft.ID || v.DraftRevision != p.draft.Revision {
		p.notice = "The draft changed; review it again."
		return m, nil
	}
	p.pub, p.stage, p.notice = &privPub{view: &v, gen: p.gen}, privPublish, ""
	return m, nil
}

// dropPreview forgets a preview the draft no longer matches. A confirmed publication is never dropped.
func (p *privateDraft) dropPreview(notice string) {
	p.previewing = false
	p.pubGen++ // a reply still in flight belongs to the old preview
	if p.pub != nil && p.pub.view != nil && p.pub.result == nil && !p.pub.running {
		p.pub = nil
		if p.stage == privPublish {
			p.stage = privReview
		}
		p.notice = notice
	}
}

func (m Model) privPublishKey(p *privateDraft, k string) (Model, tea.Cmd) {
	pub := p.pub
	switch {
	case pub == nil:
		p.stage = privReview
		return m, nil
	case pub.running:
		return m, nil
	}
	api, ok := m.privPublisher()
	switch k {
	case "esc":
		if pub.view != nil && pub.result == nil {
			p.stage, p.pub, p.notice = privEditor, nil, ""
			return m, nil
		}
		return m.privLeave()
	case "up", "k":
		pub.scroll = max(0, pub.scroll-1)
	case "down", "j":
		pub.scroll++
	case "pgup":
		pub.scroll = max(0, pub.scroll-max(1, m.gardenBoardRows()-14))
	case "pgdown":
		pub.scroll += max(1, m.gardenBoardRows()-14)
	case "enter", "r":
		switch {
		case !ok:
			p.notice = privPublishUnavailable
		case pub.view != nil && pub.result == nil:
			if k == "enter" {
				return m.privConfirmPublish(p, api)
			}
		case p.draft.Publication.State != drafts.Published && (pub.result == nil || pub.result.State != gardenapi.PublishDone):
			return m.privResumePublish(p, api)
		}
	}
	return m, nil
}

// privConfirmPublish sends the preview back unchanged. It first checks that nothing on screen moved.
func (m Model) privConfirmPublish(p *privateDraft, api gardenapi.PrivatePublisher) (Model, tea.Cmd) {
	pub := p.pub
	if p.gen != pub.gen || p.gen != p.savedGen || pub.view.DraftRevision != p.draft.Revision {
		p.dropPreview("The draft changed; review it again.")
		return m, nil
	}
	pub.running = true
	p.pubGen++
	gen, token, repo, v := p.pubGen, p.token, p.repo, *pub.view
	return m, func() tea.Msg {
		out, err := api.Publish(context.Background(), repo, v)
		return privPubDoneMsg{repo: repo, token: token, gen: gen, view: out, err: err}
	}
}

func (m Model) privResumePublish(p *privateDraft, api gardenapi.PrivatePublisher) (Model, tea.Cmd) {
	p.pub.running = true
	p.pubGen++
	gen, token, repo, id := p.pubGen, p.token, p.repo, p.draft.ID
	return m, func() tea.Msg {
		out, err := api.ResumePublish(context.Background(), repo, id)
		return privPubDoneMsg{repo: repo, token: token, gen: gen, view: out, err: err}
	}
}

func (m Model) onPrivPubDone(msg privPubDoneMsg) (Model, tea.Cmd) {
	d := &m.garden.drafts
	p := d.private
	if p == nil || p.token != msg.token || p.repo != msg.repo || p.pub == nil || msg.gen != p.pubGen || !p.pub.running {
		return m, nil
	}
	pub := p.pub
	pub.running = false
	if msg.err != nil {
		ge := safeGardenError(msg.err)
		note := ge.Message
		if ge.Next != "" {
			note += " " + ge.Next
		}
		if ge.Code == gardenapi.CodePublishStale {
			note = "The draft or the connection changed; nothing was published. Review it again."
		}
		switch {
		case ge.Code == gardenapi.CodePublishStale || ge.Code == gardenapi.CodePublishNotReady:
			// Refused before anything was reserved: back to the review, where the draft can be previewed again.
			p.stage, p.pub, p.notice = privReview, nil, note
		case pub.view != nil && pub.result == nil:
			// Any other failure of a confirmation may have come after the reservation: the draft is or may be
			// publishing, so it is read again and resuming is offered instead of claiming nothing started.
			pub.result, p.notice = &gardenapi.PublishView{State: gardenapi.PublishUnknown, Err: ge}, note
		default:
			p.notice = note
		}
		return m, m.privListCmd()
	}
	v := msg.view
	switch v.State {
	case gardenapi.PublishDone:
		for i := range d.privList {
			if d.privList[i].ID == p.draft.ID {
				d.privList[i].Publication.State = drafts.Published
			}
		}
		d.private, d.stage, d.selectedLocal, d.selected = nil, draftsList, "", 0
		d.notice = fmt.Sprintf("Published as #%d. Planning and implementation wait for your next action.", v.IssueNumber)
		d.reselect(m.garden.board.snapshot)
		m.gardenDraftsVisible()
		m.shellFocus = shellBody
		return m, m.privListCmd()
	case gardenapi.PublishFailed:
		note := "Nothing was created on GitHub, so it is safe to try again."
		if v.Err != nil {
			note = v.Err.Message + " " + note
		}
		p.stage, p.pub, p.notice = privReview, nil, note
	default:
		pub.result = &v
		p.notice = ""
	}
	return m, m.privListCmd()
}
