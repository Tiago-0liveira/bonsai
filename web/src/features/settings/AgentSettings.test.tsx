import { beforeEach, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { AgentSettings } from './AgentSettings'
import { loadAgentSettings, saveAgentSettings } from '../../api/settings'
import { useBonsaiStore } from '../../stores/bonsai'

vi.mock('../../api/settings', () => ({ loadAgentSettings: vi.fn(), saveAgentSettings: vi.fn() }))
const load = vi.mocked(loadAgentSettings)
const save = vi.mocked(saveAgentSettings)

beforeEach(() => {
  vi.resetAllMocks()
  useBonsaiStore.setState({ agents: [] })
})

it('keeps agents running once enabled and explains how to reach them', async () => {
  load.mockResolvedValue({ revision: 3, keep_running: false, active_sessions: 0 })
  save.mockResolvedValue({ revision: 4, keep_running: true, active_sessions: 0 })
  render(<AgentSettings />)
  const toggle = await screen.findByRole('checkbox', { name: 'Keep agents running when Bonsai web stops' })
  expect(toggle).not.toBeChecked()
  expect(screen.getByText('bonsai agents attach <id>')).toBeVisible()
  expect(screen.queryByRole('status')).toBeNull()
  fireEvent.click(toggle)
  await waitFor(() => expect(save).toHaveBeenCalledWith({ revision: 3, keep_running: false, active_sessions: 0 }, true))
  await waitFor(() => expect(toggle).toBeChecked())
})

it('warns that running agents will be stopped while the setting is off', async () => {
  load.mockResolvedValue({ revision: 1, keep_running: true, active_sessions: 2 })
  save.mockResolvedValue({ revision: 2, keep_running: false, active_sessions: 2 })
  render(<AgentSettings />)
  const toggle = await screen.findByRole('checkbox', { name: 'Keep agents running when Bonsai web stops' })
  expect(screen.queryByRole('status')).toBeNull()
  fireEvent.click(toggle)
  expect(await screen.findByRole('status')).toHaveTextContent('2 agents are running — they will be stopped when Bonsai web stops.')
})

it('reloads and reports a conflicting save', async () => {
  load.mockResolvedValueOnce({ revision: 1, keep_running: false, active_sessions: 0 })
    .mockResolvedValueOnce({ revision: 2, keep_running: true, active_sessions: 0 })
  save.mockRejectedValue(new Error('web settings changed'))
  render(<AgentSettings />)
  fireEvent.click(await screen.findByRole('checkbox', { name: 'Keep agents running when Bonsai web stops' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('web settings changed')
  await waitFor(() => expect(screen.getByRole('checkbox')).toBeChecked())
})

it('stays hidden when the API has no agent settings', async () => {
  load.mockRejectedValue(new Error('not found'))
  const { container } = render(<AgentSettings />)
  await waitFor(() => expect(load).toHaveBeenCalled())
  expect(container).toBeEmptyDOMElement()
})
