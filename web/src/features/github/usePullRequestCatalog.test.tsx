import { act, fireEvent, render, renderHook, screen, waitFor, within } from '@testing-library/react'
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
  expect(screen.getByRole('button', { name: /^Open/ })).toHaveAttribute('aria-pressed', 'true')
  expect(fetchClosedPullRequests).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: /^Closed/ }))
  await waitFor(() => expect(fetchClosedPullRequests).toHaveBeenCalledWith('empty'))
  expect(screen.getByRole('button', { name: /^Closed/ })).toHaveAttribute('aria-pressed', 'true')
})

it('labels an open list that comes from an unconfirmed catalog', () => {
  const open = { ...pullRequests[0], id: 'saved:1', status: 'Open' as const }
  useBonsaiStore.setState({ activeProjectId: 'saved', pullRequests: [open], projects: [], syncFreshness: { saved: { provider: { state: 'stale', source: 'disk' } } } })
  const view = render(<PullRequestsPage />)
  const list = within(screen.getByRole('region', { name: 'Pull requests' }))
  // The restored rows stay visible with a status, never as a ready list.
  expect(list.getByRole('status')).toHaveTextContent('Saved from the last session; refreshing…')
  expect(screen.getByRole('option', { name: new RegExp(`#${open.number}`) })).toBeInTheDocument()
  act(() => useBonsaiStore.setState({ syncFreshness: { saved: { provider: { state: 'ready' } } } }))
  expect(view.queryByText(/Saved from the last session/)).toBeNull()
  // Stale without an error used to read as ready; it is labeled now.
  act(() => useBonsaiStore.setState({ syncFreshness: { saved: { provider: { state: 'stale' } } } }))
  expect(list.getByRole('status')).toHaveTextContent('Not confirmed recently; refreshing…')
})
