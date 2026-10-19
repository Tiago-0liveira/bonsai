package sync_test

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	gardensync "github.com/Tiago-0liveira/bonsai/internal/core/garden/sync"
)

// -----------------------------------------------------------------------------
// Helper constructors and test doubles
// -----------------------------------------------------------------------------

type fakeReader struct {
	mu          sync.Mutex
	pages       []garden.IssuePage
	pageErrs    []error
	callCount   int
	getIssueMap map[garden.IssueID]garden.Issue
	getIssueErr map[garden.IssueID]error
	commentsMap map[garden.IssueID][]garden.IssueComment
	commentsErr map[garden.IssueID]error
	onList      func(ctx context.Context, call int, cursor string) (garden.IssuePage, error)
}

func (f *fakeReader) ListManagedIssues(ctx context.Context, _ garden.RepoID, cursor string) (garden.IssuePage, error) {
	f.mu.Lock()
	n := f.callCount
	f.callCount++
	onList := f.onList
	var p garden.IssuePage
	var err error
	if n < len(f.pages) {
		p = f.pages[n]
	} else if len(f.pages) > 0 {
		p = garden.IssuePage{Complete: true}
	}
	if n < len(f.pageErrs) {
		err = f.pageErrs[n]
	}
	f.mu.Unlock()

	if onList != nil {
		return onList(ctx, n, cursor)
	}
	return p, err
}

func (f *fakeReader) GetIssue(_ context.Context, _ garden.RepoID, id garden.IssueID) (garden.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.getIssueErr[id]; ok {
		return garden.Issue{}, err
	}
	if it, ok := f.getIssueMap[id]; ok {
		return it, nil
	}
	return garden.Issue{}, garden.ErrNotFound
}

func (f *fakeReader) ListComments(_ context.Context, _ garden.RepoID, id garden.IssueID) ([]garden.IssueComment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.commentsErr[id]; ok {
		return nil, err
	}
	return f.commentsMap[id], nil
}

type fakeProjectReader struct {
	projects []garden.Project
	err      error
}

func (p fakeProjectReader) ListProjects(context.Context, garden.RepoID) ([]garden.Project, error) {
	return p.projects, p.err
}

func testIssue(repo garden.RepoID, id garden.IssueID, stage garden.Stage, labels ...string) garden.Issue {
	allLabels := []string{
		garden.ManagedLabel,
		garden.FormatTypeLabel(garden.TypeFeature),
		garden.FormatStageLabel(stage),
	}
	allLabels = append(allLabels, labels...)
	meta := []byte(fmt.Sprintf(`{"schema_version":1,"provenance":"adversarial","owner":"alice","accepted":true,"accepted_spec_hash":"%s","spec_fingerprint":"%s"}`,
		garden.ContentHash("spec"), garden.ContentHash("spec")))
	return garden.Issue{
		Repo:      repo,
		ID:        id,
		NodeID:    fmt.Sprintf("NODE_%d", id),
		AuthorID:  "100",
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Labels:    allLabels,
		Body:      fmt.Sprintf("<!-- bonsai:garden-metadata/v1 %s -->\nspec", meta),
		Metadata:  meta,
	}
}

func testRequest(reader garden.IssueReader) gardensync.Request {
	return gardensync.Request{
		GardenID:    garden.GardenID{Repo: 42},
		LocalRepoID: "clone-alpha",
		Generation:  1,
		Epoch:       1,
		Issues:      reader,
		Limits: gardensync.Limits{
			PageCap:     10,
			ItemCap:     100,
			CallTimeout: time.Second,
		},
	}
}

func hasProblemCode(problems []gardensync.Problem, code string) bool {
	for _, p := range problems {
		if p.Code == code {
			return true
		}
	}
	return false
}

func hasFindingCode(findings []gardensync.Finding, code gardensync.FindingCode) bool {
	for _, f := range findings {
		if f.Code == code {
			return true
		}
	}
	return false
}

// -----------------------------------------------------------------------------
// Scenario 1: Ordering
// -----------------------------------------------------------------------------

