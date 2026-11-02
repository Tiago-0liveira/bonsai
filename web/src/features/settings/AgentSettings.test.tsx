import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { AgentSettings } from './AgentSettings'
import { loadAgentSettings, saveAgentSettings, type AgentSettings as Settings } from '../../api/settings'
import { useBonsaiStore } from '../../stores/bonsai'

vi.mock('../../api/settings', () => ({ loadAgentSettings: vi.fn(), saveAgentSettings: vi.fn() }))
const load = vi.mocked(loadAgentSettings)
const save = vi.mocked(saveAgentSettings)

const base = (patch: Partial<Settings> = {}): Settings => ({ revision: 1, keep_running: false, active_sessions: 0, notifications: { enabled: false, needs_input: true, turn_complete: true }, ...patch })

beforeEach(() => {
  vi.resetAllMocks()
  useBonsaiStore.setState({ agents: [] })
})
afterEach(cleanup)

it('keeps agents running once enabled and explains how to reach them', async () => {
  load.mockResolvedValue(base({ revision: 3 }))
  save.mockResolvedValue(base({ revision: 4, keep_running: true }))
  render(<AgentSettings />)
  const toggle = await screen.findByRole('checkbox', { name: 'Keep agents running when Bonsai web stops' })
  expect(toggle).not.toBeChecked()
  expect(screen.getByText('bonsai agents attach <id>')).toBeVisible()
  expect(screen.queryByRole('status')).toBeNull()
  fireEvent.click(toggle)
  await waitFor(() => expect(save).toHaveBeenCalledWith(base({ revision: 3 }), { keep_running: true }))
  await waitFor(() => expect(toggle).toBeChecked())
})

it('warns that running agents will be stopped while the setting is off', async () => {
  load.mockResolvedValue(base({ keep_running: true, active_sessions: 2 }))
  save.mockResolvedValue(base({ revision: 2, active_sessions: 2 }))
  render(<AgentSettings />)
  const toggle = await screen.findByRole('checkbox', { name: 'Keep agents running when Bonsai web stops' })
  expect(screen.queryByRole('status')).toBeNull()
  fireEvent.click(toggle)
  expect(await screen.findByRole('status')).toHaveTextContent('2 agents are running — they will be stopped when Bonsai web stops.')
})

it('reloads and reports a conflicting save', async () => {
  load.mockResolvedValueOnce(base())
    .mockResolvedValueOnce(base({ revision: 2, keep_running: true }))
  save.mockRejectedValue(new Error('web settings changed'))
  render(<AgentSettings />)
  fireEvent.click(await screen.findByRole('checkbox', { name: 'Keep agents running when Bonsai web stops' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('web settings changed')
  await waitFor(() => expect(screen.getByRole('checkbox', { name: 'Keep agents running when Bonsai web stops' })).toBeChecked())
})

it('stays hidden when the API has no agent settings', async () => {
  load.mockRejectedValue(new Error('not found'))
  const { container } = render(<AgentSettings />)
  await waitFor(() => expect(load).toHaveBeenCalled())
  expect(container).toBeEmptyDOMElement()
})

it('saves a server notification change with the read revision and explains when it applies', async () => {
  load.mockResolvedValue(base({ revision: 5 }))
  save.mockResolvedValue(base({ revision: 6, notifications: { enabled: true, needs_input: true, turn_complete: true } }))
  render(<AgentSettings />)
  const group = within(await screen.findByRole('group', { name: 'Desktop notifications' }))
  expect(group.getByText(/applies to sessions started after the change/i)).toBeVisible()
  expect(group.getByRole('checkbox', { name: 'When an agent needs input' })).toBeDisabled()
  fireEvent.click(group.getByRole('checkbox', { name: 'Send desktop notifications' }))
  await waitFor(() => expect(save).toHaveBeenCalledWith(base({ revision: 5 }), { notifications: { enabled: true } }))
  await waitFor(() => expect(group.getByRole('checkbox', { name: 'Send desktop notifications' })).toBeChecked())
  fireEvent.click(group.getByRole('checkbox', { name: 'When an agent finishes a turn' }))
  await waitFor(() => expect(save).toHaveBeenLastCalledWith(expect.objectContaining({ revision: 6 }), { notifications: { turn_complete: false } }))
})

it('reloads and reports a stale notification save (409)', async () => {
  load.mockResolvedValueOnce(base({ notifications: { enabled: true, needs_input: true, turn_complete: true } }))
    .mockResolvedValueOnce(base({ revision: 2, notifications: { enabled: true, needs_input: false, turn_complete: true } }))
  save.mockRejectedValue(new Error('web settings changed; reload and try again'))
  render(<AgentSettings />)
  fireEvent.click(await screen.findByRole('checkbox', { name: 'When an agent finishes a turn' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('web settings changed')
  await waitFor(() => expect(screen.getByRole('checkbox', { name: 'When an agent needs input' })).not.toBeChecked())
})
