import { Plus, RefreshCw } from 'lucide-react'
import { ProviderLogo } from '../../components/ui/ProviderLogo'
import { AUTH_DOT, formatAge, formatCountdown, toPercent, type FleetSummary, type ProviderSummary } from './agentsModel'
import { LOW_HEADROOM, UsageMeter } from './UsageMeter'

type Tone = 'warn' | 'ok'

function Stat({ label, value, detail, tone }: { label: string; value: string; detail?: string; tone?: Tone }) {
  return (
    <div className="min-w-0 border-l border-border-subtle px-4 py-0.5 first:border-l-0 first:pl-3" title={detail}>
      <div className="font-mono text-[9.5px] uppercase tracking-[.1em] text-muted-2">{label}</div>
      <div className={'truncate text-[15px] font-semibold leading-6 ' + (tone === 'warn' ? 'text-warn' : tone === 'ok' ? 'text-accent' : 'text-text')}>{value}</div>
      <div className="h-[14px] truncate font-mono text-[10px] text-muted-2">{detail ?? ''}</div>
    </div>
  )
}

/** One provider: how many of its profiles are signed in, its tightest headroom, what is running and the next reset. */
function ProviderTile({ item, usageLoaded, now }: { item: ProviderSummary; usageLoaded: boolean; now: number }) {
  const lowest = item.lowest
  const low = lowest !== undefined && lowest.score < LOW_HEADROOM
  const reset = item.nextReset ? formatCountdown(item.nextReset.at, now).replace(/^in /, '') : ''
  return (
    <li className={'min-w-0 rounded-[10px] border border-border-subtle bg-well/45 px-3 py-2 ' + (item.available ? '' : 'opacity-70')} aria-label={item.label}>
      <div className="flex items-center gap-2">
        <ProviderLogo provider={item.id} size={14} className="shrink-0 text-text" />
        <span className="text-[12px] font-semibold text-text">{item.label}</span>
        <span className="min-w-0 flex-1 truncate font-mono text-[10px] text-muted-2" title={item.note}>{item.note}</span>
        {item.running > 0 && <span className="shrink-0 rounded-[5px] bg-accent/12 px-1.5 font-mono text-[9.5px] text-accent">{item.running} running</span>}
      </div>
      {item.profiles === 0 ? (
        <p className="mt-2 font-mono text-[10px] text-muted-2">{item.available ? 'No profiles yet' : 'Not installed'}</p>
      ) : (
        <div className="mt-2 grid grid-cols-[auto_minmax(0,1fr)] items-center gap-x-3 gap-y-1">
          <span className="flex items-center gap-1" title={`${item.signedIn} of ${item.profiles} signed in`}>
            {item.tones.slice(0, 8).map((tone, index) => <span key={index} aria-hidden="true" className={'h-1.5 w-1.5 rounded-full ' + AUTH_DOT[tone]} />)}
            <span className="ml-1 font-mono text-[10px] text-muted">{item.signedIn}/{item.profiles} signed in</span>
          </span>
          <span className="flex min-w-0 items-center justify-end gap-2" title={lowest ? `${lowest.name} has the least headroom` : undefined}>
            {lowest ? (
              <>
                <UsageMeter label={`${item.label} tightest headroom`} fraction={lowest.score} className="w-14" />
                <span className={'font-mono text-[10px] ' + (low ? 'text-warn' : 'text-muted')}>{toPercent(lowest.score)}%</span>
              </>
            ) : <span className="font-mono text-[10px] text-muted-2">{usageLoaded ? 'no usage data' : 'loading usage'}</span>}
          </span>
          {reset && <span className="col-span-2 truncate font-mono text-[10px] text-muted-2">resets in {reset}{item.nextReset && item.profiles > 1 ? ` · ${item.nextReset.name}` : ''}</span>}
        </div>
      )}
    </li>
  )
}

export function SummaryStrip({ summary, providers, usageLoaded, usageAt, refreshing, now, onAddProfile, onRefresh }: {
  summary: FleetSummary
  providers: ProviderSummary[]
  usageLoaded: boolean
  /** When the newest usage reading was taken. */
  usageAt?: string
  refreshing: boolean
  now: number
  onAddProfile: () => void
  onRefresh: () => void
}) {
  const { lowest, nextReset } = summary
  const age = formatAge(usageAt, now)
  return (
    <div className="island shrink-0 space-y-2.5 px-2 py-2.5" role="region" aria-label="Fleet summary">
      <div className="flex flex-wrap items-center gap-y-2">
        <Stat label="Profiles" value={String(summary.profiles)} detail={providers.length ? `${providers.filter(item => item.profiles > 0).length} providers` : undefined} />
        <Stat
          label="Signed in"
          value={`${summary.signedIn}/${summary.profiles}`}
          detail={summary.profiles - summary.signedIn > 0 ? `${summary.profiles - summary.signedIn} need sign-in` : 'all signed in'}
          tone={summary.profiles > 0 && summary.signedIn === summary.profiles ? 'ok' : summary.signedIn < summary.profiles ? 'warn' : undefined}
        />
        <Stat label="Running" value={String(summary.running)} detail={summary.running === 1 ? 'session' : 'sessions'} tone={summary.running > 0 ? 'ok' : undefined} />
        <Stat
          label="Low headroom"
          value={usageLoaded ? String(summary.low) : '—'}
          detail={usageLoaded ? 'under 20%' : 'loading usage'}
          tone={summary.low > 0 ? 'warn' : undefined}
        />
        <Stat label="Lowest" value={lowest ? `${toPercent(lowest.score)}%` : '—'} detail={lowest?.name} tone={lowest && lowest.score < LOW_HEADROOM ? 'warn' : undefined} />
        <Stat label="Next reset" value={nextReset ? formatCountdown(nextReset.at, now).replace(/^in /, '') : '—'} detail={nextReset?.name} />
        <div className="ml-auto flex items-center gap-2 pl-3 pr-1">
          <span className="hidden font-mono text-[10px] text-muted-2 sm:inline" title={usageAt ? new Date(usageAt).toLocaleString() : undefined}>
            {refreshing ? 'Reading usage…' : age ? `Usage read ${age}` : usageLoaded ? 'No usage read yet' : ''}
          </span>
          <button
            type="button"
            disabled={refreshing}
            onClick={onRefresh}
            className="bonsai-focus btn-bordered inline-flex h-7 items-center gap-1.5 rounded-[7px] px-2.5 text-[11px] text-muted hover:text-text disabled:opacity-50"
          >
            <RefreshCw size={12} className={refreshing ? 'animate-spin' : ''} aria-hidden="true" /> Check usage
          </button>
          <AddProfileButton onClick={onAddProfile} />
        </div>
      </div>
      {providers.length > 0 && (
        <ul aria-label="Providers at a glance" className="grid gap-2 px-1 sm:grid-cols-2 xl:grid-cols-3">
          {providers.map(item => <ProviderTile key={item.id} item={item} usageLoaded={usageLoaded} now={now} />)}
        </ul>
      )}
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
