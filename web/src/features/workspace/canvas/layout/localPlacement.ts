import type { Edge, Node } from '@xyflow/react'
import { nearestFreePosition, resolveLocalCollisions } from './collision'
import { getRuntimeShelfSize, getNodeRect, getNodeSize, LAYOUT } from './geometry'
import { getDescendantIds, getStructuralParentMap, isRuntimeEdge } from './graphModel'
import { compactStackExpansion, detachedWorktreePosition, stackCollapsePosition } from './stackPlacement'
import type { CanvasPosition, NodePlacements, Rect } from './types'

function pos(node: Node, placements: NodePlacements, pending: Record<string, CanvasPosition>) {
  return pending[node.id] ?? placements[node.id] ?? node.position
}

function fixedRects(nodes: Node[], placements: NodePlacements, pending: Record<string, CanvasPosition>, excluded: Set<string>): Rect[] {
  return nodes
    .filter((node) => !excluded.has(node.id) &&
      (placements[node.id] || pending[node.id] || node.type === 'defaultBranch'))
    .map((node) => getNodeRect(node, pos(node, placements, pending)))
}

function runtimesFor(ownerId: string, nodes: Node[], edges: Edge[]) {
  const ids = new Set(edges.filter((edge) => edge.source === ownerId && isRuntimeEdge(edge)).map((edge) => edge.target))
  return nodes.filter((node) => (node.type === 'agent' || node.type === 'process') && ids.has(node.id)).sort((a, b) => a.id.localeCompare(b.id))
}

function childAnchor(node: Node, parent: Node | undefined, nodes: Node[], edges: Edge[], placements: NodePlacements, pending: Record<string, CanvasPosition>) {
  if (!parent) return node.position
  const parentPos = pos(parent, placements, pending)
  const parentSize = getNodeSize(parent)
  const nodeSize = getNodeSize(node)
  let y = parentPos.y + parentSize.height + LAYOUT.branchGapY
  if (parent.type === 'project') y = parentPos.y + parentSize.height + LAYOUT.projectTopGap
  if (parent.type === 'worktree' || parent.type === 'stack' || parent.type === 'runtimeShelf') {
    const runtimes = runtimesFor(parent.id, nodes, edges)
    const shelf = getRuntimeShelfSize(runtimes)
    const placedRuntimes = runtimes.filter((runtime) => placements[runtime.id] || pending[runtime.id])
    const shelfBottom = placedRuntimes.length
      ? Math.max(parentPos.y + parentSize.height, ...placedRuntimes.map((runtime) =>
        pos(runtime, placements, pending).y + getNodeSize(runtime).height))
      : parentPos.y + parentSize.height + (runtimes.length ? LAYOUT.runtimeTopGap + shelf.height : 0)
    y = shelfBottom + LAYOUT.childTopGap
  }
  return { x: parentPos.x + parentSize.width / 2 - nodeSize.width / 2, y }
}

function stackId(nodes: Node[], node: Node) {
  return node.data?.groupId ? 'stack:' + String(node.data.groupId) : ''
}

