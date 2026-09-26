import type { Edge, Node } from '@xyflow/react'
import { describe, expect, it } from 'vitest'
import { getNodeRect } from './geometry'
import { computeGlobalPlacements } from './globalLayout'

function node(id: string, type: string, data: Record<string, unknown> = {}): Node {
  return { id, type, position: { x: 0, y: 0 }, data: { title: id, ...data } }
}

function edge(source: string, target: string, relationship: 'hierarchy' | 'agent' | 'merge-pr'): Edge {
  return {
    id: relationship + ':' + source + '>' + target,
    source,
    target,
    data: { relationship },
  }
}

function baseGraph() {
  const nodes = [
    node('project', 'project'),
    node('default:project', 'defaultBranch'),
    node('env:project', 'env'),
    node('a', 'worktree', { tag: 'feat' }),
    node('b', 'worktree', { tag: 'bug' }),
    node('nested', 'worktree', { tag: 'feat' }),
  ]
  const edges = [
    edge('project', 'a', 'hierarchy'),
    edge('project', 'b', 'hierarchy'),
    edge('a', 'nested', 'hierarchy'),
  ]
  return { nodes, edges }
}

describe('branch-block global layout', () => {
  it('is deterministic for identical topology', () => {
    const { nodes, edges } = baseGraph()
    const first = computeGlobalPlacements(nodes, edges)
    const second = computeGlobalPlacements(nodes, edges)
    expect(second).toEqual(first)
  })

  it('keeps default branch left, env right, and nested worktrees below their parent', () => {
    const { nodes, edges } = baseGraph()
    const positions = computeGlobalPlacements(nodes, edges)

    expect(positions['default:project'].x).toBeLessThan(positions.project.x)
    expect(positions['env:project'].x).toBeGreaterThan(positions.project.x)
    expect(positions.nested.y).toBeGreaterThan(positions.a.y)
  })

  it('does not let PR overlay edges influence placement', () => {
    const { nodes, edges } = baseGraph()
    const withoutPr = computeGlobalPlacements(nodes, edges)
    const withPr = computeGlobalPlacements(nodes, [
      ...edges,
      edge('nested', 'a', 'merge-pr'),
    ])
    expect(withPr).toEqual(withoutPr)
  })

  it('keeps sibling branch blocks separated', () => {
    const { nodes, edges } = baseGraph()
    const positions = computeGlobalPlacements(nodes, edges)
    const a = nodes.find((item) => item.id === 'a') as Node
    const b = nodes.find((item) => item.id === 'b') as Node
    const aRect = getNodeRect(a, positions.a)
    const bRect = getNodeRect(b, positions.b)

    const separated =
      aRect.x + aRect.width <= bRect.x ||
      bRect.x + bRect.width <= aRect.x
    expect(separated).toBe(true)
  })

  it('lays six agents in a local wrapped shelf beneath their worktree', () => {
    const worktree = node('a', 'worktree', { tag: 'feat' })
    const agents = Array.from({ length: 6 }, (_, index) => node('agent-' + index, 'agent'))
    const nodes = [node('project', 'project'), worktree, ...agents]
    const edges = [
      edge('project', 'a', 'hierarchy'),
      ...agents.map((agentNode) => edge('a', agentNode.id, 'agent')),
    ]
    const positions = computeGlobalPlacements(nodes, edges)

    agents.forEach((agentNode) => {
      expect(positions[agentNode.id].y).toBeGreaterThan(positions.a.y)
    })
    expect(new Set(agents.map((agentNode) => positions[agentNode.id].x)).size).toBe(3)
    expect(new Set(agents.map((agentNode) => positions[agentNode.id].y)).size).toBe(2)
  })
})
