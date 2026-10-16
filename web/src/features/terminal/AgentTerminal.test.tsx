import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'
import { connectTerminal } from '../../api/agentTerminal'
import type { Agent } from '../../types'
import { AgentTerminal } from './AgentTerminal'

vi.mock('@xterm/xterm/css/xterm.css', () => ({}))
vi.mock('@xterm/xterm', () => ({
  Terminal: class {
    cols = 80
    rows = 24
    loadAddon() {}
    open() {}
    write() {}
    writeln() {}
    reset() {}
    dispose() {}
    onData() { return { dispose() {} } }
  },
}))
vi.mock('@xterm/addon-fit', () => ({ FitAddon: class { fit() {} } }))
vi.mock('../../theme/terminal', () => ({ TERMINAL_FONT: 'monospace', terminalTheme: () => ({}), bindTerminalAppearance: () => () => undefined }))
vi.mock('../../api/agentTerminal', async importOriginal => ({ ...await importOriginal<object>(), connectTerminal: vi.fn() }))

const take = vi.fn()
const agent = { id: 'session', projectId: 'project', lifecycleState: 'running' } as Agent

beforeEach(() => {
  vi.resetAllMocks()
  vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} })
  vi.mocked(connectTerminal).mockReturnValue({ input: vi.fn(), resize: vi.fn(), take, dispose: vi.fn() })
})

it('offers to take control while another terminal has it', async () => {
  render(<AgentTerminal agent={agent} actionsHost={null} />)
  await waitFor(() => expect(connectTerminal).toHaveBeenCalled())
  const callbacks = vi.mocked(connectTerminal).mock.calls[0][1]
  act(() => callbacks.connection('connected'))
  expect(screen.queryByRole('button', { name: 'Take control' })).toBeNull()
  act(() => callbacks.connection('read-only'))
  expect(screen.getByRole('status')).toHaveTextContent('Controlled from another terminal')
  fireEvent.click(screen.getByRole('button', { name: 'Take control' }))
  expect(take).toHaveBeenCalledTimes(1)
  act(() => callbacks.connection('connected'))
  expect(screen.queryByText('Controlled from another terminal')).toBeNull()
  // An ended session has nothing to control.
  act(() => { callbacks.connection('read-only'); callbacks.status({ state: 'exited' } as never) })
  expect(screen.queryByRole('button', { name: 'Take control' })).toBeNull()
})
