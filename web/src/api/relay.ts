import { useBonsaiStore } from '../stores/bonsai'
import { projectForGitHubRepository, refreshProject, report } from './git'

export const RELAY_HTTP_ORIGIN = 'https://api.bonsai.dev'

let reconnectTimer: ReturnType<typeof setTimeout> | undefined

function scheduleReconnect(connect: () => void, delay = 5_000) {
  if (reconnectTimer) return
  reconnectTimer = setTimeout(() => {
    reconnectTimer = undefined
    connect()
  }, delay)
}

export function relayLoginURL() {
  return `${RELAY_HTTP_ORIGIN}/auth/github`
}

export function startRelayInvalidation() {
  let closed = false
  let source: EventSource | undefined

  const connect = async () => {
    if (closed) return
    try {
      const status = await fetch(`${RELAY_HTTP_ORIGIN}/auth/session`, {
        method: 'GET',
        mode: 'cors',
        credentials: 'include',
        cache: 'no-store',
      })
      if (status.status === 401) {
        scheduleReconnect(connect, 60_000)
        return
      }
      if (!status.ok) throw new Error(`Relay session check failed (${status.status})`)

      source?.close()
      source = new EventSource(`${RELAY_HTTP_ORIGIN}/events`, { withCredentials: true })
      source.onmessage = event => {
        try {
          const payload = JSON.parse(event.data) as { repository_id?: number }
          if (!payload.repository_id) return
          const projectId = projectForGitHubRepository(payload.repository_id)
          if (projectId) void refreshProject(projectId, true).catch(report)
        } catch {
          // Invalid relay messages never mutate local state.
        }
      }
      source.addEventListener('reset', () => {
        for (const project of useBonsaiStore.getState().projects) {
          void refreshProject(project.id, true).catch(report)
        }
      })
      source.onerror = () => {
        source?.close()
        source = undefined
        if (!closed) scheduleReconnect(connect)
      }
    } catch {
      if (!closed) scheduleReconnect(connect)
    }
  }

  void connect()
  return () => {
    closed = true
    if (reconnectTimer) {
      clearTimeout(reconnectTimer)
      reconnectTimer = undefined
    }
    source?.close()
  }
}
