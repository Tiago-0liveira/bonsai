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

  const minX = Math.min(...movingRects.map((rect) => rect.x))
  const maxX = Math.max(...movingRects.map((rect) => rect.x + rect.width))
  const step = Math.max(64, maxX - minX + padding)
  const signs =
    preferredDirection === 'left' ? [-1, 1] :
      preferredDirection === 'right' ? [1, -1] :
        [1, -1]

  for (let distance = 1; distance <= 24; distance += 1) {
    for (const sign of signs) {
      const dx = sign * distance * step
      if (clearAt(movingRects, fixedRects, dx, 0, padding)) return { x: dx, y: 0 }
    }
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
