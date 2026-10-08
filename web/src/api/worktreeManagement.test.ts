import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useBonsaiStore } from '../stores/bonsai'
import { __resetGitSyncForTests, applySnapshot, createWorktree, type Snapshot } from './git'

const { localFetch } = vi.hoisted(() => ({ localFetch: vi.fn() }))
vi.mock('./local', () => ({ localFetch, invalidateLocalSession: vi.fn(), markLocalConnectionLost: vi.fn(), openLocalEvents: vi.fn() }))
const repository = { id: 'repo', workspace_id: 'workspace', full_name: 'owner/repo', default_branch: 'main' }
const snapshot = (sequence: number, present = true): Snapshot => ({ repository, sequence, online: true, metadata: {}, local: { branches: [], groups: [], worktrees: present ? [{ id: 'created', repository_id: 'repo', branch: 'feature', main: false, path: '/trees/feature', local_head_sha: 'sha' }] : [] } })
const response = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } })
const input = { projectId: 'repo', sourceType: 'origin' as const, sourceRef: 'origin/feature', mergeTargetBranch: 'main' }

describe('worktree mutation reconciliation', () => {
  beforeEach(() => {
    __resetGitSyncForTests(); localFetch.mockReset()
    const projects = ['repo', 'other'].map(id => ({ id, workspaceId: 'workspace', name: id, repository: 'owner/repo', description: '', health: 'idle' as const, defaultBranch: 'main', worktreeIds: [], openPrCount: 0 }))
    useBonsaiStore.setState({ projects, activeProjectId: 'repo', selection: { type: 'project', id: 'repo' }, worktrees: [], agents: [], processes: [], pullRequests: [], gitBranches: {}, branchCandidates: {}, worktreeGroups: {}, repositorySync: {}, syncFreshness: {}, nodePlacements: {}, openRuntimeIds: [] })
  })

  it('retries metadata after successful Git creation without creating another worktree', async () => {
    localFetch.mockResolvedValueOnce(response({ result: { id: 'created' } })).mockResolvedValueOnce(response({ error: { code: 'metadata_write_failed', message: 'disk full' } }, 500))
    await expect(createWorktree(input, { idempotencyKey: 'create-key' })).rejects.toMatchObject({ worktreeId: 'created' })
    localFetch.mockResolvedValueOnce(response({})).mockResolvedValueOnce(response(snapshot(1)))
    await expect(createWorktree(input, { worktreeId: 'created' })).resolves.toBe('created')
    expect(localFetch.mock.calls.filter(([path]) => path.endsWith('/worktrees'))).toHaveLength(1)
    expect(localFetch.mock.calls[0][1].headers['Idempotency-Key']).toBe('create-key')
    expect(useBonsaiStore.getState().selection).toEqual({ type: 'worktree', id: 'created' })
  })

  it('captures project identity and keeps a project switch during creation', async () => {
    let resolve!: (value: Response) => void
    localFetch.mockImplementationOnce(() => new Promise<Response>(done => { resolve = done })).mockResolvedValueOnce(response({})).mockResolvedValueOnce(response(snapshot(1)))
    const pending = createWorktree(input)
    useBonsaiStore.getState().setActiveProject('other')
    resolve(response({ result: { id: 'created' } }))
    await pending
    expect(localFetch.mock.calls[0][0]).toBe('/api/projects/repo/worktrees')
    expect(useBonsaiStore.getState().selection).toEqual({ type: 'project', id: 'other' })
    expect(useBonsaiStore.getState().activeProjectId).toBe('other')
  })

  it('reconciles deleted agents, runtime selection and placements through canonical state', () => {
    applySnapshot(snapshot(1))
    const agent = { id: 'agent', worktreeId: 'created', name: 'worker', provider: 'Codex' as const, model: '', reasoningEffort: '', workType: '', prompt: '', archived: false, presentation: 'canvas' as const, state: 'finished' as const, task: '', runtime: '', terminalId: 'terminal', createdAt: '' }
    useBonsaiStore.setState({ agents: [agent], selection: { type: 'agent', id: 'agent' }, dockRuntimeId: 'agent', openRuntimeIds: ['agent'], nodePlacements: { created: { x: 1, y: 2, mode: 'manual' }, agent: { x: 3, y: 4, mode: 'manual' }, unrelated: { x: 5, y: 6, mode: 'manual' } } })
    applySnapshot(snapshot(2, false))
    const state = useBonsaiStore.getState()
    expect(state.agents).toEqual([])
    expect(state.selection).toEqual({ type: 'project', id: 'repo' })
    expect(state.openRuntimeIds).toEqual([])
    expect(state.dockRuntimeId).toBe('')
    expect(state.nodePlacements).toEqual({ unrelated: { x: 5, y: 6, mode: 'manual' } })
  })

  it('applies sync completion timestamps without revising or replacing the canvas data', () => {
    const first = { ...snapshot(1), sync: { fetch: { state: 'ready' as const, completed_at: '2026-01-01T00:00:00Z' }, pull: { state: 'skipped' as const, reason: 'dirty_worktree' } } }
    applySnapshot(first)
    const before = useBonsaiStore.getState()
    applySnapshot({ ...first, sequence: 2, sync: { ...first.sync, fetch: { state: 'ready', completed_at: '2026-01-02T00:00:00Z' } } })
    const after = useBonsaiStore.getState()
    expect(after.repositorySync.repo.fetch.completed_at).toBe('2026-01-02T00:00:00Z')
    expect(after.gitRevision).toBe(before.gitRevision)
    expect(after.worktrees).toBe(before.worktrees)
    expect(after.nodePlacements).toBe(before.nodePlacements)
  })
})
