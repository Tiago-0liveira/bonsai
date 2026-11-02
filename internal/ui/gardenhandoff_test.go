package ui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/handoff"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
	gardenfake "github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi/fake"
)

// handoffUIBackend is the launch fake of the other Garden tests with the scripted handoff on top.
type handoffUIBackend struct {
	*fakeLaunchUIBackend
	*gardenfake.HandoffBackend
}

// handoffRetarget points a sample at the Issue and attempt the UI fixture holds.
func handoffRetarget(v gardenapi.HandoffView, attempt string) gardenapi.HandoffView {
	v.IssueID = 1
	if v.AttemptID != "" {
		v.AttemptID = attempt
	}
	// Ages are drawn against the clock, so the samples are read "just now".
	at := time.Now().Add(-12 * time.Second)
	v.FetchedAt, v.PR.FetchedAt = at, at
	if o := v.PR.Observed; o != nil {
		c := *o
		c.FetchedAt = at
		v.PR.Observed = &c
	}
	return v
}

// handoffUIEnv opens the Ready Issue of the launch fixture with an attempt awaiting review (or none) and the
// scripted handoff of the sample.
func handoffUIEnv(t *testing.T, w, h int, sample gardenfake.HandoffSample, withAttempt bool, glyphs string) (Model, *gardenfake.HandoffBackend, *fakeLaunchUIBackend, launch.Attempt) {
	t.Helper()
	m, _, fb := fakeLaunchUIEnv(t, w, h)
	if glyphs != "" {
		m.work.AgentGlyphs = glyphs
	}
	hb := gardenfake.NewHandoff(sample)
	var a launch.Attempt
	if withAttempt {
		p, err := fb.PreviewLaunch(context.Background(), gardenapi.LaunchPreviewRequest{IssueID: 1, AccountID: "c1", Branch: "bonsai/101-retained", BaseRef: "main", RemoteName: "origin", Name: "Implement #101", Cols: 100, Rows: 25})
		if err != nil {
			t.Fatal(err)
		}
		a = fakeLaunchUIAttempt(p.Preview)
		a.State, a.Revision = launch.StateAwaitingReview, 2
		fb.attempts = []launch.Attempt{a}
	}
	hb.View = handoffRetarget(hb.View, a.ID)
	hb.DecideView.IssueID, hb.DecideView.AttemptID = 1, a.ID
	hb.ReworkPreviewView.IssueID, hb.ReworkPreviewView.AttemptID = 1, a.ID
	m.garden.api = &handoffUIBackend{fb, hb}
	m = plannerPress(t, m, "enter")
	return m, hb, fb, a
}

func handoffCount(hb *gardenfake.HandoffBackend, call string) int {
	n := 0
	for _, c := range hb.Calls() {
		if c == call {
			n++
		}
	}
	return n
}

