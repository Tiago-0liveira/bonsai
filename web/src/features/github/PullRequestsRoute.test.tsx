import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { RouterProvider, createMemoryHistory, createRootRoute, createRoute, createRouter } from '@tanstack/react-router'
import { useBonsaiStore } from '../../stores/bonsai'
import { stackPr } from '../../test/fixtures/prStacks'
import { PullRequestsRoute } from './PullRequestsRoute'
import { validatePullRequestsSearch } from './pullRequestsSearch'

vi.mock('../../api/git', async importOriginal => ({
  ...await importOriginal<typeof import('../../api/git')>(),
  loadPullRequest: vi.fn(),
  fetchClosedPullRequests: vi.fn(),
  requestProjectRefresh: vi.fn(),
}))

afterEach(cleanup)

function renderAt(url: string) {
  const root = createRootRoute()
  const route = createRoute({ getParentRoute: () => root, path: '/github', component: PullRequestsRoute, validateSearch: validatePullRequestsSearch })
  const router = createRouter({ routeTree: root.addChildren([route]), history: createMemoryHistory({ initialEntries: [url] }) })
  render(<RouterProvider router={router} />)
  return router
}

const seed = () => {
  window.scrollTo = vi.fn()
  const first = stackPr(1, 'feat/one', 'main', { title: 'First change', id: 'p:1' })
  const second = stackPr(2, 'feat/two', 'main', { title: 'Second change', id: 'p:2' })
  useBonsaiStore.setState({
    activeProjectId: 'p', projects: [], pullRequests: [first, second],
    inspectedPullRequestId: null, syncFreshness: { p: { provider: { state: 'ready' } } },
  })
}

it('restores the selection from ?pr= and writes it back when a row is chosen', async () => {
  seed()
  const router = renderAt('/github?pr=p:2')
  await waitFor(() => expect(screen.getByRole('option', { name: /Second change/ })).toHaveAttribute('aria-selected', 'true'))
  fireEvent.click(screen.getByRole('option', { name: /First change/ }))
  await waitFor(() => expect(router.state.location.search).toEqual({ pr: 'p:1' }))
  expect(screen.getByRole('option', { name: /First change/ })).toHaveAttribute('aria-selected', 'true')
})

it('falls back to the first PR when the param is missing or unknown', async () => {
  seed()
  renderAt('/github?pr=nope')
  await waitFor(() => expect(screen.getByRole('option', { name: /First change/ })).toHaveAttribute('aria-selected', 'true'))
})

it('only accepts a non-empty string as the pr param', () => {
  expect(validatePullRequestsSearch({ pr: 'a' })).toEqual({ pr: 'a' })
  expect(validatePullRequestsSearch({ pr: '' })).toEqual({ pr: undefined })
  expect(validatePullRequestsSearch({ pr: 5 })).toEqual({ pr: undefined })
})
