import { afterEach, expect, it, vi } from 'vitest'
import { connectProcessStream, ProcessMarkerRenderer } from './processStream'
import { useBonsaiStore } from '../stores/bonsai'
import { applyProcessSummary } from './processes'
import { invalidateLocalSession, terminalCapability } from './localClient'
vi.mock('./localClient', () => ({ LOCAL_API_HTTP: 'http://127.0.0.1:7001', terminalCapability: vi.fn().mockResolvedValue('capability'), invalidateLocalSession: vi.fn() }))
vi.mock('./processes', () => ({ applyProcessSummary: vi.fn() }))
class Socket {
  static OPEN = 1
  static instances: Socket[] = []
  readyState = 1
  sent: Record<string, unknown>[] = []
  onopen?: () => void
  onmessage?: (event: { data: string }) => void
  onclose?: () => void
  onerror?: () => void
  constructor(readonly url: string) { Socket.instances.push(this) }
  send(value: string) { this.sent.push(JSON.parse(value)) }
  close() { this.readyState = 3; this.onclose?.() }
  frame(value: object) { this.onmessage?.({ data: JSON.stringify(value) }) }
}
afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); vi.clearAllMocks(); Socket.instances = []; useBonsaiStore.setState({ processVisibility: {} }) })
it('resumes accepted bytes, suppresses duplicates, reports eviction and releases silent sockets', async () => {
  vi.useFakeTimers(); vi.stubGlobal('WebSocket', Socket)
  const output = vi.fn(), gap = vi.fn(), error = vi.fn()
  const controller = connectProcessStream('project', 2, { connection: vi.fn(), output, gap, error })
  await Promise.resolve()
  const first = Socket.instances[0]; first.onopen?.()
  expect(first.url).toContain('/api/projects/project/processes/2/terminal')
  expect(first.url).not.toContain('capability')
  expect(first.sent).toEqual([{ type: 'authenticate', token: 'capability' }, { type: 'attach', generation: '', offset: 0 }])
  first.frame({ type: 'ready', version: 1, generation: 'history' })
  first.frame({ type: 'output', offset: 3, data: btoa('abc') })
  first.frame({ type: 'output', offset: 3, data: btoa('abc') })
  expect(output).toHaveBeenCalledTimes(1)
  first.close()
  await vi.advanceTimersByTimeAsync(2000)
  const second = Socket.instances[1]; second.onopen?.()
  expect(second.sent[1]).toEqual({ type: 'attach', generation: 'history', offset: 3 })
  second.frame({ type: 'ready', version: 1, generation: 'history' })
  second.frame({ type: 'gap', offset: 100 })
  second.frame({ type: 'output', offset: 104, data: btoa('tail') })
  expect(gap).toHaveBeenCalledOnce()
  expect(Array.from(output.mock.calls.at(-1)?.[0] as Uint8Array)).toEqual([116, 97, 105, 108])
  expect(error.mock.calls.every(([message]) => message === '')).toBe(true)
  const invalidations = vi.mocked(invalidateLocalSession).mock.calls.length
  controller.dispose()
  await vi.advanceTimersByTimeAsync(10000)
  expect(Socket.instances).toHaveLength(2)
  expect(invalidateLocalSession).toHaveBeenCalledTimes(invalidations)
})
it('reports attachment failures, allows reconnect and clears the output error after recovery', async () => {
  vi.useFakeTimers(); vi.stubGlobal('WebSocket', Socket)
  vi.mocked(terminalCapability).mockRejectedValueOnce(new Error('Local connection unavailable'))
  const error = vi.fn(), connection = vi.fn()
  const controller = connectProcessStream('project', 1, { connection, output: vi.fn(), gap: vi.fn(), error })
  await Promise.resolve(); await Promise.resolve()
  expect(error).toHaveBeenCalledWith('Local connection unavailable')
  expect(connection).toHaveBeenLastCalledWith('disconnected')
  await vi.advanceTimersByTimeAsync(2000)
  const socket = Socket.instances[0]; socket.onopen?.()
  socket.onerror?.()
  expect(error).toHaveBeenLastCalledWith('Unable to connect to process output. Retrying…')
  socket.frame({ type: 'ready', version: 1, generation: 'history' })
  expect(error).toHaveBeenLastCalledWith('')
  expect(connection).toHaveBeenLastCalledWith('connected')
  controller.dispose()
})
it('replays chunks rejected by a slow renderer without advancing its cursor', async () => {
  vi.useFakeTimers(); vi.stubGlobal('WebSocket', Socket)
  const output = vi.fn().mockReturnValue(false)
  const controller = connectProcessStream('project', 1, { connection: vi.fn(), output, gap: vi.fn(), error: vi.fn() })
  await Promise.resolve()
  const first = Socket.instances[0]; first.onopen?.()
  first.frame({ type: 'ready', version: 1, generation: 'history' })
  first.frame({ type: 'output', offset: 3, data: btoa('abc') })
  await vi.advanceTimersByTimeAsync(2000)
  const second = Socket.instances[1]; second.onopen?.()
  expect(second.sent[1]).toMatchObject({ offset: 0, generation: 'history' })
  controller.dispose()
})
it('preserves UTF-8 and ANSI across every byte boundary and renders embedded partial markers', () => {
  const bytes = new TextEncoder().encode('\x1b[32mé界\x1b[0mno newline\x1estart\t0\tRestarted · attempt 2\nfinal')
  const renderer = new ProcessMarkerRenderer()
  const result: number[] = []
  for (const byte of bytes) result.push(...renderer.render(Uint8Array.of(byte)))
  expect(new TextDecoder().decode(Uint8Array.from(result))).toBe('\x1b[32mé界\x1b[0mno newline\r\n\x1b[0;36m── Restarted · attempt 2 ──\x1b[0m\r\nfinal')
})

it('disposes deleted output immediately and rejects stale frames and queued reconnects', async () => {
  vi.useFakeTimers(); vi.stubGlobal('WebSocket', Socket)
  const output = vi.fn()
  const controller = connectProcessStream('deleted-project', 8, { connection: vi.fn(), output, gap: vi.fn(), error: vi.fn() })
  await Promise.resolve()
  const socket = Socket.instances[0]; socket.onopen?.(); socket.close()
  useBonsaiStore.setState({ processVisibility: { 'deleted-project': { cutoffs: {}, deleted: { 8: true } } } })
  socket.frame({ type: 'status', status: { id: 'deleted-project:8' } })
  socket.frame({ type: 'output', offset: 1, data: btoa('a') })
  await vi.advanceTimersByTimeAsync(10000)
  expect(Socket.instances).toHaveLength(1); expect(output).not.toHaveBeenCalled(); expect(applyProcessSummary).not.toHaveBeenCalled()
  const late = connectProcessStream('deleted-project', 8, { connection: vi.fn(), output, gap: vi.fn(), error: vi.fn() })
  await Promise.resolve(); expect(Socket.instances).toHaveLength(1)
  controller.dispose(); late.dispose()
})
