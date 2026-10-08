import { ScrollText } from 'lucide-react'

export function LogsPage() {
  return (
    <div className="island flex h-full min-h-0 flex-col overflow-hidden">
      <div className="island-title shrink-0 gap-2">
        <ScrollText size={13} className="text-muted" />
        <span>Logs</span>
        <span className="normal-case tracking-normal text-muted-2">Activity log unavailable</span>
      </div>
      <div className="min-h-0 flex-1 overflow-auto p-4">
        <p className="text-[12px] text-muted">Activity logs are unavailable in this view. Select a managed process in the workspace to view its current status.</p>
      </div>
    </div>
  )
}
