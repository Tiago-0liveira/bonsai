import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { agentProviders, stopAgent, type AgentAccount, type AgentCapability, type AgentUsage } from '../../api/agents'
import { agentAccountsIfChanged, agentUsage, refreshAgentUsage } from '../../api/agentsOverview'
import { useBonsaiStore } from '../../stores/bonsai'
import { projects } from '../../test/fixtures/projects'
import { worktrees } from '../../test/fixtures/worktrees'
import type { Agent, Worktree } from '../../types'
import { AgentsPage, type AgentsPageProps } from './AgentsPage'

vi.mock('../../api/agents', async importOriginal => ({ ...await importOriginal<object>(), agentProviders: vi.fn(), stopAgent: vi.fn() }))
vi.mock('../../api/agentsOverview', async importOriginal => ({
  ...await importOriginal<object>(),
  agentAccountsIfChanged: vi.fn(), agentUsage: vi.fn(), refreshAgentUsage: vi.fn(),
}))

const providers: AgentCapability[] = [
  { id: 'antigravity', label: 'Antigravity', available: false, unavailable_reason: { message: 'agy is not installed' }, install_hint: 'npm i -g agy' },
  { id: 'claude', label: 'Claude', available: true, version: '2.1.295', usage_note: 'Read from an undocumented endpoint; it may change.', launch_fields: [
    { id: 'effort', label: 'Effort', kind: 'select', options: [{ value: '', label: 'Profile default' }, { value: 'high', label: 'high' }] },
  ] },
  { id: 'codex', label: 'Codex', available: true, version: '0.162.1' },
]
const accounts: AgentAccount[] = [
  { id: 'codex-personal', provider: 'codex', name: 'Personal', auth_mode: 'chatgpt', identity: 'me@example.com · plus', auth_status: 'logged_in', created_at: '2026-10-01T10:00:00Z' },
  { id: 'codex-team', provider: 'codex', name: 'Team', auth_mode: 'device', identity: 'team@example.com · pro', auth_status: 'logged_out', warnings: ['Not logged in; run `bonsai agent run Team -- login`'] },
  { id: 'claude-work', provider: 'claude', name: 'Work', auth_mode: 'login', identity: 'dev@example.com', auth_status: 'logged_in', effort: 'high', model: 'opus' },
  { id: 'claude-token', provider: 'claude', name: 'CI token', auth_mode: 'token', auth_status: 'token' },
]
const inMinutes = (minutes: number) => new Date(Date.now() + minutes * 60_000 + 30_000).toISOString()
const usage: AgentUsage[] = [
  {
    account_id: 'codex-personal', provider: 'codex',
    usage: { provider: 'codex', account_id: 'codex-personal', fetched_at: new Date(Date.now() - 120_000).toISOString(), plan: 'plus', warnings: ['Credits balance is low'], limits: [] },
    headline: {
      primary: { id: 'five_hour', label: '5 hour', remaining_fraction: 0.72, resets_at: inMinutes(135) },
      secondary: { id: 'weekly', label: 'Weekly', remaining_fraction: 0.12, resets_at: inMinutes(3 * 24 * 60 + 4 * 60) },
      extra: [{ id: 'spark_five_hour', group: 'Codex Spark', label: '5 hour', remaining_fraction: 0.9 }],
      score: 0.12, low: true, next_reset: inMinutes(135),
    },
  },
  { account_id: 'codex-team', provider: 'codex', error: { code: 'not_authenticated', message: 'Not signed in' } },
  { account_id: 'claude-work', provider: 'claude', headline: { primary: { id: 'five_hour', label: '5 hour', remaining_fraction: 0.55 }, score: 0.55 } },
  { account_id: 'claude-token', provider: 'claude', error: { code: 'unsupported', message: 'Setup tokens cannot read plan usage' } },
]
const agent = (id: string, rest: Partial<Agent> = {}): Agent => ({
  id, name: id, provider: 'Codex', providerId: 'codex', accountId: 'codex-personal', profileName: 'Personal', model: '', reasoningEffort: '', workType: '', prompt: '',
  archived: false, presentation: 'canvas', state: 'running', lifecycleState: 'running', task: 'running', runtime: '', terminalId: id,
  createdAt: new Date(Date.now() - 5 * 60_000).toISOString(), projectId: 'bonsai', worktreeId: 'wt-web', ...rest,
})
const labWorktree: Worktree = { ...worktrees[0], id: 'wt-lab', projectId: 'sprout-lab', branch: 'lab/experiment', agentIds: [] }

