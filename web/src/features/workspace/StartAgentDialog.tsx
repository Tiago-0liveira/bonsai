import { useEffect, useRef, useState, type FormEvent } from 'react'
import { Bot, Gauge, X } from 'lucide-react'
import { BonsaiSelect } from '../../components/ui/BonsaiSelect'
import { accountAddCommand, agentUsage, byHeadroom, headroomLabel } from '../../api/agentsOverview'
import { PROVIDER_LABELS, agentAccounts, agentModels, agentProviders, type AgentAccount, type AgentCapability, type AgentModel, type AgentUsage, type LaunchField } from '../../api/agents'
import { useBonsaiStore } from '../../stores/bonsai'
import { ModelCombobox } from '../../components/ui/ModelCombobox'
import { ProviderLogo } from '../../components/ui/ProviderLogo'
import type { AgentProviderId } from '../../types'
import { LaunchFields, chosenModel, initialLaunchValues, launchRequestOptions, launchSummary, resolveLaunchValues, type LaunchValues } from './LaunchFields'

const LAST_PROVIDER_KEY = 'bonsai.startAgent.provider'
const isLaunchable = (id: string): id is AgentProviderId => Object.hasOwn(PROVIDER_LABELS, id)
const NO_FIELDS: LaunchField[] = []
const setupCommand = accountAddCommand
const fieldsOf = (providers: AgentCapability[], id: string) => providers.find(item => item.id === id)?.launch_fields ?? NO_FIELDS

function lastProvider(): string {
  try { return localStorage.getItem(LAST_PROVIDER_KEY) ?? '' } catch { return '' }
}
function rememberProvider(id: string) {
  try { localStorage.setItem(LAST_PROVIDER_KEY, id) } catch { /* per-viewer convenience only */ }
}
/** The only provider with profiles wins, then the last used one, then the first usable one. */
function defaultProvider(providers: AgentCapability[], accounts: AgentAccount[]): string {
  const usable = providers.filter(item => isLaunchable(item.id) && item.available && accounts.some(account => account.provider === item.id))
  if (usable.length === 1) return usable[0].id
  const last = lastProvider()
  return usable.find(item => item.id === last)?.id ?? usable[0]?.id ?? providers.find(item => isLaunchable(item.id))?.id ?? ''
}

export function StartAgentDialog() {
  const open = useBonsaiStore(state => state.startAgentDialogOpen)
  return open ? <StartAgentForm /> : null
}

