import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useBonsaiStore, workspacePreferences } from './bonsai'
import { applyProcessSummary } from '../api/processes'
import { __resetGitSyncForTests, applySnapshot, reconcileCatalog, type ProcessSummary, type Snapshot } from '../api/git'
import { workspaceStorage, WORKSPACE_STORAGE_KEY, WORKSPACE_STORAGE_VERSION } from './workspacePersistence'
import { projects } from '../test/fixtures/projects'
import { worktrees } from '../test/fixtures/worktrees'
import { agents } from '../mock/agents'
import type { RuntimeReference } from '../types'

const ref = (id: string, kind: RuntimeReference['kind'] = 'process'): RuntimeReference => ({ kind, id })
const summary = (id: number, worktree = 'wt-web', project = 'bonsai'): ProcessSummary => ({
  id: `${project}:${id}`, project_id: project, daemon_id: id, worktree_id: worktree,
  command: 'command with PRIVATE_COMMAND', label: 'process', status: 'running', revision: 1,
})
let sequence = 0
function snapshot(project = 'bonsai', processes: ProcessSummary[] = [], extra: Partial<Snapshot> = {}): Snapshot {
  return {
    repository: { id: project, workspace_id: 'personal', full_name: `owner/${project}`, default_branch: 'main' },
    sequence: ++sequence, online: true, metadata: {},
    local: { worktrees: worktrees.filter(tree => tree.projectId === project).map(tree => ({ id: tree.id, repository_id: project, branch: tree.branch, main: !!tree.main, local_head_sha: 'abc' })), branches: [] },
    processes, agents: [], freshness: { local: { state: 'ready' }, agents: { state: 'ready' }, processes: { state: 'ready' } },
    ...extra,
  }
}
const store = () => useBonsaiStore.getState()
const saved = () => JSON.parse(localStorage.getItem(WORKSPACE_STORAGE_KEY)!).state
const views = (project = 'bonsai', tree = 'wt-web') => store().terminalViewPreferences[project]?.worktrees[tree]
function reload() {
  const disk = localStorage.getItem(WORKSPACE_STORAGE_KEY)!
  useBonsaiStore.setState(useBonsaiStore.getInitialState(), true)
  localStorage.setItem(WORKSPACE_STORAGE_KEY, disk)
  workspacePreferences.rehydrate()
}
beforeEach(() => {
  vi.restoreAllMocks(); workspacePreferences.cancel(); __resetGitSyncForTests(); sequence = 0
  localStorage.clear()
  useBonsaiStore.setState({ ...useBonsaiStore.getInitialState(), projects, worktrees, activeProjectId: 'bonsai', activeWorkspaceId: 'personal', dockWorktreeId: 'wt-web' }, true)
  localStorage.clear()
})
afterEach(() => { workspacePreferences.cancel(); vi.restoreAllMocks() })

