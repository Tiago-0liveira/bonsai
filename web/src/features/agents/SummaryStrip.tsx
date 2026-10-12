import type { AgentCapability } from '../../api/agents'
import { toPercent, formatCountdown, type FleetSummary } from './agentsModel'
import { AddProfile } from './AddProfile'

function Stat({ label, value, detail, tone }: { label: string; value: string; detail?: string; tone?: 'warn' }) {
  return (
    <div className="min-w-0 px-3 py-1.5" title={detail}>
      <div className="font-mono text-[9.5px] uppercase tracking-[.1em] text-muted-2">{label}</div>
      <div className={'truncate text-[13px] font-semibold ' + (tone === 'warn' ? 'text-warn' : 'text-text')}>{value}</div>
      {detail && <div className="truncate font-mono text-[10px] text-muted-2">{detail}</div>}
    </div>
  )
}

export function SummaryStrip({ summary, providers, usageLoaded, now }: {
  summary: FleetSummary
  providers: AgentCapability[]
  usageLoaded: boolean
  now: number
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
      <div className="ml-auto pl-3"><AddProfile providers={providers} /></div>
    </div>
  )
}
