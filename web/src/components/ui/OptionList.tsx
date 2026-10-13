import { useEffect, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from 'react'
import { Check, Search } from 'lucide-react'

export interface ListOption {
  value: string
  label: string
  description?: string
  meta?: ReactNode
  icon?: ReactNode
  disabled?: boolean
}

const RENDER_LIMIT = 150

/**
 * A single choice shown inline instead of behind a menu: every option is
 * visible, a filter narrows a long list, and the arrow keys and Enter pick
 * from the filter box without leaving the keyboard.
 */
export function OptionList({ options, value, onChange, ariaLabel, filterLabel = 'Filter', searchable = false, placeholder = 'Filter…', emptyText = 'Nothing matches.', height = 224, mono = false, disabled = false }: {
  options: ListOption[]
  value: string
  onChange: (value: string) => void
  ariaLabel: string
  /** Names the filter box; it must not contain the list's own label. */
  filterLabel?: string
  searchable?: boolean
  placeholder?: string
  emptyText?: string
  /** Pixel height of the scrolling list. */
  height?: number
  /** Labels in the monospace face, for branch names and commands. */
  mono?: boolean
  disabled?: boolean
}) {
  const [query, setQuery] = useState('')
  const [active, setActive] = useState(-1)
  const listRef = useRef<HTMLDivElement | null>(null)
  const matches = useMemo(() => {
    const needle = query.trim().toLowerCase()
    if (!needle) return options
    return options.filter(option => (option.label + ' ' + (option.description ?? '')).toLowerCase().includes(needle))
  }, [options, query])
  const visible = matches.slice(0, RENDER_LIMIT)
  const selectedIndex = visible.findIndex(option => option.value === value)

  // Keep the chosen row in view when the list or the choice changes from outside.
  useEffect(() => {
    listRef.current?.querySelector('[aria-selected="true"]')?.scrollIntoView?.({ block: 'nearest' })
  }, [value, options])
  useEffect(() => {
    listRef.current?.querySelector('[data-active="true"]')?.scrollIntoView?.({ block: 'nearest' })
  }, [active])

  const move = (step: number) => {
    if (!visible.length) return
    setActive(current => {
      const from = current >= 0 ? current : selectedIndex
      return (from + step + visible.length) % visible.length
    })
  }
  const onKeyDown = (event: KeyboardEvent) => {
    if (event.key === 'ArrowDown') { event.preventDefault(); move(1) }
    else if (event.key === 'ArrowUp') { event.preventDefault(); move(-1) }
    else if (event.key === 'Enter' && active >= 0 && visible[active] && !visible[active].disabled) {
      // Enter picks the highlighted row; it must not also submit the dialog.
      event.preventDefault()
      onChange(visible[active].value)
    } else if (event.key === 'Escape' && query) {
      event.preventDefault()
      event.stopPropagation()
      setQuery('')
    }
  }

  return (
    <div className="overflow-hidden rounded-[10px] border border-border bg-well" onKeyDown={searchable ? undefined : onKeyDown}>
      {searchable && (
        <label className="flex h-9 items-center gap-2 border-b border-border-subtle px-3 focus-within:bg-panel/60">
          <Search size={12} className="shrink-0 text-muted-2" />
          <input
            aria-label={filterLabel}
            value={query}
            disabled={disabled}
            autoComplete="off"
            spellCheck={false}
            placeholder={placeholder}
            onChange={event => { setQuery(event.target.value); setActive(0) }}
            onKeyDown={onKeyDown}
            className="min-w-0 flex-1 bg-transparent text-[12px] outline-none placeholder:text-muted-2"
          />
          <span className="font-mono text-[10px] text-muted-2">{matches.length}</span>
        </label>
      )}
      <div
        ref={listRef}
        role="listbox"
        aria-label={ariaLabel}
        tabIndex={searchable ? -1 : 0}
        className="overflow-auto p-1.5 outline-none focus-visible:ring-1 focus-visible:ring-accent"
        style={{ height }}
      >
        {visible.map((option, index) => {
          const selected = option.value === value
          return (
            <button
              type="button"
              role="option"
              key={option.value}
              aria-selected={selected}
              data-active={index === active || undefined}
              disabled={disabled || option.disabled}
              onMouseDown={event => event.preventDefault()}
              onMouseEnter={() => setActive(index)}
              onClick={() => onChange(option.value)}
              className="pick-row bonsai-focus disabled:cursor-not-allowed disabled:opacity-40"
            >
              {option.icon && <span aria-hidden="true" className="shrink-0 text-muted">{option.icon}</span>}
              <span className="min-w-0 flex-1">
                <span className={'block truncate text-text ' + (mono ? 'font-mono text-[11.5px]' : 'text-[12px]')}>{option.label}</span>
                {option.description && <span className="mt-0.5 block truncate font-mono text-[10px] text-muted-2">{option.description}</span>}
              </span>
              {option.meta && <span className="shrink-0 font-mono text-[10px] text-muted-2">{option.meta}</span>}
              <Check size={13} aria-hidden="true" className={'shrink-0 text-accent transition-opacity ' + (selected ? 'opacity-100' : 'opacity-0')} />
            </button>
          )
        })}
        {!visible.length && <div className="grid h-full place-items-center px-3 text-center text-[11px] text-muted-2">{emptyText}</div>}
        {matches.length > visible.length && <div className="px-3 py-2 text-center font-mono text-[10px] text-muted-2">+{matches.length - visible.length} more. Keep typing to narrow.</div>}
      </div>
    </div>
  )
}
