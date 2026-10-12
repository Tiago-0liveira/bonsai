import { useEffect, useId, useRef, useState, type FormEvent } from 'react'
import { Bot, GitBranch, Gauge, LogIn, Play } from 'lucide-react'
import { useRouter } from '@tanstack/react-router'
import { BonsaiSelect } from '../../components/ui/BonsaiSelect'
import { ChoiceChips } from '../../components/ui/ChoiceChips'
import { DialogFrame, Field, TEXT_INPUT } from '../../components/ui/Dialog'
import { Segmented } from '../../components/ui/Segmented'
import { accountAddCommand, agentUsage, byHeadroom, headroomLabel } from '../../api/agentsOverview'
import { PROVIDER_LABELS, agentAccounts, agentModels, agentProviders, type AgentAccount, type AgentCapability, type AgentModel, type AgentUsage, type LaunchField } from '../../api/agents'
import { useBonsaiStore } from '../../stores/bonsai'
import { AUTH_DOT, authState } from '../agents/agentsModel'
import { ModelCombobox } from '../../components/ui/ModelCombobox'
import { ProviderLogo } from '../../components/ui/ProviderLogo'
import type { AgentProviderId } from '../../types'
import { chosenModel, initialLaunchValues, launchRequestOptions, launchSummary, resolveLaunchValues, type LaunchValues } from './LaunchFields'
import { LaunchControls } from './LaunchControls'
import { ProfilePicker } from './ProfilePicker'

