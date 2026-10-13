import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { AgentAccount, AgentCapability, AgentUsage, LaunchField } from '../../api/agents'
import { StartAgentDialog } from './StartAgentDialog'
import { useBonsaiStore } from '../../stores/bonsai'
import { agentAccounts, agentModels, agentProviders } from '../../api/agents'
import { agentUsage, refreshAgentUsage } from '../../api/agentsOverview'
import { worktrees } from '../../test/fixtures/worktrees'
vi.mock('../../api/agents', async importOriginal => ({ ...await importOriginal<object>(), agentAccounts: vi.fn(), agentProviders: vi.fn(), agentModels: vi.fn() }))
vi.mock('../../api/agentsOverview', async importOriginal => ({ ...await importOriginal<object>(), agentUsage: vi.fn(), refreshAgentUsage: vi.fn() }))
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
  vi.mocked(agentUsage).mockResolvedValue([])
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
  await screen.findByText('Choose a profile to continue.')
  expect(screen.getByRole('button', { name: 'Start agent' })).toBeDisabled()
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

it('submits the prompt with the model and options on screen, and changes the request key when they change', async () => {
  const submit = vi.fn().mockRejectedValue(new Error('Connection lost'))
  useBonsaiStore.setState({ createAgent: submit })
  render(<StartAgentDialog />)
  await waitFor(() => expect(screen.getByLabelText('Profile')).toHaveTextContent('One'))
  fireEvent.change(screen.getByLabelText('Prompt'), { target: { value: 'Fix the login flow' } })
  fireEvent.change(screen.getByLabelText('Agent model'), { target: { value: 'test-model' } })
  fireEvent.click(screen.getByRole('radio', { name: 'Full access' }))
  fireEvent.submit(screen.getByRole('dialog'))
  await screen.findByRole('alert')
  expect(submit.mock.calls[0][0]).toEqual(expect.objectContaining({ prompt: 'Fix the login flow', model: 'test-model', options: { full_access: true } }))
  // The retry of the same request keeps its key.
  fireEvent.submit(screen.getByRole('dialog'))
  await waitFor(() => expect(submit).toHaveBeenCalledTimes(2))
  expect(submit.mock.calls[1][0].requestKey).toBe(submit.mock.calls[0][0].requestKey)
  // A different access choice is a different request.
  fireEvent.click(screen.getByRole('radio', { name: 'Ask permissions' }))
  fireEvent.submit(screen.getByRole('dialog'))
  await waitFor(() => expect(submit).toHaveBeenCalledTimes(3))
  expect(submit.mock.calls[2][0].options).toEqual({ full_access: false })
  expect(submit.mock.calls[2][0].requestKey).not.toBe(submit.mock.calls[0][0].requestKey)
})

