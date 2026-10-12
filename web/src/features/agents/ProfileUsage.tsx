import { RefreshCw } from 'lucide-react'
import type { AgentAccount, AgentCapability, AgentUsage, UsageLimit } from '../../api/agents'
import { formatAge, groupExtraLimits } from './agentsModel'
import { LimitRow } from './UsageMeter'

const LABEL = 'font-mono text-[10px] font-semibold uppercase tracking-[.1em] text-muted-2'

export function ProfileUsage({ account, capability, entry, loaded, error, now, refreshing, onRefresh }: {
  account: AgentAccount
  capability?: AgentCapability
  entry?: AgentUsage
  loaded: boolean
  /** Failure of the whole usage request (older API, connection lost). */
  error: string
  now: number
  refreshing: boolean
  /** Refreshes one profile, or all when called without an ID. */
  onRefresh: (accountId?: string) => void
}) {
  const fetched = entry?.usage?.fetched_at
  const headline = entry?.headline
  const lead = [headline?.primary, headline?.secondary].filter((limit): limit is UsageLimit => limit !== undefined)
  // A provider without a layout still lists what it reported.
  const flat = headline ? [] : entry?.usage?.limits ?? []
  const groups = groupExtraLimits(headline?.extra)
  const failure = entry?.error

  return (
    <div className="space-y-4 p-4">
      <div className="flex flex-wrap items-center gap-2">
        <div className="min-w-0 flex-1 text-[11px] text-muted-2">
          {entry?.usage?.plan && <span className="mr-3 text-text">Plan: {entry.usage.plan}</span>}
          {fetched && <span>Updated {formatAge(fetched, now)}</span>}
        </div>
        <button type="button" disabled={refreshing} onClick={() => onRefresh(account.id)} className="bonsai-focus btn-bordered inline-flex h-7 items-center gap-1.5 rounded-[7px] px-2.5 text-[11px] text-muted hover:text-text disabled:opacity-50">
          <RefreshCw size={12} className={refreshing ? 'animate-spin' : ''} /> Refresh
        </button>
        <button type="button" disabled={refreshing} onClick={() => onRefresh()} className="bonsai-focus btn-ghost h-7 text-[11px]">Refresh all</button>
      </div>

      {entry?.throttled && <p role="status" className="text-[11px] text-muted-2">Refreshed a moment ago; showing the saved reading{fetched ? ` from ${formatAge(fetched, now)}` : ''}. Providers are asked at most every 30 seconds.</p>}

      {!loaded && !entry && <p className="text-[12px] text-muted">Loading usage…</p>}
      {loaded && !entry && !error && <p className="text-[12px] text-muted">No usage reading for this profile yet.</p>}
      {error && !entry && (
        <div role="alert" className="space-y-2 text-[12px]">
          <p className="text-danger">{error}</p>
          <button type="button" onClick={() => onRefresh()} className="bonsai-focus btn-bordered h-7 rounded-[7px] px-2.5 text-[11px]">Retry</button>
        </div>
      )}

      {failure && (
        <div className="space-y-2 rounded-[10px] border border-border bg-well p-3 text-[12px]">
          <p className="font-medium text-text">
            {failure.code === 'unsupported' ? 'Usage is not available for this profile'
              : failure.code === 'not_authenticated' ? 'Not signed in'
                : 'Usage is unavailable right now'}
          </p>
          <p className="text-muted">{failure.message}</p>
          {failure.code === 'unavailable' && <button type="button" disabled={refreshing} onClick={() => onRefresh(account.id)} className="bonsai-focus btn-bordered h-7 rounded-[7px] px-2.5 text-[11px] disabled:opacity-50">Retry</button>}
        </div>
      )}

      {lead.length > 0 && (
        <ul aria-label="Plan limits" className="space-y-3">
          {lead.map(limit => <LimitRow key={limit.id} limit={limit} now={now} />)}
        </ul>
      )}
      {groups.map(({ group, limits }) => (
        <section key={group || 'other'} aria-label={group || 'Other limits'} className="space-y-2">
          <h3 className={LABEL}>{group || 'Other limits'}</h3>
          <ul className="space-y-3">{limits.map(limit => <LimitRow key={limit.id} limit={limit} now={now} />)}</ul>
        </section>
      ))}
      {flat.length > 0 && (
        <ul aria-label="Limits" className="space-y-3">
          {flat.map(limit => <LimitRow key={limit.id} limit={limit} now={now} />)}
        </ul>
      )}
      {headline && lead.length === 0 && groups.length === 0 && <p className="text-[12px] text-muted">The provider reported no limits for this profile.</p>}

      {entry?.usage?.warnings?.map(warning => <p key={warning} className="text-[11px] text-warn">{warning}</p>)}
      {capability?.usage_note && <p className="border-t border-border-subtle pt-3 text-[11px] text-muted-2">{capability.usage_note}</p>}
    </div>
  )
}
