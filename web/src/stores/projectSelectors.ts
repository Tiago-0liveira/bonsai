import { useMemo } from 'react'
import { useBonsaiStore, type BonsaiState } from './bonsai'
import type { Agent } from '../types'
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
export const projectProcessesSelector = (id: string) => subsetSelector(state => state.processes, process => process.projectId === id)
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
export const useProjectAgents = (id: string) => useBonsaiStore(useMemo(() => projectAgentsSelector(id), [id]))
export const useProjectPullRequests = (id: string) => useBonsaiStore(useMemo(() => projectPullRequestsSelector(id), [id]))

export function projectCanvasPreferencesSelector(id: string) {
  const trees = projectWorktreesSelector(id)
  const agents = projectAgentsSelector(id)
  let previousInputs: unknown[] = []
  let result: Pick<BonsaiState, 'nodePlacements' | 'collapsedTagGroups' | 'detachedStackWorktreeIds' | 'expandedAutomaticGroups'> = {
    nodePlacements: {}, collapsedTagGroups: [], detachedStackWorktreeIds: [], expandedAutomaticGroups: [],
  }
  return (state: BonsaiState) => {
    const currentTrees = trees(state)
    const currentAgents = agents(state)
    const groups = state.worktreeGroups[id]
    const inputs = [currentTrees, currentAgents, groups, state.nodePlacements, state.collapsedTagGroups, state.detachedStackWorktreeIds, state.expandedAutomaticGroups]
    if (inputs.every((value, index) => value === previousInputs[index])) return result
    previousInputs = inputs
    const treeIds = new Set(currentTrees.map(tree => tree.id))
    const nodeIds = new Set([id, ...treeIds, ...currentAgents.map(agent => agent.id)])
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
