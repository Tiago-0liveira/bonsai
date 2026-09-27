import { ReactFlowProvider } from '@xyflow/react'
import { BonsaiCanvas } from './canvas/BonsaiCanvas'

export function WorkspacePage({ focus }: { focus?: 'worktrees' | 'agents' }) {
  return (
    <div className="h-full min-h-0">
      <ReactFlowProvider>
        <BonsaiCanvas focus={focus} />
      </ReactFlowProvider>
    </div>
  )
}
