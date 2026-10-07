import type { Edge, Node } from '@xyflow/react'
import type { Selection } from '../../../types'
import { reconcileById, shareEqual } from '../../../stores/reconciliation'
import { getNodeRect } from './layout/geometry'
import { placePrLabels, type PrLabelPlacement } from './layout/prLabels'

export function reconcileCanvasNodes(current: Node[], projected: Node[], previousProjection: Node[]): Node[] {
  const byId = new Map(current.map(node => [node.id, node]))
  const previousById = new Map(previousProjection.map(node => [node.id, node]))
  const next = projected.map(node => {
    const rendered = byId.get(node.id)
    if (!rendered) return node
    const previous = previousById.get(node.id)
    // Local drag positions (including companion nodes) survive live status
    // updates. Only an actual canonical placement change replaces geometry.
    const geometryChanged = !previous || shareEqual(previous.position, node.position) !== previous.position
    return shareEqual(rendered, {
      ...rendered, ...node,
      data: shareEqual(rendered.data, node.data),
      position: geometryChanged && !rendered.dragging ? shareEqual(rendered.position, node.position) : rendered.position,
      ...(Object.hasOwn(rendered, 'measured') ? { measured: rendered.measured } : {}),
      ...(Object.hasOwn(rendered, 'selected') ? { selected: rendered.selected } : {}),
    })
  })
  return next.length === current.length && next.every((node, index) => node === current[index]) ? current : next
}

export function applyCanvasSelection(nodes: Node[], selection: Selection): Node[] {
  let changed = false
  const next = nodes.map(node => {
    const kind = node.data.kind
    const selected = (kind === 'project' || kind === 'worktree' || kind === 'agent' || kind === 'process') && selection.type === kind && selection.id === node.data.entityId
    if (Boolean(node.selected) === selected) return node
    changed = true
    return { ...node, selected }
  })
  return changed ? next : nodes
}

export function createPrLabelSelector() {
  let geometryKey = ''
  let placements: Record<string, PrLabelPlacement> = {}
  let display: Edge[] = []
  return (nodes: Node[], edges: Edge[]) => {
    // Selection, status, and labels themselves cannot invalidate geometry.
    const key = JSON.stringify([
      nodes.map(node => [node.id, getNodeRect(node, node.position)]),
      edges.filter(edge => edge.data?.relationship === 'merge-pr').map(edge => [edge.id, edge.source, edge.target]),
    ])
    if (key !== geometryKey) {
      geometryKey = key
      placements = placePrLabels(nodes, edges)
    }
    display = reconcileById(display, edges.map(edge => placements[edge.id]
      ? { ...edge, data: { ...edge.data, labelPlacement: placements[edge.id] } }
      : edge))
    return display
  }
}