func TestGardenHandoffSamplesRenderTruthfullyAtEverySize(t *testing.T) {
	type want struct {
		narrow, wide []string
		never        []string
		attempt      bool
	}
	cases := map[gardenfake.HandoffSample]want{
		gardenfake.HandoffHumanReview:           {narrow: []string{"YOUR REVIEW"}, wide: []string{"AI review completed", "Fixes after review were not re-reviewed", "✓ Go tests", "PR #7 open · not merged", "THIS TICKET", "Spec a1b2c3d4"}, never: []string{"COMPLETED", "✓ Done"}, attempt: true},
		gardenfake.HandoffNoReviewNoChecks:      {narrow: []string{"YOUR REVIEW", "No checks recorded", "No AI review"}, wide: []string{"Findings: no AI review report"}, never: []string{"✓ AI review", "✓ Go tests", "COMPLETED"}, attempt: true},
		gardenfake.HandoffAcceptedAwaitingMerge: {narrow: []string{"YOUR REVIEW", "Waiting for the pull request to be merged"}, wide: []string{"Your decision: accepted"}, never: []string{"COMPLETED", "✓ Done"}, attempt: true},
		gardenfake.HandoffDone:                  {narrow: []string{"COMPLETED", "merged into base", "✓ Done", "The worktree is retained until you choose to clean it up."}, wide: []string{"Accepted by @", "commit 5555555"}, never: []string{"UNKNOWN"}, attempt: true},
		gardenfake.HandoffBlocked:               {narrow: []string{"BLOCKED", "Reopen or link a new PR"}, never: []string{"COMPLETED", "✓ Done"}, attempt: true},
		gardenfake.HandoffStale:                 {narrow: []string{"STALE", "Work changed after your acceptance; review again"}, never: []string{"COMPLETED", "✓ Done"}, attempt: true},
		gardenfake.HandoffOfflinePR:             {narrow: []string{"UNKNOWN", "GitHub unreachable; last good data shown"}, wide: []string{"status unknown: GitHub unreachable"}, never: []string{"COMPLETED", "✓ Done", "merged"}, attempt: true},
		gardenfake.HandoffChangesRequested:      {narrow: []string{"YOUR REVIEW"}, wide: []string{"changes requested", "Rename the counter"}, never: []string{"COMPLETED"}, attempt: true},
		gardenfake.HandoffNonCodeDone:           {narrow: []string{"COMPLETED", "Completed without code", "✓ Done"}, wide: []string{"decision comment #9003"}},
	}
	for _, sample := range gardenfake.HandoffSamples() {
		for _, size := range [][2]int{{80, 24}, {120, 35}, {160, 48}} {
			for _, glyphs := range []string{"unicode", "ascii"} {
				if glyphs == "ascii" && size[0] != 80 {
					continue // the ASCII map is size independent; the compact size is the tight one
				}
				t.Run(fmt.Sprintf("%s/%v/%s", sample, size, glyphs), func(t *testing.T) {
					c := cases[sample]
					m, _, _, _ := handoffUIEnv(t, size[0], size[1], sample, c.attempt, glyphs)
					mustFit(t, "handoff", m)
					out := view(m)
					wants := slices.Clone(c.narrow)
					if size[0] >= 120 {
						wants = append(wants, c.wide...)
					}
					if glyphs == "ascii" {
						for i, w := range wants {
							wants[i] = strings.NewReplacer("✓", "+", "·", "-", "…", "~").Replace(w)
						}
						for _, r := range out {
							if r > 127 && !unicode.Is(unicode.Mn, r) && r != '界' && r != 'é' { // the board fixture's own title
								t.Fatalf("non-ASCII %q in ascii mode:\n%s", r, out)
							}
						}
					}
					mustContain(t, string(sample), out, wants...)
					for _, bad := range c.never {
						if strings.Contains(out, bad) {
							t.Fatalf("%q drawn for %s:\n%s", bad, sample, out)
						}
					}
				})
			}
		}
	}
}

func TestGardenHandoffReadingNeverWrites(t *testing.T) {
	m, hb, fb, _ := handoffUIEnv(t, 120, 35, gardenfake.HandoffHumanReview, true, "")
	h := m.garden.handoff
	m = plannerPress(t, m, "tab", "tab", "esc", "esc")
	m, cmd := send(t, m, gardenHandoffTickMsg{gen: h.tickGen, issue: 1})
	m = plannerRun(t, m, cmd)
	for _, c := range hb.Calls() {
		if c != "Handoff" {
			t.Fatalf("navigation called %s: %v", c, hb.Calls())
		}
	}
	if fb.cleanups != 0 || fb.publishes != 0 || fb.stops != 0 {
		t.Fatalf("navigation had effects: %v", fb.calls)
	}
}

func TestGardenHandoffMissingCapabilityIsTruthful(t *testing.T) {
	m, _, fb, a := fakeLaunchUIHistory(t, 120, 35, false)
	fb.attempts[0].State, fb.attempts[0].Revision = launch.StateAwaitingReview, a.Revision+1
	m = plannerPress(t, m, "R", "esc")
	mustFit(t, "no handoff", m)
	out := view(m)
	mustContain(t, "unavailable", out, "Human handoff is unavailable")
	if strings.Contains(out, "YOUR REVIEW") {
		t.Fatalf("handoff drawn without the capability:\n%s", out)
	}
}

