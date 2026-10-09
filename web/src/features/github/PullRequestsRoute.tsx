import { useNavigate, useSearch } from '@tanstack/react-router'
import { PullRequestsPage } from './PullRequestsPage'
import type { PullRequestsSearch } from './pullRequestsSearch'

/** Keeps the selected pull request in the URL so a link restores it. */
export function PullRequestsRoute() {
  const { pr } = useSearch({ strict: false }) as PullRequestsSearch
  const navigate = useNavigate()
  return (
    <PullRequestsPage
      selectedId={pr}
      onSelect={(id) => void navigate({ to: '.', search: { pr: id }, replace: true })}
      onShowOnCanvas={() => void navigate({ to: '/' })}
    />
  )
}
