import { describe, expect, it } from 'vitest'
import type { Edge, Node } from '@xyflow/react'
import { getDescendantIds, layoutGraph } from './layout'

const nodes: Node[] = [
  { id: 'project', type: 'project', position: { x: 0, y: 0 }, data: {} },
  { id: 'default:project', type: 'defaultBranch', position: { x: 0, y: 0 }, data: {} },
  { id: 'env:project', type: 'env', position: { x: 0, y: 0 }, data: {} },
  { id: 'parent', type: 'worktree', position: { x: 0, y: 0 }, data: {} },
  { id: 'child', type: 'worktree', position: { x: 0, y: 0 }, data: {} },
  { id: 'agent', type: 'agent', position: { x: 0, y: 0 }, data: {} },
]

const edges: Edge[] = [
  { id: 'default', source: 'default:project', target: 'project', data: { relationship: 'default' } },
  { id: 'parent', source: 'project', target: 'parent', data: { relationship: 'hierarchy' } },
  { id: 'child', source: 'parent', target: 'child', data: { relationship: 'hierarchy' } },
  { id: 'agent', source: 'child', target: 'agent', data: { relationship: 'agent' } },
]

describe('workspace hierarchy layout', () => {
  it('returns all descendants for group dragging', () => {
    expect(getDescendantIds('parent', edges)).toEqual(['child', 'agent'])
  })

  it('places nested merge targets below their parent branch', async () => {
    const laidOut = await layoutGraph(nodes, edges)
    const parent = laidOut.find((node) => node.id === 'parent')
    const child = laidOut.find((node) => node.id === 'child')
    const agent = laidOut.find((node) => node.id === 'agent')
    const project = laidOut.find((node) => node.id === 'project')
    const defaultBranch = laidOut.find((node) => node.id === 'default:project')
    const env = laidOut.find((node) => node.id === 'env:project')
    expect(child?.position.y).toBeGreaterThan(parent?.position.y ?? 0)
    expect(agent?.position.y).toBeGreaterThan(child?.position.y ?? 0)
    expect(defaultBranch?.position.x).toBeLessThan(project?.position.x ?? 0)
    expect(env?.position.x).toBeGreaterThan(project?.position.x ?? 0)
  })
})
