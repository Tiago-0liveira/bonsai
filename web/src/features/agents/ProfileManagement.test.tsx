import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { agentModels, agentProviders, stopAgent, type AgentAccount, type AgentCapability, type AgentModel } from '../../api/agents'
import { AgentProfileError, agentAccountsIfChanged, agentUsage, removeAgentProfile, updateAgentProfile } from '../../api/agentsOverview'
import { useBonsaiStore } from '../../stores/bonsai'
import { projects } from '../../test/fixtures/projects'
import { worktrees } from '../../test/fixtures/worktrees'
import type { Agent } from '../../types'
import { AgentsPage, type AgentsPageProps } from './AgentsPage'

vi.mock('../../api/agents', async importOriginal => ({ ...await importOriginal<object>(), agentProviders: vi.fn(), agentModels: vi.fn(), stopAgent: vi.fn() }))
vi.mock('../../api/agentsOverview', async importOriginal => ({
  ...await importOriginal<object>(),
  agentAccountsIfChanged: vi.fn(), agentUsage: vi.fn(), refreshAgentUsage: vi.fn(), updateAgentProfile: vi.fn(), removeAgentProfile: vi.fn(),
}))

const providers: AgentCapability[] = [
  {
    id: 'codex', label: 'Codex', available: true, version: '0.162.1',
    launch_fields: [{ id: 'effort', label: 'Effort', kind: 'select', per_model: true, options: [{ value: '', label: 'Profile default' }, { value: 'high', label: 'high' }] }],
    profile_fields: [
      { id: 'model', label: 'Model', kind: 'model' },
      { id: 'permissions', label: 'Permissions', kind: 'select', options: [
        { value: '', label: 'Provider default' },
        { value: 'auto', label: 'Auto', set: { sandbox: 'workspace-write', approval: 'on-request' } },
        { value: 'full-access', label: 'Full access', tone: 'danger', help: 'Codex can change any file without asking.', set: { sandbox: 'danger-full-access', approval: 'never' } },
      ] },
      { id: 'effort', label: 'Effort', kind: 'select', per_model: true, options: ['', 'low', 'medium', 'high', 'xhigh'].map(value => ({ value, label: value || 'Provider default' })) },
    ],
  },
  { id: 'claude', label: 'Claude', available: true, version: '2.1.295' },
]
const personal: AgentAccount = { id: 'codex-personal', provider: 'codex', name: 'Personal', auth_mode: 'chatgpt', auth_status: 'logged_in', defaults: { model: 'gpt-6.1-sol', effort: 'high' } }
const team: AgentAccount = { id: 'codex-team', provider: 'codex', name: 'Team', auth_mode: 'device', auth_status: 'logged_in' }
const work: AgentAccount = { id: 'claude-work', provider: 'claude', name: 'Work', auth_status: 'logged_in' }
const models: AgentModel[] = [
  { id: 'gpt-6.1-sol', label: 'GPT-6.1-Sol', source: 'api', default: true, efforts: ['low', 'medium', 'high', 'xhigh'] },
  { id: 'gpt-6-luna', label: 'GPT-6-Luna', source: 'api', efforts: ['low', 'medium'] },
]
const agent = (id: string, rest: Partial<Agent> = {}): Agent => ({
  id, name: id, provider: 'Codex', providerId: 'codex', accountId: 'codex-personal', profileName: 'Personal', model: '', reasoningEffort: '', workType: '', prompt: '',
  archived: false, presentation: 'canvas', state: 'running', lifecycleState: 'running', task: 'running', runtime: '', terminalId: id,
  createdAt: new Date(Date.now() - 5 * 60_000).toISOString(), projectId: 'bonsai', worktreeId: 'wt-web', ...rest,
})