const effortSlider = () => screen.getByRole('slider', { name: 'Effort' })
const effortStops = () => Array.from(effortSlider().querySelectorAll('[data-stop]')).map(stop => stop.getAttribute('data-stop'))
const chooseEffort = (tick: string) => fireEvent.click(within(effortSlider().parentElement as HTMLElement).getByText(tick, { selector: 'button' }))
// Profiles are cards, effort is a slider and every other launch field is a row of chips.
const pick = async (label: string, option: string | RegExp) => {
  if (label === 'Profile') fireEvent.click(await screen.findByRole('option', { name: option }))
  else if (label === 'Effort') chooseEffort(String(option))
  else fireEvent.click(await screen.findByRole('radio', { name: option }))
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
  expect(screen.getByRole('option', { name: /Two/ })).toHaveAttribute('aria-selected', 'true')
  fireEvent.click(screen.getByRole('button', { name: /Claude/ }))
  expect(screen.getByRole('button', { name: /Claude/ })).toHaveAttribute('aria-pressed', 'true')
  expect(screen.getByLabelText('Profile')).toHaveTextContent('Work')
  // Only what needs attention is shown under the profile: a healthy sign-in line is not.
  expect(screen.getByTestId('profile-details')).not.toHaveTextContent('dev@example.com')
  expect(screen.getByTestId('profile-details')).toHaveTextContent('Token expires in 12 days')
  fireEvent.click(screen.getByRole('button', { name: /Antigravity/ }))
  expect(screen.getByText('Choose a profile to continue.')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Start agent' })).toBeDisabled()
  expect(screen.getByRole('radio', { name: 'Full access' })).toBeInTheDocument()
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

it('opens on the profile\'s saved model, permission and effort and sends them', async () => {
  vi.mocked(agentProviders).mockResolvedValue(providers())
  vi.mocked(agentAccounts).mockResolvedValue([{ ...claudeProfile, model: 'opus', permission_mode: 'plan', effort: 'high' }])
  const submit = vi.fn().mockRejectedValue(new Error('Connection lost'))
  useBonsaiStore.setState({ createAgent: submit })
  render(<StartAgentDialog />)
  await waitFor(() => expect(screen.getByLabelText('Profile')).toHaveTextContent('Work'))
  expect(screen.getByLabelText('Agent model')).toHaveValue('opus')
  expect(screen.getByRole('radio', { name: 'Plan' })).toBeChecked()
  expect(effortSlider()).toHaveAttribute('aria-valuetext', 'High')
  expect(screen.getByText('Claude · Work · opus · Plan · high effort')).toBeInTheDocument()
  fireEvent.submit(screen.getByRole('dialog'))
  await screen.findByRole('alert')
  expect(submit.mock.calls[0][0]).toEqual(expect.objectContaining({ provider: 'Claude', accountId: 'claude-one', model: 'opus', options: { permission_mode: 'plan', effort: 'high' } }))
})

it('opens on the provider default where the profile saved none, and one click returns to it', async () => {
  vi.mocked(agentProviders).mockResolvedValue(providers())
  vi.mocked(agentAccounts).mockResolvedValue([claudeProfile])
  render(<StartAgentDialog />)
  await waitFor(() => expect(screen.getByLabelText('Profile')).toHaveTextContent('Work'))
  // Permission: the provider's own default; effort: the middle of the levels on offer.
  expect(screen.getByRole('radio', { name: /^Default/ })).toBeChecked()
  expect(effortSlider()).toHaveAttribute('aria-valuetext', 'High')
  expect(effortStops()).toEqual(['low', 'medium', 'high', 'xhigh', 'max'])
  await pick('Permission mode', 'Plan')
  chooseEffort('max')
  expect(screen.getByRole('radio', { name: 'Plan' })).toBeChecked()
  fireEvent.click(screen.getByRole('button', { name: /Default · Default/ }))
  fireEvent.click(screen.getByRole('button', { name: /Default · High/ }))
  expect(screen.getByRole('radio', { name: /^Default/ })).toBeChecked()
  expect(effortSlider()).toHaveAttribute('aria-valuetext', 'High')
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
  expect(screen.getByLabelText('Agent model')).toHaveValue('opus')
  expect(screen.getByRole('radio', { name: 'Plan' })).toBeChecked()
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
  expect(screen.getByRole('radio', { name: 'Full access' })).toBeChecked()
  await pick('Profile', 'Safe')
  expect(screen.getByRole('radio', { name: 'Ask permissions' })).toBeChecked()
  fireEvent.click(screen.getByRole('button', { name: /Claude/ }))
  await pick('Permission mode', 'Plan')
  fireEvent.click(screen.getByRole('button', { name: /Antigravity/ }))
  fireEvent.click(screen.getByRole('button', { name: /Claude/ }))
  expect(screen.getByRole('radio', { name: /^Default/ })).toBeChecked()
})

it('renders no options for a provider that declares none', async () => {
  vi.mocked(agentProviders).mockResolvedValue([{ id: 'antigravity', label: 'Antigravity', available: true }])
  const submit = vi.fn().mockRejectedValue(new Error('Connection lost'))
  useBonsaiStore.setState({ createAgent: submit })
  render(<StartAgentDialog />)
  await waitFor(() => expect(screen.getByLabelText('Profile')).toHaveTextContent('One'))
  expect(screen.queryByRole('radiogroup', { name: 'Full access' })).toBeNull()
  expect(screen.queryByRole('slider')).toBeNull()
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
  const details = screen.getByTestId('profile-details')
  expect(details).not.toHaveTextContent('logged in')
  expect(details).toHaveTextContent('Same login as profile "Team"')
  await waitFor(() => expect(screen.getByLabelText('Agent model')).toHaveValue('gpt-6.1-sol'))
  expect(screen.getByRole('radio', { name: /^Auto/ })).toBeChecked()
  expect(screen.queryByRole('switch')).toBeNull()
})

it('submits the Codex preset, effort and model, and moves an effort the new model lacks to its default', async () => {
  vi.mocked(agentProviders).mockResolvedValue(codexProviders())
  vi.mocked(agentAccounts).mockResolvedValue([{ ...codexProfile, model: 'gpt-6.1-sol', effort: 'ultra', sandbox: 'danger-full-access', approval: 'never' }])
  vi.mocked(agentModels).mockResolvedValue(codexModels)
  const submit = vi.fn().mockRejectedValue(new Error('Connection lost'))
  useBonsaiStore.setState({ createAgent: submit })
  render(<StartAgentDialog />)
  await waitFor(() => expect(screen.getByLabelText('Profile')).toHaveTextContent('Personal'))
  expect(screen.getByLabelText('Agent model')).toHaveValue('gpt-6.1-sol')
  expect(screen.getByRole('radio', { name: 'Full access' })).toBeChecked()
  expect(effortSlider()).toHaveAttribute('aria-valuetext', 'Ultra')
  fireEvent.submit(screen.getByRole('dialog'))
  await screen.findByRole('alert')
  expect(submit.mock.calls[0][0]).toMatchObject({ provider: 'Codex', accountId: 'codex-one', model: 'gpt-6.1-sol', options: { sandbox: 'danger-full-access', approval: 'never', effort: 'ultra' } })

  // luna has no "ultra": the effort falls to that model's own default.
  fireEvent.change(screen.getByLabelText('Agent model'), { target: { value: 'gpt-6-luna' } })
  expect(effortSlider()).toHaveAttribute('aria-valuetext', 'Medium')
  await pick('Permissions', 'Bypass')
  fireEvent.submit(screen.getByRole('dialog'))
  await waitFor(() => expect(submit).toHaveBeenCalledTimes(2))
  expect(submit.mock.calls[1][0]).toMatchObject({ model: 'gpt-6-luna', options: { sandbox: 'bypass', effort: 'medium' } })
  expect(submit.mock.calls[1][0].requestKey).not.toBe(submit.mock.calls[0][0].requestKey)
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
  await waitFor(() => expect(screen.getByRole('option', { name: /Second/ })).toHaveAttribute('aria-selected', 'true'))
  expect(screen.getByRole('button', { name: /Claude/ })).toHaveAttribute('aria-pressed', 'true')
  expect(screen.getByRole('button', { name: 'Start agent' })).toBeEnabled()
})
it('ignores a target profile that no longer exists', async () => {
  useBonsaiStore.setState({ startAgentTargetAccountId: 'gone' })
  render(<StartAgentDialog />)
  await waitFor(() => expect(screen.getByLabelText('Profile')).toHaveTextContent('One'))
})

describe('usage-aware profile choice', () => {
  const profile = (id: string, name: string): AgentAccount => ({ ...codexProfile, id, name, warnings: undefined })
  // API order: by name. Usage reorders them by headroom.
  const profiles = [profile('codex-a', 'Alpha'), profile('codex-b', 'Bravo'), profile('codex-c', 'Charlie'), profile('codex-d', 'Delta')]
  const reading = (id: string, primary: number, secondary: number, low = false): AgentUsage => ({
    account_id: id, provider: 'codex',
    headline: { primary: { id: 'five_hour', label: '5h', window: '5h', remaining_fraction: primary }, secondary: { id: 'weekly', label: 'Weekly', window: 'weekly', remaining_fraction: secondary }, score: Math.min(primary, secondary), low },
  })
  const usage: AgentUsage[] = [
    reading('codex-a', 0.15, 0.55, true),
    { account_id: 'codex-b', provider: 'codex', error: { code: 'not_authenticated', message: 'Not signed in' } },
    reading('codex-c', 0.72, 0.4),
  ]
  const openProfiles = async () => {
    await waitFor(() => expect(screen.getByLabelText('Profile')).not.toBeDisabled())
    fireEvent.click(screen.getByLabelText('Profile'))
    await screen.findAllByRole('option')
  }
  const optionNames = () => screen.getAllByRole('option').map(option => option.textContent?.split('me@example.com')[0])
  beforeEach(() => {
    vi.mocked(agentProviders).mockResolvedValue(codexProviders())
    vi.mocked(agentAccounts).mockResolvedValue(profiles)
  })

  it('orders profiles by cached headroom with unknown ones last, and labels each option', async () => {
    vi.mocked(agentUsage).mockResolvedValue(usage)
    render(<StartAgentDialog />)
    await openProfiles()
    await waitFor(() => expect(optionNames()).toEqual(['Charlie', 'Alpha', 'Bravo', 'Delta']))
    const options = screen.getAllByRole('option')
    expect(options[0]).toHaveTextContent('5h 72% · week 40%')
    expect(options[1]).toHaveTextContent('5h 15% · week 55%')
    expect(options[2]).not.toHaveTextContent('%')
    fireEvent.click(options[0])
    expect(screen.getByLabelText('Profile')).toHaveTextContent('Charlie')
    expect(screen.getByLabelText('Profile')).toHaveTextContent('5h 72% · week 40%')
  })

  it('keeps the API order and shows no headroom when usage cannot be read', async () => {
    vi.mocked(agentUsage).mockRejectedValue(new Error('offline'))
    render(<StartAgentDialog />)
    await waitFor(() => expect(agentUsage).toHaveBeenCalled())
    await openProfiles()
    expect(optionNames()).toEqual(['Alpha', 'Bravo', 'Charlie', 'Delta'])
    expect(screen.getAllByRole('option').some(option => option.textContent?.includes('%'))).toBe(false)
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('warns under the select when the chosen profile runs low and names a roomier one', async () => {
    vi.mocked(agentUsage).mockResolvedValue(usage)
    render(<StartAgentDialog />)
    await openProfiles()
    await waitFor(() => expect(optionNames()[0]).toBe('Charlie'))
    fireEvent.click(screen.getByRole('option', { name: /Alpha/ }))
    expect(screen.getByRole('note')).toHaveTextContent('Low headroom: 5h 15% · week 55% left. Charlie has more.')
    await pick('Profile', /Charlie/)
    expect(screen.queryByRole('note')).toBeNull()
  })

  it('reads cached usage once and never asks for a refresh', async () => {
    vi.mocked(agentUsage).mockResolvedValue(usage)
    render(<StartAgentDialog />)
    await waitFor(() => expect(screen.getByLabelText('Profile')).not.toBeDisabled())
    await pick('Profile', /Alpha/)
    await pick('Profile', /Charlie/)
    expect(agentUsage).toHaveBeenCalledTimes(1)
    expect(agentUsage).toHaveBeenCalledWith()
    expect(refreshAgentUsage).not.toHaveBeenCalled()
  })
})

it('offers to sign a signed-out profile in again on the Agents page', async () => {
  const { RouterProvider, createMemoryHistory, createRootRoute, createRoute, createRouter } = await import('@tanstack/react-router')
  window.scrollTo = vi.fn()
  vi.mocked(agentProviders).mockResolvedValue(providers())
  vi.mocked(agentAccounts).mockResolvedValue([{ ...claudeProfile, auth_status: 'logged_out', warnings: ['Not logged in; run `bonsai agent account login Work`'] }])
  useBonsaiStore.setState({ startAgentTargetAccountId: claudeProfile.id })
  const root = createRootRoute({ component: StartAgentDialog })
  const agents = createRoute({ getParentRoute: () => root, path: '/agents', component: () => null })
  const home = createRoute({ getParentRoute: () => root, path: '/', component: () => null })
  const router = createRouter({ routeTree: root.addChildren([agents, home]), history: createMemoryHistory({ initialEntries: ['/'] }) })
  render(<RouterProvider router={router} />)
  const details = await screen.findByTestId('profile-details')
  fireEvent.click(within(details).getByRole('button', { name: 'Sign in again' }))
  await waitFor(() => expect(router.state.location.pathname).toBe('/agents'))
  expect(router.state.location.search).toEqual({ profile: claudeProfile.id, signin: true })
  expect(useBonsaiStore.getState().startAgentDialogOpen).toBe(false)
})

it('offers no sign-in link for a signed-in profile', async () => {
  vi.mocked(agentProviders).mockResolvedValue(providers())
  vi.mocked(agentAccounts).mockResolvedValue([{ ...claudeProfile, auth_status: 'logged_in' }])
  useBonsaiStore.setState({ startAgentTargetAccountId: claudeProfile.id })
  render(<StartAgentDialog />)
  await screen.findByTestId('profile-details')
  expect(screen.queryByRole('button', { name: 'Sign in again' })).toBeNull()
})

it('opens the profile\'s Defaults tab on the Agents page to change what a launch starts with', async () => {
  const { RouterProvider, createMemoryHistory, createRootRoute, createRoute, createRouter } = await import('@tanstack/react-router')
  window.scrollTo = vi.fn()
  vi.mocked(agentProviders).mockResolvedValue(providers())
  vi.mocked(agentAccounts).mockResolvedValue([claudeProfile])
  useBonsaiStore.setState({ startAgentTargetAccountId: claudeProfile.id })
  const root = createRootRoute({ component: StartAgentDialog })
  const agents = createRoute({ getParentRoute: () => root, path: '/agents', component: () => null })
  const home = createRoute({ getParentRoute: () => root, path: '/', component: () => null })
  const router = createRouter({ routeTree: root.addChildren([agents, home]), history: createMemoryHistory({ initialEntries: ['/'] }) })
  render(<RouterProvider router={router} />)
  fireEvent.click(await screen.findByRole('button', { name: 'Edit defaults' }))
  await waitFor(() => expect(router.state.location.pathname).toBe('/agents'))
  expect(router.state.location.search).toEqual({ profile: claudeProfile.id, tab: 'defaults' })
  expect(useBonsaiStore.getState().startAgentDialogOpen).toBe(false)
})

it('has no work type, no sign-in summary for a healthy profile, and the agent name beside the worktree', async () => {
  vi.mocked(agentProviders).mockResolvedValue(providers())
  vi.mocked(agentAccounts).mockResolvedValue([{ ...claudeProfile, warnings: undefined, auth_status: 'logged_in' }])
  render(<StartAgentDialog />)
  await waitFor(() => expect(screen.getByLabelText('Profile')).toHaveTextContent('Work'))
  expect(screen.queryByRole('radiogroup', { name: 'Agent work type' })).toBeNull()
  expect(screen.queryByText('Work type')).toBeNull()
  expect(screen.queryByTestId('profile-details')).toBeNull()
  const worktree = screen.getByLabelText('Worktree and branch')
  const name = screen.getByLabelText('Agent name')
  expect(worktree.compareDocumentPosition(name) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  // Both sit in the same two-column row above the provider choice.
  expect(worktree.closest('.dialog-cols')).toBe(name.closest('.dialog-cols'))
  expect(name.compareDocumentPosition(screen.getByRole('button', { name: /Claude/ })) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
})
