import type { BonsaiState } from './bonsai'
import type { RuntimeReference, RuntimeViewPreference } from '../types'
import { changedPatch, shareEqual } from './reconciliation'

const EMPTY_VIEW: RuntimeViewPreference = { open: [], active: null }
const same = (a: RuntimeReference | null, b: RuntimeReference | null) => a?.kind === b?.kind && a?.id === b?.id

export function runtimeScope(state: BonsaiState, id: string) {
  const agent = state.agents.find(item => item.id === id)
  const process = state.processes.find(item => item.id === id)
  const runtime = agent ?? process
  if (!runtime) return undefined
  const projectId = runtime.projectId ?? state.worktrees.find(tree => tree.id === runtime.worktreeId)?.projectId
  if (!projectId) return undefined
  const worktreeKnown = state.worktrees.some(tree => tree.id === runtime.worktreeId && tree.projectId === projectId)
  const worktreeId = worktreeKnown || state.syncFreshness[projectId]?.local?.state !== 'ready' ? runtime.worktreeId : ''
  return { projectId, worktreeId, ref: { kind: agent ? 'agent' as const : 'process' as const, id }, terminalId: agent?.terminalId ?? '' }
}

export function activeRuntimePatch(state: BonsaiState, projectId = state.activeProjectId, worktreeId = state.dockWorktreeId): Partial<BonsaiState> {
  const view = state.terminalViewPreferences[projectId]?.worktrees[worktreeId] ?? EMPTY_VIEW
  const active = view.open.find(ref => same(ref, view.active)) ?? view.open[0]
  return changedPatch(state, {
    dockWorktreeId: worktreeId,
    openRuntimeIds: view.open.map(ref => ref.id),
    dockRuntimeId: active?.id ?? '',
    activeTerminalId: active?.kind === 'agent' ? state.agents.find(agent => agent.id === active.id)?.terminalId ?? '' : '',
  })
}

function saveView(state: BonsaiState, projectId: string, worktreeId: string, view: RuntimeViewPreference, remember = true) {
  const previous = state.terminalViewPreferences[projectId]
  const terminalViewPreferences = shareEqual(state.terminalViewPreferences, {
    ...state.terminalViewPreferences,
    [projectId]: {
      lastWorktreeId: remember ? worktreeId : previous?.lastWorktreeId ?? worktreeId,
      worktrees: { ...previous?.worktrees, [worktreeId]: view },
    },
  })
  return terminalViewPreferences
}

export function switchRuntimeScope(state: BonsaiState, projectId: string, worktreeId?: string): Partial<BonsaiState> {
  const project = state.projects.find(item => item.id === projectId)
  if (!project) return {}
  const projectTrees = state.worktrees.filter(tree => tree.projectId === projectId)
  const trees = projectTrees.filter(tree => !tree.missing)
  const saved = state.terminalViewPreferences[projectId]?.lastWorktreeId
  const preferred = worktreeId ?? saved
  const loading = state.syncFreshness[projectId]?.local?.state !== 'ready'
  if (preferred === undefined && !trees.length) {
    // Do not mistake a project visited before discovery for an explicitly
    // selected project shelf. The first local snapshot will choose a worktree.
    return changedPatch(state, {
      activeProjectId: projectId, activeWorkspaceId: project.workspaceId,
      dockWorktreeId: '', dockRuntimeId: '', openRuntimeIds: [], activeTerminalId: '',
    })
  }
  const nextTree = preferred !== undefined && (preferred === '' || loading || projectTrees.some(tree => tree.id === preferred))
    ? preferred : (trees.find(tree => tree.branch !== project.defaultBranch) ?? trees[0])?.id ?? ''
  const view = state.terminalViewPreferences[projectId]?.worktrees[nextTree] ?? EMPTY_VIEW
  const terminalViewPreferences = saveView(state, projectId, nextTree, view)
  return changedPatch(state, {
    activeProjectId: projectId, activeWorkspaceId: project.workspaceId, terminalViewPreferences,
    ...activeRuntimePatch({ ...state, terminalViewPreferences }, projectId, nextTree),
  })
}

// Launch can save intent in an inactive project without stealing the current scope.
export function openRuntimePatch(state: BonsaiState, id: string, activate = true): Partial<BonsaiState> {
  const scope = runtimeScope(state, id)
  if (!scope) return {}
  const { projectId, worktreeId, ref } = scope
  const previous = state.terminalViewPreferences[projectId]?.worktrees[worktreeId] ?? EMPTY_VIEW
  const view = { open: previous.open.some(item => same(item, ref)) ? previous.open : [...previous.open, ref], active: ref }
  const terminalViewPreferences = saveView(state, projectId, worktreeId, view)
  if (!activate) return changedPatch(state, { terminalViewPreferences })
  return changedPatch(state, {
    terminalViewPreferences,
    ...switchRuntimeScope({ ...state, terminalViewPreferences }, projectId, worktreeId),
    ...activeRuntimePatch({ ...state, terminalViewPreferences }, projectId, worktreeId),
  })
}