func TestOrderingAdversarial(t *testing.T) {
	t.Parallel()
	gardenID := garden.GardenID{Repo: 42}
	localID := garden.LocalRepoID("clone-ordering")

	t.Run("GenerationsDelivered1_3_2", func(t *testing.T) {
		t.Parallel()
		store := gardensync.NewStore(gardenID, localID)

		// Generation 1: accepted
		snap1 := gardensync.Snapshot{GardenID: gardenID, LocalRepoID: localID, Generation: 1, Epoch: 1, Complete: true, Source: gardensync.SourceLive, Items: []gardensync.Item{{Issue: garden.Issue{ID: 1}}}}
		if err := store.Apply(snap1); err != nil {
			t.Fatalf("Apply(1): %v", err)
		}
		if store.Read().Generation != 1 || store.Dropped() != 0 {
			t.Fatalf("expected generation 1, dropped 0; got gen %d, dropped %d", store.Read().Generation, store.Dropped())
		}

		// Generation 3: accepted
		snap3 := gardensync.Snapshot{GardenID: gardenID, LocalRepoID: localID, Generation: 3, Epoch: 1, Complete: true, Source: gardensync.SourceLive, Items: []gardensync.Item{{Issue: garden.Issue{ID: 3}}}}
		if err := store.Apply(snap3); err != nil {
			t.Fatalf("Apply(3): %v", err)
		}
		if store.Read().Generation != 3 || store.Dropped() != 0 {
			t.Fatalf("expected generation 3, dropped 0; got gen %d, dropped %d", store.Read().Generation, store.Dropped())
		}

		// Generation 2: late older generation must be dropped and counted
		snap2 := gardensync.Snapshot{GardenID: gardenID, LocalRepoID: localID, Generation: 2, Epoch: 1, Complete: true, Source: gardensync.SourceLive, Items: []gardensync.Item{{Issue: garden.Issue{ID: 2}}}}
		if err := store.Apply(snap2); err != nil {
			t.Fatalf("Apply(2): %v", err)
		}
		if store.Read().Generation != 3 || store.Dropped() != 1 {
			t.Fatalf("expected generation 3, dropped 1; got gen %d, dropped %d", store.Read().Generation, store.Dropped())
		}
	})

	t.Run("GenerationsDelivered3_2_1", func(t *testing.T) {
		t.Parallel()
		store := gardensync.NewStore(gardenID, localID)

		snap3 := gardensync.Snapshot{GardenID: gardenID, LocalRepoID: localID, Generation: 3, Epoch: 1, Complete: true, Source: gardensync.SourceLive, Items: []gardensync.Item{{Issue: garden.Issue{ID: 3}}}}
		_ = store.Apply(snap3)

		snap2 := gardensync.Snapshot{GardenID: gardenID, LocalRepoID: localID, Generation: 2, Epoch: 1, Complete: true, Source: gardensync.SourceLive, Items: []gardensync.Item{{Issue: garden.Issue{ID: 2}}}}
		_ = store.Apply(snap2)

		snap1 := gardensync.Snapshot{GardenID: gardenID, LocalRepoID: localID, Generation: 1, Epoch: 1, Complete: true, Source: gardensync.SourceLive, Items: []gardensync.Item{{Issue: garden.Issue{ID: 1}}}}
		_ = store.Apply(snap1)

		if store.Read().Generation != 3 || store.Dropped() != 2 {
			t.Fatalf("expected generation 3, dropped 2; got gen %d, dropped %d", store.Read().Generation, store.Dropped())
		}
	})

	t.Run("DuplicateGenerations", func(t *testing.T) {
		t.Parallel()
		store := gardensync.NewStore(gardenID, localID)

		snap := gardensync.Snapshot{GardenID: gardenID, LocalRepoID: localID, Generation: 5, Epoch: 1, Complete: true, Source: gardensync.SourceLive, Items: []gardensync.Item{{Issue: garden.Issue{ID: 5}}}}
		_ = store.Apply(snap)
		_ = store.Apply(snap)

		if store.Read().Generation != 5 || store.Dropped() != 1 {
			t.Fatalf("duplicate generation not counted as dropped: gen %d, dropped %d", store.Read().Generation, store.Dropped())
		}
	})

	t.Run("GenerationZero", func(t *testing.T) {
		t.Parallel()
		store := gardensync.NewStore(gardenID, localID)

		// Initial unaccepted store generation is 0. A generation 0 result is <= 0, so it must be dropped.
		snap0 := gardensync.Snapshot{GardenID: gardenID, LocalRepoID: localID, Generation: 0, Epoch: 1, Complete: true, Source: gardensync.SourceLive}
		_ = store.Apply(snap0)

		if store.Dropped() != 1 || store.Read().Generation != 0 {
			t.Fatalf("generation 0 should be dropped: gen %d, dropped %d", store.Read().Generation, store.Dropped())
		}
	})

	t.Run("GenerationMaxInt64AndNegativeAsUint64", func(t *testing.T) {
		t.Parallel()
		store := gardensync.NewStore(gardenID, localID)

		// MaxInt64 accepted
		maxInt := uint64(math.MaxInt64)
		snapMax := gardensync.Snapshot{GardenID: gardenID, LocalRepoID: localID, Generation: maxInt, Epoch: 1, Complete: true, Source: gardensync.SourceLive, Items: []gardensync.Item{{Issue: garden.Issue{ID: 1}}}}
		_ = store.Apply(snapMax)
		if store.Read().Generation != maxInt {
			t.Fatalf("expected MaxInt64 (%d), got %d", maxInt, store.Read().Generation)
		}

		// Negative cast to uint64 is MaxUint64, which is strictly greater than MaxInt64, so it must be accepted
		var neg int64 = -1
		negUint := uint64(neg) // 18446744073709551615
		snapNeg := gardensync.Snapshot{GardenID: gardenID, LocalRepoID: localID, Generation: negUint, Epoch: 1, Complete: true, Source: gardensync.SourceLive, Items: []gardensync.Item{{Issue: garden.Issue{ID: 2}}}}
		_ = store.Apply(snapNeg)
		if store.Read().Generation != negUint {
			t.Fatalf("expected MaxUint64 (%d), got %d", negUint, store.Read().Generation)
		}

		// Subsequent lower generation dropped
		snapLower := gardensync.Snapshot{GardenID: gardenID, LocalRepoID: localID, Generation: 100, Epoch: 1, Complete: true, Source: gardensync.SourceLive}
		_ = store.Apply(snapLower)
		if store.Dropped() != 1 {
			t.Fatalf("expected dropped 1, got %d", store.Dropped())
		}
	})

	t.Run("LateReplyAfterReassociateDropped", func(t *testing.T) {
		t.Parallel()
		store := gardensync.NewStore(gardenID, localID)
		initialEpoch := store.Epoch()

		// Reassociate bumps epoch
		store.Reassociate(gardenID)
		if store.Epoch() != initialEpoch+1 {
			t.Fatalf("expected epoch %d, got %d", initialEpoch+1, store.Epoch())
		}

		// Late response from previous epoch
		snapOld := gardensync.Snapshot{GardenID: gardenID, LocalRepoID: localID, Generation: 999, Epoch: initialEpoch, Complete: true, Source: gardensync.SourceLive}
		_ = store.Apply(snapOld)

		if store.Dropped() != 1 || store.Read().Generation != 0 {
			t.Fatalf("late epoch result was not dropped: gen %d, dropped %d", store.Read().Generation, store.Dropped())
		}
	})

	t.Run("ForeignGardenRejectedWithStateUnchanged", func(t *testing.T) {
		t.Parallel()
		store := gardensync.NewStore(gardenID, localID)
		snapInitial := gardensync.Snapshot{GardenID: gardenID, LocalRepoID: localID, Generation: 1, Epoch: 1, Complete: true, Source: gardensync.SourceLive, Items: []gardensync.Item{{Issue: garden.Issue{ID: 10}}}}
		_ = store.Apply(snapInitial)

		before := store.Read()

		foreign := gardensync.Snapshot{GardenID: garden.GardenID{Repo: 9999}, LocalRepoID: localID, Generation: 2, Epoch: 1, Complete: true, Source: gardensync.SourceLive, Items: []gardensync.Item{{Issue: garden.Issue{ID: 99}}}}
		err := store.Apply(foreign)
		if !errors.Is(err, gardensync.ErrForeignGarden) {
			t.Fatalf("expected ErrForeignGarden, got %v", err)
		}

		after := store.Read()
		if before.Generation != after.Generation || len(before.Items) != len(after.Items) || before.Items[0].Issue.ID != after.Items[0].Issue.ID {
			t.Fatalf("store state mutated after ErrForeignGarden: before=%+v after=%+v", before, after)
		}
	})

	t.Run("ShuffledConcurrentDeliveryHighestWins", func(t *testing.T) {
		t.Parallel()
		store := gardensync.NewStore(gardenID, localID)
		const n = 50
		var wg sync.WaitGroup

		// Launch readers concurrently with writers
		stopReaders := make(chan struct{})
		var readerWg sync.WaitGroup
		for r := 0; r < 5; r++ {
			readerWg.Add(1)
			go func() {
				defer readerWg.Done()
				for {
					select {
					case <-stopReaders:
						return
					default:
						_ = store.Read()
					}
				}
			}()
		}

		// Deliver shuffled generations 1..N
		for i := 1; i <= n; i++ {
			wg.Add(1)
			gen := uint64(i)
			go func() {
				defer wg.Done()
				snap := gardensync.Snapshot{
					GardenID:    gardenID,
					LocalRepoID: localID,
					Generation:  gen,
					Epoch:       1,
					Complete:    true,
					Source:      gardensync.SourceLive,
					Items:       []gardensync.Item{{Issue: garden.Issue{ID: garden.IssueID(gen)}}},
				}
				_ = store.Apply(snap)
			}()
		}

		wg.Wait()
		close(stopReaders)
		readerWg.Wait()

		final := store.Read()
		if final.Generation != n {
			t.Fatalf("expected final generation %d, got %d", n, final.Generation)
		}
		if len(final.Items) == 0 || final.Items[len(final.Items)-1].Issue.ID != garden.IssueID(n) {
			t.Fatalf("expected item with ID %d to be present in final state, got %+v", n, final.Items)
		}
	})
}

