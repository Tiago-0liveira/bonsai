import { describe, expect, it } from 'vitest'
import { buildCanvasGraph } from './buildCanvasGraph'
import { projects } from '../../../test/fixtures/projects'
import { worktrees } from '../../../test/fixtures/worktrees'
import type { Agent, AgentActivity } from '../../../types'

const live = (id: string, worktreeId: string, activity?: AgentActivity): Agent => ({
  id, worktreeId, projectId: 'bonsai', name: id, provider: 'Claude', providerId: 'claude', model: '', reasoningEffort: '', workType: '', prompt: '', archived: false, presentation: 'canvas',
  state: 'running', lifecycleState: 'running', activity, task: 'running', runtime: '', terminalId: id, createdAt: 'now',
})
const wait: AgentActivity = { state: 'waiting', reason: 'permission', since: '2099-01-01T00:00:00Z', source: 'hook' }
const idle: AgentActivity = { state: 'idle', since: '2099-01-01T00:00:00Z', source: 'hook' }
const graph = (agents: Agent[]) => buildCanvasGraph({ project: projects[0], worktrees, agents, processes: [], detachedStackWorktreeIds: [], expandedAutomaticGroups: [], nodePlacements: {} })

describe('agent status on the canvas', () => {
  it('counts a worktree\'s waiting agents for its attention marker', () => {
    const { nodes } = graph([live('a', 'wt-web', wait), live('b', 'wt-web', idle), live('c', 'wt-web')])
    expect(nodes.find(node => node.id === 'wt-web')?.data.needsInput).toBe(1)
  })

  it('keeps idle and waiting agents alive for the live counters and the edge style', () => {
    const { nodes, edges } = graph([live('a', 'wt-web', wait), live('b', 'wt-web', idle)])
    expect(edges.filter(edge => edge.data?.relationship === 'agent').map(edge => edge.style?.strokeDasharray)).toEqual([undefined, undefined])
    const stats = nodes.find(node => node.type === 'project')?.data.stats as Array<{ label: string; value: number }>
    expect(stats.find(stat => stat.label === 'running')?.value).toBe(2)
  })
})
