package sync

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
)

// scriptReader is a configurable IssueReader for review-driven regression tests.
type scriptReader struct {
	pages        []garden.IssuePage
	get          map[garden.IssueID]garden.Issue
	getErr       map[garden.IssueID]error
	commentsErr  error
	onComments   func()
	listCalls    int
	commentCalls int
}

func (r *scriptReader) ListManagedIssues(context.Context, garden.RepoID, string) (garden.IssuePage, error) {
	n := r.listCalls
	r.listCalls++
	if n >= len(r.pages) {
		return garden.IssuePage{Complete: true}, nil
	}
	return r.pages[n], nil
}
func (r *scriptReader) GetIssue(_ context.Context, _ garden.RepoID, id garden.IssueID) (garden.Issue, error) {
	if err := r.getErr[id]; err != nil {
		return garden.Issue{}, err
	}
	return r.get[id], nil
}
func (r *scriptReader) ListComments(context.Context, garden.RepoID, garden.IssueID) ([]garden.IssueComment, error) {
	r.commentCalls++
	if r.onComments != nil {
		r.onComments()
	}
	return nil, r.commentsErr
}

func ids(items []Item) []garden.IssueID {
	out := make([]garden.IssueID, 0, len(items))
	for _, i := range items {
		out = append(out, i.Issue.ID)
	}
	return out
}

func TestItemCapTruncatesPageWithoutCursor(t *testing.T) {
	r := &scriptReader{pages: []garden.IssuePage{{Items: []garden.Issue{validIssue(1), validIssue(2), validIssue(3)}, Complete: true}}}
	q := request(r)
	q.Limits.ItemCap = 2
	s := Load(context.Background(), q)
	if s.Complete || s.Source != SourceStale || !hasProblem(s.Problems, "item-cap") || len(s.Items) != 2 {
		t.Fatalf("truncated page must be incomplete: complete=%v problems=%+v items=%v", s.Complete, s.Problems, ids(s.Items))
	}
	// An incomplete result must never mark the omitted issue as deleted.
	st := NewStore(q.GardenID, q.LocalRepoID)
	full := Snapshot{GardenID: q.GardenID, LocalRepoID: q.LocalRepoID, Epoch: 1, Generation: 1, Complete: true, Source: SourceLive, Items: []Item{{Issue: validIssue(1)}, {Issue: validIssue(2)}, {Issue: validIssue(3)}}}
	if err := st.Apply(full); err != nil {
		t.Fatal(err)
	}
	s.Generation = 2
	if err := st.Apply(s); err != nil {
		t.Fatal(err)
	}
	got := st.Read()
	if len(got.Items) != 3 {
		t.Fatalf("items lost: %v", ids(got.Items))
	}
	for _, it := range got.Items {
		if it.Deleted {
			t.Fatalf("issue %d wrongly marked deleted", it.Issue.ID)
		}
	}
}

func TestExplicitImportsAtCapAreIncompleteAndSorted(t *testing.T) {
	get := map[garden.IssueID]garden.Issue{}
	imports := map[garden.IssueID]struct{}{}
	for _, id := range []garden.IssueID{9, 3, 5, 7, 1} {
		get[id] = validIssue(id)
		imports[id] = struct{}{}
	}
	for run := 0; run < 20; run++ {
		r := &scriptReader{pages: []garden.IssuePage{{Items: []garden.Issue{validIssue(100)}, Complete: true}}, get: get}
		q := request(r)
		q.Limits.ItemCap = 3
		q.ExplicitImports = imports
		s := Load(context.Background(), q)
		if s.Complete || !hasProblem(s.Problems, "item-cap") {
			t.Fatalf("run %d: imports skipped at cap must be incomplete: %+v", run, s.Problems)
		}
		want := []garden.IssueID{1, 3, 100}
		if got := ids(s.Items); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("run %d: items %v want %v", run, got, want)
		}
	}
}

