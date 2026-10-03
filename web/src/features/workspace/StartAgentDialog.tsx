import { Bot, X } from 'lucide-react'
import { useBonsaiStore } from '../../stores/bonsai'
import { AGENT_UNAVAILABLE } from '../../stores/execution'

export function StartAgentDialog() {
  const open = useBonsaiStore((state) => state.startAgentDialogOpen)
  const setOpen = useBonsaiStore((state) => state.setStartAgentDialogOpen)
  if (!open) return null

  return (
    <div className="absolute inset-0 z-[85] grid place-items-center bg-black/60 p-6 backdrop-blur-[2px]">
      <div role="dialog" aria-modal="true" aria-labelledby="start-agent-title" aria-describedby="start-agent-unavailable" className="w-full max-w-[480px] rounded-xl border border-[rgb(var(--border-strong))] bg-[rgb(var(--panel))] p-4 shadow-2xl">
        <div className="flex items-center gap-2">
          <Bot size={15} className="text-[rgb(var(--purple))]" />
          <h2 id="start-agent-title" className="text-[12px] font-semibold">Start agent</h2>
          <button type="button" onClick={() => setOpen(false)} aria-label="Close start agent" className="bonsai-focus ml-auto p-1"><X size={14} /></button>
        </div>
        <p id="start-agent-unavailable" className="my-4 text-[11px] leading-5 text-[rgb(var(--muted))]">{AGENT_UNAVAILABLE} You can manage worktrees, review pull requests, and control managed processes.</p>
        <button type="button" disabled className="rounded-md border border-[rgb(var(--border))] px-3 py-2 text-[11px] opacity-40">Start agent</button>
      </div>
    </div>
  )
}
