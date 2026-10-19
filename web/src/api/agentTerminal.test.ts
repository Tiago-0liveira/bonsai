import { afterEach, expect, it, vi } from 'vitest'
import { agentTerminalPath, connectTerminal, setupTerminalPath } from './agentTerminal'
vi.mock('./localClient', () => ({ LOCAL_API_HTTP: 'http://127.0.0.1:7001', terminalCapability: vi.fn().mockResolvedValue('capability'), invalidateLocalSession: vi.fn() }))
class Socket {
  static OPEN = 1
  static instances: Socket[] = []
  readyState = 1
  sent: Record<string, unknown>[] = []
  onopen?: () => void
  onmessage?: (event: { data: string }) => void
  onclose?: () => void
  constructor(readonly url: string) { Socket.instances.push(this) }
  send(value: string) { this.sent.push(JSON.parse(value)) }
  close() { this.readyState = 3; this.onclose?.() }
  frame(value: object) { this.onmessage?.({ data: JSON.stringify(value) }) }
}
afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); Socket.instances = [] })
it('authenticates without URL tokens and resumes bytes while dropping disconnected input', async () => {
  vi.useFakeTimers(); vi.stubGlobal('WebSocket', Socket)
  const output = vi.fn(), reset = vi.fn(), error = vi.fn()
  const controller = connectTerminal(agentTerminalPath('project', 'session'), { connection: vi.fn(), output, reset, status: vi.fn(), ready: vi.fn(), error })
  await Promise.resolve()
  const first = Socket.instances[0]
  expect(first.url).toBe('ws://127.0.0.1:7001/api/projects/project/agents/session/terminal')
  expect(first.url).not.toContain('capability')
  first.onopen?.()
  expect(first.sent).toEqual([{ type: 'authenticate', token: 'capability' }, { type: 'attach', offset: 0, generation: '' }])
  first.frame({ type: 'ready', version: 1, generation: 'epoch', writer: true })
  first.frame({ type: 'output', offset: 0, data: btoa('\x1b[32m') })
  expect(output).toHaveBeenCalledWith(Uint8Array.from([27, 91, 51, 50, 109]))
  controller.input('a')
  expect(first.sent.at(-1)).toEqual({ type: 'input', data: btoa('a') })
  first.close(); controller.input('never replay')
  await vi.advanceTimersByTimeAsync(2000)
  const second = Socket.instances[1]; second.onopen?.()
  expect(second.sent[1]).toEqual({ type: 'attach', offset: 5, generation: 'epoch' })
  second.frame({ type: 'ready', version: 1, generation: 'epoch', writer: false })
  controller.input('read only')
  expect(second.sent).toHaveLength(2)
  second.frame({ type: 'gap', offset: 20 })
  second.frame({ type: 'output', offset: 20, data: btoa('tail') })
  expect(reset).toHaveBeenCalledWith(true)
  expect(error).not.toHaveBeenCalled()
  controller.dispose()
  await vi.advanceTimersByTimeAsync(10000)
  expect(Socket.instances).toHaveLength(2)
})

it('attaches a setup job terminal and passes its status frames on', async () => {
  vi.stubGlobal('WebSocket', Socket)
  const status = vi.fn()
  const controller = connectTerminal<{ id: string; state: string }>(setupTerminalPath('job 1'), { connection: vi.fn(), output: vi.fn(), reset: vi.fn(), status, ready: vi.fn(), error: vi.fn() })
  await Promise.resolve()
  const socket = Socket.instances[0]
  expect(socket.url).toBe('ws://127.0.0.1:7001/api/agents/setup/job%201/terminal')
  socket.onopen?.()
  socket.frame({ type: 'ready', version: 1, generation: 'epoch', writer: true })
  socket.frame({ type: 'status', offset: 0, status: { id: 'job 1', state: 'waiting_secret' } })
  expect(status).toHaveBeenCalledWith({ id: 'job 1', state: 'waiting_secret' })
  controller.dispose()
})

it('follows control changes and takes control back', async () => {
  vi.stubGlobal('WebSocket', Socket)
  const connection = vi.fn(), ready = vi.fn()
  const controller = connectTerminal(agentTerminalPath('project', 'session'), { connection, output: vi.fn(), reset: vi.fn(), status: vi.fn(), ready, error: vi.fn() })
  await Promise.resolve()
  const socket = Socket.instances[0]
  socket.onopen?.()
  socket.frame({ type: 'ready', version: 1, generation: 'holder', writer: true })
  controller.take()
  expect(socket.sent.some(message => message.type === 'take')).toBe(false)
  // Another terminal takes control: input stops, take is offered.
  socket.frame({ type: 'writer', offset: 0, writer: false })
  expect(connection).toHaveBeenLastCalledWith('read-only')
  controller.input('dropped')
  expect(socket.sent.some(message => message.type === 'input')).toBe(false)
  controller.take()
  expect(socket.sent.at(-1)).toEqual({ type: 'take' })
  ready.mockClear()
  socket.frame({ type: 'writer', offset: 0, writer: true })
  expect(connection).toHaveBeenLastCalledWith('connected')
  expect(ready).toHaveBeenCalledTimes(1)
  controller.input('a')
  expect(socket.sent.at(-1)).toEqual({ type: 'input', data: btoa('a') })
  controller.dispose()
})
