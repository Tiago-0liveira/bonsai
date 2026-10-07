import { create } from 'zustand'
import type { PersistStorage, StorageValue } from 'zustand/middleware'
import type { Agent, BoardItem, BoardList, BoardPriority, BoardType, DockState, DockTab, EditorPreference, NodePlacement, RuntimeReference, Selection, TerminalViewPreferences, ViewportState } from '../types'
import { activeRuntimePatch } from './runtimePreferences'
import type { BonsaiState } from './bonsai'

export const WORKSPACE_STORAGE_KEY = 'bonsai-web-workspace-v6'
export const WORKSPACE_STORAGE_VERSION = 3

export interface WorkspacePreferences {
  selection: Selection
  activeWorkspaceId: string
  activeProjectId: string
  sidebarCollapsed: boolean
  dockState: DockState
  dockHeight: number
  activeDockTab: DockTab
  dockWorktreeId: string
  collapsedBranchIds: string[]
  rightPanels: { files: boolean; prs: boolean }
  selectedFilePath: string
  editorPreference?: EditorPreference
  nodePlacements: Record<string, NodePlacement>
  viewport: ViewportState
  boardItems: BoardItem[]
  boardLists: BoardList[]
  boardPriorities: BoardPriority[]
  boardTypes: BoardType[]
  collapsedTagGroups: string[]
  detachedStackWorktreeIds: string[]
  expandedAutomaticGroups: string[]
  terminalViewPreferences: TerminalViewPreferences
}

type RecordValue = Record<string, unknown>
const record = (value: unknown): RecordValue | undefined => value !== null && typeof value === 'object' && !Array.isArray(value) ? value as RecordValue : undefined
const finite = (value: unknown): value is number => typeof value === 'number' && Number.isFinite(value)
const strings = (value: unknown): value is string[] => Array.isArray(value) && value.every(item => typeof item === 'string')
const member = <T extends string>(value: unknown, values: readonly T[]): value is T => typeof value === 'string' && values.includes(value as T)

function runtimeReference(value: unknown): RuntimeReference | undefined {
  const row = record(value)
  return row && member(row.kind, ['agent', 'process']) && typeof row.id === 'string' && row.id
    ? { kind: row.kind, id: row.id, ...(row.kind === 'process' && typeof row.commandKey === 'string' ? { commandKey: row.commandKey } : {}) } : undefined
}

function terminalPreferences(value: unknown): TerminalViewPreferences | undefined {
  const raw = record(value)
  if (!raw) return undefined
  return Object.fromEntries(Object.entries(raw).flatMap(([projectId, value]) => {
    const project = record(value), worktrees = record(project?.worktrees)
    if (!projectId || !project || typeof project.lastWorktreeId !== 'string' || !worktrees) return []
    return [[projectId, {
      lastWorktreeId: project.lastWorktreeId,
      reopening: project.reopening === 'restore' ? 'restore' : 'keep_closed',
      worktrees: Object.fromEntries(Object.entries(worktrees).flatMap(([worktreeId, value]) => {
        const view = record(value)
        if (!view || !Array.isArray(view.open)) return []
        const seen = new Set<string>()
        const open = view.open.flatMap(value => {
          const ref = runtimeReference(value)
          if (!ref || seen.has(ref.id)) return []
          seen.add(ref.id)
          return [ref]
        })
        const active = runtimeReference(view.active)
        return [[worktreeId, { open, active: open.find(ref => ref.id === active?.id && ref.kind === active?.kind) ?? open[0] ?? null }]]
      })),
    }]]
  }))
}

function rows<T>(value: unknown, parse: (row: RecordValue) => T | undefined): T[] | undefined {
  if (!Array.isArray(value)) return undefined
  return value.flatMap(item => {
    const row = record(item)
    const parsed = row && parse(row)
    return parsed ? [parsed] : []
  })
}

