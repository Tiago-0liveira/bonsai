import { ScrollText } from 'lucide-react'
import { activity } from '../../mock/activity'

export function LogsPage() {
  return (
    <div className="flex h-full min-h-0 flex-col bg-[rgb(var(--bg))]">
      <div className="flex h-12 shrink-0 items-center gap-2 border-b border-[rgb(var(--border))] px-4">
        <ScrollText size={15} className="text-[rgb(var(--muted))]" />
        <span className="font-medium">Logs</span>
        <span className="text-[11px] text-[rgb(var(--muted-2))]">mock activity stream</span>
      </div>
      <div className="min-h-0 flex-1 overflow-auto p-4">
        <div className="overflow-hidden rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--panel))] font-mono text-[11px]">
          {activity.map((item, index) => (
            <div key={item.id} className={`grid grid-cols-[54px_86px_1fr] gap-3 px-3 py-2.5 ${
              index !== activity.length - 1 ? 'border-b border-[rgb(var(--border))]' : ''
            }`}>
              <span className="text-[rgb(var(--muted-2))]">{item.time}</span>
              <span className="uppercase text-[10px] tracking-wider text-[rgb(var(--purple))]">{item.type}</span>
              <span><span className="text-[rgb(var(--text))]">{item.title}</span><span className="text-[rgb(var(--muted-2))]"> — {item.detail}</span></span>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}
