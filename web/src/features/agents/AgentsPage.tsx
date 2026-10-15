import * as Tabs from '@radix-ui/react-tabs'
import { lazy, Suspense, useCallback, useEffect, useMemo, useState } from 'react'
import { isLiveAgent, type AgentAccount } from '../../api/agents'
import { isActiveSession } from '../../api/agentsOverview'
import { ProviderLogo } from '../../components/ui/ProviderLogo'
import { useBonsaiStore } from '../../stores/bonsai'
import type { SetupTarget } from './AddProfileDialog'
import { AUTH_DOT, activeSessionCounts, authState, groupProfiles, identityLine, profileSessions, summarizeFleet, summarizeProviders, usageReadAt } from './agentsModel'
import { AGENT_TABS, type AgentsTab } from './agentsSearch'
import { ProfileDefaults } from './ProfileDefaults'
import { DiscardChangesDialog } from './ProfileDialog'
import { ProfileName } from './ProfileName'
import { ProfileOverview } from './ProfileOverview'
import { ProfileSessions } from './ProfileSessions'
import { ProfileUsage } from './ProfileUsage'
import { ProviderGroup } from './ProviderGroup'
import { RemoveProfileDialog } from './RemoveProfileDialog'
import { recallSetupJob } from './setupModel'
import { AddProfileButton, SummaryStrip } from './SummaryStrip'
import { useAgentsData } from './useAgentsData'
import { useNow } from './useNow'
import { useAgentSettings, willStopNotice } from '../settings/useAgentSettings'

// Sign-in needs a terminal; it loads when a profile is added or signed in again.
const AddProfileDialog = lazy(() => import('./AddProfileDialog').then(module => ({ default: module.AddProfileDialog })))

const TAB_LABEL: Record<AgentsTab, string> = { overview: 'Overview', usage: 'Usage', sessions: 'Sessions', defaults: 'Defaults' }
const tabTrigger = 'bonsai-focus relative h-full text-[12px] text-muted transition-colors hover:text-text data-[state=active]:font-medium data-[state=active]:text-text data-[state=active]:after:absolute data-[state=active]:after:inset-x-0 data-[state=active]:after:-bottom-px data-[state=active]:after:h-px data-[state=active]:after:bg-accent'

export interface AgentsPageProps {
  /** Selected profile ID from the URL; the first profile when missing or unknown. */
  profileId?: string
  tab?: AgentsTab
  /** Selects a profile, on `tab` when given. */
  onSelect: (accountId: string, tab?: AgentsTab) => void
  onTabChange: (tab: AgentsTab) => void
  /** Open "Sign in again" for the selected profile (a link from elsewhere asked for it). */
  signIn?: boolean
  /** The sign-in request was taken; the URL drops it. */
  onSignInHandled?: () => void
  /** Called after a session's agent is selected, to bring the canvas into view. */
  onShowOnCanvas: () => void
}