const props = (rest: Partial<AgentsPageProps> = {}): AgentsPageProps => ({ onSelect: vi.fn(), onTabChange: vi.fn(), onShowOnCanvas: vi.fn(), ...rest })
const renderPage = async (rest: Partial<AgentsPageProps> = {}) => {
  const pageProps = props({ profileId: 'codex-personal', ...rest })
  render(<AgentsPage {...pageProps} />)
  await screen.findByRole('region', { name: 'Fleet summary' })
  return pageProps
}

beforeEach(() => {
  localStorage.clear()
  useBonsaiStore.setState({
    ...useBonsaiStore.getInitialState(), projects, worktrees: [...worktrees, labWorktree], activeProjectId: 'bonsai', activeWorkspaceId: 'personal',
    agents: [agent('s-running'), agent('s-lab', { projectId: 'sprout-lab', worktreeId: 'wt-lab' }), agent('s-done', { lifecycleState: 'exited', state: 'finished', exitCode: 0, finishedAt: new Date(Date.now() - 60_000).toISOString() }), agent('s-other', { accountId: 'claude-work', providerId: 'claude', provider: 'Claude' })],
  }, true)
  vi.mocked(agentProviders).mockResolvedValue(providers)
  vi.mocked(agentAccountsIfChanged).mockResolvedValue({ accounts, etag: '"v1"' })
  vi.mocked(agentUsage).mockResolvedValue(usage)
  vi.mocked(refreshAgentUsage).mockResolvedValue([])
  vi.mocked(stopAgent).mockResolvedValue({} as never)
})
afterEach(() => { cleanup(); vi.clearAllMocks() })

