import { act, cleanup, render } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { FakeTerminal } from './FakeTerminal'
import { useBonsaiStore } from '../../stores/bonsai'

const terminal = vi.hoisted(() => ({ open: vi.fn(), loadAddon: vi.fn(), writeln: vi.fn(), write: vi.fn(), scrollToBottom: vi.fn(), dispose: vi.fn() }))
const fit = vi.hoisted(() => ({ fit: vi.fn() }))
vi.mock('@xterm/xterm', () => ({ Terminal: class { constructor() { return terminal } } }))
vi.mock('@xterm/addon-fit', () => ({ FitAddon: class { constructor() { return fit } } }))

describe('retained terminal measurements', () => {
  let resize: () => void
  let frames: Map<number, FrameRequestCallback>
  let frameId: number
  const flushFrame = () => { const pending = [...frames.values()]; frames.clear(); pending.forEach(callback => callback(0)) }
  beforeEach(() => {
    vi.clearAllMocks()
    frames = new Map()
    frameId = 0
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => { frames.set(++frameId, callback); return frameId })
    vi.stubGlobal('cancelAnimationFrame', (id: number) => frames.delete(id))
    vi.stubGlobal('ResizeObserver', class { constructor(callback: () => void) { resize = callback } observe() {} disconnect() {} })
    useBonsaiStore.setState({ activeTerminalId: 'recorded', terminalOutput: { recorded: ['recorded line'] } })
  })
  afterEach(() => { cleanup(); flushFrame(); flushFrame(); vi.unstubAllGlobals() })

  it('defers opening a hidden terminal and preserves the instance across collapse/expand', () => {
    const { container } = render(<div><FakeTerminal /></div>)
    const boundary = container.firstElementChild!
    const host = boundary.firstElementChild!
    Object.defineProperties(host, { clientWidth: { get: () => 500 }, clientHeight: { get: () => 200 } })
    boundary.setAttribute('inert', '')
    act(flushFrame)
    resize!()
    expect(terminal.open).not.toHaveBeenCalled()
    expect(fit.fit).not.toHaveBeenCalled()
    boundary.removeAttribute('inert')
    resize!()
    expect(terminal.open).toHaveBeenCalledTimes(1)
    expect(fit.fit).toHaveBeenCalledTimes(1)
    boundary.setAttribute('inert', '')
    resize!()
    act(() => useBonsaiStore.setState({ terminalOutput: { recorded: ['updated recorded line'] } }))
    act(flushFrame)
    expect(fit.fit).toHaveBeenCalledTimes(1)
    boundary.removeAttribute('inert')
    resize!()
    expect(terminal.open).toHaveBeenCalledTimes(1)
    expect(fit.fit).toHaveBeenCalledTimes(2)
  })

  it('skips opening and fitting a zero size host until it has measurable dimensions', () => {
    const { container } = render(<FakeTerminal />)
    act(flushFrame)
    expect(terminal.open).not.toHaveBeenCalled()
    const host = container.firstElementChild!
    Object.defineProperties(host, { clientWidth: { value: 500 }, clientHeight: { value: 200 } })
    resize!()
    expect(terminal.open).toHaveBeenCalledTimes(1)
    expect(fit.fit).toHaveBeenCalledTimes(1)
  })
})
