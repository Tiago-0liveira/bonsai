import { Minus, Plus } from 'lucide-react'

/**
 * A bounded whole number with − and + buttons. The field keeps the raw text so
 * a half-typed value survives; `valid` is the caller's to check.
 */
export function NumberStepper({ value, onChange, ariaLabel, min = 0, max = 100, disabled = false, readOnly = false }: {
  value: string
  onChange: (value: string) => void
  ariaLabel: string
  min?: number
  max?: number
  disabled?: boolean
  /** Shows a value that is not editable here, such as an inherited default. */
  readOnly?: boolean
}) {
  const number = /^\d+$/.test(value) ? Number(value) : NaN
  const nudge = (step: number) => {
    const base = Number.isNaN(number) ? min : number
    onChange(String(Math.min(Math.max(base + step, min), max)))
  }
  const off = disabled || readOnly
  const invalid = !readOnly && (Number.isNaN(number) || number < min || number > max)
  return (
    <div className={'num-field inline-flex h-9 items-stretch overflow-hidden rounded-[8px] border bg-well transition-colors ' + (invalid ? 'border-danger/60' : 'border-border focus-within:border-accent/55')}>
      <button type="button" tabIndex={-1} aria-label="Decrease" disabled={off || number <= min} onClick={() => nudge(-1)} className="grid w-9 place-items-center text-muted transition-colors hover:bg-panel-3 hover:text-text disabled:opacity-40"><Minus size={13} /></button>
      <input
        aria-label={ariaLabel}
        type="number"
        inputMode="numeric"
        min={min}
        max={max}
        step={1}
        disabled={disabled}
        readOnly={readOnly}
        value={value}
        onChange={event => onChange(event.target.value)}
        className="bonsai-focus w-14 border-x border-border-subtle bg-transparent text-center font-mono text-[12.5px] outline-none disabled:opacity-60"
      />
      <button type="button" tabIndex={-1} aria-label="Increase" disabled={off || number >= max} onClick={() => nudge(1)} className="grid w-9 place-items-center text-muted transition-colors hover:bg-panel-3 hover:text-text disabled:opacity-40"><Plus size={13} /></button>
    </div>
  )
}