// -----------------------------------------------------------------------------
// Scenario 2: Last-good retention and failure handling
// -----------------------------------------------------------------------------

func TestLastGoodAdversarial(t *testing.T) {
	t.Parallel()
	gardenID := garden.GardenID{Repo: 42}
	localID := garden.LocalRepoID("clone-lastgood")

	t.Run("CompleteEmptyClearsAfterDeletionCycle", func(t *testing.T) {
		t.Parallel()
		store := gardensync.NewStore(gardenID, localID)

		// Seed initial item
		_ = store.Apply(gardensync.Snapshot{GardenID: gardenID, LocalRepoID: localID, Generation: 1, Epoch: 1, Complete: true, Source: gardensync.SourceLive, Items: []gardensync.Item{{Issue: garden.Issue{ID: 1}}}})
		if len(store.Read().Items) != 1 {
			t.Fatal("seed item missing")
		}

		// First complete empty read marks item deleted (1-cycle retention)
		_ = store.Apply(gardensync.Snapshot{GardenID: gardenID, LocalRepoID: localID, Generation: 2, Epoch: 1, Complete: true, Source: gardensync.SourceLive, Items: nil})
		read1 := store.Read()
		if len(read1.Items) != 1 || !read1.Items[0].Deleted {
			t.Fatalf("expected 1 deleted item, got %+v", read1.Items)
		}

		// Second complete empty read completely clears the list
		_ = store.Apply(gardensync.Snapshot{GardenID: gardenID, LocalRepoID: localID, Generation: 3, Epoch: 1, Complete: true, Source: gardensync.SourceLive, Items: nil})
		read2 := store.Read()
		if len(read2.Items) != 0 {
			t.Fatalf("expected 0 items after second complete empty read, got %+v", read2.Items)
		}
	})

	t.Run("IncompleteEmptyDoesNotClear", func(t *testing.T) {
		t.Parallel()
		store := gardensync.NewStore(gardenID, localID)

		_ = store.Apply(gardensync.Snapshot{GardenID: gardenID, LocalRepoID: localID, Generation: 1, Epoch: 1, Complete: true, Source: gardensync.SourceLive, Items: []gardensync.Item{{Issue: garden.Issue{ID: 1}}}})

		// Incomplete empty read
		_ = store.Apply(gardensync.Snapshot{GardenID: gardenID, LocalRepoID: localID, Generation: 2, Epoch: 1, Complete: false, Source: gardensync.SourceStale, Items: nil})
		read := store.Read()
		if len(read.Items) != 1 || read.Items[0].Issue.ID != 1 || read.Items[0].Deleted {
			t.Fatalf("incomplete empty read modified last-good items: %+v", read.Items)
		}
		if read.Source != gardensync.SourceStale {
			t.Fatalf("expected SourceStale, got %v", read.Source)
		}
	})

	t.Run("StaleSinceDoesNotMoveOnRepeatedFailures", func(t *testing.T) {
		t.Parallel()
		store := gardensync.NewStore(gardenID, localID)

		t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
		_ = store.Apply(gardensync.Snapshot{GardenID: gardenID, LocalRepoID: localID, Generation: 1, Epoch: 1, FetchedAt: t0, Complete: true, Source: gardensync.SourceLive, Items: []gardensync.Item{{Issue: garden.Issue{ID: 1}}}})

		t1 := t0.Add(5 * time.Minute)
		_ = store.Apply(gardensync.Snapshot{GardenID: gardenID, LocalRepoID: localID, Generation: 2, Epoch: 1, FetchedAt: t1, Complete: false, Source: gardensync.SourceStale, Problems: []gardensync.Problem{{Code: "offline"}}})

		staleSinceFirst := store.Read().StaleSince
		if staleSinceFirst != t1 {
			t.Fatalf("expected StaleSince %v, got %v", t1, staleSinceFirst)
		}

		// Subsequent failure at t2 must NOT move StaleSince
		t2 := t1.Add(5 * time.Minute)
		_ = store.Apply(gardensync.Snapshot{GardenID: gardenID, LocalRepoID: localID, Generation: 3, Epoch: 1, FetchedAt: t2, Complete: false, Source: gardensync.SourceStale, Problems: []gardensync.Problem{{Code: "offline"}}})

		staleSinceSecond := store.Read().StaleSince
		if staleSinceSecond != staleSinceFirst {
			t.Fatalf("StaleSince moved on repeated failure: first=%v second=%v", staleSinceFirst, staleSinceSecond)
		}

		// Fresh recovery clears stale marker
		t3 := t2.Add(5 * time.Minute)
		_ = store.Apply(gardensync.Snapshot{GardenID: gardenID, LocalRepoID: localID, Generation: 4, Epoch: 1, FetchedAt: t3, Complete: true, Source: gardensync.SourceLive, Items: []gardensync.Item{{Issue: garden.Issue{ID: 1}}}})

		recovered := store.Read()
		if recovered.Source != gardensync.SourceLive {
			t.Fatalf("expected SourceLive after recovery, got %v", recovered.Source)
		}
		if !recovered.StaleSince.IsZero() {
			t.Fatalf("expected zero StaleSince after recovery, got %v", recovered.StaleSince)
		}
		if recovered.StaleReason != "" {
			t.Fatalf("expected empty StaleReason after recovery, got %q", recovered.StaleReason)
		}
	})

	t.Run("DistinctFindingsForFailureTypes", func(t *testing.T) {
		t.Parallel()

		// Offline
		storeOff := gardensync.NewStore(gardenID, localID)
		_ = storeOff.Apply(gardensync.Snapshot{GardenID: gardenID, LocalRepoID: localID, Generation: 1, Epoch: 1, Complete: false, Problems: []gardensync.Problem{{Code: "offline"}}})
		snapOff := storeOff.Read()
		if snapOff.BackoffHint == 0 {
			t.Error("offline did not set BackoffHint")
		}
		if snapOff.AccessRevoked {
			t.Error("offline incorrectly set AccessRevoked")
		}

		// Access Revoked
		storeRev := gardensync.NewStore(gardenID, localID)
		_ = storeRev.Apply(gardensync.Snapshot{GardenID: gardenID, LocalRepoID: localID, Generation: 1, Epoch: 1, Complete: false, Problems: []gardensync.Problem{{Code: "permission-denied"}}})
		snapRev := storeRev.Read()
		if !snapRev.AccessRevoked {
			t.Error("permission-denied did not set AccessRevoked")
		}
		if !hasFindingCode(snapRev.Findings, gardensync.FindingAccessRevoked) {
			t.Error("permission-denied missing FindingAccessRevoked")
		}
		if snapRev.StaleReason != "Garden access was revoked" {
			t.Errorf("unexpected StaleReason: %q", snapRev.StaleReason)
		}

		// Generic Error
		storeGen := gardensync.NewStore(gardenID, localID)
		_ = storeGen.Apply(gardensync.Snapshot{GardenID: gardenID, LocalRepoID: localID, Generation: 1, Epoch: 1, Complete: false, Problems: []gardensync.Problem{{Code: "read-failed"}}})
		snapGen := storeGen.Read()
		if snapGen.AccessRevoked {
			t.Error("generic error set AccessRevoked")
		}
		if hasFindingCode(snapGen.Findings, gardensync.FindingAccessRevoked) {
			t.Error("generic error produced FindingAccessRevoked")
		}
	})
}