func TestForeignRepoAndIdentityMismatchAreRejected(t *testing.T) {
	foreign := validIssue(2)
	foreign.Repo = 99
	r := &scriptReader{
		pages: []garden.IssuePage{{Items: []garden.Issue{validIssue(1), foreign}, Complete: true}},
		get: map[garden.IssueID]garden.Issue{
			10: validIssue(11),                                                       // returned ID differs from requested
			20: func() garden.Issue { i := validIssue(20); i.Repo = 99; return i }(), // other repository
			30: validIssue(1),                                                        // already listed under another requested ID
		},
	}
	q := request(r)
	q.ExplicitImports = map[garden.IssueID]struct{}{10: {}, 20: {}, 30: {}}
	s := Load(context.Background(), q)
	if s.Complete || s.Source != SourceStale {
		t.Fatalf("rejected items must make the read incomplete: %+v", s)
	}
	if got := ids(s.Items); fmt.Sprint(got) != "[1]" {
		t.Fatalf("only the valid issue may enter the snapshot, got %v", got)
	}
	for _, code := range []string{"foreign-repo", "identity-mismatch"} {
		if !hasProblem(s.Problems, code) {
			t.Fatalf("missing %s problem: %+v", code, s.Problems)
		}
	}
	var perItem int
	for _, p := range s.Problems {
		if p.Code == "foreign-repo" && p.IssueID != 0 {
			perItem++
		}
	}
	if perItem != 2 {
		t.Fatalf("foreign-repo problems must name their issue, got %+v", s.Problems)
	}
}

func TestInconsistentCompleteStaleResultNeverReplacesOrDeletes(t *testing.T) {
	id := garden.GardenID{Repo: 7}
	st := NewStore(id, "clone-a")
	good := Snapshot{GardenID: id, LocalRepoID: "clone-a", Epoch: 1, Generation: 1, Complete: true, Source: SourceLive, Items: []Item{{Issue: garden.Issue{ID: 1}}, {Issue: garden.Issue{ID: 2}}}}
	if err := st.Apply(good); err != nil {
		t.Fatal(err)
	}
	bad := Snapshot{GardenID: id, LocalRepoID: "clone-a", Epoch: 1, Generation: 2, Complete: true, Source: SourceStale, Items: []Item{{Issue: garden.Issue{ID: 9}}}}
	if err := st.Apply(bad); err != nil {
		t.Fatal(err)
	}
	got := st.Read()
	if got.Complete || got.Source != SourceStale || fmt.Sprint(ids(got.Items)) != "[1 2]" {
		t.Fatalf("contradictory result replaced last-good: %+v", got)
	}
	for _, it := range got.Items {
		if it.Deleted {
			t.Fatalf("issue %d wrongly deleted", it.Issue.ID)
		}
	}
	if !hasProblem(got.Problems, "inconsistent-result") {
		t.Fatalf("inconsistency not visible: %+v", got.Problems)
	}
}

func TestStoreDeepCopiesVerification(t *testing.T) {
	id := garden.GardenID{Repo: 7}
	st := NewStore(id, "clone-a")
	mk := func() Snapshot {
		return Snapshot{GardenID: id, LocalRepoID: "clone-a", Epoch: 1, Generation: 1, Complete: true, Source: SourceLive, Items: []Item{{
			Issue:          garden.Issue{ID: 1, Labels: []string{"a"}, Metadata: []byte("m")},
			Classification: garden.Classification{Problems: []garden.LabelProblem{"p"}},
			Problems:       []Problem{{Code: "x"}},
			Verification: garden.Verification{
				Revisions:           []garden.Revision{{CommentID: 1, Parents: []string{"h"}}},
				Heads:               []garden.Revision{{CommentID: 2, Parents: []string{"h"}}},
				ConflictingHeads:    []garden.Revision{{CommentID: 3}},
				Approvals:           []garden.Approval{{CommentID: 4}},
				ValidApprovals:      []garden.Approval{{CommentID: 5}},
				RevokedApprovals:    []garden.Approval{{CommentID: 6}},
				SupersededApprovals: []garden.Approval{{CommentID: 7}},
				Problems:            []garden.VerificationProblem{{CommentID: 8, Code: "c"}},
			},
		}}, Problems: []Problem{{Code: "p"}}, Findings: []Finding{{Code: FindingOffline}}}
	}
	in := mk()
	if err := st.Apply(in); err != nil {
		t.Fatal(err)
	}
	scribble := func(it *Item) {
		it.Issue.Labels[0] = "X"
		it.Issue.Metadata[0] = 'X'
		it.Classification.Problems[0] = "X"
		it.Problems[0].Code = "X"
		v := &it.Verification
		v.Revisions[0].CommentID, v.Revisions[0].Parents[0] = 99, "X"
		v.Heads[0].CommentID, v.Heads[0].Parents[0] = 99, "X"
		v.ConflictingHeads[0].CommentID = 99
		v.Approvals[0].CommentID = 99
		v.ValidApprovals[0].CommentID = 99
		v.RevokedApprovals[0].CommentID = 99
		v.SupersededApprovals[0].CommentID = 99
		v.Problems[0].CommentID = 99
	}
	scribble(&in.Items[0]) // caller mutates its input after Apply
	out := st.Read()
	scribble(&out.Items[0]) // caller mutates the returned copy
	again := st.Read()
	want := mk().Items[0]
	if fmt.Sprintf("%+v", again.Items[0]) != fmt.Sprintf("%+v", want) {
		t.Fatalf("stored item aliased caller memory:\n got %+v\nwant %+v", again.Items[0], want)
	}
}

