package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
	gardenfake "github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi/fake"
)

// openValidate opens draft #142 and previews its validation, as a user does with Enter and v.
func openValidate(t *testing.T, fb *gardenfake.Backend, w, h int) Model {
	t.Helper()
	m := draftsEnv(t, fb, w, h)
	m = pressAll(t, m, "enter", "v")
	if m.garden.drafts.stage != draftsValidate {
		t.Fatalf("v did not open the validation: stage %d\n%s", m.garden.drafts.stage, view(m))
	}
	return m
}

func TestValidationPreviewNamesTheExactTransitionAndWritesNothing(t *testing.T) {
	for _, sz := range draftSizes {
		fb := draftsFake(t)
		m := openValidate(t, fb, sz[0], sz[1])
		out := view(m)
		mustContain(t, "preview", out, "VALIDATE SPECIFICATION s1?", "Issue #142", "fingerprint", "Result: Draft → Approved",
			"This records specification approval only.", "A planner starts through a separate local action.", "Validate specification s1")
		mustNotContain(t, "preview", out, "Plan review", "Planning")
		mustFit(t, "preview", m)
		if fb.Writes() != 0 || fb.Count("PreviewValidate") != 1 {
			t.Fatalf("writes=%d previews=%d", fb.Writes(), fb.Count("PreviewValidate"))
		}
		mustContain(t, "chrome", strings.Join(m.gardenDraftsCrumbs(), " > "), "#142", "Validate specification")
		// Back is the first focus: Enter alone changes nothing.
		m = pressAll(t, m, "enter")
		if m.garden.drafts.stage != draftsDetail || fb.Writes() != 0 {
			t.Fatalf("Enter on Back: stage %d writes %d", m.garden.drafts.stage, fb.Writes())
		}
	}
}

func TestValidatingMovesTheSameIssueFromDraftsToTheApprovedBoard(t *testing.T) {
	for _, sz := range draftSizes {
		fb := draftsFake(t)
		m := openValidate(t, fb, sz[0], sz[1])
		m = pressAll(t, m, "right", "enter")
		if fb.Count("Validate") != 1 || fb.Writes() != 1 || len(fb.Validated) != 1 || fb.Validated[0].IssueID != 142 {
			t.Fatalf("Validate calls %d writes %d", fb.Count("Validate"), fb.Writes())
		}
		d := m.garden.drafts
		if d.stage != draftsList || d.validate != nil || d.opened != 0 {
			t.Fatalf("after validate: stage %d", d.stage)
		}
		out := view(m)
		mustContain(t, "result", out, "Validated #142: specification s1 is approved")
		mustNotContain(t, "inbox", out, "Per-repository agent budget limits  ")
		if _, _, still := d.collection.Find(142); still {
			t.Fatal("approved Issue is still a draft")
		}
		mustFit(t, "result", m)
		b := pressAll(t, m, "v")
		rec, _, ok := b.garden.board.collection.Find(142)
		if !ok || rec.Column.String() != "Approved" || rec.Item.Classification.Type != garden.TypeFeature {
			t.Fatalf("board record = %+v ok=%v", rec, ok)
		}
		mustContain(t, "board", view(b), "Approved")
	}
}

func TestValidationIsUnavailableWhenBlockedAndOffersOnlyBack(t *testing.T) {
	cases := []struct {
		name string
		keys []string
		want []string
	}{
		{"no specification revision", []string{"down", "enter", "v"}, []string{"CANNOT VALIDATE SPECIFICATION YET", "No specification revision exists yet"}},
		{"competing revision heads", []string{"down", "down", "enter", "v"}, []string{"CANNOT VALIDATE SPECIFICATION YET", "Several specification revisions compete", "needs attention"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fb := draftsFake(t)
			m := draftsEnv(t, fb, 120, 35)
			m = pressAll(t, m, tc.keys...)
			out := view(m)
			mustContain(t, tc.name, out, tc.want...)
			mustNotContain(t, tc.name, out, "Validate specification s", "A planner starts")
			mustFit(t, tc.name, m)
			m = pressAll(t, m, "right", "enter")
			if fb.Count("Validate") != 0 || fb.Writes() != 0 || m.garden.drafts.stage != draftsDetail {
				t.Fatalf("blocked preview wrote: validate=%d stage=%d", fb.Count("Validate"), m.garden.drafts.stage)
			}
		})
	}
}

func TestValidationNeedsWriteAccess(t *testing.T) {
	fb := draftsFake(t)
	fb.StatusView.Access = gardenapi.AccessRead
	fb.StatusView.Target.Access = gardenapi.AccessRead
	m := draftsEnv(t, fb, 120, 35)
	m = pressAll(t, m, "enter", "v")
	mustContain(t, "read-only", view(m), "Validating a specification needs Write access to acme/my-app")
	if m.garden.drafts.stage != draftsDetail || fb.Count("PreviewValidate") != 0 {
		t.Fatal("read-only user reached validation")
	}
}

