import { localFetch } from './localClient'
import type { Agent, AgentProvider, AgentProviderId } from '../types'

export const PROVIDER_LABELS: Record<AgentProviderId, AgentProvider> = { antigravity: 'Antigravity', claude: 'Claude' }
/** True for agents backed by a real local-API session (terminal, stop, reconcile). */
export const isLiveAgent = (agent?: Pick<Agent, 'providerId'>): boolean => !!agent?.providerId && Object.hasOwn(PROVIDER_LABELS, agent.providerId)
export const providerIdFor = (label: AgentProvider): AgentProviderId | undefined => (Object.keys(PROVIDER_LABELS) as AgentProviderId[]).find(id => PROVIDER_LABELS[id] === label)
export interface AgentSummary {
  id: string; project_id: string; worktree_id: string; account_id: string
  provider: AgentProviderId; profile_name: string; name: string
  state: 'starting' | 'running' | 'stopping' | 'exited' | 'failed'
  created_at: string; ended_at?: string; exit_code?: number; error?: string
}
export interface AgentAccount {
  id: string; name: string; provider: AgentProviderId
  full_access?: boolean
  auth_mode?: string; identity?: string; warnings?: string[]
  model?: string; permission_mode?: string; effort?: string; auth_status?: string
}
export interface AgentCapability { id: string; label: string; available: boolean; unavailable_reason?: { message: string }; version?: string }
async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await localFetch(path, init)
  if (!response.ok) {
    if (response.status === 404 && path.startsWith('/api/agents/')) throw new Error('Update Bonsai to enable agent profiles.')
    const body = await response.json().catch(() => ({}))
    throw new Error(body.error?.message ?? `Agent request failed (${response.status})`)
  }
  return response.json() as Promise<T>
}
export const agentProviders = () => request<AgentCapability[]>('/api/agents/providers')
export const agentAccounts = () => request<AgentAccount[]>('/api/agents/accounts')
export const startAgent = (project: string, body: { worktree_id: string; account_id: string; name?: string; model?: string; prompt?: string; full_access?: boolean; permission_mode?: string; effort?: string; cols: number; rows: number }, key: string) => request<AgentSummary>(`/api/projects/${encodeURIComponent(project)}/agents`, { method: 'POST', headers: { 'Content-Type': 'application/json', 'Idempotency-Key': key }, body: JSON.stringify(body) })
export const stopAgent = (project: string, id: string, key: string) => request<AgentSummary>(`/api/projects/${encodeURIComponent(project)}/agents/${encodeURIComponent(id)}`, { method: 'DELETE', headers: { 'Idempotency-Key': key } })
export function mapAgent(a: AgentSummary, previous?: Agent): Agent {
  return { id: a.id, projectId: a.project_id, worktreeId: a.worktree_id, name: a.name || a.profile_name, provider: (Object.hasOwn(PROVIDER_LABELS, a.provider) ? PROVIDER_LABELS[a.provider] : a.provider) as AgentProvider, providerId: a.provider, profileName: a.profile_name, model: '', reasoningEffort: '', workType: '', prompt: '', archived: previous?.archived ?? false, presentation: previous?.presentation ?? 'canvas', state: a.state === 'exited' || a.state === 'failed' ? 'finished' : 'running', lifecycleState: a.state, task: a.error || a.state, runtime: '', terminalId: a.id, createdAt: a.created_at, finishedAt: a.ended_at }
}