func TestStoreRejectsForeignLocalAndReassociateResetsGeneration(t *testing.T) {
	id := garden.GardenID{Repo: 7}
	st := NewStore(id, "clone-a")
	if err := st.Apply(Snapshot{GardenID: id, LocalRepoID: "clone-b", Epoch: 1, Generation: 1, Complete: true, Source: SourceLive}); !errors.Is(err, ErrForeignLocal) {
		t.Fatalf("foreign local accepted: %v", err)
	}
	if st.Read().Generation != 0 {
		t.Fatal("foreign local result changed state")
	}
	if err := st.Apply(Snapshot{GardenID: id, LocalRepoID: "clone-a", Epoch: 1, Generation: 5, Complete: true, Source: SourceLive, Items: []Item{{Issue: garden.Issue{ID: 1}}}}); err != nil {
		t.Fatal(err)
	}
	st.Reassociate(id)
	if err := st.Apply(Snapshot{GardenID: id, LocalRepoID: "clone-a", Epoch: 1, Generation: 6, Complete: true, Source: SourceLive}); err != nil || st.Dropped() != 1 {
		t.Fatalf("prior-epoch result must be dropped: err=%v dropped=%d", err, st.Dropped())
	}
	if err := st.Apply(Snapshot{GardenID: id, LocalRepoID: "clone-a", Epoch: 2, Generation: 1, Complete: true, Source: SourceLive, Items: []Item{{Issue: garden.Issue{ID: 2}}}}); err != nil {
		t.Fatal(err)
	}
	if got := st.Read(); got.Generation != 1 || got.Epoch != 2 {
		t.Fatalf("callers must restart at generation 1 after Reassociate: %+v", got)
	}
}

func TestSafeCodeDistinguishesCauses(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("x: %w", garden.ErrAuthRequired), "auth-required"},
		{fmt.Errorf("x: %w", garden.ErrNotFound), "not-found"},
		{fmt.Errorf("x: %w", garden.RateLimitError{RetryAfter: time.Second}), "rate-limited"},
		{fmt.Errorf("x: %w", garden.ErrRateLimited), "rate-limited"},
		{fmt.Errorf("x: %w", garden.ErrPermissionDenied), "permission-denied"},
		{fmt.Errorf("x: %w", garden.ErrForbidden), "forbidden"},
		{fmt.Errorf("x: %w", garden.ErrOffline), "offline"},
		{errors.New("boom"), "read-failed"},
	} {
		if got := safeCode(tc.err); got != tc.want {
			t.Errorf("safeCode(%v)=%q want %q", tc.err, got, tc.want)
		}
	}
}

func applyFailure(t *testing.T, err error) (Snapshot, ConnectionStatus) {
	t.Helper()
	r := &scriptReader{}
	q := request(r)
	q.Issues = failingLister{err}
	s := Load(context.Background(), q)
	st := NewStore(q.GardenID, q.LocalRepoID)
	if e := st.Apply(s); e != nil {
		t.Fatal(e)
	}
	snap := st.Read()
	return snap, Status(AssociationView{State: AssociationConnected}, snap)
}

type failingLister struct{ err error }

func (f failingLister) ListManagedIssues(context.Context, garden.RepoID, string) (garden.IssuePage, error) {
	return garden.IssuePage{}, f.err
}
func (failingLister) GetIssue(context.Context, garden.RepoID, garden.IssueID) (garden.Issue, error) {
	return garden.Issue{}, garden.ErrNotFound
}
func (failingLister) ListComments(context.Context, garden.RepoID, garden.IssueID) ([]garden.IssueComment, error) {
	return nil, nil
}

func TestStatusOfflineOnlyForNetworkFailures(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		state string
	}{
		{"network", fmt.Errorf("x: %w", garden.ErrOffline), "offline"},
		{"other", errors.New("boom"), "stale"},
		{"forbidden", fmt.Errorf("x: %w", garden.ErrForbidden), "stale"},
		{"not-found", fmt.Errorf("x: %w", garden.ErrNotFound), "stale"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snap, st := applyFailure(t, tc.err)
			if st.State != tc.state || snap.AccessRevoked {
				t.Fatalf("state=%q revoked=%v want %q", st.State, snap.AccessRevoked, tc.state)
			}
		})
	}
}

