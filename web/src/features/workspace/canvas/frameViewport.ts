import { getViewportForBounds, type Rect, type Viewport } from '@xyflow/react'
import type { CanvasFrame } from './canvasFrame'

/** Uses the whole host when the frame has not been measured yet. */
export function resolveFrame(frame: CanvasFrame, host: { width: number; height: number }): CanvasFrame {
  return frame.width > 0 && frame.height > 0 ? frame : { left: 0, top: 0, width: host.width, height: host.height }
}

/** Fits `bounds` inside the frame, like fitView does for the whole pane. */
export function fitBoundsInFrame(
  bounds: Rect,
  frame: CanvasFrame,
  { padding, minZoom, maxZoom }: { padding: number; minZoom: number; maxZoom: number },
): Viewport {
  const viewport = getViewportForBounds(bounds, frame.width, frame.height, minZoom, maxZoom, padding)
  return { x: viewport.x + frame.left, y: viewport.y + frame.top, zoom: viewport.zoom }
}

/** Puts the flow point at the centre of the frame, keeping `zoom`. */
export function centerInFrame(point: { x: number; y: number }, zoom: number, frame: CanvasFrame): Viewport {
  return {
    x: frame.left + frame.width / 2 - point.x * zoom,
    y: frame.top + frame.height / 2 - point.y * zoom,
    zoom,
  }
}

/** Whether the flow rectangle is fully visible inside the frame. */
export function isRectInFrame(rect: Rect, viewport: Viewport, frame: CanvasFrame) {
  const x = rect.x * viewport.zoom + viewport.x
  const y = rect.y * viewport.zoom + viewport.y
  return x >= frame.left && y >= frame.top &&
    x + rect.width * viewport.zoom <= frame.left + frame.width &&
    y + rect.height * viewport.zoom <= frame.top + frame.height
}
