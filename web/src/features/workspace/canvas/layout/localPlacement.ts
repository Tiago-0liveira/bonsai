import type { Edge, Node } from '@xyflow/react'
import { nearestFreePosition, resolveLocalCollisions } from './collision'
import { getAgentShelfSize, getNodeRect, getNodeSize, LAYOUT } from './geometry'
import { getDescendantIds, getStructuralParentMap, isAgentEdge } from './graphModel'
import { compactStackExpansion, detachedWorktreePosition, stackCollapsePosition } from './stackPlacement'
import type { CanvasPosition, NodePlacements, Rect } from './types'

function pos(node: Node, placements: NodePlacements, pending: Record<string, CanvasPosition>) {
  return pending[node.id] ?? placements[node.id] ?? node.position
}

function fixedRects(nodes: Node[], placements: NodePlacements, pending: Record<string, CanvasPosition>, excluded: Set<string>): Rect[] {
  return nodes
    .filter((node) => !excluded.has(node.id))
    .map((node) => getNodeRect(node, pos(node, placements, pending)))
}

function agentsFor(ownerId: string, nodes: Node[], edges: Edge[]) {
  const ids = new Set(edges.filter((edge) => edge.source === ownerId && isAgentEdge(edge)).map((edge) => edge.target))
  return nodes.filter((node) => node.type === 'agent' && ids.has(node.id)).sort((a, b) => a.id.localeCompare(b.id))
}

function childAnchor(node: Node, parent: Node | undefined, nodes: Node[], edges: Edge[], placements: NodePlacements, pending: Record<string, CanvasPosition>) {
  if (!parent) return node.position
  const parentPos = pos(parent, placements, pending)
  const parentSize = getNodeSize(parent)
  const nodeSize = getNodeSize(node)
  let y = parentPos.y + parentSize.height + LAYOUT.branchGapY
  if (parent.type === 'project') y = parentPos.y + parentSize.height + LAYOUT.projectTopGap
  if (parent.type === 'worktree') {
    const agents = agentsFor(parent.id, nodes, edges)
    const shelf = getAgentShelfSize(agents.length)
    y = parentPos.y + parentSize.height + (agents.length ? LAYOUT.agentTopGap + shelf.height : 0) + LAYOUT.childTopGap
  }
  return { x: parentPos.x + parentSize.width / 2 - nodeSize.width / 2, y }
}

function stackId(nodes: Node[], node: Node) {
  const project = nodes.find((candidate) => candidate.type === 'project')
  const tag = String(node.data?.tag ?? '')
  return project && tag ? 'stack:' + project.id + ':' + tag : ''
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

  const missingWorktrees = nodes.filter((node) => missing.has(node.id) && node.type === 'worktree')
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
    if (members.length < 2) return
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

export function refreshGeneratedAgentShelves(nodes: Node[], edges: Edge[], placements: NodePlacements) {
  const pending: Record<string, CanvasPosition> = {}
  nodes.filter((node) => node.type === 'worktree').forEach((owner) => {
    const ownerPos = pos(owner, placements, pending)
    const ownerSize = getNodeSize(owner)
    const agents = agentsFor(owner.id, nodes, edges)
    if (!agents.length) return
    const columns = Math.min(3, agents.length)
    const agentSize = getNodeSize({ type: 'agent', data: {} })
    const shelfY = ownerPos.y + ownerSize.height + LAYOUT.agentTopGap

    agents.forEach((agent, index) => {
      if (placements[agent.id]?.mode === 'manual') return
      const row = Math.floor(index / columns)
      const col = index % columns
      const rowCount = Math.min(columns, agents.length - row * columns)
      const rowWidth = rowCount * agentSize.width + Math.max(0, rowCount - 1) * LAYOUT.agentGapX
      const preferred = {
        x: ownerPos.x + ownerSize.width / 2 - rowWidth / 2 + col * (agentSize.width + LAYOUT.agentGapX),
        y: shelfY + row * (agentSize.height + LAYOUT.agentGapY),
      }
      const generatedSiblingIds = agents
        .filter((candidate) => candidate.id !== agent.id && placements[candidate.id]?.mode !== 'manual')
        .map((candidate) => candidate.id)
      pending[agent.id] = nearestFreePosition(
        preferred,
        agentSize,
        fixedRects(nodes, placements, pending, new Set([agent.id, ...generatedSiblingIds])),
      )
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
