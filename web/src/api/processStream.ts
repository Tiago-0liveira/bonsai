import { LOCAL_API_HTTP, terminalCapability, invalidateLocalSession } from './localClient'
import type { ProcessSummary } from './git'
import { applyProcessSummary } from './processes'

export type ProcessConnection = 'connecting' | 'connected' | 'disconnected'
interface Callbacks {
  connection(state: ProcessConnection): void
  output(bytes: Uint8Array): boolean | void
  gap(): void
  error(message: string): void
}

// Each display owns its cursor. A rejected chunk is replayed after reconnect,
// which bounds rendering queues without making the daemon block the child.
export function connectProcessStream(project: string, id: number, callbacks: Callbacks) {
  let disposed = false, offset = 0, generation = ''
  let socket: WebSocket | undefined
  let retry: ReturnType<typeof setTimeout> | undefined
  let deadline: ReturnType<typeof setTimeout> | undefined
  const reconnect = () => {
    if (disposed) return
    callbacks.connection('disconnected')
    retry = setTimeout(() => void connect(), 2000)
  }
  const connect = async () => {
    if (disposed) return
    callbacks.connection('connecting')
    try {
      const token = await terminalCapability()
      if (disposed) return
      const ws = new WebSocket(`${LOCAL_API_HTTP.replace(/^http/, 'ws')}/api/projects/${encodeURIComponent(project)}/processes/${id}/terminal`)
      socket = ws
      deadline = setTimeout(() => ws.close(), 8000)
      ws.onopen = () => {
        ws.send(JSON.stringify({ type: 'authenticate', token }))
        ws.send(JSON.stringify({ type: 'attach', generation, offset }))
      }
      ws.onmessage = event => {
        if (disposed || socket !== ws || ws.readyState !== WebSocket.OPEN) return
        try {
          const frame = JSON.parse(String(event.data)) as { type: string; version?: number; generation?: string; offset: number; data: string; status: ProcessSummary; message: string }
          if (frame.type === 'ready') {
            if (frame.version !== 1) throw new Error('Unsupported process stream version. Update Bonsai.')
            clearTimeout(deadline)
            generation = frame.generation ?? ''
            callbacks.error('')
            callbacks.connection('connected')
          } else if (frame.type === 'gap') {
            offset = frame.offset
            callbacks.gap()
          } else if (frame.type === 'output') {
            const bytes = Uint8Array.from(atob(frame.data), c => c.charCodeAt(0))
            const start = frame.offset - bytes.length
            if (frame.offset <= offset) return
            if (start > offset) throw new Error('Process output lost synchronization.')
            if (callbacks.output(bytes.subarray(Math.max(0, offset - start))) === false) { ws.close(); return }
            offset = frame.offset
          } else if (frame.type === 'status') {
            applyProcessSummary(frame.status)
          } else if (frame.type === 'error') callbacks.error(frame.message)
        } catch (error) {
          callbacks.error(error instanceof Error ? error.message : 'Invalid process stream frame.')
          ws.close()
        }
      }
      ws.onerror = () => { if (!disposed && socket === ws) callbacks.error('Unable to connect to process output. Retrying…') }
      ws.onclose = () => {
        clearTimeout(deadline)
        if (disposed || socket !== ws) return
        invalidateLocalSession(); reconnect()
      }
    } catch (error) {
      if (disposed) return
      callbacks.error(error instanceof Error ? error.message : 'Unable to connect to process output. Retrying…')
      reconnect()
    }
  }
  void connect()
  return { dispose() { disposed = true; clearTimeout(retry); clearTimeout(deadline); socket?.close() } }
}

// Start attachment as soon as launch returns, before xterm imports or panel sizing.
// The temporary replay queue is capped; rejected bytes remain available on disk.
const sessions = new Map<string, ReturnType<typeof createSession>>()
function createSession(project: string, id: number) {
  let callbacks: Callbacks | undefined
  let connection: ProcessConnection = 'connecting', error = ''
  let queued = 0
  const backlog: Array<Uint8Array | 'gap'> = []
  const controller = connectProcessStream(project, id, {
    connection(value) { connection = value; callbacks?.connection(value) },
    error(value) { error = value; callbacks?.error(value) },
    gap() { if (callbacks) callbacks.gap(); else backlog.push('gap') },
    output(bytes) {
      if (callbacks) return callbacks.output(bytes)
      if (queued + bytes.length > 512 * 1024) return false
      backlog.push(bytes); queued += bytes.length
    },
  })
  return {
    attach(next: Callbacks) {
      callbacks = next
      next.connection(connection)
      if (error) next.error(error)
      for (const item of backlog) { if (item === 'gap') next.gap(); else next.output(item) }
      backlog.length = 0; queued = 0
    },
    dispose: controller.dispose,
  }
}
export function prepareProcessStream(project: string, id: number) {
  const key = `${project}:${id}`
  let session = sessions.get(key)
  if (!session) { session = createSession(project, id); sessions.set(key, session) }
  return session
}
export function disposeProcessStream(project: string, id: number) {
  const key = `${project}:${id}`
  sessions.get(key)?.dispose(); sessions.delete(key)
}

// Only incomplete lifecycle markers are buffered; ordinary output is immediate.
export class ProcessMarkerRenderer {
  private pending = new Uint8Array()
  render(bytes: Uint8Array): Uint8Array {
    const data = new Uint8Array(this.pending.length + bytes.length)
    data.set(this.pending); data.set(bytes, this.pending.length)
    this.pending = new Uint8Array()
    const output: number[] = []
    let i = 0
    while (i < data.length) {
      if (data[i] !== 30) { output.push(data[i++]); continue }
      const end = data.indexOf(10, i)
      if (end < 0 && data.length - i < 16384) { this.pending = data.slice(i); break }
      if (end < 0) { output.push(data[i++]); continue }
      const marker = new TextDecoder().decode(data.subarray(i + 1, end))
      const match = /^(start|exit|restart|stopped)\t-?\d+\t(.*)$/.exec(marker)
      if (match) output.push(...new TextEncoder().encode(`\r\n\x1b[0;36m── ${match[2]} ──\x1b[0m\r\n`))
      else output.push(...data.subarray(i, end + 1))
      i = end + 1
    }
    return Uint8Array.from(output)
  }
  reset() { this.pending = new Uint8Array() }
}
