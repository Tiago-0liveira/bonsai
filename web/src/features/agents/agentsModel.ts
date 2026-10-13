import { isLiveAgent, type AgentAccount, type AgentCapability, type AgentUsage, type LaunchField, type UsageLimit } from '../../api/agents'
import { isActiveSession } from '../../api/agentsOverview'
import type { Agent, Project, Worktree } from '../../types'

/** A provider and the profiles under it, in display order. */
export interface ProviderGroupModel {
  id: string
  label: string
  /** Missing when only profiles of a provider this build does not list exist. */
  capability?: AgentCapability
  accounts: AgentAccount[]
}

const byName = (a: AgentAccount, b: AgentAccount) => a.name.localeCompare(b.name, undefined, { sensitivity: 'base' }) || a.id.localeCompare(b.id)

/** The providers the Agents tab lists first, in this order; any other provider follows in API order. */
const PROVIDER_ORDER = ['claude', 'codex', 'antigravity']
const providerRank = (id: string) => { const rank = PROVIDER_ORDER.indexOf(id); return rank < 0 ? PROVIDER_ORDER.length : rank }

/** Claude, Codex, Antigravity, then other providers in API order, then providers only known from profiles; profiles by name. */
export function groupProfiles(providers: AgentCapability[], accounts: AgentAccount[]): ProviderGroupModel[] {
  // Array.prototype.sort is stable, so providers of equal rank keep the API order.
  const ordered = [...providers].sort((a, b) => providerRank(a.id) - providerRank(b.id))
  const groups = ordered.map<ProviderGroupModel>(capability => ({ id: capability.id, label: capability.label || capability.id, capability, accounts: [] }))
  const byId = new Map(groups.map(group => [group.id, group]))
  for (const account of [...accounts].sort(byName)) {
    let group = byId.get(account.provider)
    if (!group) {
      group = { id: account.provider, label: account.provider, accounts: [] }
      byId.set(group.id, group)
      groups.push(group)
    }
    group.accounts.push(account)
  }
  return groups
}

export type AuthTone = 'ok' | 'bad' | 'unknown' | 'none'
export interface AuthState { tone: AuthTone; label: string }

// `auth_status` values the providers report. Anything else reads as unknown.
const AUTH_STATES: Record<string, AuthState> = {
  logged_in: { tone: 'ok', label: 'Signed in' },
  token: { tone: 'ok', label: 'Token' },
  logged_out: { tone: 'bad', label: 'Signed out' },
  expired: { tone: 'bad', label: 'Token expired' },
  missing: { tone: 'bad', label: 'Token missing' },
  unknown: { tone: 'unknown', label: 'Status unknown' },
}

export function authState(account: Pick<AgentAccount, 'auth_status'>): AuthState {
  const status = account.auth_status
  if (!status) return { tone: 'none', label: 'Not checked' }
  if (Object.hasOwn(AUTH_STATES, status)) return AUTH_STATES[status]
  const words = status.replace(/_/g, ' ')
  return { tone: 'unknown', label: words.charAt(0).toUpperCase() + words.slice(1) }
}

export const AUTH_DOT: Record<AuthTone, string> = { ok: 'bg-accent-solid', bad: 'bg-danger-solid', unknown: 'bg-warn-solid', none: 'bg-muted-2' }

export interface ProfileSession {
  agent: Agent
  /** The project the session runs in, '' when it cannot be resolved. */
  projectId: string
  /** Project and branch names resolved from the store; ids when the project is not loaded. */
  project: string
  branch: string
  active: boolean
}

const startedAt = (agent: Agent) => { const time = Date.parse(agent.createdAt); return Number.isFinite(time) ? time : 0 }

