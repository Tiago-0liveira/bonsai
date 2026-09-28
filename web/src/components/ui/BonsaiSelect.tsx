import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Check, ChevronDown, Search } from 'lucide-react'

export interface BonsaiSelectOption {
  value: string
  label: string
  description?: string
  disabled?: boolean
  meta?: string
}

interface MenuPosition {
  left: number
  top: number
  width: number
  maxHeight: number
  opensUp: boolean
}

const VIEWPORT_GAP = 8
const MENU_GAP = 5
const MIN_MENU_HEIGHT = 150
const DEFAULT_MENU_HEIGHT = 280

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
  const [position, setPosition] = useState<MenuPosition | null>(null)
  const rootRef = useRef<HTMLDivElement | null>(null)
  const triggerRef = useRef<HTMLButtonElement | null>(null)
  const menuRef = useRef<HTMLDivElement | null>(null)
  const selected = options.find((option) => option.value === value)

  const visible = useMemo(() => {
    const needle = query.trim().toLowerCase()
    if (!needle) return options
    return options.filter((option) =>
      (option.label + ' ' + (option.description ?? '') + ' ' + (option.meta ?? '')).toLowerCase().includes(needle),
    )
  }, [options, query])

  const updatePosition = useCallback(() => {
    const trigger = triggerRef.current
    if (!trigger) return
    const rect = trigger.getBoundingClientRect()
    const viewportWidth = window.innerWidth
    const viewportHeight = window.innerHeight
    const width = Math.min(Math.max(rect.width, 210), viewportWidth - VIEWPORT_GAP * 2)
    const spaceBelow = viewportHeight - rect.bottom - VIEWPORT_GAP - MENU_GAP
    const spaceAbove = rect.top - VIEWPORT_GAP - MENU_GAP
    const opensUp = spaceBelow < MIN_MENU_HEIGHT && spaceAbove > spaceBelow
    const available = Math.max(96, opensUp ? spaceAbove : spaceBelow)
    const maxHeight = Math.min(DEFAULT_MENU_HEIGHT, available)
    const unclampedLeft = rect.left
    const left = Math.max(VIEWPORT_GAP, Math.min(unclampedLeft, viewportWidth - width - VIEWPORT_GAP))
    const top = opensUp
      ? Math.max(VIEWPORT_GAP, rect.top - MENU_GAP - maxHeight)
      : Math.min(viewportHeight - VIEWPORT_GAP - maxHeight, rect.bottom + MENU_GAP)
    setPosition({ left, top, width, maxHeight, opensUp })
  }, [])

  useEffect(() => {
    if (!open) return
    updatePosition()
    const onViewportChange = () => updatePosition()
    window.addEventListener('resize', onViewportChange)
    window.addEventListener('scroll', onViewportChange, true)
    return () => {
      window.removeEventListener('resize', onViewportChange)
      window.removeEventListener('scroll', onViewportChange, true)
    }
  }, [open, updatePosition])

  useEffect(() => {
    if (!open) return
    const close = (event: PointerEvent) => {
      const target = event.target as Node
      if (rootRef.current?.contains(target) || menuRef.current?.contains(target)) return
      setOpen(false)
      setQuery('')
    }
    window.addEventListener('pointerdown', close)
    return () => window.removeEventListener('pointerdown', close)
  }, [open])

  const menu =
    open && position && typeof document !== 'undefined'
      ? createPortal(
          <div
            ref={menuRef}
            role="presentation"
            data-bonsai-select-menu={ariaLabel}
            className="fixed z-[300] flex overflow-hidden rounded-lg border border-[rgb(var(--border-strong))] bg-[rgb(var(--panel-2))] shadow-[0_20px_60px_rgb(0_0_0/.62)]"
            style={{
              left: position.left,
              top: position.top,
              width: position.width,
              maxHeight: position.maxHeight,
              flexDirection: 'column',
            }}
          >
            {searchable && (
              <label className="m-1.5 flex h-8 shrink-0 items-center gap-2 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-2">
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
            <div role="listbox" aria-label={ariaLabel} className="min-h-0 flex-1 overflow-auto p-1">
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
                    {option.meta && <span className="mt-0.5 shrink-0 text-[8px] text-[rgb(var(--muted-2))]">{option.meta}</span>}
                    {active && <Check size={11} className="mt-0.5 shrink-0 text-[rgb(var(--purple))]" />}
                  </button>
                )
              })}
              {!visible.length && <div className="px-2 py-5 text-center text-[9px] text-[rgb(var(--muted-2))]">No matching options</div>}
            </div>
          </div>,
          document.body,
        )
      : null

  return (
    <div ref={rootRef} className="relative min-w-0">
      <button
        ref={triggerRef}
        type="button"
        aria-label={ariaLabel}
        aria-haspopup="listbox"
        aria-expanded={open}
        disabled={disabled}
        onClick={() => {
          setOpen((current) => {
            const next = !current
            if (next) requestAnimationFrame(updatePosition)
            return next
          })
        }}
        className={
          'bonsai-focus flex w-full items-center gap-2 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-2.5 text-left text-[rgb(var(--text))] outline-none transition-colors hover:border-[rgb(var(--border-strong))] disabled:cursor-not-allowed disabled:opacity-50 ' +
          (compact ? 'h-7 text-[9px]' : 'h-9 text-[11px]')
        }
      >
        <span className={'min-w-0 flex-1 truncate ' + (!selected ? 'text-[rgb(var(--muted-2))]' : '')}>{selected?.label ?? placeholder}</span>
        {selected?.meta && <span className="shrink-0 text-[8px] text-[rgb(var(--muted-2))]">{selected.meta}</span>}
        <ChevronDown size={12} className={'shrink-0 text-[rgb(var(--muted-2))] transition-transform ' + (open ? 'rotate-180' : '')} />
      </button>
      {menu}
    </div>
  )
}