const LAST_PROVIDER_KEY = 'bonsai.startAgent.provider'
const isLaunchable = (id: string): id is AgentProviderId => Object.hasOwn(PROVIDER_LABELS, id)
const NO_FIELDS: LaunchField[] = []
const WORK_TYPES = ['Implementation', 'Debugging', 'Testing', 'Review', 'Research', 'Refactor', 'Documentation', 'Release']
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
  // The router is there in the app; the dialog renders without one in isolation.
  const router = useRouter({ warn: false }) as ReturnType<typeof useRouter> | undefined

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

  const detailTone = account ? authState(account).tone : 'none'
  const worktreeOptions = availableWorktrees.map((item) => ({
    value: item.id,
    label: item.branch,
    icon: <GitBranch size={13} className="text-muted" />,
    description: item.mergeTargetBranch ? 'merges into ' + item.mergeTargetBranch : item.path,
  }))
  const nameId = useId()
  const promptId = useId()

  return (
    <DialogFrame
      icon={Bot}
      title="Start agent"
      subtitle="Pick where it runs, how it behaves and what it should do."
      closeLabel="Close start agent"
      onClose={() => setOpen(false)}
      closeDisabled={pending}
      escapeCloses={!prompt.trim()}
      width="lg"
      onSubmit={submit}
      summary={[providerLabel, ...launchSummary(fields, values, launchModel)].filter(Boolean).join(' · ')}
      actions={
        <>
          <button type="button" disabled={pending} onClick={() => setOpen(false)} className="bonsai-focus btn-bordered h-9 rounded-[8px] px-3.5 text-[12px] text-muted hover:text-text">
            Cancel
          </button>
          <button type="submit" disabled={!canStart} title="Ctrl+Enter in the prompt" className="bonsai-focus btn-primary inline-flex h-9 items-center gap-1.5 px-4 text-[12px] disabled:cursor-not-allowed disabled:opacity-40">
            <Play size={12} aria-hidden="true" />
            {pending ? 'Starting…' : 'Start agent'}
          </button>
        </>
      }
    >
      <div className="space-y-5">
        <Field label="Worktree" hint={!targetWorktreeId ? 'Project-level launches require an explicit branch choice.' : undefined}>
          <BonsaiSelect
            ariaLabel="Worktree and branch"
            disabled={pending}
            searchable
            value={worktreeId}
            onChange={setWorktreeId}
            placeholder="Choose a worktree"
            options={worktreeOptions}
          />
        </Field>

        <Field label="Provider">
          {loading && !providers.length ? (
            <div className="skeleton h-[46px]" />
          ) : (
            <Segmented
              ariaLabel="Provider"
              disabled={pending}
              value={providerId}
              onChange={selectProvider}
              itemClassName="h-[34px] text-[12px]"
              options={providers.map((item) => {
                const profiles = accounts.filter(candidate => candidate.provider === item.id).length
                const connected = isLaunchable(item.id) && item.available && profiles > 0
                const reason = !item.available ? item.unavailable_reason?.message || 'Not available' : profiles ? `Launch with your saved ${item.label} profile.` : `No ${item.label} profile yet. Run ${isLaunchable(item.id) ? setupCommand(item.id) : 'its setup command'}.`
                return {
                  value: item.id,
                  disabled: !connected,
                  title: reason,
                  label: (
                    <>
                      <ProviderLogo provider={item.id} size={14} className="shrink-0 text-text" />
                      <span className="truncate">{item.label}</span>
                      {connected ? (
                        <span className="flex items-center gap-1 font-mono text-[10px] text-accent"><span className="h-1.5 w-1.5 rounded-full bg-accent-solid" />connected</span>
                      ) : (
                        <span className="font-mono text-[10px] text-muted-2">{item.available && isLaunchable(item.id) ? 'no profile' : 'not connected'}</span>
                      )}
                      <span className="sr-only">{reason}</span>
                    </>
                  ),
                }
              })}
            />
          )}
        </Field>

        <div className="dialog-cols">
          <div className="space-y-5">
            <Field label="Profile" hint={!loading && providerAccounts.length > 1 && !account ? 'Choose a profile to continue.' : undefined}>
              <ProfilePicker accounts={providerAccounts} usage={usage} value={accountId} onChange={selectAccount} loading={loading} disabled={pending} />
              {headline?.low && (
                <span role="note" className="mt-2 flex items-center gap-1.5 font-mono text-[10px] text-warn">
                  <Gauge size={11} className="shrink-0" />Low headroom: {headroomLabel(headline)} left.{roomier && ` ${roomier.name} has more.`}
                </span>
              )}
              {!loading && !error && selected?.available && launchable && !providerAccounts.length && <p className="mt-2 text-[11px] text-muted">Set up a profile with <code className="font-mono">{setupCommand(launchable)}</code></p>}
              {selected && !selected.available && <p className="mt-2 text-[11px] text-muted">{selected.unavailable_reason?.message}</p>}
            </Field>
            {account && (signedIn.length > 0 || profileDefaults.length > 0 || account.warnings?.length) && (
              <div className="dialog-enter space-y-1.5 rounded-[10px] border border-border-subtle bg-well/50 px-3 py-2.5 font-mono text-[10px] text-muted-2" data-testid="profile-details">
                {signedIn.length > 0 && <div className="flex items-center gap-2"><span aria-hidden="true" className={'h-1.5 w-1.5 shrink-0 rounded-full ' + AUTH_DOT[detailTone]} />{signedIn.join(' · ')}</div>}
                {profileDefaults.length > 0 && <div>{'profile defaults: ' + profileDefaults.join(' · ')}</div>}
                {account.warnings?.map(warning => <div key={warning} className="text-warn">{warning}</div>)}
                {router && detailTone === 'bad' && (
                  <button type="button" disabled={pending} onClick={() => { setOpen(false); void router.navigate({ to: '/agents', search: { profile: account.id, signin: true } }) }} className="bonsai-focus btn-bordered mt-1 inline-flex h-7 items-center gap-1.5 rounded-[7px] px-2.5 font-sans text-[11px] text-text">
                    <LogIn size={11} /> Sign in again
                  </button>
                )}
              </div>
            )}
          </div>
          <LaunchControls
            fields={fields}
            values={values}
            model={launchModel}
            account={account}
            providerLabel={providerLabel}
            disabled={pending}
            onChange={(id, value) => setLaunchValues(current => ({ ...current, [id]: value }))}
          />
        </div>

        <div className="space-y-5 border-t border-border-subtle pt-5">
          <div className="dialog-cols">
            <Field label="Agent name" htmlFor={nameId}>
              <input
                id={nameId}
                maxLength={128}
                disabled={pending}
                value={name}
                onChange={(event) => setName(event.target.value)}
                placeholder="e.g. Login refactor"
                className={TEXT_INPUT}
              />
            </Field>
            <Field label="Model" hint={!model.trim() ? 'Empty uses the profile default.' : undefined}>
              <ModelCombobox ariaLabel="Agent model" disabled={pending} loading={modelsLoading} value={model} onChange={setModel} suggestions={models} placeholder={chosenModel(models, '')?.label ?? 'Use profile default'} />
            </Field>
          </div>
          <Field label="Work type">
            <ChoiceChips ariaLabel="Agent work type" disabled={pending} value={workType} onChange={setWorkType} options={WORK_TYPES.map(item => ({ value: item, label: item }))} />
          </Field>
          <Field label="Prompt" htmlFor={promptId}>
            <textarea
              id={promptId}
              disabled={pending}
              value={prompt}
              onChange={(event) => setPrompt(event.target.value)}
              onKeyDown={(event) => {
                if ((event.ctrlKey || event.metaKey) && event.key === 'Enter') { event.preventDefault(); event.currentTarget.form?.requestSubmit() }
              }}
              placeholder="Describe the exact work this agent should execute…"
              rows={5}
              className="bonsai-focus w-full resize-y rounded-[8px] border border-border bg-well px-3 py-2.5 text-[12.5px] leading-5 outline-none transition-colors placeholder:text-muted-2 hover:border-border-strong focus:border-accent/55 disabled:opacity-50"
            />
          </Field>
        </div>
        {error && <p role="alert" className="text-[11px] text-danger">{error}</p>}
      </div>
    </DialogFrame>
  )
}
