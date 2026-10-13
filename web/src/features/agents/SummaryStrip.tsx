import { Plus } from 'lucide-react'
import { toPercent, formatCountdown, type FleetSummary } from './agentsModel'

function Stat({ label, value, detail, tone }: { label: string; value: string; detail?: string; tone?: 'warn' }) {
  return (
    <div className="min-w-0 px-3 py-1.5" title={detail}>
      <div className="font-mono text-[9.5px] uppercase tracking-[.1em] text-muted-2">{label}</div>
      <div className={'truncate text-[13px] font-semibold ' + (tone === 'warn' ? 'text-warn' : 'text-text')}>{value}</div>
      {detail && <div className="truncate font-mono text-[10px] text-muted-2">{detail}</div>}
    </div>
  )
}

export function SummaryStrip({ summary, usageLoaded, now, onAddProfile }: {
  summary: FleetSummary
  usageLoaded: boolean
  now: number
  onAddProfile: () => void
}) {
  const { lowest, nextReset } = summary
  return (
    <div className="island flex shrink-0 flex-wrap items-center gap-y-1 py-1.5 pl-1 pr-3" role="region" aria-label="Fleet summary">
      <Stat label="Profiles" value={String(summary.profiles)} />
      <Stat label="Signed in" value={`${summary.signedIn}/${summary.profiles}`} />
      <Stat label="Running" value={String(summary.running)} detail={summary.running === 1 ? 'session' : 'sessions'} />
      <Stat
        label="Low headroom"
        value={usageLoaded ? String(summary.low) : '—'}
        detail={usageLoaded ? 'under 20%' : 'loading usage'}
        tone={summary.low > 0 ? 'warn' : undefined}
      />
      <Stat label="Lowest" value={lowest ? `${toPercent(lowest.score)}%` : '—'} detail={lowest?.name} tone={lowest && lowest.score < 0.2 ? 'warn' : undefined} />
      <Stat label="Next reset" value={nextReset ? formatCountdown(nextReset.at, now).replace(/^in /, '') : '—'} detail={nextReset?.name} />
      <div className="ml-auto pl-3"><AddProfileButton onClick={onAddProfile} /></div>
    </div>
  )
}

export function AddProfileButton({ onClick }: { onClick: () => void }) {
  return (
    <button type="button" aria-haspopup="dialog" onClick={onClick} className="bonsai-focus btn-accent-tint inline-flex h-7 items-center gap-1.5 rounded-[7px] px-2.5 text-[11px] font-medium">
      <Plus size={12} /> Add profile
    </button>
  )
}