const props = (rest: Partial<AgentsPageProps> = {}): AgentsPageProps => ({ profileId: 'codex-personal', tab: 'defaults', onSelect: vi.fn(), onTabChange: vi.fn(), onShowOnCanvas: vi.fn(), ...rest })
const renderPage = async (rest: Partial<AgentsPageProps> = {}) => {
  const pageProps = props(rest)
  const view = render(<AgentsPage {...pageProps} />)
  await screen.findByRole('region', { name: 'Fleet summary' })
  return { ...pageProps, view }
}
const defaultsForm = () => screen.getByRole('form', { name: 'Profile defaults' })
const modelInput = () => within(defaultsForm()).getByRole('combobox', { name: 'Default model' })
// Permissions is a row of chips; effort is a slider of the model's levels.
const pick = (_name: string, option: string) => fireEvent.click(within(defaultsForm()).getByRole('radio', { name: option }))
const effortSlider = () => within(defaultsForm()).getByRole('slider', { name: 'Effort' })
const effortStops = () => Array.from(effortSlider().querySelectorAll('[data-stop]')).map(stop => stop.getAttribute('data-stop'))

beforeEach(() => {
  useBonsaiStore.setState({ ...useBonsaiStore.getInitialState(), projects, worktrees, activeProjectId: 'bonsai', activeWorkspaceId: 'personal', agents: [] }, true)
  vi.mocked(agentProviders).mockResolvedValue(providers)
  vi.mocked(agentModels).mockResolvedValue(models)
  vi.mocked(agentAccountsIfChanged).mockResolvedValue({ accounts: [personal, team, work], etag: '"v1"' })
  vi.mocked(agentUsage).mockResolvedValue([])
  vi.mocked(stopAgent).mockResolvedValue({} as never)
})
afterEach(() => { cleanup(); vi.clearAllMocks() })

