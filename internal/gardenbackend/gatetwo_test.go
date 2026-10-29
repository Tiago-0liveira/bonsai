package gardenbackend

import (
	"context"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/work"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

func publishForApproval(t *testing.T, h *harness, id uint64, attempt, text string) gardenapi.PlanReviewView {
	t.Helper()
	ctx := context.Background()
	p, err := h.b.PreviewPlanPublish(ctx, gardenapi.PlanPublishRequest{IssueID: id, AttemptID: attempt, Text: text})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.b.PublishPlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	rv, err := h.b.PlanReview(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return rv
}

func exactApproval(v gardenapi.PlanReviewView) gardenapi.PlanDecisionRequest {
	return gardenapi.PlanDecisionRequest{Stamp: v.Stamp, IssueID: v.IssueID, ActorID: v.ActorID, SpecHash: v.Review.Gate.Head.Hash, RevisionHash: v.Review.Current.Hash, Fingerprint: v.Review.Gate.Fingerprint, Decision: garden.DecisionApproved}
}

func TestGateTwoThroughBackendAndBoard(t *testing.T) {
	for _, changed := range []string{"spec", "plan"} {
		t.Run(changed, func(t *testing.T) {
			h, f, id, ag, attempt := reviewHarness(t)
			ctx := context.Background()
			rv := publishForApproval(t, h, id, attempt, backendPlan)
			before := len(gateWrites(f))
			req := exactApproval(rv)
			if _, err := h.b.DecidePlan(ctx, req); err != nil {
				t.Fatal(err)
			}
			rv, err := h.b.PlanReview(ctx, id)
			if err != nil || !rv.Review.Ready {
				t.Fatalf("Ready=%v err=%v", rv.Review.Ready, err)
			}
			snapshot := rv.Review.Approved.Content
			w, err := h.b.Work(ctx)
			if err != nil {
				t.Fatal(err)
			}
			rows := work.Select(w.Snapshot.Items, work.Query{}, nil)
			r, _, ok := rows.Find(garden.IssueID(id))
			if !ok || r.Column != work.Ready {
				t.Fatalf("Board=%+v", rows)
			}
			if len(gateWrites(f)) != before || len(ag.started) != 1 || creates(f) != 1 {
				t.Fatal("gate two projected metadata, created worktree/issue or started an agent")
			}
			if changed == "plan" {
				publishForApproval(t, h, id, attempt, backendPlan+"\n\n## Notes\nNew revision.")
			} else {
				body, err := garden.NewSpecRevision(garden.IssueIdentity{Repo: acmeID, Issue: garden.IssueID(id)}, completeSpec+"\n\n## Constraints\nNew limit.", []string{req.SpecHash})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = f.AppendComment(ctx, acmeID, garden.IssueID(id), body); err != nil {
					t.Fatal(err)
				}
			}
			rv, err = h.b.PlanReview(ctx, id)
			if err != nil || rv.Review.Ready || !rv.Review.StaleApproval || rv.Review.Approved.Content != snapshot {
				t.Fatalf("stale=%+v err=%v", rv.Review, err)
			}
			w, err = h.b.Work(ctx)
			if err != nil {
				t.Fatal(err)
			}
			rows = work.Select(w.Snapshot.Items, work.Query{}, nil)
			if _, _, ok = rows.Find(garden.IssueID(id)); !ok || len(rows.Columns[work.Ready]) != 0 || work.Drafts(w.Snapshot.Items, work.Query{}).Total != 0 {
				t.Fatal("stale accepted work left Board or stayed Ready")
			}
		})
	}
}

func TestGateTwoRechecksAuthorityAndScopeAtConfirmation(t *testing.T) {
	for _, changed := range []string{"account", "permissions", "plan", "spec", "epoch"} {
		t.Run(changed, func(t *testing.T) {
			h, f, id, ag, attempt := reviewHarness(t)
			ctx := context.Background()
			rv := publishForApproval(t, h, id, attempt, backendPlan)
			req := exactApproval(rv)
			switch changed {
			case "account":
				h.b.deps.Viewer = func(context.Context) (connect.GitHubIdentity, error) {
					return connect.GitHubIdentity{ID: "999", Login: "other"}, nil
				}
			case "permissions":
				h.gh.Repositories[acmeURL] = acmeRepo(connect.AccessRead)
				h.gh.Repositories[acmeSSH] = acmeRepo(connect.AccessRead)
			case "plan":
				publishForApproval(t, h, id, attempt, backendPlan+"\n\nChanged")
			case "spec":
				body, _ := garden.NewSpecRevision(garden.IssueIdentity{Repo: acmeID, Issue: garden.IssueID(id)}, completeSpec+"\n\n## Constraints\nNew limit.", []string{req.SpecHash})
				f.AppendComment(ctx, acmeID, garden.IssueID(id), body)
			case "epoch":
				req.Stamp.Epoch++
			}
			before := len(f.WriteLog())
			if _, err := h.b.DecidePlan(ctx, req); err == nil {
				t.Fatal("stale confirmation accepted")
			}
			if len(f.WriteLog()) != before || len(ag.started) != 1 {
				t.Fatal("refusal wrote or started an agent")
			}
		})
	}
}

func TestIssueLinkActionsUseCanonicalURL(t *testing.T) {
	h, _, id, _, _ := reviewHarness(t)
	ctx := context.Background()
	var err error
	var opened, copied string
	h.b.deps.OpenURL = func(s string) error { opened = s; return nil }
	h.b.deps.CopyText = func(s string) error { copied = s; return nil }
	if err = h.b.IssueLink(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	if err = h.b.IssueLink(ctx, id, false); err != nil {
		t.Fatal(err)
	}
	if opened != copied || !strings.HasPrefix(opened, "https://github.com/acme/my-app/issues/") {
		t.Fatalf("open=%q copy=%q", opened, copied)
	}
}
