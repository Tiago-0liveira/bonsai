import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import type { AgentAccount, AgentCapability } from '../../api/agents'
import { StartAgentDialog } from './StartAgentDialog'
import { useBonsaiStore } from '../../stores/bonsai'
import { agentAccounts, agentProviders } from '../../api/agents'
import { worktrees } from '../../test/fixtures/worktrees'
vi.mock('../../api/agents', async importOriginal => ({ ...await importOriginal<object>(), agentAccounts: vi.fn(), agentProviders: vi.fn() }))
const create = useBonsaiStore.getState().createAgent
const NOT_YET = { message: 'Not available yet' }
const providers = (claude: Partial<AgentCapability> = { available: true }): AgentCapability[] => [
  { id: 'antigravity', label: 'Antigravity', available: true },
  { id: 'claude', label: 'Claude', available: false, unavailable_reason: NOT_YET, ...claude },
  { id: 'codex', label: 'Codex', available: false, unavailable_reason: NOT_YET },
]
const claudeProfile: AgentAccount = { id: 'claude-one', name: 'Work', provider: 'claude', auth_mode: 'login', identity: 'dev@example.com', warnings: ['Token expires in 12 days'] }
beforeEach(() => {
  localStorage.clear()
  useBonsaiStore.setState({ startAgentDialogOpen: true, startAgentTargetWorktreeId: worktrees[0].id, activeProjectId: worktrees[0].projectId, worktrees, createAgent: vi.fn().mockResolvedValue(undefined) })
  vi.mocked(agentProviders).mockResolvedValue(providers({ available: false, unavailable_reason: NOT_YET }))
  vi.mocked(agentAccounts).mockResolvedValue([{ id: 'profile-one', name: 'One', provider: 'antigravity' }])
})
afterEach(() => { cleanup(); useBonsaiStore.setState({ createAgent: create }); vi.clearAllMocks() })
it('disables unavailable providers, selects the only real account and prevents duplicate submit', async () => {
  let finish!: () => void
  const pending = new Promise<void>(resolve => { finish = resolve })
  const submit = vi.fn(() => pending)
  useBonsaiStore.setState({ createAgent: submit })
  render(<StartAgentDialog />)
  await waitFor(() => expect(screen.getByLabelText('Profile')).toHaveTextContent('One'))
  expect(screen.getByRole('button', { name: /Claude/ })).toBeDisabled()
  expect(screen.getByRole('button', { name: /Codex/ })).toBeDisabled()
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

const pick = async (label: string, option: string) => {
  fireEvent.click(screen.getByLabelText(label))
  fireEvent.click(await screen.findByRole('option', { name: option }))
}

it('enables Claude when it is available and has profiles, and disables it with its reason otherwise', async () => {
  const { unmount } = render(<StartAgentDialog />)
  await waitFor(() => expect(screen.getByLabelText('Profile')).toHaveTextContent('One'))
  expect(screen.getByRole('button', { name: /Claude/ })).toBeDisabled()
  expect(screen.getByRole('button', { name: /Claude/ })).toHaveTextContent('Not available yet')
  unmount()
  vi.mocked(agentProviders).mockResolvedValue(providers())
  render(<StartAgentDialog />)
  await waitFor(() => expect(screen.getByRole('button', { name: /Claude/ })).toBeDisabled())
  expect(screen.getByRole('button', { name: /Claude/ })).toHaveTextContent('no profile')
  cleanup()
  vi.mocked(agentAccounts).mockResolvedValue([{ id: 'profile-one', name: 'One', provider: 'antigravity' }, claudeProfile])
  render(<StartAgentDialog />)
  await waitFor(() => expect(screen.getByRole('button', { name: /Claude/ })).toBeEnabled())
  expect(screen.getByRole('button', { name: /Codex/ })).toBeDisabled()
})

it('switches provider, filters profiles, resets the selection and shows profile details', async () => {
  vi.mocked(agentProviders).mockResolvedValue(providers())
  vi.mocked(agentAccounts).mockResolvedValue([
    { id: 'profile-one', name: 'One', provider: 'antigravity' },
    { id: 'profile-two', name: 'Two', provider: 'antigravity' },
    claudeProfile,
  ])
  render(<StartAgentDialog />)
  await waitFor(() => expect(screen.getByLabelText('Profile')).not.toBeDisabled())
  expect(screen.getByRole('button', { name: /Antigravity/ })).toHaveAttribute('aria-pressed', 'true')
  await pick('Profile', 'Two')
  expect(screen.getByLabelText('Profile')).toHaveTextContent('Two')
  fireEvent.change(screen.getByLabelText('Agent model'), { target: { value: 'gemini' } })
  fireEvent.click(screen.getByRole('button', { name: /Claude/ }))
  expect(screen.getByRole('button', { name: /Claude/ })).toHaveAttribute('aria-pressed', 'true')
  expect(screen.getByLabelText('Profile')).toHaveTextContent('Work')
  expect(screen.getByLabelText('Agent model')).toHaveValue('')
  expect(screen.queryByRole('switch', { name: 'Antigravity full access' })).toBeNull()
  expect(screen.getByTestId('profile-details')).toHaveTextContent('auth: login · dev@example.com')
  expect(screen.getByTestId('profile-details')).toHaveTextContent('Token expires in 12 days')
  fireEvent.click(screen.getByRole('button', { name: /Antigravity/ }))
  expect(screen.getByLabelText('Profile')).toHaveTextContent('Choose a profile')
  expect(screen.getByRole('button', { name: 'Start agent' })).toBeDisabled()
  expect(screen.getByRole('switch', { name: 'Antigravity full access' })).toBeInTheDocument()
})

it('defaults to the only provider with profiles and then to the last used one', async () => {
  vi.mocked(agentProviders).mockResolvedValue(providers())
  vi.mocked(agentAccounts).mockResolvedValue([claudeProfile])
  const { unmount } = render(<StartAgentDialog />)
  await waitFor(() => expect(screen.getByLabelText('Profile')).toHaveTextContent('Work'))
  expect(screen.getByRole('button', { name: /Claude/ })).toHaveAttribute('aria-pressed', 'true')
  unmount()
  vi.mocked(agentAccounts).mockResolvedValue([{ id: 'profile-one', name: 'One', provider: 'antigravity' }, { ...claudeProfile }])
  localStorage.setItem('bonsai.startAgent.provider', 'claude')
  render(<StartAgentDialog />)
  await waitFor(() => expect(screen.getByLabelText('Profile')).toHaveTextContent('Work'))
  expect(screen.getByRole('button', { name: /Claude/ })).toHaveAttribute('aria-pressed', 'true')
})

it('submits Claude launch options and never sends full access', async () => {
  vi.mocked(agentProviders).mockResolvedValue(providers())
  vi.mocked(agentAccounts).mockResolvedValue([claudeProfile])
  const submit = vi.fn().mockRejectedValue(new Error('Connection lost'))
  useBonsaiStore.setState({ createAgent: submit })
  render(<StartAgentDialog />)
  await waitFor(() => expect(screen.getByLabelText('Profile')).toHaveTextContent('Work'))
  expect(screen.getByLabelText('Agent model')).toHaveAttribute('placeholder', expect.stringContaining('sonnet'))
  await pick('Permission mode', 'Plan')
  await pick('Effort', 'high')
  fireEvent.change(screen.getByLabelText('Agent model'), { target: { value: 'opus' } })
  fireEvent.submit(screen.getByRole('dialog'))
  await screen.findByRole('alert')
  expect(submit.mock.calls[0][0]).toEqual(expect.objectContaining({ provider: 'Claude', accountId: 'claude-one', permissionMode: 'plan', effort: 'high', model: 'opus' }))
  expect(submit.mock.calls[0][0]).not.toHaveProperty('fullAccess')
  expect(screen.getByText(/Claude · Plan · high effort/)).toBeInTheDocument()
})

it('warns on bypass permissions, changes the request key with the mode and keeps it on retry', async () => {
  vi.mocked(agentProviders).mockResolvedValue(providers())
  vi.mocked(agentAccounts).mockResolvedValue([claudeProfile])
  const submit = vi.fn().mockRejectedValue(new Error('Connection lost'))
  useBonsaiStore.setState({ createAgent: submit })
  render(<StartAgentDialog />)
  await waitFor(() => expect(screen.getByLabelText('Profile')).toHaveTextContent('Work'))
  fireEvent.submit(screen.getByRole('dialog'))
  await screen.findByRole('alert')
  fireEvent.submit(screen.getByRole('dialog'))
  await waitFor(() => expect(submit).toHaveBeenCalledTimes(2))
  expect(submit.mock.calls[1][0].requestKey).toBe(submit.mock.calls[0][0].requestKey)
  expect(screen.queryByRole('note')).toBeNull()
  await pick('Permission mode', 'Bypass permissions')
  expect(screen.getByRole('note')).toHaveTextContent('without asking')
  fireEvent.submit(screen.getByRole('dialog'))
  await waitFor(() => expect(submit).toHaveBeenCalledTimes(3))
  expect(submit.mock.calls[2][0].permissionMode).toBe('bypassPermissions')
  expect(submit.mock.calls[2][0].requestKey).not.toBe(submit.mock.calls[0][0].requestKey)
})

it('shows the setup hint of the selected provider', async () => {
  vi.mocked(agentProviders).mockResolvedValue(providers())
  vi.mocked(agentAccounts).mockResolvedValue([])
  render(<StartAgentDialog />)
  await screen.findByText(/bonsai agent account add antigravity/)
  cleanup()
  vi.mocked(agentAccounts).mockResolvedValue([{ id: 'profile-one', name: 'One', provider: 'antigravity' }])
  localStorage.setItem('bonsai.startAgent.provider', 'claude')
  render(<StartAgentDialog />)
  await waitFor(() => expect(screen.getByLabelText('Profile')).toHaveTextContent('One'))
  // Claude has no profile, so it stays disabled and the usable provider is selected.
  expect(screen.getByRole('button', { name: /Claude/ })).toBeDisabled()
})