export function AgentsPage({ profileId, tab = 'overview', onSelect, onTabChange, onShowOnCanvas, signIn = false, onSignInHandled }: AgentsPageProps) {
  const data = useAgentsData()
  const now = useNow()
  const agents = useBonsaiStore(state => state.agents)
  const projects = useBonsaiStore(state => state.projects)
  const worktrees = useBonsaiStore(state => state.worktrees)
  const { providers, accounts } = data
  // Unsaved defaults block a tab or profile switch until the user decides.
  const [dirty, setDirty] = useState(false)
  const [blockedSwitch, setBlockedSwitch] = useState<(() => void) | null>(null)
  const [removing, setRemoving] = useState<AgentAccount | null>(null)
  const guarded = useCallback((go: () => void) => { if (dirty) setBlockedSwitch(() => go); else go() }, [dirty])
  const closeRemove = useCallback(() => setRemoving(null), [])
  // A sign-in still running from before a reload is reattached.
  const [setup, setSetup] = useState<SetupTarget | null>(() => { const job = recallSetupJob(); return job ? { kind: 'resume', jobId: job.id } : null })
  const closeSetup = useCallback(() => setSetup(null), [])
  const { applyAccount } = data
  const signedIn = useCallback((account: AgentAccount) => { applyAccount(account); onSelect(account.id) }, [applyAccount, onSelect])
  const openStartAgentDialog = useBonsaiStore(state => state.openStartAgentDialog)
  const { settings: agentSettings } = useAgentSettings()

  const groups = useMemo(() => groupProfiles(providers ?? [], accounts ?? []), [providers, accounts])
  const sessionCounts = useMemo(() => activeSessionCounts(agents), [agents])
  const running = useMemo(() => agents.filter(agent => isLiveAgent(agent) && isActiveSession(agent.lifecycleState)).length, [agents])
  const summary = useMemo(() => summarizeFleet(accounts ?? [], data.usage, running, now), [accounts, data.usage, running, now])
  const providerSummaries = useMemo(() => summarizeProviders(groups, data.usage, sessionCounts, now), [groups, data.usage, sessionCounts, now])
  const usageAt = useMemo(() => usageReadAt(data.usage), [data.usage])
  const selected = accounts?.find(account => account.id === profileId) ?? groups.flatMap(group => group.accounts)[0]

  useEffect(() => {
    if (!signIn || !accounts) return
    if (selected && !setup) setSetup({ kind: 'login', account: selected })
    onSignInHandled?.()
  }, [signIn, accounts, selected, setup, onSignInHandled])

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

  const capability = providers.find(item => item.id === selected?.provider)
  const sessions = selected ? profileSessions(agents, selected.id, projects, worktrees) : []
  const activeSessions = selected ? sessionCounts.get(selected.id) ?? 0 : 0
  const auth = selected ? authState(selected) : undefined
  const siblings = selected ? accounts.filter(account => account.provider === selected.provider) : []
  const selectProfile = (accountId: string) => { if (accountId !== selected?.id) guarded(() => onSelect(accountId)) }
  const changeTab = (next: AgentsTab) => { if (next !== tab) guarded(() => onTabChange(next)) }
  const removed = (account: AgentAccount) => {
    data.dropAccount(account.id)
    // The profile list falls back to the first profile; keep the URL on a profile that exists.
    const next = siblings.find(item => item.id !== account.id) ?? accounts.find(item => item.id !== account.id)
    onSelect(next?.id ?? '')
  }

  return (
    <div className="flex h-full min-h-0 flex-col gap-3">
      <SummaryStrip
        summary={summary}
        providers={providerSummaries}
        usageLoaded={data.usageLoaded}
        usageAt={usageAt}
        refreshing={data.refreshing.length > 0}
        now={now}
        onAddProfile={() => setSetup({ kind: 'create' })}
        onRefresh={() => void data.refreshUsage()}
      />
      {data.error && <div role="status" className="island shrink-0 px-3 py-2 text-[11px] text-warn">Could not refresh agents: {data.error}</div>}
      {agentSettings && !agentSettings.keep_running && running > 0 && (
        <div role="note" aria-label="Agents stop with Bonsai web" className="island shrink-0 px-3 py-2 text-[11px] text-warn">
          {willStopNotice(running)} To keep them, turn on Keep agents running in Settings → Agents.
        </div>
      )}
      <div className="flex min-h-0 flex-1 flex-col gap-3 lg:flex-row">
        <aside aria-label="Providers and profiles" className="island flex max-h-[42%] min-h-0 shrink-0 flex-col overflow-y-auto lg:max-h-none lg:w-[320px]">
          {groups.map(group => (
            <ProviderGroup key={group.id} group={group} selectedId={selected?.id} sessionCounts={sessionCounts} usage={data.usage} onSelect={selectProfile} />
          ))}
        </aside>

        <section aria-label="Profile details" className="island flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
          {!selected || !auth ? (
            <div className="space-y-4 overflow-auto p-6">
              <h2 className="text-[14px] font-semibold text-text">{providers.some(item => item.available) ? 'No profiles yet' : 'No agent provider is installed'}</h2>
              <p className="max-w-[56ch] text-[12px] text-muted">
                {providers.some(item => item.available)
                  ? 'A profile holds one sign-in of a provider. Add one and sign in right here.'
                  : 'Install a provider CLI, then add a profile for it.'}
              </p>
              {providers.some(item => item.available)
                ? <AddProfileButton onClick={() => setSetup({ kind: 'create' })} />
                : (
                  <ul aria-label="Install hints" className="max-w-[460px] space-y-2">
                    {providers.map(provider => (
                      <li key={provider.id} className="space-y-1">
                        <div className="flex items-center gap-2 text-[12px] font-medium text-text"><ProviderLogo provider={provider.id} size={14} className="shrink-0" />{provider.label}</div>
                        <p className="text-[11px] text-muted-2">{provider.unavailable_reason?.message || 'Not available'}</p>
                        {provider.install_hint && <p className="text-[11px] text-muted-2"><code className="select-all font-mono">{provider.install_hint}</code></p>}
                      </li>
                    ))}
                  </ul>
                )}
            </div>
          ) : (
            <Tabs.Root value={tab} onValueChange={value => changeTab(value as AgentsTab)} className="flex min-h-0 flex-1 flex-col">
              <header className="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-1 border-b border-border-subtle px-4 pt-3">
                <ProviderLogo provider={selected.provider} size={16} className="shrink-0 text-text" />
                <ProfileName key={selected.id} account={selected} siblings={siblings} onRenamed={data.applyAccount} />
                <span className="flex items-center gap-1.5 font-mono text-[10px] text-muted">
                  <span className={'h-1.5 w-1.5 rounded-full ' + AUTH_DOT[auth.tone]} aria-hidden="true" />{auth.label}
                </span>
                {identityLine(selected) && <span className="min-w-0 truncate font-mono text-[10px] text-muted-2">{identityLine(selected)}</span>}
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
                  <ProfileOverview account={selected} capability={capability} onRemove={() => setRemoving(selected)} onSignIn={() => setSetup({ kind: 'login', account: selected })} />
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
                  <ProfileSessions sessions={sessions} fields={capability?.launch_fields ?? []} now={now} onShowOnCanvas={onShowOnCanvas} />
                </Tabs.Content>
                <Tabs.Content value="defaults" className="outline-none">
                  <ProfileDefaults key={selected.id} account={selected} capability={capability} onSaved={data.applyAccount} onDirtyChange={setDirty} />
                </Tabs.Content>
              </div>
            </Tabs.Root>
          )}
        </section>
      </div>
      {blockedSwitch && (
        <DiscardChangesDialog
          onKeep={() => setBlockedSwitch(null)}
          onDiscard={() => { const go = blockedSwitch; setBlockedSwitch(null); setDirty(false); go() }}
        />
      )}
      {setup && (
        <Suspense fallback={null}>
          <AddProfileDialog
            target={setup}
            providers={providers}
            accounts={accounts}
            onClose={closeSetup}
            onSignedIn={signedIn}
            onStartAgent={accountId => openStartAgentDialog('', accountId)}
            onSetDefaults={accountId => onSelect(accountId, 'defaults')}
          />
        </Suspense>
      )}
      {removing && (
        <RemoveProfileDialog
          account={removing}
          providerLabel={providers.find(item => item.id === removing.provider)?.label ?? removing.provider}
          onClose={closeRemove}
          onRemoved={() => removed(removing)}
        />
      )}
    </div>
  )
}
