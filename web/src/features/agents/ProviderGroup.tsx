import type { AgentUsage } from '../../api/agents'
import { ProviderLogo } from '../../components/ui/ProviderLogo'
import type { ProviderGroupModel } from './agentsModel'
import { ProfileRow } from './ProfileRow'

const versionLabel = (version: string) => (/^\d/.test(version) ? `v${version}` : version)

export function ProviderGroup({ group, selectedId, sessionCounts, usage, onSelect }: {
  group: ProviderGroupModel
  selectedId?: string
  sessionCounts: Map<string, number>
  usage: Record<string, AgentUsage>
  onSelect: (accountId: string) => void
}) {
  const { capability } = group
  const unavailable = capability !== undefined && !capability.available
  const reason = capability?.unavailable_reason?.message || 'Not available'
  return (
    <section aria-label={group.label} className="px-2 pb-2 pt-3">
      <header className="mb-1 flex items-center gap-2 px-1">
        <ProviderLogo provider={group.id} size={14} className="shrink-0 text-text" />
        <h3 className="text-[12px] font-semibold text-text">{group.label}</h3>
        <span className="min-w-0 flex-1 truncate font-mono text-[10px] text-muted-2" title={unavailable ? reason : undefined}>
          {unavailable ? reason : capability?.version ? versionLabel(capability.version) : ''}
        </span>
      </header>
      {unavailable && capability.install_hint && <p className="mb-1 px-1 font-mono text-[10px] text-muted-2"><code className="select-all">{capability.install_hint}</code></p>}
      {group.accounts.length === 0
        ? <p className="px-1 py-1 text-[11px] text-muted-2">No profiles</p>
        : (
          <ul aria-label={`${group.label} profiles`} className="space-y-0.5">
            {group.accounts.map(account => (
              <ProfileRow
                key={account.id}
                account={account}
                selected={account.id === selectedId}
                sessions={sessionCounts.get(account.id) ?? 0}
                headline={usage[account.id]?.headline}
                onSelect={() => onSelect(account.id)}
              />
            ))}
          </ul>
        )}
    </section>
  )
}
