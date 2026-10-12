export const AGENT_TABS = ['overview', 'usage', 'sessions', 'defaults'] as const
export type AgentsTab = typeof AGENT_TABS[number]

export interface AgentsSearch {
  profile?: string
  tab?: AgentsTab
  /** Opens "Sign in again" for the profile once; dropped from the URL after. */
  signin?: boolean
}

/** `/agents?profile=<id>&tab=usage`: unknown tabs and empty values are dropped. */
export const validateAgentsSearch = (search: Record<string, unknown>): AgentsSearch => ({
  profile: typeof search.profile === 'string' && search.profile ? search.profile : undefined,
  tab: AGENT_TABS.find(tab => tab === search.tab),
  signin: search.signin === true || search.signin === 'true' ? true : undefined,
})
