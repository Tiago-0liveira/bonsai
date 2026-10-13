import { localFetch } from './localClient'
import type { Agent, AgentProvider, AgentProviderId } from '../types'

export const PROVIDER_LABELS: Record<AgentProviderId, AgentProvider> = { antigravity: 'Antigravity', claude: 'Claude', codex: 'Codex' }
/** Extra `bonsai agent account add` flags worth pointing at when a provider has no profile yet. */
export const SETUP_HINTS: Partial<Record<AgentProviderId, string>> = { codex: '[--auth device]' }
/** True for agents backed by a real local-API session (terminal, stop, reconcile). */
export const isLiveAgent = (agent?: Pick<Agent, 'providerId'>): boolean => !!agent?.providerId && Object.hasOwn(PROVIDER_LABELS, agent.providerId)
export const providerIdFor = (label: AgentProvider): AgentProviderId | undefined => (Object.keys(PROVIDER_LABELS) as AgentProviderId[]).find(id => PROVIDER_LABELS[id] === label)
export interface AgentSummary {
  id: string; project_id: string; worktree_id: string; account_id: string
  provider: AgentProviderId; profile_name: string; name: string
  state: 'starting' | 'running' | 'stopping' | 'exited' | 'failed'
  created_at: string; ended_at?: string; exit_code?: number; error?: string
  /** The launch model and the accepted launch options keyed by start request field; never the prompt. */
  model?: string; options?: Record<string, string>
}
/** Profile fields plus the provider's profile defaults, keyed by launch field ID. */
export interface AgentAccount {
  id: string; name: string; provider: AgentProviderId
  auth_mode?: string; identity?: string; warnings?: string[]
  /** The subscription plan's display name ("Pro", "Google AI Pro"), when known. */
  plan?: string
  model?: string; auth_status?: string
  created_at?: string; updated_at?: string
  /** Saved profile defaults by profile field ID; a field with no entry uses the provider's default. */
  defaults?: Record<string, string>
  [option: string]: unknown
}
/** One value of a launch field. A toggle has a "false" and a "true" option. */
export interface LaunchFieldOption {
  value: string; label: string
  /** "danger": the agent acts without asking. */
  tone?: 'danger'
  /** Shown as a note while a danger option is chosen. */
  help?: string
  /** Preset: the request fields this option sends instead of the field's own ID. */
  set?: Record<string, string>
}
/** A launch option the provider accepts; the dialog renders it without knowing the provider. */
export interface LaunchField {
  /** The start request field this value is sent as. */
  id: string; label: string
  /** "model" appears only in a profile's fields: free text with model suggestions. */
  kind: 'select' | 'toggle' | 'model'
  options?: LaunchFieldOption[]
  /** Initial value; empty selects the first option. A toggle starts from the profile's value. */
  default?: string
  /** Values come from the chosen model's efforts; options are the fallback. */
  per_model?: boolean
  help?: string
}
/** Start request fields produced by the launch fields, flattened into the request body. */
export type LaunchOptions = Record<string, string | boolean>
export interface AgentCapability {
  id: string; label: string; available: boolean; unavailable_reason?: { message: string }; version?: string; launch_fields?: LaunchField[]
  /** Where the provider's usage numbers come from, when that needs a caveat. */
  usage_note?: string
  /** The fields of a profile's defaults: the model, then the launch fields with a "Provider default" choice. */
  profile_fields?: LaunchField[]
  /** A short install command or link, present while the provider is unavailable. */
  install_hint?: string
  /** How profiles of this provider are created and signed in; missing when the provider declares nothing. */
  setup?: SetupSpec
}
/** A pasted key or token a sign-in method takes. Optional: the provider can get one another way. */
export interface SecretSpec { label: string; placeholder?: string; optional?: boolean }
/** One sign-in method of a provider. */
export interface AuthModeOption {
  value: string; label: string; help?: string
  /** The provider's choice for this host (device code over SSH, say). */
  recommended?: boolean
  secret?: SecretSpec
}
export interface SetupSpec {
  /** Empty: the provider has one implicit method. */
  auth_modes?: AuthModeOption[]
  /** Where a new profile is seeded from, home-relative for display; missing when the provider does not seed. */
  seed?: { source: string }
  notes?: string[]
}
async function readJSON<T>(response: Response, path: string): Promise<T> {
  if (!response.ok) {
    if (response.status === 404 && path.startsWith('/api/agents/')) throw new Error('Update Bonsai to enable agent profiles.')
    const body = await response.json().catch(() => ({}))
    throw new Error(body.error?.message ?? `Agent request failed (${response.status})`)
  }
  return response.json() as Promise<T>
}
async function request<T>(path: string, init?: RequestInit): Promise<T> {
  return readJSON<T>(await localFetch(path, init), path)
}
// The read side of the Agents tab lives in agentsOverview.ts so it stays out of the first screen.
export { readJSON as readAgentJSON, request as agentRequest }
export const agentProviders = () => request<AgentCapability[]>('/api/agents/providers')
export interface AgentModel {
  id: string; label: string; description?: string; source: 'alias' | 'api' | 'bundled'
  /** Reasoning efforts this model accepts, when they vary by model. */
  efforts?: string[]; default_effort?: string
  /** The provider says this model has no effort control (as opposed to not saying). */
  no_effort?: boolean
  /** The model the provider uses when none is chosen. */
  default?: boolean
}
/** Model suggestions for the launch dialog; empty when the provider cannot list models. */
export const agentModels = (provider: string, accountId?: string) =>
  request<AgentModel[]>(`/api/agents/providers/${encodeURIComponent(provider)}/models${accountId ? `?account_id=${encodeURIComponent(accountId)}` : ''}`)
