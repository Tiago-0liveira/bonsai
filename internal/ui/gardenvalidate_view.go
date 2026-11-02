package ui

import (
	"fmt"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// gateResult is the transition validating records, in the words of the mockup.
func gateResult(a approve.Assessment) string {
	switch {
	case a.State == approve.StateFinishing:
		return "Draft → Approved (finishes the approval already recorded)"
	case a.From == garden.StageDraft:
		return "Draft → Approved"
	}
	return fmt.Sprintf("stays Approved, now for specification s%d", a.Ordinal)
}

// stepLine describes one write of the transition.
func stepLine(i int, s approve.Step, actor string, a approve.Assessment) string {
	switch s {
	case approve.StepApproval:
		return fmt.Sprintf("%d. One approval comment by %s for revision %s.", i, orDefault(actor, "you"), shortHash(a.Head.Hash))
	case approve.StepLabel:
		return fmt.Sprintf("%d. Stage label %s → ready; type is kept.", i, a.From)
	}
	return ""
}

func (m Model) gsValidate(width, rows int) []string {
	p := theme.Current
	g := m.garden
	d := g.drafts
	pv := d.validate
	if pv == nil {
		return gCompose(width, rows, []gblock{{lines: []string{gText(width, p.Muted, false, "Reading the specification…")}}}, nil)
	}
	a := pv.Gate
	title := "CANNOT VALIDATE SPECIFICATION YET"
	label := fmt.Sprintf("Validate specification s%d", a.Ordinal)
	if pv.CanValidate() {
		title = fmt.Sprintf("VALIDATE SPECIFICATION s%d?", a.Ordinal)
	}
	blocks := []gblock{{lines: []string{gHeadChip(width, title, p.Success), gText(width, p.Text, true, fmt.Sprintf("Issue #%d · %s", pv.Number, pv.Title))}}}
	if a.Head != nil {
		blocks = append(blocks, gblock{lines: []string{
			gText(width, p.Text, false, fmt.Sprintf("Revision %s · fingerprint %s", shortHash(a.Head.Hash), shortHash(a.Fingerprint))),
			gText(width, p.Text, false, "Approver: "+pv.Actor),
			gText(width, p.Text, true, "Result: "+gateResult(a)),
		}})
	}
	if pv.CanValidate() {
		lines := []string{gText(width, p.Text, true, "This records specification approval only."), gText(width, p.Muted, false, "A planner starts through a separate local action.")}
		for i, s := range pv.Steps {
			lines = append(lines, gText(width, p.Text, false, stepLine(i+1, s, pv.Actor, a)))
		}
		blocks = append(blocks, gblock{lines: lines})
	} else {
		lines := []string{gText(width, p.Warning, true, gateLabel(a.State))}
		for _, b := range a.Blockers {
			lines = append(lines, gText(width, p.Text, false, b.Message))
		}
		blocks = append(blocks, gblock{lines: lines})
	}
	if d.validated != nil && d.validated.Err != nil {
		e := d.validated.Err
		blocks = append(blocks, gblock{lines: []string{gText(width, p.Warning, true, strings.TrimSpace(e.Message+" "+e.Next))}})
	}
	var action string
	switch {
	case g.busy == busyValidate:
		action = gText(width, p.Warning, true, "Validating… keys are ignored until GitHub answers.")
	case pv.CanValidate():
		if d.validated != nil {
			label = "Validate again"
		}
		action = gButtons(width, "                    ", gButton("Back", d.btn == 0), gButton(label, d.btn == 1))
	default:
		action = gButtons(width, "  ", gButton("Back", true))
	}
	blocks = blocksWithNotices(m, width, append(blocks, gblock{lines: []string{action}}, gblock{prio: 5, lines: []string{gText(width, p.Muted, false, "Bonsai re-reads GitHub right before it writes and again afterwards. Nothing is launched.")}})...)
	return m.settingsASCII(gCompose(width, rows, blocks, nil))
}