describe('scoped runtime actions', () => {
  it('opens, focuses, reorders and closes agents and processes through the same ordered scope', () => {
    const agent = { ...agents[0], id: 'real-agent', projectId: 'bonsai', worktreeId: 'wt-web', terminalId: 'real-terminal', providerId: 'antigravity' as const }
    useBonsaiStore.setState({ agents: [agent] })
    for (const id of [1, 2, 3]) applyProcessSummary(summary(id))
    store().focusRuntime('bonsai:1')
    expect(store().openRuntimeIds).toEqual([])
    store().openRuntime('bonsai:1'); store().openRuntime(agent.id); store().openRuntime('bonsai:3')
    store().reorderOpenRuntime('bonsai:3', 'bonsai:1')
    expect(views()?.open).toEqual([ref('bonsai:3'), ref('bonsai:1'), ref(agent.id, 'agent')])
    store().focusRuntime('bonsai:1'); store().closeRuntime('bonsai:1')
    expect(store()).toMatchObject({ dockRuntimeId: agent.id, activeTerminalId: 'real-terminal', openRuntimeIds: ['bonsai:3', agent.id] })
    store().closeRuntime(agent.id); store().closeRuntime('bonsai:3')
    expect(views()).toEqual({ open: [], active: null })
    expect(store()).toMatchObject({ dockRuntimeId: '', activeTerminalId: '', openRuntimeIds: [] })
    expect(store().processes).toHaveLength(3)
  })

  it('restores each project and worktree including an intentionally empty scope and its last selected worktree', () => {
    const secondTree = { ...worktrees[0], id: 'second-tree', projectId: 'sprout-lab' }
    useBonsaiStore.setState({ worktrees: [...worktrees, secondTree] })
    applyProcessSummary(summary(1)); applyProcessSummary(summary(2, 'wt-daemon')); applyProcessSummary(summary(3, 'second-tree', 'sprout-lab'))
    store().openRuntime('bonsai:1'); store().openRuntime('bonsai:2')
    store().setActiveProject('sprout-lab'); store().openRuntime('sprout-lab:3')
    store().setActiveProject('bonsai')
    expect(store()).toMatchObject({ dockWorktreeId: 'wt-daemon', openRuntimeIds: ['bonsai:2'], dockRuntimeId: 'bonsai:2' })
    store().closeRuntime('bonsai:2'); store().setDockWorktreeId('wt-web')
    expect(store().openRuntimeIds).toEqual(['bonsai:1'])
    store().setDockWorktreeId('wt-daemon')
    store().setActiveWorkspace(projects.find(project => project.id === 'sprout-lab')!.workspaceId)
    store().setActiveProject('bonsai')
    expect(store()).toMatchObject({ dockWorktreeId: 'wt-daemon', openRuntimeIds: [], dockRuntimeId: '' })
    expect(views('sprout-lab', 'second-tree')?.open).toEqual([ref('sprout-lab:3')])
  })

  it('chooses a valid worktree when a newly selected project finishes initial discovery, leaving its views closed', () => {
    useBonsaiStore.setState({ worktrees: worktrees.filter(tree => tree.projectId !== 'sprout-lab') })
    store().setActiveProject('sprout-lab')
    expect(store().terminalViewPreferences['sprout-lab']).toBeUndefined()
    const discovered = summary(1, 'new-tree', 'sprout-lab')
    applySnapshot(snapshot('sprout-lab', [discovered], { local: { branches: [], worktrees: [{ id: 'new-tree', repository_id: 'sprout-lab', branch: 'feature', main: false, local_head_sha: 'a' }] } }))
    expect(store()).toMatchObject({ dockWorktreeId: 'new-tree', openRuntimeIds: [], dockRuntimeId: '' })
  })

  it('saves a delayed launch under its original scope without changing the current project, selection or panel', () => {
    store().setActiveProject('sprout-lab')
    const previous = store()
    applyProcessSummary(summary(1), true)
    expect(store()).toMatchObject({ activeProjectId: previous.activeProjectId, selection: previous.selection, dockWorktreeId: previous.dockWorktreeId, openRuntimeIds: previous.openRuntimeIds, dockRuntimeId: previous.dockRuntimeId })
    expect(views()).toEqual({ open: [ref('bonsai:1')], active: ref('bonsai:1') })
    store().setActiveProject('bonsai')
    expect(store().openRuntimeIds).toEqual(['bonsai:1'])
  })

  it('does not reopen on restart or older snapshots and keeps a manual process placement', () => {
    applyProcessSummary(summary(1), true)
    store().setManualNodePlacement('bonsai:1', { x: 123, y: 456 }); store().closeRuntime('bonsai:1')
    applyProcessSummary({ ...summary(1), status: 'starting', revision: 3 })
    applySnapshot(snapshot('bonsai', [{ ...summary(1), status: 'running', revision: 2 }]))
    expect(store().processes[0]).toMatchObject({ revision: 3, lifecycleStatus: 'starting' })
    expect(store().openRuntimeIds).toEqual([])
    expect(store().nodePlacements['bonsai:1']).toEqual({ x: 123, y: 456, mode: 'manual' })
  })

  it('treats repeated launch responses as the same open intent', () => {
    applyProcessSummary(summary(1), true)
    const notify = vi.fn(), unsubscribe = useBonsaiStore.subscribe(notify)
    applyProcessSummary(summary(1), true)
    expect(notify).not.toHaveBeenCalled()
    expect(store().openRuntimeIds).toEqual(['bonsai:1'])
    unsubscribe()
  })
})

