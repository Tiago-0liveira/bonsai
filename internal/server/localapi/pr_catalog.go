package localapi

import (
	"context"
	"sort"
	"time"

	gh "github.com/Tiago-0liveira/bonsai/internal/git/github"
)

const prPagesPerJob = 5

// A bounded job keeps its continuation on failure and publishes only a complete
// replacement. Open PRs are fully reconciled so closed or merged PRs disappear.
// Closed history is fetched separately only when requested by the browser.
func readPRCatalog(ctx context.Context, service gh.GitHubService, repo string, entry *providerRepoEntry, now time.Time) ([]gh.PullRequest, bool, error) {
	paged, ok := service.(gh.PagedPullRequests)
	if !ok {
		prs, err := service.PullRequests(ctx, repo, gh.PRFilter{State: "open"})
		return prs, true, err
	}
	if entry.nextPage == 0 {
		entry.nextPage, entry.startedAt, entry.pending = 1, now, nil
	}
	for count := 0; count < prPagesPerJob; count++ {
		batch, err := paged.PullRequestPage(ctx, repo, gh.PRFilter{State: "open"}, entry.nextPage)
		if err != nil {
			return nil, false, err
		}
		finished := batch.NextPage == 0
		for _, pr := range batch.Items {
			entry.pending = append(entry.pending, pr)
		}
		entry.nextPage = batch.NextPage
		if finished {
			byNumber := map[int]gh.PullRequest{}
			for _, pr := range entry.pending {
				if previous, exists := byNumber[pr.Number]; !exists || !previous.UpdatedAt.After(pr.UpdatedAt) {
					byNumber[pr.Number] = pr
				}
			}
			prs := make([]gh.PullRequest, 0, len(byNumber))
			for _, pr := range byNumber {
				prs = append(prs, pr)
			}
			sort.Slice(prs, func(i, j int) bool {
				if prs[i].UpdatedAt.Equal(prs[j].UpdatedAt) {
					return prs[i].Number > prs[j].Number
				}
				return prs[i].UpdatedAt.After(prs[j].UpdatedAt)
			})
			entry.pending, entry.nextPage = nil, 0
			return prs, true, nil
		}
	}
	if entry.value != nil && entry.value.PRCatalogComplete {
		return append([]gh.PullRequest(nil), entry.value.PullRequests...), false, nil
	}
	// On a first connection, publish pages as they arrive. A complete existing
	// catalog still stays visible until its replacement scan finishes.
	return append([]gh.PullRequest(nil), entry.pending...), false, nil
}
