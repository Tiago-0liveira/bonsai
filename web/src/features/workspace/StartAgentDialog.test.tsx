import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { StartAgentDialog } from './StartAgentDialog'
import { useBonsaiStore } from '../../stores/bonsai'
import { agentAccounts, agentProviders } from '../../api/agents'
import { worktrees } from '../../test/fixtures/worktrees'
vi.mock('../../api/agents', async importOriginal => ({ ...await importOriginal<object>(), agentAccounts: vi.fn(), agentProviders: vi.fn() }))
const create = useBonsaiStore.getState().createAgent
beforeEach(() => {
  useBonsaiStore.setState({ startAgentDialogOpen: true, startAgentTargetWorktreeId: worktrees[0].id, activeProjectId: worktrees[0].projectId, worktrees, createAgent: vi.fn().mockResolvedValue(undefined) })
  vi.mocked(agentProviders).mockResolvedValue([{ id: 'antigravity', label: 'Antigravity', available: true }])
  vi.mocked(agentAccounts).mockResolvedValue([{ id: 'profile-one', name: 'One', provider: 'antigravity' }])
})
afterEach(() => { cleanup(); useBonsaiStore.setState({ createAgent: create }); vi.clearAllMocks() })
it('disables other providers, selects the only real account and prevents duplicate submit', async () => {
  let finish!: () => void
  const pending = new Promise<void>(resolve => { finish = resolve })
  const submit = vi.fn(() => pending)
  useBonsaiStore.setState({ createAgent: submit })
  render(<StartAgentDialog />)
  expect(screen.getByRole('button', { name: /Claude/ })).toBeDisabled()
  expect(screen.getByRole('button', { name: /Codex/ })).toBeDisabled()
  await waitFor(() => expect(screen.getByLabelText('Profile')).toHaveTextContent('One'))
  const form = screen.getByRole('dialog')
  fireEvent.submit(form); fireEvent.submit(form)
  expect(submit).toHaveBeenCalledTimes(1)
  expect(submit.mock.calls[0]).toEqual([expect.objectContaining({ accountId: 'profile-one', worktreeId: worktrees[0].id, provider: 'Antigravity', requestKey: expect.any(String) })])
  await act(async () => finish())
})
it('requires a choice for multiple accounts and keeps the request key on retry', async () => {
  vi.mocked(agentAccounts).mockResolvedValue([{ id: 'one', name: 'One', provider: 'antigravity' }, { id: 'two', name: 'Two', provider: 'antigravity' }])
  const submit = vi.fn().mockRejectedValue(new Error('Connection lost'))
  useBonsaiStore.setState({ createAgent: submit })
  render(<StartAgentDialog />)
  await waitFor(() => expect(screen.getByLabelText('Profile')).not.toBeDisabled())
  expect(screen.getByLabelText('Profile')).toHaveTextContent('Choose a profile')
  expect(screen.getByRole('button', { name: 'Start agent' })).toBeDisabled()
  fireEvent.click(screen.getByLabelText('Profile'))
  fireEvent.click(await screen.findByRole('option', { name: 'Two' }))
  fireEvent.submit(screen.getByRole('dialog'))
  await screen.findByRole('alert')
  fireEvent.submit(screen.getByRole('dialog'))
  await waitFor(() => expect(submit).toHaveBeenCalledTimes(2))
  expect(submit.mock.calls[0][0].requestKey).toBe(submit.mock.calls[1][0].requestKey)
})
it('explains missing profile setup and older API failures', async () => {
  vi.mocked(agentAccounts).mockResolvedValue([])
  render(<StartAgentDialog />)
  await screen.findByText(/bonsai agent account add antigravity/)
  expect(screen.getByRole('button', { name: 'Start agent' })).toBeDisabled()
})

it('submits the prompt, model and full-access setting, and changes the request key when permissions change', async () => {
  const submit = vi.fn().mockRejectedValue(new Error('Connection lost'))
  useBonsaiStore.setState({ createAgent: submit })
  render(<StartAgentDialog />)
  await waitFor(() => expect(screen.getByLabelText('Profile')).toHaveTextContent('One'))
  fireEvent.change(screen.getByLabelText('Prompt'), { target: { value: 'Fix the login flow' } })
  fireEvent.change(screen.getByLabelText('Agent model'), { target: { value: 'test-model' } })
  fireEvent.click(screen.getByRole('switch', { name: 'Antigravity full access' }))
  fireEvent.submit(screen.getByRole('dialog'))
  await screen.findByRole('alert')
  expect(submit.mock.calls[0][0]).toEqual(expect.objectContaining({ prompt: 'Fix the login flow', model: 'test-model', fullAccess: true }))
  fireEvent.click(screen.getByRole('switch', { name: 'Antigravity full access' }))
  fireEvent.submit(screen.getByRole('dialog'))
  await waitFor(() => expect(submit).toHaveBeenCalledTimes(2))
  expect(submit.mock.calls[1][0].fullAccess).toBe(false)
  expect(submit.mock.calls[1][0].requestKey).not.toBe(submit.mock.calls[0][0].requestKey)
})
