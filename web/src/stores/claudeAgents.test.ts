import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { startAgent, stopAgent, type AgentSummary } from '../api/agents'
import { agents } from '../mock/agents'
import { worktrees } from '../test/fixtures/worktrees'
import { projects } from '../test/fixtures/projects'
import { useBonsaiStore } from './bonsai'
import { mergeWorkspacePreferences } from './workspacePersistence'

vi.mock('../api/agents', async importOriginal => ({ ...await importOriginal<object>(), startAgent: vi.fn(), stopAgent: vi.fn() }))

const session = (provider: AgentSummary['provider'], id: string): AgentSummary => ({ id, project_id: worktrees[0].projectId, worktree_id: worktrees[0].id, account_id: 'account', provider, profile_name: 'Work', name: 'Work', state: 'running', created_at: 'now' })
const input = { worktreeId: worktrees[0].id, accountId: 'account', name: 'n', model: 'opus', reasoningEffort: '', fastMode: false, workType: 'Implementation', prompt: 'hi' }

beforeEach(() => {
  useBonsaiStore.setState({ ...useBonsaiStore.getInitialState(), projects, worktrees, agents: [], activeProjectId: worktrees[0].projectId }, true)
  vi.mocked(stopAgent).mockResolvedValue(session('claude', 'x'))
})
afterEach(() => vi.clearAllMocks())

describe('Claude agents in the store', () => {
  it('sends permission mode and effort for Claude and never full access', async () => {
    vi.mocked(startAgent).mockResolvedValue(session('claude', 'c1'))
    await useBonsaiStore.getState().createAgent({ ...input, provider: 'Claude', permissionMode: 'plan', effort: 'high', fullAccess: true })
    const body = vi.mocked(startAgent).mock.calls[0][1]
    expect(body).toMatchObject({ account_id: 'account', model: 'opus', permission_mode: 'plan', effort: 'high' })
    expect(body).not.toHaveProperty('full_access')
    expect(useBonsaiStore.getState().agents[0]).toMatchObject({ id: 'c1', provider: 'Claude', providerId: 'claude' })
  })

  it('omits unset Claude options and sends full access only for Antigravity', async () => {
    vi.mocked(startAgent).mockResolvedValueOnce(session('claude', 'c1')).mockResolvedValueOnce(session('antigravity', 'a1'))
    await useBonsaiStore.getState().createAgent({ ...input, provider: 'Claude', permissionMode: '', effort: '' })
    await useBonsaiStore.getState().createAgent({ ...input, provider: 'Antigravity', fullAccess: true, permissionMode: 'plan' })
    const [claude, antigravity] = vi.mocked(startAgent).mock.calls.map(call => call[1])
    expect(claude).not.toHaveProperty('permission_mode')
    expect(claude).not.toHaveProperty('effort')
    expect(antigravity).toMatchObject({ full_access: true })
    expect(antigravity).not.toHaveProperty('permission_mode')
  })

  it('starts two sessions of one profile and keeps both', async () => {
    vi.mocked(startAgent).mockResolvedValueOnce(session('claude', 'c1')).mockResolvedValueOnce(session('claude', 'c2'))
    await useBonsaiStore.getState().createAgent({ ...input, provider: 'Claude' })
    await useBonsaiStore.getState().createAgent({ ...input, provider: 'Claude' })
    expect(useBonsaiStore.getState().agents.map(agent => agent.id)).toEqual(['c1', 'c2'])
    expect(useBonsaiStore.getState().worktrees.find(w => w.id === worktrees[0].id)?.agentIds).toEqual(expect.arrayContaining(['c1', 'c2']))
  })

  it('rejects providers the API cannot launch with a provider neutral message', async () => {
    await expect(useBonsaiStore.getState().createAgent({ ...input, provider: 'Codex' })).rejects.toThrow('Select a profile and worktree.')
    expect(startAgent).not.toHaveBeenCalled()
  })

  it('stops and opens the terminal of a Claude agent but not of a simulated one', () => {
    const claude = { ...agents[0], id: 'c1', projectId: worktrees[0].projectId, worktreeId: worktrees[0].id, providerId: 'claude' as const }
    useBonsaiStore.setState({ agents: [claude, { ...agents[1], id: 'mock' }] })
    useBonsaiStore.getState().setAgentState('c1', 'finished')
    expect(stopAgent).toHaveBeenCalledWith(worktrees[0].projectId, 'c1', 'stop-c1')
    useBonsaiStore.getState().openTerminal('c1')
    expect(useBonsaiStore.getState().openRuntimeIds).toContain('c1')
    useBonsaiStore.getState().setAgentState('mock', 'finished')
    expect(useBonsaiStore.getState().notice).toBe('This agent action is unavailable.')
  })

  it('keeps live Claude agents and drops simulated ones when merging persisted preferences', () => {
    const claude = { ...agents[0], id: 'c1', worktreeId: 'tree', providerId: 'claude' as const }
    const current = { ...useBonsaiStore.getInitialState(), agents: [claude, { ...agents[1], id: 'mock', worktreeId: 'tree' }] }
    expect(mergeWorkspacePreferences({}, current).agents.map(agent => agent.id)).toEqual(['c1'])
  })
})
