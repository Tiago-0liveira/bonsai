package localapi

import (
	"context"
	"errors"
	"testing"
	"time"

	gh "github.com/Tiago-0liveira/bonsai/internal/git/github"
)

type pagedTestProvider struct {
	*syncTestGitHub
	calls []int
	fail  int
	total int
}

func (p *pagedTestProvider) PullRequestPage(_ context.Context, _ string, f gh.PRFilter, page int) (gh.PullRequestPage, error) {
	p.calls = append(p.calls, page)
	if f.State != "open" {
		panic("expected open-only catalog")
	}
	if page == p.fail {
		return gh.PullRequestPage{}, errors.New("provider offline")
	}
	next := page + 1
	if page == p.total {
		next = 0
	}
	return gh.PullRequestPage{Items: []gh.PullRequest{{Number: page, State: "open", UpdatedAt: time.Unix(int64(p.total-page), 0)}}, NextPage: next}, nil
}

func TestPRCatalogResumesBeyondHundredPagesAndRetainsCompleteCatalog(t *testing.T) {
	provider := &pagedTestProvider{syncTestGitHub: &syncTestGitHub{}, total: 103}
	entry := &providerRepoEntry{value: &browserRemoteSnapshot{PullRequests: []gh.PullRequest{{Number: 900}}, PRCatalogComplete: true}}
	now := time.Unix(10000, 0)
	for job := 0; job < 30; job++ {
		rows, complete, err := readPRCatalog(context.Background(), provider, "owner/repo", entry, now)
		if err != nil {
			t.Fatal(err)
		}
		if complete {
			if len(rows) != 103 {
				t.Fatal(len(rows))
			}
			break
		}
		if len(rows) != 1 || rows[0].Number != 900 {
			t.Fatal("published incomplete replacement", rows)
		}
		if job == 29 {
			t.Fatal("did not finish")
		}
	}
	if len(provider.calls) != 103 || provider.calls[102] != 103 {
		t.Fatal(provider.calls)
	}
}

func TestPRCatalogFailureResumesAndOpenCatalogFullyReconciles(t *testing.T) {
	provider := &pagedTestProvider{syncTestGitHub: &syncTestGitHub{}, total: 12, fail: 7}
	entry := &providerRepoEntry{}
	now := time.Unix(100, 0)
	if _, done, err := readPRCatalog(context.Background(), provider, "owner/repo", entry, now); err != nil || done {
		t.Fatal(done, err)
	}
	if _, _, err := readPRCatalog(context.Background(), provider, "owner/repo", entry, now); err == nil || entry.nextPage != 7 {
		t.Fatal(err, entry.nextPage)
	}
	provider.fail = 0
	for entry.nextPage != 0 {
		if _, _, err := readPRCatalog(context.Background(), provider, "owner/repo", entry, now); err != nil {
			t.Fatal(err)
		}
	}
	entry.value = &browserRemoteSnapshot{UpdatedAt: time.Unix(11, 0), PRCatalogComplete: true, PullRequests: []gh.PullRequest{{Number: 100}}}
	entry.fullReconcileAt = now
	entry.catalogUpdatedThrough = time.Unix(11, 0)
	provider.calls = nil
	var rows []gh.PullRequest
	var done bool
	var err error
	for !done && err == nil {
		rows, done, err = readPRCatalog(context.Background(), provider, "owner/repo", entry, now.Add(time.Minute))
	}
	if err != nil || !done || len(rows) != 12 || len(provider.calls) != 12 {
		t.Fatal(rows, done, err, provider.calls)
	}
}

func TestPRCatalogPublishesInitialPagesBeforeScanCompletes(t *testing.T) {
	provider := &pagedTestProvider{syncTestGitHub: &syncTestGitHub{}, total: 12}
	entry := &providerRepoEntry{}
	now := time.Unix(100, 0)
	for expected := 5; expected <= 10; expected += 5 {
		rows, complete, err := readPRCatalog(context.Background(), provider, "owner/repo", entry, now)
		if err != nil || complete || len(rows) != expected {
			t.Fatalf("initial catalog rows=%d complete=%v error=%v", len(rows), complete, err)
		}
		entry.value = &browserRemoteSnapshot{PullRequests: rows, PRCatalogLoading: true}
	}
}
