import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { workspacePreferences, useBonsaiStore } from './bonsai'
import { applySnapshot, __resetGitSyncForTests } from '../api/git'
import { agents } from '../mock/agents'
import { projects } from '../test/fixtures/projects'
import { boardItems, boardLists, boardPriorities, boardTypes } from '../mock/board'
import { createWorkspaceStorage, mergeWorkspacePreferences, retryWorkspaceStorage, sanitizeWorkspacePreferences, useWorkspaceStorageStatus, WORKSPACE_STORAGE_KEY, WORKSPACE_STORAGE_VERSION } from './workspacePersistence'

const marker = 'SYNTHETIC_ENV_VALUE_MUST_DISAPPEAR'
const legacy = () => ({
  state: {
    activeProjectId: 'repo', activeWorkspaceId: 'personal', selection: { type: 'agent', id: 'worker' },
    dockWorktreeId: 'old-tree', dockRuntimeId: 'worker', openRuntimeIds: ['worker', 'old-process'],
    sidebarCollapsed: true, dockHeight: 42, dockState: 'normal', editorPreference: 'cursor',
    selectedFilePath: 'src/main.ts', viewport: { x: 15, y: 25, zoom: 0.75 }, rightPanels: { files: false, prs: true },
    agents: [{ id: 'worker', worktreeId: 'tree', state: 'running', prompt: marker }],
    terminalSessions: [{ id: 'shell', label: 'Shell' }], activeTerminalId: 'shell', terminalOutput: { shell: [marker] },
    processes: [{ id: 'old-process', command: marker }],
    envVariables: { repo: [{ id: 'secret', key: 'SECRET', value: marker, secret: true }, { id: 'plain', key: 'PLAIN', value: marker, secret: false }] },
    nodePlacements: { tree: { x: 10, y: 20, mode: 'manual', value: marker }, worker: { x: 30, y: 40, mode: 'manual' }, 'agent-orphan': { x: 50, y: 60, mode: 'manual' } },
    boardItems: [{ ...boardItems[0], title: 'My saved draft', metadata: { value: marker } }],
    boardLists, boardPriorities, boardTypes,
    collapsedBranchIds: ['tree'], collapsedTagGroups: ['repo:feat'], detachedStackWorktreeIds: ['tree'], expandedAutomaticGroups: ['unlinked:repo'],
    notice: marker, unknownField: marker,
  }, version: 0,
})

beforeEach(() => {
  vi.restoreAllMocks()
  workspacePreferences.cancel()
  localStorage.clear()
  useWorkspaceStorageStatus.setState({ error: '' })
  useBonsaiStore.setState(useBonsaiStore.getInitialState(), true)
  localStorage.clear()
})
afterEach(() => { workspacePreferences.cancel(); vi.restoreAllMocks(); vi.unstubAllGlobals() })

const read = () => JSON.parse(localStorage.getItem(WORKSPACE_STORAGE_KEY)!)

