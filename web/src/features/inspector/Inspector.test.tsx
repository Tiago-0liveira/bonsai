import { Profiler } from 'react'
import { act, cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { cachedAgentProviders } from '../../api/launchDetails'
import { Inspector } from './Inspector'
import { useBonsaiStore } from '../../stores/bonsai'
import { projects } from '../../test/fixtures/projects'
import { worktrees } from '../../test/fixtures/worktrees'
import { pullRequests } from '../../test/fixtures/pullRequests'
import { agents } from '../../mock/agents'

vi.mock('../../api/launchDetails', async importOriginal => ({ ...await importOriginal<object>(), cachedAgentProviders: vi.fn() }))
beforeEach(() => { vi.mocked(cachedAgentProviders).mockResolvedValue([]) })
afterEach(cleanup)

it('shows the launch details of a live session labelled by its provider\'s launch fields', async () => {
  vi.mocked(cachedAgentProviders).mockResolvedValue([{ id: 'codex', label: 'Codex', available: true, launch_fields: [
    { id: 'permissions', label: 'Permissions', kind: 'select', options: [{ value: 'read-only', label: 'Read only', set: { sandbox: 'read-only', approval: 'on-request' } }] },
    { id: 'effort', label: 'Effort', kind: 'select', per_model: true, options: [{ value: '', label: 'Profile default' }, { value: 'high', label: 'high' }] },
  ] }])
  const live = { ...agents[0], id: 'codex-live', provider: 'Codex' as const, providerId: 'codex' as const, profileName: 'Personal', model: 'gpt-6.1-sol', reasoningEffort: 'high', launchOptions: { effort: 'high', sandbox: 'read-only', approval: 'on-request' }, projectId: 'bonsai', worktreeId: 'wt-web' }
  useBonsaiStore.setState({ ...useBonsaiStore.getInitialState(), projects, worktrees, agents: [live], activeProjectId: 'bonsai', selection: { type: 'agent', id: live.id } }, true)
  render(<Inspector />)
  expect((await screen.findByText('Launch')).nextElementSibling).toHaveTextContent('gpt-6.1-sol · Read only · high effort')
  cleanup()
  // A session launched without choices has no launch row; nor does a simulated agent.
  useBonsaiStore.setState({ agents: [{ ...live, model: '', launchOptions: undefined }] })
  render(<Inspector />)
  expect(screen.queryByText('Launch')).toBeNull()
  cleanup()
  useBonsaiStore.setState({ agents: [{ ...live, providerId: undefined }] })
  render(<Inspector />)
  expect(screen.queryByText('Launch')).toBeNull()
  expect(screen.getByText('Reasoning')).toBeInTheDocument()
})

it.each(['Claude', 'Codex', 'Antigravity', 'Gemini'] as const)('shows the %s mark on the agent card and worktree agent row', (provider) => {
  const agent = { ...agents[0], id: 'provider-agent', provider, projectId: 'bonsai', worktreeId: 'wt-web' }
  useBonsaiStore.setState({ ...useBonsaiStore.getInitialState(), projects, worktrees, agents: [agent],
    activeProjectId: 'bonsai', selection: { type: 'agent', id: agent.id } }, true)
  const { container } = render(<Inspector />)
  expect(container.querySelector('.inspector-hero svg')).toHaveAttribute('data-provider-logo', provider.toLowerCase())
  expect(container.querySelector('.inspector-hero .lucide-bot')).toBeNull()
  act(() => useBonsaiStore.setState({ selection: { type: 'worktree', id: 'wt-web' } }))
  const row = screen.getByRole('button', { name: new RegExp(agent.name) })
  expect(row.querySelector('svg')).toHaveAttribute('data-provider-logo', provider.toLowerCase())
})

it('enables stop and the terminal for a live Codex session and keeps them off for a simulated one', () => {
  const live = { ...agents[0], id: 'codex-live', provider: 'Codex' as const, providerId: 'codex' as const, profileName: 'Personal', state: 'running' as const, projectId: 'bonsai', worktreeId: 'wt-web' }
  useBonsaiStore.setState({ ...useBonsaiStore.getInitialState(), projects, worktrees, agents: [live], activeProjectId: 'bonsai', selection: { type: 'agent', id: live.id } }, true)
  render(<Inspector />)
  expect(screen.getByRole('button', { name: 'Open terminal' })).toBeEnabled()
  expect(screen.getByRole('button', { name: 'Stop agent' })).toBeEnabled()
  expect(screen.getByText('Personal')).toBeInTheDocument()
  cleanup()
  const simulated = { ...live, id: 'codex-mock', providerId: undefined }
  useBonsaiStore.setState({ agents: [simulated], selection: { type: 'agent', id: simulated.id } })
  render(<Inspector />)
  expect(screen.getByRole('button', { name: 'Open terminal' })).toBeDisabled()
  expect(screen.getByRole('button', { name: 'Stop agent' })).toBeDisabled()
})

it('ignores unrelated group preferences and terminal output while reacting to the selected group', () => {
  useBonsaiStore.setState({ projects, worktrees, agents: [], pullRequests: [], activeProjectId: 'bonsai',
    selection: { type: 'worktree', id: 'wt-web' }, expandedAutomaticGroups: [], worktreeGroups: { bonsai: [{ id: 'unlinked:bonsai', kind: 'unlinked', worktree_ids: ['wt-web', 'wt-docs'] }] }, terminalOutput: {} })
  const commits = vi.fn()
  render(<Profiler id="inspector" onRender={commits}><Inspector /></Profiler>)
  const baseline = commits.mock.calls.length
  act(() => useBonsaiStore.setState({ expandedAutomaticGroups: ['other-group'], terminalOutput: { other: ['new output'] } }))
  expect(commits).toHaveBeenCalledTimes(baseline)
  act(() => useBonsaiStore.setState({ expandedAutomaticGroups: ['other-group', 'unlinked:bonsai'] }))
  expect(commits.mock.calls.length).toBeGreaterThan(baseline)
  expect(screen.getByRole('complementary', { name: 'Inspector' })).toBeInTheDocument()
})

it('inspects retained process failures and their worktree without opening a terminal on render', () => {
  useBonsaiStore.setState({ ...useBonsaiStore.getInitialState(), projects, worktrees, activeProjectId: 'bonsai',
    selection: { type: 'process', id: 'bonsai:1' }, processes: [{ id: 'bonsai:1', projectId: 'bonsai', daemonId: 1, worktreeId: 'wt-web', name: 'Broken server', command: 'pnpm run server', status: 'error', lifecycleStatus: 'failed', exitCode: 2, exitError: 'Port in use', retryCount: 3, attempt: 4 }] }, true)
  render(<Inspector />)
  const inspector = screen.getByRole('complementary', { name: 'Inspector' })
  expect(inspector).toHaveTextContent('Broken server')
  expect(inspector).toHaveTextContent('pnpm run server')
  expect(screen.getByRole('alert')).toHaveTextContent('Port in use')
  expect(screen.getByRole('button', { name: 'Stop' })).toBeDisabled()
  expect(screen.getByRole('button', { name: 'Restart' })).toBeEnabled()
  expect(useBonsaiStore.getState().openRuntimeIds).toEqual([])
})

it.each([
  [true, 'no conflicts'],
  [false, 'conflicts'],
  [undefined, 'checking…'],
])('shows PR mergeability %s as "%s"', (mergeable, text) => {
  const pr = { ...pullRequests[0], id: 'bonsai:24', mergeable }
  useBonsaiStore.setState({ ...useBonsaiStore.getInitialState(), projects, worktrees: [worktrees[1]], activeProjectId: 'bonsai', pullRequests: [pr],
    selection: { type: 'worktree', id: worktrees[1].id } }, true)
  render(<Inspector />)
  expect(screen.getByRole('button', { name: /Pull request/ })).toHaveTextContent(new RegExp(`${text}$`))
  expect(screen.queryByText('Resolve merge conflicts') === null).toBe(mergeable !== false)
})

it.each(['claude', 'antigravity'] as const)('enables terminal and stop for a live %s agent and not for a simulated one', (providerId) => {
  const agent = { ...agents[0], id: 'live', projectId: 'bonsai', worktreeId: 'wt-web', presentation: 'canvas' as const, state: 'running' as const, providerId, profileName: 'Work' }
  const setup = (value: typeof agent | Omit<typeof agent, 'providerId'>) => {
    useBonsaiStore.setState({ ...useBonsaiStore.getInitialState(), projects, worktrees, agents: [value as typeof agent], activeProjectId: 'bonsai', selection: { type: 'agent', id: 'live' } }, true)
    return render(<Inspector />)
  }
  const { unmount } = setup(agent)
  expect(screen.getByRole('button', { name: 'Open terminal' })).toBeEnabled()
  expect(screen.getByRole('button', { name: 'Stop agent' })).toBeEnabled()
  expect(screen.getByText('API session')).toBeInTheDocument()
  unmount()
  setup({ ...agent, providerId: undefined })
  expect(screen.getByRole('button', { name: 'Open terminal' })).toBeDisabled()
})