describe('defaults tab', () => {
  it('shows the saved defaults and lists models for the profile', async () => {
    await renderPage()
    expect(modelInput()).toHaveValue('gpt-6.1-sol')
    // Nothing saved for permissions: the provider's default (the first option here) is the chosen chip, tagged.
    expect(within(defaultsForm()).getByRole('radio', { name: /^Auto/ })).toBeChecked()
    expect(within(within(defaultsForm()).getByRole('radiogroup', { name: 'Permissions' })).getByText('default')).toBeInTheDocument()
    expect(within(defaultsForm()).getByRole('button', { name: /Default · Auto/, pressed: true })).toBeInTheDocument()
    expect(effortSlider()).toHaveAttribute('aria-valuetext', 'High')
    expect(agentModels).toHaveBeenCalledWith('codex', 'codex-personal')
    expect(screen.getByText(/Every new launch of this profile starts with these choices/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Save defaults' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Reset' })).toBeDisabled()
  })

  it('sends only the changed fields, null for one set back to the provider default', async () => {
    const saved: AgentAccount = { ...personal, defaults: { model: 'gpt-6.1-sol', permissions: 'auto' } }
    vi.mocked(updateAgentProfile).mockResolvedValue(saved)
    await renderPage()
    pick('Permissions', 'Full access')
    expect(screen.getByRole('note')).toHaveTextContent('Codex can change any file without asking.')
    fireEvent.change(modelInput(), { target: { value: '' } })
    expect(screen.getByText('Unsaved changes')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Save defaults' }))
    await waitFor(() => expect(updateAgentProfile).toHaveBeenCalledWith('codex-personal', { defaults: { model: null, permissions: 'full-access' } }))
    expect(await screen.findByText('Defaults saved')).toBeInTheDocument()
  })

  it('resets to the saved values', async () => {
    await renderPage()
    fireEvent.change(modelInput(), { target: { value: 'gpt-6-luna' } })
    expect(screen.getByRole('button', { name: 'Save defaults' })).toBeEnabled()
    fireEvent.click(screen.getByRole('button', { name: 'Reset' }))
    expect(modelInput()).toHaveValue('gpt-6.1-sol')
    expect(screen.getByRole('button', { name: 'Save defaults' })).toBeDisabled()
  })

  it('offers the chosen model\'s efforts and falls back to the provider default when the new model lacks one', async () => {
    await renderPage()
    await waitFor(() => expect(agentModels).toHaveBeenCalled())
    await waitFor(() => expect(screen.getByRole('combobox', { name: 'Default model' })).not.toHaveAttribute('aria-busy'))
    fireEvent.change(modelInput(), { target: { value: 'gpt-6-luna' } })
    // The new model has no effort 'high': it falls back to the middle of the levels it offers (low of low/medium), not to nothing.
    expect(effortSlider()).toHaveAttribute('aria-valuetext', 'Low')
    expect(effortStops()).toEqual(['low', 'medium'])
  })

  it('opens a profile with no saved defaults on the provider defaults, not dirty, and saves only a real change', async () => {
    vi.mocked(updateAgentProfile).mockResolvedValue({ ...team, defaults: { effort: 'high' } })
    await renderPage({ profileId: 'codex-team' })
    // The default model is the one the provider flags; its default effort is not listed, so the middle level is used.
    await waitFor(() => expect(modelInput()).toHaveValue('gpt-6.1-sol'))
    expect(within(defaultsForm()).getByRole('radio', { name: /^Auto/ })).toBeChecked()
    expect(effortSlider()).toHaveAttribute('aria-valuetext', 'Medium')
    expect(within(defaultsForm()).getByRole('button', { name: /Default · Medium/, pressed: true })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Save defaults' })).toBeDisabled()
    expect(screen.queryByText('Unsaved changes')).not.toBeInTheDocument()

    fireEvent.click(effortSlider().querySelector('[data-stop="high"]') as HTMLElement)
    expect(screen.getByText('Unsaved changes')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Save defaults' }))
    await waitFor(() => expect(updateAgentProfile).toHaveBeenCalledWith('codex-team', { defaults: { effort: 'high' } }))
  })

  it('puts a saved choice back on the provider default with one click and saves it as null', async () => {
    vi.mocked(updateAgentProfile).mockResolvedValue({ ...personal, defaults: { model: 'gpt-6.1-sol' } })
    await renderPage()
    await waitFor(() => expect(modelInput()).toHaveValue('gpt-6.1-sol'))
    expect(effortSlider()).toHaveAttribute('aria-valuetext', 'High')
    fireEvent.click(within(defaultsForm()).getByRole('button', { name: /Default · Medium/ }))
    expect(effortSlider()).toHaveAttribute('aria-valuetext', 'Medium')
    fireEvent.click(screen.getByRole('button', { name: 'Save defaults' }))
    await waitFor(() => expect(updateAgentProfile).toHaveBeenCalledWith('codex-personal', { defaults: { effort: null } }))
  })

  it('shows the provider\'s message when a default is refused and keeps the edits', async () => {
    vi.mocked(updateAgentProfile).mockRejectedValue(new AgentProfileError('effort "ultra" is not supported by this model', 400, 'invalid_profile'))
    await renderPage()
    fireEvent.change(modelInput(), { target: { value: 'gpt-6-luna' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save defaults' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('effort "ultra" is not supported by this model')
    expect(modelInput()).toHaveValue('gpt-6-luna')
    expect(screen.getByRole('button', { name: 'Save defaults' })).toBeEnabled()
  })

  it('says so when the provider has no profile fields', async () => {
    await renderPage({ profileId: 'claude-work' })
    expect(screen.getByText('Claude profiles have no defaults to set.')).toBeInTheDocument()
  })
})

describe('unsaved changes guard', () => {
  it('asks before a profile or tab switch drops edits, and keeps them on "Keep editing"', async () => {
    const page = await renderPage()
    fireEvent.change(modelInput(), { target: { value: 'gpt-6-luna' } })
    fireEvent.click(screen.getByRole('button', { name: /^Team/ }))
    expect(screen.getByRole('dialog', { name: 'Discard unsaved defaults' })).toBeInTheDocument()
    expect(page.onSelect).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Keep editing' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(modelInput()).toHaveValue('gpt-6-luna')

    fireEvent.mouseDown(screen.getByRole('tab', { name: 'Overview' }), { button: 0 })
    expect(screen.getByRole('dialog', { name: 'Discard unsaved defaults' })).toBeInTheDocument()
    expect(page.onTabChange).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Discard changes' }))
    expect(page.onTabChange).toHaveBeenCalledWith('overview')
  })

  it('switches straight away when nothing is unsaved', async () => {
    const page = await renderPage()
    fireEvent.click(screen.getByRole('button', { name: /^Team/ }))
    expect(page.onSelect).toHaveBeenCalledWith('codex-team')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })
})

describe('rename', () => {
  const rename = (value: string) => {
    fireEvent.click(screen.getByRole('button', { name: 'Rename profile' }))
    const input = screen.getByRole('textbox', { name: 'Profile name' })
    fireEvent.change(input, { target: { value } })
    return input
  }

  it('saves on Enter and shows the renamed profile', async () => {
    vi.mocked(updateAgentProfile).mockResolvedValue({ ...personal, name: 'Main' })
    await renderPage({ tab: 'overview' })
    const input = rename('Main')
    fireEvent.keyDown(input, { key: 'Enter' })
    await waitFor(() => expect(updateAgentProfile).toHaveBeenCalledWith('codex-personal', { name: 'Main' }))
    expect(await screen.findByRole('heading', { level: 2, name: 'Main' })).toBeInTheDocument()
    expect(screen.queryByRole('textbox', { name: 'Profile name' })).not.toBeInTheDocument()
  })

  it('cancels on Escape without a request', async () => {
    await renderPage({ tab: 'overview' })
    const input = rename('Main')
    fireEvent.keyDown(input, { key: 'Escape' })
    expect(screen.getByRole('heading', { level: 2, name: 'Personal' })).toBeInTheDocument()
    expect(updateAgentProfile).not.toHaveBeenCalled()
  })

  it('rejects an invalid or taken name inline, before any request', async () => {
    await renderPage({ tab: 'overview' })
    const input = rename('bad name')
    expect(screen.getByRole('alert')).toHaveTextContent(/Names start with a letter or digit/)
    fireEvent.keyDown(input, { key: 'Enter' })
    fireEvent.change(input, { target: { value: 'Team' } })
    expect(screen.getByRole('alert')).toHaveTextContent('Another profile of this provider already has this name')
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(updateAgentProfile).not.toHaveBeenCalled()
  })

  it('shows the API\'s refusal and stays in edit mode', async () => {
    vi.mocked(updateAgentProfile).mockRejectedValue(new AgentProfileError('Another profile of this provider already has this name', 409, 'name_taken'))
    await renderPage({ tab: 'overview' })
    fireEvent.keyDown(rename('Fresh'), { key: 'Enter' })
    expect(await screen.findByRole('alert')).toHaveTextContent('already has this name')
    expect(screen.getByRole('textbox', { name: 'Profile name' })).toHaveValue('Fresh')
  })

  it('keeps the same name without a request', async () => {
    await renderPage({ tab: 'overview' })
    fireEvent.keyDown(rename('Personal'), { key: 'Enter' })
    expect(updateAgentProfile).not.toHaveBeenCalled()
    expect(screen.getByRole('heading', { level: 2, name: 'Personal' })).toBeInTheDocument()
  })
})

describe('remove', () => {
  const open = () => fireEvent.click(screen.getByRole('button', { name: /Remove profile…/ }))

  it('states what is deleted and removes the profile', async () => {
    vi.mocked(removeAgentProfile).mockResolvedValue({ warnings: [] })
    const page = await renderPage({ tab: 'overview' })
    open()
    const dialog = screen.getByRole('dialog', { name: 'Remove profile' })
    expect(dialog).toHaveTextContent('Remove Personal?')
    expect(dialog).toHaveTextContent("Codex home folder: its conversation history, session data and settings")
    fireEvent.click(within(dialog).getByRole('button', { name: 'Remove profile' }))
    await waitFor(() => expect(removeAgentProfile).toHaveBeenCalledWith('codex-personal'))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    // The list drops the profile and the selection moves to a remaining one of the provider.
    expect(page.onSelect).toHaveBeenCalledWith('codex-team')
    expect(screen.queryByRole('button', { name: /^Personal/ })).not.toBeInTheDocument()
  })

  it('shows provider cleanup warnings after removing', async () => {
    vi.mocked(removeAgentProfile).mockResolvedValue({ warnings: ['Could not delete the keychain entry'] })
    await renderPage({ tab: 'overview' })
    open()
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Remove profile' }))
    const done = await screen.findByRole('dialog', { name: 'Profile removed' })
    expect(within(done).getByText('Could not delete the keychain entry')).toBeInTheDocument()
    fireEvent.click(within(done).getByRole('button', { name: 'Close' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('lists the running sessions on 409 and removes after stopping them', async () => {
    useBonsaiStore.setState({ agents: [agent('s1', { name: 'Daemon cleanup' }), agent('s2', { name: 'Lab experiment', projectId: 'sprout-lab', worktreeId: 'wt-lab' })] })
    vi.mocked(removeAgentProfile)
      .mockRejectedValueOnce(new AgentProfileError('Stop the profile\'s running sessions before removing it', 409, 'profile_in_use', ['s1', 's2']))
      .mockRejectedValueOnce(new AgentProfileError('in use', 409, 'profile_in_use', ['s2']))
      .mockResolvedValueOnce({ warnings: [] })
    await renderPage({ tab: 'overview' })
    open()
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Remove profile' }))
    const list = await screen.findByRole('list', { name: 'Running sessions' })
    expect(within(list).getAllByRole('listitem').map(item => item.textContent)).toEqual(['Daemon cleanup', 'Lab experiment'])
    expect(removeAgentProfile).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByRole('button', { name: 'Stop 2 sessions and remove' }))
    await waitFor(() => expect(removeAgentProfile).toHaveBeenCalledTimes(3), { timeout: 3000 })
    expect(stopAgent).toHaveBeenCalledWith('bonsai', 's1', 'stop-s1')
    expect(stopAgent).toHaveBeenCalledWith('sprout-lab', 's2', 'stop-s2')
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('keeps the dialog open with a message when sessions do not stop in time', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    try {
      useBonsaiStore.setState({ agents: [agent('s1', { name: 'Daemon cleanup' })] })
      vi.mocked(removeAgentProfile).mockRejectedValue(new AgentProfileError('in use', 409, 'profile_in_use', ['s1']))
      await renderPage({ tab: 'overview' })
      open()
      fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Remove profile' }))
      fireEvent.click(await screen.findByRole('button', { name: 'Stop session and remove' }))
      await act(async () => { await vi.advanceTimersByTimeAsync(10_000) })
      expect(await screen.findByText('The sessions are still stopping. Try again in a moment.')).toBeInTheDocument()
      expect(screen.getByRole('dialog', { name: 'Remove profile' })).toBeInTheDocument()
    } finally {
      vi.useRealTimers()
    }
  })

  it('shows an unexpected failure and stays open', async () => {
    vi.mocked(removeAgentProfile).mockRejectedValue(new AgentProfileError('Cannot update profiles', 503, 'profiles_unavailable'))
    await renderPage({ tab: 'overview' })
    open()
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Remove profile' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Cannot update profiles')
    expect(screen.getByRole('dialog', { name: 'Remove profile' })).toBeInTheDocument()
  })

  it('closes on Cancel without a request', async () => {
    await renderPage({ tab: 'overview' })
    open()
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(removeAgentProfile).not.toHaveBeenCalled()
  })
})