func TestValidateWriteIsNeverCancelledAndIgnoresKeys(t *testing.T) {
	fb := draftsFake(t)
	m := openValidate(t, fb, 120, 35)
	release := make(chan struct{})
	fb.Hook = func(method string) {
		if method == "Validate" {
			<-release
		}
	}
	m = pressAll(t, m, "right")
	m, cmd := send(t, m, keyOf("enter"))
	if cmd == nil || m.garden.busy != busyValidate {
		t.Fatalf("busy %v", m.garden.busy)
	}
	mustContain(t, "pending", view(m), "Validating…")
	for _, k := range []string{"esc", "enter", "v", "tab"} {
		var c tea.Cmd
		m, c = send(t, m, keyOf(k))
		if c != nil || m.garden.busy != busyValidate || m.garden.drafts.stage != draftsValidate {
			t.Fatalf("%s was not swallowed", k)
		}
	}
	close(release)
	m, _ = drainAll(t, m, cmd)
	if fb.Count("Validate") != 1 || m.garden.drafts.stage != draftsList {
		t.Fatalf("Validate %d stage %d", fb.Count("Validate"), m.garden.drafts.stage)
	}
}

func TestValidatePreviewCanBeAbandonedWithoutAnyWrite(t *testing.T) {
	fb := draftsFake(t)
	m := draftsEnv(t, fb, 120, 35)
	m = pressAll(t, m, "enter")
	release := make(chan struct{})
	fb.Hook = func(method string) {
		if method == "PreviewValidate" {
			<-release
		}
	}
	m, cmd := send(t, m, runeKey("v"))
	if m.garden.busy != busyPreviewValidate {
		t.Fatalf("busy %v", m.garden.busy)
	}
	m = pressAll(t, m, "esc")
	close(release)
	m, _ = drainAll(t, m, cmd)
	if m.garden.drafts.stage != draftsDetail || m.garden.drafts.validate != nil || fb.Writes() != 0 {
		t.Fatalf("late preview landed: stage %d", m.garden.drafts.stage)
	}
}

func TestLateOrOutOfOrderValidateRepliesNeverChangeTheScreen(t *testing.T) {
	fb := draftsFake(t)
	m := openValidate(t, fb, 120, 35)
	// A reply for an abandoned request is dropped by its sequence number.
	m, _ = send(t, m, gardenValidateMsg{seq: m.garden.seq + 7, view: gardenapi.ValidateView{State: gardenapi.ValidateDone}})
	if m.garden.drafts.stage != draftsValidate {
		t.Fatal("a reply for another request moved the screen")
	}
	// A reply of the right request but an older generation is rejected by the gate.
	stale := gardenapi.ValidateView{Stamp: gardenapi.Stamp{LocalRepoID: gardenfake.Clone, Generation: 1, Epoch: 1}, State: gardenapi.ValidateDone}
	m.garden.busy = busyValidate
	m, _ = send(t, m, gardenValidateMsg{seq: m.garden.seq, view: stale})
	if m.garden.drafts.stage != draftsValidate || m.garden.err == nil {
		t.Fatalf("stale reply accepted: stage %d err %v", m.garden.drafts.stage, m.garden.err)
	}
}

func TestPartialAndUnknownResultsStayOnTheScreenAndOfferValidateAgain(t *testing.T) {
	for _, state := range []gardenapi.ValidateState{gardenapi.ValidatePartial, gardenapi.ValidateUnknown} {
		fb := draftsFake(t)
		fb.ValidateResult = &gardenapi.ValidateView{State: state, ApprovalCommentID: 77,
			Err: &gardenapi.Error{Code: gardenapi.CodeFailed, Message: "The approval is recorded, but the Issue could not be updated.", Next: "Validate again to finish."}}
		m := openValidate(t, fb, 120, 35)
		m = pressAll(t, m, "right", "enter")
		out := view(m)
		mustContain(t, string(state), out, "The approval is recorded", "Validate again to finish.", "Validate again")
		if m.garden.drafts.stage != draftsValidate || fb.Count("Validate") != 1 {
			t.Fatalf("%s: stage %d", state, m.garden.drafts.stage)
		}
		mustFit(t, string(state), m)
		fb.ValidateResult = nil
		m = pressAll(t, m, "enter") // focus stays on the primary button
		if fb.Count("Validate") != 2 || m.garden.drafts.stage != draftsList {
			t.Fatalf("%s: second Validate %d stage %d", state, fb.Count("Validate"), m.garden.drafts.stage)
		}
	}
}

func TestSpecificationChangedDuringValidationReturnsToTheIssueAndRereads(t *testing.T) {
	fb := draftsFake(t)
	m := openValidate(t, fb, 120, 35)
	fb.ValidateErr = &gardenapi.Error{Code: gardenapi.CodeSpecChanged, Message: "The specification changed since the preview, so nothing was approved.", Next: "Read the new revision."}
	works := fb.Count("Work")
	m = pressAll(t, m, "right", "enter")
	if m.garden.drafts.stage != draftsDetail || fb.Count("Work") != works+1 {
		t.Fatalf("stage %d work reads %d->%d", m.garden.drafts.stage, works, fb.Count("Work"))
	}
	mustContain(t, "changed", view(m), "nothing was approved")
}