describe('terminal preference persistence and authority', () => {
  it('saves view order and focus immediately with only stable references, preserving runtime node placements', () => {
    for (const id of [1, 2, 3]) applyProcessSummary(summary(id))
    store().openRuntime('bonsai:1'); store().openRuntime('bonsai:3'); store().reorderOpenRuntime('bonsai:3', 'bonsai:1'); store().focusRuntime('bonsai:1')
    store().setManualNodePlacement('bonsai:1', { x: 30, y: 40 })
    store().setDockState('normal')
    workspacePreferences.flush()
    expect(saved().terminalViewPreferences.bonsai.worktrees['wt-web']).toEqual({ open: [ref('bonsai:3'), ref('bonsai:1')], active: ref('bonsai:1') })
    const disk = localStorage.getItem(WORKSPACE_STORAGE_KEY)!
    expect(disk).not.toContain('PRIVATE_COMMAND')
    expect(saved()).not.toHaveProperty('processes')
    expect(saved()).not.toHaveProperty('agents')
    expect(saved()).not.toHaveProperty('terminalOutput')
    reload()
    expect(store().processes).toEqual([])
    expect(store().openRuntimeIds).toEqual(['bonsai:3', 'bonsai:1'])
    expect(store().dockRuntimeId).toBe('bonsai:1')
    expect(store().nodePlacements['bonsai:1']).toEqual({ x: 30, y: 40, mode: 'manual' })
    reconcileCatalog([{ id: 'bonsai', workspace_id: 'personal', full_name: 'owner/bonsai', default_branch: 'main' }])
    applySnapshot(snapshot('bonsai', [summary(1), summary(2), summary(3)]))
    expect(store().openRuntimeIds).toEqual(['bonsai:3', 'bonsai:1'])
  })

  it('retains unresolved references through bootstrap, other projects, failed snapshots and component loading', () => {
    applyProcessSummary(summary(1), true); applyProcessSummary(summary(2), true)
    reload()
    reconcileCatalog([{ id: 'bonsai', workspace_id: 'personal', full_name: 'owner/bonsai', default_branch: 'main' }, { id: 'sprout-lab', workspace_id: 'personal', full_name: 'owner/other', default_branch: 'main' }])
    applySnapshot(snapshot('sprout-lab'))
    expect(views()?.open).toEqual([ref('bonsai:1'), ref('bonsai:2')])
    applySnapshot(snapshot('bonsai', [], { local: undefined, freshness: { local: { state: 'loading' }, processes: { state: 'loading' } } }))
    expect(store().dockWorktreeId).toBe('wt-web')
    expect(views()?.open).toEqual([ref('bonsai:1'), ref('bonsai:2')])
    applySnapshot(snapshot('bonsai', [], { online: false, freshness: { local: { state: 'unavailable' }, processes: { state: 'error' } } }))
    expect(views()?.open).toHaveLength(2)
    applySnapshot(snapshot('bonsai', [summary(2)]))
    expect(views()).toEqual({ open: [ref('bonsai:2')], active: ref('bonsai:2') })
    applySnapshot(snapshot('bonsai'))
    expect(views()).toEqual({ open: [], active: null })
  })

  it('stays empty across reload, switching, reconnect and repeated snapshots with three available processes', () => {
    for (const id of [1, 2, 3]) { applyProcessSummary(summary(id), true); store().closeRuntime(`bonsai:${id}`) }
    reload()
    reconcileCatalog([{ id: 'bonsai', workspace_id: 'personal', full_name: 'owner/bonsai', default_branch: 'main' }, { id: 'sprout-lab', workspace_id: 'personal', full_name: 'owner/other', default_branch: 'main' }])
    for (let i = 0; i < 4; i++) applySnapshot(snapshot('bonsai', [summary(1), summary(2), summary(3)]))
    store().setActiveProject('sprout-lab'); store().setActiveProject('bonsai')
    expect(store()).toMatchObject({ dockRuntimeId: '', openRuntimeIds: [] })
    expect(views()).toEqual({ open: [], active: null })
  })

  it('prunes only the authoritative component and preserves unresolved agent references', () => {
    useBonsaiStore.setState({ terminalViewPreferences: { bonsai: { lastWorktreeId: 'wt-web', worktrees: { 'wt-web': { open: [ref('missing-process'), ref('loading-agent', 'agent')], active: ref('missing-process') } } } } })
    applySnapshot(snapshot('bonsai', [], { freshness: { local: { state: 'ready' }, agents: { state: 'loading' }, processes: { state: 'ready' } } }))
    expect(views()).toEqual({ open: [ref('loading-agent', 'agent')], active: ref('loading-agent', 'agent') })
    applySnapshot(snapshot('bonsai'))
    expect(views()).toEqual({ open: [], active: null })
  })

  it('preserves a temporarily unavailable worktree and its views rather than treating it as removed', () => {
    applyProcessSummary(summary(1), true)
    const source = snapshot('bonsai', [summary(1)])
    applySnapshot({ ...source, local: { ...source.local!, worktrees: source.local!.worktrees.map(tree => ({ ...tree, missing: tree.id === 'wt-web' })) } })
    expect(views()?.open).toEqual([ref('bonsai:1')])
    store().setActiveProject('sprout-lab'); store().setActiveProject('bonsai')
    expect(store()).toMatchObject({ dockWorktreeId: 'wt-web', openRuntimeIds: ['bonsai:1'], dockRuntimeId: 'bonsai:1' })
  })

  it('removes worktree and project preferences after authoritative removal without opening a replacement', () => {
    applyProcessSummary(summary(1), true)
    applySnapshot(snapshot('bonsai', [summary(1)], { local: { worktrees: [{ id: 'survivor', repository_id: 'bonsai', branch: 'other', main: false, local_head_sha: 'a' }], branches: [] } }))
    expect(views()).toBeUndefined()
    expect(store()).toMatchObject({ dockWorktreeId: 'survivor', dockRuntimeId: '', openRuntimeIds: [] })
    reconcileCatalog([{ id: 'sprout-lab', workspace_id: 'personal', full_name: 'owner/other', default_branch: 'main' }])
    expect(store().terminalViewPreferences).not.toHaveProperty('bonsai')
  })

  it('preserves empty preferences and layout when upgrading v1 storage', () => {
    localStorage.setItem(WORKSPACE_STORAGE_KEY, JSON.stringify({ version: 1, state: { activeProjectId: 'bonsai', dockWorktreeId: 'wt-web', dockHeight: 42, sidebarCollapsed: true, nodePlacements: { 'wt-web': { x: 50, y: 60, mode: 'manual' } } } }))
    workspacePreferences.rehydrate()
    expect(store()).toMatchObject({ dockHeight: 42, sidebarCollapsed: true, openRuntimeIds: [], terminalViewPreferences: {}, nodePlacements: { 'wt-web': { x: 50, y: 60, mode: 'manual' } } })
    expect(JSON.parse(localStorage.getItem(WORKSPACE_STORAGE_KEY)!).version).toBe(WORKSPACE_STORAGE_VERSION)
  })

  it('does not serialize view preferences on equivalent snapshots or streamed statuses', () => {
    applyProcessSummary(summary(1), true)
    applySnapshot(snapshot('bonsai', [summary(1)]))
    const write = vi.spyOn(workspaceStorage, 'setItem')
    for (let i = 0; i < 4; i++) applySnapshot(snapshot('bonsai', [summary(1)]))
    for (let revision = 2; revision < 5; revision++) applyProcessSummary({ ...summary(1), revision, status: 'backoff' })
    expect(write).not.toHaveBeenCalled()
    store().closeRuntime('bonsai:1')
    expect(write).toHaveBeenCalledTimes(1)
    expect(saved().terminalViewPreferences.bonsai.worktrees['wt-web'].open).toEqual([])
  })

  it('keeps a close usable in memory when browser storage fails', () => {
    applyProcessSummary(summary(1), true)
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('full') })
    store().closeRuntime('bonsai:1')
    expect(store().openRuntimeIds).toEqual([])
    expect(views()).toEqual({ open: [], active: null })
  })
})
