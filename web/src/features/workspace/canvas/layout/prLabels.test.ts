import type { Edge, Node } from '@xyflow/react'
import { describe, expect, it } from 'vitest'
import { computeGlobalPlacements } from './globalLayout'
import { getNodeRect, rectsOverlap } from './geometry'
import { placePrLabels, PR_LABEL_SIZE } from './prLabels'

function graph() {
  const nodes: Node[] = [
    { id: 'project', type: 'project', position: { x: 0, y: 0 }, data: {} },
    { id: 'parent', type: 'worktree', position: { x: 0, y: 0 }, data: {} },
    { id: 'child', type: 'worktree', position: { x: 0, y: 0 }, data: {} },
    ...Array.from({ length: 4 }, (_, index) => ({
      id: 'agent-' + index, type: 'agent', position: { x: 0, y: 0 }, data: {},
    })),
  ]
  const edges: Edge[] = [
    { id: 'root', source: 'project', target: 'parent', data: { relationship: 'hierarchy' } },
    { id: 'nested', source: 'parent', target: 'child', data: { relationship: 'hierarchy' } },
    { id: 'pr', source: 'child', target: 'parent', data: { relationship: 'merge-pr' } },
    ...nodes.filter((node) => node.type === 'agent').map((node) => ({
      id: node.id, source: 'parent', target: node.id, data: { relationship: 'agent' },
    })),
  ]
  const positions = computeGlobalPlacements(nodes, edges)
  return { nodes: nodes.map((node) => ({ ...node, position: positions[node.id] })), edges }
}

function rect(center: { x: number; y: number }) {
  return { ...PR_LABEL_SIZE, x: center.x - PR_LABEL_SIZE.width / 2, y: center.y - PR_LABEL_SIZE.height / 2 }
}

describe('PR connection labels', () => {
  it('finds clear space on the connection when four agents occupy its midpoint', () => {
    const { nodes, edges } = graph()
    const before = structuredClone(nodes)
    const label = placePrLabels(nodes, edges).pr
    expect(label).toBeDefined()
    expect(label.anchor).toBeUndefined()
    nodes.forEach((node) => expect(rectsOverlap(rect(label), getNodeRect(node, node.position), 8), node.id).toBe(false))
    expect(nodes).toEqual(before)
  })

  it('avoids other labels and stays deterministic when edge order changes', () => {
    const { nodes, edges } = graph()
    edges.push({ ...edges.find((edge) => edge.id === 'pr')!, id: 'pr-2' })
    const labels = placePrLabels(nodes, edges)
    expect(rectsOverlap(rect(labels.pr), rect(labels['pr-2']), 8)).toBe(false)
    expect(placePrLabels(nodes, [...edges].reverse())).toEqual(labels)
  })

  it('uses a connected callout if the curve has no room for a label', () => {
    const { nodes, edges } = graph()
    nodes.push({ id: 'obstacle', type: 'worktree', data: {}, position: { x: -500, y: 0 },
      measured: { width: 3000, height: 3000 } })
    const label = placePrLabels(nodes, edges).pr
    expect(label.anchor).toBeDefined()
    nodes.forEach((node) => expect(rectsOverlap(rect(label), getNodeRect(node, node.position), 8)).toBe(false))
  })
})
