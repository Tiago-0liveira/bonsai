import type { Process, RuntimeReference } from '../types'
import type { BonsaiState } from './bonsai'
import { changedPatch } from './reconciliation'

export const processKey = (p: Process) => p.commandKey ?? p.id
export const executionOrder = (p: Process) => p.executionOrder ?? p.daemonId
export const processDeleted = (state: BonsaiState, project: string, id: number) => !!state.processVisibility?.[project]?.deleted?.[id]
export const processActive = (p: Pick<Process, 'lifecycleStatus'>) => ['starting', 'running', 'backoff', 'stopping', 'orphan'].includes(p.lifecycleStatus)

export function visibleProcesses(state: BonsaiState, projectId: string): Process[] {
  const latest = new Map<string, Process>()
  const visibility = state.processVisibility?.[projectId]
  for (const p of state.processes) {
    if (p.projectId !== projectId || visibility?.deleted?.[p.daemonId]
      || (p.commandKey && executionOrder(p) <= (visibility?.cutoffs?.[p.commandKey] ?? 0))) continue
    const key = processKey(p)
    const previous = latest.get(key)
    if (!previous || executionOrder(p) > executionOrder(previous)
      || (executionOrder(p) === executionOrder(previous) && p.daemonId > previous.daemonId)) latest.set(key, p)
  }
  return [...latest.values()]
}

// Remap only existing intent. Authority traffic never opens a closed command.
export function remapProcessReferences(state: BonsaiState, previous: BonsaiState, projectId: string): Partial<BonsaiState> {
  const latest = new Map(visibleProcesses(state, projectId).map(p => [processKey(p), p]))
  const lookup = (ref: RuntimeReference): RuntimeReference | undefined => {
    if (ref.kind !== 'process') return ref
    const old = previous.processes.find(p => p.id === ref.id) ?? state.processes.find(p => p.id === ref.id)
    if (old && old.projectId !== projectId) return ref
    const key = ref.commandKey ?? (old && processKey(old))
    const p = key && latest.get(key)
    return p ? { kind: 'process', id: p.id, ...(p.commandKey ? { commandKey: p.commandKey } : {}) } : old ? undefined : ref
  }
  const remapId = (id: string) => lookup({ kind: 'process', id })?.id ?? ''
  const project = state.terminalViewPreferences[projectId]
  const worktrees = project && Object.fromEntries(Object.entries(project.worktrees).map(([id, view]) => {
    const seen = new Set<string>()
    const open = view.open.flatMap(ref => { const next = lookup(ref); if (!next || seen.has(next.id)) return []; seen.add(next.id); return [next] })
    const selected = view.active && lookup(view.active)
    return [id, { open, active: open.find(ref => ref.id === selected?.id) ?? open[0] ?? null }]
  }))
  const nodePlacements = { ...state.nodePlacements }
  for (const old of [...state.processes, ...previous.processes].filter(p => p.projectId === projectId)) {
    const next = latest.get(processKey(old))
    if (next?.id === old.id) continue
    if (next && previous.nodePlacements[old.id]) nodePlacements[next.id] = previous.nodePlacements[old.id]
    delete nodePlacements[old.id]
  }
  for (const view of Object.values(previous.terminalViewPreferences[projectId]?.worktrees ?? {})) {
    for (const ref of view.open) {
      if (ref.kind !== 'process' || !ref.commandKey) continue
      const next = latest.get(ref.commandKey)
      if (next?.id === ref.id) continue
      if (next && previous.nodePlacements[ref.id]) nodePlacements[next.id] = previous.nodePlacements[ref.id]
      delete nodePlacements[ref.id]
    }
  }
  const selected = previous.selection.type === 'process' && previous.processes.some(p => p.id === previous.selection.id && p.projectId === projectId)
    ? remapId(previous.selection.id) : undefined
  const reveal = previous.canvasReveal.projectId === projectId && previous.processes.some(p => p.id === previous.canvasReveal.nodeId)
    ? remapId(previous.canvasReveal.nodeId) : undefined
  return changedPatch(state, {
    ...(worktrees ? { terminalViewPreferences: { ...state.terminalViewPreferences, [projectId]: { ...project, worktrees } } } : {}),
    nodePlacements,
    visitOpenedRuntimeIds: [...new Set((state.visitOpenedRuntimeIds ?? []).map(id => remapId(id)).filter(Boolean))],
    ...(selected !== undefined ? { selection: selected ? { type: 'process' as const, id: selected } : { type: 'project' as const, id: state.activeProjectId } } : {}),
    ...(reveal !== undefined ? { canvasReveal: { ...state.canvasReveal, nodeId: reveal } } : {}),
  })
}
