import { beforeEach, expect, it, vi } from 'vitest'
import { useBonsaiStore } from './bonsai'
import { applyProcessSummary, removeProcess } from '../api/processes'
import { applySnapshot, __resetGitSyncForTests, type ProcessSummary, type Snapshot } from '../api/git'
import { localFetch } from '../api/localClient'
import { visibleProcesses } from './processProjection'
import { projectCanvasProcessesSelector } from './projectSelectors'
import { sanitizeWorkspacePreferences, mergeWorkspacePreferences } from './workspacePersistence'
vi.mock('../api/localClient', () => ({ localFetch: vi.fn(), LOCAL_API_HTTP: 'http://localhost' }))
const store = () => useBonsaiStore.getState()
const summary = (id: number, key = 'command-a', order = id): ProcessSummary => ({ id: `repo:${id}`, project_id: 'repo', worktree_id: 'tree', daemon_id: id, command_key: key, execution_order: order, revision: 1, command: 'same display', label: 'command', status: 'running' })
let sequence = 0
const snapshot = (processes: ProcessSummary[], visibility = { cutoffs: {}, deleted: {} }): Snapshot => ({ repository: { id: 'repo', workspace_id: 'workspace', full_name: 'o/repo', default_branch: 'main' }, sequence: ++sequence, online: true, metadata: {}, processes, process_visibility: visibility, freshness: { processes: { state: 'ready' } } })
beforeEach(() => {
  vi.clearAllMocks(); __resetGitSyncForTests(); sequence = 0
  useBonsaiStore.setState({ ...useBonsaiStore.getInitialState(), activeProjectId: 'repo', projects: [{ id: 'repo', workspaceId: 'workspace', name: 'repo', repository: 'o/repo', description: '', health: 'idle', defaultBranch: 'main', worktreeIds: ['tree'], openPrCount: 0 }], dockWorktreeId: 'tree' }, true)
})
it('projects the latest failed execution, separates opaque invocation keys and preserves all active inventory', () => {
  applyProcessSummary(summary(1)); applyProcessSummary({ ...summary(2), status: 'failed' }); applyProcessSummary(summary(3, 'other-dir-or-args')); applyProcessSummary({ ...summary(4, 'other-worktree'), worktree_id: 'elsewhere' })
  expect(visibleProcesses(store(), 'repo').map(p => p.id)).toEqual(['repo:2', 'repo:3', 'repo:4'])
  expect(store().processes).toHaveLength(4)
  applyProcessSummary({ ...summary(1, 'command-a', 5), revision: 2 })
  expect(visibleProcesses(store(), 'repo')[0].id).toBe('repo:1')
  applyProcessSummary({ ...summary(2), attempt: 8, revision: 3, started_at: '2099-01-01' })
  expect(visibleProcesses(store(), 'repo')[0].id).toBe('repo:1')
})
it('reuses an open terminal slot, selection, reveal and placement while keeping closed commands closed', () => {
  applyProcessSummary(summary(1), true); applyProcessSummary(summary(3, 'other'), true)
  store().setManualNodePlacement('repo:1', { x: 120, y: 90 }); store().focusRuntime('repo:1')
  applyProcessSummary(summary(2), true)
  expect(store().openRuntimeIds).toEqual(['repo:2', 'repo:3'])
  expect(store().nodePlacements['repo:2']).toEqual({ x: 120, y: 90, mode: 'manual' })
  expect(store().nodePlacements['repo:1']).toBeUndefined()
  expect(store().selection).toEqual({ type: 'process', id: 'repo:2' })
  expect(store().canvasReveal.nodeId).toBe('repo:2')
  store().closeRuntime('repo:2')
  applySnapshot(snapshot([summary(1), summary(2), summary(4), summary(3, 'other')]))
  expect(store().openRuntimeIds).toEqual(['repo:3'])
  expect(visibleProcesses(store(), 'repo')[0].id).toBe('repo:4')
})
it('remaps open snapshot arrivals without appending tabs and isolates PID traffic from canvas and preferences', () => {
  applyProcessSummary(summary(1), true)
  applySnapshot(snapshot([summary(1), summary(2)]))
  expect(store().openRuntimeIds).toEqual(['repo:2'])
  const select = projectCanvasProcessesSelector('repo'), graph = select(store())
  const prefs = store().terminalViewPreferences
  applyProcessSummary({ ...summary(2), pid: 1234, revision: 2 })
  expect(select(store())).toBe(graph); expect(store().terminalViewPreferences).toBe(prefs)
})
it('deletes confirmed runs, rejects late summaries and snapshots, and hides older runs until a new explicit execution', async () => {
  applyProcessSummary(summary(1)); applyProcessSummary(summary(2), true)
  vi.mocked(localFetch).mockResolvedValue({ ok: true, json: async () => ({ id: 'repo:2', process_visibility: { cutoffs: { 'command-a': 2 }, deleted: { 2: true } } }) } as Response)
  await removeProcess('repo', 2, true)
  expect(store().openRuntimeIds).toEqual([]); expect(store().selection.type).toBe('project')
  expect(visibleProcesses(store(), 'repo')).toEqual([])
  applyProcessSummary({ ...summary(2), revision: 99 }, true)
  applySnapshot(snapshot([summary(1), summary(2)]))
  expect(store().processes.map(p => p.id)).toEqual(['repo:1']); expect(visibleProcesses(store(), 'repo')).toEqual([])
  applyProcessSummary(summary(3), true)
  expect(visibleProcesses(store(), 'repo').map(p => p.id)).toEqual(['repo:3'])
})
it('retains retry controls on deletion failure and does not disturb a project switched during deletion', async () => {
  applyProcessSummary(summary(1), true)
  vi.mocked(localFetch).mockResolvedValueOnce({ ok: false, status: 400, json: async () => ({ error: { message: 'cleanup failed' } }) } as Response)
  await expect(removeProcess('repo', 1, true)).rejects.toThrow('cleanup failed')
  expect(store().processes).toHaveLength(1); expect(store().openRuntimeIds).toEqual(['repo:1'])
  let finish!: (r: Response) => void
  vi.mocked(localFetch).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
  const removing = removeProcess('repo', 1, true)
  useBonsaiStore.setState({ activeProjectId: 'other', selection: { type: 'project', id: 'other' }, dockWorktreeId: 'elsewhere', openRuntimeIds: [], dockRuntimeId: '' })
  finish({ ok: true, json: async () => ({ id: 'repo:1', process_visibility: { cutoffs: { 'command-a': 1 }, deleted: { 1: true } } }) } as Response)
  await removing
  expect(store().activeProjectId).toBe('other'); expect(store().selection.id).toBe('other')
})
it('keeps terminals closed by default across reload and project visits, and restores only saved commands when enabled', () => {
  applyProcessSummary(summary(1), true)
  const persisted = sanitizeWorkspacePreferences(store())
  expect(persisted.terminalViewPreferences?.repo.reopening).toBe('keep_closed')
  useBonsaiStore.setState(mergeWorkspacePreferences(persisted, store()), true)
  applySnapshot(snapshot([summary(1), summary(2), summary(3, 'unsaved')]))
  expect(store().openRuntimeIds).toEqual([])
  store().openRuntime('repo:2'); expect(store().openRuntimeIds).toEqual(['repo:2'])
  useBonsaiStore.setState({ activeProjectId: 'other' }); store().setActiveProject('repo')
  expect(store().openRuntimeIds).toEqual([])
  useBonsaiStore.setState({ terminalViewPreferences: { repo: { ...store().terminalViewPreferences.repo, reopening: 'restore' } } })
  useBonsaiStore.setState({ activeProjectId: 'other' }); store().setActiveProject('repo')
  expect(store().openRuntimeIds).toEqual(['repo:2'])
  const text = JSON.stringify(sanitizeWorkspacePreferences(store()))
  expect(text).toContain('command-a'); expect(text).not.toContain('same display'); expect(text).not.toContain('processVisibility')
})

