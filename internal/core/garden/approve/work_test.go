package approve_test

import (
	"context"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
	gardenfake "github.com/Tiago-0liveira/bonsai/internal/core/garden/fake"
	gardensync "github.com/Tiago-0liveira/bonsai/internal/core/garden/sync"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/work"
	"strings"
	"testing"
)

func TestApprovedIssueLeavesDraftsAndKeepsItsType(t *testing.T) {
	ctx := context.Background()
	f := gardenfake.New()
	target := approve.Target{Repo: 9, Issue: 7, Viewer: "fake-authenticated-id"}
	body, err := garden.CaptureBody(target.Viewer, strings.Repeat("a", 32), "Idea")
	if err != nil {
		t.Fatal(err)
	}
	f.SeedIssue(garden.Issue{Repo: 9, ID: 7, Number: 7, Body: body, Metadata: garden.BodyMetadata(body), Labels: []string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeBug), garden.FormatStageLabel(garden.StageDraft)}})
	content := "## Summary\nCap spend.\n\n## Requirements\nA monthly ceiling.\n\n## Acceptance criteria\nRequests stop.\n\n## Open questions\nNone"
	rev, err := garden.NewSpecRevision(garden.IssueIdentity{Repo: 9, Issue: 7}, content, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.AppendComment(ctx, 9, 7, rev); err != nil {
		t.Fatal(err)
	}
	svc, err := approve.New(approve.Deps{Reader: f, Writer: f})
	if err != nil {
		t.Fatal(err)
	}
	item := func() gardensync.Item {
		log, err := svc.Read(ctx, target)
		if err != nil {
			t.Fatal(err)
		}
		return gardensync.Item{Issue: log.Issue, Verification: log.Verification}
	}
	if work.Drafts([]gardensync.Item{item()}, work.Query{}).Total != 1 || len(work.Select([]gardensync.Item{item()}, work.Query{}).Records) != 0 {
		t.Fatal("draft must start in Drafts only")
	}
	pv, err := svc.Preview(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Validate(ctx, pv); err != nil {
		t.Fatal(err)
	}
	rows := work.Select([]gardensync.Item{item()}, work.Query{}).Records
	if work.Drafts([]gardensync.Item{item()}, work.Query{}).Total != 0 || len(rows) != 1 {
		t.Fatalf("work=%v", rows)
	}
	if rows[0].Column != work.Approved || rows[0].Item.Classification.Type != garden.TypeBug {
		t.Fatal("wrong stage/type")
	}
}
