import { Flame, Gauge, RotateCcw, Shield } from 'lucide-react'
import { ChoiceChips, type ChoiceOption } from '../../components/ui/ChoiceChips'
import { Field } from '../../components/ui/Dialog'
import { Reveal } from '../../components/ui/Dialog'
import { StepSlider, heatLevel } from '../../components/ui/StepSlider'
import type { AgentAccount, AgentModel, LaunchField } from '../../api/agents'
import { fieldOptions, type LaunchValues } from './LaunchFields'

const LEVEL_NAMES: Record<string, string> = { none: 'None', minimal: 'Minimal', low: 'Low', medium: 'Medium', high: 'High', xhigh: 'Extra high', max: 'Max', ultra: 'Ultra' }
const levelName = (label: string) => LEVEL_NAMES[label] ?? label

/** The value a profile saved for a field, when it saved one. */
function profileValue(field: LaunchField, account?: AgentAccount): string {
  const own = account?.[field.id]
  if (typeof own === 'string') return own
  const saved = account?.defaults?.[field.id]
  return typeof saved === 'string' ? saved : ''
}

/** The "use the profile's own setting" switch every field with a default shares. */
function DefaultChip({ label, detail, pressed, disabled, onClick }: { label: string; detail?: string; pressed: boolean; disabled: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      aria-pressed={pressed}
      disabled={disabled || pressed}
      onClick={onClick}
      className={'bonsai-focus inline-flex h-5 items-center gap-1 rounded-[5px] border px-1.5 font-mono text-[10px] normal-case tracking-normal transition-colors ' + (pressed ? 'border-accent/40 bg-accent/10 text-accent' : 'border-border text-muted hover:border-border-strong hover:text-text')}
    >
      <RotateCcw size={9} aria-hidden="true" />
      {label}{pressed && detail ? ` · ${detail}` : ''}
    </button>
  )
}

function Note({ children }: { children: string }) {
  return <Reveal><p role="note" className="mt-2 flex items-start gap-1.5 font-mono text-[10px] leading-4 text-warn"><Shield size={11} className="mt-px shrink-0" />{children}</p></Reveal>
}

/** A select or toggle field as chips, with its danger note and its profile default. */
function ChoiceField({ field, value, model, models, account, defaultValue, disabled, onChange }: {
  field: LaunchField
  value: string | boolean | undefined
  model?: AgentModel
  models?: AgentModel[]
  account?: AgentAccount
  /** The option the provider uses when nothing is set: tagged "default" and one click away. */
  defaultValue?: string
  disabled: boolean
  onChange: (value: string | boolean) => void
}) {
  const toggle = field.kind === 'toggle'
  const all = fieldOptions(field, model, models)
  const reset = toggle ? undefined : all.find(option => option.value === '')
  const choices = all.filter(option => option.value !== '')
  const current = toggle ? String(value === true) : String(value ?? '')
  const chosen = choices.find(option => option.value === current)
  const inherited = profileValue(field, account)
  const inheritedLabel = choices.find(option => option.value === inherited)?.label
  const options: ChoiceOption[] = choices.map(option => ({
    value: option.value,
    label: option.label,
    tone: option.tone,
    badge: defaultValue !== undefined && option.value === defaultValue ? 'default' : undefined,
    icon: option.tone === 'danger' ? <Shield size={12} aria-hidden="true" /> : undefined,
  }))
  const note = chosen?.tone === 'danger' ? chosen.help || field.help : ''
  const defaultLabel = defaultValue === undefined ? undefined : choices.find(option => option.value === defaultValue)?.label
  const aside = defaultValue !== undefined && defaultLabel
    ? <DefaultChip label={`Default · ${defaultLabel}`} pressed={current === defaultValue} disabled={disabled} onClick={() => onChange(defaultValue)} />
    : reset && <DefaultChip label={reset.label} detail={inheritedLabel} pressed={current === ''} disabled={disabled} onClick={() => onChange('')} />
  return (
    <Field
      label={field.label}
      aside={aside}
      hint={!note && toggle ? field.help : undefined}
    >
      <ChoiceChips
        ariaLabel={field.label}
        disabled={disabled}
        options={options}
        value={current}
        inherited={inherited}
        onChange={next => onChange(toggle ? next === 'true' : next)}
      />
      {note && <Note>{note}</Note>}
    </Field>
  )
}