function StartAgentForm() {
  const setOpen = useBonsaiStore((state) => state.setStartAgentDialogOpen)
  const targetWorktreeId = useBonsaiStore((state) => state.startAgentTargetWorktreeId)
  const activeProjectId = useBonsaiStore((state) => state.activeProjectId)
  const worktrees = useBonsaiStore((state) => state.worktrees)
  const createAgent = useBonsaiStore((state) => state.createAgent)

  const availableWorktrees = worktrees.filter(item => item.projectId === activeProjectId && !item.missing)
  const [worktreeId, setWorktreeId] = useState(targetWorktreeId)
  const [accounts, setAccounts] = useState<AgentAccount[]>([])
  const [providers, setProviders] = useState<AgentCapability[]>([])
  const [usage, setUsage] = useState<Record<string, AgentUsage>>({})
  const [accountId, setAccountId] = useState('')
  const [providerId, setProviderId] = useState('')
  const [model, setModel] = useState('')
  const [models, setModels] = useState<AgentModel[]>([])
  const [modelsLoading, setModelsLoading] = useState(false)
  const [launchValues, setLaunchValues] = useState<LaunchValues>({})
  const [name, setName] = useState('')
  const [workType, setWorkType] = useState('Implementation')
  const [prompt, setPrompt] = useState('')
  const [loading, setLoading] = useState(true)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const busy = useRef(false)
  const request = useRef({ fingerprint: '', key: '' })
  useEffect(() => {
    let disposed = false
    Promise.all([agentAccounts(), agentProviders()]).then(([accounts, providers]) => {
      if (disposed) return
      setAccounts(accounts)
      setProviders(providers)
      // The Agents tab opens the dialog on one profile; it wins over the remembered provider.
      const wanted = useBonsaiStore.getState().startAgentTargetAccountId
      const target = wanted ? accounts.find(account => account.id === wanted && providers.some(item => item.id === account.provider)) : undefined
      const initial = target?.provider ?? defaultProvider(providers, accounts)
      setProviderId(initial)
      const own = accounts.filter(account => account.provider === initial)
      const chosen = target ?? (own.length === 1 ? own[0] : undefined)
      if (chosen) setAccountId(chosen.id)
      setLaunchValues(initialLaunchValues(fieldsOf(providers, initial), chosen))
    }).catch(error => { if (!disposed) setError(String(error.message)) })
      .finally(() => { if (!disposed) setLoading(false) })
    return () => { disposed = true }
  }, [])
  // Cached usage only: opening the dialog never asks a provider. Without it, profiles keep their order.
  useEffect(() => {
    let disposed = false
    agentUsage().then(list => { if (!disposed) setUsage(Object.fromEntries(list.map(entry => [entry.account_id, entry]))) }).catch(() => {})
    return () => { disposed = true }
  }, [])
  // Suggestions follow the provider and profile; failures just leave a plain input.
  const modelProvider = isLaunchable(providerId) ? providerId : ''
  useEffect(() => {
    setModels([])
    if (!modelProvider || !accountId) return
    let disposed = false
    setModelsLoading(true)
    agentModels(modelProvider, accountId).then(list => { if (!disposed) setModels(list) }).catch(() => {}).finally(() => { if (!disposed) setModelsLoading(false) })
    return () => { disposed = true }
  }, [modelProvider, accountId])
  const selected = providers.find(item => item.id === providerId)
  const launchable = isLaunchable(providerId) ? providerId : undefined
  const providerLabel = selected?.label ?? ''
  const providerAccounts = byHeadroom(accounts.filter(account => account.provider === providerId), usage)
  const account = providerAccounts.find(item => item.id === accountId)
  const headline = account && usage[account.id]?.headline
  // The roomiest profile that is not low itself, to point at when the chosen one runs low.
  const roomier = headline?.low ? providerAccounts.find(item => item.id !== accountId && usage[item.id]?.headline?.score !== undefined && !usage[item.id]?.headline?.low) : undefined
  const fields = fieldsOf(providers, providerId)
  const launchModel = chosenModel(models, model)
  const values = resolveLaunchValues(fields, launchValues, launchModel)
  const options = launchRequestOptions(fields, values)
  const profileDefaults = [account?.model, ...launchSummary(fields.filter(field => field.kind === 'select'), account ?? {})].filter(Boolean)
  const signedIn = [account?.auth_mode && `auth: ${account.auth_mode}`, account?.identity, account?.auth_status?.replace(/_/g, ' ')].filter(Boolean)
  const selectProvider = (id: string) => {
    if (id === providerId) return
    const own = accounts.filter(item => item.provider === id)
    setProviderId(id)
    setError('')
    rememberProvider(id)
    setAccountId(own.length === 1 ? own[0].id : '')
    setModel('')
    setLaunchValues(initialLaunchValues(fieldsOf(providers, id), own.length === 1 ? own[0] : undefined))
  }
  const selectAccount = (id: string) => {
    setAccountId(id)
    // Toggles start from the profile's own setting; selects keep the user's choice.
    setLaunchValues(current => ({ ...current, ...initialLaunchValues(fields.filter(field => field.kind === 'toggle'), accounts.find(item => item.id === id)) }))
  }
  const canStart = !loading && !pending && launchable && selected?.available && account && availableWorktrees.some(item => item.id === worktreeId)
  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (busy.current || !canStart || !launchable) return
    busy.current = true
    setPending(true)
    setError('')
    const fingerprint = JSON.stringify({ providerId, worktreeId, accountId, name, model, workType, prompt, options })
    if (request.current.fingerprint !== fingerprint) request.current = { fingerprint, key: crypto.randomUUID() }
    try {
      await createAgent({ worktreeId, accountId, requestKey: request.current.key, name: name.trim(), provider: PROVIDER_LABELS[launchable], model: model.trim(), reasoningEffort: '', fastMode: false, workType, prompt: prompt.trim(), options })
    } catch (error) {
      setError(error instanceof Error ? error.message : String(error))
    } finally {
      busy.current = false
      setPending(false)
    }
  }

  return (
    <div className="absolute inset-0 z-[85] grid place-items-center bg-well/70 p-6 backdrop-blur-[2px]">
      <form
        onSubmit={submit}
        role="dialog" aria-modal="true" aria-labelledby="start-agent-title"
        className="w-full max-w-[720px] overflow-hidden rounded-[14px] border border-border-strong bg-panel shadow-overlay"
      >
        <div className="flex h-11 items-center border-b border-border-subtle px-4">
          <Bot size={15} className="mr-2 text-accent" />
          <div>
            <div id="start-agent-title" className="text-[13px] font-semibold">Start agent</div>
            <div className="font-mono text-[10px] text-muted-2">Choose the worktree, profile and task.</div>
          </div>
          <button
            type="button"
            disabled={pending}
            onClick={() => setOpen(false)}
            aria-label="Close start agent"
            className="bonsai-focus ml-auto grid h-7 w-7 place-items-center rounded-md text-muted hover:bg-panel-3 hover:text-text"
          >
            <X size={14} />
          </button>
        </div>

        <div className="max-h-[72vh] space-y-5 overflow-auto p-4">
          <label className="block">
            <span className="mb-1.5 block font-mono text-[10px] font-semibold uppercase tracking-[.1em] text-muted-2">Worktree</span>
            <BonsaiSelect
              ariaLabel="Worktree and branch"
              disabled={pending}
              searchable
              value={worktreeId}
              onChange={setWorktreeId}
              placeholder="Choose a worktree"
              options={availableWorktrees.map((item) => ({
                value: item.id,
                label: item.branch,
                description: item.mergeTargetBranch ? 'merges into ' + item.mergeTargetBranch : item.path,
              }))}
            />
            {!targetWorktreeId && (
              <span className="mt-1.5 block font-mono text-[10px] text-muted-2">
                Project-level launches require an explicit branch choice.
              </span>
            )}
          </label>

          <section>
            <div className="mb-2 font-mono text-[10px] font-semibold uppercase tracking-[.1em] text-muted-2">Provider</div>
            <div className="grid gap-1 rounded-[9px] border border-border bg-well p-1" style={{ gridTemplateColumns: `repeat(${Math.max(providers.length, 1)}, minmax(0, 1fr))` }}>
              {providers.map((item) => {
                const profiles = accounts.filter(candidate => candidate.provider === item.id).length
                const connected = isLaunchable(item.id) && item.available && profiles > 0
                const active = item.id === providerId
                const reason = !item.available ? item.unavailable_reason?.message || 'Not available' : profiles ? `Launch with your saved ${item.label} profile.` : `No ${item.label} profile yet. Run ${isLaunchable(item.id) ? setupCommand(item.id) : 'its setup command'}.`
                return (
                  <button
                    type="button"
                    key={item.id}
                    disabled={pending || !connected}
                    aria-pressed={active}
                    title={reason}
                    onClick={() => selectProvider(item.id)}
                    className={'bonsai-focus flex h-[30px] items-center justify-center gap-2 rounded-[7px] text-[12px] transition-colors disabled:opacity-40 ' + (active ? 'bg-panel-3 text-text' : 'text-muted hover:text-text')}
                  >
                    <ProviderLogo provider={item.id} size={13} className="shrink-0 text-text" />
                    <span>{item.label}</span>
                    {connected ? (
                      <span className="flex items-center gap-1 font-mono text-[10px] text-accent"><span className="h-1.5 w-1.5 rounded-full bg-accent-solid" />connected</span>
                    ) : (
                      <span className="font-mono text-[10px] text-muted-2">{item.available && isLaunchable(item.id) ? 'no profile' : 'not connected'}</span>
                    )}
                    <span className="sr-only">{reason}</span>
                  </button>
                )
              })}
            </div>
          </section>

          <div className="grid grid-cols-2 gap-3">
            <label className="block">
              <span className="mb-1.5 block font-mono text-[10px] font-semibold uppercase tracking-[.1em] text-muted-2">Profile</span>
              <BonsaiSelect ariaLabel="Profile" disabled={pending || loading} value={accountId} placeholder={loading ? 'Loading profiles…' : 'Choose a profile'} onChange={selectAccount} options={providerAccounts.map(item => ({ value: item.id, label: item.name, icon: <ProviderLogo provider={item.provider} size={14} className="text-text" />, description: [item.auth_mode, item.identity].filter(Boolean).join(' · ') || undefined, meta: headroomLabel(usage[item.id]?.headline) || undefined }))} />
              {headline?.low && (
                <span role="note" className="mt-1.5 flex items-center gap-1.5 font-mono text-[10px] text-warn">
                  <Gauge size={11} className="shrink-0" />Low headroom: {headroomLabel(headline)} left.{roomier && ` ${roomier.name} has more.`}
                </span>
              )}
            </label>
            <label className="block">
              <span className="mb-1.5 block font-mono text-[10px] font-semibold uppercase tracking-[.1em] text-muted-2">Model</span>
              <ModelCombobox ariaLabel="Agent model" disabled={pending} loading={modelsLoading} value={model} onChange={setModel} suggestions={models} placeholder={chosenModel(models, '')?.label ?? 'Use profile default'} />
            </label>
          </div>
          {account && (signedIn.length > 0 || profileDefaults.length > 0 || account.warnings?.length) && (
            <div className="space-y-1 rounded-[10px] border border-border bg-well p-3 font-mono text-[10px] text-muted-2" data-testid="profile-details">
              {signedIn.length > 0 && <div>{signedIn.join(' · ')}</div>}
              {profileDefaults.length > 0 && <div>{'profile defaults: ' + profileDefaults.join(' · ')}</div>}
              {account.warnings?.map(warning => <div key={warning} className="text-warn">{warning}</div>)}
            </div>
          )}
          <LaunchFields fields={fields} values={values} model={launchModel} providerLabel={providerLabel} disabled={pending} onChange={(id, value) => setLaunchValues(current => ({ ...current, [id]: value }))} />
          <div className="grid grid-cols-2 gap-3">
            <label className="block">
              <span className="mb-1.5 block font-mono text-[10px] font-semibold uppercase tracking-[.1em] text-muted-2">Agent name</span>
              <input
                aria-label="Agent name"
                maxLength={128}
                disabled={pending}
                value={name}
                onChange={(event) => setName(event.target.value)}
                placeholder="e.g. Login refactor"
                className="bonsai-focus h-8 w-full rounded-[7px] border border-border bg-well px-2.5 text-[12px] outline-none placeholder:text-muted-2 focus:border-accent/55"
              />
            </label>
            <label className="block">
              <span className="mb-1.5 block font-mono text-[10px] font-semibold uppercase tracking-[.1em] text-muted-2">Work type</span>
              <BonsaiSelect
                ariaLabel="Agent work type"
                disabled={pending}
                value={workType}
                onChange={setWorkType}
                options={['Implementation', 'Debugging', 'Testing', 'Review', 'Research', 'Refactor', 'Documentation', 'Release'].map((item) => ({
                  value: item,
                  label: item,
                }))}
              />
            </label>
          </div>

          <label className="block">
            <span className="mb-1.5 flex items-center gap-1.5 font-mono text-[10px] font-semibold uppercase tracking-[.1em] text-muted-2">
              <Gauge size={10} /> Prompt
            </span>
            <textarea
              aria-label="Prompt"
              disabled={pending}
              value={prompt}
              onChange={(event) => setPrompt(event.target.value)}
              placeholder="Describe the exact work this agent should execute…"
              rows={6}
              className="bonsai-focus w-full resize-y rounded-[7px] border border-border bg-well px-3 py-2.5 text-[12px] leading-5 outline-none placeholder:text-muted-2 focus:border-accent/55"
            />
          </label>
          {!loading && !error && selected?.available && launchable && !providerAccounts.length && <p className="text-[10px] text-muted">Set up a profile with <code>{setupCommand(launchable)}</code></p>}
          {selected && !selected.available && <p className="text-[10px] text-muted">{selected.unavailable_reason?.message}</p>}
          {error && <p role="alert" className="text-[11px] text-danger">{error}</p>}
        </div>

        <div className="flex items-center justify-between border-t border-border-subtle bg-well/45 px-4 py-3">
          <div className="font-mono text-[10px] text-muted-2">
            {[providerLabel, ...launchSummary(fields, values, launchModel)].filter(Boolean).join(' · ')}
          </div>
          <div className="flex gap-2">
            <button type="button" disabled={pending} onClick={() => setOpen(false)} className="bonsai-focus btn-bordered h-8 rounded-[7px] px-3 text-[12px] text-muted hover:text-text">
              Cancel
            </button>
            <button
              type="submit"
              disabled={!canStart}
              className="bonsai-focus btn-primary h-8 px-3 text-[12px] disabled:cursor-not-allowed disabled:opacity-40"
            >
              {pending ? 'Starting…' : 'Start agent'}
            </button>
          </div>
        </div>
      </form>
    </div>
  )
}
