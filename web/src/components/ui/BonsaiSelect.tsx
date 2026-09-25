import { useEffect, useMemo, useRef, useState } from 'react'
import { Check, ChevronDown, Search } from 'lucide-react'

export interface BonsaiSelectOption {
  value: string
  label: string
  description?: string
  disabled?: boolean
  meta?: string
}

export function BonsaiSelect({
  value,
  options,
  onChange,
  placeholder = 'Select…',
  ariaLabel,
  searchable = false,
  compact = false,
  disabled = false,
}: {
  value: string
  options: BonsaiSelectOption[]
  onChange: (value: string) => void
  placeholder?: string
  ariaLabel: string
  searchable?: boolean
  compact?: boolean
  disabled?: boolean
}) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const rootRef = useRef<HTMLDivElement | null>(null)
  const selected = options.find((option) => option.value === value)
  const visible = useMemo(() => {
    const needle = query.trim().toLowerCase()
    if (!needle) return options
    return options.filter((option) =>
      (option.label + ' ' + (option.description ?? '') + ' ' + (option.meta ?? '')).toLowerCase().includes(needle),
    )
  }, [options, query])

  useEffect(() => {
    if (!open) return
    const close = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false)
    }
    window.addEventListener('pointerdown', close)
    return () => window.removeEventListener('pointerdown', close)
  }, [open])

  return (
    <div ref={rootRef} className="relative min-w-0">
      <button
        type="button"
        aria-label={ariaLabel}
        aria-haspopup="listbox"
        aria-expanded={open}
        disabled={disabled}
        onClick={() => setOpen((current) => !current)}
        className={
          'bonsai-focus flex w-full items-center gap-2 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-2.5 text-left text-[rgb(var(--text))] outline-none transition-colors hover:border-[rgb(var(--border-strong))] disabled:cursor-not-allowed disabled:opacity-50 ' +
          (compact ? 'h-7 text-[9px]' : 'h-9 text-[11px]')
        }
      >
        <span className={'min-w-0 flex-1 truncate ' + (!selected ? 'text-[rgb(var(--muted-2))]' : '')}>
          {selected?.label ?? placeholder}
        </span>
        {selected?.meta && <span className="shrink-0 text-[8px] text-[rgb(var(--muted-2))]">{selected.meta}</span>}
        <ChevronDown size={12} className={'shrink-0 text-[rgb(var(--muted-2))] transition-transform ' + (open ? 'rotate-180' : '')} />
      </button>
      {open && (
        <div className="absolute left-0 top-[calc(100%+5px)] z-[100] w-full min-w-[210px] overflow-hidden rounded-lg border border-[rgb(var(--border-strong))] bg-[rgb(var(--panel-2))] shadow-[0_20px_60px_rgb(0_0_0/.55)]">
          {searchable && (
            <label className="m-1.5 flex h-8 items-center gap-2 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-2">
              <Search size={11} className="text-[rgb(var(--muted-2))]" />
              <input
                autoFocus
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder="Filter…"
                className="min-w-0 flex-1 bg-transparent text-[10px] outline-none placeholder:text-[rgb(var(--muted-2))]"
              />
            </label>
          )}
          <div role="listbox" aria-label={ariaLabel} className="max-h-64 overflow-auto p-1">
            {visible.map((option) => {
              const active = option.value === value
              return (
                <button
                  type="button"
                  role="option"
                  aria-selected={active}
                  disabled={option.disabled}
                  key={option.value}
                  onClick={() => {
                    onChange(option.value)
                    setOpen(false)
                    setQuery('')
                  }}
                  className={
                    'flex w-full items-start gap-2 rounded-md px-2 py-2 text-left transition-colors disabled:cursor-not-allowed disabled:opacity-40 ' +
                    (active ? 'bg-[rgb(var(--purple)/.12)]' : 'hover:bg-[rgb(var(--panel-3))]')
                  }
                >
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-[10px] text-[rgb(var(--text))]">{option.label}</span>
                    {option.description && <span className="mt-0.5 block truncate text-[8px] text-[rgb(var(--muted-2))]">{option.description}</span>}
                  </span>
                  {option.meta && <span className="mt-0.5 text-[8px] text-[rgb(var(--muted-2))]">{option.meta}</span>}
                  {active && <Check size={11} className="mt-0.5 shrink-0 text-[rgb(var(--purple))]" />}
                </button>
              )
            })}
            {!visible.length && <div className="px-2 py-5 text-center text-[9px] text-[rgb(var(--muted-2))]">No matching options</div>}
          </div>
        </div>
      )}
    </div>
  )
}