export function focusRuntimePatch(state: BonsaiState, id: string): Partial<BonsaiState> {
  const previous = state.terminalViewPreferences[state.activeProjectId]?.worktrees[state.dockWorktreeId] ?? EMPTY_VIEW
  const active = previous.open.find(ref => ref.id === id)
  if (!active || same(active, previous.active)) return {}
  const terminalViewPreferences = saveView(state, state.activeProjectId, state.dockWorktreeId, { ...previous, active })
  return { terminalViewPreferences, ...activeRuntimePatch({ ...state, terminalViewPreferences }) }
}

export function closeRuntimePatch(state: BonsaiState, id: string): Partial<BonsaiState> {
  let terminalViewPreferences = state.terminalViewPreferences
  for (const [projectId, project] of Object.entries(terminalViewPreferences)) {
    for (const [worktreeId, previous] of Object.entries(project.worktrees)) {
      const index = previous.open.findIndex(ref => ref.id === id)
      if (index < 0) continue
      const open = previous.open.filter(ref => ref.id !== id)
      const active = previous.active?.id === id ? open[Math.min(index, open.length - 1)] ?? null : previous.active
      terminalViewPreferences = saveView({ ...state, terminalViewPreferences }, projectId, worktreeId, { open, active }, false)
    }
  }
  return changedPatch(state, { terminalViewPreferences, ...activeRuntimePatch({ ...state, terminalViewPreferences }) })
}

export function reorderRuntimePatch(state: BonsaiState, id: string, overId: string): Partial<BonsaiState> {
  const previous = state.terminalViewPreferences[state.activeProjectId]?.worktrees[state.dockWorktreeId] ?? EMPTY_VIEW
  const from = previous.open.findIndex(ref => ref.id === id), to = previous.open.findIndex(ref => ref.id === overId)
  if (from < 0 || to < 0 || from === to) return {}
  const open = [...previous.open]
  open.splice(to, 0, ...open.splice(from, 1))
  const terminalViewPreferences = saveView(state, state.activeProjectId, state.dockWorktreeId, { open, active: previous.active })
  return { terminalViewPreferences, ...activeRuntimePatch({ ...state, terminalViewPreferences }) }
}

export interface RuntimeAuthority { worktrees: boolean; agents: boolean; processes: boolean }

// Only a successful snapshot of this component can prove that a saved ID is gone.
export function reconcileRuntimePreferences(state: BonsaiState, projectId: string, authority: RuntimeAuthority): Partial<BonsaiState> {
  const project = state.terminalViewPreferences[projectId]
  const trees = state.worktrees.filter(tree => tree.projectId === projectId)
  const fallbackTrees = trees.filter(tree => !tree.missing)
  if (!project) {
    const worktreeId = authority.worktrees && state.dockWorktreeId && !trees.some(tree => tree.id === state.dockWorktreeId)
      ? fallbackTrees[0]?.id ?? '' : state.dockWorktreeId
    return state.activeProjectId === projectId ? activeRuntimePatch(state, projectId, worktreeId) : {}
  }
  const worktrees = Object.fromEntries(Object.entries(project.worktrees).flatMap(([worktreeId, previous]) => {
    if (worktreeId && authority.worktrees && !trees.some(tree => tree.id === worktreeId)) return []
    const open = previous.open.filter(ref => {
      if (!authority[ref.kind === 'agent' ? 'agents' : 'processes']) return true
      const scope = runtimeScope(state, ref.id)
      return scope?.ref.kind === ref.kind && scope.projectId === projectId && scope.worktreeId === worktreeId
        && (ref.kind !== 'agent' || !state.agents.find(agent => agent.id === ref.id)?.archived)
    })
    const active = open.find(ref => same(ref, previous.active)) ?? open[0] ?? null
    return [[worktreeId, { open, active }]]
  }))
  const lastWorktreeId = authority.worktrees && project.lastWorktreeId && !trees.some(tree => tree.id === project.lastWorktreeId)
    ? (fallbackTrees.find(tree => !tree.main) ?? fallbackTrees[0])?.id ?? '' : project.lastWorktreeId
  const terminalViewPreferences = shareEqual(state.terminalViewPreferences, {
    ...state.terminalViewPreferences, [projectId]: { lastWorktreeId, worktrees },
  })
  const dockWorktreeId = state.activeProjectId === projectId && authority.worktrees && state.dockWorktreeId && !trees.some(tree => tree.id === state.dockWorktreeId)
    ? lastWorktreeId : state.dockWorktreeId
  return changedPatch(state, {
    terminalViewPreferences,
    ...(state.activeProjectId === projectId ? activeRuntimePatch({ ...state, terminalViewPreferences }, projectId, dockWorktreeId) : {}),
  })
}