func TestGardenHandoffAcceptRequiresEveryAcknowledgmentAndStartsCancelFirst(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 35}, {160, 48}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m, hb, _, _ := handoffUIEnv(t, size[0], size[1], gardenfake.HandoffNoReviewNoChecks, true, "")
			hb.Acknowledgments = []string{"no-ai-review", "checks-missing-or-failed"}
			m = plannerPress(t, m, "a")
			mustFit(t, "accept confirmation", m)
			mustContain(t, "confirmation", view(m), "ACCEPT THIS WORK", "[ ] 1", "[ ] 2", "No AI review was run", "Checks are missing or failed", "Acceptance records your review. Merging remains a separate action.")
			if m.garden.board.overlayFocus != "ho-cancel" || len(hb.Decided) != 0 || len(hb.Previews) != 1 {
				t.Fatalf("not cancel-first: focus=%q decided=%d", m.garden.board.overlayFocus, len(hb.Decided))
			}
			// Enter on the default focus cancels.
			m = plannerPress(t, m, "enter")
			if m.garden.handoff.mode != handoffIdle || len(hb.Decided) != 0 {
				t.Fatal("Enter on Cancel did not cancel")
			}
			// Esc cancels too.
			m = plannerPress(t, m, "a", "esc")
			if m.garden.handoff.mode != handoffIdle || len(hb.Decided) != 0 {
				t.Fatal("Esc did not cancel")
			}
			m = plannerPress(t, m, "a", "tab", "enter")
			if !m.garden.handoff.acks[0] || m.garden.handoff.acks[1] || m.handoffConfirmable() {
				t.Fatal("first acknowledgment did not toggle alone")
			}
			m = plannerPress(t, m, "tab", "tab")
			if m.garden.board.overlayFocus != "ho-cancel" {
				t.Fatalf("a disabled Confirm took focus: %q", m.garden.board.overlayFocus)
			}
			// Numbers toggle too; Confirm is reachable only once every box is checked.
			m = plannerPress(t, m, "2", "tab", "tab", "tab")
			if m.garden.board.overlayFocus != "ho-confirm" || len(hb.Decided) != 0 {
				t.Fatalf("focus=%q decided=%d", m.garden.board.overlayFocus, len(hb.Decided))
			}
			var cmd tea.Cmd
			m, cmd = send(t, m, keyFor("enter"))
			m, _ = send(t, m, keyFor("enter")) // a second Enter while the write is out is ignored
			m = plannerRun(t, m, cmd)
			mustContain(t, "result", view(m), "Recorded on GitHub. Merging remains a separate action.")
			m = plannerPress(t, m, "enter") // and so is one after it answered: focus is gone
			if len(hb.Decided) != 1 || hb.Decided[0].Decision != garden.HandoffAccepted || len(hb.Decided[0].Acknowledgments) != 2 {
				t.Fatalf("decided = %+v", hb.Decided)
			}
			for _, c := range hb.Calls() {
				if c == "StartRework" || c == "OpenHandoffPR" {
					t.Fatalf("accept did more: %v", hb.Calls())
				}
			}
		})
	}
}

