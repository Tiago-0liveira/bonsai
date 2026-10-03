import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Edge, Node } from '@xyflow/react'
import { buildCanvasGraph, type CanvasGraphInput } from './buildCanvasGraph'
import { applyCanvasSelection, createPrLabelSelector, reconcileCanvasNodes } from './graphReconciliation'
import { reconcileById } from '../../../stores/reconciliation'
import { projects } from '../../../test/fixtures/projects'
import { worktrees } from '../../../test/fixtures/worktrees'
import * as labelGeometry from './layout/prLabels'

const input = (): CanvasGraphInput => ({ project: projects[0], worktrees: worktrees.filter(tree => tree.projectId === projects[0].id), agents: [], tags: [], collapsedTagGroups: [], detachedStackWorktreeIds: [], expandedAutomaticGroups: [], nodePlacements: {}, envCount: 0 })
beforeEach(() => vi.restoreAllMocks())
describe('incremental canvas identity', () => {
  it('retains unrelated node/data/geometry references on a worktree status update', () => {
    const first = buildCanvasGraph(input())
    const measured = { width: 230, height: 160 }
    const current = first.nodes.map(node => ({ ...node, measured, selected: node.id === 'wt-daemon' }))
    const nextInput = input()
    nextInput.worktrees = nextInput.worktrees.map(tree => tree.id === 'wt-daemon' ? { ...tree, status: 'healthy', ciStatus: 'passed', ciFailed: 0 } : tree)
    const next = buildCanvasGraph(nextInput)
    const reconciled = reconcileCanvasNodes(current, next.nodes, first.nodes)
    for (const node of current.filter(node => node.id !== 'wt-daemon' && node.type !== 'project')) expect(reconciled.find(value => value.id === node.id)).toBe(node)
    const changed = reconciled.find(node => node.id === 'wt-daemon')!
    expect(changed.data.health).toBe('healthy')
    expect(changed.position).toBe(current.find(node => node.id === changed.id)!.position)
    expect(changed.measured).toBe(measured)
    expect(changed.selected).toBe(true)
    expect(next.topologyKey).toBe(first.topologyKey)
    expect(reconcileById(first.edges, next.edges)).toBe(first.edges)
  })

  it('keeps the entire node collection on an equivalent projection', () => {
    const first = buildCanvasGraph(input())
    expect(reconcileCanvasNodes(first.nodes, buildCanvasGraph(input()).nodes, first.nodes)).toBe(first.nodes)
  })

  it('preserves local drag and companion positions on status updates, applying actual canonical placement changes', () => {
    const first = buildCanvasGraph(input())
    const local = first.nodes.map(node => ({ ...node, position: { x: node.position.x + 100, y: node.position.y + 20 } }))
    const next = buildCanvasGraph(input())
    expect(reconcileCanvasNodes(local, next.nodes, first.nodes)).toBe(local)
    const movedInput = input()
    movedInput.nodePlacements['wt-daemon'] = { x: 700, y: 450, mode: 'manual' }
    const moved = reconcileCanvasNodes(local, buildCanvasGraph(movedInput).nodes, first.nodes)
    expect(moved.find(node => node.id === 'wt-daemon')?.position).toEqual({ x: 700, y: 450 })
    for (const node of local.filter(node => node.id !== 'wt-daemon')) expect(moved.find(value => value.id === node.id)).toBe(node)
  })

  it('updates only old/new selected nodes and does not change data or measurement', () => {
    const graph = buildCanvasGraph(input())
    const first = applyCanvasSelection(graph.nodes, { type: 'worktree', id: 'wt-daemon' })
    const second = applyCanvasSelection(first, { type: 'worktree', id: 'wt-web' })
    for (const node of first.filter(node => node.id !== 'wt-daemon' && node.id !== 'wt-web')) expect(second.find(value => value.id === node.id)).toBe(node)
    expect(second.find(node => node.id === 'wt-web')?.data).toBe(first.find(node => node.id === 'wt-web')?.data)
    expect(applyCanvasSelection(second, { type: 'worktree', id: 'wt-web' })).toBe(second)
  })
})

describe('PR label geometry triggers', () => {
  const nodes: Node[] = [
    { id: 'parent', type: 'worktree', position: { x: 0, y: 0 }, data: { kind: 'worktree', entityId: 'parent' } },
    { id: 'child', type: 'worktree', position: { x: 400, y: 300 }, data: { kind: 'worktree', entityId: 'child' } },
  ]
  const edges: Edge[] = [{ id: 'pr', source: 'child', target: 'parent', data: { relationship: 'merge-pr' } }]
  it('skips placement and retains displayed edges on selection and status changes', () => {
    const place = vi.spyOn(labelGeometry, 'placePrLabels')
    const select = createPrLabelSelector()
    const first = select(nodes, edges)
    const selected = applyCanvasSelection(nodes, { type: 'worktree', id: 'child' })
    expect(select(selected, edges)).toBe(first)
    expect(select(nodes.map(node => ({ ...node, data: { ...node.data, health: 'warning' } })), edges)).toBe(first)
    expect(place).toHaveBeenCalledTimes(1)
  })
  it('recomputes for drag geometry, measured History expansion, and edge topology', () => {
    const place = vi.spyOn(labelGeometry, 'placePrLabels')
    const select = createPrLabelSelector()
    select(nodes, edges)
    const moved = nodes.map(node => ({ ...node, position: { ...node.position, x: node.position.x + 30 } }))
    select(moved, edges)
    const expanded = moved.map(node => ({ ...node, measured: { width: 230, height: 250 } }))
    select(expanded, edges)
    select(expanded, [])
    expect(place).toHaveBeenCalledTimes(4)
  })
})
