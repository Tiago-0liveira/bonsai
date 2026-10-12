import * as Tabs from '@radix-ui/react-tabs'
import { useMemo } from 'react'
import { isLiveAgent } from '../../api/agents'
import { isActiveSession } from '../../api/agentsOverview'
import { ProviderLogo } from '../../components/ui/ProviderLogo'
import { useBonsaiStore } from '../../stores/bonsai'
import { AddProfileCommands } from './AddProfile'
import { AUTH_DOT, activeSessionCounts, authState, groupProfiles, profileSessions, summarizeFleet } from './agentsModel'
import { AGENT_TABS, type AgentsTab } from './agentsSearch'
import { ProfileOverview } from './ProfileOverview'
import { ProfileSessions } from './ProfileSessions'
import { ProfileUsage } from './ProfileUsage'
import { ProviderGroup } from './ProviderGroup'
import { SummaryStrip } from './SummaryStrip'
import { useAgentsData } from './useAgentsData'
import { useNow } from './useNow'

const TAB_LABEL: Record<AgentsTab, string> = { overview: 'Overview', usage: 'Usage', sessions: 'Sessions' }
const tabTrigger = 'bonsai-focus relative h-full text-[12px] text-muted transition-colors hover:text-text data-[state=active]:font-medium data-[state=active]:text-text data-[state=active]:after:absolute data-[state=active]:after:inset-x-0 data-[state=active]:after:-bottom-px data-[state=active]:after:h-px data-[state=active]:after:bg-accent'

export interface AgentsPageProps {
  /** Selected profile ID from the URL; the first profile when missing or unknown. */
  profileId?: string
  tab?: AgentsTab
  onSelect: (accountId: string) => void
  onTabChange: (tab: AgentsTab) => void
  /** Called after a session's agent is selected, to bring the canvas into view. */
  onShowOnCanvas: () => void
}

export function AgentsPage({ profileId, tab = 'overview', onSelect, onTabChange, onShowOnCanvas }: AgentsPageProps) {
  const data = useAgentsData()
  const now = useNow()
  const agents = useBonsaiStore(state => state.agents)
  const projects = useBonsaiStore(state => state.projects)
  const worktrees = useBonsaiStore(state => state.worktrees)
  const { providers, accounts } = data

  const groups = useMemo(() => groupProfiles(providers ?? [], accounts ?? []), [providers, accounts])
  const sessionCounts = useMemo(() => activeSessionCounts(agents), [agents])
  const running = useMemo(() => agents.filter(agent => isLiveAgent(agent) && isActiveSession(agent.lifecycleState)).length, [agents])
  const summary = useMemo(() => summarizeFleet(accounts ?? [], data.usage, running, now), [accounts, data.usage, running, now])

  if (!providers || !accounts) {
    return (
      <div className="island grid h-full place-items-center p-6 text-[12px] text-muted">
        {data.error
          ? (
            <div role="alert" className="space-y-3 text-center">
              <p className="text-danger">{data.error}</p>
              <button type="button" onClick={data.reload} className="bonsai-focus btn-bordered h-8 rounded-[7px] px-3 text-[12px]">Retry</button>
            </div>
          )
          : <p>Loading agents…</p>}
      </div>
    )
  }

  const selected = accounts.find(account => account.id === profileId) ?? groups.flatMap(group => group.accounts)[0]
  const capability = providers.find(item => item.id === selected?.provider)
  const sessions = selected ? profileSessions(agents, selected.id, projects, worktrees) : []
  const activeSessions = selected ? sessionCounts.get(selected.id) ?? 0 : 0
  const auth = selected ? authState(selected) : undefined

  return (
    <div className="flex h-full min-h-0 flex-col gap-3">
      <SummaryStrip summary={summary} providers={providers} usageLoaded={data.usageLoaded} now={now} />
      {data.error && <div role="status" className="island shrink-0 px-3 py-2 text-[11px] text-warn">Could not refresh agents: {data.error}</div>}
      <div className="flex min-h-0 flex-1 flex-col gap-3 lg:flex-row">
        <aside aria-label="Providers and profiles" className="island flex max-h-[42%] min-h-0 shrink-0 flex-col overflow-y-auto lg:max-h-none lg:w-[320px]">
          {groups.map(group => (
            <ProviderGroup key={group.id} group={group} selectedId={selected?.id} sessionCounts={sessionCounts} usage={data.usage} onSelect={onSelect} />
          ))}
        </aside>

        <section aria-label="Profile details" className="island flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
          {!selected || !auth ? (
            <div className="space-y-4 overflow-auto p-6">
              <h2 className="text-[14px] font-semibold text-text">{providers.some(item => item.available) ? 'No profiles yet' : 'No agent provider is installed'}</h2>
              <p className="max-w-[56ch] text-[12px] text-muted">
                {providers.some(item => item.available)
                  ? 'A profile holds one sign-in of a provider. Add one from a terminal; it appears here as soon as it exists.'
                  : 'Install a provider CLI, then add a profile for it.'}
              </p>
              <div className="max-w-[460px]"><AddProfileCommands providers={providers} /></div>
            </div>
          ) : (
            <Tabs.Root value={tab} onValueChange={value => onTabChange(value as AgentsTab)} className="flex min-h-0 flex-1 flex-col">
              <header className="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-1 border-b border-border-subtle px-4 pt-3">
                <ProviderLogo provider={selected.provider} size={16} className="shrink-0 text-text" />
                <h2 className="min-w-0 truncate text-[14px] font-semibold text-text">{selected.name}</h2>
                <span className="flex items-center gap-1.5 font-mono text-[10px] text-muted">
                  <span className={'h-1.5 w-1.5 rounded-full ' + AUTH_DOT[auth.tone]} aria-hidden="true" />{auth.label}
                </span>
                {selected.identity && <span className="min-w-0 truncate font-mono text-[10px] text-muted-2">{selected.identity}</span>}
                <Tabs.List aria-label="Profile sections" className="flex h-9 w-full items-end gap-[22px]">
                  {AGENT_TABS.map(value => (
                    <Tabs.Trigger key={value} value={value} className={tabTrigger}>
                      {TAB_LABEL[value]}{value === 'sessions' && sessions.length > 0 && <span className="island-count ml-1.5">{activeSessions || sessions.length}</span>}
                    </Tabs.Trigger>
                  ))}
                </Tabs.List>
              </header>
              <div className="min-h-0 flex-1 overflow-auto">
                <Tabs.Content value="overview" className="outline-none">
                  <ProfileOverview account={selected} capability={capability} />
                </Tabs.Content>
                <Tabs.Content value="usage" className="outline-none">
                  <ProfileUsage
                    account={selected}
                    capability={capability}
                    entry={data.usage[selected.id]}
                    loaded={data.usageLoaded}
                    error={data.usageError}
                    now={now}
                    refreshing={data.refreshing.includes('') || data.refreshing.includes(selected.id)}
                    onRefresh={accountId => void data.refreshUsage(accountId)}
                  />
                </Tabs.Content>
                <Tabs.Content value="sessions" className="outline-none">
                  <ProfileSessions sessions={sessions} now={now} onShowOnCanvas={onShowOnCanvas} />
                </Tabs.Content>
              </div>
            </Tabs.Root>
          )}
        </section>
      </div>
    </div>
  )
}
