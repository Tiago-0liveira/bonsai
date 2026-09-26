import type { Edge, Node } from '@xyflow/react'
import { resolveLocalCollisions } from './collision'
import { getNodeRect, rectsOverlap } from './geometry'
import type { CanvasPosition, Rect } from './types'

export const PR_LABEL_SIZE = { width: 208, height: 30 }
const LABEL_PADDING = 8

export interface PrLabelPlacement extends CanvasPosition {
  // If the entire curve is obstructed, retain a short leader to the connection.
  anchor?: CanvasPosition
}

function labelRect(center: CanvasPosition): Rect {
  return {
    x: center.x - PR_LABEL_SIZE.width / 2,
    y: center.y - PR_LABEL_SIZE.height / 2,
    ...PR_LABEL_SIZE,
  }
}

// PR edges use the child's left handle and the parent's right handle. Sample
// the same cubic used by getBezierPath (curvature 0.34), without moving nodes.
function curvePoint(source: Rect, target: Rect, t: number): CanvasPosition {
  const sx = source.x
  const sy = source.y + source.height / 2
  const tx = target.x + target.width
  const ty = target.y + target.height / 2
  const distance = sx - tx
  const offset = distance >= 0 ? distance / 2 : 0.34 * 25 * Math.sqrt(-distance)
  const u = 1 - t
  return {
    x: u ** 3 * sx + 3 * u ** 2 * t * (sx - offset) +
      3 * u * t ** 2 * (tx + offset) + t ** 3 * tx,
    y: u ** 3 * sy + 3 * u ** 2 * t * sy + 3 * u * t ** 2 * ty + t ** 3 * ty,
  }
}

export function placePrLabels(nodes: Node[], edges: Edge[]): Record<string, PrLabelPlacement> {
  const rects = new Map(nodes.map((node) => [node.id, getNodeRect(node, node.position)]))
  const obstacles = [...rects.values()]
  const placements: Record<string, PrLabelPlacement> = {}
  edges.filter((edge) => edge.data?.relationship === 'merge-pr')
    .sort((a, b) => a.id.localeCompare(b.id))
    .forEach((edge) => {
      const source = rects.get(edge.source)
      const target = rects.get(edge.target)
      if (!source || !target) return
      const center = curvePoint(source, target, 0.5)
      let placement: PrLabelPlacement | undefined
      // Prefer the midpoint, then the closest clear point toward either end.
      for (let step = 0; step < 100 && !placement; step += 1) {
        for (const t of step === 0 ? [0.5] : [0.5 - step / 200, 0.5 + step / 200]) {
          const point = curvePoint(source, target, t)
          if (obstacles.every((rect) => !rectsOverlap(labelRect(point), rect, LABEL_PADDING))) {
            placement = point
            break
          }
        }
      }
      if (!placement) {
        const delta = resolveLocalCollisions({
          movingRects: [labelRect(center)], fixedRects: obstacles, padding: LABEL_PADDING,
        })
        placement = { x: center.x + delta.x, y: center.y + delta.y, anchor: center }
      }
      placements[edge.id] = placement
      obstacles.push(labelRect(placement))
    })
  return placements
}
