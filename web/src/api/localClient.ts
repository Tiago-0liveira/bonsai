export const LOCAL_API_HTTP = 'http://127.0.0.1:7001'
export const LOCAL_API_WS = 'ws://127.0.0.1:7001/events'
export const LOCAL_API_PROTOCOL_VERSION = 1

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
  if (window.location.protocol === 'https:' || window.location.hostname === 'localhost' || window.location.hostname === '127.0.0.1') return true
  return window.isSecureContext
}

async function localNetworkPermission(): Promise<PermissionState | 'unknown'> {
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
    const request: RequestInit & { targetAddressSpace: 'loopback' } = {
      ...init,
      signal: controller.signal,
      mode: 'cors',
      credentials: 'omit',
      cache: 'no-store',
      targetAddressSpace: 'loopback',
    }
    return await fetch(url, request)
  } finally {
    window.clearTimeout(timer)
  }
}

function classifyProbeFailure(permission: PermissionState | 'unknown', error?: unknown) {
  if (permission === 'denied') {
    publish({
      status: 'permission-denied',
      message: 'This browser denied access to devices on your local network. Allow local network access for app.bonsai.dev, then try again.',
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
      ? 'Bonsai did not respond on 127.0.0.1:7001.'
      : 'Bonsai is not reachable on this computer. Start it with `bonsai serve`, then try again. If Bonsai is already running, check whether this browser has restricted Local Network Access for app.bonsai.dev.',
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
  publish({ status: 'requesting-permission', message: 'Requesting access to Bonsai on this computer…' })
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
    const request: RequestInit & { targetAddressSpace: 'loopback' } = {
      ...init,
      headers,
      mode: 'cors',
      credentials: 'omit',
      cache: 'no-store',
      targetAddressSpace: 'loopback',
    }
    response = await fetch(`${LOCAL_API_HTTP}${path}`, request)
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

export async function openLocalEvents(): Promise<WebSocket> {
  if (snapshot.status !== 'connected') throw new Error('Connect to local Bonsai before opening local events.')
  const value = await currentSession()
  return new Promise((resolve, reject) => {
    const socket = new WebSocket(LOCAL_API_WS)
    const fail = () => reject(new Error('Local Bonsai event socket failed to connect'))
    socket.addEventListener('error', fail, { once: true })
    socket.addEventListener('open', () => {
      socket.removeEventListener('error', fail)
      socket.send(JSON.stringify({ type: 'authenticate', token: value.token }))
      resolve(socket)
    }, { once: true })
  })
}

export function __resetLocalClientForTests() {
  session = undefined
  sessionRequest = undefined
  snapshot = { status: 'not-attempted' }
  listeners.clear()
}