// -----------------------------------------------------------------------------
// Scenario 3: Pagination
// -----------------------------------------------------------------------------

func TestPaginationAdversarial(t *testing.T) {
	t.Parallel()

	t.Run("PageSizes", func(t *testing.T) {
		t.Parallel()
		const totalIssues = 5
		for _, pageSize := range []int{1, 2, totalIssues, totalIssues + 1} {
			t.Run(fmt.Sprintf("PageSize_%d", pageSize), func(t *testing.T) {
				reader := &fakeReader{}
				for i := 0; i < totalIssues; i += pageSize {
					end := i + pageSize
					if end > totalIssues {
						end = totalIssues
					}
					var page garden.IssuePage
					for j := i; j < end; j++ {
						page.Items = append(page.Items, testIssue(42, garden.IssueID(j+1), garden.StageReady))
					}
					if end < totalIssues {
						page.NextCursor = fmt.Sprintf("cursor-%d", end)
					} else {
						page.Complete = true
					}
					reader.pages = append(reader.pages, page)
				}

				req := testRequest(reader)
				snap := gardensync.Load(context.Background(), req)
				if !snap.Complete || len(snap.Items) != totalIssues {
					t.Fatalf("pageSize %d: complete=%v items=%d", pageSize, snap.Complete, len(snap.Items))
				}
			})
		}
	})

	t.Run("ExactlyAtItemCap", func(t *testing.T) {
		t.Parallel()
		reader := &fakeReader{
			pages: []garden.IssuePage{
				{
					Items: []garden.Issue{
						testIssue(42, 1, garden.StageReady),
						testIssue(42, 2, garden.StageReady),
					},
					Complete: true,
				},
			},
		}
		req := testRequest(reader)
		req.Limits.ItemCap = 2
		snap := gardensync.Load(context.Background(), req)
		if !snap.Complete || len(snap.Items) != 2 || hasProblemCode(snap.Problems, "item-cap") {
			t.Fatalf("exactly at item cap should be complete: %+v", snap)
		}
	})

	t.Run("ItemCapPlusOne", func(t *testing.T) {
		t.Parallel()
		reader := &fakeReader{
			pages: []garden.IssuePage{
				{
					Items: []garden.Issue{
						testIssue(42, 1, garden.StageReady),
						testIssue(42, 2, garden.StageReady),
					},
					NextCursor: "cursor-2",
					Complete:   false,
				},
			},
		}
		req := testRequest(reader)
		req.Limits.ItemCap = 2 // Cap reached while more items exist
		snap := gardensync.Load(context.Background(), req)
		if snap.Complete || !hasProblemCode(snap.Problems, "item-cap") {
			t.Fatalf("expected item-cap problem and incomplete: %+v", snap)
		}
	})

	t.Run("PageCapReached", func(t *testing.T) {
		t.Parallel()
		reader := &fakeReader{
			pages: []garden.IssuePage{
				{Items: []garden.Issue{testIssue(42, 1, garden.StageReady)}, NextCursor: "cursor-1"},
				{Items: []garden.Issue{testIssue(42, 2, garden.StageReady)}, NextCursor: "cursor-2"},
			},
		}
		req := testRequest(reader)
		req.Limits.PageCap = 1
		snap := gardensync.Load(context.Background(), req)
		if snap.Complete || !hasProblemCode(snap.Problems, "page-cap") {
			t.Fatalf("expected page-cap problem and incomplete: %+v", snap)
		}
	})

	t.Run("ZeroOrNegativeLimits", func(t *testing.T) {
		t.Parallel()
		testCases := []struct {
			name   string
			limits gardensync.Limits
		}{
			{"ZeroPageCap", gardensync.Limits{PageCap: 0, ItemCap: 10, CallTimeout: time.Second}},
			{"NegativePageCap", gardensync.Limits{PageCap: -1, ItemCap: 10, CallTimeout: time.Second}},
			{"ZeroItemCap", gardensync.Limits{PageCap: 10, ItemCap: 0, CallTimeout: time.Second}},
			{"NegativeItemCap", gardensync.Limits{PageCap: 10, ItemCap: -5, CallTimeout: time.Second}},
			{"ZeroTimeout", gardensync.Limits{PageCap: 10, ItemCap: 10, CallTimeout: 0}},
			{"NegativeTimeout", gardensync.Limits{PageCap: 10, ItemCap: 10, CallTimeout: -time.Second}},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				req := testRequest(&fakeReader{})
				req.Limits = tc.limits
				snap := gardensync.Load(context.Background(), req)

				// CONTRACT: "Reads have caller-selected page, item, and per-call timeout bounds."
				// PLAN: "0/negative limits => ErrInvalidLimits"
				// Bug finding: sync.Load emits "invalid-request" instead of exposing or matching ErrInvalidLimits.
				found := false
				for _, p := range snap.Problems {
					if p.Code == "invalid-limits" || strings.Contains(p.Message, gardensync.ErrInvalidLimits.Error()) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("// CONTRACT: \"Reads have caller-selected page, item, and per-call timeout bounds.\" - expected ErrInvalidLimits in problems for %s, got: %+v", tc.name, snap.Problems)
				}
			})
		}
	})

	t.Run("InvalidCursorRestartOnceThenFailsVisibly", func(t *testing.T) {
		t.Parallel()
		reader := &fakeReader{
			pageErrs: []error{garden.ErrInvalidCursor, garden.ErrInvalidCursor},
		}
		req := testRequest(reader)
		snap := gardensync.Load(context.Background(), req)

		// Assert exactly 2 calls: initial call + 1 restart
		if reader.callCount != 2 {
			t.Fatalf("expected exactly 2 calls on invalid cursor restart, got %d", reader.callCount)
		}
		if snap.Complete || !hasProblemCode(snap.Problems, "read-failed") {
			t.Fatalf("second cursor error must fail visibly: %+v", snap)
		}
	})

	t.Run("CursorLoopTerminates", func(t *testing.T) {
		t.Parallel()
		// Adapter returns the exact same cursor forever
		reader := &fakeReader{
			onList: func(_ context.Context, _ int, _ string) (garden.IssuePage, error) {
				return garden.IssuePage{
					Items:      []garden.Issue{testIssue(42, 1, garden.StageReady)},
					NextCursor: "identical-cursor",
					Complete:   false,
				}, nil
			},
		}
		req := testRequest(reader)
		req.Limits.PageCap = 3
		snap := gardensync.Load(context.Background(), req)

		if snap.Complete || !hasProblemCode(snap.Problems, "page-cap") {
			t.Fatalf("cursor loop must terminate via page cap: %+v", snap)
		}
		if reader.callCount != 3 {
			t.Fatalf("expected 3 calls, got %d", reader.callCount)
		}
	})

	t.Run("EmptyPageWithNextCursorForeverTerminates", func(t *testing.T) {
		t.Parallel()
		reader := &fakeReader{
			onList: func(_ context.Context, call int, _ string) (garden.IssuePage, error) {
				return garden.IssuePage{
					Items:      nil,
					NextCursor: fmt.Sprintf("cursor-%d", call+1),
					Complete:   false,
				}, nil
			},
		}
		req := testRequest(reader)
		req.Limits.PageCap = 4
		snap := gardensync.Load(context.Background(), req)

		if snap.Complete || !hasProblemCode(snap.Problems, "page-cap") {
			t.Fatalf("empty page loop must terminate via page cap: %+v", snap)
		}
		if reader.callCount != 4 {
			t.Fatalf("expected 4 calls, got %d", reader.callCount)
		}
	})

	t.Run("DuplicateItemsAcrossPagesDeduped", func(t *testing.T) {
		t.Parallel()
		reader := &fakeReader{
			pages: []garden.IssuePage{
				{
					Items: []garden.Issue{
						testIssue(42, 1, garden.StageReady),
						testIssue(42, 2, garden.StageReady),
					},
					NextCursor: "cursor-1",
				},
				{
					Items: []garden.Issue{
						testIssue(42, 2, garden.StageReady), // duplicate issue 2
						testIssue(42, 3, garden.StageReady),
					},
					Complete: true,
				},
			},
		}
		req := testRequest(reader)
		snap := gardensync.Load(context.Background(), req)

		if !snap.Complete || len(snap.Items) != 3 {
			t.Fatalf("expected exactly 3 deduped items, got %d: %+v", len(snap.Items), snap.Items)
		}
		if snap.Items[0].Issue.ID != 1 || snap.Items[1].Issue.ID != 2 || snap.Items[2].Issue.ID != 3 {
			t.Fatalf("unexpected items order/identity: %+v", snap.Items)
		}
	})

	t.Run("ItemWithEmptyID", func(t *testing.T) {
		t.Parallel()
		// Issue with ID 0 and Draft stage (normally valid for Drafts if ID were non-zero)
		badIssue := garden.Issue{
			Repo:     42,
			ID:       0,
			Labels:   []string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeFeature), garden.FormatStageLabel(garden.StageDraft)},
			Metadata: []byte(`{"schema_version":1,"provenance":"adversarial","owner":"alice"}`),
		}
		reader := &fakeReader{
			pages: []garden.IssuePage{
				{
					Items:    []garden.Issue{badIssue},
					Complete: true,
				},
			},
		}
		req := testRequest(reader)
		snap := gardensync.Load(context.Background(), req)

		// CONTRACT: "A canonical work item is one GitHub Issue, identified by its numeric GitHub repository ID and Issue id (and node_id when supplied)."
		// An item with empty ID (0) is not a canonical work item and must not be accepted into Draft/Work without problem/attention.
		if len(snap.Items) > 0 && (snap.Items[0].Lane.Lane != garden.LaneAttention || len(snap.Items[0].Problems) == 0) {
			t.Errorf("// CONTRACT: \"A canonical work item is one GitHub Issue, identified by its numeric GitHub repository ID and Issue id (and node_id when supplied).\" - item with ID 0 must be in Attention with a Problem, got lane: %v, problems: %+v", snap.Items[0].Lane.Lane, snap.Items[0].Problems)
		}
	})

	t.Run("ContextCancelled", func(t *testing.T) {
		t.Parallel()

		// Cancelled before first call
		ctxBefore, cancelBefore := context.WithCancel(context.Background())
		cancelBefore()
		readerBefore := &fakeReader{pages: []garden.IssuePage{{Complete: true}}}
		snapBefore := gardensync.Load(ctxBefore, testRequest(readerBefore))
		if snapBefore.Complete || !hasProblemCode(snapBefore.Problems, "cancelled") {
			t.Fatalf("cancelled before call: %+v", snapBefore)
		}

		// Cancelled mid-page
		ctxMid, cancelMid := context.WithCancel(context.Background())
		readerMid := &fakeReader{
			onList: func(_ context.Context, call int, _ string) (garden.IssuePage, error) {
				if call == 0 {
					return garden.IssuePage{Items: []garden.Issue{testIssue(42, 1, garden.StageReady)}, NextCursor: "cursor-1"}, nil
				}
				cancelMid()
				<-ctxMid.Done()
				return garden.IssuePage{}, ctxMid.Err()
			},
		}
		snapMid := gardensync.Load(ctxMid, testRequest(readerMid))
		if snapMid.Complete || !hasProblemCode(snapMid.Problems, "cancelled") {
			t.Fatalf("cancelled mid-page: %+v", snapMid)
		}
	})
}

