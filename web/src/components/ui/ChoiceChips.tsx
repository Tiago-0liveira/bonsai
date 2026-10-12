import type { ReactNode } from 'react'
import { Check } from 'lucide-react'

export interface ChoiceOption {
  value: string
  label: string
  icon?: ReactNode
  /** "danger": the choice lets the agent act without asking. */
  tone?: 'danger'
  disabled?: boolean
  title?: string
}

/**
 * Single choice as wrapping chips. Without a chosen value, `inherited` marks
 * the chip the surrounding default resolves to, with a dashed outline.
 */
export function ChoiceChips({ options, value, onChange, ariaLabel, disabled = false, inherited }: {
  options: ChoiceOption[]
  value: string
  onChange: (value: string) => void
  ariaLabel: string
  disabled?: boolean
  inherited?: string
}) {
  return (
    <div role="radiogroup" aria-label={ariaLabel} className="flex flex-wrap gap-2">
      {options.map(option => {
        const active = option.value === value
        return (
          <button
            type="button"
            role="radio"
            key={option.value}
            aria-checked={active}
            disabled={disabled || option.disabled}
            title={option.title}
            data-tone={option.tone}
            data-inherited={!value && inherited === option.value ? 'true' : undefined}
            onClick={() => onChange(option.value)}
            className="chip-choice bonsai-focus disabled:cursor-not-allowed disabled:opacity-40"
          >
            {option.icon}
            <span>{option.label}</span>
            <Check size={12} className="chip-mark" aria-hidden="true" />
          </button>
        )
      })}
    </div>
  )
}
