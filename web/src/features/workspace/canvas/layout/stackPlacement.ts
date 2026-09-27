import type { Node } from '@xyflow/react'
import { nearestFreePosition, resolveLocalCollisions } from './collision'
import { centroid, getNodeRect, getNodeSize, LAYOUT } from './geometry'
import type { CanvasPosition, NodePlacements, Rect } from './types'

function stackMemberIds(stackNode: Node) {
  return ((stackNode.data?.stackItems ?? []) as Array<{ id: string }>).map((item) => item.id)
}

export function stackCollapsePosition(stackNode: Node, placements: NodePlacements): CanvasPosition | undefined {
  const centers = stackMemberIds(stackNode)
    .map((id) => placements[id])
    .filter((placement): placement is NonNullable<typeof placement> => Boolean(placement))
    .map((placement) => ({
      x: placement.x + 230 / 2,
      y: placement.y + 154 / 2,
    }))
  if (!centers.length) return undefined
  const center = centroid(centers)
  const size = getNodeSize(stackNode)
  return { x: center.x - size.width / 2, y: center.y - size.height / 2 }
}

export function compactStackExpansion(
  worktreeNodes: Node[],
  anchor: CanvasPosition,
  fixedRects: Rect[],
): Record<string, CanvasPosition> {
  if (!worktreeNodes.length) return {}
  const gapX = 24
  const gapY = 24
  const columns = worktreeNodes.length === 1 ? 1 : 2
  const size = getNodeSize(worktreeNodes[0])
  const rows = Math.ceil(worktreeNodes.length / columns)
  const blockWidth = columns * size.width + Math.max(0, columns - 1) * gapX
  const blockHeight = rows * size.height + Math.max(0, rows - 1) * gapY
  const blockX = anchor.x - (blockWidth - size.width) / 2
  const blockY = anchor.y

  const positions: Record<string, CanvasPosition> = {}
  const rects = worktreeNodes.map((node, index) => {
    const row = Math.floor(index / columns)
    const rowStart = row * columns
    const rowCount = Math.min(columns, worktreeNodes.length - rowStart)
    const rowWidth = rowCount * size.width + Math.max(0, rowCount - 1) * gapX
    const rowX = anchor.x + size.width / 2 - rowWidth / 2
    const position = {
      x: rowX + (index % columns) * (size.width + gapX),
      y: blockY + row * (size.height + gapY),
    }
    positions[node.id] = position
    return getNodeRect(node, position)
  })

  const delta = resolveLocalCollisions({ movingRects: rects, fixedRects, padding: LAYOUT.collisionPadding })
  if (delta.x || delta.y) {
    Object.keys(positions).forEach((id) => {
      positions[id] = { x: positions[id].x + delta.x, y: positions[id].y + delta.y }
    })
  }

  void blockX
  void blockHeight
  return positions
}

export function detachedWorktreePosition(
  stackNode: Node,
  stackPosition: CanvasPosition,
  worktreeNode: Node,
  fixedRects: Rect[],
): CanvasPosition {
  const stackSize = getNodeSize(stackNode)
  const worktreeSize = getNodeSize(worktreeNode)
  const y = stackPosition.y + Math.max(0, (stackSize.height - worktreeSize.height) / 2)
  const right = {
    x: stackPosition.x + stackSize.width + LAYOUT.branchGapX,
    y,
  }
  const rightFree = nearestFreePosition(right, worktreeSize, fixedRects, 'right')
  if (rightFree.x === right.x && rightFree.y === right.y) return rightFree
  const left = {
    x: stackPosition.x - worktreeSize.width - LAYOUT.branchGapX,
    y,
  }
  return nearestFreePosition(left, worktreeSize, fixedRects, 'left')
}