func TestGardenHandoffRequestChangesNeedsTextAndReworkIsSeparate(t *testing.T) {
	m, hb, _, a := handoffUIEnv(t, 120, 35, gardenfake.HandoffHumanReview, true, "")
	m = plannerPress(t, m, "c")
	if !m.handoffTyping() || m.shellFocus != shellInput {
		t.Fatal("text entry did not take the keyboard")
	}
	m = plannerPress(t, m, "ctrl+s")
	mustContain(t, "empty text", view(m), "Text is required")
	if len(hb.Previews) != 0 {
		t.Fatal("empty text was previewed")
	}
	m = typed(t, m, "Rename the counter")
	m = plannerPress(t, m, "ctrl+s")
	mustContain(t, "preview", view(m), "REQUEST CHANGES", "Rename the counter", "Records feedback on GitHub. Rework does not start automatically.")
	if len(hb.Previews) != 1 || hb.Previews[0].Text != "Rename the counter" || len(hb.Decided) != 0 || m.garden.board.overlayFocus != "ho-cancel" {
		t.Fatalf("previews=%+v decided=%d focus=%q", hb.Previews, len(hb.Decided), m.garden.board.overlayFocus)
	}
	changed := handoffRetarget(gardenfake.SampleHandoff(gardenfake.HandoffChangesRequested), a.ID)
	hb.View = changed
	m = plannerPress(t, m, "tab", "enter")
	if len(hb.Decided) != 1 || hb.Decided[0].Decision != garden.HandoffChangesRequested || handoffCount(hb, "StartRework") != 0 {
		t.Fatalf("decided=%+v calls=%v", hb.Decided, hb.Calls())
	}
	mustContain(t, "after request", view(m), "changes requested", "Rework")
	// Rework asks for its own confirmation and starts once.
	m = plannerPress(t, m, "m")
	mustContain(t, "rework confirmation", view(m), "START REWORK", "Rename the counter and cover the overflow case.", "Codex / personal", "sandbox: workspace-write")
	if len(hb.Reworked) != 0 || m.garden.board.overlayFocus != "ho-cancel" {
		t.Fatal("rework preview started work")
	}
	m = plannerPress(t, m, "esc")
	if len(hb.Reworked) != 0 {
		t.Fatal("canceled rework started")
	}
	m = plannerPress(t, m, "m", "tab")
	var cmd tea.Cmd
	m, cmd = send(t, m, keyFor("enter"))
	m, _ = send(t, m, keyFor("enter"))
	m = plannerRun(t, m, cmd)
	if len(hb.Reworked) != 1 || !hb.Reworked[0].Confirmed {
		t.Fatalf("reworked = %+v", hb.Reworked)
	}
}

func TestGardenHandoffReworkEscalationsNeedAcknowledgment(t *testing.T) {
	m, hb, _, _ := handoffUIEnv(t, 120, 35, gardenfake.HandoffChangesRequested, true, "")
	hb.ReworkPreviewView.Escalated = true
	hb.ReworkPreviewView.Review.Policy.Reviewer.Escalated = true
	hb.ReworkPreviewView.Review.Hash = strings.Repeat("a", 64)
	m = plannerPress(t, m, "m")
	mustContain(t, "escalations", view(m), "[ ] 1", "[ ] 2", "implementer's native permissions", "AI reviewer's native permissions")
	m = plannerPress(t, m, "tab", "tab", "tab")
	if m.garden.board.overlayFocus != "ho-cancel" || len(hb.Reworked) != 0 {
		t.Fatalf("unacknowledged rework reachable: %q", m.garden.board.overlayFocus)
	}
	m = plannerPress(t, m, "1", "2", "tab", "tab", "tab", "enter")
	if len(hb.Reworked) != 1 || !hb.Reworked[0].EscalationConfirmed || !hb.Reworked[0].ReviewEscalationConfirmed {
		t.Fatalf("reworked = %+v", hb.Reworked)
	}
}

func TestGardenHandoffActiveRunRefusesRework(t *testing.T) {
	m, hb, _, _ := handoffUIEnv(t, 120, 35, gardenfake.HandoffChangesRequested, true, "")
	hb.StartReworkErr = &gardenapi.Error{Code: gardenapi.CodeLaunchActive, Message: "raw"}
	m = plannerPress(t, m, "m", "tab", "enter")
	mustContain(t, "active run", view(m), "Previous run still active; stop it first.")
}

func TestGardenHandoffUnknownAndChangedWritesRefreshAndNeverRetry(t *testing.T) {
	for name, tc := range map[string]struct {
		decideErr error
		state     gardenapi.PlanWriteState
		want      string
	}{
		"unknown error": {decideErr: &gardenapi.Error{Code: gardenapi.CodeHandoffUnknown, Message: "x"}, want: "may have been recorded"},
		"unknown state": {state: gardenapi.PlanWriteUnknown, want: "may have been recorded"},
		"changed":       {decideErr: &gardenapi.Error{Code: gardenapi.CodeHandoffChanged, Message: "x"}, want: "Changed since preview; review again"},
	} {
		t.Run(name, func(t *testing.T) {
			m, hb, _, _ := handoffUIEnv(t, 120, 35, gardenfake.HandoffHumanReview, true, "")
			hb.DecideErr = tc.decideErr
			if tc.state != "" {
				hb.DecideView.State = tc.state
			}
			reads := handoffCount(hb, "Handoff")
			m = plannerPress(t, m, "a", "tab", "enter")
			mustContain(t, name, view(m), tc.want)
			if len(hb.Decided) != 1 || handoffCount(hb, "Handoff") != reads+1 {
				t.Fatalf("decided=%d reads=%d->%d", len(hb.Decided), reads, handoffCount(hb, "Handoff"))
			}
			m = plannerPress(t, m, "enter", "enter")
			if len(hb.Decided) != 1 {
				t.Fatal("the write was retried")
			}
		})
	}
}

