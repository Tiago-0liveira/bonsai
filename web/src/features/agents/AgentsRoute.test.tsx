import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { RouterProvider, createMemoryHistory, createRootRoute, createRoute, createRouter } from '@tanstack/react-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { agentProviders, type AgentAccount, type AgentCapability } from '../../api/agents'
import { agentAccountsIfChanged, agentUsage } from '../../api/agentsOverview'
import { useBonsaiStore } from '../../stores/bonsai'
import { AgentsRoute } from './AgentsRoute'
import { validateAgentsSearch } from './agentsSearch'

vi.mock('../../api/agents', async importOriginal => ({ ...await importOriginal<object>(), agentProviders: vi.fn() }))
vi.mock('../../api/agentsOverview', async importOriginal => ({ ...await importOriginal<object>(), agentAccountsIfChanged: vi.fn(), agentUsage: vi.fn() }))

const providers: AgentCapability[] = [{ id: 'codex', label: 'Codex', available: true, version: '0.162.1' }]
const accounts: AgentAccount[] = [
  { id: 'codex-personal', provider: 'codex', name: 'Personal', auth_status: 'logged_in' },
  { id: 'codex-team', provider: 'codex', name: 'Team', auth_status: 'logged_out' },
]

function renderAt(url: string) {
  const root = createRootRoute()
  const agents = createRoute({ getParentRoute: () => root, path: '/agents', component: AgentsRoute, validateSearch: validateAgentsSearch })
  const canvas = createRoute({ getParentRoute: () => root, path: '/', component: () => <div>canvas</div> })
  const router = createRouter({ routeTree: root.addChildren([agents, canvas]), history: createMemoryHistory({ initialEntries: [url] }) })
  render(<RouterProvider router={router} />)
  return router
}

beforeEach(() => {
  window.scrollTo = vi.fn()
  useBonsaiStore.setState({ ...useBonsaiStore.getInitialState(), agents: [], projects: [], worktrees: [] }, true)
  vi.mocked(agentProviders).mockResolvedValue(providers)
  vi.mocked(agentAccountsIfChanged).mockResolvedValue({ accounts })
  vi.mocked(agentUsage).mockResolvedValue([])
})
afterEach(() => { cleanup(); vi.clearAllMocks() })

it('restores the profile and tab from the URL and writes both back', async () => {
  const router = renderAt('/agents?profile=codex-team&tab=usage')
  expect(await screen.findByRole('heading', { level: 2, name: 'Team' })).toBeInTheDocument()
  expect(screen.getByRole('tab', { name: 'Usage' })).toHaveAttribute('data-state', 'active')
  fireEvent.click(screen.getByRole('button', { name: /Personal/ }))
  await waitFor(() => expect(router.state.location.search).toEqual({ profile: 'codex-personal', tab: 'usage' }))
  fireEvent.mouseDown(screen.getByRole('tab', { name: 'Sessions' }), { button: 0 })
  await waitFor(() => expect(router.state.location.search).toEqual({ profile: 'codex-personal', tab: 'sessions' }))
  expect(screen.getByRole('tab', { name: /Sessions/ })).toHaveAttribute('data-state', 'active')
})

it('ignores an unknown tab and an unknown profile', async () => {
  renderAt('/agents?profile=gone&tab=bogus')
  expect(await screen.findByRole('heading', { level: 2, name: 'Personal' })).toBeInTheDocument()
  expect(screen.getByRole('tab', { name: 'Overview' })).toHaveAttribute('data-state', 'active')
})

it('shows a session on the canvas by navigating to the root', async () => {
  useBonsaiStore.setState({
    projects: [{ id: 'bonsai', workspaceId: 'personal', name: 'bonsai' } as never],
    agents: [{ id: 's1', name: 'Session', provider: 'Codex', providerId: 'codex', accountId: 'codex-personal', projectId: 'bonsai', worktreeId: 'wt', lifecycleState: 'running', state: 'running', createdAt: new Date().toISOString(), terminalId: 's1', model: '', reasoningEffort: '', workType: '', prompt: '', archived: false, presentation: 'canvas', task: 'running', runtime: '' }],
    activeProjectId: 'bonsai',
  })
  const router = renderAt('/agents?profile=codex-personal&tab=sessions')
  fireEvent.click(await screen.findByRole('button', { name: 'Show Session on canvas' }))
  await waitFor(() => expect(router.state.location.pathname).toBe('/'))
})

it('opens Sign in again from a link once and drops the request from the URL', async () => {
  const router = renderAt('/agents?profile=codex-team&signin=true')
  expect(await screen.findByRole('dialog', { name: 'Sign in to Team again' })).toBeInTheDocument()
  await waitFor(() => expect(router.state.location.search).toEqual({ profile: 'codex-team' }))
  expect(validateAgentsSearch({ signin: 'true' })).toEqual({ signin: true })
  expect(validateAgentsSearch({ signin: 'yes' })).toEqual({})
})
