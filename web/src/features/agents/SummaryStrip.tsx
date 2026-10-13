import type { ComponentType } from 'react'
import { Activity, Clock, Gauge, Plus, RefreshCw, ShieldCheck, TrendingDown, UsersRound, type LucideProps } from 'lucide-react'
import { ProviderLogo } from '../../components/ui/ProviderLogo'
import { AUTH_DOT, formatAge, formatCountdown, toPercent, type FleetSummary, type ProviderSummary, type ProviderWindow } from './agentsModel'
import { LOW_HEADROOM, UsageMeter } from './UsageMeter'

type Tone = 'warn' | 'ok' | 'bad'

const TONE_TEXT: Record<Tone | 'none', string> = { ok: 'text-accent', warn: 'text-warn', bad: 'text-danger', none: 'text-text' }
const TONE_BUBBLE: Record<Tone | 'none', string> = {
  ok: 'bg-accent/12 text-accent',
  warn: 'bg-warn-solid/18 text-warn',
  bad: 'bg-danger-solid/30 text-danger',
  none: 'bg-panel-3 text-muted',
}

/** A fleet number with its own icon bubble. Label, then value, then detail, in that reading order. */
function Metric({ icon: Icon, label, value, detail, tone, title }: {
  icon: ComponentType<LucideProps>
  label: string
  value: string
  detail?: string
  tone?: Tone
  title?: string
}) {
  const key = tone ?? 'none'
  return (
    <div className="flex min-w-0 items-center gap-2.5 rounded-[11px] border border-border-subtle bg-well/45 py-1.5 pl-1.5 pr-3.5" title={title ?? detail}>
      <span className={'grid h-8 w-8 shrink-0 place-items-center rounded-[9px] ' + TONE_BUBBLE[key]} aria-hidden="true"><Icon size={15} /></span>
      <span className="min-w-0">
        <span className="block font-mono text-[9.5px] uppercase leading-3 tracking-[.1em] text-muted-2">{label}</span>
        <span className={'block truncate text-[15px] font-semibold leading-5 ' + TONE_TEXT[key]}>{value}</span>
        <span className="block h-3 truncate font-mono text-[10px] leading-3 text-muted-2">{detail ?? ''}</span>
      </span>
    </div>
  )
}


/** A pooled plan window: the headroom of every profile of the provider added up. */
function WindowRow({ item, now }: { item: ProviderWindow; now: number }) {
  const low = item.fraction < LOW_HEADROOM
  const reset = item.resetsAt ? formatCountdown(item.resetsAt, now).replace(/^in /, '') : ''
  const left = item.remaining.toFixed(1).replace(/\.0$/, '')
  return (
    <li className="grid grid-cols-[3.5rem_minmax(0,1fr)_2.5rem] items-center gap-x-2.5" title={`${item.label}: ${left} of ${item.count} ${item.count === 1 ? 'profile' : 'profiles'} left${reset ? `, first reset in ${reset}` : ''}`}>
      <span className="truncate font-mono text-[10px] uppercase tracking-[.06em] text-muted-2">{item.label}</span>
      <UsageMeter label={`${item.label} pooled`} fraction={item.fraction} />
      <span className={'text-right font-mono text-[11px] ' + (low ? 'text-warn' : 'text-text')}>{toPercent(item.fraction)}%</span>
      <span />
      <span className="col-span-2 truncate font-mono text-[10px] text-muted-2">
        {[item.count > 1 ? `${left} of ${item.count} left` : '', reset ? `next reset ${reset}` : ''].filter(Boolean).join(' · ') || 'no reset reported'}
      </span>
    </li>
  )
}

