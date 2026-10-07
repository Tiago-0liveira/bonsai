import type { Node } from '@xyflow/react'
import type { CanvasPosition, Rect, Size } from './types'

export const LAYOUT = {
  branchGapX: 72,
  branchGapY: 90,
  runtimeGapX: 14,
  runtimeGapY: 12,
  runtimeTopGap: 24,
  childTopGap: 70,
  projectTopGap: 90,
  collisionPadding: 28,
} as const

function estimateNodeSize(node: Pick<Node, 'type' | 'data'>): Size {
  const type = node.type ?? 'agent'
  if (type === 'project') return { width: 300, height: 154 }
  if (type === 'defaultBranch') return { width: 232, height: 132 }
  if (type === 'env') return { width: 150, height: 56 }
  if (type === 'process') return { width: 240, height: 112 }
  if (type === 'runtimeShelf') return { width: 230, height: 74 }
  if (type === 'worktree') {
    const historyItems = node.data?.historyItems as unknown[] | undefined
    // The History header is always visible when entries exist. Reserve the
    // expanded rows as well so opening History never collides with an agent
    // shelf laid out beneath the worktree.
    const historyAllowance = historyItems?.length ? 34 + historyItems.length * 32 : 0
    return { width: 230, height: 154 + historyAllowance }
  }
  if (type === 'stack') {
    const count = Math.max(Number(node.data?.stackCount) || 1,
      Array.isArray(node.data?.stackItems) ? node.data.stackItems.length : 0)
    const connectionRows = ((node.data?.stackItems ?? []) as Array<{ connectionLabel?: string }>).filter(item => item.connectionLabel).length
    const processRows = ((node.data?.stackItems ?? []) as Array<{ processCount?: number }>).filter(item => item.processCount).length
    return { width: 286, height: 50 + count * 37 + (connectionRows + processRows) * 13 }
  }
  return { width: 188, height: 98 }
}

type SizedNode = Pick<Node, 'type' | 'data'> & Partial<Pick<Node, 'measured'>>

export function getNodeSize(node: SizedNode): Size {
  const estimate = estimateNodeSize(node)
  const valid = (value: number | undefined, fallback: number) =>
    value !== undefined && Number.isFinite(value) && value > 0 ? value : fallback
  return {
    width: valid(node.measured?.width, estimate.width),
    height: valid(node.measured?.height, estimate.height),
  }
}

export function getNodeRect(node: SizedNode, position: CanvasPosition): Rect {
  return { ...position, ...getNodeSize(node) }
}

export function rectsOverlap(a: Rect, b: Rect, padding = 0) {
  return !(
    a.x + a.width + padding <= b.x ||
    b.x + b.width + padding <= a.x ||
    a.y + a.height + padding <= b.y ||
    b.y + b.height + padding <= a.y
  )
}

export function translateRect(rect: Rect, dx: number, dy: number): Rect {
  return { ...rect, x: rect.x + dx, y: rect.y + dy }
}

export function centroid(points: CanvasPosition[]): CanvasPosition {
  if (!points.length) return { x: 0, y: 0 }
  return {
    x: points.reduce((sum, point) => sum + point.x, 0) / points.length,
    y: points.reduce((sum, point) => sum + point.y, 0) / points.length,
  }
}

export function getRuntimeShelfSize(nodes: Node[]): Size {
  const rows = []
  for (let index = 0; index < nodes.length; index += 3) {
    const sizes = nodes.slice(index, index + 3).map(getNodeSize)
    rows.push({
      width: sizes.reduce((total, size) => total + size.width, 0) + (sizes.length - 1) * LAYOUT.runtimeGapX,
      height: Math.max(...sizes.map(size => size.height)),
    })
  }
  return {
    width: Math.max(0, ...rows.map(row => row.width)),
    height: rows.reduce((total, row) => total + row.height, 0) + Math.max(0, rows.length - 1) * LAYOUT.runtimeGapY,
  }
}