func TestGardenHandoffStaleReadsAreDroppedAndFailureKeepsLastGoodView(t *testing.T) {
	m, hb, _, a := handoffUIEnv(t, 120, 35, gardenfake.HandoffHumanReview, true, "")
	h := m.garden.handoff
	done := handoffRetarget(gardenfake.SampleHandoff(gardenfake.HandoffDone), a.ID)
	done.Stamp = h.view.Stamp
	done.Stamp.Generation += 10
	// Older request generation: ignored.
	m, _ = send(t, m, gardenHandoffReadMsg{seq: h.seq - 1, issue: 1, view: done})
	// Other Issue: ignored.
	m, _ = send(t, m, gardenHandoffReadMsg{seq: h.seq, issue: 2, view: done})
	if strings.Contains(view(m), "COMPLETED") {
		t.Fatalf("a stale read changed the panel:\n%s", view(m))
	}
	// An older stamp generation than the accepted one is refused as well.
	older := handoffRetarget(gardenfake.SampleHandoff(gardenfake.HandoffDone), a.ID)
	older.Stamp = h.view.Stamp
	older.Stamp.Generation = 0
	m, _ = send(t, m, gardenHandoffReadMsg{seq: h.seq, issue: 1, view: older})
	if strings.Contains(view(m), "COMPLETED") || m.garden.handoff.readErr == "" {
		t.Fatal("an older generation upgraded the view")
	}
	// A failed refresh keeps the last good view, says so and never upgrades it.
	m, _ = send(t, m, gardenHandoffReadMsg{seq: m.garden.handoff.seq, issue: 1, err: context.DeadlineExceeded})
	out := view(m)
	mustContain(t, "failed refresh", out, "YOUR REVIEW", "refresh failed", "Last read")
	if strings.Contains(out, "COMPLETED") {
		t.Fatal("failed refresh upgraded the view")
	}
	// An obsolete tick starts nothing.
	reads := handoffCount(hb, "Handoff")
	m, cmd := send(t, m, gardenHandoffTickMsg{gen: m.garden.handoff.tickGen - 1, issue: 1})
	m = plannerRun(t, m, cmd)
	if handoffCount(hb, "Handoff") != reads {
		t.Fatal("obsolete tick read")
	}
}

func TestGardenHandoffFirstReadFailureIsNeverBlankOrGreen(t *testing.T) {
	m, _, fb := fakeLaunchUIEnv(t, 120, 35)
	p, err := fb.PreviewLaunch(context.Background(), gardenapi.LaunchPreviewRequest{IssueID: 1, AccountID: "c1", Branch: "b", BaseRef: "main", RemoteName: "origin", Name: "x", Cols: 100, Rows: 25})
	if err != nil {
		t.Fatal(err)
	}
	a := fakeLaunchUIAttempt(p.Preview)
	a.State = launch.StateAwaitingReview
	fb.attempts = []launch.Attempt{a}
	hb := gardenfake.NewHandoff(gardenfake.HandoffDone)
	hb.ViewErr = &gardenapi.Error{Code: gardenapi.CodeFailed, Message: "GitHub is down."}
	m.garden.api = &handoffUIBackend{fb, hb}
	m = plannerPress(t, m, "enter")
	mustFit(t, "failed first read", m)
	out := view(m)
	mustContain(t, "unknown", out, "UNKNOWN", "The handoff could not be read", "Nothing is shown as done")
	if strings.Contains(out, "COMPLETED") {
		t.Fatal("unreadable handoff shown as done")
	}
}

