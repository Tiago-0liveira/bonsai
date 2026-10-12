import { useEffect, useMemo, useRef, useState } from 'react'
import { agentModels, type AgentAccount, type AgentCapability, type AgentModel } from '../../api/agents'
import { updateAgentProfile } from '../../api/agentsOverview'
import { ModelCombobox } from '../../components/ui/ModelCombobox'
import { LaunchFields, chosenModel, resolveLaunchValues, type LaunchValues } from '../workspace/LaunchFields'
import { profileChanges, profileFormValues } from './profileForm'

const LABEL = 'mb-1.5 block font-mono text-[10px] font-semibold uppercase tracking-[.1em] text-muted-2'

/**
 * The profile's launch defaults, rendered from the provider's `profile_fields`.
 * Only the fields the user changed are sent; a field set back to "Provider
 * default" is sent as null. Reports unsaved changes so the page can guard
 * tab and profile switches.
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
  const saved = account.defaults
  const savedValues = useMemo(() => profileFormValues(fields, saved), [fields, saved])
  // Only what the user touched; everything else follows the saved defaults, so a change made elsewhere shows up.
  const [edits, setEdits] = useState<LaunchValues>({})
  const values: LaunchValues = { ...savedValues, ...edits }
  const changes = profileChanges(fields, saved, values)
  const dirty = Object.keys(changes).length > 0
  const [models, setModels] = useState<AgentModel[]>([])
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
    setEdits(current => {
      const next = { ...savedValues, ...current, [id]: value }
      if (id !== modelField?.id) return { ...current, [id]: value }
      // A different model may not offer the chosen effort: fall back to the provider default.
      const resolved = resolveLaunchValues(launchFields, next, chosenModel(models, String(value)))
      const dropped = Object.fromEntries(launchFields.filter(field => resolved[field.id] !== next[field.id]).map(field => [field.id, resolved[field.id]]))
      return { ...current, ...dropped, [id]: value }
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

  return (
    <form aria-label="Profile defaults" onSubmit={event => { event.preventDefault(); void save() }} className="space-y-5 p-4">
      <p className="max-w-[64ch] text-[11px] text-muted-2">Defaults apply to new launches of this profile only. Sessions already running keep the choices they started with, and the launch dialog can still override any of them.</p>
      {modelField && (
        <label className="block max-w-[420px]">
          <span className={LABEL}>{modelField.label}</span>
          <ModelCombobox
            ariaLabel={`Default ${modelField.label.toLowerCase()}`}
            disabled={pending}
            loading={modelsLoading}
            value={String(values[modelField.id] ?? '')}
            onChange={value => change(modelField.id, value)}
            suggestions={models}
            placeholder={chosenModel(models, '')?.label ?? 'Provider default'}
          />
        </label>
      )}
      <div className="max-w-[560px] space-y-3">
        <LaunchFields fields={launchFields} values={values} model={model} providerLabel={capability?.label ?? account.provider} disabled={pending} onChange={change} />
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
