import { useBonsaiStore } from '../stores/bonsai'
import { projectForGitHubRepository, requestProjectRefresh, report } from './git'
import { runtimeMeta } from './runtimeConfig'

// An empty runtime relay origin means the server that delivered this page has
// no relay (the local API while the hosted relay is frozen): no relay UI, no
// relay requests. An absent or placeholder value keeps the build default.
const runtimeRelayOrigin = runtimeMeta('bonsai-relay-origin')
export const RELAY_ENABLED = runtimeRelayOrigin !== ''
const buildRelayOrigin = (import.meta as ImportMeta & { env?: Record<string, string | undefined> }).env?.VITE_BONSAI_RELAY_ORIGIN
const configuredRelayOrigin = (runtimeRelayOrigin || buildRelayOrigin)?.replace(/\/$/, '')
export const RELAY_HTTP_ORIGIN = configuredRelayOrigin || 'https://api.bonsai.dev'
const relayUsesCloudSession = RELAY_HTTP_ORIGIN.startsWith('https://')

export type RelayConnectionStatus = 'disconnected' | 'connecting' | 'connected' | 'authorization-expired' | 'offline'

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

export function relayLoginURL() {
  return `${RELAY_HTTP_ORIGIN}/auth/github`
}

export async function disconnectGitHubRelay() {
  try {
    await fetch(`${RELAY_HTTP_ORIGIN}/auth/logout`, {
      method: 'POST',
      mode: 'cors',
      credentials: 'include',
      cache: 'no-store',
    })
  } finally {
    publish({ status: 'disconnected' })
  }
}

export function startRelayInvalidation() {
  if (!RELAY_ENABLED) return () => {}
  let closed = false
  let source: EventSource | undefined
  let reconnectTimer: ReturnType<typeof setTimeout> | undefined
  let authCheckTimer: ReturnType<typeof setTimeout> | undefined
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
      if (relayUsesCloudSession) {
        const status = await fetch(`${RELAY_HTTP_ORIGIN}/auth/session`, {
          method: 'GET',
          mode: 'cors',
          credentials: 'include',
          cache: 'no-store',
        })
        if (status.status === 401) {
          publish({ status: 'authorization-expired', message: 'Connect GitHub to restore realtime pull request updates.' })
          scheduleReconnect(connect, 60_000)
          return
        }
        if (!status.ok) throw new Error(`Relay session check failed (${status.status})`)
      }

      source?.close()
      source = new EventSource(`${RELAY_HTTP_ORIGIN}/events`, { withCredentials: relayUsesCloudSession })
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
        if (!relayUsesCloudSession || authCheckTimer) return
        authCheckTimer = setTimeout(async () => {
          authCheckTimer = undefined
          if (closed) return
          try {
            const auth = await fetch(`${RELAY_HTTP_ORIGIN}/auth/session`, {
              method: 'GET',
              mode: 'cors',
              credentials: 'include',
              cache: 'no-store',
            })
            if (auth.status === 401) {
              source?.close()
              source = undefined
              publish({ status: 'authorization-expired', message: 'Connect GitHub to restore realtime pull request updates.' })
              scheduleReconnect(connect, 60_000)
            }
          } catch {
            // Native EventSource reconnection continues while the relay is offline.
          }
        }, 1_500)
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
    if (authCheckTimer) clearTimeout(authCheckTimer)
    for (const timer of refreshTimers.values()) clearTimeout(timer)
    refreshTimers.clear()
    source?.close()
    publish({ status: 'disconnected' })
  }
}
