import type { Edge, Node } from '@xyflow/react'
import { describe, expect, it } from 'vitest'
import { computeGlobalPlacements } from './globalLayout'
import { getNodeRect, rectsOverlap } from './geometry'
import {
  placeAddedNodesLocally,
  placeExpandedStackLocally,
  placeMissingNodes,
  refreshGeneratedAgentShelves,
  relocateGeneratedBranches,
} from './localPlacement'
import type { NodePlacements } from './types'

function node(id: string, type: string, data: Record<string, unknown> = {}): Node {
  return { id, type, position: { x: 0, y: 0 }, data }
}

function edge(source: string, target: string, relationship: 'hierarchy' | 'agent'): Edge {
  return {
    id: relationship + ':' + source + '>' + target,
    source,
    target,
    data: { relationship },
  }
}

describe('local canvas placement', () => {
  it('places a newly detached worktree beside a still-visible stack without prior coordinates', () => {
    const nodes = [
      node('project', 'project'),
      node('stack:project:feat', 'stack', { tag: 'feat', stackItems: [{ id: 'b' }, { id: 'c' }] }),
      node('a', 'worktree', { tag: 'feat' }),
    ]
    const placements: NodePlacements = {
      project: { x: 420, y: 34, mode: 'generated' },
      'stack:project:feat': { x: 300, y: 280, mode: 'generated' },
    }

    const result = placeAddedNodesLocally(nodes, placements, ['a'])
    expect(result.a).toBeDefined()
    expect(result.a.x).toBeGreaterThan(placements['stack:project:feat'].x)
  })

  it('recenters a newly collapsed stack on its member positions', () => {
    const nodes = [
      node('stack:project:feat', 'stack', { stackItems: [{ id: 'a' }, { id: 'b' }], stackCount: 2 }),
    ]
    const placements: NodePlacements = {
      a: { x: 100, y: 200, mode: 'manual' },
      b: { x: 500, y: 400, mode: 'generated' },
      'stack:project:feat': { x: 20, y: 20, mode: 'generated' },
    }

    const result = placeAddedNodesLocally(nodes, placements, ['stack:project:feat'])
    expect(result['stack:project:feat']).toBeDefined()
    expect(result['stack:project:feat'].x).toBeGreaterThan(100)
    expect(result['stack:project:feat'].y).toBeGreaterThan(100)
  })

  it('expands missing stack members around the disappearing stack anchor without moving unrelated nodes', () => {
    const nodes = [
      node('a', 'worktree', { tag: 'feat' }),
      node('b', 'worktree', { tag: 'feat' }),
      node('unrelated', 'worktree', { tag: 'bug' }),
    ]
    const placements: NodePlacements = {
      unrelated: { x: 900, y: 280, mode: 'manual' },
    }
    const result = placeExpandedStackLocally(nodes, placements, ['a', 'b'], { x: 300, y: 280 })

    expect(Object.keys(result).sort()).toEqual(['a', 'b'])
    expect(result.unrelated).toBeUndefined()
    const unrelatedRect = getNodeRect(nodes[2], placements.unrelated)
    expect(
      ['a', 'b'].some((id) =>
        rectsOverlap(getNodeRect(nodes.find((item) => item.id === id) as Node, result[id]), unrelatedRect, 28),
      ),
    ).toBe(false)
  })

  it('refreshes generated agent shelves while respecting a manually placed agent', () => {
    const nodes = [
      node('worktree', 'worktree'),
      node('manual-agent', 'agent'),
      node('generated-agent', 'agent'),
    ]
    const edges = [
      edge('worktree', 'manual-agent', 'agent'),
      edge('worktree', 'generated-agent', 'agent'),
    ]
    const placements: NodePlacements = {
      worktree: { x: 300, y: 250, mode: 'manual' },
      'manual-agent': { x: 40, y: 700, mode: 'manual' },
      'generated-agent': { x: 320, y: 450, mode: 'generated' },
    }

    const result = refreshGeneratedAgentShelves(nodes, edges, placements)
    expect(result['manual-agent']).toBeUndefined()
    expect(result['generated-agent']).toBeDefined()
  })

  it('places a new main-target worktree below the project without moving the project', () => {
    const nodes = [
      node('project', 'project'),
      node('created', 'worktree', { tag: 'feat' }),
    ]
    const edges = [edge('project', 'created', 'hierarchy')]
    const placements: NodePlacements = {
      project: { x: 420, y: 34, mode: 'manual' },
    }

    const result = placeMissingNodes(nodes, edges, placements, ['created'])

    expect(result.created).toBeDefined()
    expect(result.created.y).toBeGreaterThan(placements.project.y)
    expect(result.project).toBeUndefined()
  })

  it('places a new nested worktree below its merge parent', () => {
    const nodes = [
      node('project', 'project'),
      node('parent', 'worktree'),
      node('child', 'worktree'),
    ]
    const edges = [
      edge('project', 'parent', 'hierarchy'),
      edge('parent', 'child', 'hierarchy'),
    ]
    const placements: NodePlacements = {
      project: { x: 420, y: 34, mode: 'generated' },
      parent: { x: 360, y: 300, mode: 'manual' },
    }

    const result = placeMissingNodes(nodes, edges, placements, ['child'])

    expect(result.child).toBeDefined()
    expect(result.child.y).toBeGreaterThan(placements.parent.y)
    expect(result.parent).toBeUndefined()
  })

  it('routes a new worktree around a manually placed collision obstacle', () => {
    const nodes = [
      node('project', 'project'),
      node('obstacle', 'worktree'),
      node('created', 'worktree'),
    ]
    const edges = [
      edge('project', 'obstacle', 'hierarchy'),
      edge('project', 'created', 'hierarchy'),
    ]
    const placements: NodePlacements = {
      project: { x: 420, y: 34, mode: 'generated' },
      obstacle: { x: 455, y: 278, mode: 'manual' },
    }

    const result = placeMissingNodes(nodes, edges, placements, ['created'])

    expect(result.created).toBeDefined()
    const obstacleRect = getNodeRect(nodes[1], placements.obstacle)
    const createdRect = getNodeRect(nodes[2], result.created)
    expect(rectsOverlap(createdRect, obstacleRect, 28)).toBe(false)
    expect(placements.obstacle).toEqual({ x: 455, y: 278, mode: 'manual' })
  })

  it('does not relocate a manually placed branch when its merge parent changes', () => {
    const nodes = [
      node('project', 'project'),
      node('parent', 'worktree'),
      node('child', 'worktree'),
    ]
    const edges = [
      edge('project', 'parent', 'hierarchy'),
      edge('parent', 'child', 'hierarchy'),
    ]
    const placements: NodePlacements = {
      project: { x: 420, y: 34, mode: 'generated' },
      parent: { x: 400, y: 300, mode: 'generated' },
      child: { x: 100, y: 650, mode: 'manual' },
    }

    expect(relocateGeneratedBranches(nodes, edges, placements, ['child'])).toEqual({})
  })
})


