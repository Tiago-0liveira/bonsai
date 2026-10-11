import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { startAgent, stopAgent, type AgentSummary } from '../api/agents'
import { agents } from '../mock/agents'
import { worktrees } from '../test/fixtures/worktrees'
import { projects } from '../test/fixtures/projects'
import { useBonsaiStore } from './bonsai'
import { mergeWorkspacePreferences } from './workspacePersistence'

vi.mock('../api/agents', async importOriginal => ({ ...await importOriginal<object>(), startAgent: vi.fn(), stopAgent: vi.fn() }))

const session = (id: string, account = 'account'): AgentSummary => ({ id, project_id: worktrees[0].projectId, worktree_id: worktrees[0].id, account_id: account, provider: 'codex', profile_name: 'Personal', name: 'Personal', state: 'running', created_at: 'now' })
const input = { worktreeId: worktrees[0].id, accountId: 'account', name: 'n', model: 'gpt-6.1-sol', reasoningEffort: '', fastMode: false, workType: 'Implementation', prompt: 'hi' }

beforeEach(() => {
  useBonsaiStore.setState({ ...useBonsaiStore.getInitialState(), projects, worktrees, agents: [], activeProjectId: worktrees[0].projectId }, true)
  vi.mocked(stopAgent).mockResolvedValue(session('x'))
})
afterEach(() => vi.clearAllMocks())

describe('Codex agents in the store', () => {
  it('sends sandbox, approval, effort and model as given', async () => {
    vi.mocked(startAgent).mockResolvedValueOnce(session('x1'))
    await useBonsaiStore.getState().createAgent({ ...input, provider: 'Codex', options: { sandbox: 'workspace-write', approval: 'on-request', effort: 'high' } })
    expect(startAgent).toHaveBeenCalledWith(worktrees[0].projectId, expect.objectContaining({ account_id: 'account', model: 'gpt-6.1-sol', options: { sandbox: 'workspace-write', approval: 'on-request', effort: 'high' } }), expect.any(String))
    expect(useBonsaiStore.getState().agents[0]).toMatchObject({ id: 'x1', provider: 'Codex', providerId: 'codex', profileName: 'Personal' })
  })

  it('keeps two sessions of one profile and a session of another', async () => {
    vi.mocked(startAgent).mockResolvedValueOnce(session('x1')).mockResolvedValueOnce(session('x2')).mockResolvedValueOnce(session('x3', 'other'))
    await useBonsaiStore.getState().createAgent({ ...input, provider: 'Codex' })
    await useBonsaiStore.getState().createAgent({ ...input, provider: 'Codex' })
    await useBonsaiStore.getState().createAgent({ ...input, provider: 'Codex', accountId: 'other' })
    expect(useBonsaiStore.getState().agents.map(agent => agent.id)).toEqual(['x1', 'x2', 'x3'])
    expect(useBonsaiStore.getState().worktrees.find(w => w.id === worktrees[0].id)?.agentIds).toEqual(expect.arrayContaining(['x1', 'x2', 'x3']))
  })

  it('stops and opens the terminal of a Codex agent', () => {
    const codex = { ...agents[0], id: 'x1', projectId: worktrees[0].projectId, worktreeId: worktrees[0].id, provider: 'Codex' as const, providerId: 'codex' as const }
    useBonsaiStore.setState({ agents: [codex] })
    useBonsaiStore.getState().setAgentState('x1', 'finished')
    expect(stopAgent).toHaveBeenCalledWith(worktrees[0].projectId, 'x1', 'stop-x1')
    useBonsaiStore.getState().openTerminal('x1')
    expect(useBonsaiStore.getState().openRuntimeIds).toContain('x1')
  })

  it('keeps live Codex agents across a reload and drops simulated Codex ones', () => {
    const codex = { ...agents[0], id: 'x1', worktreeId: 'tree', provider: 'Codex' as const, providerId: 'codex' as const }
    const simulated = { ...agents[0], id: 'mock', worktreeId: 'tree', provider: 'Codex' as const }
    const current = { ...useBonsaiStore.getInitialState(), agents: [codex, simulated] }
    expect(mergeWorkspacePreferences({}, current).agents.map(agent => agent.id)).toEqual(['x1'])
  })
})
