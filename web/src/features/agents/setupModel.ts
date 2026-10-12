import type { AgentAccount, AuthModeOption, SetupSpec } from '../../api/agents'
import type { ProfileSetupBody, SetupJobState } from '../../api/agentSetup'
import { profileNameProblem } from './profileForm'

/** Why a new profile name cannot be used, or '' when it can; the API's rule, names unique per provider. */
export function profileNameError(name: string, provider: string, accounts: AgentAccount[]): string {
  if (!name) return 'Enter a name for the profile.'
  return profileNameProblem(name, accounts.filter(account => account.provider === provider).map(account => account.name))
}

/** The provider's recommended method, else its first; '' when it has no choice. */
export function defaultAuthMode(spec?: SetupSpec, current?: string): string {
  const modes = spec?.auth_modes ?? []
  if (current && modes.some(mode => mode.value === current)) return current
  return (modes.find(mode => mode.recommended) ?? modes[0])?.value ?? ''
}

export const authModeOf = (spec: SetupSpec | undefined, value: string): AuthModeOption | undefined => spec?.auth_modes?.find(mode => mode.value === value)

/** Why a pasted secret is missing, or '' when the method needs none or it may be left empty. */
export function secretError(mode: AuthModeOption | undefined, secret: string): string {
  if (!mode?.secret || mode.secret.optional || secret.trim()) return ''
  return `Paste the ${mode.secret.label}.`
}

/**
 * The request a key belongs to. The secret is left out, as the API does: a
 * retry with the same choices reuses the key whatever was pasted.
 */
export function setupFingerprint(target: string, body: Partial<ProfileSetupBody>): string {
  const fields = Object.entries(body).filter(([key, value]) => key !== 'secret' && value !== undefined).sort(([a], [b]) => a.localeCompare(b))
  return JSON.stringify([target, fields])
}

export const JOB_STATE_LABEL: Record<SetupJobState, string> = {
  preparing: 'Preparing the profile…',
  running: 'Waiting for the sign-in to finish in the terminal…',
  waiting_secret: 'Waiting for you to paste a secret…',
  verifying: 'Checking the sign-in…',
  succeeded: 'Signed in',
  failed: 'Sign-in failed',
  canceled: 'Sign-in canceled',
}

/** The CLI command for a new profile, for platforms where the browser cannot sign in. */
export const accountAddCommandFor = (provider: string, name: string, mode: string) =>
  ['bonsai agent account add', provider, name || '<name>', mode && `--auth ${mode}`].filter(Boolean).join(' ')
/** The CLI command that signs a profile in again. */
export const accountLoginCommand = (account: Pick<AgentAccount, 'provider' | 'name'>, mode = '') =>
  [`bonsai agent account login ${account.provider}/${account.name}`, mode && `--auth ${mode}`].filter(Boolean).join(' ')

/** A running sign-in the page reattaches to after a reload. Holds no secret. */
export interface RememberedSetupJob { id: string; kind: 'create' | 'login'; accountId?: string }
const JOB_KEY = 'bonsai.agents.setupJob'

export function rememberSetupJob(job: RememberedSetupJob) {
  try { sessionStorage.setItem(JOB_KEY, JSON.stringify(job)) } catch { /* reattach is a convenience */ }
}
export function recallSetupJob(): RememberedSetupJob | undefined {
  try {
    const value = JSON.parse(sessionStorage.getItem(JOB_KEY) ?? 'null') as Partial<RememberedSetupJob> | null
    if (!value || typeof value.id !== 'string' || !value.id || (value.kind !== 'create' && value.kind !== 'login')) return undefined
    return { id: value.id, kind: value.kind, accountId: typeof value.accountId === 'string' ? value.accountId : undefined }
  } catch { return undefined }
}
export function forgetSetupJob(id?: string) {
  try {
    if (id && recallSetupJob()?.id !== id) return
    sessionStorage.removeItem(JOB_KEY)
  } catch { /* nothing to forget */ }
}
