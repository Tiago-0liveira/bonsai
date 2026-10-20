package fake_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/fake"
)

// Helper creates an isolated fake instance populated with count issues for a given repo.
func makeFakeWithManagedIssues(repo garden.RepoID, count int, pageSize int) *fake.Fake {
	f := fake.New()
	f.SetPageSize(pageSize)
	for i := garden.IssueID(1); i <= garden.IssueID(count); i++ {
		f.SeedIssue(garden.Issue{
			Repo:   repo,
			ID:     i,
			Labels: []string{garden.ManagedLabel},
		})
	}
	return f
}

// 6. Fake adapter adversarial tests

func TestPaginationAdversarial(t *testing.T) {
	t.Parallel()
	const repo = garden.RepoID(99)
	const totalItems = 7

	// Test across page sizes 1, 2, N, N+1
	for _, pageSize := range []int{1, 2, totalItems, totalItems + 1} {
		t.Run(fmt.Sprintf("PageSize_%d", pageSize), func(t *testing.T) {
			t.Parallel()
			f := makeFakeWithManagedIssues(repo, totalItems, pageSize)
			seen := make(map[garden.IssueID]int)
			cursor := ""
			pageCount := 0

			for {
				pageCount++
				page, err := f.ListManagedIssues(t.Context(), repo, cursor)
				if err != nil {
					t.Fatalf("page %d failed: %v", pageCount, err)
				}
				for _, item := range page.Items {
					seen[item.ID]++
				}
				if page.Complete {
					if page.NextCursor != "" {
						t.Fatalf("final complete page returned non-empty cursor: %q", page.NextCursor)
					}
					break
				}
				if page.NextCursor == "" {
					t.Fatalf("incomplete page %d returned empty NextCursor", pageCount)
				}
				cursor = page.NextCursor
			}

			if len(seen) != totalItems {
				t.Fatalf("expected %d distinct items, saw %d", totalItems, len(seen))
			}
			for id, count := range seen {
				if count != 1 {
					t.Fatalf("item %v seen %d times, want exactly 1", id, count)
				}
			}
		})
	}
}

func TestCursorFromMutatedCollectionAdversarial(t *testing.T) {
	t.Parallel()
	// CONTRACT: Before implementing a real adapter, verify conditional writes, pagination, permissions, and payload limits against current primary GitHub documentation; this contract makes no claim about their availability or exact behavior.
	const repo = garden.RepoID(10)
	f := makeFakeWithManagedIssues(repo, 5, 2)

	p1, err := f.ListManagedIssues(t.Context(), repo, "")
	if err != nil {
		t.Fatalf("first page failed: %v", err)
	}
	if p1.NextCursor == "" {
		t.Fatalf("expected cursor from page 1, got empty")
	}

	// Mutate collection by removing issues so the remaining item count is less than cursor index
	f.ReplaceIssues(repo, []garden.Issue{{Repo: repo, ID: 1, Labels: []string{garden.ManagedLabel}}})

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("// CONTRACT: Before implementing a real adapter, verify conditional writes, pagination, permissions, and payload limits against current primary GitHub documentation; this contract makes no claim about their availability or exact behavior: cursor from mutated collection panicked: %v", r)
		}
	}()

	p2, err := f.ListManagedIssues(t.Context(), repo, p1.NextCursor)
	t.Logf("ListManagedIssues after mutation returned page=%+v err=%v", p2, err)
}

