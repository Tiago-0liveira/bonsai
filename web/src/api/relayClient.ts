import { useBonsaiStore } from '../stores/bonsai'
import { projectForGitHubRepository, requestProjectRefresh, report } from './git'
import { loopbackOrigin, runtimeMeta } from './runtimeConfig'

// SSE exists only for the explicit loopback contributor harness.
const contributorBuild = import.meta.env?.DEV || import.meta.env?.MODE === 'test' || import.meta.env?.MODE === 'e2e'
export const RELAY_HTTP_ORIGIN = contributorBuild
  ? loopbackOrigin(runtimeMeta('bonsai-relay-origin') ?? import.meta.env?.VITE_BONSAI_RELAY_ORIGIN) || ''
  : ''
export const RELAY_ENABLED = Boolean(RELAY_HTTP_ORIGIN)

export type RelayConnectionStatus = 'disconnected' | 'connecting' | 'connected' | 'offline'

export interface RelayConnectionSnapshot {
  status: RelayConnectionStatus
  message?: string
}

type Listener = () => void
let snapshot: RelayConnectionSnapshot = { status: 'disconnected' }
const listeners = new Set<Listener>()

function publish(next: RelayConnectionSnapshot) {
  snapshot = next
  for (const listener of listeners) listener()
}

export function subscribeRelayConnection(listener: Listener) {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

export function getRelayConnectionSnapshot() {
  return snapshot
}

export function startRelayInvalidation() {
  if (!RELAY_ENABLED) return () => {}
  let closed = false
  let source: EventSource | undefined
  let reconnectTimer: ReturnType<typeof setTimeout> | undefined
  const refreshTimers = new Map<string, ReturnType<typeof setTimeout>>()

  const debounceRefresh = (projectId: string) => {
    if (refreshTimers.has(projectId)) return
    refreshTimers.set(projectId, setTimeout(() => {
      refreshTimers.delete(projectId)
      void requestProjectRefresh(projectId, 'provider').catch(report)
    }, 250))
  }

  const scheduleReconnect = (connect: () => void, delay = 5_000) => {
    if (closed || reconnectTimer) return
    reconnectTimer = setTimeout(() => {
      reconnectTimer = undefined
      connect()
    }, delay)
  }

  const connect = async () => {
    if (closed) return
    publish({ status: 'connecting' })
    try {
      source?.close()
      source = new EventSource(`${RELAY_HTTP_ORIGIN}/events`, { withCredentials: false })
      source.onopen = () => publish({ status: 'connected' })
      source.onmessage = event => {
        try {
          const payload = JSON.parse(event.data) as { repository_id?: number }
          if (!payload.repository_id) return
          for (const projectId of projectForGitHubRepository(payload.repository_id)) debounceRefresh(projectId)
        } catch {
          // Relay payloads only invalidate local state; malformed messages are ignored.
        }
      }
      source.addEventListener('reset', () => {
        for (const project of useBonsaiStore.getState().projects) debounceRefresh(project.id)
      })
      source.onerror = () => {
        if (closed) return
        // Keep the EventSource open so the browser reconnects with its
        // Last-Event-ID cursor and the relay can replay missed events.
        publish({ status: 'offline', message: 'GitHub realtime is temporarily unavailable. Local Bonsai remains usable.' })

      }
    } catch {
      if (!closed) {
        publish({ status: 'offline', message: 'GitHub realtime is temporarily unavailable. Local Bonsai remains usable.' })
        scheduleReconnect(connect)
      }
    }
  }

  void connect()
  return () => {
    closed = true
    if (reconnectTimer) clearTimeout(reconnectTimer)
    for (const timer of refreshTimers.values()) clearTimeout(timer)
    refreshTimers.clear()
    source?.close()
    publish({ status: 'disconnected' })
  }
}