// Both hydration and serialization use the same field allowlist, including nested data.
export function sanitizeWorkspacePreferences(value: unknown): Partial<WorkspacePreferences> {
  const raw = record(value)
  if (!raw) return {}
  const result: Partial<WorkspacePreferences> = {}
  const views = terminalPreferences(raw.terminalViewPreferences)
  if (views) result.terminalViewPreferences = views
  for (const key of ['activeWorkspaceId', 'activeProjectId', 'dockWorktreeId', 'selectedFilePath'] as const) {
    if (typeof raw[key] === 'string') result[key] = raw[key]
  }
  if (typeof raw.sidebarCollapsed === 'boolean') result.sidebarCollapsed = raw.sidebarCollapsed
  if (member(raw.dockState, ['collapsed', 'normal', 'maximized'])) result.dockState = raw.dockState
  if (finite(raw.dockHeight)) result.dockHeight = Math.min(72, Math.max(14, raw.dockHeight))
  if (member(raw.activeDockTab, ['agent', 'terminal', 'tests', 'files', 'pr', 'checks', 'logs'])) result.activeDockTab = raw.activeDockTab
  if (member(raw.editorPreference, ['vscode', 'cursor', 'zed', 'system'])) result.editorPreference = raw.editorPreference
  for (const key of ['collapsedBranchIds', 'collapsedTagGroups', 'detachedStackWorktreeIds', 'expandedAutomaticGroups'] as const) {
    if (strings(raw[key])) result[key] = [...raw[key]]
  }
  const panels = record(raw.rightPanels)
  if (panels && typeof panels.files === 'boolean' && typeof panels.prs === 'boolean') result.rightPanels = { files: panels.files, prs: panels.prs }
  const viewport = record(raw.viewport)
  if (viewport && finite(viewport.x) && finite(viewport.y) && finite(viewport.zoom) && viewport.zoom > 0) result.viewport = { x: viewport.x, y: viewport.y, zoom: viewport.zoom }

  const agents = Array.isArray(raw.agents) ? raw.agents.map(record).filter((agent): agent is RecordValue => Boolean(agent)) : []
  const discardedIds = new Set(agents.flatMap(agent => typeof agent.id === 'string' && (!views || agent.providerId !== 'antigravity') ? [agent.id] : []))
  if (!views && typeof raw.dockRuntimeId === 'string' && raw.dockRuntimeId) discardedIds.add(raw.dockRuntimeId)
  if (!views && strings(raw.openRuntimeIds)) raw.openRuntimeIds.forEach(id => discardedIds.add(id))
  if (result.dockWorktreeId && discardedIds.has(result.dockWorktreeId)) result.dockWorktreeId = ''
  const selection = record(raw.selection)
  if (selection && typeof selection.id === 'string') {
    if (selection.type === 'project' || selection.type === 'worktree') result.selection = { type: selection.type, id: selection.id }
    if ((selection.type === 'agent' || selection.type === 'process') && views && !discardedIds.has(selection.id)) {
      result.selection = { type: selection.type, id: selection.id }
    } else if (selection.type === 'agent' || selection.type === 'process') {
      discardedIds.add(selection.id)
      const worktreeId = agents.find(agent => agent.id === selection.id)?.worktreeId
      const fallback = typeof worktreeId === 'string' && worktreeId ? worktreeId : result.dockWorktreeId
      result.selection = fallback ? { type: 'worktree', id: fallback } : { type: 'project', id: result.activeProjectId ?? '' }
      if (fallback) result.dockWorktreeId = fallback
    }
  }
  const placements = record(raw.nodePlacements)
  if (placements) result.nodePlacements = Object.fromEntries(Object.entries(placements).flatMap(([id, value]) => {
    const placement = record(value)
    if (discardedIds.has(id) || (!views && id.startsWith('agent-')) || !placement || !finite(placement.x) || !finite(placement.y) || !member(placement.mode, ['manual', 'generated'])) return []
    return [[id, { x: placement.x, y: placement.y, mode: placement.mode }]]
  }))

  const items = rows<BoardItem>(raw.boardItems, row => {
    const { id, title, kind, status, assignee, priority } = row
    return typeof id === 'string' && typeof title === 'string' && typeof kind === 'string' && typeof status === 'string' && typeof assignee === 'string' && typeof priority === 'string' ? { id, title, kind, status, assignee, priority } : undefined
  })
  const lists = rows<BoardList>(raw.boardLists, row => {
    const { id, name, color, priority, itemType, order, archived } = row
    return typeof id === 'string' && typeof name === 'string' && member(color, ['purple', 'blue', 'green', 'orange', 'red', 'cyan', 'pink']) && typeof priority === 'string' && typeof itemType === 'string' && finite(order) ? { id, name, color, priority, itemType, order, ...(typeof archived === 'boolean' ? { archived } : {}) } : undefined
  })
  const priorities = rows<BoardPriority>(raw.boardPriorities, row => {
    const { id, name, rank } = row
    return typeof id === 'string' && typeof name === 'string' && finite(rank) ? { id, name, rank } : undefined
  })
  const types = rows<BoardType>(raw.boardTypes, row => {
    const { id, name } = row
    return typeof id === 'string' && typeof name === 'string' ? { id, name } : undefined
  })
  if (items) result.boardItems = items
  if (lists) result.boardLists = lists
  if (priorities) result.boardPriorities = priorities
  if (types) result.boardTypes = types
  return result
}

