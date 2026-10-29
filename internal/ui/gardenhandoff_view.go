package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/handoff"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

const (
	handoffAcceptCopy  = "Acceptance records your review. Merging remains a separate action."
	handoffChangesCopy = "Records feedback on GitHub. Rework does not start automatically."
	handoffHintCopy    = "Tab action · Enter activate · Esc restores your selection"
	handoffDoneCopy    = "The worktree is retained until you choose to clean it up."
	handoffFileRows    = 8
)

type handoffAck struct{ code, label string }

// handoffAckText words an acknowledgment code; an unknown code is shown as is so it can never be skipped.
func handoffAckText(code string) string {
	switch code {
	case "no-ai-review":
		return "No AI review was run on this work"
	case "post-review-fixes-unreviewed":
		return "Fixes made after the AI review were not reviewed again"
	case "checks-missing-or-failed":
		return "Checks are missing or failed"
	}
	return boardSafe(code)
}

func handoffReworkAcks(p gardenapi.ReworkPreview) []handoffAck {
	var out []handoffAck
	if p.Escalated {
		out = append(out, handoffAck{"implementer", "The implementer's native permissions let it act without asking"})
	}
	if p.Review.Policy.Reviewer.Escalated {
		out = append(out, handoffAck{"reviewer", "The AI reviewer's native permissions let it act without asking"})
	}
	return out
}

// handoffAcks are the toggles the open confirmation requires.
func (m Model) handoffAcks() []handoffAck {
	h := m.garden.handoff
	switch {
	case h.mode == handoffDecide && h.preview != nil:
		out := make([]handoffAck, len(h.preview.Acknowledgments))
		for i, c := range h.preview.Acknowledgments {
			out[i] = handoffAck{c, handoffAckText(c)}
		}
		return out
	case h.mode == handoffRework && h.rework != nil:
		return handoffReworkAcks(*h.rework)
	}
	return nil
}

func handoffTitle(d garden.HandoffDecision) string {
	switch d {
	case garden.HandoffAccepted:
		return "ACCEPT THIS WORK"
	case garden.HandoffChangesRequested:
		return "REQUEST CHANGES"
	case garden.HandoffCompleted:
		return "COMPLETE WITHOUT CODE"
	case garden.HandoffRevoked:
		return "REVOKE ACCEPTANCE"
	}
	return "DECISION"
}

func handoffVerb(d garden.HandoffDecision) string {
	switch d {
	case garden.HandoffAccepted:
		return "accepted"
	case garden.HandoffChangesRequested:
		return "changes requested"
	case garden.HandoffCompleted:
		return "completed without code"
	case garden.HandoffRevoked:
		return "revoked"
	}
	return string(d)
}

// handoffReasonText says what a completion reason means and what to do next, in plain words.
func handoffReasonText(r handoff.Reason) string {
	switch r {
	case handoff.ReasonDirtyWorktree:
		return "The worktree has uncommitted changes; commit or discard them before deciding"
	case handoff.ReasonAttemptNotDone:
		return "The implementation run has not finished"
	case handoff.ReasonHeadUnknown:
		return "The final head could not be read"
	case handoff.ReasonAcceptanceStale:
		return "Your acceptance no longer matches the work; review again"
	case handoff.ReasonAcceptanceRevoked:
		return "You revoked your acceptance; decide again"
	case handoff.ReasonPRNotPublished:
		return "No pull request yet; publish a Draft PR to merge it"
	case handoff.ReasonPRReadFailed:
		return "Pull request status could not be read; refresh"
	case handoff.ReasonPROffline:
		return "GitHub unreachable; last good data shown"
	case handoff.ReasonPRNotFound:
		return "The linked pull request was not found; link it again"
	case handoff.ReasonWrongPR:
		return "GitHub answered with a different pull request than the one linked"
	case handoff.ReasonPRClosedUnmerged:
		return "Pull request closed without merging. Reopen or link a new PR"
	case handoff.ReasonPRNotMerged:
		return "Waiting for the pull request to be merged; merging stays a separate action"
	case handoff.ReasonMergedHeadDiffers:
		return "The merged commit differs from the work you accepted"
	case handoff.ReasonHasCodeChanges:
		return "The work changed files; accept the code instead of completing without it"
	case handoff.ReasonPRHeadChanged:
		return "The pull request changed after your acceptance; review again"
	case handoff.ReasonLogUnreadable:
		return "The handoff record could not be read; refresh"
	case handoff.ReasonWrongBase:
		return "The pull request was merged into a different base branch"
	case handoff.ReasonPRReadStale:
		return "Pull request status is too old to trust; refresh"
	case "pr-changed":
		return "The pull request changed after your acceptance; review again"
	case "head-changed":
		return "Work changed after your acceptance; review again"
	case "tree-changed":
		return "File contents changed after your acceptance"
	case "spec-changed":
		return "The specification changed after your acceptance"
	case "plan-changed":
		return "The plan changed after your acceptance"
	case "review-changed":
		return "The AI review changed after your acceptance"
	case "attempt-changed":
		return "The attempt changed after your acceptance"
	}
	return "Unrecognized reason: " + boardSafe(string(r))
}

