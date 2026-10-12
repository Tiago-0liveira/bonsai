import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import type { AgentAccount, AgentCapability, LaunchField } from '../../api/agents'
import { StartAgentDialog } from './StartAgentDialog'
import { useBonsaiStore } from '../../stores/bonsai'
import { agentAccounts, agentModels, agentProviders } from '../../api/agents'
import { worktrees } from '../../test/fixtures/worktrees'
vi.mock('../../api/agents', async importOriginal => ({ ...await importOriginal<object>(), agentAccounts: vi.fn(), agentProviders: vi.fn(), agentModels: vi.fn() }))
const create = useBonsaiStore.getState().createAgent
const NOT_YET = { message: 'Not available yet' }
// Mirrors what the Go providers declare.
const ANTIGRAVITY_FIELDS: LaunchField[] = [{ id: 'full_access', label: 'Full access', kind: 'toggle', help: 'Allow Antigravity to run tools without permission prompts.', options: [{ value: 'false', label: 'Ask permissions' }, { value: 'true', label: 'Full access', tone: 'danger' }] }]
const CLAUDE_FIELDS: LaunchField[] = [
  { id: 'permission_mode', label: 'Permission mode', kind: 'select', options: [{ value: '', label: 'Profile default' }, { value: 'default', label: 'Default' }, { value: 'acceptEdits', label: 'Accept edits' }, { value: 'plan', label: 'Plan' }, { value: 'auto', label: 'Auto' }, { value: 'bypassPermissions', label: 'Bypass permissions', tone: 'danger', help: 'Claude will run tools without asking.' }] },
  { id: 'effort', label: 'Effort', kind: 'select', options: ['', 'low', 'medium', 'high', 'xhigh', 'max'].map(value => ({ value, label: value || 'Profile default' })) },
]
const providers = (claude: Partial<AgentCapability> = { available: true }): AgentCapability[] => [
  { id: 'antigravity', label: 'Antigravity', available: true, launch_fields: ANTIGRAVITY_FIELDS },
  { id: 'claude', label: 'Claude', available: false, unavailable_reason: NOT_YET, launch_fields: CLAUDE_FIELDS, ...claude },
  { id: 'codex', label: 'Codex', available: false, unavailable_reason: NOT_YET },
]
const claudeProfile: AgentAccount = { id: 'claude-one', name: 'Work', provider: 'claude', auth_mode: 'login', identity: 'dev@example.com', warnings: ['Token expires in 12 days'] }
beforeEach(() => {
  localStorage.clear()
  vi.mocked(agentModels).mockResolvedValue([])
  useBonsaiStore.setState({ startAgentDialogOpen: true, startAgentTargetWorktreeId: worktrees[0].id, activeProjectId: worktrees[0].projectId, worktrees, createAgent: vi.fn().mockResolvedValue(undefined) })
  vi.mocked(agentProviders).mockResolvedValue(providers({ available: false, unavailable_reason: NOT_YET }))
  vi.mocked(agentAccounts).mockResolvedValue([{ id: 'profile-one', name: 'One', provider: 'antigravity' }])
})
afterEach(() => { cleanup(); useBonsaiStore.setState({ createAgent: create }); vi.clearAllMocks() })
it('shows the provider marks on every tab and profile option', async () => {
  render(<StartAgentDialog />)
  await waitFor(() => expect(screen.getByLabelText('Profile')).toHaveTextContent('One'))
  for (const provider of ['Antigravity', 'Claude', 'Codex']) {
    const tab = screen.getByRole('button', { name: new RegExp(provider) })
    expect(tab.querySelector('svg')).toHaveAttribute('data-provider-logo', provider.toLowerCase())
    expect(tab.querySelector('.lucide-sparkles')).toBeNull()
  }
  expect(screen.getByLabelText('Profile').querySelector('svg')).toHaveAttribute('data-provider-logo', 'antigravity')
  fireEvent.click(screen.getByLabelText('Profile'))
  expect((await screen.findByRole('option', { name: 'One' })).querySelector('svg')).toHaveAttribute('data-provider-logo', 'antigravity')
})
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
  expect(submit.mock.calls[0][0]).toEqual(expect.objectContaining({ prompt: 'Fix the login flow', model: 'test-model', options: { full_access: true } }))
  expect(screen.getByText('Antigravity · Full access')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('switch', { name: 'Antigravity full access' }))
  fireEvent.submit(screen.getByRole('dialog'))
  await waitFor(() => expect(submit).toHaveBeenCalledTimes(2))
  expect(submit.mock.calls[1][0].options).toEqual({ full_access: false })
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

