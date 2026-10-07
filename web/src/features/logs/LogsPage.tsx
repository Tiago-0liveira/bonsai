import { ScrollText } from 'lucide-react'

export function LogsPage() {
  return (
    <div className="flex h-full min-h-0 flex-col bg-[rgb(var(--bg))]">
      <div className="flex h-12 shrink-0 items-center gap-2 border-b border-[rgb(var(--border))] px-4">
        <ScrollText size={15} className="text-[rgb(var(--muted))]" />
        <span className="font-medium">Logs</span>
        <span className="text-[11px] text-[rgb(var(--muted-2))]">Activity log unavailable</span>
      </div>
      <div className="min-h-0 flex-1 overflow-auto p-4">
        <p className="text-[11px] text-[rgb(var(--muted))]">Activity logs are unavailable in this view. Select a managed process in the workspace to view its current status.</p>
      </div>
    </div>
  )
}
