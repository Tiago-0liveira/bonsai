import { useEffect, useMemo, useRef, useState } from 'react'
import { agentModels, type AgentAccount, type AgentCapability, type AgentModel, type LaunchField } from '../../api/agents'
import { updateAgentProfile } from '../../api/agentsOverview'
import { Field } from '../../components/ui/Dialog'
import { ModelCombobox } from '../../components/ui/ModelCombobox'
import { LaunchControls } from '../workspace/LaunchControls'
import { chosenModel, fieldOptions, resolveLaunchValues, type LaunchValues } from '../workspace/LaunchFields'
import { effectiveDefaults, profileChanges, profileFormValues } from './profileForm'

/**
 * Opens every field on a real choice: a field the profile saved nothing for
 * shows the provider's default (the default model, that model's default
 * effort, the usual permission). `touched` fields are left as the user typed
 * them, so a cleared model stays empty while it is being retyped.
 */
function withDefaults(fields: LaunchField[], launchFields: LaunchField[], models: AgentModel[], values: LaunchValues, touched: string[]) {
  const modelId = fields.find(field => field.kind === 'model')?.id
  const out: LaunchValues = { ...values }
  if (modelId && !String(out[modelId] ?? '') && !touched.includes(modelId)) {
    const fallback = effectiveDefaults(fields, launchFields, models, '')[modelId]
    if (fallback) out[modelId] = fallback
  }
  // The efforts on offer, and the default among them, follow the chosen model.
  const modelValue = String(out[modelId ?? ''] ?? '')
  const defaults = effectiveDefaults(fields, launchFields, models, modelValue)
  for (const field of fields) {
    if (field.kind !== 'select' || defaults[field.id] === undefined) continue
    const value = String(out[field.id] ?? '')
    if (!value || !fieldOptions(field, chosenModel(models, modelValue)).some(option => option.value === value)) out[field.id] = defaults[field.id]
  }
  return { values: out, defaults }
}

/**
 * The profile's launch defaults (model, permissions, effort), rendered from the
 * provider's `profile_fields`. Each control opens on the provider's default; a
 * field left on it, or put back on it, is saved as null. Only the fields the
 * user changed are sent. Reports unsaved changes so the page can guard tab and
 * profile switches.
 */
