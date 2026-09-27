import { LAYOUT, rectsOverlap, translateRect } from './geometry'
import type { CanvasPosition, Rect, Size } from './types'

export interface ResolveLocalCollisionsInput {
  movingRects: Rect[]
  fixedRects: Rect[]
  preferredDirection?: 'horizontal' | 'left' | 'right'
  padding?: number
}

function clearAt(movingRects: Rect[], fixedRects: Rect[], dx: number, dy: number, padding: number) {
  return movingRects.every((rect) =>
    fixedRects.every((fixed) => !rectsOverlap(translateRect(rect, dx, dy), fixed, padding)),
  )
}

export function resolveLocalCollisions({
  movingRects,
  fixedRects,
  preferredDirection = 'horizontal',
  padding = LAYOUT.collisionPadding,
}: ResolveLocalCollisionsInput): CanvasPosition {
  if (!movingRects.length || clearAt(movingRects, fixedRects, 0, 0, padding)) return { x: 0, y: 0 }

  // Collision interval boundaries are the nearest useful translations. Large
  // fixed steps can jump over usable gaps and scatter otherwise compact shelves.
  const candidates = new Set<number>()
  for (const moving of movingRects) {
    for (const fixed of fixedRects) {
      if (moving.y + moving.height + padding <= fixed.y ||
          fixed.y + fixed.height + padding <= moving.y) continue
      candidates.add(fixed.x - moving.x - moving.width - padding)
      candidates.add(fixed.x + fixed.width + padding - moving.x)
    }
  }
  const preferredSign = preferredDirection === 'left' ? -1 : 1
  const ordered = [...candidates].sort((a, b) =>
    Math.abs(a) - Math.abs(b) || preferredSign * (b - a))
  for (const dx of ordered) {
    if (clearAt(movingRects, fixedRects, dx, 0, padding)) return { x: dx, y: 0 }
  }

  const maxY = Math.max(...fixedRects.map((rect) => rect.y + rect.height), 0)
  const minMovingY = Math.min(...movingRects.map((rect) => rect.y))
  return { x: 0, y: maxY + padding - minMovingY }
}

export function nearestFreePosition(
  position: CanvasPosition,
  size: Size,
  fixedRects: Rect[],
  preferredDirection: ResolveLocalCollisionsInput['preferredDirection'] = 'horizontal',
): CanvasPosition {
  const delta = resolveLocalCollisions({
    movingRects: [{ ...position, ...size }],
    fixedRects,
    preferredDirection,
  })
  return { x: position.x + delta.x, y: position.y + delta.y }
}
