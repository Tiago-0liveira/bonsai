package ui

import (
	"fmt"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/drafts"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// privPublishView is the confirmation, or the state of a publication that already started.
func (m Model) privPublishView(p *privateDraft, width, rows int) []string {
	pub := p.pub
	if pub == nil {
		return m.privReviewView(p, width, rows)
	}
	if pub.view == nil || pub.result != nil {
		return m.privPublicationView(p, width, rows)
	}
	c := theme.Current
	v := pub.view
	out := m.privHeader(p, width)
	out = append(out, gHeadChip(width, "APPROVE SPECIFICATION & CREATE ISSUE", c.Accent))
	chips := make([]string, 0, len(v.Labels))
	for _, l := range v.Labels {
		chips = append(chips, l)
	}
	out = append(out,
		gField(width, 12, "Repository", grun{v.RepoFullName, c.Text, false}),
		gField(width, 12, "Account", grun{"@" + v.ActorLogin, c.Text, false}),
		gField(width, 12, "Draft", grun{fmt.Sprintf("%s · revision %d", v.DraftLabel, v.DraftRevision), c.Text, false}),
		gField(width, 12, "Title", grun{boardTitle(v.Title), c.Text, true}, grun{"  " + typeWord(v.Type), c.Warning, false}),
		gField(width, 12, "Labels", grun{strings.Join(finalLabels(chips), "  "), c.Accent, false}),
	)
	body := gWrap(width, c.Text, false, v.SpecText+" ")
	foot := []string{
		gText(width, c.Text, false, "✓ Publish this exact specification as a real GitHub issue."),
		gText(width, c.Text, false, "✓ Add it to this Garden's Backlog, ready for planning."),
		gText(width, c.Muted, false, "Created at stage/draft; moves to stage/ready after approval is verified."),
		gText(width, c.Muted, false, "Planning and implementation wait for your next action."),
	}
	avail := max(1, rows-len(out)-len(foot)-3)
	start := max(0, min(pub.scroll, len(body)-avail))
	pub.scroll = start
	for i := 0; i < avail; i++ {
		if start+i < len(body) {
			out = append(out, body[start+i])
		} else {
			out = append(out, blankRow(width, c.Background))
		}
	}
	out = append(out, gText(width, c.Muted, false, fmt.Sprintf("Lines %d–%d of %d", min(start+1, len(body)), min(start+avail, len(body)), len(body))))
	out = append(out, foot...)
	switch {
	case pub.running:
		out = append(out, gText(width, c.Warning, true, "Publishing… keys are ignored until GitHub answers."))
	default:
		out = append(out, gButtons(width, "  ", privButton("Back to draft (Esc)", false, false), privButton("Approve & create issue (Enter)", true, true)))
	}
	return out
}

// finalLabels shows the stage the Issue ends at: it is created at Draft and moves once the approval is verified.
func finalLabels(labels []string) []string {
	out := make([]string, len(labels))
	for i, l := range labels {
		out[i] = l
		if l == garden.FormatStageLabel(garden.StageDraft) {
			out[i] = garden.FormatStageLabel(garden.StageReady)
		}
	}
	return out
}

// publicationStatus is the state of a publication that is under way or finished, from the last reply or,
// for a draft opened from the inbox, from the stored publication.
func (p *privateDraft) publicationStatus() (title string, lines []string, resume bool) {
	pub := p.pub
	number, url := int64(0), ""
	if is := p.draft.Publication.Issue; is != nil {
		number, url = is.Number, is.URL
	}
	state := gardenapi.PublishIncomplete
	var reply *gardenapi.PublishView
	if pub != nil && pub.result != nil {
		reply = pub.result
		state = reply.State
		if reply.IssueNumber != 0 {
			number, url = reply.IssueNumber, reply.IssueURL
		}
	} else if p.draft.Publication.State == drafts.Published {
		state = gardenapi.PublishDone
	} else if number == 0 {
		state = gardenapi.PublishUnknown
	}
	switch state {
	case gardenapi.PublishDone:
		return fmt.Sprintf("Published as #%d", number), []string{url, "The specification is approved. Planning waits for your next action."}, false
	case gardenapi.PublishUnknown:
		lines = []string{"Checking GitHub for the issue…", "It may already exist; Bonsai will not create it again until it has looked."}
	default:
		lines = []string{"The issue exists but is not approved yet.", "Resuming reuses what exists: no second issue, revision or approval."}
		if number != 0 {
			lines = []string{fmt.Sprintf("The issue #%d exists but is not approved yet.", number), url, lines[1]}
		}
	}
	if reply != nil && reply.Err != nil {
		lines = append(lines, reply.Err.Message)
	}
	title = "Publishing incomplete"
	if state == gardenapi.PublishUnknown {
		title = "Publishing"
	} else if number != 0 {
		title += fmt.Sprintf(" — #%d", number)
	}
	return title, lines, true
}

func (m Model) privPublicationView(p *privateDraft, width, rows int) []string {
	c := theme.Current
	out := m.privHeader(p, width)
	title, lines, resume := p.publicationStatus()
	fg := c.Warning
	if !resume {
		fg = c.Accent
	}
	out = append(out, gHeadChip(width, strings.ToUpper(title), fg), blankRow(width, c.Background))
	for _, l := range lines {
		if l != "" {
			out = append(out, gWrap(width, c.Text, false, l)...)
		}
	}
	out = append(out, blankRow(width, c.Background))
	switch {
	case p.pub != nil && p.pub.running:
		out = append(out, gText(width, c.Warning, true, "Working… keys are ignored until GitHub answers."))
	case resume:
		out = append(out, gButtons(width, "  ", privButton("Drafts (Esc)", false, false), privButton("Resume publishing (Enter)", true, true)))
	default:
		out = append(out, gButtons(width, "  ", privButton("Drafts (Esc)", true, false)))
	}
	return out
}
