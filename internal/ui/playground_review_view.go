package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden/review"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
	"github.com/Tiago-0liveira/bonsai/internal/ui/playgroundapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

func reviewAckText(a review.Ack) string {
	switch a {
	case review.AckOpenHigh:
		return "High severity findings remain unresolved"
	case review.AckCINotPassed:
		return "CI has not passed for this acceptance"
	case review.AckReportStale:
		return "The report covers an older commit; current HEAD was not reviewed"
	case review.AckNoReport:
		return "There is no completed review report"
	case review.AckCIStale:
		return "CI is stale or describes another commit"
	}
	return "This acceptance needs an additional acknowledgment"
}

func (m Model) pgReviewAcceptDialog() Model {
	a := m.pg.rev.action
	if a.preview == nil {
		return m
	}
	p := a.preview
	body := fmt.Sprintf("Attempt: %s\nHEAD: %s\nReport: r%d at %s\nUnresolved: %d high, %d medium, %d low, %d info\nCI: %s (%s), read %s\n", pgClean(p.AttemptID, 100), pgShortSHA(p.HeadSHA), p.ReportRevision, pgShortSHA(p.ReportCommit), p.Counts[review.SeverityHigh], p.Counts[review.SeverityMedium], p.Counts[review.SeverityLow], p.Counts[review.SeverityInfo], p.CI.State, pgShortSHA(p.CI.HeadSHA), pgSince(p.CI.FetchedAt))
	title := "Record human acceptance?"
	if len(a.acknowledged) < len(p.Required) {
		title = "Acknowledge acceptance condition?"
		body += "\n" + reviewAckText(p.Required[len(a.acknowledged)]) + ".\nConfirm to acknowledge this condition; acceptance follows separately."
	} else {
		body += "\nRecords your local decision for this exact commit and report. Merge, Issue closure and worktree cleanup are separate actions."
	}
	return m.pgReviewConfirm(modals.KindPlaygroundReviewAcceptAsk, title, body)
}

func (m Model) pgReviewHints() []shellHint {
	if m.pg.rev.action.preview != nil {
		return []shellHint{{"↑/↓", "scroll preview"}, {"enter", "continue acceptance"}, {"esc", "cancel"}}
	}
	return []shellHint{{"↑/↓", "scroll"}, {"n/p", "finding"}, {"[/]", "report"}, {"d", "dismiss"}, {"t", "retry review"}, {"a", "accept"}, {"f", "request fixes"}, {"r", "refresh"}, {"esc", "sessions"}}
}

