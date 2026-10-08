import { KeyRound, X } from 'lucide-react'
import { useBonsaiStore } from '../../stores/bonsai'
import { ENV_UNAVAILABLE } from '../../stores/execution'

export function EnvEditor() {
  const open = useBonsaiStore((state) => state.envEditorOpen)
  const setOpen = useBonsaiStore((state) => state.setEnvEditorOpen)
  if (!open) return null

  return (
    <aside aria-label="Environment variables" className="absolute right-3 top-14 z-50 w-[420px] max-w-[calc(100%-24px)] rounded-[14px] border border-border-strong bg-panel p-3 shadow-overlay">
      <div className="flex items-center gap-2">
        <KeyRound size={14} className="text-warn" />
        <h2 className="text-[11px] font-semibold">Environment variables</h2>
        <button type="button" onClick={() => setOpen(false)} aria-label="Close environment editor" className="bonsai-focus ml-auto p-1"><X size={13} /></button>
      </div>
      <p className="my-4 text-[11px] leading-5 text-muted">{ENV_UNAVAILABLE} Manage your environment files locally. Environment values are not saved in browser storage.</p>
      <button type="button" disabled className="btn-bordered h-8 rounded-[7px] px-3 text-[12px] opacity-40">Add variable</button>
    </aside>
  )
}
