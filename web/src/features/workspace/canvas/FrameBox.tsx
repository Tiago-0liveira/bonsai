import type { ReactNode } from 'react'
import { useCanvasFrame } from './canvasFrame'

/** Positions children over the visible canvas frame; the whole layer until it is measured. */
export function FrameBox({ children, className = '' }: { children: ReactNode; className?: string }) {
  const frame = useCanvasFrame(state => state.frame)
  const measured = frame.width > 0 && frame.height > 0
  return (
    <div
      className={'absolute ' + (measured ? '' : 'inset-0 ') + className}
      style={measured ? { left: frame.left, top: frame.top, width: frame.width, height: frame.height } : undefined}
    >
      {children}
    </div>
  )
}
