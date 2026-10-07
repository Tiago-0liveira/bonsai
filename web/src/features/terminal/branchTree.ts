import type { Agent, Worktree } from '../../types'
import type { BonsaiState } from '../../stores/bonsai'
import { projectAgentsSelector, projectWorktreesSelector } from '../../stores/projectSelectors'
import { shareEqual } from '../../stores/reconciliation'

export interface BranchTreeIndex {
  count: number
  defaultId?: string
  roots: string[]
  children: Record<string, string[]>
  canvasAgents: Record<string, string[]>
  historyAgents: Record<string, string[]>
}

export function buildBranchTreeIndex(trees: Worktree[], agents: Agent[], defaultBranch: string): BranchTreeIndex {
  const index: BranchTreeIndex = { count: trees.length, defaultId: trees.find(tree => tree.branch === defaultBranch)?.id, roots: [], children: {}, canvasAgents: {}, historyAgents: {} }
  const branches = new Map<string, string[]>()
  for (const tree of trees) {
    branches.set(tree.branch, [...(branches.get(tree.branch) ?? []), tree.id])
    if (tree.branch !== defaultBranch && tree.mergeTargetBranch === defaultBranch) index.roots.push(tree.id)
  }
  for (const tree of trees) for (const parent of branches.get(tree.mergeTargetBranch) ?? []) {
    if (parent !== tree.id) (index.children[parent] ??= []).push(tree.id)
  }
  for (const agent of agents) {
    const presentation = agent.presentation ?? (agent.archived ? 'archived' : 'canvas')
    if (presentation === 'canvas') (index.canvasAgents[agent.worktreeId] ??= []).push(agent.id)
    if (presentation === 'history') (index.historyAgents[agent.worktreeId] ??= []).push(agent.id)
  }
  return index
}

export function branchTreeSelector(projectId: string) {
  const trees = projectWorktreesSelector(projectId)
  const agents = projectAgentsSelector(projectId)
  let previousInputs: unknown[] = []
  let index: BranchTreeIndex = { count: 0, roots: [], children: {}, canvasAgents: {}, historyAgents: {} }
  return (state: BonsaiState) => {
    const currentTrees = trees(state)
    const currentAgents = agents(state)
    const branch = state.projects.find(project => project.id === projectId)?.defaultBranch ?? ''
    const inputs = [currentTrees, currentAgents, branch]
    if (inputs.every((value, i) => value === previousInputs[i])) return index
    previousInputs = inputs
    index = shareEqual(index, buildBranchTreeIndex(currentTrees, currentAgents, branch))
    return index
  }
}

const treeMaps = new WeakMap<Worktree[], Map<string, Worktree>>()
const agentMaps = new WeakMap<Agent[], Map<string, Agent>>()
export function worktreeById(trees: Worktree[], id: string) {
  let map = treeMaps.get(trees)
  if (!map) { map = new Map(trees.map(tree => [tree.id, tree])); treeMaps.set(trees, map) }
  return map.get(id)
}
export function agentById(agents: Agent[], id: string) {
  let map = agentMaps.get(agents)
  if (!map) { map = new Map(agents.map(agent => [agent.id, agent])); agentMaps.set(agents, map) }
  return map.get(id)
}
