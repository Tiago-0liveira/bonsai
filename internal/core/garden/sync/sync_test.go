package sync

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
)

type testReader struct {
	pages       []garden.IssuePage
	pageErr     []error
	commentsErr error
	call        int
	get         map[garden.IssueID]garden.Issue
	onList      func(context.Context, int) (garden.IssuePage, error)
}

func (r *testReader) ListManagedIssues(ctx context.Context, _ garden.RepoID, _ string) (garden.IssuePage, error) {
	n := r.call
	r.call++
	if r.onList != nil {
		return r.onList(ctx, n)
	}
	if n < len(r.pageErr) && r.pageErr[n] != nil {
		return garden.IssuePage{}, r.pageErr[n]
	}
	if n >= len(r.pages) {
		return garden.IssuePage{Complete: true}, nil
	}
	return r.pages[n], nil
}
func (r *testReader) GetIssue(_ context.Context, _ garden.RepoID, id garden.IssueID) (garden.Issue, error) {
	return r.get[id], nil
}
func (r *testReader) ListComments(context.Context, garden.RepoID, garden.IssueID) ([]garden.IssueComment, error) {
	return nil, r.commentsErr
}

type projectReader struct {
	projects []garden.Project
	err      error
}

func (p projectReader) ListProjects(context.Context, garden.RepoID) ([]garden.Project, error) {
	return p.projects, p.err
}
func validIssue(id garden.IssueID, labels ...string) garden.Issue {
	return garden.Issue{Repo: 7, ID: id, Labels: append([]string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeBug), garden.FormatStageLabel(garden.StageReady)}, labels...), Metadata: []byte(`{"schema_version":1,"provenance":"import","owner":"owner"}`)}
}
func request(reader garden.IssueReader) Request {
	return Request{GardenID: garden.GardenID{Repo: 7}, LocalRepoID: "clone-a", Generation: 1, Epoch: 1, Issues: reader, Limits: Limits{PageCap: 10, ItemCap: 100, CallTimeout: time.Second}}
}

func TestPaginationSizesAndCaps(t *testing.T) {
	for _, size := range []int{1, 2, 5, 6} {
		t.Run(string(rune('a'+size)), func(t *testing.T) {
			r := &testReader{}
			for start := 0; start < 5; start += size {
				end := start + size
				if end > 5 {
					end = 5
				}
				p := garden.IssuePage{}
				for n := start; n < end; n++ {
					p.Items = append(p.Items, validIssue(garden.IssueID(n+1)))
				}
				p.NextCursor = string(rune('1' + end))
				if end == 5 {
					p.NextCursor = ""
					p.Complete = true
				}
				r.pages = append(r.pages, p)
			}
			q := request(r)
			s := Load(context.Background(), q)
			if !s.Complete || len(s.Items) != 5 {
				t.Fatalf("size %d: complete=%v items=%d", size, s.Complete, len(s.Items))
			}
		})
	}
	r := &testReader{pages: []garden.IssuePage{{Items: []garden.Issue{validIssue(1)}, NextCursor: "next"}, {Items: []garden.Issue{validIssue(2)}, NextCursor: "next"}}}
	q := request(r)
	q.Limits.PageCap = 1
	s := Load(context.Background(), q)
	if s.Complete || s.Source != SourceStale || !hasProblem(s.Problems, "page-cap") {
		t.Fatalf("cap result: %+v", s)
	}
}

func TestCancelMidPageAndCursorRestart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &testReader{onList: func(ctx context.Context, n int) (garden.IssuePage, error) {
		if n == 0 {
			return garden.IssuePage{Items: []garden.Issue{validIssue(1)}, NextCursor: "cursor"}, nil
		}
		cancel()
		<-ctx.Done()
		return garden.IssuePage{}, ctx.Err()
	}}
	s := Load(ctx, request(r))
	if s.Complete || !hasProblem(s.Problems, "cancelled") {
		t.Fatalf("cancel result %+v", s)
	}
	r = &testReader{pages: []garden.IssuePage{{Items: []garden.Issue{validIssue(1)}, Complete: true}}, pageErr: []error{garden.ErrInvalidCursor}}
	s = Load(context.Background(), request(r))
	if !s.Complete || r.call != 2 {
		t.Fatalf("restart once: calls=%d snapshot=%+v", r.call, s)
	}
	r = &testReader{pageErr: []error{garden.ErrInvalidCursor, garden.ErrInvalidCursor}}
	s = Load(context.Background(), request(r))
	if s.Complete || r.call != 2 || !hasProblem(s.Problems, "read-failed") {
		t.Fatalf("second cursor error not visible: %+v", s)
	}
}

