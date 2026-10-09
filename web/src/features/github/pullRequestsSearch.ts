export interface PullRequestsSearch {
  pr?: string
}

export const validatePullRequestsSearch = (search: Record<string, unknown>): PullRequestsSearch => ({
  pr: typeof search.pr === 'string' && search.pr ? search.pr : undefined,
})
