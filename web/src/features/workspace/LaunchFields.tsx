import { Shield } from 'lucide-react'
import { BonsaiSelect } from '../../components/ui/BonsaiSelect'
import type { AgentAccount, AgentModel, LaunchField, LaunchFieldOption, LaunchOptions } from '../../api/agents'
import { launchOptionLabel } from '../../api/launchDetails'

/** Dialog state: one value per launch field ID, a string for a select and a boolean for a toggle. */
export type LaunchValues = Record<string, string | boolean>

const toggleValue = (on: boolean) => (on ? 'true' : 'false')

/** Initial values for a provider: select defaults, and toggles from the profile's own setting. */
export function initialLaunchValues(fields: LaunchField[], account?: AgentAccount): LaunchValues {
  const values: LaunchValues = {}
  for (const field of fields) {
    if (field.kind === 'toggle') {
      const saved = account?.[field.id]
      values[field.id] = typeof saved === 'boolean' ? saved : field.default === 'true'
    } else {
      values[field.id] = field.default ?? field.options?.[0]?.value ?? ''
    }
  }
  return values
}

/** The model the launch uses: the typed one, or the provider default when empty. */
export function chosenModel(models: AgentModel[], model: string): AgentModel | undefined {
  const id = model.trim()
  return id ? models.find(item => item.id === id) : models.find(item => item.default)
}

/**
 * The options a field offers. A per-model field lists the chosen model's
 * efforts, keeping the field's own empty "default" option; typed or unlisted
 * models fall back to the field's options.
 */
export function fieldOptions(field: LaunchField, model?: AgentModel): LaunchFieldOption[] {
  const own = field.options ?? []
  if (!field.per_model || !model?.efforts?.length) return own
  return [...own.filter(option => option.value === ''), ...model.efforts.map(value => ({ value, label: value }))]
}

/** Drops select values the current options no longer offer, such as an effort the new model lacks. */
export function resolveLaunchValues(fields: LaunchField[], values: LaunchValues, model?: AgentModel): LaunchValues {
  const resolved = { ...values }
  for (const field of fields) {
    if (field.kind !== 'select') continue
    const options = fieldOptions(field, model)
    if (options.length && !options.some(option => option.value === resolved[field.id])) {
      resolved[field.id] = options.some(option => option.value === '') ? '' : field.default ?? options[0].value
    }
  }
  return resolved
}

/** Start request fields: presets send their fields, empty selects send nothing, toggles always send. */
export function launchRequestOptions(fields: LaunchField[], values: LaunchValues): LaunchOptions {
  const out: LaunchOptions = {}
  for (const field of fields) {
    const value = values[field.id]
    if (field.kind === 'toggle') {
      out[field.id] = value === true
      continue
    }
    const option = field.options?.find(item => item.value === value)
    if (option?.set) Object.assign(out, option.set)
    else if (typeof value === 'string' && value) out[field.id] = value
  }
  return out
}

/** Labels of the chosen non-default values, for the footer and the profile defaults. */
export function launchSummary(fields: LaunchField[], values: Record<string, unknown>, model?: AgentModel): string[] {
  const out: string[] = []
  for (const field of fields) {
    const value = values[field.id]
    if (field.kind === 'toggle') {
      if (typeof value !== 'boolean') continue
      const option = field.options?.find(item => item.value === toggleValue(value))
      if (option) out.push(option.label)
      continue
    }
    if (typeof value !== 'string' || !value) continue
    const option = fieldOptions(field, model).find(item => item.value === value) ?? field.options?.find(item => item.value === value)
    out.push(option ? launchOptionLabel(field, option) : value)
  }
  return out
}

const LABEL = 'mb-1.5 block font-mono text-[10px] font-semibold uppercase tracking-[.1em] text-muted-2'

/** Renders the provider's launch fields: selects side by side, then one row per toggle. */
export function LaunchFields({ fields, values, model, providerLabel, disabled, onChange }: {
  fields: LaunchField[]
  values: LaunchValues
  model?: AgentModel
  providerLabel: string
  disabled: boolean
  onChange: (id: string, value: string | boolean) => void
}) {
  const selects = fields.filter(field => field.kind === 'select')
  const toggles = fields.filter(field => field.kind === 'toggle')
  return (
    <>
      {selects.length > 0 && (
        <div className="grid grid-cols-2 gap-3">
          {selects.map(field => {
            const options = fieldOptions(field, model)
            const chosen = options.find(option => option.value === values[field.id])
            const note = chosen?.tone === 'danger' ? chosen.help || field.help : ''
            return (
              <label key={field.id} className="block">
                <span className={LABEL}>{field.label}</span>
                <BonsaiSelect ariaLabel={field.label} disabled={disabled} value={String(values[field.id] ?? '')} onChange={value => onChange(field.id, value)} options={options.map(option => ({ value: option.value, label: option.label }))} />
                {note && <span role="note" className="mt-1.5 flex items-center gap-1.5 font-mono text-[10px] text-warn"><Shield size={11} />{note}</span>}
              </label>
            )
          })}
        </div>
      )}
      {toggles.map(field => {
        const on = values[field.id] === true
        const danger = field.options?.find(option => option.value === toggleValue(on))?.tone === 'danger'
        return (
          <div key={field.id} className="flex items-center gap-3 rounded-[10px] border border-border bg-well p-3">
            <Shield size={15} className={danger ? 'text-warn' : 'text-muted'} />
            <div className="flex-1"><div className="text-[12px] font-medium">{field.label}</div>{field.help && <p className="mt-1 font-mono text-[10px] text-muted-2">{field.help}</p>}</div>
            <button type="button" role="switch" aria-label={`${providerLabel} ${field.label.toLowerCase()}`} aria-checked={on} disabled={disabled} onClick={() => onChange(field.id, !on)} className={'bonsai-focus flex h-5 w-9 items-center rounded-full border p-0.5 transition-colors ' + (danger ? 'border-warn-solid/22 bg-warn-solid/30' : on ? 'border-accent/40 bg-accent-solid/30' : 'border-border-strong bg-panel-3')}><span className={'h-3.5 w-3.5 rounded-full bg-text transition-transform ' + (on ? 'translate-x-4' : '')} /></button>
          </div>
        )
      })}
    </>
  )
}