func TestFindingsProjectionAndUnavailable(t *testing.T) {
	r := &testReader{pages: []garden.IssuePage{{Items: []garden.Issue{validIssue(1, garden.FormatStageLabel(garden.StageDraft))}, Complete: true}}}
	q := request(r)
	q.ProjectID = "P"
	q.StageField = "Stage"
	q.TypeField = "Type"
	q.Projects = projectReader{projects: []garden.Project{{ID: "P", IssueFields: map[garden.IssueID]map[string]string{1: {"Stage": "Done", "Type": "bug"}}}}}
	s := Load(context.Background(), q)
	if !hasFinding(s.Findings, FindingProjectionDrift) {
		t.Fatalf("drift missing: %+v", s.Findings)
	}
	q.Projects = projectReader{err: garden.ErrPermissionDenied}
	s = Load(context.Background(), q)
	if !hasFinding(s.Findings, FindingProjectionUnavailable) || hasFinding(s.Findings, FindingProjectionDrift) {
		t.Fatalf("unavailable projection findings: %+v", s.Findings)
	}
	conflict := buildItem(context.Background(), request(&testReader{}), validIssue(3, garden.FormatStageLabel(garden.StageDone)))
	if !hasFinding(itemFindings(conflict), FindingStageConflict) {
		t.Fatalf("stage conflict missing: %+v", itemFindings(conflict))
	}
	stale := Item{Issue: garden.Issue{ID: 4}, Verification: garden.Verification{SupersededApprovals: []garden.Approval{{}}}}
	if !hasFinding(itemFindings(stale), FindingStaleApproval) {
		t.Fatal("stale approval finding missing")
	}
}

func TestStoreFailuresDeletedAccessOfflineEpochAndClones(t *testing.T) {
	id := garden.GardenID{Repo: 7}
	a := NewStore(id, "clone-a")
	b := NewStore(id, "clone-b")
	if LocalKey("clone-a") == LocalKey("clone-b") {
		t.Fatal("clone local keys collided")
	}
	good := Snapshot{GardenID: id, LocalRepoID: "clone-a", Epoch: 1, Generation: 1, Complete: true, Source: SourceLive, Items: []Item{{Issue: garden.Issue{ID: 1}}}}
	_ = a.Apply(good)
	if len(b.Read().Items) != 0 {
		t.Fatal("clones share snapshot state")
	}
	_ = a.Apply(Snapshot{GardenID: id, LocalRepoID: "clone-a", Epoch: 1, Generation: 2, Complete: false, Source: SourceStale})
	if len(a.Read().Items) != 1 || a.Read().Source != SourceStale {
		t.Fatal("incomplete read cleared last-good")
	}
	_ = a.Apply(Snapshot{GardenID: id, LocalRepoID: "clone-a", Epoch: 1, Generation: 2, Complete: true, Source: SourceLive})
	if a.Dropped() != 1 || len(a.Read().Items) != 1 {
		t.Fatal("late generation overwrote newer state")
	}
	if err := a.Apply(Snapshot{GardenID: garden.GardenID{Repo: 8}, LocalRepoID: "clone-a", Epoch: 1, Generation: 9, Complete: true}); !errors.Is(err, ErrForeignGarden) {
		t.Fatalf("foreign Garden accepted: %v", err)
	}
	_ = a.Apply(Snapshot{GardenID: id, LocalRepoID: "clone-a", Epoch: 1, Generation: 3, Complete: true, Source: SourceLive})
	if len(a.Read().Items) != 1 || !a.Read().Items[0].Deleted || !hasFinding(a.Read().Findings, FindingDeleted) {
		t.Fatal("deleted marker/finding missing")
	}
	_ = a.Apply(Snapshot{GardenID: id, LocalRepoID: "clone-a", Epoch: 1, Generation: 4, Complete: true, Source: SourceLive})
	if len(a.Read().Items) != 0 {
		t.Fatal("deleted marker lasted more than one cycle")
	}
	_ = a.Apply(Snapshot{GardenID: id, LocalRepoID: "clone-a", Epoch: 1, Generation: 5, Complete: false, Problems: []Problem{{Code: "permission-denied"}}})
	if !a.Read().AccessRevoked || !hasFinding(a.Read().Findings, FindingAccessRevoked) {
		t.Fatal("access revocation not retained")
	}
	_ = a.Apply(Snapshot{GardenID: id, LocalRepoID: "clone-a", Epoch: 1, Generation: 6, Complete: false, Problems: []Problem{{Code: "offline"}}})
	if a.Read().BackoffHint == 0 {
		t.Fatal("offline backoff hint missing")
	}
	_ = a.Apply(Snapshot{GardenID: id, LocalRepoID: "clone-a", Epoch: 1, Generation: 10, Complete: true, Source: SourceLive, Items: []Item{{Issue: garden.Issue{ID: 10}}}})
	_ = a.Apply(Snapshot{GardenID: id, LocalRepoID: "clone-a", Epoch: 1, Generation: 9, Complete: true, Source: SourceLive, Items: []Item{{Issue: garden.Issue{ID: 9}}}})
	if a.Read().Generation != 10 || a.Read().Items[0].Issue.ID != 10 {
		t.Fatal("older late success replaced newer success")
	}
	if !hasFinding([]Finding{{Code: FindingOffline}}, FindingOffline) {
		t.Fatal("offline finding enum invalid")
	}
	epoch := a.Epoch()
	a.Reassociate(id)
	if a.Epoch() != epoch+1 {
		t.Fatal("epoch did not advance")
	}
	before := a.Read().Generation
	_ = a.Apply(Snapshot{GardenID: id, LocalRepoID: "clone-a", Epoch: epoch, Generation: 100, Complete: true, Source: SourceLive})
	if a.Read().Generation != before {
		t.Fatal("in-flight old epoch landed")
	}
}

