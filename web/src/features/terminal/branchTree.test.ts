import { expect, it } from 'vitest'
import { buildBranchTreeIndex, branchTreeSelector } from './branchTree'
import { useBonsaiStore } from '../../stores/bonsai'
import { worktrees } from '../../test/fixtures/worktrees'
import { projects } from '../../test/fixtures/projects'
import { agents } from '../../mock/agents'

it('indexes children and agent presentation once, including finite cyclic relationships', () => {
  const trees = worktrees.filter(tree => tree.projectId === 'bonsai')
  const index = buildBranchTreeIndex(trees, agents, 'main')
  expect(index.children['wt-web']).toContain('wt-docs')
  expect(index.canvasAgents['wt-web']).toContain('agent-ui')
  expect(index.historyAgents['wt-web']).toContain('agent-history-a')
  const cycle = [{ ...trees[1], id: 'a', branch: 'a', mergeTargetBranch: 'b' }, { ...trees[1], id: 'b', branch: 'b', mergeTargetBranch: 'a' }]
  expect(buildBranchTreeIndex(cycle, [], 'main').children).toEqual({ a: ['b'], b: ['a'] })
})

it('retains the whole structural index for status, selection, and inactive project updates', () => {
  const select = branchTreeSelector('bonsai')
  const state = { ...useBonsaiStore.getInitialState(), projects, worktrees, agents }
  const first = select(state)
  const status = { ...state, worktrees: worktrees.map(tree => ({ ...tree, status: 'warning' as const })), agents: agents.map(agent => ({ ...agent, state: 'idle' as const })) }
  expect(select(status)).toBe(first)
  expect(select({ ...status, dockWorktreeId: 'wt-docs' })).toBe(first)
  const topology = select({ ...status, worktrees: status.worktrees.map(tree => tree.id === 'wt-docs' ? { ...tree, mergeTargetBranch: 'main' } : tree) })
  expect(topology).not.toBe(first)
  expect(topology.children['wt-web']).toBeUndefined()
  expect(topology.roots).toContain('wt-docs')
})