describe('status', () => {
  it('groups profiles by provider with versions, unavailable reasons and sign-in state', async () => {
    await renderPage()
    const list = screen.getByRole('complementary', { name: 'Providers and profiles' })
    const codex = within(within(list).getByRole('region', { name: 'Codex' }))
    expect(codex.getByText('v0.162.1')).toBeInTheDocument()
    const rows = within(codex.getByRole('list', { name: 'Codex profiles' })).getAllByRole('button')
    expect(rows.map(row => within(row).getByText(/Personal|Team/).textContent)).toEqual(['Personal', 'Team'])
    expect(rows[0]).toHaveTextContent('Signed in')
    expect(rows[1]).toHaveTextContent('Signed out')
    const antigravity = within(within(list).getByRole('region', { name: 'Antigravity' }))
    expect(antigravity.getByText('agy is not installed')).toBeInTheDocument()
    expect(antigravity.getByText('npm i -g agy')).toBeInTheDocument()
    expect(antigravity.getByText('No profiles')).toBeInTheDocument()
  })

  it('lists providers as Claude, Codex, Antigravity whatever order the API uses', async () => {
    await renderPage()
    const list = screen.getByRole('complementary', { name: 'Providers and profiles' })
    expect(within(list).getAllByRole('region').map(region => region.getAttribute('aria-label'))).toEqual(['Claude', 'Codex', 'Antigravity'])
  })

  it('collapses and expands a provider, remembers it, and keeps the profile list out of the page while collapsed', async () => {
    await renderPage({ profileId: 'codex-personal' })
    const list = screen.getByRole('complementary', { name: 'Providers and profiles' })
    const claude = within(within(list).getByRole('region', { name: 'Claude' }))
    const toggle = claude.getByRole('button', { name: /^Claude/ })
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    expect(claude.getByRole('list', { name: 'Claude profiles' })).toBeInTheDocument()

    fireEvent.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(claude.queryByRole('list', { name: 'Claude profiles' })).not.toBeInTheDocument()
    expect(claude.getByLabelText('2 profiles')).toBeInTheDocument()
    expect(JSON.parse(localStorage.getItem('bonsai.agents.collapsedProviders') ?? '[]')).toEqual(['claude'])

    fireEvent.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    expect(claude.getByRole('list', { name: 'Claude profiles' })).toBeInTheDocument()
    expect(JSON.parse(localStorage.getItem('bonsai.agents.collapsedProviders') ?? '[]')).toEqual([])
  })

  it('starts collapsed when it was left collapsed, and opens the group of the selected profile', async () => {
    localStorage.setItem('bonsai.agents.collapsedProviders', JSON.stringify(['claude', 'codex']))
    await renderPage({ profileId: 'codex-personal' })
    const list = screen.getByRole('complementary', { name: 'Providers and profiles' })
    expect(within(list).getByRole('button', { name: /^Claude/ })).toHaveAttribute('aria-expanded', 'false')
    // The selected profile's group opens so the selection is never hidden.
    expect(within(list).getByRole('button', { name: /^Codex/ })).toHaveAttribute('aria-expanded', 'true')
    expect(within(list).getByRole('list', { name: 'Codex profiles' })).toBeInTheDocument()
  })

  it('shows each provider at a glance in the summary: sign-ins, tightest headroom, running and reset', async () => {
    await renderPage()
    const summary = screen.getByRole('region', { name: 'Fleet summary' })
    await waitFor(() => expect(within(summary).getByRole('meter', { name: 'Codex tightest headroom remaining' })).toHaveAttribute('aria-valuenow', '12'))
    const tiles = within(within(summary).getByRole('list', { name: 'Providers at a glance' }))
    const codex = within(tiles.getByRole('listitem', { name: 'Codex' }))
    expect(codex.getByText('1/2 signed in')).toBeInTheDocument()
    expect(codex.getByText('12%')).toBeInTheDocument()
    expect(codex.getByText('2 running')).toBeInTheDocument()
    expect(codex.getByText(/resets in 2h 15m/)).toBeInTheDocument()
    const antigravity = within(tiles.getByRole('listitem', { name: 'Antigravity' }))
    expect(antigravity.getByText('Not installed')).toBeInTheDocument()
  })

  it('says how old the usage reading is and re-reads every profile on demand', async () => {
    await renderPage()
    const summary = screen.getByRole('region', { name: 'Fleet summary' })
    await waitFor(() => expect(within(summary).getByText(/Usage read 2m ago/)).toBeInTheDocument())
    fireEvent.click(within(summary).getByRole('button', { name: 'Check usage' }))
    await waitFor(() => expect(refreshAgentUsage).toHaveBeenCalledWith(undefined))
  })

  it('colours the sign-in dot by state', async () => {
    vi.mocked(agentAccountsIfChanged).mockResolvedValue({ accounts: [
      { id: 'a', provider: 'codex', name: 'A', auth_status: 'logged_in' },
      { id: 'b', provider: 'codex', name: 'B', auth_status: 'logged_out' },
      { id: 'c', provider: 'codex', name: 'C', auth_status: 'unknown' },
      { id: 'd', provider: 'codex', name: 'D' },
    ] })
    await renderPage({ profileId: 'a' })
    const dot = (name: string) => screen.getByRole('button', { name: new RegExp(`^${name} `) }).querySelector('[aria-hidden="true"]')
    expect(dot('A')).toHaveClass('bg-accent-solid')
    expect(dot('B')).toHaveClass('bg-danger-solid')
    expect(dot('C')).toHaveClass('bg-warn-solid')
    expect(dot('D')).toHaveClass('bg-muted-2')
  })

  it('shows running sessions per profile across projects and a fleet summary', async () => {
    await renderPage()
    const summary = screen.getByRole('region', { name: 'Fleet summary' })
    await waitFor(() => expect(summary).toHaveTextContent('Low headroom1'))
    expect(summary).toHaveTextContent('Running3')
    expect(summary).toHaveTextContent('Signed in3/4')
    expect(summary).toHaveTextContent('Lowest12%')
    expect(summary).toHaveTextContent('Next reset2h 15m')
    expect(within(screen.getByRole('button', { name: /Personal/ })).getByText('2 running')).toBeInTheDocument()
  })

  it('shows a mini usage bar on the profile row and tints it when low', async () => {
    await renderPage()
    const meter = await screen.findByRole('meter', { name: 'Personal headroom remaining' })
    expect(meter).toHaveAttribute('aria-valuenow', '12')
    expect(meter.firstElementChild).toHaveClass('bg-warn-solid')
    expect(screen.getByRole('meter', { name: 'Work headroom remaining' }).firstElementChild).toHaveClass('bg-accent-solid')
  })

  it('selects the first profile without a deep link, or the one named by it', async () => {
    // Providers keep the API order and profiles sort by name, so the first one is Claude's "CI token".
    await renderPage({ profileId: undefined })
    expect(screen.getByRole('heading', { level: 2, name: 'CI token' })).toBeInTheDocument()
    cleanup()
    await renderPage({ profileId: 'claude-work' })
    expect(screen.getByRole('heading', { level: 2, name: 'Work' })).toBeInTheDocument()
    cleanup()
    await renderPage({ profileId: 'gone' })
    expect(screen.getByRole('heading', { level: 2, name: 'CI token' })).toBeInTheDocument()
  })

  it('reports selection and tab changes to the route', async () => {
    const { onSelect, onTabChange } = await renderPage()
    fireEvent.click(screen.getByRole('button', { name: /Team/ }))
    expect(onSelect).toHaveBeenCalledWith('codex-team')
    fireEvent.mouseDown(screen.getByRole('tab', { name: 'Usage' }), { button: 0 })
    expect(onTabChange).toHaveBeenCalledWith('usage')
  })
})