/** Sessions of one profile across every project: running first, then newest first. */
export function profileSessions(agents: Agent[], accountId: string, projects: Project[], worktrees: Worktree[]): ProfileSession[] {
  return agents
    .filter(agent => isLiveAgent(agent) && agent.accountId === accountId)
    .map<ProfileSession>(agent => {
      const worktree = worktrees.find(item => item.id === agent.worktreeId)
      const projectId = agent.projectId ?? worktree?.projectId ?? ''
      return {
        agent,
        projectId,
        project: projects.find(item => item.id === projectId)?.name ?? projectId,
        branch: worktree?.branch ?? '',
        active: isActiveSession(agent.lifecycleState),
      }
    })
    .sort((a, b) => Number(b.active) - Number(a.active) || startedAt(b.agent) - startedAt(a.agent))
}

/** Active sessions per profile ID. */
export function activeSessionCounts(agents: Agent[]): Map<string, number> {
  const counts = new Map<string, number>()
  for (const agent of agents) {
    if (!isLiveAgent(agent) || !agent.accountId || !isActiveSession(agent.lifecycleState)) continue
    counts.set(agent.accountId, (counts.get(agent.accountId) ?? 0) + 1)
  }
  return counts
}

export const toPercent = (fraction: number) => Math.round(Math.min(Math.max(fraction, 0), 1) * 100)

/** The label of a limit; ids such as `five_hour` are shown as words when no label came. */
export function limitName(limit: UsageLimit): string {
  if (limit.label) return limit.label
  const words = limit.id.replace(/[_-]+/g, ' ').trim()
  return words.charAt(0).toUpperCase() + words.slice(1)
}

/** "in 2h 15m", "in 3d 4h"; "due" once the reset time has passed. */
export function formatCountdown(iso: string | undefined, now: number): string {
  const at = iso ? Date.parse(iso) : NaN
  if (!Number.isFinite(at)) return ''
  const minutes = Math.floor((at - now) / 60_000)
  if (at - now <= 0) return 'due'
  if (minutes < 1) return 'in <1m'
  if (minutes < 60) return `in ${minutes}m`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `in ${hours}h ${minutes % 60}m`
  return `in ${Math.floor(hours / 24)}d ${hours % 24}h`
}