func TestForbiddenIsDegradedNeverAccessRevoked(t *testing.T) {
	snap, _ := applyFailure(t, fmt.Errorf("x: %w", garden.ErrForbidden))
	if snap.AccessRevoked || hasFinding(snap.Findings, FindingAccessRevoked) || !hasFinding(snap.Findings, FindingDegraded) {
		t.Fatalf("bare 403 must be a degraded finding only: %+v", snap)
	}
}

func TestAuthRequiredRevokesAccessRetainingData(t *testing.T) {
	snap, st := applyFailure(t, fmt.Errorf("x: %w", garden.ErrAuthRequired))
	if !snap.AccessRevoked || st.State != "access-revoked" || !hasFinding(snap.Findings, FindingAccessRevoked) {
		t.Fatalf("401 must report access revoked: %+v", snap)
	}
}

func TestRateLimitHintIsBoundedAndSurfaced(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   time.Duration
		want time.Duration
	}{{"given", 90 * time.Second, 90 * time.Second}, {"missing", 0, defaultRetryAfter}, {"huge", 100 * time.Hour, maxRetryAfterHint}, {"negative", -time.Second, defaultRetryAfter}} {
		t.Run(tc.name, func(t *testing.T) {
			snap, st := applyFailure(t, fmt.Errorf("x: %w", garden.RateLimitError{RetryAfter: tc.in}))
			if st.State != "rate-limited" || st.RetryAfter != tc.want || snap.RetryAfter != tc.want {
				t.Fatalf("state=%q status=%v snap=%v want %v", st.State, st.RetryAfter, snap.RetryAfter, tc.want)
			}
			if snap.BackoffHint != 0 || snap.AccessRevoked {
				t.Fatalf("rate limit must not look offline/revoked: %+v", snap)
			}
			var found bool
			for _, f := range snap.Findings {
				if f.Code == FindingRateLimited && f.RetryAfter == tc.want {
					found = true
				}
			}
			if !found {
				t.Fatalf("rate-limit finding missing: %+v", snap.Findings)
			}
		})
	}
}

func TestCancelStopsCommentReads(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &scriptReader{pages: []garden.IssuePage{{Items: []garden.Issue{validIssue(1), validIssue(2), validIssue(3)}, Complete: true}}, onComments: cancel}
	s := Load(ctx, request(r))
	if r.commentCalls != 1 {
		t.Fatalf("comment reads continued after cancel: %d", r.commentCalls)
	}
	if s.Complete || s.Source != SourceStale || !hasProblem(s.Problems, "cancelled") || len(s.Items) != 3 {
		t.Fatalf("cancelled read must be incomplete: %+v", s)
	}
}

func TestAdapterIssueProblemsStayVisibleWithoutBreakingTheSnapshot(t *testing.T) {
	bad := validIssue(2)
	bad.Problems = []string{garden.IssueProblemAuthor, "<script>"}
	r := &scriptReader{pages: []garden.IssuePage{{Items: []garden.Issue{validIssue(1), bad}, Complete: true}}}
	s := Load(context.Background(), request(r))
	if !s.Complete || len(s.Items) != 2 {
		t.Fatalf("complete=%v items=%d problems=%+v", s.Complete, len(s.Items), s.Problems)
	}
	var item Item
	for _, it := range s.Items {
		if it.Issue.ID == 2 {
			item = it
		}
	}
	if item.Lane.Lane != garden.LaneAttention || !hasProblem(item.Problems, "invalid-author") || !hasProblem(item.Problems, "invalid-issue") {
		t.Fatalf("item=%+v", item)
	}
	for _, p := range item.Problems {
		if p.Code == "<script>" {
			t.Fatal("adapter-supplied text reached a problem code")
		}
	}
	st := NewStore(garden.GardenID{Repo: 7}, "clone-a")
	if err := st.Apply(s); err != nil {
		t.Fatal(err)
	}
	got := st.Read()
	got.Items[0].Issue.Problems = append(got.Items[0].Issue.Problems, "x")
	for i := range got.Items {
		if len(got.Items[i].Issue.Problems) > 0 {
			got.Items[i].Issue.Problems[0] = "mutated"
		}
	}
	for _, it := range st.Read().Items {
		for _, p := range it.Issue.Problems {
			if p == "mutated" {
				t.Fatal("Issue.Problems aliased")
			}
		}
	}
}
