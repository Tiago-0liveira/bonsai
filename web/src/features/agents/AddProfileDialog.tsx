import { CheckCircle2, KeyRound, Loader2, XCircle } from 'lucide-react'
import { useCallback, useEffect, useRef, useState, type FormEvent, type ReactNode } from 'react'
import type { AgentAccount, AgentCapability } from '../../api/agents'
import { answerSetupSecret, cancelSetupJob, isSetupJobActive, setupJob, startProfileLogin, startProfileSetup, type ProfileSetupBody, type SetupJob } from '../../api/agentSetup'
import { AgentProfileError } from '../../api/agentsOverview'
import { ProviderLogo } from '../../components/ui/ProviderLogo'
import { authState } from './agentsModel'
import { CopyCommand } from './CopyCommand'
import { ProfileDialog } from './ProfileDialog'
import { SetupTerminal } from './SetupTerminal'
import {
  JOB_STATE_LABEL, accountAddCommandFor, accountLoginCommand, authModeOf, defaultAuthMode, forgetSetupJob,
  profileNameError, rememberSetupJob, secretError, setupFingerprint,
} from './setupModel'

/** What the dialog does: create a profile, sign one in again, or reattach to a running sign-in. */
export type SetupTarget =
  | { kind: 'create'; provider?: string }
  | { kind: 'login'; account: AgentAccount }
  | { kind: 'resume'; jobId: string }

type Step = 'provider' | 'details' | 'signin' | 'done' | 'cli'

const LABEL = 'mb-1.5 block font-mono text-[10px] font-semibold uppercase tracking-[.1em] text-muted-2'
const INPUT = 'bonsai-focus h-8 w-full rounded-[7px] border border-border bg-well px-2.5 text-[12px] outline-none placeholder:text-muted-2 focus:border-accent/55 disabled:opacity-50'
const SECONDARY = 'bonsai-focus btn-bordered h-8 rounded-[7px] px-3 text-[12px] text-muted hover:text-text disabled:opacity-40'
const PRIMARY = 'bonsai-focus btn-primary inline-flex h-8 items-center gap-2 px-3 text-[12px] disabled:cursor-not-allowed disabled:opacity-40'
const messageOf = (failure: unknown) => failure instanceof Error ? failure.message : String(failure)

export interface AddProfileDialogProps {
  target: SetupTarget
  providers: AgentCapability[]
  accounts: AgentAccount[]
  onClose: () => void
  /** A sign-in succeeded: the created or signed-in profile, as described by the API. */
  onSignedIn: (account: AgentAccount) => void
  onStartAgent: (accountId: string) => void
  onSetDefaults: (accountId: string) => void
}

/**
 * Creates a profile or signs one in again from the browser: provider, details,
 * the provider's own login in a terminal, done. The job keeps running on the
 * API when the page goes away; its ID stays in sessionStorage so the page can
 * reattach. Pasted secrets live only in a field until they are sent.
 */
