import { act, fireEvent, render, renderHook, screen, waitFor } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { useBonsaiStore } from '../../stores/bonsai'
import { pullRequests } from '../../test/fixtures/pullRequests'
import { fetchClosedPullRequests } from '../../api/git'
import { usePullRequestCatalog } from './usePullRequestCatalog'
import { PullRequestsPage } from './PullRequestsPage'

vi.mock('../../api/git', async importOriginal => ({ ...await importOriginal<typeof import('../../api/git')>(), fetchClosedPullRequests: vi.fn() }))

it('loads only open rows initially and shares closed history after explicit selection', async () => {
  const open = { ...pullRequests[0], id: 'lazy:1', status: 'Open' as const }
  const draft = { ...open, id: 'lazy:2', status: 'Draft' as const }
  const merged = { ...open, id: 'lazy:3', status: 'Merged' as const }
  vi.mocked(fetchClosedPullRequests).mockResolvedValue([merged])
  useBonsaiStore.setState({ pullRequests: [open, draft, merged], projects: [] })
  const first = renderHook(() => usePullRequestCatalog('lazy'))
  const second = renderHook(() => usePullRequestCatalog('lazy'))
  expect(first.result.current.rows).toEqual([open, draft])
  expect(fetchClosedPullRequests).not.toHaveBeenCalled()
  act(() => first.result.current.setTab('closed'))
  await waitFor(() => expect(first.result.current.rows).toEqual([merged]))
  act(() => second.result.current.setTab('closed'))
  await waitFor(() => expect(second.result.current.rows).toEqual([merged]))
  expect(fetchClosedPullRequests).toHaveBeenCalledTimes(1)
  act(() => first.result.current.setTab('open'))
  expect(first.result.current.rows).toEqual([open, draft])
})


it('keeps Closed accessible when the GitHub page has no open PRs', async () => {
  vi.mocked(fetchClosedPullRequests).mockClear().mockResolvedValue([])
  useBonsaiStore.setState({ activeProjectId: 'empty', pullRequests: [], projects: [], syncFreshness: { empty: { provider: { state: 'ready' } } } })
  render(<PullRequestsPage />)
  expect(screen.getByRole('button', { name: 'Open' })).toHaveAttribute('aria-pressed', 'true')
  expect(fetchClosedPullRequests).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: 'Closed' }))
  await waitFor(() => expect(fetchClosedPullRequests).toHaveBeenCalledWith('empty'))
  expect(screen.getByRole('button', { name: 'Closed' })).toHaveAttribute('aria-pressed', 'true')
})
