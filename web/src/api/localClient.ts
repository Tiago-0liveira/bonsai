import { runtimeEntry, runtimeMeta } from './runtimeConfig'

const buildLocalOrigin = (import.meta as ImportMeta & { env?: Record<string, string | undefined> }).env?.VITE_BONSAI_LOCAL_API_ORIGIN
const configuredLocalOrigin = (runtimeMeta('bonsai-local-api-origin') || buildLocalOrigin)?.replace(/\/$/, '')
export const LOCAL_API_HTTP = configuredLocalOrigin || 'http://127.0.0.1:7001'
// True when the local API served this page itself (`bonsai web`). Requests are
// then same-origin: no CORS, no Local Network Access permission, no prompt.
export const LOCAL_API_SAME_ORIGIN = typeof window !== 'undefined' && LOCAL_API_HTTP === window.location.origin
export const LOCAL_ENTRY = LOCAL_API_SAME_ORIGIN && runtimeEntry() === 'local'
export const LOCAL_API_WS = LOCAL_API_HTTP.replace(/^http:/, 'ws:').replace(/^https:/, 'wss:') + '/events'
export const LOCAL_API_PROTOCOL_VERSION = 3

export type LocalConnectionStatus =
  | 'not-attempted'
  | 'requesting-permission'
  | 'bonsai-not-running'
  | 'permission-denied'
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
  if (typeof window === 'undefined') return false
  if (LOCAL_API_SAME_ORIGIN) return true
  if (window.location.protocol === 'https:' || window.location.hostname === 'localhost' || window.location.hostname === '127.0.0.1') return true
  return window.isSecureContext
}

async function localNetworkPermission(): Promise<PermissionState | 'unknown'> {
  // Same-origin requests are not Local Network Access requests; never probe.
  if (LOCAL_API_SAME_ORIGIN) return 'unknown'
  if (typeof navigator === 'undefined' || !navigator.permissions?.query) return 'unknown'
  const query = navigator.permissions.query.bind(navigator.permissions) as unknown as (
    descriptor: { name: string },
  ) => Promise<PermissionStatus>
  for (const name of ['loopback-network', 'local-network-access']) {
    try {
      return (await query({ name })).state
    } catch {
      // Newer browsers use loopback-network; older implementations expose the alias.
    }
  }
  return 'unknown'
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
      ...loopbackTarget(),
    })
  } finally {
    window.clearTimeout(timer)
  }
}

// Cross-origin pages declare the loopback target for Local Network Access.
// A page served by the local API is already on loopback and declares nothing.
function loopbackTarget(): { targetAddressSpace?: 'loopback' } {
  return LOCAL_API_SAME_ORIGIN ? {} : { targetAddressSpace: 'loopback' }
}

function classifyProbeFailure(permission: PermissionState | 'unknown', error?: unknown) {
  if (permission === 'denied') {
    publish({
      status: 'permission-denied',
      message: 'This browser denied access to devices on your local network. Allow local network access for this Bonsai site, then try again.',
    })
    return
  }
  if (!browserCanAttemptLoopback()) {
    publish({
      status: 'unsupported-browser',
      message: 'This browser context cannot reach Bonsai on localhost. Open Bonsai from a supported secure browser context.',
    })
    return
  }
  publish({
    status: 'bonsai-not-running',
    message: error instanceof DOMException && error.name === 'AbortError'
      ? `Bonsai did not respond at ${LOCAL_API_HTTP}.`
      : LOCAL_API_SAME_ORIGIN
        ? 'Bonsai stopped responding on this computer. Start it again with `bonsai web`, then try again.'
        : 'Bonsai is not reachable on this computer. Start it with `bonsai web`, then try again. If Bonsai is already running, check whether this browser has restricted Local Network Access for this frontend origin.',
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
  if (!LOCAL_API_SAME_ORIGIN) publish({ status: 'requesting-permission', message: 'Requesting access to Bonsai on this computer…' })
  const permission = await localNetworkPermission()
  if (permission === 'denied') {
    classifyProbeFailure(permission)
    return snapshot
  }

  let health: Response
  try {
    health = await fetchWithTimeout(`${LOCAL_API_HTTP}/health`)
  } catch (error) {
    classifyProbeFailure(await localNetworkPermission(), error)
    return snapshot
  }
  if (!health.ok) {
    classifyProbeFailure(permission)
    return snapshot
  }

  let versionResponse: Response
  try {
    versionResponse = await fetchWithTimeout(`${LOCAL_API_HTTP}/version`)
  } catch (error) {
    classifyProbeFailure(await localNetworkPermission(), error)
    return snapshot
  }
  if (!versionResponse.ok) {
    classifyProbeFailure(permission)
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
    classifyProbeFailure(await localNetworkPermission(), error)
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
      ...loopbackTarget(),
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
    const closed = () => {
      if (!settled) {
        settled = true
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
}

// Capabilities travel only in the first frame, never in a terminal URL.
export async function terminalCapability(): Promise<string> {
  return (await currentSession()).token
}
