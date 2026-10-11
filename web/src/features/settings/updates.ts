import { useEffect, useState } from 'react'
import { loadUpdateSettings, type LiveRepositoryUpdates, type UpdateSettings } from '../../api/settings'

export const UPDATE_SETTINGS_REFRESH_MS = 30_000

// useUpdateSettings reads GET /api/settings/updates on mount (the app only
// mounts once connected), when the window regains focus and every 30 s. An
// API without the endpoint (an older bonsai) leaves it undefined.
export function useUpdateSettings() {
  const [settings, setSettings] = useState<UpdateSettings>()
  useEffect(() => {
    let active = true
    const load = () => {
      loadUpdateSettings().then(next => { if (active) setSettings(next) }, () => undefined)
    }
    load()
    const timer = window.setInterval(load, UPDATE_SETTINGS_REFRESH_MS)
    window.addEventListener('focus', load)
    return () => {
      active = false
      window.clearInterval(timer)
      window.removeEventListener('focus', load)
    }
  }, [])
  return settings
}

export function intervalText(seconds: number) {
  if (seconds < 60) return `${seconds} s`
  const minutes = Math.round(seconds / 60)
  return `${minutes} min`
}

export const TUNNEL_NAMES: Record<string, string> = {
  'cloudflared-quick': 'Cloudflare quick tunnel',
  'cloudflared-named': 'Cloudflare tunnel',
  ngrok: 'ngrok',
  tailscale: 'Tailscale Funnel',
  'external-url': 'your own URL',
  custom: 'custom command',
}

export const HOOK_STATE_LABELS: Record<string, string> = {
  live: 'Live',
  waiting_for_ping: "Waiting for GitHub's ping",
  failing: 'Failing',
  needs_admin: 'Needs admin',
  scope_missing: 'gh login cannot manage webhooks',
  pending: 'Setting up',
}

// liveRepositoryFor is the live repository whose remote is the project's.
export function liveRepositoryFor(settings: UpdateSettings | undefined, projectId: string) {
  return settings?.live?.repositories.find(repository => repository.project_ids.includes(projectId))
}

// whyNotLive explains, in one sentence, why a repository is not live.
export function whyNotLive(settings: UpdateSettings, repository: LiveRepositoryUpdates | undefined) {
  const live = settings.live
  if (!live) return ''
  if (!repository) return 'Live updates do not cover this project. Add its repository with bonsai web setup.'
  if (!live.tunnel_up) return `Live updates are paused: ${live.tunnel_error || 'the tunnel is not running'}.`
  const state = HOOK_STATE_LABELS[repository.state] ?? repository.state
  return `Live updates for ${repository.full_name}: ${state.toLowerCase()}${repository.last_error ? ` (${repository.last_error})` : ''}.`
}