export function ProfileDefaults({ account, capability, onSaved, onDirtyChange }: {
  account: AgentAccount
  capability?: AgentCapability
  onSaved: (account: AgentAccount) => void
  onDirtyChange: (dirty: boolean) => void
}) {
  const fields = useMemo(() => capability?.profile_fields ?? [], [capability])
  const modelField = fields.find(field => field.kind === 'model')
  const launchFields = useMemo(() => fields.filter(field => field.kind !== 'model'), [fields])
  const providerFields = useMemo(() => capability?.launch_fields ?? [], [capability])
  const saved = account.defaults
  const savedValues = useMemo(() => profileFormValues(fields, saved), [fields, saved])
  const [models, setModels] = useState<AgentModel[]>([])
  // Only what the user touched; everything else follows the saved defaults, so a change made elsewhere shows up.
  const [edits, setEdits] = useState<LaunchValues>({})
  const baseline = withDefaults(fields, providerFields, models, savedValues, [])
  const current = withDefaults(fields, providerFields, models, { ...savedValues, ...edits }, Object.keys(edits))
  const { values, defaults } = current
  // Against what the form opened on: a field back on its default is "unset", not a new explicit value.
  const rawChanges = profileChanges(fields, Object.fromEntries(Object.entries(baseline.values).map(([id, value]) => [id, value === true ? 'true' : value === false ? '' : String(value)])), values)
  const changes: Record<string, string | null> = {}
  for (const [id, value] of Object.entries(rawChanges)) {
    const field = fields.find(item => item.id === id)
    const alreadyUnset = (saved?.[id] ?? '') === ''
    if (field?.kind !== 'toggle' && (value === null || value === defaults[id])) {
      if (!alreadyUnset) changes[id] = null
      continue
    }
    changes[id] = value
  }
  const dirty = Object.keys(changes).length > 0
  const [modelsLoading, setModelsLoading] = useState(false)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const [savedNote, setSavedNote] = useState(false)
  const busy = useRef(false)

  useEffect(() => { onDirtyChange(dirty) }, [dirty, onDirtyChange])
  useEffect(() => () => onDirtyChange(false), [onDirtyChange])

  // Suggestions follow the profile; failures leave a plain input.
  useEffect(() => {
    setModels([])
    if (!modelField) return
    let disposed = false
    setModelsLoading(true)
    agentModels(account.provider, account.id).then(list => { if (!disposed) setModels(list) }).catch(() => {}).finally(() => { if (!disposed) setModelsLoading(false) })
    return () => { disposed = true }
  }, [account.provider, account.id, modelField])

  if (fields.length === 0) {
    return <p className="p-4 text-[12px] text-muted">{capability ? `${capability.label} profiles have no defaults to set.` : 'Profile defaults are not available for this provider.'}</p>
  }

  const model = chosenModel(models, String(values[modelField?.id ?? 'model'] ?? ''))
  const change = (id: string, value: string | boolean) => {
    setSavedNote(false)
    setError('')
    setEdits(currentEdits => {
      const next = { ...savedValues, ...currentEdits, [id]: value }
      if (id !== modelField?.id) return { ...currentEdits, [id]: value }
      // A different model may not offer the chosen effort: it falls back to that model's default.
      const resolved = resolveLaunchValues(launchFields, next, chosenModel(models, String(value)))
      const dropped = Object.fromEntries(launchFields.filter(field => resolved[field.id] !== next[field.id]).map(field => [field.id, resolved[field.id]]))
      return { ...currentEdits, ...dropped, [id]: value }
    })
  }
  const save = async () => {
    if (busy.current || !dirty) return
    busy.current = true
    setPending(true)
    setError('')
    try {
      const updated = await updateAgentProfile(account.id, { defaults: changes })
      setEdits({})
      setSavedNote(true)
      onSaved(updated)
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : String(failure))
    } finally {
      busy.current = false
      setPending(false)
    }
  }
  const reset = () => { setEdits({}); setError(''); setSavedNote(false) }
  const modelValue = String(values[modelField?.id ?? 'model'] ?? '')

  return (
    <form aria-label="Profile defaults" onSubmit={event => { event.preventDefault(); void save() }} className="space-y-5 p-4">
      <p className="max-w-[64ch] text-[11px] text-muted-2">Every new launch of this profile starts with these choices. Sessions already running keep the ones they started with. A choice left on its default follows the provider.</p>
      <div className="max-w-[560px] space-y-5">
        {modelField && (
          <Field
            label={modelField.label}
            hint={modelValue && modelValue === defaults[modelField.id] ? 'The provider\'s default model.' : undefined}
          >
            <ModelCombobox
              ariaLabel={`Default ${modelField.label.toLowerCase()}`}
              disabled={pending}
              loading={modelsLoading}
              value={modelValue}
              onChange={value => change(modelField.id, value)}
              suggestions={models}
              placeholder={chosenModel(models, '')?.label ?? 'Provider default'}
            />
          </Field>
        )}
        <LaunchControls fields={launchFields} values={values} model={model} defaults={defaults} providerLabel={capability?.label ?? account.provider} disabled={pending} onChange={change} />
      </div>
      {error && <p role="alert" className="text-[11px] text-danger">{error}</p>}
      {savedNote && !dirty && <p role="status" className="font-mono text-[10px] text-accent">Defaults saved</p>}
      <div className="flex items-center gap-2">
        <button type="submit" disabled={!dirty || pending} className="bonsai-focus btn-primary inline-flex h-8 items-center px-3 text-[12px] disabled:cursor-not-allowed disabled:opacity-40">{pending ? 'Saving…' : 'Save defaults'}</button>
        <button type="button" disabled={!dirty || pending} onClick={reset} className="bonsai-focus btn-bordered h-8 rounded-[7px] px-3 text-[12px] text-muted hover:text-text disabled:opacity-40">Reset</button>
        {dirty && <span className="font-mono text-[10px] text-warn">Unsaved changes</span>}
      </div>
    </form>
  )
}