export function placeMissingNodes(nodes: Node[], edges: Edge[], placements: NodePlacements, missingIds: string[]) {
  const pending: Record<string, CanvasPosition> = {}
  const missing = new Set(missingIds)
  const nodeMap = new Map(nodes.map((node) => [node.id, node]))
  const parents = getStructuralParentMap(edges)

  nodes.filter((node) => missing.has(node.id) && node.type === 'stack').forEach((node) => {
    const preferred = stackCollapsePosition(node, placements) ?? node.position
    pending[node.id] = nearestFreePosition(preferred, getNodeSize(node), fixedRects(nodes, placements, pending, new Set([node.id])))
  })

  const missingWorktrees = nodes.filter((node) => missing.has(node.id) && (node.type === 'worktree' || node.type === 'runtimeShelf'))
  const expansionHandled = new Set<string>()
  const expansionGroups = new Map<string, Node[]>()

  missingWorktrees.forEach((node) => {
    const id = stackId(nodes, node)
    const visibleStack = nodes.some((candidate) => candidate.id === id && candidate.type === 'stack')
    if (!id || visibleStack || !placements[id]) return
    expansionGroups.set(id, [...(expansionGroups.get(id) ?? []), node])
  })

  expansionGroups.forEach((members, id) => {
    if (members.length < 2) return
    const anchor = placements[id]
    const excluded = new Set(members.map((member) => member.id))
    Object.assign(
      pending,
      compactStackExpansion(members, { x: anchor.x, y: anchor.y }, fixedRects(nodes, placements, pending, excluded)),
    )
    members.forEach((member) => expansionHandled.add(member.id))
  })

  missingWorktrees.filter((node) => !expansionHandled.has(node.id)).forEach((node) => {
    const id = stackId(nodes, node)
    const stack = nodes.find((candidate) => candidate.id === id && candidate.type === 'stack')
    const stackPlacement = placements[id]
    if (stack && stackPlacement) {
      pending[node.id] = detachedWorktreePosition(
        stack,
        stackPlacement,
        node,
        fixedRects(nodes, placements, pending, new Set([node.id])),
      )
      return
    }
    const parent = nodeMap.get(parents.get(node.id) ?? '')
    const preferred = childAnchor(node, parent, nodes, edges, placements, pending)
    pending[node.id] = nearestFreePosition(preferred, getNodeSize(node), fixedRects(nodes, placements, pending, new Set([node.id])))
  })

  nodes.filter((node) => missing.has(node.id) && node.type === 'project').forEach((node) => {
    pending[node.id] = node.position
  })

  return pending
}

export function placeCollapsedStacksLocally(
  nodes: Node[],
  placements: NodePlacements,
  addedIds: string[],
) {
  const pending: Record<string, CanvasPosition> = {}
  const added = new Set(addedIds)
  nodes
    .filter(
      (node) =>
        added.has(node.id) &&
        node.type === 'stack' &&
        placements[node.id]?.mode !== 'manual',
    )
    .forEach((node) => {
      const preferred = stackCollapsePosition(node, placements) ?? placements[node.id] ?? node.position
      pending[node.id] = nearestFreePosition(
        preferred,
        getNodeSize(node),
        fixedRects(nodes, placements, pending, new Set([node.id])),
      )
    })
  return pending
}

export function placeExpandedStackLocally(
  nodes: Node[],
  placements: NodePlacements,
  memberIds: string[],
  anchor: CanvasPosition,
) {
  const members = memberIds
    .map((id) => nodes.find((node) => node.id === id && node.type === 'worktree'))
    .filter((node): node is Node => Boolean(node))
  if (!members.length) return {}
  const excluded = new Set(members.map((member) => member.id))
  return compactStackExpansion(
    members,
    anchor,
    fixedRects(nodes, placements, {}, excluded),
  )
}

export function placeAddedNodesLocally(nodes: Node[], placements: NodePlacements, addedIds: string[]) {
  const pending: Record<string, CanvasPosition> = {}
  const added = new Set(addedIds)

  nodes.filter((node) => added.has(node.id) && node.type === 'stack').forEach((node) => {
    if (placements[node.id]?.mode === 'manual') return
    const preferred = stackCollapsePosition(node, placements)
    if (!preferred) return
    pending[node.id] = nearestFreePosition(
      preferred,
      getNodeSize(node),
      fixedRects(nodes, placements, pending, new Set([node.id])),
    )
  })

  const addedWorktrees = nodes.filter(
    (node) => added.has(node.id) && node.type === 'worktree' && placements[node.id]?.mode !== 'manual',
  )

  addedWorktrees.forEach((node) => {
    const id = stackId(nodes, node)
    const stack = nodes.find((candidate) => candidate.id === id && candidate.type === 'stack')
    const stackPlacement = placements[id]
    if (!stack || !stackPlacement) return
    pending[node.id] = detachedWorktreePosition(
      stack,
      stackPlacement,
      node,
      fixedRects(nodes, placements, pending, new Set([node.id])),
    )
  })

  const restoredByStack = new Map<string, Node[]>()
  addedWorktrees.forEach((node) => {
    const id = stackId(nodes, node)
    const visibleStack = nodes.some((candidate) => candidate.id === id && candidate.type === 'stack')
    if (!id || visibleStack || !placements[id] || !placements[node.id]) return
    restoredByStack.set(id, [...(restoredByStack.get(id) ?? []), node])
  })

  restoredByStack.forEach((members) => {
    if (!members.length) return
    const memberIds = new Set(members.map((member) => member.id))
    const movingRects = members.map((member) => getNodeRect(member, placements[member.id]))
    const delta = resolveLocalCollisions({
      movingRects,
      fixedRects: fixedRects(nodes, placements, pending, memberIds),
    })
    if (!delta.x && !delta.y) return
    members.forEach((member) => {
      const placement = placements[member.id]
      pending[member.id] = { x: placement.x + delta.x, y: placement.y + delta.y }
    })
  })

  return pending
}