func TestEmptyCompleteClearsAndConcurrentAccess(t *testing.T) {
	id := garden.GardenID{Repo: 7}
	s := NewStore(id, "repo")
	_ = s.Apply(Snapshot{GardenID: id, LocalRepoID: "repo", Epoch: 1, Generation: 1, Complete: true, Source: SourceLive, Items: []Item{{Issue: garden.Issue{ID: 1}}}})
	_ = s.Apply(Snapshot{GardenID: id, LocalRepoID: "repo", Epoch: 1, Generation: 2, Complete: true, Source: SourceLive})
	if len(s.Read().Items) != 1 || !s.Read().Items[0].Deleted {
		t.Fatal("first empty read must mark deletion")
	}
	_ = s.Apply(Snapshot{GardenID: id, LocalRepoID: "repo", Epoch: 1, Generation: 3, Complete: true, Source: SourceLive})
	if len(s.Read().Items) != 0 {
		t.Fatal("second empty read must clear")
	}
	var wg sync.WaitGroup
	for n := 1; n <= 300; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = s.Apply(Snapshot{GardenID: id, LocalRepoID: "repo", Epoch: 1, Generation: uint64(n + 3), Complete: true, Source: SourceLive})
			_ = s.Read()
		}(n)
	}
	wg.Wait()
}

func TestConnectionStatusFullMatrix(t *testing.T) {
	assocs := []AssociationState{AssociationNone, AssociationSkipped, AssociationReadOnly, AssociationConnected, AssociationPartialSetup, AssociationAwaitingManual, AssociationOperationInProgress}
	snaps := []struct {
		name string
		s    Snapshot
	}{{"none", Snapshot{}}, {"fresh-complete", Snapshot{Complete: true, Source: SourceLive}}, {"stale", Snapshot{Source: SourceStale}}, {"incomplete", Snapshot{Complete: false, Source: SourceLive}}, {"access-revoked", Snapshot{Source: SourceStale, AccessRevoked: true}}, {"offline", Snapshot{Source: SourceStale, BackoffHint: time.Second}}}
	for _, a := range assocs {
		for _, x := range snaps {
			t.Run(string(a)+"/"+x.name, func(t *testing.T) {
				got := Status(AssociationView{State: a}, x.s)
				want := string(a)
				if want == "" {
					want = "none"
				}
				if x.s.AccessRevoked {
					want = "access-revoked"
				} else if x.s.BackoffHint > 0 {
					want = "offline"
				} else if a == AssociationConnected && x.name == "stale" {
					want = "stale"
				} else if a == AssociationConnected && (x.name == "incomplete" || x.name == "none") {
					want = "partial"
				}
				if got.State != want {
					t.Fatalf("state=%q want=%q", got.State, want)
				}
			})
		}
	}
}

func TestPackageHasNoWriterOrConnectDependency(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.AllErrors)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			if strings.Contains(imp.Path.Value, "garden/connect") {
				t.Fatalf("read model imports connect in %s", name)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && (id.Name == "IssueWriter" || id.Name == "ProjectSetupWriter") {
				t.Errorf("writer capability referenced in %s", name)
			}
			return true
		})
	}
	if _, err := os.Stat("sync.go"); err != nil {
		t.Fatal(err)
	}
}

