import type { Edge, Node } from '@xyflow/react'
import { describe, expect, it } from 'vitest'
import { getNodeRect, rectsOverlap } from './geometry'
import {
  placeAddedNodesLocally,
  placeExpandedStackLocally,
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