export function refreshGeneratedRuntimeShelves(
  nodes: Node[],
  edges: Edge[],
  placements: NodePlacements,
  ownerIds?: Set<string>,
) {
  const pending: Record<string, CanvasPosition> = {}
  nodes.filter((node) => (node.type === 'worktree' || node.type === 'stack' || node.type === 'runtimeShelf') && (!ownerIds || ownerIds.has(node.id))).forEach((owner) => {
    const ownerPos = pos(owner, placements, pending)
    const ownerSize = getNodeSize(owner)
    const runtimes = runtimesFor(owner.id, nodes, edges)
      .filter((runtime) => placements[runtime.id]?.mode !== 'manual')
    if (!runtimes.length) return

    // Resolve the complete shelf together, so collision adjustments preserve
    // rows and spacing instead of sending each runtime to an unrelated position.
    const preferred: Record<string, CanvasPosition> = {}
    let rowY = ownerPos.y + ownerSize.height + LAYOUT.runtimeTopGap
    for (let index = 0; index < runtimes.length; index += 3) {
      const row = runtimes.slice(index, index + 3)
      const sizes = row.map(getNodeSize)
      const width = sizes.reduce((sum, size) => sum + size.width, 0) + (row.length - 1) * LAYOUT.runtimeGapX
      let rowX = ownerPos.x + (ownerSize.width - width) / 2
      row.forEach((runtime, column) => {
        preferred[runtime.id] = { x: rowX, y: rowY }
        rowX += sizes[column].width + LAYOUT.runtimeGapX
      })
      rowY += Math.max(...sizes.map((size) => size.height)) + LAYOUT.runtimeGapY
    }
    const excluded = new Set(runtimes.map((runtime) => runtime.id))
    const delta = resolveLocalCollisions({
      movingRects: runtimes.map((runtime) => getNodeRect(runtime, preferred[runtime.id])),
      fixedRects: fixedRects(nodes, placements, pending, excluded),
      // Match the shelf's own spacing; branch padding exceeds the top gap.
      padding: LAYOUT.runtimeGapY,
    })
    runtimes.forEach((runtime) => {
      const preferredPos = preferred[runtime.id]
      pending[runtime.id] = { x: preferredPos.x + delta.x, y: preferredPos.y + delta.y }
    })
  })
  return pending
}

export function relocateGeneratedBranches(nodes: Node[], edges: Edge[], placements: NodePlacements, rootIds: string[]) {
  const pending: Record<string, CanvasPosition> = {}
  const nodeMap = new Map(nodes.map((node) => [node.id, node]))
  const parents = getStructuralParentMap(edges)

  rootIds.forEach((rootId) => {
    const root = nodeMap.get(rootId)
    const placement = placements[rootId]
    const parent = nodeMap.get(parents.get(rootId) ?? '')
    if (!root || !parent || !placement || placement.mode === 'manual') return

    const target = childAnchor(root, parent, nodes, edges, placements, pending)
    const dx = target.x - placement.x
    const dy = target.y - placement.y
    const movingIds = [rootId, ...getDescendantIds(rootId, edges)].filter((id) => {
      const item = placements[id]
      return item && item.mode !== 'manual' && nodeMap.has(id)
    })
    if (!movingIds.length) return

    const movingRects = movingIds.map((id) => {
      const item = placements[id]
      return getNodeRect(nodeMap.get(id) as Node, { x: item.x + dx, y: item.y + dy })
    })
    const collision = resolveLocalCollisions({
      movingRects,
      fixedRects: fixedRects(nodes, placements, pending, new Set(movingIds)),
    })
    movingIds.forEach((id) => {
      const item = placements[id]
      pending[id] = { x: item.x + dx + collision.x, y: item.y + dy + collision.y }
    })
  })
  return pending
}