it('keeps late launch responses below the deletion cutoff closed while retaining live inventory', () => {
  useBonsaiStore.setState({ processVisibility: { repo: { cutoffs: { 'command-a': 5 }, deleted: { 5: true } } } })
  applyProcessSummary(summary(4), true)
  expect(store().processes).toHaveLength(1)
  expect(store().openRuntimeIds).toEqual([])
  expect(store().selection.type).not.toBe('process')
})
it('uses deterministic equal order ties and distinct ownership keys for unresolved worktrees', () => {
  applyProcessSummary({ ...summary(1, 'owner-a', 10), worktree_id: '' })
  applyProcessSummary({ ...summary(2, 'owner-a', 10), worktree_id: '' })
  applyProcessSummary({ ...summary(3, 'owner-b', 10), worktree_id: '' })
  expect(visibleProcesses(store(), 'repo').map(p => p.id)).toEqual(['repo:2', 'repo:3'])
})
it('defers restoration and pruning until visibility metadata accompanies authoritative inventory', () => {
  const ref = { kind: 'process' as const, id: 'repo:old', commandKey: 'command-a' }
  useBonsaiStore.setState({ terminalViewPreferences: { repo: { reopening: 'restore', lastWorktreeId: 'tree', worktrees: { tree: { open: [ref], active: ref } } } } })
  applySnapshot({ ...snapshot([summary(2)]), process_visibility: undefined })
  expect(store().terminalViewPreferences.repo.worktrees.tree.open).toEqual([ref])
  expect(store().openRuntimeIds).toEqual([])
  applySnapshot(snapshot([summary(2)]))
  expect(store().openRuntimeIds).toEqual(['repo:2'])
})