func TestGardenHandoffOpenPRAndCleanupReuseExistingFlows(t *testing.T) {
	m, hb, fb, _ := handoffUIEnv(t, 120, 35, gardenfake.HandoffHumanReview, true, "")
	m = plannerPress(t, m, "g")
	if len(hb.Opened) != 1 {
		t.Fatalf("PR not opened: %v", hb.Calls())
	}
	m = plannerPress(t, m, "w")
	mustContain(t, "cleanup confirmation", view(m), "Cancel")
	if fb.cleanupPreviews != 1 || fb.cleanups != 0 {
		t.Fatalf("cleanup previews=%d cleanups=%d", fb.cleanupPreviews, fb.cleanups)
	}
	m = plannerPress(t, m, "enter")
	if fb.cleanups != 0 {
		t.Fatal("Enter on the cancel-first cleanup confirmation cleaned")
	}
}

func TestGardenHandoffRefusedActionIsNotAButton(t *testing.T) {
	m, hb, _, _ := handoffUIEnv(t, 120, 35, gardenfake.HandoffHumanReview, true, "")
	out := view(m)
	if strings.Contains(out, "Revoke") || strings.Contains(out, "Start rework…") {
		t.Fatalf("refused action drawn as a button:\n%s", out)
	}
	mustContain(t, "hint", out, "Start rework unavailable: Request changes on the current result first.")
	m = plannerPress(t, m, "m", "u")
	if len(hb.Previews) != 0 || handoffCount(hb, "PreviewRework") != 0 {
		t.Fatal("a refused action called the backend")
	}
}

func TestGardenHandoffLateDecideResultNeverTouchesANewerWrite(t *testing.T) {
	m, hb, _, _ := handoffUIEnv(t, 120, 35, gardenfake.HandoffHumanReview, true, "")
	m = plannerPress(t, m, "a", "tab")
	m, cmd := send(t, m, keyFor("enter"))
	old, ok := cmd().(gardenHandoffDecideMsg)
	if !ok || !m.garden.handoff.inflight {
		t.Fatalf("no write in flight: %T", cmd())
	}
	// The Issue is left and opened again: the state is new, the old write is still out.
	m.handoffReset(m.garden.handoff.issue)
	m = handoffUpdate(m, old)
	if m.garden.handoff.note != "" || m.garden.handoff.inflight {
		t.Fatalf("a late result changed a new state: %+v", m.garden.handoff.note)
	}
	// A second write of the same Issue is in flight; the old result must not clear it.
	var read tea.Cmd
	m, read = m.handoffSync()
	m = plannerRun(t, m, read)
	m = plannerPress(t, m, "a", "tab")
	m, _ = send(t, m, keyFor("enter"))
	if !m.garden.handoff.inflight {
		t.Fatal("second write not in flight")
	}
	reads := handoffCount(hb, "Handoff")
	m = handoffUpdate(m, old)
	if !m.garden.handoff.inflight || m.garden.handoff.note != "" || handoffCount(hb, "Handoff") != reads {
		t.Fatal("a late result cleared or applied to the newer write")
	}
	// A result for another attempt's preview is ignored too.
	other := old
	other.wseq, other.attempt = m.garden.handoff.writeSeq, "other-attempt"
	m = handoffUpdate(m, other)
	if !m.garden.handoff.inflight || m.garden.handoff.note != "" {
		t.Fatal("a result for another preview was applied")
	}
}

func handoffUpdate(m Model, msg tea.Msg) Model {
	n, _ := m.Update(msg)
	return n.(Model)
}

func TestGardenHandoffNoOpenChangesControl(t *testing.T) {
	m, _, _, _ := handoffUIEnv(t, 120, 35, gardenfake.HandoffHumanReview, true, "")
	if strings.Contains(view(m), "Open changes") {
		t.Fatal("a control that opens nothing is drawn")
	}
	m = plannerPress(t, m, "d")
	if m.garden.notice != "" {
		t.Fatalf("d claimed something: %q", m.garden.notice)
	}
}

var _ = handoff.CompletionDone