// -----------------------------------------------------------------------------
// Scenario 4: Malformed items
// -----------------------------------------------------------------------------

func TestMalformedItemsAdversarial(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		malformed  garden.Issue
		wantReason string
	}{
		{
			name: "MetadataInvalidJSON",
			malformed: garden.Issue{
				Repo:     42,
				ID:       101,
				Labels:   []string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeBug), garden.FormatStageLabel(garden.StageReady)},
				Metadata: []byte("{not-valid-json"),
			},
		},
		{
			name: "MetadataDuplicateKeys",
			malformed: garden.Issue{
				Repo:     42,
				ID:       102,
				Labels:   []string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeBug), garden.FormatStageLabel(garden.StageReady)},
				Metadata: []byte(`{"schema_version":1,"provenance":"import","owner":"alice","owner":"bob"}`),
			},
		},
		{
			name: "MetadataFutureSchema",
			malformed: garden.Issue{
				Repo:     42,
				ID:       103,
				Labels:   []string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeBug), garden.FormatStageLabel(garden.StageReady)},
				Metadata: []byte(`{"schema_version":999,"provenance":"future","owner":"alice"}`),
			},
		},
		{
			name: "MetadataOversize",
			malformed: garden.Issue{
				Repo:     42,
				ID:       104,
				Labels:   []string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeBug), garden.FormatStageLabel(garden.StageReady)},
				Metadata: []byte(strings.Repeat("x", garden.MaxMetadataBytes+1)),
			},
		},
		{
			name: "TwoStageLabels",
			malformed: garden.Issue{
				Repo: 42,
				ID:   105,
				Labels: []string{
					garden.ManagedLabel,
					garden.FormatTypeLabel(garden.TypeBug),
					garden.FormatStageLabel(garden.StageReady),
					garden.FormatStageLabel(garden.StageDraft),
				},
				Metadata: []byte(`{"schema_version":1,"provenance":"test","owner":"alice"}`),
			},
		},
		{
			name: "NoStageLabel",
			malformed: garden.Issue{
				Repo:     42,
				ID:       106,
				Labels:   []string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeBug)},
				Metadata: []byte(`{"schema_version":1,"provenance":"test","owner":"alice"}`),
			},
		},
		{
			name: "UnknownBonsaiLabel",
			malformed: garden.Issue{
				Repo:     42,
				ID:       107,
				Labels:   []string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeBug), garden.FormatStageLabel(garden.StageReady), "bonsai:invalid-extension"},
				Metadata: []byte(`{"schema_version":1,"provenance":"test","owner":"alice"}`),
			},
		},
		{
			name: "MissingManagedMarkerExplicitlyImported",
			malformed: garden.Issue{
				Repo:     42,
				ID:       108,
				Labels:   []string{garden.FormatTypeLabel(garden.TypeBug), garden.FormatStageLabel(garden.StageReady)},
				Metadata: []byte(`{"schema_version":1,"provenance":"test","owner":"alice"}`),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			validItem := testIssue(42, 200, garden.StageReady)

			reader := &fakeReader{
				pages: []garden.IssuePage{
					{
						Items:    []garden.Issue{tc.malformed, validItem},
						Complete: true,
					},
				},
				getIssueMap: map[garden.IssueID]garden.Issue{
					108: tc.malformed,
				},
			}

			req := testRequest(reader)
			if tc.malformed.ID == 108 {
				// Opt in via ExplicitImports
				req.ExplicitImports = map[garden.IssueID]struct{}{108: {}}
				reader.pages[0].Items = []garden.Issue{validItem}
			}

			snap := gardensync.Load(context.Background(), req)

			// Snapshot must remain complete
			if !snap.Complete {
				t.Fatalf("snapshot should remain complete despite malformed item: %+v", snap)
			}

			// Find malformed item
			var foundMalformed, foundValid bool
			for _, it := range snap.Items {
				if it.Issue.ID == tc.malformed.ID {
					foundMalformed = true
					if it.Lane.Lane != garden.LaneAttention {
						t.Fatalf("malformed item %d should be in LaneAttention, got %v", tc.malformed.ID, it.Lane.Lane)
					}
					if len(it.Problems) == 0 {
						t.Fatalf("malformed item %d should have problems recorded", tc.malformed.ID)
					}
				}
				if it.Issue.ID == validItem.ID {
					foundValid = true
					if len(it.Problems) > 0 {
						t.Fatalf("valid item poisoned by malformed neighbor: %+v", it.Problems)
					}
				}
			}

			if !foundMalformed {
				t.Fatalf("malformed item %d vanished from snapshot", tc.malformed.ID)
			}
			if !foundValid {
				t.Fatalf("valid item 200 vanished from snapshot")
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Scenario 5: Deleted lifecycle
// -----------------------------------------------------------------------------

func TestDeletedLifecycleAdversarial(t *testing.T) {
	t.Parallel()
	gardenID := garden.GardenID{Repo: 42}
	localID := garden.LocalRepoID("clone-deleted")
	store := gardensync.NewStore(gardenID, localID)

	// Cycle 1: Issue 1 is present in complete snapshot
	snap1 := gardensync.Snapshot{
		GardenID:    gardenID,
		LocalRepoID: localID,
		Generation:  1,
		Epoch:       1,
		Complete:    true,
		Source:      gardensync.SourceLive,
		Items:       []gardensync.Item{{Issue: garden.Issue{ID: 1}}},
	}
	_ = store.Apply(snap1)
	if len(store.Read().Items) != 1 || store.Read().Items[0].Deleted {
		t.Fatal("cycle 1: issue 1 not properly set")
	}

	// Absent in INCOMPLETE result => NO deletion marker
	snapIncomplete := gardensync.Snapshot{
		GardenID:    gardenID,
		LocalRepoID: localID,
		Generation:  2,
		Epoch:       1,
		Complete:    false,
		Source:      gardensync.SourceStale,
		Items:       nil,
	}
	_ = store.Apply(snapIncomplete)
	if len(store.Read().Items) != 1 || store.Read().Items[0].Deleted {
		t.Fatalf("absent in incomplete result marked item deleted: %+v", store.Read().Items)
	}

	// Absent in COMPLETE result => Deleted marker for exactly ONE accepted cycle
	snap2 := gardensync.Snapshot{
		GardenID:    gardenID,
		LocalRepoID: localID,
		Generation:  3,
		Epoch:       1,
		Complete:    true,
		Source:      gardensync.SourceLive,
		Items:       nil,
	}
	_ = store.Apply(snap2)
	read2 := store.Read()
	if len(read2.Items) != 1 || !read2.Items[0].Deleted {
		t.Fatalf("cycle 2: expected 1 deleted item, got %+v", read2.Items)
	}
	if !hasFindingCode(read2.Findings, gardensync.FindingDeleted) {
		t.Fatal("cycle 2: expected FindingDeleted")
	}

	// Cycle 3: Second complete cycle without issue 1 => completely removed
	snap3 := gardensync.Snapshot{
		GardenID:    gardenID,
		LocalRepoID: localID,
		Generation:  4,
		Epoch:       1,
		Complete:    true,
		Source:      gardensync.SourceLive,
		Items:       nil,
	}
	_ = store.Apply(snap3)
	read3 := store.Read()
	if len(read3.Items) != 0 {
		t.Fatalf("cycle 3: deleted item survived more than one cycle: %+v", read3.Items)
	}

	// Reappearing issue clears marker
	_ = store.Apply(gardensync.Snapshot{GardenID: gardenID, LocalRepoID: localID, Generation: 5, Epoch: 1, Complete: true, Source: gardensync.SourceLive, Items: []gardensync.Item{{Issue: garden.Issue{ID: 2}}}})
	_ = store.Apply(gardensync.Snapshot{GardenID: gardenID, LocalRepoID: localID, Generation: 6, Epoch: 1, Complete: true, Source: gardensync.SourceLive, Items: nil})
	if !store.Read().Items[0].Deleted {
		t.Fatal("issue 2 should be marked deleted")
	}
	_ = store.Apply(gardensync.Snapshot{GardenID: gardenID, LocalRepoID: localID, Generation: 7, Epoch: 1, Complete: true, Source: gardensync.SourceLive, Items: []gardensync.Item{{Issue: garden.Issue{ID: 2}}}})
	if store.Read().Items[0].Deleted {
		t.Fatal("reappearing issue 2 must have Deleted cleared")
	}
}

// -----------------------------------------------------------------------------
// Scenario 6: Findings and secrets scrubbing
// -----------------------------------------------------------------------------

func TestFindingsAndSecretsAdversarial(t *testing.T) {
	t.Parallel()

	t.Run("ProjectionDriftVsUnavailable", func(t *testing.T) {
		t.Parallel()
		reader := &fakeReader{
			pages: []garden.IssuePage{
				{
					Items:    []garden.Issue{testIssue(42, 1, garden.StageInProgress)},
					Complete: true,
				},
			},
		}
		req := testRequest(reader)
		req.ProjectID = "PROJ-1"
		req.StageField = "Stage"
		req.TypeField = "Type"

		// Drift: Project field says "done", canonical label says "in-progress"
		req.Projects = fakeProjectReader{
			projects: []garden.Project{
				{
					ID: "PROJ-1",
					IssueFields: map[garden.IssueID]map[string]string{
						1: {"Stage": "done", "Type": "feature"},
					},
				},
			},
		}
		snapDrift := gardensync.Load(context.Background(), req)
		if !hasFindingCode(snapDrift.Findings, gardensync.FindingProjectionDrift) {
			t.Fatalf("expected FindingProjectionDrift, got %+v", snapDrift.Findings)
		}

		// Unavailable: Project unreadable yields informational finding NOT drift
		req.Projects = fakeProjectReader{err: errors.New("cannot connect to projects")}
		snapUnavail := gardensync.Load(context.Background(), req)
		if !hasFindingCode(snapUnavail.Findings, gardensync.FindingProjectionUnavailable) {
			t.Fatalf("expected FindingProjectionUnavailable, got %+v", snapUnavail.Findings)
		}
		if hasFindingCode(snapUnavail.Findings, gardensync.FindingProjectionDrift) {
			t.Fatalf("unreadable projects should NOT produce FindingProjectionDrift: %+v", snapUnavail.Findings)
		}
	})

	t.Run("StaleApprovalAndConflictingStages", func(t *testing.T) {
		t.Parallel()
		// Issue with 2 stage labels
		issueConflict := testIssue(42, 1, garden.StageReady, garden.FormatStageLabel(garden.StageBlocked))
		reader := &fakeReader{
			pages: []garden.IssuePage{
				{
					Items:    []garden.Issue{issueConflict},
					Complete: true,
				},
			},
		}
		snap := gardensync.Load(context.Background(), testRequest(reader))
		if !hasFindingCode(snap.Findings, gardensync.FindingStageConflict) {
			t.Fatalf("expected FindingStageConflict, got %+v", snap.Findings)
		}
	})

	t.Run("NoCredentialsInFindingsProblemsOrStatus", func(t *testing.T) {
		t.Parallel()
		fakeToken := "ghp_FAKETOKEN123_SYNC_SECRET"

		reader := &fakeReader{
			pageErrs: []error{fmt.Errorf("upstream failure token: %s", fakeToken)},
			getIssueErr: map[garden.IssueID]error{
				99: fmt.Errorf("auth error token: %s", fakeToken),
			},
		}

		req := testRequest(reader)
		snap := gardensync.Load(context.Background(), req)

		status := gardensync.Status(gardensync.AssociationView{State: gardensync.AssociationConnected}, snap)

		// Check all findings
		for _, f := range snap.Findings {
			if strings.Contains(f.Message, fakeToken) {
				t.Fatalf("secret leaked in Finding message: %s", f.Message)
			}
		}

		// Check all problems
		for _, p := range snap.Problems {
			if strings.Contains(p.Message, fakeToken) || strings.Contains(p.Code, fakeToken) {
				t.Fatalf("secret leaked in Problem: %+v", p)
			}
		}

		// Check status
		if strings.Contains(status.State, fakeToken) {
			t.Fatalf("secret leaked in Status.State: %s", status.State)
		}
	})
}

// -----------------------------------------------------------------------------
// Scenario 7: Status matrix
// -----------------------------------------------------------------------------

func TestStatusMatrixAdversarial(t *testing.T) {
	t.Parallel()

	assocs := []gardensync.AssociationState{
		gardensync.AssociationNone,
		gardensync.AssociationSkipped,
		gardensync.AssociationReadOnly,
		gardensync.AssociationConnected,
		gardensync.AssociationPartialSetup,
		gardensync.AssociationAwaitingManual,
		gardensync.AssociationOperationInProgress,
	}

	snapStates := []struct {
		name string
		snap gardensync.Snapshot
	}{
		{"FreshComplete", gardensync.Snapshot{Complete: true, Source: gardensync.SourceLive}},
		{"Stale", gardensync.Snapshot{Complete: false, Source: gardensync.SourceStale}},
		{"IncompleteLive", gardensync.Snapshot{Complete: false, Source: gardensync.SourceLive}},
		{"AccessRevoked", gardensync.Snapshot{Source: gardensync.SourceStale, AccessRevoked: true}},
		{"Offline", gardensync.Snapshot{Source: gardensync.SourceStale, BackoffHint: 30 * time.Second}},
		{"Empty", gardensync.Snapshot{}},
	}

	for _, a := range assocs {
		for _, s := range snapStates {
			t.Run(fmt.Sprintf("%s/%s", a, s.name), func(t *testing.T) {
				status := gardensync.Status(gardensync.AssociationView{State: a}, s.snap)

				// INVARIANT: Read-only and Skipped must never claim "connected"
				if (a == gardensync.AssociationReadOnly || a == gardensync.AssociationSkipped) && status.State == "connected" {
					t.Fatalf("read-only or skipped association claimed 'connected': %s", status.State)
				}

				// Check precedence
				switch {
				case s.snap.AccessRevoked:
					if status.State != "access-revoked" {
						t.Fatalf("expected 'access-revoked', got %q", status.State)
					}
				case s.snap.Source == gardensync.SourceStale && s.snap.BackoffHint > 0:
					if status.State != "offline" {
						t.Fatalf("expected 'offline', got %q", status.State)
					}
				case a == gardensync.AssociationNone:
					if status.State != "none" {
						t.Fatalf("expected 'none', got %q", status.State)
					}
				case a == gardensync.AssociationSkipped:
					if status.State != "skipped" {
						t.Fatalf("expected 'skipped', got %q", status.State)
					}
				case a == gardensync.AssociationReadOnly:
					if status.State != "read-only" {
						t.Fatalf("expected 'read-only', got %q", status.State)
					}
				case a == gardensync.AssociationPartialSetup:
					if status.State != "partial-setup" {
						t.Fatalf("expected 'partial-setup', got %q", status.State)
					}
				case a == gardensync.AssociationAwaitingManual:
					if status.State != "awaiting-manual" {
						t.Fatalf("expected 'awaiting-manual', got %q", status.State)
					}
				case a == gardensync.AssociationOperationInProgress:
					if status.State != "operation-in-progress" {
						t.Fatalf("expected 'operation-in-progress', got %q", status.State)
					}
				case a == gardensync.AssociationConnected:
					if s.snap.Source == gardensync.SourceStale {
						if status.State != "stale" {
							t.Fatalf("expected 'stale', got %q", status.State)
						}
					} else if !s.snap.Complete {
						if status.State != "partial" {
							t.Fatalf("expected 'partial', got %q", status.State)
						}
					} else {
						if status.State != "connected" {
							t.Fatalf("expected 'connected', got %q", status.State)
						}
					}
				}
			})
		}
	}
}

// -----------------------------------------------------------------------------
// Scenario 8: Store isolation and defensive copies
// -----------------------------------------------------------------------------

func TestStoreIsolationAdversarial(t *testing.T) {
	t.Parallel()

	gardenID := garden.GardenID{Repo: 42}
	localA := garden.LocalRepoID("clone-alpha")
	localB := garden.LocalRepoID("clone-beta")

	if gardensync.LocalKey(localA) == gardensync.LocalKey(localB) {
		t.Fatalf("LocalKey collision: %s == %s", gardensync.LocalKey(localA), gardensync.LocalKey(localB))
	}

	storeA := gardensync.NewStore(gardenID, localA)
	storeB := gardensync.NewStore(gardenID, localB)

	// Apply item to storeA
	snap := gardensync.Snapshot{
		GardenID:    gardenID,
		LocalRepoID: localA,
		Generation:  1,
		Epoch:       1,
		Complete:    true,
		Source:      gardensync.SourceLive,
		Items: []gardensync.Item{
			{
				Issue: garden.Issue{
					ID:       1,
					Labels:   []string{"bonsai:managed", "bonsai:type/feature"},
					Metadata: []byte(`{"schema_version":1}`),
				},
				Problems: []gardensync.Problem{{Code: "prob1", Message: "p1"}},
			},
		},
		Problems: []gardensync.Problem{{Code: "top-prob"}},
		Findings: []gardensync.Finding{{Code: gardensync.FindingDeleted}},
	}
	_ = storeA.Apply(snap)

	// storeB must remain empty
	if len(storeB.Read().Items) != 0 {
		t.Fatalf("storeB contaminated by storeA write: %+v", storeB.Read())
	}

	// Defensive copy verification: mutate returned snapshot from storeA
	readA1 := storeA.Read()
	readA1.Items[0].Issue.Labels[0] = "MUTATED_LABEL"
	readA1.Items[0].Issue.Metadata[0] = 'Z'
	readA1.Items[0].Problems[0].Code = "MUTATED_PROBLEM"
	readA1.Problems[0].Code = "MUTATED_TOP_PROBLEM"
	readA1.Findings[0].Code = "MUTATED_FINDING"

	// Read again from storeA; assert original state unchanged
	readA2 := storeA.Read()
	if readA2.Items[0].Issue.Labels[0] == "MUTATED_LABEL" {
		t.Fatal("defensive copy failed: internal Item Labels was mutated")
	}
	if readA2.Items[0].Issue.Metadata[0] == 'Z' {
		t.Fatal("defensive copy failed: internal Item Metadata was mutated")
	}
	if readA2.Items[0].Problems[0].Code == "MUTATED_PROBLEM" {
		t.Fatal("defensive copy failed: internal Item Problems was mutated")
	}
	if readA2.Problems[0].Code == "MUTATED_TOP_PROBLEM" {
		t.Fatal("defensive copy failed: internal Problems was mutated")
	}
	if readA2.Findings[0].Code == "MUTATED_FINDING" {
		t.Fatal("defensive copy failed: internal Findings was mutated")
	}
}

// -----------------------------------------------------------------------------
// Scenario 9: Package boundary
// -----------------------------------------------------------------------------

func TestPackageBoundaryAdversarial(t *testing.T) {
	t.Parallel()

	dir := "."
	if _, err := os.Stat("sync.go"); err != nil {
		dir = filepath.Join("internal", "core", "garden", "sync")
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatalf("reading sync dir: %v", err)
	}

	for _, filePath := range files {
		fileName := filepath.Base(filePath)
		if strings.HasSuffix(fileName, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		node, err := parser.ParseFile(fset, filePath, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parsing %s: %v", filePath, err)
		}

		for _, imp := range node.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if strings.Contains(importPath, "garden/connect") {
				t.Fatalf("sync package boundary violation: %s imports %s", fileName, importPath)
			}
		}

		fullNode, err := parser.ParseFile(fset, filePath, nil, parser.AllErrors)
		if err != nil {
			t.Fatalf("full parsing %s: %v", filePath, err)
		}
		ast.Inspect(fullNode, func(n ast.Node) bool {
			if ident, ok := n.(*ast.Ident); ok {
				if ident.Name == "IssueWriter" || ident.Name == "ProjectSetupWriter" {
					t.Fatalf("sync references writer capability %s in %s", ident.Name, fileName)
				}
			}
			return true
		})
	}
}

// -----------------------------------------------------------------------------
// Scenario 10: Fuzz Apply invariants
// -----------------------------------------------------------------------------

func FuzzApplyOrder(f *testing.F) {
	gardenID := garden.GardenID{Repo: 42}
	localID := garden.LocalRepoID("clone-fuzz")

	// Seed corpus with byte slices encoding action sequences
	f.Add([]byte{1, 1, 1, 2, 1, 1, 3, 1, 1})
	f.Add([]byte{3, 1, 1, 2, 1, 1, 1, 1, 1})
	f.Add([]byte{1, 1, 0, 2, 1, 1, 3, 1, 0})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 3 {
			return
		}

		store := gardensync.NewStore(gardenID, localID)
		var prevAcceptedGen uint64
		var prevItemCount int

		for i := 0; i+2 < len(data); i += 3 {
			gen := uint64(data[i])
			complete := (data[i+1] % 2) == 1
			hasError := (data[i+2] % 2) == 1

			snap := gardensync.Snapshot{
				GardenID:    gardenID,
				LocalRepoID: localID,
				Generation:  gen,
				Epoch:       1,
				Complete:    complete && !hasError,
				Source:      gardensync.SourceLive,
			}
			if !hasError {
				for itemID := 1; itemID <= int(data[i]%5); itemID++ {
					snap.Items = append(snap.Items, gardensync.Item{Issue: garden.Issue{ID: garden.IssueID(itemID)}})
				}
			} else {
				snap.Problems = append(snap.Problems, gardensync.Problem{Code: "read-failed"})
			}
			if !snap.Complete {
				snap.Source = gardensync.SourceStale
			}

			_ = store.Apply(snap)
			cur := store.Read()

			// INVARIANT 1: Accepted generation never decreases within epoch
			if cur.Generation < prevAcceptedGen {
				t.Fatalf("accepted generation decreased: was %d, now %d", prevAcceptedGen, cur.Generation)
			}
			prevAcceptedGen = cur.Generation

			// INVARIANT 2: Incomplete/error never reduces item set
			if (!complete || hasError) && len(cur.Items) < prevItemCount {
				t.Fatalf("incomplete/error reduced items: was %d, now %d", prevItemCount, len(cur.Items))
			}

			prevItemCount = len(cur.Items)
		}
	})
}