func handoffHeading(s handoff.CompletionState) (string, bool) {
	switch s {
	case handoff.CompletionDone:
		return "✓ COMPLETED", true
	case handoff.CompletionBlocked:
		return "✗ BLOCKED", false
	case handoff.CompletionStale:
		return "! STALE", false
	case handoff.CompletionUnknown:
		return "? UNKNOWN", false
	case handoff.CompletionNotReady:
		return "– NOT READY FOR REVIEW", false
	}
	return "◇ YOUR REVIEW", false
}

func handoffShort(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func handoffTime(t time.Time) string { return t.Format("2006-01-02 15:04 MST") }

// handoffDocument is the Overview body while the panel is drawn: a flow in progress, or the evidence.
func (m Model) handoffDocument(width int) []string {
	p := theme.Current
	d := &ovDoc{w: width}
	h := m.garden.handoff
	if h.note != "" {
		d.wrap(p.Warning, true, "", boardSafe(h.note))
		d.blank()
	}
	switch {
	case m.handoffModal():
		m.handoffFlowRows(d)
	default:
		if v, ok := m.handoffView(); ok {
			m.handoffPanelRows(d, v)
		} else {
			m.handoffMissingRows(d)
		}
	}
	return m.settingsASCII(d.rows)
}

// handoffMissingRows is the panel before any read of this attempt answered: never blank, never green.
func (m Model) handoffMissingRows(d *ovDoc) {
	p := theme.Current
	h := m.garden.handoff
	d.line(p.Warning, true, "? UNKNOWN")
	d.blank()
	if h.readErr != "" {
		d.wrap(p.Warning, false, "", "The handoff could not be read: "+boardSafe(h.readErr))
		d.wrap(p.Muted, false, "", "Nothing is shown as done. It is read again automatically.")
		return
	}
	d.wrap(p.Muted, false, "", "Reading the handoff…")
}

func (m Model) handoffGlyphLine(d *ovDoc, fg lipgloss.Color, glyph, text string) {
	d.wrap(fg, false, "", glyph+" "+text)
}

func (m Model) handoffPanelRows(d *ovDoc, v gardenapi.HandoffView) {
	p := theme.Current
	h := m.garden.handoff
	head, done := handoffHeading(v.Completion.State)
	fg := p.Warning
	switch {
	case done:
		fg = p.Success
	case v.Completion.State == handoff.CompletionBlocked:
		fg = p.Danger
	case v.Completion.State == handoff.CompletionAwaitingDecision || v.Completion.State == handoff.CompletionChangesRequested || v.Completion.State == handoff.CompletionAccepted:
		fg = p.Accent
	}
	d.line(fg, true, head)
	if h.readErr != "" {
		d.wrap(p.Warning, false, "", "Last read "+pgSince(v.FetchedAt)+" · refresh failed: "+boardSafe(h.readErr))
	}
	if v.Outcome != "" {
		d.wrap(p.Text, false, "", boardSafe(v.Outcome))
	}
	if !v.InputsCurrent {
		d.wrap(p.Warning, false, "", "! The approved specification or plan moved since this run.")
	}
	d.blank()
	if done {
		m.handoffDoneRows(d, v)
		return
	}
	for _, r := range v.Completion.Reasons {
		glyph, c := "!", p.Warning
		if r == handoff.ReasonPRNotMerged || r == handoff.ReasonPRNotPublished {
			glyph, c = "–", p.Muted
		}
		m.handoffGlyphLine(d, c, glyph, handoffReasonText(r))
	}
	d.gap()
	m.handoffChangesRows(d, v)
	m.handoffVerificationRows(d, v)
	m.handoffPRRows(d, v)
	m.handoffDecisionRows(d, v)
	// A refused action is explained dimly; the buttons only offer what is possible.
	for _, c := range []struct{ name, id string }{{"Accept", "ho-accept"}, {"Request changes", "ho-changes"}, {"Start rework", "ho-rework"}} {
		if a := handoffAction(v, c.id); !a.Allowed && a.Message != "" {
			d.wrap(p.Dim, false, "", c.name+" unavailable: "+boardSafe(a.Message))
		}
	}
	if v.AttemptID == "" && !v.Actions.CompleteNonCode.Allowed && v.Actions.CompleteNonCode.Message != "" {
		d.wrap(p.Dim, false, "", "Complete without code unavailable: "+boardSafe(v.Actions.CompleteNonCode.Message))
	}
	m.handoffFileRows(d, v)
}

func (m Model) handoffChangesRows(d *ovDoc, v gardenapi.HandoffView) {
	p := theme.Current
	c := v.Changes
	if v.AttemptID == "" {
		d.wrap(p.Muted, false, "", "No implementation attempt: this work has no code deliverable.")
		return
	}
	if !c.Available {
		d.wrap(p.Warning, false, "", "! Changes unavailable: "+orDefault(boardSafe(c.Reason), "the worktree could not be read"))
		return
	}
	line := fmt.Sprintf("CHANGES  %d files  +%d / -%d", c.Totals.Files, c.Totals.Add, c.Totals.Del)
	if c.FinalHead != "" {
		line += " · head " + handoffShort(c.FinalHead)
	}
	d.wrap(p.Text, false, "", line)
	if c.Dirty {
		d.wrap(p.Warning, false, "", "! The worktree has uncommitted changes that are not part of this review.")
	}
	if c.Truncated {
		d.wrap(p.Muted, false, "", fmt.Sprintf("The file list is truncated: %d of %d files shown.", len(c.Files), c.Totals.Files))
	}
}

func (m Model) handoffVerificationRows(d *ovDoc, v gardenapi.HandoffView) {
	p := theme.Current
	if v.AttemptID == "" {
		return
	}
	if !v.ChecksRecorded {
		d.wrap(p.Warning, false, "", "No checks recorded")
	}
	for _, c := range v.Checks {
		glyph, fg := "?", p.Warning
		word := "unknown"
		switch c.Outcome {
		case "passed":
			glyph, fg, word = "✓", p.Success, "passed"
		case "failed":
			glyph, fg, word = "✗", p.Danger, "failed"
		case "skipped":
			glyph, fg, word = "–", p.Muted, "skipped"
		}
		d.wrap(fg, false, "", glyph+" "+boardSafe(c.Name)+" · "+word)
	}
	r := v.Review
	switch {
	case r.ReviewUnknown || r.State == gardenapi.HandoffReviewUnknown:
		d.wrap(p.Warning, false, "", "? AI review: unknown (its record could not be read)")
	case r.State == gardenapi.HandoffReviewNone || r.State == "":
		d.wrap(p.Warning, false, "", "No AI review")
	case r.State == gardenapi.HandoffReviewAuthorizedNotRun:
		d.wrap(p.Warning, false, "", "AI review authorized, not run")
	case r.State == gardenapi.HandoffReviewCompleted:
		d.wrap(p.Success, false, "", "✓ AI review completed")
	default:
		d.wrap(p.Warning, false, "", "AI review: "+boardSafe(string(r.State)))
	}
	if r.FixesAfterReview || r.NoSecondReview {
		d.wrap(p.Warning, false, "", "! Fixes after review were not re-reviewed")
	}
	switch {
	case !v.ConcernsKnown:
		d.wrap(p.Muted, false, "", "Findings: no AI review report")
	case len(v.Concerns) == 0:
		d.wrap(p.Success, false, "", "Unresolved findings: 0")
	default:
		d.wrap(p.Warning, false, "", fmt.Sprintf("! Unresolved findings: %d", len(v.Concerns)))
		for i, c := range v.Concerns {
			if i == 5 {
				d.wrap(p.Muted, false, "  ", fmt.Sprintf("… %d more", len(v.Concerns)-5))
				break
			}
			d.wrap(p.Text, false, "  - ", boardSafe(c))
		}
	}
}

func (m Model) handoffPRRows(d *ovDoc, v gardenapi.HandoffView) {
	p := theme.Current
	pr := v.PR
	if !pr.Linked {
		if v.AttemptID != "" {
			d.wrap(p.Muted, false, "", "No pull request linked")
		}
		return
	}
	switch o := pr.Observed; {
	case o == nil:
		reason := map[handoff.ReadError]string{handoff.ReadOffline: "GitHub unreachable", handoff.ReadUnavailable: "the read failed", handoff.ReadNotFound: "pull request not found"}[pr.ReadErr]
		d.wrap(p.Warning, false, "", fmt.Sprintf("? PR #%d status unknown: %s", pr.Number, orDefault(reason, "not read yet")))
	case o.Merged:
		d.wrap(p.Success, false, "", fmt.Sprintf("✓ PR #%d merged · commit %s", pr.Number, handoffShort(o.MergeCommitSHA)))
	case o.Open:
		d.wrap(p.Text, false, "", fmt.Sprintf("PR #%d open · not merged", pr.Number))
	default:
		d.wrap(p.Danger, false, "", fmt.Sprintf("✗ PR #%d closed without merge", pr.Number))
	}
	if !pr.FetchedAt.IsZero() {
		d.wrap(p.Muted, false, "", "GitHub read "+pgSince(pr.FetchedAt))
	}
}

func (m Model) handoffDecisionRows(d *ovDoc, v gardenapi.HandoffView) {
	p := theme.Current
	dec := v.Decision
	entry := func(e *gardenapi.HandoffEntryView) string {
		return fmt.Sprintf("decision comment #%d by @%s at %s", e.CommentID, boardSafe(e.Author), handoffTime(e.At))
	}
	switch {
	case dec.State == "unknown":
		d.wrap(p.Warning, false, "", "? Your decision: the handoff log could not be read")
	case dec.Entry == nil && dec.State == string(handoff.DecisionRevoked):
		d.wrap(p.Muted, false, "", "Your acceptance was revoked")
	case dec.Entry == nil:
		d.wrap(p.Muted, false, "", "No decision recorded yet")
	case dec.State == string(handoff.DecisionStale):
		var why []string
		for _, r := range dec.Stale {
			why = append(why, handoffReasonText(handoff.Reason(r)))
		}
		d.wrap(p.Warning, false, "", "! Your "+handoffVerb(dec.Entry.Decision)+" is stale: "+strings.Join(why, "; "))
		d.wrap(p.Muted, false, "", entry(dec.Entry))
	case dec.State == string(handoff.DecisionRevoked) || dec.Entry.Revoked:
		d.wrap(p.Muted, false, "", "Your "+handoffVerb(dec.Entry.Decision)+" was revoked · "+entry(dec.Entry))
	default:
		d.wrap(p.Text, false, "", "Your decision: "+handoffVerb(dec.Entry.Decision)+" · "+entry(dec.Entry))
		if dec.Entry.Decision == garden.HandoffChangesRequested && dec.Entry.Text != "" {
			handoffExcerpt(d, p.Muted, "  ", dec.Entry.Text, 3)
		}
	}
}

func (m Model) handoffFileRows(d *ovDoc, v gardenapi.HandoffView) {
	p := theme.Current
	if len(v.Changes.Files) == 0 {
		return
	}
	d.gap()
	d.line(p.Muted, true, "FILES")
	for i, f := range v.Changes.Files {
		if i == handoffFileRows {
			d.line(p.Muted, false, fmt.Sprintf("  … %d more", len(v.Changes.Files)-handoffFileRows))
			break
		}
		counts := fmt.Sprintf("+%d/-%d", f.Add, f.Del)
		if f.Binary {
			counts = "binary"
		}
		d.line(p.Text, false, "  "+counts+"  "+boardSafe(f.Path))
	}
}

// handoffDoneRows lists what the evidence proves. Each line says what was actually recorded, so a missing review
// or check is named, not ticked.
func (m Model) handoffDoneRows(d *ovDoc, v gardenapi.HandoffView) {
	p := theme.Current
	ev := v.Completion.Evidence
	by := "you"
	if e := v.Decision.Entry; e != nil && e.Author != "" {
		by = "@" + boardSafe(e.Author)
	}
	if v.AttemptID == "" {
		m.handoffGlyphLine(d, p.Success, "✓", "Completed without code, accepted by "+by)
		if e := v.Decision.Entry; e != nil {
			d.wrap(p.Muted, false, "", fmt.Sprintf("Evidence: decision comment #%d by @%s at %s", e.CommentID, boardSafe(e.Author), handoffTime(e.At)))
			if e.Text != "" {
				handoffExcerpt(d, p.Text, "  ", e.Text, 4)
			}
		}
		return
	}
	plan := "approved plan"
	if v.PlanLabel != "" {
		plan = "plan " + boardSafe(v.PlanLabel)
	}
	m.handoffGlyphLine(d, p.Success, "✓", plan+" implemented")
	m.handoffEvidenceLine(d, v)
	m.handoffGlyphLine(d, p.Success, "✓", "Accepted by "+by)
	if o := v.PR.Observed; o != nil && o.Merged {
		m.handoffGlyphLine(d, p.Success, "✓", fmt.Sprintf("PR #%d merged into base", v.PR.Number))
		d.blank()
		when := ""
		if ev.MergedAt != nil {
			when = "Merged " + handoffTime(*ev.MergedAt) + " · "
		}
		d.wrap(p.Muted, false, "", when+"commit "+handoffShort(ev.MergeCommitSHA))
	}
	if !v.PR.FetchedAt.IsZero() {
		d.wrap(p.Muted, false, "", "GitHub read "+pgSince(v.PR.FetchedAt))
	}
	m.handoffFileRows(d, v)
}

// handoffEvidenceLine is the review and checks summary of a completed attempt, ticked only for what was recorded.
func (m Model) handoffEvidenceLine(d *ovDoc, v gardenapi.HandoffView) {
	p := theme.Current
	review := v.Review.State == gardenapi.HandoffReviewCompleted && !v.Review.FixesAfterReview && !v.Review.NoSecondReview
	reviewWord := "AI review completed"
	switch {
	case v.Review.State == gardenapi.HandoffReviewCompleted && !review:
		reviewWord = "AI review completed, fixes after it not re-reviewed"
	case v.Review.State != gardenapi.HandoffReviewCompleted:
		reviewWord = "No AI review"
	}
	passed := v.ChecksRecorded
	for _, c := range v.Checks {
		passed = passed && c.Outcome == "passed"
	}
	checkWord := "checks passed"
	if !v.ChecksRecorded {
		checkWord = "No checks recorded"
	} else if !passed {
		checkWord = "checks not all passed"
	}
	if review && passed {
		m.handoffGlyphLine(d, p.Success, "✓", reviewWord+" + "+checkWord)
		return
	}
	m.handoffGlyphLine(d, p.Warning, "!", reviewWord+" · "+checkWord)
}

// handoffExcerpt adds at most limit wrapped rows of s and says how many more there are.
func handoffExcerpt(d *ovDoc, fg lipgloss.Color, indent, s string, limit int) {
	var all []string
	for _, para := range strings.Split(strings.TrimSpace(boardSafe(s)), "\n") {
		all = append(all, strings.Split(ansi.Wrap(para, max(d.w-ansi.StringWidth(indent), 1), ""), "\n")...)
	}
	for i, l := range all {
		if i == limit {
			d.line(theme.Current.Muted, false, indent+fmt.Sprintf("… %d more lines", len(all)-limit))
			return
		}
		d.line(fg, false, indent+l)
	}
}

// --- flows ---

func (m Model) handoffFlowRows(d *ovDoc) {
	p := theme.Current
	h := m.garden.handoff
	switch h.mode {
	case handoffText:
		d.line(p.Warning, true, handoffTitle(h.decision))
		intro := handoffChangesCopy
		if h.decision == garden.HandoffCompleted {
			intro = "Say what completes this work. Your words are the evidence, recorded on GitHub; nothing is merged."
		}
		d.wrap(p.Muted, false, "", intro)
		d.blank()
		var ta textarea.Model
		if h.text != nil {
			ta = *h.text
		}
		w, rows := m.gardenOverlayBody()
		ta.SetWidth(max(w-2, 4))
		ta.SetHeight(max(2, min(rows-7, 8)))
		for _, l := range strings.Split(ta.View(), "\n") {
			d.rows = append(d.rows, shellRow(d.w, p.Surface, seg(p.Text, p.Surface, false).Render(" ")+l, ""))
		}
		d.wrap(p.Muted, false, "", "ctrl+s previews the exact record. Nothing is written yet.")
	case handoffPreviewing:
		d.line(p.Warning, true, "READING THE PREVIEW")
		d.wrap(p.Muted, false, "", "Reading the exact record that would be written… esc cancels. Nothing is written.")
	case handoffDecide:
		m.handoffDecideRows(d)
	case handoffRework:
		m.handoffReworkRows(d)
	}
	if h.inflight {
		d.gap()
		d.line(p.Warning, true, "Recording… keys are ignored until GitHub answers.")
	}
}

func (m Model) handoffAckRows(d *ovDoc) {
	p := theme.Current
	for i, a := range m.handoffAcks() {
		box, fg := "[ ]", p.Text
		if m.garden.handoff.acks[i] {
			box, fg = "[x]", p.Success
		}
		d.wrap(fg, false, "", fmt.Sprintf("%s %d  %s", box, i+1, a.label))
	}
}

func (m Model) handoffDecideRows(d *ovDoc) {
	p := theme.Current
	pv := m.garden.handoff.preview
	d.line(p.Warning, true, handoffTitle(pv.Decision))
	d.wrap(p.Text, false, "", boardSafe(pv.Summary))
	d.blank()
	d.line(p.Muted, true, "EXACT TEXT RECORDED ON GITHUB")
	if strings.TrimSpace(pv.Body) == "" {
		d.line(p.Muted, false, "  (no additional text)")
	} else {
		handoffExcerpt(d, p.Text, "  ", pv.Body, 6)
	}
	d.blank()
	if pv.Identity.FinalHead != "" {
		d.wrap(p.Muted, false, "", "Frozen work: head "+handoffShort(pv.Identity.FinalHead)+" · tree "+handoffShort(pv.Identity.FinalTree))
	}
	if pv.PRNumber != 0 {
		d.wrap(p.Muted, false, "", fmt.Sprintf("Frozen PR: #%d at %s", pv.PRNumber, handoffShort(pv.PRHeadSHA)))
	} else if pv.Decision == garden.HandoffAccepted {
		d.wrap(p.Muted, false, "", "No pull request is linked yet.")
	}
	if len(pv.Acknowledgments) > 0 {
		d.blank()
		d.line(p.Warning, true, "ACKNOWLEDGE EACH TO CONTINUE")
		m.handoffAckRows(d)
	}
}

func (m Model) handoffReworkRows(d *ovDoc) {
	p := theme.Current
	rp := m.garden.handoff.rework
	d.line(p.Warning, true, "START REWORK")
	d.wrap(p.Muted, false, "", "One more run in the retained worktree, with your recorded feedback:")
	handoffExcerpt(d, p.Text, "  ", rp.Feedback, 4)
	d.blank()
	d.wrap(p.Text, false, "", "Implementer  "+launchWho(rp.Provider, rp.AccountName, rp.Native.Effort)+" · model "+orDefault(rp.Native.Model, "unknown"))
	var native []string
	for _, f := range [][2]string{{"sandbox", rp.Native.Sandbox}, {"approval", rp.Native.Approval}, {"permission mode", rp.Native.PermissionMode}} {
		if f[1] != "" {
			native = append(native, f[0]+": "+boardSafe(f[1]))
		}
	}
	if rp.Native.FullAccess != nil {
		native = append(native, fmt.Sprintf("full access: %t", *rp.Native.FullAccess))
	}
	d.wrap(p.Text, false, "", "Permissions  "+orDefault(strings.Join(native, "; "), "unknown"))
	switch r := rp.Review; {
	case r.Hash != "":
		rv := r.Policy.Reviewer
		d.wrap(p.Text, false, "", "AI review    "+launchWho(rv.Provider, rv.AccountName, rv.Effort)+" · model "+orDefault(rv.Model, "unknown")+": one review + fixes + checks, then a stop for your review")
	case r.Enabled:
		d.wrap(p.Warning, false, "", "AI review unavailable: "+orDefault(boardSafe(r.Reason), "unknown")+". The run stops at your review.")
	default:
		d.wrap(p.Text, false, "", "AI review    Off: the run stops at your review")
	}
	if len(handoffReworkAcks(*rp)) > 0 {
		d.blank()
		d.line(p.Warning, true, "ACKNOWLEDGE EACH TO CONTINUE")
		m.handoffAckRows(d)
	}
}

// --- controls, footer, rail and hints ---

// handoffControls is the footer button row. Only an allowed action is a button.
func (m Model) handoffControls() ([]issueControl, bool) {
	h := m.garden.handoff
	idle := m.garden.api != nil && !m.garden.busy.writes() && !h.inflight
	if m.handoffModal() {
		switch h.mode {
		case handoffText:
			return []issueControl{{id: "ho-back", key: "esc", long: "Back", short: "Back", enabled: idle},
				{id: "ho-send", key: "ctrl+s", long: "Preview the record", short: "Preview", enabled: idle}}, true
		case handoffPreviewing:
			return []issueControl{{id: "ho-cancel", key: "esc", long: "Cancel", short: "Cancel", enabled: true}}, true
		}
		out := []issueControl{{id: "ho-cancel", key: "esc", long: "Cancel", short: "Cancel", enabled: idle}}
		for i, a := range m.handoffAcks() {
			box := "[ ]"
			if i < len(h.acks) && h.acks[i] {
				box = "[x]"
			}
			out = append(out, issueControl{id: fmt.Sprintf("ho-ack-%d", i), key: fmt.Sprint(i + 1), long: box + " " + a.label, short: box, enabled: idle})
		}
		confirm := issueControl{id: "ho-confirm", key: "enter", long: "Record " + strings.ToLower(handoffTitle(m.handoffDecisionKind())), short: "Record", enabled: m.handoffConfirmable()}
		if h.mode == handoffRework {
			confirm.long, confirm.short = "Start rework", "Start"
		}
		return append(out, confirm), true
	}
	if !m.handoffShown() {
		return nil, false
	}
	out := []issueControl{{id: "close", key: "esc", long: "Back to " + m.garden.board.layoutName(), short: m.garden.board.layoutName(), enabled: true}}
	if v, ok := m.handoffView(); ok {
		add := func(id, key, long, short string) {
			if handoffAction(v, id).Allowed {
				out = append(out, issueControl{id: id, key: key, long: long, short: short, enabled: idle})
			}
		}
		add("ho-accept", "a", "Accept…", "Accept…")
		add("ho-changes", "c", "Request changes…", "Changes…")
		add("ho-revoke", "u", "Revoke acceptance…", "Revoke…")
		add("ho-rework", "m", "Start rework…", "Rework…")
		add("ho-complete", "n", "Complete without code…", "Complete…")
		pr := "Open PR"
		if o := v.PR.Observed; o != nil && o.Merged {
			pr = "Open merged PR"
		}
		add("ho-pr", "g", pr, "PR")
	}
	if _, has := m.handoffNewest(); has {
		out = append(out, issueControl{id: "ho-terminal", key: "o", long: "Open terminal", short: "Session", enabled: idle})
		if v, ok := m.handoffView(); ok && handoffAction(v, "ho-cleanup").Allowed {
			out = append(out, issueControl{id: "ho-cleanup", key: "w", long: "Preview cleanup…", short: "Cleanup…", enabled: idle})
		}
	} else if m.launchSupported() && m.launchOffered() {
		out = append(out, issueControl{id: "start", key: "s", long: "Start implementation…", short: "Start…", enabled: idle})
	}
	return out, true
}

// handoffDecisionKind is the decision the open confirmation records.
func (m Model) handoffDecisionKind() garden.HandoffDecision {
	if p := m.garden.handoff.preview; p != nil {
		return p.Decision
	}
	return m.garden.handoff.decision
}

// handoffReason is the footer line under the buttons.
func (m Model) handoffReason() (string, bool) {
	h := m.garden.handoff
	if h.inflight {
		return "Recording… keys are ignored until GitHub answers.", true
	}
	if m.handoffModal() {
		switch {
		case h.mode == handoffRework:
			return "Starts one more run of the retained worktree. Nothing is merged or published.", true
		case h.mode == handoffPreviewing:
			return "Reading the exact record. Nothing is written.", true
		}
		switch m.handoffDecisionKind() {
		case garden.HandoffAccepted:
			return handoffAcceptCopy, true
		case garden.HandoffChangesRequested:
			return handoffChangesCopy, true
		case garden.HandoffRevoked:
			return "Withdraws your recorded decision. Nothing is merged, closed or cleaned.", true
		}
		return "Records completion on GitHub. Nothing is merged.", true
	}
	if !m.handoffShown() {
		return "", false
	}
	if v, ok := m.handoffView(); ok {
		switch {
		case v.Completion.State == handoff.CompletionDone:
			return handoffDoneCopy, true
		case v.Actions.Accept.Allowed:
			return handoffAcceptCopy, true
		}
	}
	return handoffHintCopy, true
}

// handoffRail adds the ticket facts to the wide summary rail.
func (m Model) handoffRail(add func(fg lipgloss.Color, bold bool, s string)) {
	if !m.handoffShown() {
		return
	}
	p := theme.Current
	v, ok := m.handoffView()
	if !ok {
		return
	}
	add(p.Muted, true, "THIS TICKET")
	if v.SpecLabel != "" {
		add(p.Text, false, "Spec "+boardSafe(v.SpecLabel))
	}
	if v.PlanLabel != "" {
		add(p.Text, false, "Plan "+boardSafe(v.PlanLabel))
	}
	add(p.Muted, false, fmt.Sprintf("GitHub Issue #%d", m.handoffIssueNumber()))
}

func (m Model) handoffIssueNumber() int64 {
	if r, _, ok := m.garden.board.collection.Find(m.garden.board.opened); ok {
		return r.Item.Issue.Number
	}
	return 0
}

func (m Model) handoffHints() []shellHint {
	h := m.garden.handoff
	switch h.mode {
	case handoffText:
		return []shellHint{{"ctrl+s", "preview the record"}, {"esc", "back"}}
	case handoffPreviewing:
		return []shellHint{{"esc", "cancel"}}
	case handoffDecide, handoffRework:
		return []shellHint{{"tab", "next"}, {"1-9", "acknowledge"}, {"enter", "activate (Cancel first)"}, {"esc", "cancel"}}
	}
	out := []shellHint{{"esc", "restore selection / close"}, {m.keys.Tab.Help().Key, "actions"}, {"enter", "activate"}}
	if v, ok := m.handoffView(); ok {
		for _, c := range [][2]string{{"ho-accept", "a accept"}, {"ho-changes", "c request changes"}, {"ho-rework", "m rework"}, {"ho-revoke", "u revoke"}, {"ho-pr", "g PR"}} {
			if handoffAction(v, c[0]).Allowed {
				key, label, _ := strings.Cut(c[1], " ")
				out = append(out, shellHint{key, label})
			}
		}
	}
	return append(out, shellHint{"o", "terminal"}, shellHint{m.keys.Refresh.Help().Key, "refresh"})
}
