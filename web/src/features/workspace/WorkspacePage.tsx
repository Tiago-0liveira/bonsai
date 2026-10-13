import '@xyflow/react/dist/style.css'
import { ReactFlowProvider } from '@xyflow/react'
import { BonsaiCanvas } from './canvas/BonsaiCanvas'

export type CanvasRoute = 'canvas' | 'worktrees'

/**
 * The canvas layer. The shell mounts it behind every island once a canvas
 * route has been visited, and keeps it mounted while other pages are open.
 * `route` is null on those pages: the canvas stays visible but inert.
 */
export function WorkspacePage({ route }: { route: CanvasRoute | null }) {
  const active = route !== null
  return (
    <div
      data-canvas-layer
      className="absolute inset-0"
      aria-hidden={!active || undefined}
      {...(active ? {} : { inert: '' })}
    >
      <ReactFlowProvider>
        <BonsaiCanvas focus={route === 'worktrees' ? route : undefined} active={active} />
      </ReactFlowProvider>
    </div>
  )
}
