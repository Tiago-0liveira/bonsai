import type { ReactNode } from 'react'
import type { Tone } from '../../../lib/github/prDetail'

export const TONE_TEXT: Record<Tone, string> = {
  ok: 'text-ok',
  warn: 'text-warn',
  danger: 'text-danger',
  muted: 'text-muted-2',
}

/**
 * Titled panel used by every block of the overview. `flex-none` keeps its
 * content height: with `overflow-hidden` it would otherwise shrink to fit the
 * column and clip its text instead of letting the column scroll.
 */
export function Card({ title, aside, children, className = '' }: {
  title: string
  aside?: ReactNode
  children: ReactNode
  className?: string
}) {
  return (
    <section aria-label={title} className={`flex-none overflow-hidden rounded-xl border border-border bg-panel-2 ${className}`}>
      <div className="flex h-7 items-center justify-between gap-2 border-b border-border px-3 font-mono text-[10px] uppercase tracking-[0.08em] text-muted">
        <span>{title}</span>
        {aside && <span className="min-w-0 truncate normal-case tracking-[0.02em] text-muted-2">{aside}</span>}
      </div>
      {children}
    </section>
  )
}
