import { LOCAL_API_HTTP, terminalCapability, invalidateLocalSession } from './localClient'
import type { AgentSummary } from './agents'

export type TerminalConnection = 'connecting' | 'connected' | 'read-only' | 'disconnected'
interface TerminalCallbacks {
  connection: (state: TerminalConnection) => void
  output: (bytes: Uint8Array) => void
  reset: (gap: boolean) => void
  status: (summary: AgentSummary) => void
  ready: () => void
  error: (message: string) => void
}
type Frame =
  | { type: 'ready'; version: number; generation: string; writer: boolean }
  | { type: 'output'; offset: number; data: string }
  | { type: 'gap'; offset: number }
  | { type: 'status'; status: AgentSummary }
  | { type: 'error'; message: string }

// A controller belongs to one terminal display. Reconnect keeps its cursor;
// reopening a disposed display replays from zero into a fresh xterm instance.
export function connectAgentTerminal(project: string, id: string, callbacks: TerminalCallbacks) {
  let disposed = false, writer = false, connected = false
  let socket: WebSocket | undefined
  let retry: ReturnType<typeof setTimeout> | undefined
  let deadline: ReturnType<typeof setTimeout> | undefined
  let offset = 0, generation = ''
  const send = (message: object) => {
    if (socket?.readyState === WebSocket.OPEN && connected && writer) socket.send(JSON.stringify(message))
  }
  const reconnect = () => {
    if (disposed) return
    callbacks.connection('disconnected')
    invalidateLocalSession()
    retry = setTimeout(() => void connect(), 2000)
  }
  const connect = async () => {
    callbacks.connection('connecting')
    try {
      const token = await terminalCapability()
      if (disposed) return
      const ws = new WebSocket(`${LOCAL_API_HTTP.replace(/^http/, 'ws')}/api/projects/${encodeURIComponent(project)}/agents/${encodeURIComponent(id)}/terminal`)
      socket = ws
      deadline = setTimeout(() => ws.close(), 8000)
      ws.onopen = () => {
        ws.send(JSON.stringify({ type: 'authenticate', token }))
        ws.send(JSON.stringify({ type: 'attach', offset, generation }))
      }
      ws.onmessage = event => {
        if (disposed) return
        try {
          const frame = JSON.parse(String(event.data)) as Frame
          if (frame.type === 'ready') {
            if (frame.version !== 1) { callbacks.error('Unsupported terminal protocol. Update Bonsai.'); ws.close(); return }
            clearTimeout(deadline)
            if (generation && generation !== frame.generation) { callbacks.reset(false); offset = 0 }
            generation = frame.generation
            writer = frame.writer
            connected = true
            callbacks.connection(writer ? 'connected' : 'read-only')
            callbacks.ready()
          } else if (frame.type === 'output') {
            const bytes = Uint8Array.from(atob(frame.data), c => c.charCodeAt(0))
            if (frame.offset !== offset) { callbacks.error('Terminal stream lost synchronization.'); ws.close(); return }
            callbacks.output(bytes)
            offset = frame.offset + bytes.length
          } else if (frame.type === 'gap') {
            callbacks.reset(true)
            offset = frame.offset
          } else if (frame.type === 'status') {
            callbacks.status(frame.status)
          } else if (frame.type === 'error') {
            callbacks.error(frame.message)
          }
        } catch { callbacks.error('Invalid terminal frame.'); ws.close() }
      }
      ws.onclose = () => { clearTimeout(deadline); connected = false; writer = false; reconnect() }
    } catch { reconnect() }
  }
  void connect()
  return {
    input(data: string) {
      const bytes = new TextEncoder().encode(data)
      // Keystrokes are dropped while disconnected; never queue them for replay.
      for (let i = 0; i < bytes.length; i += 16384) send({ type: 'input', data: btoa(String.fromCharCode(...bytes.slice(i, i + 16384))) })
    },
    resize(cols: number, rows: number) { send({ type: 'resize', cols: Math.min(500, Math.max(1, cols)), rows: Math.min(500, Math.max(1, rows)) }) },
    dispose() { disposed = true; clearTimeout(retry); clearTimeout(deadline); socket?.close() },
  }
}