func TestLoadOfflineFindingAndMalformedItem(t *testing.T) {
	r := &testReader{pageErr: []error{garden.ErrOffline}}
	s := Load(context.Background(), request(r))
	if s.Complete || !hasFinding(s.Findings, FindingOffline) {
		t.Fatalf("offline finding missing: %+v", s)
	}
	r = &testReader{pages: []garden.IssuePage{{Items: []garden.Issue{{Repo: 7, ID: 8, Labels: []string{garden.ManagedLabel}, Metadata: []byte("{bad")}}, Complete: true}}}
	s = Load(context.Background(), request(r))
	if len(s.Items) != 1 || s.Items[0].Lane.Lane != garden.LaneAttention {
		t.Fatalf("bad item poisoned/vanished: %+v", s)
	}
}

func hasFinding(fs []Finding, code FindingCode) bool {
	for _, f := range fs {
		if f.Code == code {
			return true
		}
	}
	return false
}

func TestInvalidLimitsRejectedBeforeAnyPortCall(t *testing.T) {
	for _, l := range []Limits{{0, 1, time.Second}, {1, -1, time.Second}, {1, 1, 0}} {
		r := &testReader{}
		req := request(r)
		req.Limits = l
		if err := req.ValidateLimits(); !errors.Is(err, ErrInvalidLimits) {
			t.Fatalf("limits %+v: err=%v", l, err)
		}
		s := Load(context.Background(), req)
		if r.call != 0 || s.Complete || !hasProblem(s.Problems, "invalid-limits") {
			t.Fatalf("limits %+v: calls=%d snapshot=%+v", l, r.call, s)
		}
	}
}

func TestIssueWithoutStableIDStaysVisibleAndSnapshotComplete(t *testing.T) {
	r := &testReader{pages: []garden.IssuePage{{Items: []garden.Issue{validIssue(0), validIssue(5)}, Complete: true}}}
	s := Load(context.Background(), request(r))
	if !s.Complete || s.Source != SourceLive || len(s.Items) != 2 {
		t.Fatalf("one bad item must not make the snapshot partial: %+v", s)
	}
	bad, good := s.Items[0], s.Items[1]
	if bad.Issue.ID != 0 || bad.Lane.Lane != garden.LaneAttention || bad.Lane.Reason != "issue without stable ID" || !hasProblem(bad.Problems, "invalid-id") {
		t.Fatalf("zero-ID item not isolated in attention: %+v", bad)
	}
	if len(good.Problems) != 0 || !good.Verification.Complete {
		t.Fatalf("valid item affected by bad neighbour: %+v", good)
	}
}

func TestIssueDisplayEditsChangeRevision(t *testing.T) {
	issue := validIssue(10)
	issue.Number = 161
	issue.Title = "Before"
	initial := issueRevision(issue)
	issue.Title = "After 界 é"
	if issueRevision(issue) == initial {
		t.Fatal("title edit did not change revision")
	}
	titleRev := issueRevision(issue)
	issue.Number = 162
	if issueRevision(issue) == titleRev {
		t.Fatal("number edit did not change revision")
	}
}

func TestPlanDecisionsDoNotAliasOrAcceptLateSnapshots(t *testing.T) {
	id := garden.GardenID{Repo: 7}
	store := NewStore(id, "clone-a")
	input := Snapshot{GardenID: id, LocalRepoID: "clone-a", Epoch: 1, Generation: 1, Complete: true, Source: SourceLive, Items: []Item{{Issue: garden.Issue{ID: 1}, Verification: garden.Verification{PlanDecisions: []garden.PlanDecision{{Decision: garden.DecisionApproved, RevisionHash: "exact-plan"}}}}}}
	if err := store.Apply(input); err != nil {
		t.Fatal(err)
	}
	input.Items[0].Verification.PlanDecisions[0].Decision = garden.DecisionRejected
	if got := store.Read().Items[0].Verification.PlanDecisions[0].Decision; got != garden.DecisionApproved {
		t.Fatal("input aliases stored decision")
	}
	read := store.Read()
	read.Items[0].Verification.PlanDecisions[0].Decision = garden.DecisionChangesRequested
	if got := store.Read().Items[0].Verification.PlanDecisions[0].Decision; got != garden.DecisionApproved {
		t.Fatal("read aliases stored decision")
	}
	newer := input
	newer.Generation = 3
	if err := store.Apply(newer); err != nil {
		t.Fatal(err)
	}
	read.Generation = 2
	_ = store.Apply(read)
	if got := store.Read().Items[0].Verification.PlanDecisions[0].Decision; got != garden.DecisionRejected {
		t.Fatal("late approval overwrote newer decision")
	}
}
