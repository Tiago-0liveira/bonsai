import { formatCountdown, limitName, toPercent } from './agentsModel'
import type { UsageLimit } from '../../api/agents'

/** Same threshold as `bonsai agent usage` and the API's `low` flag. */
export const LOW_HEADROOM = 0.2

/** How many colours a meter has: rank 1 is almost empty (red), rank 10 is nearly full (green). */
export const USAGE_RANKS = 10

/** 0-9% remaining is rank 1, 10-19% rank 2, and so on up to 90-100% as rank 10. */
export function usageRank(fraction: number): number {
  return Math.min(USAGE_RANKS, Math.floor(toPercent(fraction) / (100 / USAGE_RANKS)) + 1)
}

/** A thin bar of remaining headroom, coloured by how much is left: the fuller, the better. */
export function UsageMeter({ label, fraction, className = '' }: { label: string; fraction?: number; className?: string }) {
  const percent = fraction === undefined ? undefined : toPercent(fraction)
  return (
    <div
      role="meter"
      aria-label={`${label} remaining`}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={percent}
      aria-valuetext={percent === undefined ? 'unknown' : `${percent}% remaining`}
      className={'h-1.5 overflow-hidden rounded-full bg-panel-4 ' + className}
    >
      <div className="usage-fill h-full rounded-full" data-rank={fraction === undefined ? undefined : usageRank(fraction)} style={{ width: `${percent ?? 0}%` }} />
    </div>
  )
}

/** "Oct 18, 09:00": a reset's date and time in the viewer's time zone. */
export function formatResetDate(iso: string): string {
  return new Date(iso).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

/** One plan limit: name, remaining bar, percentage, reset countdown and its date. */
export function LimitRow({ limit, now }: { limit: UsageLimit; now: number }) {
  const name = limitName(limit)
  const percent = limit.remaining_fraction === undefined ? undefined : toPercent(limit.remaining_fraction)
  const reset = formatCountdown(limit.resets_at, now)
  const low = limit.remaining_fraction !== undefined && limit.remaining_fraction < LOW_HEADROOM
  // A window that has not started yet (nothing used) has no reset time.
  const notStarted = !limit.resets_at && percent === 100
  return (
    <li className="grid grid-cols-[minmax(0,8.5rem)_minmax(0,1fr)_3rem_minmax(0,11rem)] items-center gap-3 text-[12px]">
      <span className="truncate" title={limit.window ? `${name} · ${limit.window}` : name}>{name}</span>
      <UsageMeter label={name} fraction={limit.remaining_fraction} />
      <span className={'text-right font-mono text-[11px] ' + (low ? 'text-warn' : 'text-text')}>{percent === undefined ? '—' : `${percent}%`}</span>
      {limit.resets_at && reset
        ? <time dateTime={limit.resets_at} title={new Date(limit.resets_at).toLocaleString()} className="truncate font-mono text-[10px] text-muted-2">resets {reset} · {formatResetDate(limit.resets_at)}</time>
        : notStarted ? <span className="truncate font-mono text-[10px] text-muted-2">window not started</span> : <span />}
    </li>
  )
}
