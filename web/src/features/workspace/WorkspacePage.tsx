import '@xyflow/react/dist/style.css'
import { ReactFlowProvider } from '@xyflow/react'
import { BonsaiCanvas } from './canvas/BonsaiCanvas'

export function WorkspacePage({ focus }: { focus?: 'worktrees' }) {
  return (
    <div className="h-full min-h-0">
      <ReactFlowProvider>
        <BonsaiCanvas focus={focus} />
      </ReactFlowProvider>
    </div>
  )
}
