import { useEffect, useId, useState } from 'react'
import { ChevronDown } from 'lucide-react'
import type { AgentUsage } from '../../api/agents'
import { ProviderLogo } from '../../components/ui/ProviderLogo'
import type { ProviderGroupModel } from './agentsModel'
import { ProfileRow } from './ProfileRow'

const COLLAPSED_KEY = 'bonsai.agents.collapsedProviders'
const versionLabel = (version: string) => (/^\d/.test(version) ? `v${version}` : version)

function collapsedProviders(): string[] {
  try {
    const stored: unknown = JSON.parse(localStorage.getItem(COLLAPSED_KEY) ?? '[]')
    return Array.isArray(stored) ? stored.filter((id): id is string => typeof id === 'string') : []
  } catch { return [] }
}
function rememberCollapsed(id: string, collapsed: boolean) {
  try {
    const rest = collapsedProviders().filter(item => item !== id)
    localStorage.setItem(COLLAPSED_KEY, JSON.stringify(collapsed ? [...rest, id] : rest))
  } catch { /* per-viewer convenience only */ }
}

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
  const bodyId = useId()
  const [collapsed, setCollapsed] = useState(() => collapsedProviders().includes(group.id))
  const hasSelected = group.accounts.some(account => account.id === selectedId)
  // A profile picked from a link or another list must not stay hidden in a collapsed group.
  useEffect(() => { if (hasSelected) setCollapsed(false) }, [hasSelected, selectedId])
  const toggle = () => { rememberCollapsed(group.id, !collapsed); setCollapsed(!collapsed) }
  const running = group.accounts.reduce((sum, account) => sum + (sessionCounts.get(account.id) ?? 0), 0)
  return (
    <section aria-label={group.label} className="px-2 pb-2 pt-3">
      <h3 className="mb-1 text-[12px] font-semibold text-text">
        <button
          type="button"
          aria-expanded={!collapsed}
          aria-controls={bodyId}
          onClick={toggle}
          className="bonsai-focus flex w-full items-center gap-2 rounded-md px-1 py-0.5 text-left transition-colors hover:bg-panel-2"
        >
          <ChevronDown size={12} aria-hidden="true" className={'shrink-0 text-muted-2 transition-transform ' + (collapsed ? '-rotate-90' : '')} />
          <ProviderLogo provider={group.id} size={14} className="shrink-0 text-text" />
          <span>{group.label}</span>
          <span className="min-w-0 flex-1 truncate font-mono text-[10px] font-normal text-muted-2" title={unavailable ? reason : undefined}>
            {unavailable ? reason : capability?.version ? versionLabel(capability.version) : ''}
          </span>
          {collapsed && (
            <span className="flex shrink-0 items-center gap-1.5 font-mono text-[10px] font-normal text-muted-2">
              {running > 0 && <span className="rounded-[5px] bg-accent/12 px-1.5 text-[9.5px] text-accent">{running} running</span>}
              <span className="island-count" aria-label={`${group.accounts.length} profiles`}>{group.accounts.length}</span>
            </span>
          )}
        </button>
      </h3>
      <div id={bodyId} hidden={collapsed}>
        {!collapsed && (
          <>
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
          </>
        )}
      </div>
    </section>
  )
}
