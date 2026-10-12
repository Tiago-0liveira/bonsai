import { loopbackOrigin, runtimeMeta } from './runtimeConfig'

const contributorBuild = import.meta.env?.DEV || import.meta.env?.MODE === 'test' || import.meta.env?.MODE === 'e2e'
const configuredLocalOrigin = contributorBuild
  ? loopbackOrigin(runtimeMeta('bonsai-local-api-origin') || import.meta.env?.VITE_BONSAI_LOCAL_API_ORIGIN)
  : undefined
export const LOCAL_API_HTTP = configuredLocalOrigin || (typeof window !== 'undefined' ? loopbackOrigin(window.location.origin) : undefined) || ''
export const LOCAL_API_SAME_ORIGIN = typeof window !== 'undefined' && LOCAL_API_HTTP === window.location.origin
export const LOCAL_ENTRY = Boolean(LOCAL_API_HTTP)
export const LOCAL_API_WS = LOCAL_API_HTTP.replace(/^http:/, 'ws:').replace(/^https:/, 'wss:') + '/events'
export const LOCAL_API_PROTOCOL_VERSION = 3

export type LocalConnectionStatus =
  | 'not-attempted'
  | 'bonsai-not-running'
  | 'version-incompatible'
  | 'unsupported-browser'
  | 'connected'
  | 'connection-lost'

export interface LocalConnectionSnapshot {
  status: LocalConnectionStatus
  version?: string
  apiVersion?: number
  message?: string
}

interface LocalSession {
  token: string
  expires_at: string
}

interface VersionResponse {
  version?: string
  api_version?: number
}

type Listener = () => void

let session: LocalSession | undefined
let sessionRequest: Promise<LocalSession> | undefined
let snapshot: LocalConnectionSnapshot = { status: 'not-attempted' }
const listeners = new Set<Listener>()

function publish(next: LocalConnectionSnapshot) {
  snapshot = next
  for (const listener of listeners) listener()
}

