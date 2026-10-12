import type { AgentProviderId } from '../types'
import { SETUP_HINTS, agentRequest, readAgentJSON, type AgentAccount, type AgentSummary, type AgentUsage, type UsageHeadline, type UsageLimit } from './agents'
import { LOCAL_API_SAME_ORIGIN, localFetch } from './localClient'

// Read side of the Agents tab. It has its own module, imported only by the lazy
// page, so none of it is part of the first screen.

/** The CLI command that adds a profile for a provider. */
export const accountAddCommand = (id: AgentProviderId): string =>
  ['bonsai agent account add', id, '<name>', SETUP_HINTS[id]].filter(Boolean).join(' ')

/** Sessions in these states hold the profile; the others are history. */
export const isActiveSession = (state?: AgentSummary['state']) => state === 'starting' || state === 'running' || state === 'stopping'

export interface AccountsRevision { accounts: AgentAccount[]; etag?: string }
/**
 * Profiles, revalidated with the ETag of an earlier answer. Resolves to
 * undefined when the server says nothing changed. The conditional header only
 * goes out same-origin: cross-origin it would need a preflight the API does
 * not allow, so those pages fetch the list in full.
 */
export async function agentAccountsIfChanged(etag?: string): Promise<AccountsRevision | undefined> {
  const path = '/api/agents/accounts'
  const response = await localFetch(path, etag && LOCAL_API_SAME_ORIGIN ? { headers: { 'If-None-Match': etag } } : undefined)
  if (response.status === 304) return undefined
  const accounts = await readAgentJSON<AgentAccount[]>(response, path)
  return { accounts, etag: LOCAL_API_SAME_ORIGIN ? response.headers.get('ETag') ?? undefined : undefined }
}


/** Cached usage of every profile, or of one. Never contacts a provider while the cache is fresh. */
export const agentUsage = (accountId?: string) =>
  agentRequest<AgentUsage[]>(`/api/agents/usage${accountId ? `?account_id=${encodeURIComponent(accountId)}` : ''}`)
/** Asks providers for fresh usage; the API refreshes each profile at most once every 30 seconds. */
export const refreshAgentUsage = (accountId?: string) =>
  agentRequest<AgentUsage[]>('/api/agents/usage/refresh', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(accountId ? { account_id: accountId } : {}) })

/** "5h", "week": a limit's window, else its label or ID. */
const shortLimitName = (limit: UsageLimit) => limit.window === 'weekly' ? 'week' : limit.window || limit.label || limit.id

/** "5h 72% · week 40%": the headline pair of a profile's cached usage; '' when it has none. */
export function headroomLabel(headline?: UsageHeadline): string {
  return [headline?.primary, headline?.secondary]
    .filter((limit): limit is UsageLimit => limit?.remaining_fraction !== undefined)
    .map(limit => `${shortLimitName(limit)} ${Math.round(Math.min(Math.max(limit.remaining_fraction!, 0), 1) * 100)}%`)
    .join(' · ')
}

/** Profiles with the most headroom first; profiles without a score keep their order after them. */
export function byHeadroom<T extends { id: string }>(accounts: T[], usage: Record<string, AgentUsage | undefined>): T[] {
  const score = (account: T) => usage[account.id]?.headline?.score
  return [...accounts].sort((a, b) => {
    const left = score(a), right = score(b)
    if (left === undefined || right === undefined) return Number(left === undefined) - Number(right === undefined)
    return right - left
  })
}

/** A failed profile change. `code` and `sessionIds` come from the API's error body. */
export class AgentProfileError extends Error {
  constructor(message: string, readonly status: number, readonly code: string, readonly sessionIds: string[] = []) {
    super(message)
    this.name = 'AgentProfileError'
  }
}

async function profileRequest<T>(path: string, init: RequestInit): Promise<T> {
  const response = await localFetch(path, init)
  if (response.ok) return response.json() as Promise<T>
  const body = await response.json().catch(() => ({})) as { error?: { code?: string; message?: string; session_ids?: unknown } }
  const sessionIds = Array.isArray(body.error?.session_ids) ? body.error.session_ids.filter((id): id is string => typeof id === 'string') : []
  const message = body.error?.message ?? (response.status === 404 ? 'Update Bonsai to edit agent profiles.' : `Agent request failed (${response.status})`)
  throw new AgentProfileError(message, response.status, body.error?.code ?? '', sessionIds)
}

/** Renames a profile and/or changes its defaults; a null default returns that field to the provider's own. */
export const updateAgentProfile = (id: string, change: { name?: string; defaults?: Record<string, string | null> }) =>
  profileRequest<AgentAccount>(`/api/agents/accounts/${encodeURIComponent(id)}`, { method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(change) })

/** Deletes a profile; rejects with code `profile_in_use` and the session IDs while sessions run. */
export const removeAgentProfile = (id: string) =>
  profileRequest<{ warnings: string[] }>(`/api/agents/accounts/${encodeURIComponent(id)}`, { method: 'DELETE' })