export const agentAccounts = () => request<AgentAccount[]>('/api/agents/accounts')
/** One plan limit: a window of the plan (5 h, weekly) or a metered feature. */
export interface UsageLimit {
  id: string; group?: string; label?: string; window?: string
  /** 0..1; missing when the provider does not report it. */
  remaining_fraction?: number
  resets_at?: string
}
/** A balance of money (`unit` is a currency such as "USD") or provider credits (`unit` "credits"), in major units. */
export interface UsageCredit {
  id: string; label: string; unit: string
  used?: number; limit?: number; balance?: number
  unlimited?: boolean; enabled: boolean
  resets_at?: string
}
/** A one-off rate-limit reset the account was granted (Codex). Shown only; Bonsai never redeems one. */
export interface ResetCredit {
  title: string
  /** "available" when it can be used now. */
  status?: string
  /** Resets left in this grant; missing means one. */
  count?: number
  /** Limits a reset refills, such as "five_hour". */
  clears?: string[]
  granted_at?: string; expires_at?: string
}
export interface UsageSnapshot {
  provider: string; account_id: string; fetched_at: string; plan?: string; limits: UsageLimit[]; warnings?: string[]
  credits?: UsageCredit[]; reset_credits?: ResetCredit[]
}
/** The provider's reading of a snapshot: which limits lead, and the profile's score. */
export interface UsageHeadline { primary?: UsageLimit; secondary?: UsageLimit; extra?: UsageLimit[]; score?: number; low?: boolean; next_reset?: string }
export type UsageErrorCode = 'unsupported' | 'not_authenticated' | 'unavailable'
export interface AgentUsage {
  account_id: string; provider: string
  usage?: UsageSnapshot; headline?: UsageHeadline
  error?: { code: UsageErrorCode | (string & {}); message: string }
  /** A refresh was asked for too soon and the cached snapshot answered. */
  throttled?: boolean
}
export const startAgent = (project: string, { options, ...body }: { worktree_id: string; account_id: string; name?: string; model?: string; prompt?: string; options?: LaunchOptions; cols: number; rows: number }, key: string) => request<AgentSummary>(`/api/projects/${encodeURIComponent(project)}/agents`, { method: 'POST', headers: { 'Content-Type': 'application/json', 'Idempotency-Key': key }, body: JSON.stringify({ ...options, ...body }) })
export const stopAgent = (project: string, id: string, key: string) => request<AgentSummary>(`/api/projects/${encodeURIComponent(project)}/agents/${encodeURIComponent(id)}`, { method: 'DELETE', headers: { 'Idempotency-Key': key } })
export function mapAgent(a: AgentSummary, previous?: Agent): Agent {
  return { id: a.id, projectId: a.project_id, worktreeId: a.worktree_id, accountId: a.account_id, name: a.name || a.profile_name, provider: (Object.hasOwn(PROVIDER_LABELS, a.provider) ? PROVIDER_LABELS[a.provider] : a.provider) as AgentProvider, providerId: a.provider, profileName: a.profile_name, model: a.model ?? '', reasoningEffort: a.options?.effort ?? '', launchOptions: a.options, workType: '', prompt: '', archived: previous?.archived ?? false, presentation: previous?.presentation ?? 'canvas', state: a.state === 'exited' || a.state === 'failed' ? 'finished' : 'running', lifecycleState: a.state, task: a.error || a.state, runtime: '', terminalId: a.id, createdAt: a.created_at, finishedAt: a.ended_at, exitCode: a.exit_code }
}
