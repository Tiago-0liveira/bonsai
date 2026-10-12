import type { AgentProviderId } from '../types'
import { SETUP_HINTS, agentRequest, readAgentJSON, type AgentAccount, type AgentSummary, type AgentUsage } from './agents'
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