export function subscribeLocalConnection(listener: Listener) {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

export function getLocalConnectionSnapshot() {
  return snapshot
}

export function invalidateLocalSession() {
  session = undefined
}

export function resetLocalConnection() {
  invalidateLocalSession()
  sessionRequest = undefined
  publish({ status: 'not-attempted' })
}

export function markLocalConnectionLost(message = 'The connection to Bonsai on this computer was lost.') {
  invalidateLocalSession()
  publish({ status: 'connection-lost', version: snapshot.version, apiVersion: snapshot.apiVersion, message })
}

function sessionUsable(value: LocalSession | undefined): value is LocalSession {
  if (!value?.token) return false
  const expires = Date.parse(value.expires_at)
  return Number.isFinite(expires) && expires - Date.now() > 5_000
}

function browserCanAttemptLoopback() {
  return typeof window !== 'undefined' && Boolean(LOCAL_API_HTTP) && Boolean(loopbackOrigin(window.location.origin))
}

async function fetchWithTimeout(url: string, init: RequestInit = {}, timeout = 3_500) {
  const controller = new AbortController()
  const timer = window.setTimeout(() => controller.abort(), timeout)
  try {
    return await fetch(url, {
      ...init,
      signal: controller.signal,
      mode: 'cors',
      credentials: 'omit',
      cache: 'no-store',
    })
  } finally {
    window.clearTimeout(timer)
  }
}

function classifyProbeFailure(error?: unknown) {
  publish({
    status: 'bonsai-not-running',
    message: error instanceof DOMException && error.name === 'AbortError'
      ? `Bonsai did not respond at ${LOCAL_API_HTTP}.`
      : 'Bonsai stopped responding on this computer. Start it again with `bonsai web`, then try again.',
  })
}

async function createSession(): Promise<LocalSession> {
  const response = await fetchWithTimeout(`${LOCAL_API_HTTP}/api/session`, { method: 'POST' })
  if (!response.ok) throw new Error(`Local Bonsai session failed (${response.status})`)
  const value = await response.json() as LocalSession
  if (!sessionUsable(value)) throw new Error('Local Bonsai session response was invalid')
  session = value
  return value
}

async function currentSession(): Promise<LocalSession> {
  if (sessionUsable(session)) return session
  session = undefined
  sessionRequest ??= createSession().finally(() => { sessionRequest = undefined })
  return sessionRequest
}

export async function connectLocalBonsai(): Promise<LocalConnectionSnapshot> {
  if (!browserCanAttemptLoopback()) {
    const next: LocalConnectionSnapshot = {
      status: 'unsupported-browser',
      message: 'This browser context cannot reach Bonsai on localhost. Open Bonsai from a supported secure browser context.',
    }
    publish(next)
    return next
  }

  invalidateLocalSession()
  let health: Response
  try {
    health = await fetchWithTimeout(`${LOCAL_API_HTTP}/health`)
  } catch (error) {
    classifyProbeFailure(error)
    return snapshot
  }
  if (!health.ok) {
    classifyProbeFailure()
    return snapshot
  }

  let versionResponse: Response
  try {
    versionResponse = await fetchWithTimeout(`${LOCAL_API_HTTP}/version`)
  } catch (error) {
    classifyProbeFailure(error)
    return snapshot
  }
  if (!versionResponse.ok) {
    classifyProbeFailure()
    return snapshot
  }

  const version = await versionResponse.json() as VersionResponse
  if (version.api_version !== LOCAL_API_PROTOCOL_VERSION) {
    const next: LocalConnectionSnapshot = {
      status: 'version-incompatible',
      version: version.version,
      apiVersion: version.api_version,
      message: `This Bonsai local API uses protocol ${version.api_version ?? 'unknown'}; the web client expects protocol ${LOCAL_API_PROTOCOL_VERSION}. Update Bonsai and reload the app.`,
    }
    publish(next)
    return next
  }

  try {
    await currentSession()
  } catch (error) {
    classifyProbeFailure(error)
    return snapshot
  }

  const next: LocalConnectionSnapshot = {
    status: 'connected',
    version: version.version,
    apiVersion: version.api_version,
  }
  publish(next)
  return next
}

export async function localFetch(path: string, init: RequestInit = {}, retry = true): Promise<Response> {
  if (snapshot.status !== 'connected') throw new Error('Connect to local Bonsai before making privileged requests.')
  const value = await currentSession()
  const headers = new Headers(init.headers)
  headers.set('X-Bonsai-Session', value.token)

  let response: Response
  try {
    response = await fetch(`${LOCAL_API_HTTP}${path}`, {
      ...init,
      headers,
      mode: 'cors',
      credentials: 'omit',
      cache: 'no-store',
    })
  } catch (error) {
    markLocalConnectionLost(error instanceof Error ? error.message : undefined)
    throw error
  }
  if (response.status === 401 && retry) {
    invalidateLocalSession()
    return localFetch(path, init, false)
  }
  return response
}

export interface LocalEvent {
  type?: string
  epoch?: string
  project_id?: string
  component?: string
  sequence?: number
  projects?: unknown[]
  snapshot?: unknown
  /** agents_changed only: counts account, setup and session changes made through the API. */
  revision?: number
}

type AgentsChangedListener = (revision: number) => void
const agentsChangedListeners = new Set<AgentsChangedListener>()

/** Calls listener when the API reports a profile, setup or session change. */
export function subscribeAgentsChanged(listener: AgentsChangedListener) {
  agentsChangedListeners.add(listener)
  return () => { agentsChangedListeners.delete(listener) }
}

export interface LocalEventConnection {
  socket: WebSocket
  epoch: string
}

function connectEventSocket(value: LocalSession, onEvent: (event: LocalEvent) => void, activeProjectId?: string): Promise<LocalEventConnection> {
  return new Promise((resolve, reject) => {
    const socket = new WebSocket(LOCAL_API_WS)
    let settled = false

    const fail = () => {
      if (!settled) {
        settled = true
        reject(new Error('Local Bonsai event socket failed to connect'))
      }
    }
    const closed = (event: Event) => {
      if (!settled) {
        settled = true
        // A policy close before ready rejects this capability, as after an API
        // restart. Drop it so the next attempt asks for a fresh session.
        if ((event as CloseEvent).code === 1008) invalidateLocalSession()
        reject(new Error('Local Bonsai event socket closed before authentication completed'))
      }
    }
    socket.addEventListener('error', fail, { once: true })
    socket.addEventListener('close', closed, { once: true })
    socket.addEventListener('message', event => {
      let data: LocalEvent
      try {
        data = JSON.parse(String(event.data)) as LocalEvent
      } catch {
        return
      }
      if (data.type === 'agents_changed') {
        for (const listener of agentsChangedListeners) listener(data.revision ?? 0)
      }
      if (data.type === 'ready') {
        if (!settled) {
          if (!data.epoch) {
            settled = true
            reject(new Error('Local Bonsai ready event did not include a backend epoch'))
            socket.close()
            return
          }
          settled = true
          socket.removeEventListener('error', fail)
          socket.removeEventListener('close', closed)
          resolve({ socket, epoch: data.epoch })
        }
        onEvent(data)
        return
      }
      onEvent(data)
    })
    socket.addEventListener('open', () => {
      // active_project lets the backend refresh the project in view first.
      socket.send(JSON.stringify({
        type: 'authenticate',
        token: value.token,
        ...(activeProjectId ? { active_project: activeProjectId } : {}),
      }))
    }, { once: true })
  })
}

// sendEventFocus tells the backend which project this page has in view ('' when
// none, such as a hidden tab), so that project's GitHub data is polled often and
// the rest rarely. Servers that predate it ignore the frame.
export function sendEventFocus(socket: WebSocket, projectId: string): boolean {
  if (socket.readyState !== 1) return false
  socket.send(JSON.stringify({ type: 'focus', active_project: projectId }))
  return true
}

export function openLocalEvents(onEvent: (event: LocalEvent) => void = () => {}, activeProjectId?: string): Promise<LocalEventConnection> {
  if (snapshot.status !== 'connected') return Promise.reject(new Error('Connect to local Bonsai before opening local events.'))
  if (sessionUsable(session)) return connectEventSocket(session, onEvent, activeProjectId)
  return currentSession().then(value => connectEventSocket(value, onEvent, activeProjectId))
}

export function __resetLocalClientForTests() {
  session = undefined
  sessionRequest = undefined
  snapshot = { status: 'not-attempted' }
  listeners.clear()
  agentsChangedListeners.clear()
}

// Capabilities travel only in the first frame, never in a terminal URL.
export async function terminalCapability(): Promise<string> {
  return (await currentSession()).token
}
