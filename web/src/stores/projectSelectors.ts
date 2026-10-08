import { useMemo } from 'react'
import { useBonsaiStore, type BonsaiState } from './bonsai'
import type { Agent, CanvasProcess, Process } from '../types'
import { visibleProcesses } from './processProjection'
import { shareEqual } from './reconciliation'

function retainSubset<T>(previous: T[], next: T[]) {
  return previous.length === next.length && next.every((value, index) => value === previous[index]) ? previous : next
}

function subsetSelector<T>(source: (state: BonsaiState) => T[], include: (value: T) => boolean) {
  let input: T[] | undefined
  let result: T[] = []
  return (state: BonsaiState) => {
    const next = source(state)
    if (next !== input) {
      input = next
      result = retainSubset(result, next.filter(include))
    }
    return result
  }
}

export const projectWorktreesSelector = (id: string) => subsetSelector(state => state.worktrees, tree => tree.projectId === id)
export const projectProcessInventorySelector = (id: string) => subsetSelector(state => state.processes, process => process.projectId === id)
export function projectProcessesSelector(id: string) {
  let input: Process[] | undefined, visibility: unknown
  let result: Process[] = []
  return (state: BonsaiState) => {
    if (input !== state.processes || visibility !== state.processVisibility?.[id]) {
      input = state.processes; visibility = state.processVisibility?.[id]
      result = retainSubset(result, visibleProcesses(state, id))
    }
    return result
  }
}

// Transport metadata (PID, revision, retry counters) does not change canvas
// topology or presentation. Subscribe only to the fields the graph projects.
export function projectCanvasProcessesSelector(id: string) {
  const processes = projectProcessesSelector(id)
  let input: Process[] | undefined
  let result: CanvasProcess[] = []
  return (state: BonsaiState) => {
    const next = processes(state)
    if (input !== next) {
      input = next
      result = shareEqual(result, next.map(({ id, projectId, worktreeId, name, command, status, lifecycleStatus }) => ({ id, projectId, worktreeId, name, command, status, lifecycleStatus })))
    }
    return result
  }
}

export function processNodeSelector(id: string) {
  let input: Process[] | undefined
  let result: Pick<Process, 'id' | 'projectId' | 'daemonId' | 'lifecycleStatus' | 'exitCode' | 'exitError' | 'port'> | undefined
  return (state: BonsaiState) => {
    if (input !== state.processes) {
      input = state.processes
      const process = input.find(process => process.id === id)
      result = shareEqual(result, process ? {
        id: process.id, projectId: process.projectId, daemonId: process.daemonId,
        lifecycleStatus: process.lifecycleStatus, exitCode: process.exitCode, exitError: process.exitError, port: process.port,
      } : undefined)
    }
    return result
  }
}
export const projectPullRequestsSelector = (id: string) => subsetSelector(state => state.pullRequests, pr => pr.id.startsWith(`${id}:`))

export function projectAgentsSelector(id: string) {
  const trees = projectWorktreesSelector(id)
  let previousTrees: ReturnType<typeof trees> | undefined
  let previousAgents: Agent[] | undefined
  let result: Agent[] = []
  return (state: BonsaiState) => {
    const currentTrees = trees(state)
    if (currentTrees !== previousTrees || state.agents !== previousAgents) {
      const ids = new Set(currentTrees.map(tree => tree.id))
      result = retainSubset(result, state.agents.filter(agent => ids.has(agent.worktreeId)))
      previousTrees = currentTrees
      previousAgents = state.agents
    }
    return result
  }
}

export const useProjectWorktrees = (id: string) => useBonsaiStore(useMemo(() => projectWorktreesSelector(id), [id]))
export const useProjectProcesses = (id: string) => useBonsaiStore(useMemo(() => projectProcessesSelector(id), [id]))
export const useProjectCanvasProcesses = (id: string) => useBonsaiStore(useMemo(() => projectCanvasProcessesSelector(id), [id]))
export const useProjectAgents = (id: string) => useBonsaiStore(useMemo(() => projectAgentsSelector(id), [id]))
export const useProjectPullRequests = (id: string) => useBonsaiStore(useMemo(() => projectPullRequestsSelector(id), [id]))

export function projectCanvasPreferencesSelector(id: string) {
  const trees = projectWorktreesSelector(id)
  const agents = projectAgentsSelector(id)
  const processes = projectProcessesSelector(id)
  let previousInputs: unknown[] = []
  let result: Pick<BonsaiState, 'nodePlacements' | 'collapsedTagGroups' | 'detachedStackWorktreeIds' | 'expandedAutomaticGroups'> = {
    nodePlacements: {}, collapsedTagGroups: [], detachedStackWorktreeIds: [], expandedAutomaticGroups: [],
  }
  return (state: BonsaiState) => {
    const currentTrees = trees(state)
    const currentAgents = agents(state)
    const currentProcesses = processes(state)
    const groups = state.worktreeGroups[id]
    const inputs = [currentTrees, currentAgents, currentProcesses, groups, state.nodePlacements, state.collapsedTagGroups, state.detachedStackWorktreeIds, state.expandedAutomaticGroups]
    if (inputs.every((value, index) => value === previousInputs[index])) return result
    previousInputs = inputs
    const treeIds = new Set(currentTrees.map(tree => tree.id))
    const nodeIds = new Set([id, `process-shelf:${id}`, ...treeIds, ...currentAgents.map(agent => agent.id), ...currentProcesses.map(process => process.id)])
    const groupIds = new Set(groups?.map(group => group.id))
    result = shareEqual(result, {
      nodePlacements: Object.fromEntries(Object.entries(state.nodePlacements).filter(([nodeId]) => nodeIds.has(nodeId) || nodeId.startsWith(`stack:${id}:`) || groupIds.has(nodeId.replace(/^stack:/, '')))),
      collapsedTagGroups: state.collapsedTagGroups.filter(key => key.startsWith(`${id}:`)),
      detachedStackWorktreeIds: state.detachedStackWorktreeIds.filter(key => treeIds.has(key)),
      expandedAutomaticGroups: state.expandedAutomaticGroups.filter(key => groupIds.has(key)),
    })
    return result
  }
}

export const useProjectCanvasPreferences = (id: string) => useBonsaiStore(useMemo(() => projectCanvasPreferencesSelector(id), [id]))
