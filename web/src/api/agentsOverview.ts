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
