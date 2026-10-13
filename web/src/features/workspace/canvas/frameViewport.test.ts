import { describe, expect, it } from 'vitest'
import { centerInFrame, fitBoundsInFrame, isRectInFrame, resolveFrame } from './frameViewport'

const frame = { left: 428, top: 68, width: 800, height: 600 }

describe('frame viewport', () => {
  it('fits bounds inside the frame instead of the whole canvas', () => {
    const viewport = fitBoundsInFrame({ x: 0, y: 0, width: 400, height: 300 }, frame, { padding: 0, minZoom: 0.1, maxZoom: 4 })
    expect(viewport.zoom).toBe(2)
    // The fitted content starts at the frame origin, not at the canvas origin.
    expect(viewport.x).toBe(428)
    expect(viewport.y).toBe(68)
  })

  it('centres a point in the frame and keeps the zoom', () => {
    const viewport = centerInFrame({ x: 100, y: 50 }, 0.5, frame)
    expect(viewport).toEqual({ x: 428 + 400 - 50, y: 68 + 300 - 25, zoom: 0.5 })
  })

  it('treats content under the rail or dock as hidden', () => {
    const viewport = { x: 0, y: 0, zoom: 1 }
    expect(isRectInFrame({ x: 500, y: 100, width: 100, height: 100 }, viewport, frame)).toBe(true)
    expect(isRectInFrame({ x: 100, y: 100, width: 100, height: 100 }, viewport, frame)).toBe(false)
    expect(isRectInFrame({ x: 500, y: 600, width: 100, height: 100 }, viewport, frame)).toBe(false)
  })

  it('falls back to the host before the frame is measured', () => {
    expect(resolveFrame({ left: 0, top: 0, width: 0, height: 0 }, { width: 300, height: 200 })).toEqual({ left: 0, top: 0, width: 300, height: 200 })
    expect(resolveFrame(frame, { width: 300, height: 200 })).toBe(frame)
  })
})