/** The effort slot when there is nothing to set: it keeps its height so the dialog never reflows. */
function NoEffort({ text }: { text: string }) {
  return (
    <div className="flex h-[74px] items-center justify-center gap-2 rounded-[10px] border border-dashed border-border bg-well/40 px-3 text-center font-mono text-[10.5px] text-muted-2">
      <Gauge size={13} className="shrink-0" aria-hidden="true" />
      {text}
    </div>
  )
}

/** The effort field as a slider; fewer than two levels fall back to chips. */
function EffortField({ field, value, model, models, account, defaultValue, disabled, onChange }: {
  field: LaunchField
  value: string
  model?: AgentModel
  models?: AgentModel[]
  account?: AgentAccount
  defaultValue?: string
  disabled: boolean
  onChange: (value: string) => void
}) {
  const all = fieldOptions(field, model, models)
  const reset = all.find(option => option.value === '')
  const levels = all.filter(option => option.value !== '')
  if (levels.length === 0 && model?.no_effort) return <Field label={field.label}><NoEffort text={`${model.label} has no effort setting.`} /></Field>
  if (levels.length < 2) return <ChoiceField field={field} value={value} model={model} models={models} account={account} defaultValue={defaultValue} disabled={disabled} onChange={next => onChange(String(next))} />
  const stops = levels.map(option => ({ value: option.value, label: option.label === option.value ? levelName(option.label) : option.label, tick: option.label }))
  const inherited = profileValue(field, account)
  const current = stops.find(stop => stop.value === value)
  const fallback = stops.find(stop => stop.value === inherited)
  const defaultStop = stops.find(stop => stop.value === defaultValue)
  // Past the middle of the scale the label gets a flame, as the slider does.
  const heat = current ? heatLevel(stops.indexOf(current), stops.length, current.value) : 0
  const aside = defaultStop
    ? <DefaultChip label={`Default · ${defaultStop.label}`} pressed={value === defaultStop.value} disabled={disabled} onClick={() => onChange(defaultStop.value)} />
    : reset && <DefaultChip label={reset.label} detail={fallback?.label} pressed={value === ''} disabled={disabled} onClick={() => onChange('')} />
  return (
    <Field
      label={<>{field.label}{current && <span key={current.value} data-heat={heat} className={'effort-label dialog-enter ml-1.5 inline-flex items-center gap-1 text-[12px] font-semibold normal-case tracking-normal ' + (heat === 0 ? 'text-text' : '')}>{heat > 0 && <Flame size={13} strokeWidth={2.4} aria-hidden="true" className="effort-flame" />}{current.label}</span>}</>}
      aside={aside}
    >
      <StepSlider ariaLabel={field.label} stops={stops} value={value} inherited={inherited} defaultValue={defaultStop?.value} heat unsetText={reset?.label} disabled={disabled} onChange={onChange} />
    </Field>
  )
}

/**
 * The provider's launch fields in one fixed frame: the access choice (chips),
 * then effort (a slider). A provider without one of them keeps its slot, so
 * switching providers never reflows the dialog.
 */
export function LaunchControls({ fields, values, model, models, account, defaults, providerLabel, disabled, onChange }: {
  fields: LaunchField[]
  values: LaunchValues
  model?: AgentModel
  /** Every model the provider lists: the effort levels of a typed model come from these. */
  models?: AgentModel[]
  account?: AgentAccount
  /** The value each field takes when nothing is set, by field ID. Tagged "default" and selectable in one click. */
  defaults?: Record<string, string>
  providerLabel: string
  disabled: boolean
  onChange: (id: string, value: string | boolean) => void
}) {
  const effort = fields.find(field => field.id === 'effort' && field.kind === 'select')
  const access = fields.filter(field => field !== effort)
  return (
    <div className="space-y-5">
      {access.map(field => (
        <ChoiceField key={field.id} field={field} value={values[field.id]} model={model} models={models} account={account} defaultValue={defaults?.[field.id]} disabled={disabled} onChange={value => onChange(field.id, value)} />
      ))}
      {effort ? (
        <EffortField field={effort} value={String(values[effort.id] ?? '')} model={model} models={models} account={account} defaultValue={defaults?.[effort.id]} disabled={disabled} onChange={value => onChange(effort.id, value)} />
      ) : (
        <Field label="Effort">
          <NoEffort text={providerLabel ? `${providerLabel} has no effort setting.` : 'Choose a provider to set effort.'} />
        </Field>
      )}
    </div>
  )
}
