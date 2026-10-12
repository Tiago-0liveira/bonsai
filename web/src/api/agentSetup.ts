import type { AgentAccount } from './agents'
import { AgentProfileError, agentProfileRequest } from './agentsOverview'
import { localFetch } from './localClient'

// Web sign-in: setup jobs that create a profile or sign one in again. Imported
// only by the Agents page's sign-in dialog, so none of it is on the first screen.

export type SetupJobState = 'preparing' | 'running' | 'waiting_secret' | 'verifying' | 'succeeded' | 'failed' | 'canceled'
/** A setup job as the API shows it. It never carries a secret. */
export interface SetupJob {
  id: string
  kind: 'create' | 'login'
  provider: string
  account_id?: string
  name: string
  auth_mode?: string
  state: SetupJobState
  /** Set while the job waits for a pasted key or token. */
  prompt?: { label: string }
  error?: string
  warnings: string[]
  created_at: string
  ended_at?: string
  /** The described profile, on a succeeded job read over HTTP. */
  account?: AgentAccount
}

export const isSetupJobActive = (state: SetupJobState) => state !== 'succeeded' && state !== 'failed' && state !== 'canceled'

/** Creates a profile. `secret` is a pasted API key or token; the API never stores or echoes it. */
export interface ProfileSetupBody { provider: string; name: string; auth_mode?: string; seed?: boolean; secret?: string }
/** Signs an existing profile in again; without `auth_mode` it keeps its method. */
export interface ProfileLoginBody { auth_mode?: string; secret?: string }

const post = (body: object, key: string): RequestInit => ({ method: 'POST', headers: { 'Content-Type': 'application/json', 'Idempotency-Key': key }, body: JSON.stringify(body) })
const jobPath = (id: string) => `/api/agents/setup/${encodeURIComponent(id)}`

/** Starts a profile setup job; a retry with the same key and body answers the same job. */
export const startProfileSetup = (body: ProfileSetupBody, key: string) => agentProfileRequest<SetupJob>('/api/agents/setup', post(body, key))
/** Starts a sign-in job for an existing profile. */
export const startProfileLogin = (accountId: string, body: ProfileLoginBody, key: string) =>
  agentProfileRequest<SetupJob>(`/api/agents/accounts/${encodeURIComponent(accountId)}/login`, post(body, key))
/** Reads a job; rejects with status 404 once the job is no longer retained. */
export const setupJob = (id: string) => agentProfileRequest<SetupJob>(jobPath(id), { method: 'GET' })
/** Cancels a job: its login process is stopped and a new profile is removed. */
export const cancelSetupJob = (id: string) => agentProfileRequest<SetupJob>(jobPath(id), { method: 'DELETE' })

/** Answers a job's secret prompt; rejects with code `no_prompt` when it is not waiting. */
export async function answerSetupSecret(id: string, secret: string): Promise<void> {
  const response = await localFetch(`${jobPath(id)}/secret`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ secret }) })
  if (response.ok) return
  const body = await response.json().catch(() => ({})) as { error?: { code?: string; message?: string } }
  throw new AgentProfileError(body.error?.message ?? `Agent request failed (${response.status})`, response.status, body.error?.code ?? '')
}
