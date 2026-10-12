import { Check } from 'lucide-react'
import { ProviderLogo } from '../../components/ui/ProviderLogo'
import { headroomLabel } from '../../api/agentsOverview'
import type { AgentAccount, AgentUsage } from '../../api/agents'

/**
 * The provider's profiles as cards: every one is visible with its login and
 * remaining headroom, so one click picks it and a single profile reads as the
 * confirmed choice. Past four profiles the list scrolls.
 */
export function ProfilePicker({ accounts, usage, value, onChange, loading, disabled }: {
  accounts: AgentAccount[]
  usage: Record<string, AgentUsage>
  value: string
  onChange: (id: string) => void
  loading: boolean
  disabled: boolean
}) {
  if (loading) {
    return <div role="status" aria-label="Profile" className="skeleton h-[58px]"><span className="sr-only">Loading profiles…</span></div>
  }
  if (!accounts.length) {
    return <div role="listbox" aria-label="Profile" className="rounded-[10px] border border-dashed border-border px-3 py-4 text-center font-mono text-[10.5px] text-muted-2">No profile for this provider yet.</div>
  }
  return (
    <div role="listbox" aria-label="Profile" className={'space-y-2 ' + (accounts.length > 4 ? 'max-h-[260px] overflow-auto pr-1' : '')}>
      {accounts.map(account => {
        const selected = account.id === value
        const detail = account.identity || account.auth_mode
        const headroom = headroomLabel(usage[account.id]?.headline)
        return (
          <button
            type="button"
            role="option"
            key={account.id}
            aria-selected={selected}
            disabled={disabled}
            onClick={() => onChange(account.id)}
            className="profile-card bonsai-focus disabled:cursor-not-allowed disabled:opacity-50"
          >
            <span className="grid h-8 w-8 shrink-0 place-items-center rounded-[8px] border border-border bg-panel-2"><ProviderLogo provider={account.provider} size={15} className="text-text" /></span>
            <span className="min-w-0 flex-1">
              <span className="block truncate text-[12.5px] font-medium text-text">{account.name}</span>
              {detail && <span className="mt-0.5 block truncate font-mono text-[10px] text-muted-2">{detail}</span>}
            </span>
            {headroom && <span className="shrink-0 font-mono text-[10px] text-muted-2">{headroom}</span>}
            <Check size={14} aria-hidden="true" className={'shrink-0 text-accent transition-opacity ' + (selected ? 'opacity-100' : 'opacity-0')} />
          </button>
        )
      })}
    </div>
  )
}