describe('workspace migration and hydration', () => {
  it('scrubs legacy values on disk before merging, preserving preferences and user data', async () => {
    localStorage.setItem('unrelated-app', marker)
    localStorage.setItem(WORKSPACE_STORAGE_KEY, JSON.stringify(legacy()))
    await workspacePreferences.rehydrate()
    const state = useBonsaiStore.getState()
    expect(state.envVariables).toEqual({})
    expect(state.agents).toEqual([])
    expect(state.processes).toEqual([])
    expect(state.terminalSessions).toEqual([])
    expect(state.terminalOutput).toEqual({})
    expect(state.activeTerminalId).toBe('')
    expect(state.dockRuntimeId).toBe('')
    expect(state.openRuntimeIds).toEqual([])
    expect(state.selection).toEqual({ type: 'worktree', id: 'tree' })
    expect(state.dockWorktreeId).toBe('tree')
    expect(state.nodePlacements).toEqual({ tree: { x: 10, y: 20, mode: 'manual' } })
    expect(state.sidebarCollapsed).toBe(true)
    expect(state.dockHeight).toBe(42)
    expect(state.editorPreference).toBe('cursor')
    expect(state.boardItems[0].title).toBe('My saved draft')
    expect(state.boardLists).toEqual(boardLists)
    expect(state.boardPriorities).toEqual(boardPriorities)
    expect(state.boardTypes).toEqual(boardTypes)
    expect(state.viewport).toEqual({ x: 15, y: 25, zoom: 0.75 })
    expect(state.rightPanels).toEqual({ files: false, prs: true })
    expect(read().version).toBe(WORKSPACE_STORAGE_VERSION)
    expect(localStorage.getItem(WORKSPACE_STORAGE_KEY)).not.toContain(marker)
    expect(JSON.stringify(state)).not.toContain(marker)
    expect(localStorage.getItem('unrelated-app')).toBe(marker)
    expect(localStorage.length).toBe(2)
  })

  it('is idempotent and never restores simulated agents after a reload', async () => {
    localStorage.setItem(WORKSPACE_STORAGE_KEY, JSON.stringify(legacy()))
    await workspacePreferences.rehydrate()
    const sanitized = localStorage.getItem(WORKSPACE_STORAGE_KEY)!
    const writes = vi.spyOn(Storage.prototype, 'setItem')
    await workspacePreferences.rehydrate()
    expect(writes).not.toHaveBeenCalled()
    expect(localStorage.getItem(WORKSPACE_STORAGE_KEY)).toBe(sanitized)
    writes.mockRestore()
    useBonsaiStore.setState(useBonsaiStore.getInitialState(), true)
    localStorage.setItem(WORKSPACE_STORAGE_KEY, sanitized)
    await workspacePreferences.rehydrate()
    expect(useBonsaiStore.getState().agents).toEqual([])
    expect(useBonsaiStore.getState().boardItems[0].title).toBe('My saved draft')
  })

  it('sanitizes explicit rehydration even for a record claiming the current or a future version', async () => {
    for (const version of [WORKSPACE_STORAGE_VERSION, WORKSPACE_STORAGE_VERSION + 1]) {
      localStorage.setItem(WORKSPACE_STORAGE_KEY, JSON.stringify({ ...legacy(), version }))
      await workspacePreferences.rehydrate()
      expect(useBonsaiStore.getState().envVariables).toEqual({})
      expect(useBonsaiStore.getState().agents).toEqual([])
      expect(localStorage.getItem(WORKSPACE_STORAGE_KEY)).not.toContain(marker)
    }
  })

  it('never serializes injected environment values or agent/session/output data on later writes', () => {
    useBonsaiStore.setState({ envVariables: legacy().state.envVariables, terminalOutput: { old: [marker] } })
    useBonsaiStore.getState().setDockHeight(40)
    workspacePreferences.flush()
    expect(read().state).not.toHaveProperty('envVariables')
    expect(read().state).not.toHaveProperty('agents')
    expect(read().state).not.toHaveProperty('terminalSessions')
    expect(read().state).not.toHaveProperty('terminalOutput')
    expect(read().state).not.toHaveProperty('dockRuntimeId')
    expect(read().state).not.toHaveProperty('openRuntimeIds')
    expect(localStorage.getItem(WORKSPACE_STORAGE_KEY)).not.toContain(marker)
  })

  it.each(['{broken ' + marker, 'null', '[]', '{"state":null}', '{"state":[]}'])('loads safe defaults and cleans malformed storage: %s', async payload => {
    localStorage.setItem(WORKSPACE_STORAGE_KEY, payload)
    await workspacePreferences.rehydrate()
    expect(useBonsaiStore.getState().envVariables).toEqual({})
    expect(useBonsaiStore.getState().agents).toEqual([])
    expect(useBonsaiStore.getState().dockHeight).toBe(30)
    expect(useWorkspaceStorageStatus.getState().error).toContain('invalid')
    expect(localStorage.getItem(WORKSPACE_STORAGE_KEY)).not.toContain(marker)
    expect(read()).toEqual({ state: {}, version: WORKSPACE_STORAGE_VERSION })
  })

  it('falls back to the active project when legacy runtime has no associated worktree', () => {
    const prefs = sanitizeWorkspacePreferences({ activeProjectId: 'repo', selection: { type: 'agent', id: 'gone' }, nodePlacements: { gone: { x: 0, y: 0, mode: 'manual' } } })
    expect(prefs.selection).toEqual({ type: 'project', id: 'repo' })
    expect(prefs.nodePlacements).toEqual({})
  })

  it('drops orphan runtime placements even when their original agent records are missing', () => {
    const prefs = sanitizeWorkspacePreferences({ dockRuntimeId: 'orphan', openRuntimeIds: ['other'], nodePlacements: { orphan: { x: 0, y: 0, mode: 'manual' }, other: { x: 1, y: 1, mode: 'manual' }, tree: { x: 2, y: 2, mode: 'manual' } } })
    expect(prefs.nodePlacements).toEqual({ tree: { x: 2, y: 2, mode: 'manual' } })
  })

  it('stores only scoped runtime references and preserves independent placements for open and closed real runtimes', () => {
    const preferences = sanitizeWorkspacePreferences({
      terminalViewPreferences: { repo: { lastWorktreeId: 'tree', prompt: marker, worktrees: {
        tree: { open: [{ kind: 'process', id: 'repo:1', command: marker, pid: 10 }, { kind: 'agent', id: 'agent-real', output: marker }, { kind: 'process', id: 'repo:1' }, { kind: 'shell', id: 'bad' }], active: { kind: 'agent', id: 'agent-real', token: marker }, environment: { value: marker } },
        empty: { open: [], active: null, output: marker },
        invalid: { open: marker },
      } } },
      dockRuntimeId: 'repo:1', openRuntimeIds: ['repo:1', 'agent-real'],
      nodePlacements: { 'repo:1': { x: 1, y: 2, mode: 'manual' }, 'repo:2': { x: 3, y: 4, mode: 'manual' }, 'agent-real': { x: 5, y: 6, mode: 'manual' } },
    })
    expect(preferences.terminalViewPreferences).toEqual({ repo: { lastWorktreeId: 'tree', worktrees: {
      tree: { open: [{ kind: 'process', id: 'repo:1' }, { kind: 'agent', id: 'agent-real' }], active: { kind: 'agent', id: 'agent-real' } },
      empty: { open: [], active: null },
    } } })
    expect(preferences.nodePlacements).toHaveProperty('repo:1')
    expect(preferences.nodePlacements).toHaveProperty('repo:2')
    expect(preferences.nodePlacements).toHaveProperty('agent-real')
    expect(JSON.stringify(preferences)).not.toContain(marker)
    expect(sanitizeWorkspacePreferences(preferences)).toEqual(preferences)
  })

  it('derives legacy dock fields from the saved scope before canonical entities load', () => {
    const ref = { kind: 'process' as const, id: 'repo:1' }
    const state = mergeWorkspacePreferences({
      activeProjectId: 'repo', dockWorktreeId: 'stale-dock-field',
      terminalViewPreferences: { repo: { lastWorktreeId: 'saved-tree', worktrees: { 'saved-tree': { open: [ref], active: ref } } } },
    }, useBonsaiStore.getInitialState())
    expect(state.worktrees).toEqual([])
    expect(state).toMatchObject({ dockWorktreeId: 'saved-tree', dockRuntimeId: 'repo:1', openRuntimeIds: ['repo:1'] })
  })

  it('reconciles in-memory simulated selection and placements when rehydrating an empty record', () => {
    const current = { ...useBonsaiStore.getInitialState(), selection: { type: 'agent' as const, id: 'worker' }, agents: [{ ...agents[0], id: 'worker', worktreeId: 'tree' }], nodePlacements: { worker: { x: 0, y: 0, mode: 'manual' as const } } }
    const state = mergeWorkspacePreferences({}, current)
    expect(state.selection).toEqual({ type: 'worktree', id: 'tree' })
    expect(state.agents).toEqual([])
    expect(state.nodePlacements).toEqual({})
  })

  it('validates nested preference fields instead of merging malformed data or unknown properties', () => {
    const prefs = sanitizeWorkspacePreferences({ ...legacy().state, dockHeight: 999, rightPanels: { files: marker }, viewport: { x: marker, y: 0, zoom: 1 }, collapsedBranchIds: [null], boardTypes: [{ id: 'type', name: 'Custom', value: marker }, null] })
    expect(prefs.dockHeight).toBe(72)
    expect(prefs).not.toHaveProperty('rightPanels')
    expect(prefs).not.toHaveProperty('viewport')
    expect(prefs).not.toHaveProperty('collapsedBranchIds')
    expect(prefs.boardTypes).toEqual([{ id: 'type', name: 'Custom' }])
    expect(JSON.stringify(prefs)).not.toContain(marker)
    expect(sanitizeWorkspacePreferences(prefs)).toEqual(prefs)
  })

  it('resolves dangling worktree selection when the first canonical snapshot arrives', async () => {
    __resetGitSyncForTests()
    localStorage.setItem(WORKSPACE_STORAGE_KEY, JSON.stringify(legacy()))
    await workspacePreferences.rehydrate()
    useBonsaiStore.setState({ projects: [{ ...projects[0], id: 'repo' }] })
    applySnapshot({ repository: { id: 'repo', workspace_id: 'personal', full_name: 'owner/repo', default_branch: 'main' }, sequence: 1, online: true, metadata: {}, local: { worktrees: [], branches: [] } })
    expect(useBonsaiStore.getState().selection).toEqual({ type: 'project', id: 'repo' })
    expect(useBonsaiStore.getState().dockWorktreeId).toBe('')
  })

  it('keeps live managed processes while reconciling stale runtime and project IDs', () => {
    const current = { ...useBonsaiStore.getInitialState(), activeProjectId: 'real', projects: [{ id: 'real', workspaceId: 'workspace' }], worktrees: [{ id: 'real-tree', projectId: 'real' }], processes: [{ id: 'live-process' }], dockRuntimeId: 'live-process', openRuntimeIds: ['live-process', 'worker'] }
    const state = mergeWorkspacePreferences(legacy().state, current)
    expect(state.activeProjectId).toBe('real')
    expect(state.selection).toEqual({ type: 'project', id: 'real' })
    expect(state.dockWorktreeId).toBe('real-tree')
    expect(state.dockRuntimeId).toBe('')
    expect(state.openRuntimeIds).toEqual([])
    expect(state.processes).toBe(current.processes)
  })
})