func (m Model) pgReviewLines(w, rows int) []string {
	if m.pg.rev.action.preview != nil {
		return m.pgReviewPreviewLines(w, rows)
	}
	p := theme.Current
	v := m.pg.rev
	out := []string{gHead(w, "HUMAN REVIEW", pgClean(v.session, 40))}
	add := func(c lipgloss.Color, s string) { out = append(out, gWrap(w, c, false, pgSafe(s))...) }
	if v.busy {
		add(p.Warning, "Recording local action…")
	}
	if v.note != "" {
		add(p.Warning, v.note)
	}
	if v.reconcileErr != nil {
		add(p.Warning, "Review reconciliation unavailable: "+pgReviewError(v.reconcileErr))
	}
	if !v.reports.have {
		if v.reports.err != nil {
			add(p.Warning, "Review reports unavailable: "+pgReviewError(v.reports.err))
		} else {
			add(p.Muted, "Reading review reports…")
		}
	} else {
		if v.reports.err != nil {
			add(p.Warning, "STALE reports: "+pgReviewError(v.reports.err)+"; last good read "+pgSince(v.reports.at))
		}
		if !v.reports.val.Reviewable {
			add(p.Muted, pgClean(v.reports.val.Reason, 240))
		}
	}
	if v.head.err != nil {
		add(p.Warning, "HEAD unavailable; previous provenance is unverified")
	}
	if v.head.have && v.head.val.HeadRead {
		add(p.Muted, "Local HEAD: "+pgShortSHA(v.head.val.Head)+" / read "+pgSince(v.head.at))
	}
	if v.policy.have {
		pol := v.policy.val
		mode := "off"
		if pol.Enabled {
			mode = "on"
		}
		add(p.Muted, fmt.Sprintf("Automatic review %s / %s / %s / scope: %s / limit %d", mode, pgClean(pol.AccountName, 60), pol.Trigger, pgClean(pol.Scope, 60), pol.MaxAutoRuns))
		add(p.Muted, "Reviewer role: "+playgroundapi.ReviewRole)
		if !pol.Capability.Supported {
			add(p.Warning, "Reviewer unavailable: "+pgClean(pol.Capability.Reason, 240))
		}
	}
	if v.policy.err != nil {
		add(p.Warning, "Review policy/account unavailable: "+pgReviewError(v.policy.err))
	}
	ci := m
	ci.pg.chg.checks = v.checks
	out = append(out, ci.pgChgCI(w))
	if v.checks.err != nil {
		add(p.Warning, "CI unavailable now; any previous result is STALE (read "+pgSince(v.checks.at)+")")
	} else if v.checks.have {
		add(p.Muted, "CI commit: "+pgShortSHA(v.checks.val.HeadSHA)+" / read "+pgSince(v.checks.at))
		if v.checks.val.HeadSHA != "" && v.head.val.HeadRead && v.checks.val.HeadSHA != v.head.val.Head {
			add(p.Warning, "CI describes another commit")
		}
	}
	r := m.pgReviewReport()
	if r.Revision == 0 {
		if v.reports.have && v.reports.err == nil {
			add(p.Muted, "No review reports yet. Retry review starts an explicit local read-only run.")
		}
	} else {
		add(p.Text, fmt.Sprintf("REPORT r%d / %s / commit %s / read %s", r.Revision, r.State, pgShortSHA(r.Commit), pgSince(v.reports.at)))
		if r.Superseded {
			add(p.Warning, "Superseded by a later ready report; retained for provenance")
		}
		if v.head.have && v.head.val.HeadRead && r.Commit != v.head.val.Head {
			add(p.Warning, "FOR OLDER COMMIT: current HEAD was not reviewed by this report")
		}
		add(p.Muted, fmt.Sprintf("Base: %s / merge-base %s / trigger: %s", pgClean(r.Base.Ref, 120), pgShortSHA(r.Base.MergeBase), r.Trigger))
		if r.FailCode != "" {
			add(p.Warning, "Reviewer outcome: "+string(r.FailCode))
		}
		if r.Summary != "" {
			add(p.Text, pgClean(r.Summary, 4000))
		}
		if r.State == review.StateReady && len(r.Findings) == 0 {
			add(p.Muted, "No findings in this report")
		}
		if v.finding < len(r.Findings) {
			f := r.Findings[v.finding]
			fg := p.Warning
			if f.Severity == review.SeverityHigh {
				fg = p.Danger
			}
			add(fg, fmt.Sprintf("FINDING %d/%d / %s / %s", v.finding+1, len(r.Findings), f.Severity, pgClean(f.Title, 500)))
			add(p.Accent, fmt.Sprintf("Evidence: %s:%d", pgClean(f.Path, 500), f.Line))
			add(p.Text, pgClean(f.Evidence, 4000))
			add(p.Text, "Recommendation: "+pgClean(f.Recommendation, 4000))
			if f.Dismissed != nil {
				add(p.Muted, fmt.Sprintf("Dismissed r%d at %s: %s", f.Dismissed.Revision, pgShortSHA(f.Dismissed.Commit), pgClean(f.Dismissed.Reason, 500)))
			} else {
				add(p.Warning, "Unresolved")
				if f.PriorDismissal != "" {
					add(p.Muted, pgClean(f.PriorDismissal, 100)+"; this revision remains unresolved")
				}
			}
		}
	}
	add(p.Accent, "d Dismiss · t Retry review · a Accept changes · f Request fixes")
	add(p.Muted, "n/p finding · [ newer / ] older report · ↑/↓ scroll")
	if len(v.reports.val.Reports) > 0 {
		var labels []string
		for _, rr := range v.reports.val.Reports {
			labels = append(labels, fmt.Sprintf("r%d %s %s", rr.Revision, pgShortSHA(rr.Commit), rr.State))
		}
		add(p.Muted, "Report history: "+strings.Join(labels, " / "))
	}
	for _, d := range v.reports.val.Decisions {
		text := fmt.Sprintf("Human acceptance: %s / r%d / CI %s / %s", pgShortSHA(d.ReviewedHead), d.ReportRevision, d.CI.State, d.DecidedAt.Format("2006-01-02 15:04"))
		if v.head.val.HeadRead && d.ReviewedHead != v.head.val.Head {
			text += " / FOR OLDER COMMIT"
		}
		add(p.Muted, text)
	}
	top := max(0, min(v.scroll, max(len(out)-rows, 0)))
	return out[top:min(top+rows, len(out))]
}

// The complete preview is scrollable at every supported size. The compact
// cancel-first dialogs then acknowledge one condition at a time.
func (m Model) pgReviewPreviewLines(w, rows int) []string {
	p := theme.Current
	v := m.pg.rev
	a := v.action.preview
	out := []string{gHead(w, "ACCEPTANCE PREVIEW", "enter continues / esc cancels")}
	add := func(c lipgloss.Color, text string) { out = append(out, gWrap(w, c, false, pgSafe(text))...) }
	add(p.Text, fmt.Sprintf("Attempt %s / HEAD %s / report r%d at %s", pgClean(a.AttemptID, 100), pgShortSHA(a.HeadSHA), a.ReportRevision, pgShortSHA(a.ReportCommit)))
	add(p.Muted, fmt.Sprintf("Policy: require CI passed %t / acknowledge open high findings %t", a.Policy.RequireCI, a.Policy.BlockOnHigh))
	add(p.Text, fmt.Sprintf("CI: %s / commit %s / read %s", a.CI.State, pgShortSHA(a.CI.HeadSHA), pgSince(a.CI.FetchedAt)))
	if a.CIErr != nil {
		add(p.Warning, "CI unavailable: "+pgReviewError(a.CIErr))
	}
	for _, ack := range a.Required {
		add(p.Warning, "Required acknowledgment: "+reviewAckText(ack))
	}
	add(p.Text, fmt.Sprintf("UNRESOLVED FINDINGS: %d", len(a.Unresolved)))
	for _, f := range a.Unresolved {
		add(p.Warning, fmt.Sprintf("%s: %s / %s:%d", f.Severity, pgClean(f.Title, 500), pgClean(f.Path, 500), f.Line))
		add(p.Text, "Evidence: "+pgClean(f.Evidence, 4000))
		add(p.Text, "Recommendation: "+pgClean(f.Recommendation, 4000))
	}
	add(p.Muted, "Records a human decision for this commit. Merge, Issue closure and worktree cleanup remain separate actions.")
	top := max(0, min(v.scroll, max(len(out)-rows, 0)))
	return out[top:min(top+rows, len(out))]
}