func TestContextCancellationAdversarial(t *testing.T) {
	t.Parallel()
	// CONTRACT: Before implementing a real adapter, verify conditional writes, pagination, permissions, and payload limits against current primary GitHub documentation; this contract makes no claim about their availability or exact behavior.
	const repo = garden.RepoID(20)
	f := makeFakeWithManagedIssues(repo, 5, 1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	page, err := f.ListManagedIssues(ctx, repo, "")
	if err == nil {
		t.Errorf("// CONTRACT: Before implementing a real adapter, verify conditional writes, pagination, permissions, and payload limits against current primary GitHub documentation; this contract makes no claim about their availability or exact behavior: cancelled context returned nil error")
	} else if !errors.Is(err, context.Canceled) {
		t.Errorf("expected error wrapping context.Canceled, got %v", err)
	}
	if page.Complete {
		t.Errorf("// CONTRACT: Before implementing a real adapter, verify conditional writes, pagination, permissions, and payload limits against current primary GitHub documentation; this contract makes no claim about their availability or exact behavior: cancelled context returned page presented as complete")
	}
}

func TestPermissionDeniedInjectionAdversarial(t *testing.T) {
	t.Parallel()
	f := fake.New()
	f.SetDenied(true)
	const repo = garden.RepoID(30)
	const issueID = garden.IssueID(1)

	t.Run("ListManagedIssues", func(t *testing.T) {
		t.Parallel()
		_, err := f.ListManagedIssues(t.Context(), repo, "")
		if !errors.Is(err, garden.ErrPermissionDenied) {
			t.Fatalf("expected ErrPermissionDenied, got %v", err)
		}
	})

	t.Run("GetIssue", func(t *testing.T) {
		t.Parallel()
		_, err := f.GetIssue(t.Context(), repo, issueID)
		if !errors.Is(err, garden.ErrPermissionDenied) {
			t.Fatalf("expected ErrPermissionDenied, got %v", err)
		}
	})

	t.Run("ListComments", func(t *testing.T) {
		t.Parallel()
		_, err := f.ListComments(t.Context(), repo, issueID)
		if !errors.Is(err, garden.ErrPermissionDenied) {
			t.Fatalf("expected ErrPermissionDenied, got %v", err)
		}
	})

	t.Run("AppendComment", func(t *testing.T) {
		t.Parallel()
		_, err := f.AppendComment(t.Context(), repo, issueID, "comment body")
		if !errors.Is(err, garden.ErrPermissionDenied) {
			t.Fatalf("expected ErrPermissionDenied, got %v", err)
		}
	})

	t.Run("UpdateLabels", func(t *testing.T) {
		t.Parallel()
		err := f.UpdateLabels(t.Context(), repo, issueID, []string{garden.ManagedLabel})
		if !errors.Is(err, garden.ErrPermissionDenied) {
			t.Fatalf("expected ErrPermissionDenied, got %v", err)
		}
	})

	t.Run("ListProjects", func(t *testing.T) {
		t.Parallel()
		_, err := f.ListProjects(t.Context(), repo)
		if !errors.Is(err, garden.ErrPermissionDenied) {
			t.Fatalf("expected ErrPermissionDenied, got %v", err)
		}
	})

	t.Run("EnsureProject", func(t *testing.T) {
		t.Parallel()
		_, err := f.EnsureProject(t.Context(), repo, "Test Project")
		if !errors.Is(err, garden.ErrPermissionDenied) {
			t.Fatalf("expected ErrPermissionDenied, got %v", err)
		}
	})

	t.Run("EnsureField", func(t *testing.T) {
		t.Parallel()
		_, err := f.EnsureField(t.Context(), "project-id", garden.ProjectField{Name: "Status"})
		if !errors.Is(err, garden.ErrPermissionDenied) {
			t.Fatalf("expected ErrPermissionDenied, got %v", err)
		}
	})
}

func TestIssueReaderInterfaceIsolation(t *testing.T) {
	t.Parallel()
	f := fake.New()
	const repo = garden.RepoID(40)
	f.SeedIssue(garden.Issue{Repo: repo, ID: 1, Labels: []string{garden.ManagedLabel}})

	// Assign to narrow read-only port
	var reader garden.IssueReader = f

	// Observation methods succeed
	issue, err := reader.GetIssue(t.Context(), repo, 1)
	if err != nil {
		t.Fatalf("reader.GetIssue failed: %v", err)
	}
	if issue.ID != 1 {
		t.Fatalf("unexpected issue ID: %v", issue.ID)
	}

	// Document compile-time interface separation: IssueReader method set
	// contains only ListManagedIssues, GetIssue, and ListComments.
	type readerMethodSet interface {
		ListManagedIssues(context.Context, garden.RepoID, string) (garden.IssuePage, error)
		GetIssue(context.Context, garden.RepoID, garden.IssueID) (garden.Issue, error)
		ListComments(context.Context, garden.RepoID, garden.IssueID) ([]garden.IssueComment, error)
	}
	var _ readerMethodSet = reader

	// Document compile-time interface separation: IssueWriter is a distinct interface
	type writerMethodSet interface {
		AppendComment(context.Context, garden.RepoID, garden.IssueID, string) (garden.IssueComment, error)
		UpdateLabels(context.Context, garden.RepoID, garden.IssueID, []string) error
	}
	var writer garden.IssueWriter = f
	var _ writerMethodSet = writer
}

// 7. Concurrency adversarial tests

func TestConcurrencyAdversarial(t *testing.T) {
	t.Parallel()
	f := fake.New()
	const repo = garden.RepoID(77)

	for i := garden.IssueID(1); i <= 10; i++ {
		f.SeedIssue(garden.Issue{
			Repo:   repo,
			ID:     i,
			Labels: []string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeFeature), garden.FormatStageLabel(garden.StageDraft)},
		})
	}

	var wg sync.WaitGroup
	ctx := t.Context()

	// 10 concurrent writers
	for w := 0; w < 10; w++ {
		wg.Add(1)
		workerID := w
		go func() {
			defer wg.Done()
			for iter := 0; iter < 50; iter++ {
				issueID := garden.IssueID((iter % 10) + 1)
				if workerID%2 == 0 {
					_, _ = f.AppendComment(ctx, repo, issueID, fmt.Sprintf("comment from worker %d iter %d", workerID, iter))
				} else {
					_ = f.UpdateLabels(ctx, repo, issueID, []string{
						garden.ManagedLabel,
						garden.FormatTypeLabel(garden.TypeFeature),
						garden.FormatStageLabel(garden.StageInProgress),
					})
				}
				f.SeedIssue(garden.Issue{
					Repo:   repo,
					ID:     garden.IssueID(100 + workerID*50 + iter),
					Labels: []string{garden.ManagedLabel},
				})
			}
		}()
	}

	// 10 concurrent readers
	for r := 0; r < 10; r++ {
		wg.Add(1)
		workerID := r
		go func() {
			defer wg.Done()
			for iter := 0; iter < 50; iter++ {
				issueID := garden.IssueID((iter % 10) + 1)
				if workerID%2 == 0 {
					_, _ = f.ListManagedIssues(ctx, repo, "")
				} else {
					_, _ = f.GetIssue(ctx, repo, issueID)
					_, _ = f.ListComments(ctx, repo, issueID)
				}
				_, _ = f.ListProjects(ctx, repo)
				_, _ = f.EnsureProject(ctx, repo, fmt.Sprintf("proj-%d", iter%3))
			}
		}()
	}

	wg.Wait()
}