describe('storage failures', () => {
  it('handles unavailable storage and denied reads with in-memory defaults', () => {
    for (const storage of [() => { throw new Error(marker) }, () => ({ getItem() { throw new Error(marker) } } as unknown as Storage)]) {
      const adapter = createWorkspaceStorage(storage)
      expect(adapter.getItem(WORKSPACE_STORAGE_KEY)).toBeNull()
      expect(useWorkspaceStorageStatus.getState().error).toContain('in-memory defaults')
      expect(useWorkspaceStorageStatus.getState().error).not.toContain(marker)
    }
  })

  it('removes the legacy copy if rewriting fails, returning only sanitized preferences', () => {
    const storage = { getItem: () => JSON.stringify(legacy()), setItem: vi.fn(() => { throw new Error(marker) }), removeItem: vi.fn() }
    const adapter = createWorkspaceStorage(() => storage as unknown as Storage)
    const value = adapter.getItem(WORKSPACE_STORAGE_KEY)
    expect(JSON.stringify(value)).not.toContain(marker)
    expect(storage.removeItem).toHaveBeenCalledWith(WORKSPACE_STORAGE_KEY)
    expect(useWorkspaceStorageStatus.getState().error).toContain('could not be saved')
  })

  it('reports failed cleanup without ever hydrating or logging removed values', async () => {
    const logs = [vi.spyOn(console, 'log'), vi.spyOn(console, 'warn'), vi.spyOn(console, 'error')]
    localStorage.setItem(WORKSPACE_STORAGE_KEY, JSON.stringify(legacy()))
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error(marker) })
    vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(() => { throw new Error(marker) })
    await workspacePreferences.rehydrate()
    expect(useBonsaiStore.getState().envVariables).toEqual({})
    expect(useBonsaiStore.getState().agents).toEqual([])
    expect(useWorkspaceStorageStatus.getState().error).toContain('could not be cleaned')
    expect(useWorkspaceStorageStatus.getState().error).not.toContain(marker)
    logs.forEach(log => expect(log).not.toHaveBeenCalled())
  })

  it('keeps preference changes usable in memory after a write failure, then retries safely', () => {
    const write = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error(marker) })
    expect(() => useBonsaiStore.getState().setDockHeight(44)).not.toThrow()
    expect(useBonsaiStore.getState().dockHeight).toBe(44)
    workspacePreferences.flush()
    expect(useWorkspaceStorageStatus.getState().error).toContain('remain in memory')
    write.mockRestore()
    retryWorkspaceStorage(useBonsaiStore.getState())
    expect(useWorkspaceStorageStatus.getState().error).toBe('')
    expect(read().state.dockHeight).toBe(44)
    expect(localStorage.getItem(WORKSPACE_STORAGE_KEY)).not.toContain(marker)
  })
})
