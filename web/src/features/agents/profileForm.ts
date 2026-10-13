import type { AgentModel, LaunchField } from '../../api/agents'
import { fieldOptions, type LaunchValues } from '../workspace/LaunchFields'

/** Profile names as the API accepts them: a letter or digit first, then letters, digits, `.`, `_`, `-`; at most 64. */
const NAME_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/
export const NAME_RULE = "Names start with a letter or digit, then use letters, digits, '.', '_' or '-' (at most 64)"

/** The problem with a profile name, or '' when it is acceptable. `taken` lists the other names of the same provider. */
export function profileNameProblem(name: string, taken: string[]): string {
  if (!NAME_PATTERN.test(name)) return NAME_RULE
  if (taken.includes(name)) return 'Another profile of this provider already has this name'
  return ''
}

/**
 * Form values from saved defaults: a select or the model is its saved value or
 * '' (provider default); a toggle is on only when saved as "true".
 */
export function profileFormValues(fields: LaunchField[], defaults: Record<string, string> = {}): LaunchValues {
  const values: LaunchValues = {}
  for (const field of fields) {
    values[field.id] = field.kind === 'toggle' ? defaults[field.id] === 'true' : defaults[field.id] ?? ''
  }
  return values
}

/**
 * The PATCH `defaults` that turn the saved defaults into the form values:
 * changed fields only, with null for a field returned to the provider default.
 */
export function profileChanges(fields: LaunchField[], defaults: Record<string, string> = {}, values: LaunchValues): Record<string, string | null> {
  const changes: Record<string, string | null> = {}
  for (const field of fields) {
    const saved = defaults[field.id] ?? ''
    const value = values[field.id]
    if (field.kind === 'toggle') {
      const on = value === true
      if (on !== (saved === 'true')) changes[field.id] = on ? 'true' : null
      continue
    }
    const next = typeof value === 'string' ? value.trim() : ''
    if (next !== saved) changes[field.id] = next === '' ? null : next
  }
  return changes
}

/**
 * What each field takes when the profile saved nothing, so every control opens
 * on a real choice: the provider's default model (the one flagged, else the
 * first listed); the effort that model defaults to (else "medium", else the
 * middle level); the permission the provider launches with (its declared
 * default, else "default", else the first). A toggle has no entry: off is its
 * default. A field with no usable option has none either.
 */
export function effectiveDefaults(fields: LaunchField[], launchFields: LaunchField[], models: AgentModel[], modelValue: string): Record<string, string> {
  const out: Record<string, string> = {}
  const fallbackModel = models.find(item => item.default) ?? models[0]
  const modelField = fields.find(field => field.kind === 'model')
  if (modelField && fallbackModel) out[modelField.id] = fallbackModel.id
  const model = modelValue.trim() ? models.find(item => item.id === modelValue.trim()) : fallbackModel
  for (const field of fields) {
    if (field.kind !== 'select') continue
    const options = fieldOptions(field, model).filter(option => option.value !== '')
    if (options.length === 0) continue
    const has = (value?: string) => options.find(option => option.value === value)?.value
    if (field.id === 'effort' || field.per_model) {
      out[field.id] = has(model?.default_effort) ?? has('medium') ?? options[Math.floor((options.length - 1) / 2)].value
    } else {
      out[field.id] = has(launchFields.find(item => item.id === field.id)?.default) ?? has('default') ?? options[0].value
    }
  }
  return out
}
