import type { AgentAccount, UsageHeadline } from '../../api/agents'
import { AUTH_DOT, authState, identityLine, toPercent } from './agentsModel'
import { UsageMeter } from './UsageMeter'

export function ProfileRow({ account, selected, sessions, headline, onSelect }: {
  account: AgentAccount
  selected: boolean
  /** Active sessions of this profile in any project. */
  sessions: number
  headline?: UsageHeadline
  onSelect: () => void
}) {
  const auth = authState(account)
  const score = headline?.score
  const identity = identityLine(account)
  return (
    <li>
      <button
        type="button"
        aria-current={selected ? 'true' : undefined}
        onClick={onSelect}
        className={'bonsai-focus flex w-full flex-col gap-1 rounded-lg px-2.5 py-2 text-left transition-colors ' + (selected ? 'bg-panel-3' : 'hover:bg-panel-2')}
      >
        <span className="flex items-center gap-2">
          <span className={'h-1.5 w-1.5 shrink-0 rounded-full ' + AUTH_DOT[auth.tone]} aria-hidden="true" />
          <span className="min-w-0 flex-1 truncate text-[12px] font-medium text-text">{account.name}</span>
          <span className="sr-only">{auth.label}</span>
          {sessions > 0 && <span className="shrink-0 rounded-[5px] bg-accent/12 px-1.5 font-mono text-[9.5px] text-accent">{sessions} running</span>}
        </span>
        {(identity || score !== undefined) && (
          <span className="flex items-center gap-2 pl-3.5">
            <span className="min-w-0 flex-1 truncate font-mono text-[10px] text-muted-2">{identity}</span>
            {score !== undefined && (
              <span className="flex shrink-0 items-center gap-1.5" title={`${toPercent(score)}% headroom${headline?.low ? ' (low)' : ''}`}>
                <UsageMeter label={`${account.name} headroom`} fraction={score} className="w-12" />
                <span className={'w-8 text-right font-mono text-[10px] ' + (headline?.low ? 'text-warn' : 'text-muted')}>{toPercent(score)}%</span>
              </span>
            )}
          </span>
        )}
      </button>
    </li>
  )
}
