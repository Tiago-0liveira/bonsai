import { formatCountdown, limitName, toPercent } from './agentsModel'
import type { UsageLimit } from '../../api/agents'

/** Same threshold as `bonsai agent usage` and the API's `low` flag. */
export const LOW_HEADROOM = 0.2

/** A thin bar of remaining headroom, tinted when it runs low. */
export function UsageMeter({ label, fraction, className = '' }: { label: string; fraction?: number; className?: string }) {
  const percent = fraction === undefined ? undefined : toPercent(fraction)
  const low = fraction !== undefined && fraction < LOW_HEADROOM
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
      <div className={'h-full rounded-full ' + (low ? 'bg-warn-solid' : 'bg-accent-solid')} style={{ width: `${percent ?? 0}%` }} />
    </div>
  )
}

/** One plan limit: name, remaining bar, percentage and reset countdown (absolute time on hover). */
export function LimitRow({ limit, now }: { limit: UsageLimit; now: number }) {
  const name = limitName(limit)
  const percent = limit.remaining_fraction === undefined ? undefined : toPercent(limit.remaining_fraction)
  const reset = formatCountdown(limit.resets_at, now)
  const low = limit.remaining_fraction !== undefined && limit.remaining_fraction < LOW_HEADROOM
  return (
    <li className="grid grid-cols-[minmax(0,8.5rem)_minmax(0,1fr)_3rem_minmax(0,8rem)] items-center gap-3 text-[12px]">
      <span className="truncate" title={limit.window ? `${name} · ${limit.window}` : name}>{name}</span>
      <UsageMeter label={name} fraction={limit.remaining_fraction} />
      <span className={'text-right font-mono text-[11px] ' + (low ? 'text-warn' : 'text-text')}>{percent === undefined ? '—' : `${percent}%`}</span>
      {limit.resets_at && reset
        ? <time dateTime={limit.resets_at} title={new Date(limit.resets_at).toLocaleString()} className="truncate font-mono text-[10px] text-muted-2">resets {reset}</time>
        : <span />}
    </li>
  )
}