export function mergeWorkspacePreferences<T extends WorkspacePreferences & {
  agents: Agent[]; envVariables: unknown; terminalSessions: unknown[]; terminalOutput: unknown
  activeTerminalId: string; dockRuntimeId: string; openRuntimeIds: string[]
  processes: { id: string }[]; projects: { id: string; workspaceId: string }[]; worktrees: { id: string; projectId: string }[]
}>(persisted: unknown, current: T): T {
  const raw = record(persisted)
  const saved = sanitizeWorkspacePreferences({ ...raw, agents: [...current.agents, ...(Array.isArray(raw?.agents) ? raw.agents : [])] })
  const next = { ...current, ...sanitizeWorkspacePreferences(current), ...saved, terminalViewPreferences: saved.terminalViewPreferences ?? {}, agents: current.agents.filter(agent => agent.providerId === 'antigravity'), envVariables: {}, terminalSessions: [], terminalOutput: {}, activeTerminalId: '', dockRuntimeId: '', openRuntimeIds: [] as string[], visitOpenedRuntimeIds: [] as string[] }
  next.dockWorktreeId = next.terminalViewPreferences[next.activeProjectId]?.lastWorktreeId ?? next.dockWorktreeId
  // On an explicit rehydrate, canonical entities may already be available.
  if (current.projects.length) {
    const project = current.projects.find(project => project.id === next.activeProjectId) ?? current.projects[0]
    next.activeProjectId = project.id
    next.activeWorkspaceId = project.workspaceId
    const remembered = next.terminalViewPreferences[project.id]?.lastWorktreeId
    const tree = current.worktrees.find(tree => tree.id === next.dockWorktreeId && tree.projectId === project.id)
    next.dockWorktreeId = remembered ?? tree?.id ?? current.worktrees.find(tree => tree.projectId === project.id)?.id ?? next.dockWorktreeId
    const authoritativeTrees = (current as unknown as BonsaiState).syncFreshness?.[project.id]?.local?.state === 'ready'
    const invalidTree = next.selection.type === 'worktree' && (authoritativeTrees || !saved.terminalViewPreferences) && !current.worktrees.some(tree => tree.id === next.selection.id && tree.projectId === project.id)
    const invalidProject = next.selection.type === 'project' && next.selection.id !== project.id
    if (invalidTree || invalidProject) {
      next.selection = { type: 'project', id: project.id }
    }
  }
  Object.assign(next, activeRuntimePatch(next as unknown as BonsaiState))
  return next
}

export const useWorkspaceStorageStatus = create<{ error: string }>(() => ({ error: '' }))
const READ_ERROR = 'Browser storage is unavailable. Your workspace is using in-memory defaults.'
const WRITE_ERROR = 'Browser storage could not be saved. Your changes remain in memory; previous stored data may remain until saving succeeds.'
const CLEANUP_ERROR = 'Browser storage could not be cleaned. Old stored data may remain; retry saving when storage is available.'
const INVALID_ERROR = 'Saved workspace data was invalid. Safe defaults have been loaded.'

// A synchronous read scrubs the existing record before returning anything to Zustand.
// No backup or logging of the original record is performed.
export function createWorkspaceStorage(getStorage: () => Storage = () => window.localStorage): PersistStorage<Partial<WorkspacePreferences>, boolean> {
  const fail = (error: string) => useWorkspaceStorageStatus.setState({ error })
  const clean = (storage: Storage, name: string, payload: string) => {
    try { storage.setItem(name, payload); return true } catch {
      try { storage.removeItem(name); fail(WRITE_ERROR) } catch { fail(CLEANUP_ERROR) }
      return false
    }
  }
  return {
    getItem(name) {
      let storage: Storage
      let raw: string | null
      try { storage = getStorage(); raw = storage.getItem(name) } catch { fail(READ_ERROR); return null }
      if (raw === null) return null
      let parsed: RecordValue | undefined
      try {
        parsed = record(JSON.parse(raw))
        if (!parsed || !record(parsed.state)) throw new Error('invalid record')
      } catch {
        clean(storage, name, JSON.stringify({ state: {}, version: WORKSPACE_STORAGE_VERSION }))
        // Keep a cleanup failure actionable instead of replacing it with a parsing notice.
        if (!useWorkspaceStorageStatus.getState().error) fail(INVALID_ERROR)
        return null
      }
      const value = { state: sanitizeWorkspacePreferences(parsed.state), version: WORKSPACE_STORAGE_VERSION }
      const payload = JSON.stringify(value)
      if (raw !== payload) clean(storage, name, payload)
      return value
    },
    setItem(name, value) {
      try {
        const payload = JSON.stringify({ state: sanitizeWorkspacePreferences(value.state), version: WORKSPACE_STORAGE_VERSION })
        const storage = getStorage()
        if (storage.getItem(name) !== payload) storage.setItem(name, payload)
        return true
      } catch { fail(WRITE_ERROR); return false }
    },
    removeItem(name) {
      try { getStorage().removeItem(name); return true } catch { fail(CLEANUP_ERROR); return false }
    },
  }
}

export const workspaceStorage = createWorkspaceStorage()

export function retryWorkspaceStorage(state: WorkspacePreferences & { agents: Agent[] }) {
  useWorkspaceStorageStatus.setState({ error: '' })
  workspaceStorage.setItem(WORKSPACE_STORAGE_KEY, { state: sanitizeWorkspacePreferences(state), version: WORKSPACE_STORAGE_VERSION } satisfies StorageValue<Partial<WorkspacePreferences>>)
}
