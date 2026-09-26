import type { Node } from '@xyflow/react'
import type { CanvasPosition, Rect, Size } from './types'

export const LAYOUT = {
  branchGapX: 72,
  branchGapY: 90,
  agentGapX: 14,
  agentGapY: 12,
  agentTopGap: 24,
  childTopGap: 70,
  projectTopGap: 90,
  collisionPadding: 28,
} as const

export function getNodeSize(node: Pick<Node, 'type' | 'data'>): Size {
  const type = node.type ?? 'agent'
  if (type === 'project') return { width: 300, height: 154 }
  if (type === 'defaultBranch') return { width: 232, height: 132 }
  if (type === 'env') return { width: 150, height: 56 }
  if (type === 'worktree') {
    const historyItems = node.data?.historyItems as unknown[] | undefined
    const historyAllowance = historyItems?.length ? 28 : 0
    return { width: 230, height: 154 + historyAllowance }
  }
  if (type === 'stack') {
    const count = Number(node.data?.stackCount ?? 1)
    return { width: 286, height: 50 + Math.min(7, count) * 34 }
  }
  return { width: 188, height: 98 }
}

export function getNodeRect(node: Pick<Node, 'type' | 'data'>, position: CanvasPosition): Rect {
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

export function getAgentShelfSize(count: number): Size {
  if (count <= 0) return { width: 0, height: 0 }
  const agent = getNodeSize({ type: 'agent', data: {} })
  const columns = Math.min(3, count)
  const rows = Math.ceil(count / columns)
  return {
    width: columns * agent.width + Math.max(0, columns - 1) * LAYOUT.agentGapX,
    height: rows * agent.height + Math.max(0, rows - 1) * LAYOUT.agentGapY,
  }
}
