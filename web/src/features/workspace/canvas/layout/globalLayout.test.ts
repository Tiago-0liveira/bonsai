import type { Edge, Node } from '@xyflow/react'
import { describe, expect, it } from 'vitest'
import { getNodeRect, getNodeSize, LAYOUT, rectsOverlap } from './geometry'
import { buildBranchForest } from './graphModel'
import type { CanvasPosition, NodePlacements } from './types'
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

function expectNoOverlaps(nodes: Node[], positions: Record<string, CanvasPosition>) {
  for (let i = 0; i < nodes.length; i += 1) {
    const a = nodes[i]
    expect(positions[a.id], a.id).toBeDefined()
    for (const b of nodes.slice(i + 1)) {
      const aRect = { ...getNodeSize(a), ...a.measured, ...positions[a.id] }
      const bRect = { ...getNodeSize(b), ...b.measured, ...positions[b.id] }
      expect(rectsOverlap(aRect, bRect), a.id + ' overlaps ' + b.id).toBe(false)
    }
  }
}

function generated(positions: Record<string, CanvasPosition>): NodePlacements {
  return Object.fromEntries(Object.entries(positions).map(([id, position]) =>
    [id, { ...position, mode: 'generated' }]))
}

describe('Auto-layout spacing regressions', () => {
  it('reserves measured History height and wraps variable-size agents above child branches', () => {
    const { nodes, edges } = baseGraph()
    nodes.find((item) => item.id === 'a')!.measured = { width: 230, height: 440 }
    const agents = Array.from({ length: 7 }, (_, index) => ({
      ...node('agent-' + index, 'agent'),
      measured: { width: 188 + index * 20, height: index === 1 ? 240 : 98 },
    }))
    nodes.push(...agents)
    edges.push(...agents.map((agent) => edge('a', agent.id, 'agent')))
    const positions = computeGlobalPlacements(nodes, edges)
    expectNoOverlaps(nodes, positions)
    expect(positions['agent-0'].y - positions.a.y).toBe(440 + LAYOUT.agentTopGap)
    expect(positions['agent-3'].y - positions['agent-0'].y).toBe(240 + LAYOUT.agentGapY)
    expect(positions.nested.y - positions['agent-6'].y).toBe(98 + LAYOUT.childTopGap)
    expect(computeGlobalPlacements(nodes, edges, generated(positions))).toEqual(positions)
  })

  it('leaves space below every header companion', () => {
    const { nodes, edges } = baseGraph()
    nodes.find((item) => item.type === 'defaultBranch')!.measured = { width: 232, height: 430 }
    nodes.find((item) => item.type === 'env')!.measured = { width: 150, height: 500 }
    const positions = computeGlobalPlacements(nodes, edges)
    expectNoOverlaps(nodes, positions)
    expect(positions.a.y).toBe(positions['env:project'].y + 500 + LAYOUT.projectTopGap)
  })

  it('reserves all stack rows before measurements exist', () => {
    const nodes = [node('project', 'project'), node('stack', 'stack', { stackCount: 12 }), node('child', 'worktree')]
    const edges = [edge('project', 'stack', 'hierarchy'), edge('stack', 'child', 'hierarchy')]
    const positions = computeGlobalPlacements(nodes, edges)
    expect(positions.child.y - positions.stack.y).toBeGreaterThanOrEqual(50 + 12 * 34 + LAYOUT.childTopGap)
  })

  it('places disconnected cards below a deep tree with room for their actual widths', () => {
    const { nodes, edges } = baseGraph()
    nodes.find((item) => item.id === 'nested')!.measured = { width: 230, height: 600 }
    nodes.push(
      { ...node('loose-a', 'agent'), measured: { width: 350, height: 120 } },
      { ...node('loose-b', 'agent'), measured: { width: 420, height: 150 } },
    )
    const positions = computeGlobalPlacements(nodes, edges)
    expectNoOverlaps(nodes, positions)
    expect(positions['loose-a'].y).toBe(positions.nested.y + 600 + LAYOUT.childTopGap)
    expect(positions['loose-b'].x - positions['loose-a'].x).toBe(350 + LAYOUT.branchGapX)
  })

  it('orders disconnected branches together with connected roots on consecutive runs', () => {
    const { nodes, edges } = baseGraph()
    nodes.push(node('orphan', 'worktree', { tag: 'aaa' }))
    const previous: NodePlacements = {
      orphan: { x: -500, y: 300, mode: 'manual' },
      a: { x: 0, y: 300, mode: 'manual' },
      b: { x: 500, y: 300, mode: 'manual' },
    }
    const positions = computeGlobalPlacements(nodes, edges, previous)
    expect(positions.orphan.x).toBeLessThan(positions.a.x)
    expect(positions.a.x).toBeLessThan(positions.b.x)
    expect(computeGlobalPlacements([...nodes].reverse(), [...edges].reverse(), generated(positions))).toEqual(positions)
  })

  it('builds each branch and agent once despite duplicate edges and cycles', () => {
    const nodes = [node('project', 'project'), ...['a', 'b', 'c', 'orphan'].map((id) => node(id, 'worktree')), node('agent', 'agent')]
    const edges = [
      edge('a', 'b', 'hierarchy'), edge('b', 'a', 'hierarchy'),
      edge('b', 'c', 'hierarchy'), edge('b', 'c', 'hierarchy'),
      edge('project', 'c', 'hierarchy'), edge('missing', 'orphan', 'hierarchy'),
      edge('a', 'agent', 'agent'), edge('a', 'agent', 'agent'), edge('b', 'agent', 'agent'),
    ]
    const forest = buildBranchForest(nodes, edges, {})
    const ids: string[] = []
    const visit = (blocks: typeof forest) => blocks.forEach((block) => {
      ids.push(block.id, ...block.agentNodes.map((agent) => agent.id))
      visit(block.childBlocks)
    })
    visit(forest)
    expect(ids.sort()).toEqual(['a', 'agent', 'b', 'c', 'orphan'])
    const positions = computeGlobalPlacements(nodes, edges)
    expectNoOverlaps(nodes, positions)
    expect(positions.b.y).toBeGreaterThan(positions.a.y)
    expect(positions.c.y).toBeGreaterThan(positions.b.y)
    expect(computeGlobalPlacements([...nodes].reverse(), [...edges].reverse())).toEqual(positions)
    expect(computeGlobalPlacements(nodes, edges, generated(positions))).toEqual(positions)
  })

  it('falls back to estimates for invalid or partial measurements', () => {
    const { nodes, edges } = baseGraph()
    const baseline = computeGlobalPlacements(nodes, edges)
    nodes.forEach((item) => { item.measured = { width: 0, height: Number.NaN } })
    expect(computeGlobalPlacements(nodes, edges)).toEqual(baseline)
    nodes.find((item) => item.id === 'a')!.measured = { height: 450 }
    const positions = computeGlobalPlacements(nodes, edges)
    expect(positions.nested.y - positions.a.y).toBe(450 + LAYOUT.childTopGap)
  })
})