describe('overview', () => {
  it('shows identity, warnings, defaults from the launch fields and sign in again', async () => {
    await renderPage({ profileId: 'claude-work' })
    const details = screen.getByRole('region', { name: 'Profile details' })
    expect(details).toHaveTextContent('dev@example.com')
    expect(within(details).getByRole('region', { name: 'Profile defaults' })).toHaveTextContent('Modelopus')
    expect(within(details).getByRole('region', { name: 'Profile defaults' })).toHaveTextContent('Efforthigh')
    expect(within(details).getByRole('button', { name: 'Sign in again…' })).toBeEnabled()
    // Only a signed-out profile's warnings offer to sign in right there.
    expect(within(details).queryByRole('button', { name: 'Sign in now' })).toBeNull()
    cleanup()
    await renderPage({ profileId: 'codex-team' })
    expect(screen.getByRole('list', { name: 'Warnings' })).toHaveTextContent('Not logged in')
    fireEvent.click(screen.getByRole('button', { name: 'Sign in now' }))
    expect(await screen.findByRole('dialog', { name: 'Sign in to Team again' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'Profile defaults' })).toHaveTextContent('None saved')
  })

  it('starts an agent with the profile preselected', async () => {
    await renderPage({ profileId: 'codex-team' })
    fireEvent.click(screen.getByRole('button', { name: 'Start agent' }))
    expect(useBonsaiStore.getState()).toMatchObject({ startAgentDialogOpen: true, startAgentTargetAccountId: 'codex-team', startAgentTargetWorktreeId: '' })
  })

  it('disables Start agent while the provider is unavailable', async () => {
    vi.mocked(agentAccountsIfChanged).mockResolvedValue({ accounts: [{ id: 'agy-1', provider: 'antigravity', name: 'Fixture' }] })
    await renderPage({ profileId: 'agy-1' })
    expect(screen.getByRole('button', { name: 'Start agent' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Start agent' })).toHaveAttribute('title', 'agy is not installed')
  })
})

describe('add profile', () => {
  it('opens the sign-in dialog on provider cards; unavailable providers are disabled with their reason', async () => {
    await renderPage()
    fireEvent.click(screen.getByRole('button', { name: 'Add profile' }))
    const dialog = await screen.findByRole('dialog', { name: 'Add profile' })
    const cards = within(within(dialog).getByRole('list', { name: 'Providers' }))
    expect(cards.getByRole('button', { name: /Antigravity/ })).toBeDisabled()
    expect(cards.getByRole('button', { name: /Antigravity/ })).toHaveTextContent('agy is not installed')
    expect(cards.getByRole('button', { name: /Antigravity/ })).toHaveTextContent('npm i -g agy')
    expect(cards.getByRole('button', { name: /Codex/ })).toHaveTextContent('v0.162.1 · 2 profiles')
    expect(cards.getByRole('button', { name: /Claude/ })).toBeEnabled()
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(screen.queryByRole('dialog', { name: 'Add profile' })).toBeNull()
  })
})

describe('empty states', () => {
  it('points at install hints when no provider is available', async () => {
    vi.mocked(agentProviders).mockResolvedValue([{ id: 'codex', label: 'Codex', available: false, unavailable_reason: { message: 'codex not found on PATH' }, install_hint: 'npm i -g @openai/codex' }])
    vi.mocked(agentAccountsIfChanged).mockResolvedValue({ accounts: [] })
    await renderPage()
    const details = screen.getByRole('region', { name: 'Profile details' })
    expect(details).toHaveTextContent('No agent provider is installed')
    expect(within(details).getByText('npm i -g @openai/codex')).toBeInTheDocument()
  })

  it('offers Add profile when providers exist but no profile does', async () => {
    vi.mocked(agentAccountsIfChanged).mockResolvedValue({ accounts: [] })
    await renderPage()
    const details = screen.getByRole('region', { name: 'Profile details' })
    expect(details).toHaveTextContent('No profiles yet')
    fireEvent.click(within(details).getByRole('button', { name: 'Add profile' }))
    expect(await screen.findByRole('dialog', { name: 'Add profile' })).toBeInTheDocument()
  })

  it('explains a failed first load and retries', async () => {
    vi.mocked(agentProviders).mockRejectedValueOnce(new Error('Update Bonsai to enable agent profiles.'))
    render(<AgentsPage {...props()} />)
    expect(await screen.findByRole('alert')).toHaveTextContent('Update Bonsai to enable agent profiles.')
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    await screen.findByRole('region', { name: 'Fleet summary' })
  })
})

describe('usage tab', () => {
  it('shows plan, headline bars, extra groups, countdowns, warnings and the provider note', async () => {
    await renderPage({ tab: 'usage' })
    await screen.findByText('Plan: plus')
    expect(screen.getByText(/Updated 2m ago/)).toBeInTheDocument()
    const limits = screen.getByRole('list', { name: 'Plan limits' })
    const five = within(limits).getByRole('meter', { name: '5 hour remaining' })
    expect(five).toHaveAttribute('aria-valuenow', '72')
    expect(within(limits).getByText('72%')).toBeInTheDocument()
    expect(within(limits).getByText('resets in 2h 15m')).toBeInTheDocument()
    expect(within(limits).getByText('resets in 3d 4h')).toBeInTheDocument()
    const weekly = within(limits).getByRole('meter', { name: 'Weekly remaining' })
    expect(weekly.firstElementChild).toHaveClass('bg-warn-solid')
    expect(five.firstElementChild).toHaveClass('bg-accent-solid')
    expect(within(screen.getByRole('region', { name: 'Codex Spark' })).getByRole('meter', { name: '5 hour remaining' })).toHaveAttribute('aria-valuenow', '90')
    expect(screen.getByText('Credits balance is low')).toBeInTheDocument()
    cleanup()
    await renderPage({ tab: 'usage', profileId: 'claude-work' })
    expect(await screen.findByText('Read from an undocumented endpoint; it may change.')).toBeInTheDocument()
  })

  it('shows the absolute reset time on hover', async () => {
    await renderPage({ tab: 'usage' })
    const reset = await screen.findByText('resets in 2h 15m')
    expect(reset).toHaveAttribute('title', new Date(usage[0].headline!.primary!.resets_at!).toLocaleString())
  })

  it('maps usage errors to what the user can do', async () => {
    await renderPage({ tab: 'usage', profileId: 'claude-token' })
    expect(await screen.findByText('Usage is not available for this profile')).toBeInTheDocument()
    expect(screen.getByText('Setup tokens cannot read plan usage')).toBeInTheDocument()
    cleanup()
    await renderPage({ tab: 'usage', profileId: 'codex-team' })
    expect(await screen.findAllByText('Not signed in')).toHaveLength(2)
  })

  it('retries an unavailable reading by refreshing that profile', async () => {
    vi.mocked(agentUsage).mockResolvedValue([{ account_id: 'codex-personal', provider: 'codex', error: { code: 'unavailable', message: 'Usage is unavailable right now' } }])
    await renderPage({ tab: 'usage' })
    fireEvent.click(await screen.findByRole('button', { name: 'Retry' }))
    expect(refreshAgentUsage).toHaveBeenCalledWith('codex-personal')
  })

  it('refreshes one profile or all, and labels a throttled answer with the saved reading age', async () => {
    await renderPage({ tab: 'usage' })
    await screen.findByText('Plan: plus')
    vi.mocked(refreshAgentUsage).mockResolvedValue([{ ...usage[0], throttled: true }])
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }))
    expect(refreshAgentUsage).toHaveBeenLastCalledWith('codex-personal')
    expect(await screen.findByRole('status')).toHaveTextContent('at most every 30 seconds')
    expect(screen.getByRole('status')).toHaveTextContent('from 2m ago')
    fireEvent.click(screen.getByRole('button', { name: 'Refresh all' }))
    expect(refreshAgentUsage).toHaveBeenLastCalledWith(undefined)
  })

  it('says so when the usage request fails as a whole', async () => {
    vi.mocked(agentUsage).mockRejectedValue(new Error('Update Bonsai to enable agent profiles.'))
    await renderPage({ tab: 'usage' })
    expect(await screen.findByRole('alert')).toHaveTextContent('Update Bonsai to enable agent profiles.')
  })
})