/** "just now", "42s ago", "5m ago", "3h ago". */
export function formatAge(iso: string | undefined, now: number): string {
  const at = iso ? Date.parse(iso) : NaN
  if (!Number.isFinite(at)) return ''
  const seconds = Math.max(0, Math.floor((now - at) / 1000))
  if (seconds < 10) return 'just now'
  if (seconds < 60) return `${seconds}s ago`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`
  if (seconds < 86_400) return `${Math.floor(seconds / 3600)}h ago`
  return `${Math.floor(seconds / 86_400)}d ago`
}

export interface FleetSummary {
  profiles: number
  signedIn: number
  /** Profiles whose headline reports low headroom. */
  low: number
  running: number
  lowest?: { accountId: string; name: string; score: number }
  nextReset?: { accountId: string; name: string; at: string }
}

export function summarizeFleet(accounts: AgentAccount[], usage: Record<string, AgentUsage>, running: number, now: number): FleetSummary {
  const summary: FleetSummary = { profiles: accounts.length, signedIn: 0, low: 0, running }
  for (const account of accounts) {
    if (authState(account).tone === 'ok') summary.signedIn++
    const headline = usage[account.id]?.headline
    if (!headline) continue
    if (headline.low) summary.low++
    if (headline.score !== undefined && (!summary.lowest || headline.score < summary.lowest.score)) {
      summary.lowest = { accountId: account.id, name: account.name, score: headline.score }
    }
    const reset = headline.next_reset ? Date.parse(headline.next_reset) : NaN
    if (Number.isFinite(reset) && reset > now && (!summary.nextReset || reset < Date.parse(summary.nextReset.at))) {
      summary.nextReset = { accountId: account.id, name: account.name, at: headline.next_reset! }
    }
  }
  return summary
}

/** One provider's slice of the fleet, for the summary strip. */
export interface ProviderSummary {
  id: string
  label: string
  available: boolean
  /** The version, or why the provider is unavailable. */
  note: string
  /** Sign-in tone of each profile, in list order. */
  tones: AuthTone[]
  profiles: number
  signedIn: number
  running: number
  lowest?: { name: string; score: number }
  nextReset?: { name: string; at: string }
}

export function summarizeProviders(groups: ProviderGroupModel[], usage: Record<string, AgentUsage>, sessionCounts: Map<string, number>, now: number): ProviderSummary[] {
  return groups.map(group => {
    const capability = group.capability
    const available = capability === undefined || capability.available
    const version = capability?.version ? (/^\d/.test(capability.version) ? `v${capability.version}` : capability.version) : ''
    const summary: ProviderSummary = {
      id: group.id,
      label: group.label,
      available,
      note: available ? version : capability?.unavailable_reason?.message || 'Not available',
      tones: group.accounts.map(account => authState(account).tone),
      profiles: group.accounts.length,
      signedIn: 0,
      running: 0,
    }
    for (const account of group.accounts) {
      if (authState(account).tone === 'ok') summary.signedIn++
      summary.running += sessionCounts.get(account.id) ?? 0
      const headline = usage[account.id]?.headline
      if (!headline) continue
      if (headline.score !== undefined && (!summary.lowest || headline.score < summary.lowest.score)) summary.lowest = { name: account.name, score: headline.score }
      const reset = headline.next_reset ? Date.parse(headline.next_reset) : NaN
      if (Number.isFinite(reset) && reset > now && (!summary.nextReset || reset < Date.parse(summary.nextReset.at))) summary.nextReset = { name: account.name, at: headline.next_reset! }
    }
    return summary
  })
}

/** When the newest usage reading was taken, or undefined when none was. */
export function usageReadAt(usage: Record<string, AgentUsage>): string | undefined {
  let newest: { at: number; iso: string } | undefined
  for (const entry of Object.values(usage)) {
    const iso = entry.usage?.fetched_at
    const at = iso ? Date.parse(iso) : NaN
    if (Number.isFinite(at) && (!newest || at > newest.at)) newest = { at, iso: iso! }
  }
  return newest?.iso
}

/** Extra limits grouped by their `group`, keeping provider order; ungrouped limits come first. */
export function groupExtraLimits(extra: UsageLimit[] | undefined): { group: string; limits: UsageLimit[] }[] {
  const groups = new Map<string, UsageLimit[]>()
  for (const limit of extra ?? []) {
    const key = limit.group ?? ''
    groups.set(key, [...(groups.get(key) ?? []), limit])
  }
  return [...groups].map(([group, limits]) => ({ group, limits })).sort((a, b) => Number(!!a.group) - Number(!!b.group))
}

export interface ProfileDefault { label: string; value: string }

/**
 * A profile's saved defaults as "field: value" rows, labelled by the provider's
 * launch fields. A preset field (one whose options carry `set`) shows the
 * option that matches the profile's saved fields.
 */
export function profileDefaults(fields: LaunchField[], account: AgentAccount): ProfileDefault[] {
  const out: ProfileDefault[] = []
  if (typeof account.model === 'string' && account.model) out.push({ label: 'Model', value: account.model })
  for (const field of fields) {
    if (field.kind === 'toggle') {
      const value = account[field.id]
      if (typeof value !== 'boolean') continue
      out.push({ label: field.label, value: field.options?.find(option => option.value === String(value))?.label ?? String(value) })
      continue
    }
    const preset = field.options?.find(option => option.set && Object.entries(option.set).every(([key, expected]) => account[key] === expected))
    if (preset) {
      out.push({ label: field.label, value: preset.label })
      continue
    }
    const value = account[field.id]
    if (typeof value !== 'string' || !value) continue
    out.push({ label: field.label, value: field.options?.find(option => option.value === value)?.label ?? value })
  }
  return out
}