it('submits Claude launch options from its declared fields', async () => {
  vi.mocked(agentProviders).mockResolvedValue(providers())
  vi.mocked(agentAccounts).mockResolvedValue([claudeProfile])
  const submit = vi.fn().mockRejectedValue(new Error('Connection lost'))
  useBonsaiStore.setState({ createAgent: submit })
  render(<StartAgentDialog />)
  await waitFor(() => expect(screen.getByLabelText('Profile')).toHaveTextContent('Work'))
  expect(screen.getByLabelText('Agent model')).toHaveAttribute('placeholder', 'Use profile default')
  await pick('Permission mode', 'Plan')
  await pick('Effort', 'high')
  fireEvent.change(screen.getByLabelText('Agent model'), { target: { value: 'opus' } })
  fireEvent.submit(screen.getByRole('dialog'))
  await screen.findByRole('alert')
  expect(submit.mock.calls[0][0]).toEqual(expect.objectContaining({ provider: 'Claude', accountId: 'claude-one', model: 'opus', options: { permission_mode: 'plan', effort: 'high' } }))
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
  expect(submit.mock.calls[2][0].options).toEqual({ permission_mode: 'bypassPermissions' })
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

it('clears a stale error and shows profile defaults when switching provider', async () => {
  vi.mocked(agentProviders).mockResolvedValue(providers())
  vi.mocked(agentAccounts).mockResolvedValue([
    { id: 'profile-one', name: 'One', provider: 'antigravity' },
    { ...claudeProfile, model: 'opus', permission_mode: 'plan', effort: 'high' },
  ])
  useBonsaiStore.setState({ createAgent: vi.fn().mockRejectedValue(new Error('Connection lost')) })
  render(<StartAgentDialog />)
  await waitFor(() => expect(screen.getByLabelText('Profile')).not.toBeDisabled())
  await pick('Profile', 'One')
  fireEvent.submit(screen.getByRole('dialog'))
  await screen.findByRole('alert')
  fireEvent.click(screen.getByRole('button', { name: /Claude/ }))
  expect(screen.queryByRole('alert')).toBeNull()
  expect(screen.getByTestId('profile-details')).toHaveTextContent('profile defaults: opus · Plan · high effort')
})

it('suggests the profile\'s models, still accepts typed text, and ignores a failed lookup', async () => {
  vi.mocked(agentProviders).mockResolvedValue(providers())
  vi.mocked(agentAccounts).mockResolvedValue([claudeProfile])
  vi.mocked(agentModels).mockResolvedValue([
    { id: 'opus', label: 'Opus', description: 'Latest Opus', source: 'alias' },
    { id: 'claude-sonnet-5-5', label: 'Claude Sonnet 5.5', description: 'claude-sonnet-5-5', source: 'api' },
  ])
  const submit = vi.fn().mockRejectedValue(new Error('Connection lost'))
  useBonsaiStore.setState({ createAgent: submit })
  render(<StartAgentDialog />)
  await waitFor(() => expect(agentModels).toHaveBeenCalledWith('claude', 'claude-one'))
  const input = screen.getByLabelText('Agent model')
  fireEvent.focus(input)
  expect(await screen.findAllByRole('option')).toHaveLength(2)
  fireEvent.click(screen.getByRole('option', { name: /Claude Sonnet 5.5/ }))
  expect(input).toHaveValue('claude-sonnet-5-5')
  fireEvent.change(input, { target: { value: 'claude-future-9' } })
  expect(screen.queryAllByRole('option')).toHaveLength(0)
  fireEvent.submit(screen.getByRole('dialog'))
  await screen.findByRole('alert')
  expect(submit.mock.calls[0][0].model).toBe('claude-future-9')
})

it('falls back to a plain input when models cannot be loaded', async () => {
  vi.mocked(agentModels).mockRejectedValue(new Error('offline'))
  render(<StartAgentDialog />)
  await waitFor(() => expect(screen.getByLabelText('Profile')).toHaveTextContent('One'))
  await waitFor(() => expect(agentModels).toHaveBeenCalledWith('antigravity', 'profile-one'))
  fireEvent.focus(screen.getByLabelText('Agent model'))
  fireEvent.change(screen.getByLabelText('Agent model'), { target: { value: 'test-model' } })
  expect(screen.queryAllByRole('option')).toHaveLength(0)
  expect(screen.getByLabelText('Agent model')).toHaveValue('test-model')
})

// A provider declaring a preset select and a per-model effort, as Codex will.
const PRESET_FIELDS: LaunchField[] = [
  { id: 'permissions', label: 'Permissions', kind: 'select', default: 'auto', options: [
    { value: 'read-only', label: 'Read only', set: { sandbox: 'read-only', approval: 'on-request' } },
    { value: 'auto', label: 'Auto', set: { sandbox: 'workspace-write', approval: 'on-request' } },
    { value: 'bypass', label: 'Bypass', tone: 'danger', help: 'No sandbox and no approvals.', set: { sandbox: 'bypass' } },
  ] },
  { id: 'effort', label: 'Effort', kind: 'select', per_model: true, options: [{ value: '', label: 'Model default' }, { value: 'low', label: 'low' }, { value: 'high', label: 'high' }] },
]
const effortOptions = async () => within(await screen.findByRole('listbox', { name: 'Effort' })).getAllByRole('option').map(option => option.textContent)
const schemaProviders = (): AgentCapability[] => [{ ...providers()[1], launch_fields: PRESET_FIELDS }]
const schemaModels = [
  { id: 'big', label: 'Big', source: 'api' as const, default: true, efforts: ['low', 'high', 'ultra'], default_effort: 'high' },
  { id: 'small', label: 'Small', source: 'api' as const, efforts: ['low', 'medium'] },
]

it('renders a schema-declared preset and per-model effort without knowing the provider', async () => {
  vi.mocked(agentProviders).mockResolvedValue(schemaProviders())
  vi.mocked(agentAccounts).mockResolvedValue([claudeProfile])
  vi.mocked(agentModels).mockResolvedValue(schemaModels)
  const submit = vi.fn().mockRejectedValue(new Error('Connection lost'))
  useBonsaiStore.setState({ createAgent: submit })
  render(<StartAgentDialog />)
  await waitFor(() => expect(screen.getByLabelText('Agent model')).toHaveAttribute('placeholder', 'Big'))
  expect(screen.getByLabelText('Permissions')).toHaveTextContent('Auto')
  // The default model's efforts are offered while no model is typed.
  fireEvent.click(screen.getByLabelText('Effort'))
  expect(await effortOptions()).toEqual(['Model default', 'low', 'high', 'ultra'])
  fireEvent.click(screen.getByRole('option', { name: 'ultra' }))
  fireEvent.submit(screen.getByRole('dialog'))
  await screen.findByRole('alert')
  expect(submit.mock.calls[0][0].options).toEqual({ sandbox: 'workspace-write', approval: 'on-request', effort: 'ultra' })
  expect(screen.getByText('Claude · Auto · ultra effort')).toBeInTheDocument()

  // A model without that effort drops it; the list follows the model.
  fireEvent.change(screen.getByLabelText('Agent model'), { target: { value: 'small' } })
  expect(screen.getByLabelText('Effort')).toHaveTextContent('Model default')
  fireEvent.click(screen.getByLabelText('Effort'))
  expect(await effortOptions()).toEqual(['Model default', 'low', 'medium'])
  fireEvent.click(screen.getByRole('option', { name: 'medium' }))
  // A typed, unlisted model falls back to the field's own options.
  fireEvent.change(screen.getByLabelText('Agent model'), { target: { value: 'custom-model' } })
  expect(screen.getByLabelText('Effort')).toHaveTextContent('Model default')

  expect(screen.queryByRole('note')).toBeNull()
  await pick('Permissions', 'Bypass')
  expect(screen.getByRole('note')).toHaveTextContent('No sandbox and no approvals.')
  fireEvent.submit(screen.getByRole('dialog'))
  await waitFor(() => expect(submit).toHaveBeenCalledTimes(2))
  expect(submit.mock.calls[1][0].options).toEqual({ sandbox: 'bypass' })
  expect(submit.mock.calls[1][0].requestKey).not.toBe(submit.mock.calls[0][0].requestKey)
})

it('starts each toggle from the selected profile and resets options on provider change', async () => {
  vi.mocked(agentProviders).mockResolvedValue(providers())
  vi.mocked(agentAccounts).mockResolvedValue([
    { id: 'safe', name: 'Safe', provider: 'antigravity', full_access: false },
    { id: 'open', name: 'Open', provider: 'antigravity', full_access: true },
    claudeProfile,
  ])
  render(<StartAgentDialog />)
  await waitFor(() => expect(screen.getByLabelText('Profile')).not.toBeDisabled())
  await pick('Profile', 'Open')
  expect(screen.getByRole('switch', { name: 'Antigravity full access' })).toHaveAttribute('aria-checked', 'true')
  await pick('Profile', 'Safe')
  expect(screen.getByRole('switch', { name: 'Antigravity full access' })).toHaveAttribute('aria-checked', 'false')
  fireEvent.click(screen.getByRole('button', { name: /Claude/ }))
  await pick('Permission mode', 'Plan')
  fireEvent.click(screen.getByRole('button', { name: /Antigravity/ }))
  fireEvent.click(screen.getByRole('button', { name: /Claude/ }))
  expect(screen.getByLabelText('Permission mode')).toHaveTextContent('Profile default')
})

it('renders no options for a provider that declares none', async () => {
  vi.mocked(agentProviders).mockResolvedValue([{ id: 'antigravity', label: 'Antigravity', available: true }])
  const submit = vi.fn().mockRejectedValue(new Error('Connection lost'))
  useBonsaiStore.setState({ createAgent: submit })
  render(<StartAgentDialog />)
  await waitFor(() => expect(screen.getByLabelText('Profile')).toHaveTextContent('One'))
  expect(screen.queryByRole('switch')).toBeNull()
  fireEvent.submit(screen.getByRole('dialog'))
  await screen.findByRole('alert')
  expect(submit.mock.calls[0][0].options).toEqual({})
})

// The Codex schema as the Go provider declares it.
const CODEX_FIELDS: LaunchField[] = [
  { id: 'permissions', label: 'Permissions', kind: 'select', default: 'auto', options: [
    { value: 'read-only', label: 'Read only', set: { sandbox: 'read-only', approval: 'on-request' } },
    { value: 'auto', label: 'Auto', set: { sandbox: 'workspace-write', approval: 'on-request' } },
    { value: 'full-access', label: 'Full access', tone: 'danger', help: 'Codex can change any file and use the network without asking.', set: { sandbox: 'danger-full-access', approval: 'never' } },
    { value: 'bypass', label: 'Bypass', tone: 'danger', help: 'Codex runs every command without a sandbox and without asking.', set: { sandbox: 'bypass' } },
  ] },
  { id: 'effort', label: 'Effort', kind: 'select', per_model: true, options: ['', 'low', 'medium', 'high', 'xhigh', 'max', 'ultra'].map(value => ({ value, label: value || 'Profile default' })) },
]
const codexProviders = (): AgentCapability[] => [...providers().slice(0, 2), { id: 'codex', label: 'Codex', available: true, version: '0.162.1', launch_fields: CODEX_FIELDS }]
const codexModels = [
  { id: 'gpt-6.1-sol', label: 'GPT-6.1-Sol', source: 'api' as const, default: true, efforts: ['low', 'medium', 'high', 'xhigh', 'max', 'ultra'], default_effort: 'low' },
  { id: 'gpt-6-luna', label: 'GPT-6-Luna', source: 'api' as const, efforts: ['low', 'medium', 'high', 'xhigh', 'max'], default_effort: 'medium' },
]
const codexProfile: AgentAccount = { id: 'codex-one', name: 'Personal', provider: 'codex', auth_mode: 'chatgpt', identity: 'me@example.com · plus', auth_status: 'logged_in', warnings: ['Same login as profile "Team"; both share its usage limits'] }

it('enables Codex with its profile and shows its sign-in details and warnings', async () => {
  vi.mocked(agentProviders).mockResolvedValue(codexProviders())
  vi.mocked(agentAccounts).mockResolvedValue([codexProfile])
  vi.mocked(agentModels).mockResolvedValue(codexModels)
  render(<StartAgentDialog />)
  await waitFor(() => expect(screen.getByLabelText('Profile')).toHaveTextContent('Personal'))
  expect(screen.getByRole('button', { name: /Codex/ })).toBeEnabled()
  expect(screen.getByRole('button', { name: /Codex/ })).toHaveAttribute('aria-pressed', 'true')
  expect(screen.getByLabelText('Agent model')).toHaveAttribute('placeholder', 'GPT-6.1-Sol')
  const details = screen.getByTestId('profile-details')
  expect(details).toHaveTextContent('auth: chatgpt · me@example.com · plus · logged in')
  expect(details).toHaveTextContent('Same login as profile "Team"')
  expect(screen.getByLabelText('Permissions')).toHaveTextContent('Auto')
  expect(screen.queryByRole('switch')).toBeNull()
})

it('submits the Codex preset, effort and model, and drops an effort the new model lacks', async () => {
  vi.mocked(agentProviders).mockResolvedValue(codexProviders())
  vi.mocked(agentAccounts).mockResolvedValue([codexProfile])
  vi.mocked(agentModels).mockResolvedValue(codexModels)
  const submit = vi.fn().mockRejectedValue(new Error('Connection lost'))
  useBonsaiStore.setState({ createAgent: submit })
  render(<StartAgentDialog />)
  await waitFor(() => expect(screen.getByLabelText('Agent model')).toHaveAttribute('placeholder', 'GPT-6.1-Sol'))
  fireEvent.change(screen.getByLabelText('Agent model'), { target: { value: 'gpt-6.1-sol' } })
  await pick('Effort', 'ultra')
  fireEvent.submit(screen.getByRole('dialog'))
  await screen.findByRole('alert')
  expect(submit.mock.calls[0][0]).toMatchObject({ provider: 'Codex', accountId: 'codex-one', model: 'gpt-6.1-sol', options: { sandbox: 'workspace-write', approval: 'on-request', effort: 'ultra' } })
  expect(screen.getByText('Codex · Auto · ultra effort')).toBeInTheDocument()

  fireEvent.change(screen.getByLabelText('Agent model'), { target: { value: 'gpt-6-luna' } })
  expect(screen.getByLabelText('Effort')).toHaveTextContent('Profile default')
  await pick('Permissions', 'Full access')
  expect(screen.getByRole('note')).toHaveTextContent('without asking')
  fireEvent.submit(screen.getByRole('dialog'))
  await waitFor(() => expect(submit).toHaveBeenCalledTimes(2))
  expect(submit.mock.calls[1][0].options).toEqual({ sandbox: 'danger-full-access', approval: 'never' })
  expect(submit.mock.calls[1][0].requestKey).not.toBe(submit.mock.calls[0][0].requestKey)

  await pick('Permissions', 'Bypass')
  fireEvent.submit(screen.getByRole('dialog'))
  await waitFor(() => expect(submit).toHaveBeenCalledTimes(3))
  expect(submit.mock.calls[2][0].options).toEqual({ sandbox: 'bypass' })
})

it('explains why Codex is unavailable and how to add its first profile', async () => {
  vi.mocked(agentProviders).mockResolvedValue([...providers().slice(0, 2), { id: 'codex', label: 'Codex', available: false, unavailable_reason: { message: 'Install Codex CLI and restart Bonsai' } }])
  const { unmount } = render(<StartAgentDialog />)
  await waitFor(() => expect(screen.getByLabelText('Profile')).toHaveTextContent('One'))
  expect(screen.getByRole('button', { name: /Codex/ })).toBeDisabled()
  expect(screen.getByRole('button', { name: /Codex/ })).toHaveTextContent('Install Codex CLI and restart Bonsai')
  unmount()
  vi.mocked(agentProviders).mockResolvedValue(codexProviders())
  vi.mocked(agentAccounts).mockResolvedValue([])
  render(<StartAgentDialog />)
  await waitFor(() => expect(screen.getByRole('button', { name: /Codex/ })).toHaveTextContent('no profile'))
  expect(screen.getByRole('button', { name: /Codex/ })).toHaveTextContent('bonsai agent account add codex <name> [--auth device]')
  expect(screen.getByRole('button', { name: /Codex/ })).toBeDisabled()
})
it('preselects the provider and profile the Agents tab opened it for, over the remembered provider', async () => {
  localStorage.setItem('bonsai.startAgent.provider', 'antigravity')
  vi.mocked(agentProviders).mockResolvedValue(providers({ available: true }))
  const second: AgentAccount = { ...claudeProfile, id: 'claude-two', name: 'Second' }
  vi.mocked(agentAccounts).mockResolvedValue([{ id: 'profile-one', name: 'One', provider: 'antigravity' }, claudeProfile, second])
  useBonsaiStore.setState({ startAgentTargetAccountId: 'claude-two' })
  render(<StartAgentDialog />)
  await waitFor(() => expect(screen.getByLabelText('Profile')).toHaveTextContent('Second'))
  expect(screen.getByRole('button', { name: /Claude/ })).toHaveAttribute('aria-pressed', 'true')
  expect(screen.getByRole('button', { name: 'Start agent' })).toBeEnabled()
  expect(screen.getByLabelText('Effort')).toBeInTheDocument()
})
it('ignores a target profile that no longer exists', async () => {
  useBonsaiStore.setState({ startAgentTargetAccountId: 'gone' })
  render(<StartAgentDialog />)
  await waitFor(() => expect(screen.getByLabelText('Profile')).toHaveTextContent('One'))
})
