import { useNavigate, useSearch } from '@tanstack/react-router'
import { useCallback } from 'react'
import { AgentsPage } from './AgentsPage'
import type { AgentsSearch } from './agentsSearch'

/** Keeps the selected profile and tab in the URL so a link restores them. */
export function AgentsRoute() {
  const { profile, tab, signin } = useSearch({ strict: false }) as AgentsSearch
  const navigate = useNavigate()
  const signInHandled = useCallback(() => void navigate({ to: '.', search: { profile, tab }, replace: true }), [navigate, profile, tab])
  return (
    <AgentsPage
      profileId={profile}
      tab={tab}
      onSelect={(accountId, next) => void navigate({ to: '.', search: { profile: accountId, tab: next ?? tab }, replace: true })}
      onTabChange={next => void navigate({ to: '.', search: { profile, tab: next }, replace: true })}
      onShowOnCanvas={() => void navigate({ to: '/' })}
      signIn={signin}
      onSignInHandled={signInHandled}
    />
  )
}