describe('sessions tab', () => {
  it('shows each session\'s launch details labelled by the provider\'s launch fields', async () => {
    useBonsaiStore.setState({ agents: [agent('s-launch', { accountId: 'claude-work', providerId: 'claude', provider: 'Claude', model: 'opus', launchOptions: { effort: 'high' } }), agent('s-plain', { accountId: 'claude-work', providerId: 'claude', provider: 'Claude' })] })
    await renderPage({ profileId: 'claude-work', tab: 'sessions' })
    const items = within(screen.getByRole('list', { name: 'Sessions' })).getAllByRole('listitem')
    expect(within(items.find(item => item.textContent?.includes('s-launch'))!).getByText('opus · high effort')).toBeInTheDocument()
    expect(items.find(item => item.textContent?.includes('s-plain'))).not.toHaveTextContent('effort')
  })

  it('lists the profile sessions of every project, running first', async () => {
    await renderPage({ tab: 'sessions' })
    const list = screen.getByRole('list', { name: 'Sessions' })
    const items = within(list).getAllByRole('listitem')
    expect(items.map(item => within(item).getByText(/^s-/).textContent)).toEqual(['s-running', 's-lab', 's-done'])
    expect(items[1]).toHaveTextContent('sprout-lab · lab/experiment')
    expect(items[2]).toHaveTextContent('Exited')
    expect(items[2]).toHaveTextContent('exit 0')
    expect(within(items[2]).getByRole('button', { name: 'Stop s-done' })).toBeDisabled()
  })

  it('opens the terminal of a session in another project and switches to it', async () => {
    await renderPage({ tab: 'sessions' })
    fireEvent.click(screen.getByRole('button', { name: 'Open terminal for s-lab' }))
    const state = useBonsaiStore.getState()
    expect(state.activeProjectId).toBe('sprout-lab')
    expect(state.openRuntimeIds).toContain('s-lab')
    expect(state.dockRuntimeId).toBe('s-lab')
    expect(state.dockState).toBe('normal')
  })

  it('stops a running session in its own project', async () => {
    await renderPage({ tab: 'sessions' })
    fireEvent.click(screen.getByRole('button', { name: 'Stop s-lab' }))
    expect(stopAgent).toHaveBeenCalledWith('sprout-lab', 's-lab', 'stop-s-lab')
  })

  it('shows a session on the canvas of its project', async () => {
    const { onShowOnCanvas } = await renderPage({ tab: 'sessions' })
    const before = useBonsaiStore.getState().canvasReveal.nonce
    fireEvent.click(screen.getByRole('button', { name: 'Show s-lab on canvas' }))
    const state = useBonsaiStore.getState()
    expect(state.selection).toEqual({ type: 'agent', id: 's-lab' })
    expect(state.activeProjectId).toBe('sprout-lab')
    expect(state.canvasReveal).toEqual({ projectId: 'sprout-lab', nodeId: 's-lab', nonce: before + 1 })
    expect(onShowOnCanvas).toHaveBeenCalledTimes(1)
  })

  it('shows failures and an empty state', async () => {
    useBonsaiStore.setState({ agents: [agent('s-bad', { lifecycleState: 'failed', state: 'finished', task: 'codex exited before it was ready', exitCode: 1 })] })
    await renderPage({ tab: 'sessions' })
    expect(screen.getByText('codex exited before it was ready')).toBeInTheDocument()
    cleanup()
    useBonsaiStore.setState({ agents: [] })
    await renderPage({ tab: 'sessions' })
    expect(screen.getByText(/No sessions yet/)).toBeInTheDocument()
  })
})

describe('freshness', () => {
  it('picks up a profile added elsewhere on the next revalidation', async () => {
    await renderPage()
    expect(screen.queryByRole('button', { name: /Late/ })).toBeNull()
    vi.mocked(agentAccountsIfChanged).mockResolvedValue({ accounts: [...accounts, { id: 'codex-late', provider: 'codex', name: 'Late' }], etag: '"v2"' })
    await act(async () => { window.dispatchEvent(new Event('focus')) })
    expect(await screen.findByRole('button', { name: /Late/ })).toBeInTheDocument()
  })
})
