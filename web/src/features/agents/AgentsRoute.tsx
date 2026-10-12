import { useNavigate, useSearch } from '@tanstack/react-router'
import { AgentsPage } from './AgentsPage'
import type { AgentsSearch } from './agentsSearch'

/** Keeps the selected profile and tab in the URL so a link restores them. */
export function AgentsRoute() {
  const { profile, tab } = useSearch({ strict: false }) as AgentsSearch
  const navigate = useNavigate()
  return (
    <AgentsPage
      profileId={profile}
      tab={tab}
      onSelect={accountId => void navigate({ to: '.', search: { profile: accountId, tab }, replace: true })}
      onTabChange={next => void navigate({ to: '.', search: { profile, tab: next }, replace: true })}
      onShowOnCanvas={() => void navigate({ to: '/' })}
    />
  )
}
