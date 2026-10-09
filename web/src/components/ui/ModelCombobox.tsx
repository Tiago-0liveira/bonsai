import { useEffect, useId, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Check } from 'lucide-react'

export interface ModelSuggestion {
  id: string
  label: string
  description?: string
  source?: string
}

const GAP = 5
const MAX_HEIGHT = 240

/**
 * A text input that suggests known models. The value is always free text, so a
 * model that is not listed (a new release, a private alias) can still be typed.
 * Without suggestions it behaves as a plain input.
 */
export function ModelCombobox({
  value, onChange, suggestions, ariaLabel, placeholder, disabled = false, loading = false,
}: {
  value: string
  onChange: (value: string) => void
  suggestions: ModelSuggestion[]
  ariaLabel: string
  placeholder?: string
  disabled?: boolean
  loading?: boolean
}) {
  const listId = useId()
  const inputRef = useRef<HTMLInputElement | null>(null)
  const menuRef = useRef<HTMLDivElement | null>(null)
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(-1)
  const [box, setBox] = useState<{ left: number; top: number; width: number; maxHeight: number; up: boolean } | null>(null)

  // Typing filters the list; an exact pick shows everything again so the user can switch.
  const visible = useMemo(() => {
    const needle = value.trim().toLowerCase()
    if (!needle || suggestions.some(item => item.id === value)) return suggestions
    return suggestions.filter(item => (item.id + ' ' + item.label).toLowerCase().includes(needle))
  }, [suggestions, value])

  const place = () => {
    const input = inputRef.current
    if (!input) return
    const rect = input.getBoundingClientRect()
    const below = window.innerHeight - rect.bottom - GAP - 8
    const above = rect.top - GAP - 8
    const up = below < 140 && above > below
    const maxHeight = Math.max(96, Math.min(MAX_HEIGHT, up ? above : below))
    setBox({ left: rect.left, top: up ? rect.top - GAP - maxHeight : rect.bottom + GAP, width: rect.width, maxHeight, up })
  }

  useEffect(() => {
    if (!open) return
    place()
    const close = (event: PointerEvent) => {
      const target = event.target as Node
      if (inputRef.current?.contains(target) || menuRef.current?.contains(target)) return
      setOpen(false)
    }
    window.addEventListener('pointerdown', close)
    window.addEventListener('resize', place)
    window.addEventListener('scroll', place, true)
    return () => {
      window.removeEventListener('pointerdown', close)
      window.removeEventListener('resize', place)
      window.removeEventListener('scroll', place, true)
    }
  }, [open])

  const pick = (item: ModelSuggestion) => {
    onChange(item.id)
    setOpen(false)
    setActive(-1)
  }
  const showMenu = open && !disabled && visible.length > 0

  return (
    <>
      <input
        ref={inputRef}
        role="combobox"
        aria-label={ariaLabel}
        aria-expanded={showMenu}
        aria-controls={listId}
        aria-autocomplete="list"
        aria-activedescendant={showMenu && active >= 0 ? `${listId}-${active}` : undefined}
        aria-busy={loading || undefined}
        autoComplete="off"
        spellCheck={false}
        disabled={disabled}
        value={value}
        placeholder={placeholder}
        onFocus={() => suggestions.length && setOpen(true)}
        onClick={() => suggestions.length && setOpen(true)}
        onChange={event => { onChange(event.target.value); setActive(-1); if (suggestions.length) setOpen(true) }}
        onKeyDown={event => {
          if (event.key === 'Escape' && open) { event.stopPropagation(); setOpen(false) }
          else if (event.key === 'ArrowDown' && visible.length) { event.preventDefault(); setOpen(true); setActive(index => (index + 1) % visible.length) }
          else if (event.key === 'ArrowUp' && visible.length) { event.preventDefault(); setOpen(true); setActive(index => (index <= 0 ? visible.length - 1 : index - 1)) }
          else if (event.key === 'Enter' && showMenu && active >= 0) { event.preventDefault(); pick(visible[active]) }
        }}
        className="bonsai-focus h-8 w-full rounded-[7px] border border-border bg-well px-2.5 text-[12px] outline-none placeholder:text-muted-2 focus:border-accent/55 disabled:opacity-50"
      />
      {showMenu && box && createPortal(
        <div
          ref={menuRef}
          id={listId}
          role="listbox"
          aria-label={`${ariaLabel} suggestions`}
          className="fixed z-[300] overflow-auto rounded-[10px] border border-border-strong bg-panel-3 p-1 shadow-overlay"
          style={{ left: box.left, top: box.top, width: box.width, maxHeight: box.maxHeight }}
        >
          {visible.map((item, index) => (
            <button
              type="button"
              role="option"
              id={`${listId}-${index}`}
              key={item.id}
              aria-selected={item.id === value}
              // Keep focus in the input so the list does not close before the click lands.
              onMouseDown={event => event.preventDefault()}
              onClick={() => pick(item)}
              onMouseEnter={() => setActive(index)}
              className={'flex w-full items-start gap-2 rounded-md px-2 py-1.5 text-left transition-colors ' + (index === active ? 'bg-panel-4' : item.id === value ? 'bg-accent/12' : 'hover:bg-panel-4')}
            >
              <span className="min-w-0 flex-1">
                <span className="block truncate text-[12px] text-text">{item.label}</span>
                {item.description && item.description !== item.label && <span className="mt-0.5 block truncate font-mono text-[10px] text-muted-2">{item.description}</span>}
              </span>
              {item.source === 'alias' && <span className="mt-0.5 shrink-0 font-mono text-[10px] text-muted-2">latest</span>}
              {item.id === value && <Check size={11} className="mt-0.5 shrink-0 text-accent" />}
            </button>
          ))}
        </div>,
        document.body,
      )}
    </>
  )
}