/** One provider: its sign-ins, its plan windows pooled over every profile, and what is running. */
function ProviderTile({ item, usageLoaded, now }: { item: ProviderSummary; usageLoaded: boolean; now: number }) {
  return (
    <li className={'min-w-0 rounded-[11px] border border-border-subtle bg-well/45 px-3.5 py-2.5 ' + (item.available ? '' : 'opacity-70')} aria-label={item.label}>
      <div className="flex items-center gap-2">
        <ProviderLogo provider={item.id} size={15} className="shrink-0 text-text" />
        <span className="text-[12.5px] font-semibold text-text">{item.label}</span>
        <span className="min-w-0 flex-1 truncate font-mono text-[10px] text-muted-2" title={item.note}>{item.note}</span>
        {item.running > 0 && <span className="shrink-0 rounded-[5px] bg-accent/12 px-1.5 font-mono text-[9.5px] text-accent">{item.running} running</span>}
      </div>
      {item.profiles === 0 ? (
        <p className="mt-2 font-mono text-[10px] text-muted-2">{item.available ? 'No profiles yet' : 'Not installed'}</p>
      ) : (
        <>
          <div className="mt-2 flex items-center gap-1" title={`${item.signedIn} of ${item.profiles} signed in`}>
            {item.tones.slice(0, 10).map((tone, index) => <span key={index} aria-hidden="true" className={'h-1.5 w-1.5 rounded-full ' + AUTH_DOT[tone]} />)}
            <span className="ml-1 font-mono text-[10px] text-muted">{item.signedIn}/{item.profiles} signed in</span>
          </div>
          {item.windows.length > 0 ? (
            <ul aria-label={`${item.label} plan windows`} className="mt-2 space-y-1.5">
              {item.windows.map(window => <WindowRow key={window.slot} item={window} now={now} />)}
            </ul>
          ) : (
            <p className="mt-2 font-mono text-[10px] text-muted-2">{usageLoaded ? 'no usage data' : 'loading usage'}</p>
          )}
        </>
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
  const needSignIn = summary.profiles - summary.signedIn
  const age = formatAge(usageAt, now)
  return (
    <div className="island shrink-0 space-y-3 px-3 py-3" role="region" aria-label="Fleet summary">
      <div className="flex flex-wrap items-center gap-2.5">
        <Metric icon={UsersRound} label="Profiles" value={String(summary.profiles)} detail={`${providers.filter(item => item.profiles > 0).length} providers`} />
        <Metric
          icon={ShieldCheck}
          label="Signed in"
          value={`${summary.signedIn}/${summary.profiles}`}
          detail={needSignIn > 0 ? `${needSignIn} need sign-in` : 'all signed in'}
          tone={summary.profiles === 0 ? undefined : needSignIn > 0 ? 'warn' : 'ok'}
        />
        <Metric icon={Activity} label="Running" value={String(summary.running)} detail={summary.running === 1 ? 'session' : 'sessions'} tone={summary.running > 0 ? 'ok' : undefined} />
        <Metric
          icon={Gauge}
          label="Low headroom"
          value={usageLoaded ? String(summary.low) : '—'}
          detail={usageLoaded ? 'under 20%' : 'loading usage'}
          tone={summary.low > 0 ? 'warn' : undefined}
        />
        <Metric icon={TrendingDown} label="Lowest" value={lowest ? `${toPercent(lowest.score)}%` : '—'} detail={lowest?.name} tone={lowest && lowest.score < LOW_HEADROOM ? 'warn' : undefined} />
        <Metric icon={Clock} label="Next reset" value={nextReset ? formatCountdown(nextReset.at, now).replace(/^in /, '') : '—'} detail={nextReset?.name} />
        <div className="ml-auto flex items-center gap-2">
          <span className="hidden font-mono text-[10px] text-muted-2 lg:inline" title={usageAt ? new Date(usageAt).toLocaleString() : undefined}>
            {refreshing ? 'Reading usage…' : age ? `Usage read ${age}` : usageLoaded ? 'No usage read yet' : ''}
          </span>
          <button
            type="button"
            disabled={refreshing}
            onClick={onRefresh}
            className="bonsai-focus btn-bordered inline-flex h-8 items-center gap-1.5 rounded-[8px] px-3 text-[11px] text-muted hover:text-text disabled:opacity-50"
          >
            <RefreshCw size={12} className={refreshing ? 'animate-spin' : ''} aria-hidden="true" /> Check usage
          </button>
          <AddProfileButton onClick={onAddProfile} />
        </div>
      </div>
      {providers.length > 0 && (
        <ul aria-label="Providers at a glance" className="grid gap-2.5 sm:grid-cols-2 xl:grid-cols-3">
          {providers.map(item => <ProviderTile key={item.id} item={item} usageLoaded={usageLoaded} now={now} />)}
        </ul>
      )}
    </div>
  )
}

export function AddProfileButton({ onClick }: { onClick: () => void }) {
  return (
    <button type="button" aria-haspopup="dialog" onClick={onClick} className="bonsai-focus btn-accent-tint inline-flex h-8 items-center gap-1.5 rounded-[8px] px-3 text-[11px] font-medium">
      <Plus size={12} /> Add profile
    </button>
  )
}
