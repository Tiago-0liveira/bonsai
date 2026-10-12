import { create } from 'zustand'

/**
 * The visible canvas area: the shell rectangle not covered by the rail, the
 * top bar or the dock. The canvas renders full-screen behind the shell, but
 * fit, center and reveal frame content inside this rectangle. Offsets are
 * relative to the canvas layer, which covers the whole shell.
 */
export interface CanvasFrame {
  left: number
  top: number
  width: number
  height: number
}

/** Used before the shell has been measured, e.g. on the first render or in tests. */
export const FALLBACK_FRAME: CanvasFrame = { left: 0, top: 0, width: 0, height: 0 }

export const useCanvasFrame = create<{ frame: CanvasFrame }>(() => ({ frame: FALLBACK_FRAME }))

function sameFrame(a: CanvasFrame, b: CanvasFrame) {
  return a.left === b.left && a.top === b.top && a.width === b.width && a.height === b.height
}

export function setCanvasFrame(frame: CanvasFrame) {
  if (!sameFrame(useCanvasFrame.getState().frame, frame)) useCanvasFrame.setState({ frame })
}

/** Measures `element` against `ground` and keeps the frame store current. */
export function observeCanvasFrame(element: HTMLElement, ground: HTMLElement) {
  const measure = () => {
    const box = element.getBoundingClientRect(), origin = ground.getBoundingClientRect()
    setCanvasFrame({
      left: Math.round(box.left - origin.left),
      top: Math.round(box.top - origin.top),
      width: Math.round(box.width),
      height: Math.round(box.height),
    })
  }
  measure()
  const observer = typeof ResizeObserver === 'undefined' ? undefined : new ResizeObserver(measure)
  observer?.observe(element)
  observer?.observe(ground)
  window.addEventListener('resize', measure)
  return () => {
    observer?.disconnect()
    window.removeEventListener('resize', measure)
  }
}