export function AddProfileDialog({ target, providers, accounts, onClose, onSignedIn, onStartAgent, onSetDefaults }: AddProfileDialogProps) {
  const login = target.kind === 'login' ? target.account : undefined
  const [step, setStep] = useState<Step>(target.kind === 'resume' ? 'signin' : target.kind === 'create' && !target.provider ? 'provider' : 'details')
  const [providerId, setProviderId] = useState(target.kind === 'create' ? target.provider ?? '' : login?.provider ?? '')
  const capability = providers.find(item => item.id === providerId)
  const spec = capability?.setup
  const [name, setName] = useState('')
  const [nameTouched, setNameTouched] = useState(false)
  const [authMode, setAuthMode] = useState(() => defaultAuthMode(capability?.setup, login?.auth_mode))
  const [secret, setSecret] = useState('')
  const [seed, setSeed] = useState(true)
  const [job, setJob] = useState<SetupJob>()
  const [account, setAccount] = useState<AgentAccount>()
  const [error, setError] = useState('')
  const [pending, setPending] = useState(false)
  const [confirm, setConfirm] = useState<'' | 'cancel' | 'close'>('')
  const [answer, setAnswer] = useState('')
  const [answerError, setAnswerError] = useState('')
  const busy = useRef(false)
  const request = useRef({ fingerprint: '', key: '' })
  const finished = useRef('')
  const alive = useRef(true)

  const mode = authModeOf(spec, authMode)
  const providerLabel = capability?.label ?? job?.provider ?? providerId
  const profileName = login?.name ?? job?.name ?? name
  const nameProblem = target.kind === 'create' ? profileNameError(name, providerId, accounts) : ''
  const secretProblem = secretError(mode, secret)

  const updateJob = useCallback((next: SetupJob) => setJob(current => current && current.id !== next.id ? current : { ...current, ...next }), [])

  // Reattach: the job is read once; a job the API no longer has is forgotten.
  useEffect(() => {
    if (target.kind !== 'resume') return
    let disposed = false
    setupJob(target.jobId).then(next => { if (!disposed) { setJob(next); setProviderId(next.provider) } }).catch(failure => {
      if (disposed) return
      if (failure instanceof AgentProfileError && failure.status === 404) forgetSetupJob(target.jobId)
      setError(failure instanceof AgentProfileError && failure.status === 404 ? 'This sign-in has ended and is no longer available.' : messageOf(failure))
    })
    return () => { disposed = true }
  }, [target])

  useEffect(() => {
    alive.current = true
    return () => { alive.current = false }
  }, [])

  // An ended job: a new attempt needs a new request key, and a success needs the described profile.
  useEffect(() => {
    if (!job || isSetupJobActive(job.state) || finished.current === job.id) return
    finished.current = job.id
    request.current = { fingerprint: '', key: '' }
    forgetSetupJob(job.id)
    if (job.state !== 'succeeded') return
    const settle = (described?: AgentAccount) => {
      if (!alive.current) return
      const found = described ?? accounts.find(item => item.id === job.account_id)
      setAccount(found)
      if (found) onSignedIn(found)
      setStep('done')
    }
    // Only an HTTP read carries the described profile; terminal status frames do not.
    if (job.account) settle(job.account)
    else setupJob(job.id).then(next => { if (alive.current) updateJob(next); settle(next.account) }).catch(() => settle())
  }, [job, accounts, onSignedIn, updateJob])

  const chooseProvider = (id: string) => {
    setProviderId(id)
    setAuthMode(defaultAuthMode(providers.find(item => item.id === id)?.setup))
    setSecret('')
    setError('')
    setStep('details')
  }

  const start = async (event: FormEvent) => {
    event.preventDefault()
    setNameTouched(true)
    if (busy.current || nameProblem || secretProblem) return
    busy.current = true
    setPending(true)
    setError('')
    const pasted = secret.trim() || undefined
    // The field is emptied as the request leaves; a retry needs the secret pasted again.
    setSecret('')
    try {
      let started: SetupJob
      if (login) {
        const body = { auth_mode: authMode && authMode !== login.auth_mode ? authMode : undefined }
        const fingerprint = setupFingerprint(`login:${login.id}`, body)
        if (request.current.fingerprint !== fingerprint) request.current = { fingerprint, key: crypto.randomUUID() }
        started = await startProfileLogin(login.id, { ...body, secret: pasted }, request.current.key)
      } else {
        const body: ProfileSetupBody = { provider: providerId, name, auth_mode: authMode || undefined, seed: spec?.seed ? seed : undefined }
        const fingerprint = setupFingerprint('create', body)
        if (request.current.fingerprint !== fingerprint) request.current = { fingerprint, key: crypto.randomUUID() }
        started = await startProfileSetup({ ...body, secret: pasted }, request.current.key)
      }
      finished.current = ''
      setJob(started)
      rememberSetupJob({ id: started.id, kind: login ? 'login' : 'create', accountId: login?.id })
      setStep('signin')
    } catch (failure) {
      if (failure instanceof AgentProfileError && failure.code === 'terminal_unsupported') setStep('cli')
      else setError(messageOf(failure))
    } finally {
      busy.current = false
      setPending(false)
    }
  }

  const sendAnswer = async (event: FormEvent) => {
    event.preventDefault()
    if (!job || busy.current || !answer) return
    busy.current = true
    setPending(true)
    setAnswerError('')
    const value = answer
    setAnswer('')
    try {
      await answerSetupSecret(job.id, value)
      setJob(current => current && current.state === 'waiting_secret' ? { ...current, state: 'running', prompt: undefined } : current)
    } catch (failure) {
      setAnswerError(messageOf(failure))
    } finally {
      busy.current = false
      setPending(false)
    }
  }

  const cancel = async (then: 'stay' | 'close') => {
    if (!job || busy.current) return
    busy.current = true
    setPending(true)
    setError('')
    try {
      updateJob(await cancelSetupJob(job.id))
      setConfirm('')
      if (then === 'close') onClose()
    } catch (failure) {
      setError(messageOf(failure))
    } finally {
      busy.current = false
      setPending(false)
    }
  }

  const running = job !== undefined && isSetupJobActive(job.state)
  const requestClose = () => {
    if (pending) return
    if (confirm) { setConfirm(''); return }
    if (running) { setConfirm('close'); return }
    onClose()
  }
  const title = login || job?.kind === 'login' ? `Sign in to ${profileName} again` : 'Add profile'

  return (
    <ProfileDialog label={title} wide={step === 'signin'} locked={pending} onClose={requestClose}>
      <header className="flex items-center gap-2">
        {providerId && <ProviderLogo provider={providerId} size={16} className="shrink-0 text-text" />}
        <h2 className="text-[13px] font-semibold">{title}</h2>
        {step !== 'provider' && providerLabel && <span className="font-mono text-[10px] text-muted-2">{providerLabel}{profileName && step !== 'details' ? ` · ${profileName}` : ''}</span>}
      </header>

      {step === 'provider' && (
        <>
          <p className="text-xs text-muted">Choose the agent provider this profile signs in to.</p>
          <ul aria-label="Providers" className="grid gap-2 sm:grid-cols-3">
            {providers.map(provider => {
              const count = accounts.filter(item => item.provider === provider.id).length
              return (
                <li key={provider.id}>
                  <button
                    type="button"
                    disabled={!provider.available}
                    onClick={() => chooseProvider(provider.id)}
                    className="bonsai-focus flex h-full w-full flex-col items-start gap-1 rounded-[10px] border border-border bg-well p-3 text-left transition-colors hover:border-accent/55 disabled:cursor-not-allowed disabled:opacity-60 disabled:hover:border-border"
                  >
                    <span className="flex items-center gap-2 text-[12px] font-semibold text-text"><ProviderLogo provider={provider.id} size={14} />{provider.label}</span>
                    <span className="font-mono text-[10px] text-muted-2">
                      {provider.available ? `${provider.version ? `v${provider.version} · ` : ''}${count} ${count === 1 ? 'profile' : 'profiles'}` : provider.unavailable_reason?.message || 'Not available'}
                    </span>
                    {!provider.available && provider.install_hint && <code className="select-all break-all font-mono text-[10px] text-muted-2">{provider.install_hint}</code>}
                  </button>
                </li>
              )
            })}
          </ul>
          <Footer><button type="button" onClick={onClose} className={SECONDARY}>Cancel</button></Footer>
        </>
      )}

      {step === 'details' && (
        <form aria-label="Sign-in details" noValidate onSubmit={event => void start(event)} className="space-y-4">
          {!login && (
            <label className="block">
              <span className={LABEL}>Profile name</span>
              <input
                autoFocus
                aria-label="Profile name"
                aria-invalid={nameTouched && nameProblem ? true : undefined}
                maxLength={64}
                spellCheck={false}
                disabled={pending}
                value={name}
                onChange={event => { setName(event.target.value); setNameTouched(true); setError('') }}
                placeholder="e.g. work"
                className={INPUT}
              />
              {nameTouched && nameProblem && <span role="alert" className="mt-1 block text-[11px] text-danger">{nameProblem}</span>}
            </label>
          )}
          {(spec?.auth_modes?.length ?? 0) > 0 && (
            <fieldset disabled={pending} className="space-y-1.5">
              <legend className={LABEL}>Sign-in method</legend>
              {spec!.auth_modes!.map(option => (
                <label key={option.value} className={'flex cursor-pointer items-start gap-2.5 rounded-[10px] border p-2.5 ' + (option.value === authMode ? 'border-accent/55 bg-accent/[.06]' : 'border-border bg-well')}>
                  <input type="radio" name="auth_mode" value={option.value} checked={option.value === authMode} onChange={() => { setAuthMode(option.value); setSecret('') }} className="mt-0.5 accent-[rgb(var(--accent-solid))]" />
                  <span className="min-w-0">
                    <span className="flex items-center gap-2 text-[12px] font-medium text-text">
                      {option.label}
                      {option.recommended && <span className="chip border border-accent/35 bg-accent/[.12] text-accent">Recommended</span>}
                      {login?.auth_mode === option.value && <span className="font-mono text-[10px] text-muted-2">current</span>}
                    </span>
                    {option.help && <span className="block text-[11px] text-muted">{option.help}</span>}
                  </span>
                </label>
              ))}
            </fieldset>
          )}
          {mode?.secret && (
            <label className="block">
              <span className={LABEL}>{mode.secret.label}</span>
              <input
                type="password"
                aria-label={mode.secret.label}
                autoComplete="off"
                spellCheck={false}
                disabled={pending}
                value={secret}
                onChange={event => setSecret(event.target.value)}
                placeholder={mode.secret.placeholder}
                className={INPUT + ' font-mono'}
              />
              <span className="mt-1 block text-[11px] text-muted-2">
                {mode.secret.optional ? 'Leave empty to generate one in the terminal.' : 'Sent once to Bonsai on this computer; never shown again or stored outside the profile.'}
              </span>
              {nameTouched && secretProblem && <span role="alert" className="mt-1 block text-[11px] text-danger">{secretProblem}</span>}
            </label>
          )}
          {!login && spec?.seed && (
            <label className="flex cursor-pointer items-start gap-2.5">
              <input type="checkbox" checked={seed} disabled={pending} onChange={event => setSeed(event.target.checked)} className="mt-0.5 accent-[rgb(var(--accent-solid))]" />
              <span className="text-[12px] text-text">
                Copy settings from <code className="font-mono text-[11px]">{spec.seed.source}</code>
                <span className="block text-[11px] text-muted">Your {providerLabel} configuration, not its sign-in.</span>
              </span>
            </label>
          )}
          {spec?.notes && spec.notes.length > 0 && (
            <ul aria-label="Provider notes" className="space-y-1 rounded-[10px] border border-border bg-well p-3">
              {spec.notes.map(note => <li key={note} className="text-[11px] text-muted">{note}</li>)}
            </ul>
          )}
          {error && <p role="alert" className="text-xs text-danger">{error}</p>}
          <Footer>
            {target.kind === 'create' && !target.provider
              ? <button type="button" disabled={pending} onClick={() => { setStep('provider'); setError('') }} className={SECONDARY}>Back</button>
              : <button type="button" disabled={pending} onClick={onClose} className={SECONDARY}>Cancel</button>}
            <button type="submit" disabled={pending || !capability?.available} className={PRIMARY}>{pending ? 'Starting…' : 'Sign in'}</button>
          </Footer>
        </form>
      )}

      {step === 'signin' && (
        <div className="space-y-3">
          <JobStatus job={job} />
          {/* An ended sign-in keeps its output on screen: a failure explains itself there. */}
          {job && <SetupTerminal jobId={job.id} onStatus={updateJob} />}
          {job?.state === 'waiting_secret' && (
            <form aria-label="Secret prompt" onSubmit={event => void sendAnswer(event)} className="flex flex-wrap items-end gap-2 rounded-[10px] border border-accent/35 bg-accent/[.06] p-3">
              <label className="min-w-[240px] flex-1">
                <span className={LABEL}><KeyRound size={10} className="mr-1 inline" />{job.prompt?.label || 'Secret'}</span>
                <input type="password" autoFocus autoComplete="off" spellCheck={false} aria-label={job.prompt?.label || 'Secret'} disabled={pending} value={answer} onChange={event => setAnswer(event.target.value)} className={INPUT + ' font-mono'} />
              </label>
              <button type="submit" disabled={pending || !answer} className={PRIMARY}>Send</button>
              {answerError && <p role="alert" className="w-full text-xs text-danger">{answerError}</p>}
            </form>
          )}
          {job && !running && job.state !== 'succeeded' && (
            <p role="alert" className="text-xs text-danger">{job.state === 'canceled' ? 'The sign-in was canceled.' : job.error || 'The sign-in failed.'}</p>
          )}
          {error && <p role="alert" className="text-xs text-danger">{error}</p>}
          {confirm
            ? (
              <div role="alertdialog" aria-label="Cancel sign-in" className="space-y-2 rounded-[10px] border border-warn-solid/30 bg-warn-solid/10 p-3">
                <p className="text-xs text-warn">Cancel this sign-in? The login stops{job?.kind === 'create' ? ' and the new profile is removed' : ''}.</p>
                <div className="flex justify-end gap-3">
                  <button type="button" autoFocus disabled={pending} onClick={() => setConfirm('')} className={SECONDARY}>Keep signing in</button>
                  <button type="button" disabled={pending} onClick={() => void cancel(confirm === 'close' ? 'close' : 'stay')} className="bonsai-focus btn-danger-tint h-8 rounded-[7px] px-3 text-[12px] disabled:opacity-40">{pending ? 'Canceling…' : 'Cancel sign-in'}</button>
                </div>
              </div>
            )
            : (
              <Footer>
                {running
                  ? <button type="button" disabled={!job} onClick={() => setConfirm('cancel')} className={SECONDARY}>Cancel sign-in…</button>
                  : (
                    <>
                      {target.kind !== 'resume' && job && <button type="button" onClick={() => { setJob(undefined); setError(''); setStep('details') }} className={SECONDARY}>Try again</button>}
                      <button type="button" onClick={onClose} className={SECONDARY}>Close</button>
                    </>
                  )}
              </Footer>
            )}
        </div>
      )}

      {step === 'done' && (
        <div className="space-y-3">
          <p className="flex items-center gap-2 text-[12px] text-text"><CheckCircle2 size={14} className="text-accent" />{job?.kind === 'login' ? `${profileName} is signed in again.` : `${profileName} is ready.`}</p>
          {account && (
            <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-6 gap-y-1.5 text-[12px]">
              {account.identity && <><dt className="text-muted">Identity</dt><dd className="min-w-0 break-words text-text">{account.identity}</dd></>}
              {account.plan && <><dt className="text-muted">Plan</dt><dd className="min-w-0 break-words text-text">{account.plan}</dd></>}
              <dt className="text-muted">Status</dt><dd className="text-text">{authState(account).label}</dd>
            </dl>
          )}
          {job && job.warnings.length > 0 && (
            <ul aria-label="Warnings" className="space-y-1.5 rounded-[10px] border border-warn-solid/30 bg-warn-solid/10 p-3">
              {job.warnings.map(warning => <li key={warning} className="text-[11px] text-warn">{warning}</li>)}
            </ul>
          )}
          <Footer>
            <button type="button" onClick={onClose} className={SECONDARY}>Close</button>
            {account && job?.kind === 'create' && <button type="button" onClick={() => { onSetDefaults(account.id); onClose() }} className={SECONDARY}>Set defaults</button>}
            {account && <button type="button" onClick={() => { onClose(); onStartAgent(account.id) }} className={PRIMARY}>Start agent with this profile</button>}
          </Footer>
        </div>
      )}

      {step === 'cli' && (
        <div className="space-y-3">
          <p className="text-xs text-muted">This computer cannot run a sign-in terminal for the browser. Run this in a terminal instead; the profile appears here once it is signed in.</p>
          <CopyCommand
            command={login ? accountLoginCommand(login, authMode !== login.auth_mode ? authMode : '') : accountAddCommandFor(providerId, name, authMode)}
            label="Copy command"
          />
          <Footer><button type="button" autoFocus onClick={onClose} className={SECONDARY}>Close</button></Footer>
        </div>
      )}
    </ProfileDialog>
  )
}

function Footer({ children }: { children: ReactNode }) {
  return <div className="flex flex-wrap justify-end gap-3">{children}</div>
}

function JobStatus({ job }: { job?: SetupJob }) {
  if (!job) return <p role="status" className="flex items-center gap-2 text-[12px] text-muted"><Loader2 size={13} className="animate-spin" />Loading the sign-in…</p>
  const active = isSetupJobActive(job.state)
  const icon = active ? <Loader2 size={13} className="animate-spin text-muted" /> : job.state === 'succeeded' ? <CheckCircle2 size={13} className="text-accent" /> : <XCircle size={13} className="text-danger" />
  return (
    <p role="status" aria-label="Sign-in status" className="flex items-center gap-2 text-[12px] text-text">
      {icon}{JOB_STATE_LABEL[job.state] ?? job.state}
    </p>
  )
}
