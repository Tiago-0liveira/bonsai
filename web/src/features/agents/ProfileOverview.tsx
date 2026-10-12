import { Bot } from 'lucide-react'
import { PROVIDER_LABELS, type AgentAccount, type AgentCapability } from '../../api/agents'
import { useBonsaiStore } from '../../stores/bonsai'
import { authState, profileDefaults } from './agentsModel'
import { CopyCommand } from './CopyCommand'

const LABEL = 'font-mono text-[10px] font-semibold uppercase tracking-[.1em] text-muted-2'
const when = (iso?: string) => { const time = iso ? Date.parse(iso) : NaN; return Number.isFinite(time) ? new Date(time).toLocaleString() : '' }

export function ProfileOverview({ account, capability }: { account: AgentAccount; capability?: AgentCapability }) {
  const openStartAgentDialog = useBonsaiStore(state => state.openStartAgentDialog)
  const auth = authState(account)
  const defaults = profileDefaults(capability?.launch_fields ?? [], account)
  const unavailable = capability !== undefined && !capability.available
  const rows: [string, string][] = [
    ['Provider', capability?.label ?? PROVIDER_LABELS[account.provider as keyof typeof PROVIDER_LABELS] ?? account.provider],
    ['Sign-in method', account.auth_mode ?? ''],
    ['Identity', account.identity ?? ''],
    ['Status', auth.label],
    ['Created', when(account.created_at)],
    ['Updated', when(account.updated_at)],
  ]
  return (
    <div className="space-y-5 p-4">
      <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-6 gap-y-2 text-[12px]">
        {rows.filter(([, value]) => value).map(([label, value]) => (
          <div key={label} className="contents">
            <dt className="text-muted">{label}</dt>
            <dd className="min-w-0 break-words text-text">{value}</dd>
          </div>
        ))}
      </dl>

      {account.warnings && account.warnings.length > 0 && (
        <ul aria-label="Warnings" className="space-y-1.5 rounded-[10px] border border-warn-solid/30 bg-warn-solid/10 p-3">
          {account.warnings.map(warning => <li key={warning} className="text-[11px] text-warn">{warning}</li>)}
        </ul>
      )}

      <section aria-label="Profile defaults">
        <h3 className={LABEL + ' mb-2'}>Profile defaults</h3>
        {defaults.length === 0
          ? <p className="text-[11px] text-muted-2">None saved. New sessions use the provider's own defaults.</p>
          : (
            <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-6 gap-y-1.5 text-[12px]">
              {defaults.map(item => (
                <div key={item.label} className="contents">
                  <dt className="text-muted">{item.label}</dt>
                  <dd className="min-w-0 break-words text-text">{item.value}</dd>
                </div>
              ))}
            </dl>
          )}
      </section>

      <section aria-label="Sign in again" className="space-y-2">
        <h3 className={LABEL}>Sign in again</h3>
        <p className="text-[11px] text-muted-2">Open the provider in this profile from a terminal and sign in there.</p>
        <CopyCommand command={`bonsai agent run ${account.provider}/${account.name}`} label="Copy sign-in command" />
      </section>

      <div>
        <button
          type="button"
          disabled={unavailable}
          title={unavailable ? capability.unavailable_reason?.message || 'Not available' : undefined}
          onClick={() => openStartAgentDialog('', account.id)}
          className="bonsai-focus btn-primary inline-flex h-8 items-center gap-2 px-3 text-[12px] disabled:cursor-not-allowed disabled:opacity-40"
        >
          <Bot size={13} /> Start agent
        </button>
      </div>
    </div>
  )
}
