import { useEffect, useId, type FormEvent, type ReactNode } from 'react'
import { X, type LucideIcon } from 'lucide-react'

export const FIELD_LABEL = 'font-mono text-[10px] font-semibold uppercase tracking-[.1em] text-muted-2'

const WIDTHS = { md: 'max-w-[760px]', lg: 'max-w-[900px]' } as const

/**
 * The shell the launch dialogs share: backdrop, header, scrolling body and a
 * footer with a summary on the left and the actions on the right. It renders
 * as a form so Enter and the primary button submit.
 */
export function DialogFrame({ icon: Icon, title, subtitle, closeLabel, onClose, closeDisabled = false, escapeCloses = true, width = 'md', layer = 85, onSubmit, summary, actions, children }: {
  icon: LucideIcon
  title: string
  subtitle: ReactNode
  closeLabel: string
  onClose: () => void
  closeDisabled?: boolean
  /** False keeps Escape from closing, for a dialog holding unsent text. */
  escapeCloses?: boolean
  width?: keyof typeof WIDTHS
  layer?: number
  onSubmit: (event: FormEvent) => void
  summary?: ReactNode
  actions: ReactNode
  children: ReactNode
}) {
  const titleId = useId()
  useEffect(() => {
    if (!escapeCloses || closeDisabled) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || event.defaultPrevented) return
      // An open select menu or model list takes the key first.
      if (document.querySelector('[data-bonsai-select-menu]')) return
      onClose()
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [escapeCloses, closeDisabled, onClose])

  return (
    <div style={{ zIndex: layer }} className="dialog-backdrop absolute inset-0 grid place-items-center bg-well/70 p-6 backdrop-blur-[3px]">
      <form
        onSubmit={onSubmit}
        role="dialog" aria-modal="true" aria-labelledby={titleId}
        className={'dialog-card flex max-h-full w-full flex-col overflow-hidden rounded-[16px] border border-border-strong bg-panel shadow-overlay ' + WIDTHS[width]}
      >
        <div className="flex shrink-0 items-center gap-3 border-b border-border-subtle px-5 py-3.5">
          <span className="grid h-9 w-9 shrink-0 place-items-center rounded-[10px] border border-accent/25 bg-accent/10 text-accent"><Icon size={16} /></span>
          <div className="min-w-0">
            <div id={titleId} className="text-[14px] font-semibold leading-tight">{title}</div>
            <div className="mt-0.5 truncate font-mono text-[10.5px] text-muted-2">{subtitle}</div>
          </div>
          <button
            type="button"
            disabled={closeDisabled}
            onClick={onClose}
            aria-label={closeLabel}
            className="bonsai-focus ml-auto grid h-8 w-8 shrink-0 place-items-center rounded-lg text-muted transition-colors hover:bg-panel-3 hover:text-text disabled:opacity-40"
          >
            <X size={15} />
          </button>
        </div>
        <div className="dialog-body min-h-0 flex-1 overflow-auto p-5">{children}</div>
        <div className="flex shrink-0 items-center justify-between gap-3 border-t border-border-subtle bg-well/45 px-5 py-3">
          <div className="min-w-0 truncate font-mono text-[10.5px] text-muted-2">{summary}</div>
          <div className="flex shrink-0 gap-2">{actions}</div>
        </div>
      </form>
    </div>
  )
}

/** A label above its control, with an optional note on the right and a hint below. */
export function Field({ label, aside, hint, htmlFor, children }: {
  label: ReactNode
  aside?: ReactNode
  hint?: ReactNode
  /** The control's ID when the label should name it. */
  htmlFor?: string
  children: ReactNode
}) {
  const Label = htmlFor ? 'label' : 'span'
  return (
    <div className="min-w-0">
      <div className="mb-2 flex min-h-[20px] items-center justify-between gap-2">
        <Label {...(htmlFor ? { htmlFor } : {})} className={FIELD_LABEL + ' flex items-center gap-1.5'}>{label}</Label>
        {aside}
      </div>
      {children}
      {hint && <div className="mt-1.5 font-mono text-[10px] leading-4 text-muted-2">{hint}</div>}
    </div>
  )
}

/** Opens to its natural height when it mounts; under reduced motion it just appears. */
export function Reveal({ children }: { children: ReactNode }) {
  return <div className="dialog-reveal"><div>{children}</div></div>
}

export const TEXT_INPUT = 'bonsai-focus h-9 w-full rounded-[8px] border border-border bg-well px-3 text-[12.5px] outline-none transition-colors placeholder:text-muted-2 hover:border-border-strong focus:border-accent/55 disabled:cursor-not-allowed disabled:opacity-50'
