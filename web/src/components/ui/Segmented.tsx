import type { ReactNode } from 'react'

export interface SegmentOption {
  value: string
  label: ReactNode
  disabled?: boolean
  title?: string
}

/**
 * A row of equal choices with a highlight that slides to the chosen one.
 * Buttons carry aria-pressed, so each reads as a toggle in its group.
 */
export function Segmented({ options, value, onChange, ariaLabel, disabled = false, itemClassName = 'h-9 text-[12px]' }: {
  options: SegmentOption[]
  value: string
  onChange: (value: string) => void
  ariaLabel: string
  disabled?: boolean
  itemClassName?: string
}) {
  const index = options.findIndex(option => option.value === value)
  const count = Math.max(options.length, 1)
  return (
    <div role="group" aria-label={ariaLabel} className="seg" style={{ gridTemplateColumns: `repeat(${count}, minmax(0, 1fr))` }}>
      <span
        aria-hidden="true"
        className="seg-pill"
        style={{ width: `calc((100% - 8px) / ${count})`, transform: `translateX(${Math.max(index, 0) * 100}%)`, opacity: index < 0 ? 0 : 1 }}
      />
      {options.map(option => {
        const active = option.value === value
        return (
          <button
            type="button"
            key={option.value}
            disabled={disabled || option.disabled}
            aria-pressed={active}
            title={option.title}
            onClick={() => onChange(option.value)}
            className={'seg-item bonsai-focus px-2 disabled:cursor-not-allowed disabled:opacity-40 ' + itemClassName + ' ' + (active ? 'text-text' : 'text-muted hover:text-text')}
          >
            {option.label}
          </button>
        )
      })}
    </div>
  )
}
