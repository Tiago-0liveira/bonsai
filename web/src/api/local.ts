const LOCAL_API_HTTP = 'http://127.0.0.1:7001'
const LOCAL_API_WS = 'ws://127.0.0.1:7001/events'

interface LocalSession {
  token: string
  expires_at: string
}

let session: LocalSession | undefined
let sessionRequest: Promise<LocalSession> | undefined

function sessionUsable(value: LocalSession | undefined): value is LocalSession {
  if (!value?.token) return false
  const expires = Date.parse(value.expires_at)
  return Number.isFinite(expires) && expires - Date.now() > 5_000
}

async function createSession(): Promise<LocalSession> {
  const response = await fetch(`${LOCAL_API_HTTP}/api/session`, {
    method: 'POST',
    mode: 'cors',
    credentials: 'omit',
    cache: 'no-store',
  })
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

export function invalidateLocalSession() {
  session = undefined
}

export async function localFetch(path: string, init: RequestInit = {}, retry = true): Promise<Response> {
  const value = await currentSession()
  const headers = new Headers(init.headers)
  headers.set('X-Bonsai-Session', value.token)

  const response = await fetch(`${LOCAL_API_HTTP}${path}`, {
    ...init,
    headers,
    mode: 'cors',
    credentials: 'omit',
    cache: 'no-store',
  })
  if (response.status === 401 && retry) {
    invalidateLocalSession()
    return localFetch(path, init, false)
  }
  return response
}

export async function openLocalEvents(): Promise<WebSocket> {
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