describe('shelf changes after Auto-layout', () => {
  function graph() {
    const agents = ['a0', 'a1', 'a2'].map((id) => ({
      ...node(id, 'agent'), measured: { width: 188, height: 87 },
    }))
    const nodes = [node('project', 'project'), node('owner', 'worktree'),
      node('child', 'worktree'), node('sibling', 'worktree'),
      ...agents, node('sibling-agent', 'agent')]
    const edges = [edge('project', 'owner', 'hierarchy'), edge('owner', 'child', 'hierarchy'),
      edge('project', 'sibling', 'hierarchy'), edge('sibling', 'sibling-agent', 'agent'),
      ...agents.map((agent) => edge('owner', agent.id, 'agent'))]
    const positions = computeGlobalPlacements(nodes, edges)
    const placements: NodePlacements = Object.fromEntries(Object.entries(positions)
      .map(([id, position]) => [id, { ...position, mode: 'generated' }]))
    return { nodes, edges, placements }
  }

  it('keeps an expanded shelf together and collision-free without touching another branch', () => {
    const { nodes, edges, placements } = graph()
    nodes.push({ ...node('a3', 'agent'), measured: { width: 188, height: 87 } })
    edges.push(edge('owner', 'a3', 'agent'))
    const before = structuredClone(placements)
    const result = refreshGeneratedAgentShelves(nodes, edges, placements, new Set(['owner']))
    expect(Object.keys(result).sort()).toEqual(['a0', 'a1', 'a2', 'a3'])
    expect(placements).toEqual(before)
    expect(result.a1.x - result.a0.x).toBe(202)
    expect(result.a2.x - result.a1.x).toBe(202)
    expect(result.a3.x).toBe(result.a1.x)
    expect(result.a3.y - result.a0.y).toBe(99)
    const final = { ...placements, ...result }
    nodes.forEach((a, index) => nodes.slice(index + 1).forEach((b) => {
      expect(rectsOverlap(getNodeRect(a, final[a.id]), getNodeRect(b, final[b.id])),
        a.id + ' overlaps ' + b.id).toBe(false)
    }))
  })

  it('compacts a removal only in its owning shelf', () => {
    const { nodes, edges, placements } = graph()
    const result = refreshGeneratedAgentShelves(nodes.filter((item) => item.id !== 'a1'),
      edges.filter((item) => item.target !== 'a1'), placements, new Set(['owner']))
    expect(Object.keys(result).sort()).toEqual(['a0', 'a2'])
    expect(result.a0.y).toBe(result.a2.y)
    expect(result.a2.x - result.a0.x).toBe(202)
    expect((result.a0.x + result.a2.x + 188) / 2).toBe(placements.owner.x + 115)
  })

  it('uses measured History height and treats manual agents as fixed obstacles', () => {
    const { nodes, edges, placements } = graph()
    nodes.find((item) => item.id === 'owner')!.measured = { width: 230, height: 420 }
    placements.a1 = { ...placements.a1, mode: 'manual' }
    const result = refreshGeneratedAgentShelves(nodes, edges, placements, new Set(['owner']))
    expect(result.a1).toBeUndefined()
    expect(result.a0.y).toBe(placements.owner.y + 420 + 24)
    expect(rectsOverlap(getNodeRect(nodes.find((item) => item.id === 'a0')!, result.a0),
      getNodeRect(nodes.find((item) => item.id === 'a1')!, placements.a1))).toBe(false)
  })

  it('places a new child below an existing measured shelf', () => {
    const nodes = [node('owner', 'worktree'), node('agent', 'agent'), node('child', 'worktree')]
    nodes[1].measured = { width: 188, height: 150 }
    const edges = [edge('owner', 'agent', 'agent'), edge('owner', 'child', 'hierarchy')]
    const placements: NodePlacements = {
      owner: { x: 0, y: 0, mode: 'generated' }, agent: { x: 0, y: 450, mode: 'manual' },
    }
    const result = placeMissingNodes(nodes, edges, placements, ['child'])
    expect(result.child.y).toBe(670)
  })
})
