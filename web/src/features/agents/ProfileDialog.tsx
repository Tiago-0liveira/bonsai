import { useEffect, type ReactNode } from 'react'

/** Modal chrome shared by the Agents page confirmations. Escape asks to close unless `locked`. */
export function ProfileDialog({ label, locked = false, onClose, children }: {
  label: string
  /** True while a request runs: the dialog cannot be dismissed. */
  locked?: boolean
  onClose: () => void
  children: ReactNode
}) {
  useEffect(() => {
    const escape = (event: KeyboardEvent) => { if (event.key === 'Escape' && !locked) { event.stopPropagation(); onClose() } }
    window.addEventListener('keydown', escape)
    return () => window.removeEventListener('keydown', escape)
  }, [locked, onClose])
  return (
    <div className="fixed inset-0 z-[90] grid place-items-center bg-well/70 p-6 backdrop-blur-[2px]">
      <div role="dialog" aria-modal="true" aria-label={label} className="w-full max-w-lg space-y-4 rounded-[14px] border border-border-strong bg-panel p-5 shadow-overlay">
        {children}
      </div>
    </div>
  )
}

/** Asks before unsaved profile defaults are dropped by a tab or profile switch. */
export function DiscardChangesDialog({ onKeep, onDiscard }: { onKeep: () => void; onDiscard: () => void }) {
  return (
    <ProfileDialog label="Discard unsaved defaults" onClose={onKeep}>
      <h2 className="text-[13px] font-semibold">Discard unsaved defaults?</h2>
      <p className="text-xs text-muted">The changes to this profile's defaults have not been saved.</p>
      <div className="flex justify-end gap-3">
        <button type="button" autoFocus onClick={onKeep} className="bonsai-focus btn-bordered h-8 rounded-[7px] px-3 text-[12px] text-muted hover:text-text">Keep editing</button>
        <button type="button" onClick={onDiscard} className="bonsai-focus btn-danger-tint h-8 rounded-[7px] px-3 text-[12px]">Discard changes</button>
      </div>
    </ProfileDialog>
  )
}
