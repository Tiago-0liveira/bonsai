package gardenbackend

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
	gardenfake "github.com/Tiago-0liveira/bonsai/internal/core/garden/fake"
	gardensync "github.com/Tiago-0liveira/bonsai/internal/core/garden/sync"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/work"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

func seedBoardIssue(t *testing.T, f *gardenfake.Fake) {
	t.Helper()
	ctx := context.Background()
	spec := "Canonical specification"
	hash := garden.ContentHash(spec)
	meta, err := garden.EncodeMetadata(garden.Metadata{SchemaVersion: 1, Provenance: "fixture", Owner: "fake-authenticated-id", Accepted: true, AcceptedSpecHash: hash, SpecFingerprint: hash})
	if err != nil {
		t.Fatal(err)
	}
	f.SeedIssue(garden.Issue{Repo: acmeID, ID: 20, Number: 101, Title: "Real reader identity", Metadata: meta, Labels: []string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeBug), garden.FormatStageLabel(garden.StageReady)}})
	add := func(e garden.CommentEnvelope, text string) {
		t.Helper()
		e.SchemaVersion = 1
		e.RepoID = acmeID
		e.IssueID = 20
		body, err := garden.EncodeCommentEnvelope(e, text)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.AppendComment(ctx, acmeID, 20, body); err != nil {
			t.Fatal(err)
		}
	}
	add(garden.CommentEnvelope{Kind: garden.CommentRevision, RevisionKind: garden.RevisionSpec, ContentHash: hash}, spec)
	add(garden.CommentEnvelope{Kind: garden.CommentApproval, RevisionHash: hash, RevisionCommentID: 1, SourceSpecFingerprint: hash}, "Approved")
}
func workHarness(t *testing.T) (*harness, *gardenfake.Fake) {
	t.Helper()
	h := newHarness(t, connect.AccessWrite)
	f := gardenfake.New()
	h.b.deps.Issues = f
	h.b.deps.Viewer = func(context.Context) (connect.GitHubIdentity, error) {
		return connect.GitHubIdentity{ID: "fake-authenticated-id", Login: viewerName}, nil
	}
	h.seedCanonicalLabels()
	p := h.preview(h.b, gardenapi.PlanRequest{Mode: gardenapi.ModeCreate})
	if _, err := h.b.Apply(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	seedBoardIssue(t, f)
	return h, f
}
func TestWorkUsesVerifiedReaderAndPreservesLastSuccessThroughOfflineAndEmpty(t *testing.T) {
	h, f := workHarness(t)
	ctx := context.Background()
	v, err := h.b.Work(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Snapshot.Complete || v.Snapshot.SuccessfulAt.IsZero() || len(work.Select(v.Snapshot.Items, work.Query{}).Records) != 1 {
		t.Fatalf("not verified: %+v", v)
	}
	last := v.Snapshot.SuccessfulAt
	f.FailNext(garden.ErrOffline)
	failed, err := h.b.Work(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Snapshot.Source != gardensync.SourceStale || len(failed.Snapshot.Items) != 1 || !failed.Snapshot.SuccessfulAt.Equal(last) || failed.Stamp.Generation <= v.Stamp.Generation {
		t.Fatalf("last-good lost: %+v", failed)
	}
	status, err := h.b.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !status.LastSync.Equal(last) || status.Sync != "offline" {
		t.Fatalf("status disagrees: %+v", status)
	}
	f.ReplaceIssues(acmeID, nil)
	empty, err := h.b.Work(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !empty.Snapshot.Complete || len(work.Select(empty.Snapshot.Items, work.Query{}).Records) != 0 {
		t.Fatal("successful empty retained work")
	}
	before, _, err := h.b.association(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err = h.b.Work(ctx); err != nil {
			t.Fatal(err)
		}
	}
	after, _, err := h.b.association(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("read changed association")
	}
}
func TestWorkPartialReadAndRevokedAccessKeepBoard(t *testing.T) {
	h, f := workHarness(t)
	ctx := context.Background()
	good, err := h.b.Work(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, failure := range []error{garden.ErrPermissionDenied, garden.ErrAuthRequired, garden.ErrRateLimited} {
		f.FailNext(failure)
		v, err := h.b.Work(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if v.Snapshot.Complete || len(v.Snapshot.Items) != 1 || !v.Snapshot.SuccessfulAt.Equal(good.Snapshot.SuccessfulAt) {
			t.Fatalf("failure erased board: %+v", v)
		}
	}
}
func TestWorkReadOnlyAndSkipCapabilities(t *testing.T) {
	h := newHarness(t, connect.AccessRead)
	f := gardenfake.New()
	h.b.deps.Issues = f
	h.b.deps.Viewer = func(context.Context) (connect.GitHubIdentity, error) {
		return connect.GitHubIdentity{ID: "fake-authenticated-id", Login: viewerName}, nil
	}
	seedBoardIssue(t, f)
	v, err := h.b.Work(context.Background())
	if err != nil || len(work.Select(v.Snapshot.Items, work.Query{}).Records) != 1 {
		t.Fatalf("read-only: %+v %v", v, err)
	}
	if _, err = h.b.Skip(context.Background(), gardenapi.TargetRef{}); err != nil {
		t.Fatal(err)
	}
	_, err = h.b.Work(context.Background())
	mustErrCode(t, err, gardenapi.CodeSkipped)
}

type heldIssues struct {
	garden.IssueReader
	entered chan struct{}
	release chan struct{}
}

func (r heldIssues) ListManagedIssues(ctx context.Context, repo garden.RepoID, cursor string) (garden.IssuePage, error) {
	close(r.entered)
	select {
	case <-r.release:
		return r.IssueReader.ListManagedIssues(ctx, repo, cursor)
	case <-ctx.Done():
		return garden.IssuePage{}, ctx.Err()
	}
}
func TestWorkReassociationRejectsLateCollection(t *testing.T) {
	h, f := workHarness(t)
	if _, err := h.b.Work(context.Background()); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	h.b.deps.Issues = heldIssues{f, entered, release}
	done := make(chan error, 1)
	go func() { _, err := h.b.Work(context.Background()); done <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("reader never started")
	}
	h.b.reassociate(garden.GardenID{Repo: acmeID + 1})
	close(release)
	select {
	case err := <-done:
		if err == nil || len(h.b.Reads().Read().Items) != 0 {
			t.Fatal("late collection landed after epoch changed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reader never completed")
	}
}

func TestWorkWithoutRepositoryNeverReturnsSuccessfulEmptyView(t *testing.T) {
	for _, invalidID := range []bool{false, true} {
		t.Run(fmt.Sprint(invalidID), func(t *testing.T) {
			h := newHarness(t, connect.AccessRead)
			if invalidID {
				for remote, repo := range h.gh.Repositories {
					repo.ID = 0
					h.gh.Repositories[remote] = repo
				}
			} else {
				h.gh.Remotes = nil
			}
			view, err := h.b.Work(context.Background())
			mustErrCode(t, err, gardenapi.CodeNoGitHubRemote)
			if view.Stamp.GardenID != 0 || h.b.Reads() != nil {
				t.Fatal("missing repository created a successful read")
			}
		})
	}
}
