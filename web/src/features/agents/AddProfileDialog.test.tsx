import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { agentProviders, type AgentAccount, type AgentCapability } from '../../api/agents'
import { answerSetupSecret, cancelSetupJob, setupJob, startProfileLogin, startProfileSetup, type SetupJob } from '../../api/agentSetup'
import { AgentProfileError, agentAccountsIfChanged, agentUsage } from '../../api/agentsOverview'
import { useBonsaiStore } from '../../stores/bonsai'
import { AddProfileDialog, type AddProfileDialogProps } from './AddProfileDialog'
import { AgentsPage } from './AgentsPage'

vi.mock('../../api/agents', async importOriginal => ({ ...await importOriginal<object>(), agentProviders: vi.fn() }))
vi.mock('../../api/agentsOverview', async importOriginal => ({ ...await importOriginal<object>(), agentAccountsIfChanged: vi.fn(), agentUsage: vi.fn() }))
vi.mock('../../api/agentSetup', async importOriginal => ({
  ...await importOriginal<object>(),
  startProfileSetup: vi.fn(), startProfileLogin: vi.fn(), setupJob: vi.fn(), cancelSetupJob: vi.fn(), answerSetupSecret: vi.fn(),
}))
// The terminal is xterm over a websocket; tests drive the status frames it would pass on.
const terminal = vi.hoisted(() => ({ jobs: [] as string[], status: undefined as undefined | ((job: SetupJob) => void) }))
vi.mock('./SetupTerminal', () => ({
  SetupTerminal: ({ jobId, onStatus }: { jobId: string; onStatus: (job: SetupJob) => void }) => {
    terminal.jobs.push(jobId)
    terminal.status = onStatus
    return <div data-testid="setup-terminal">{jobId}</div>
  },
}))

const providers: AgentCapability[] = [
  { id: 'antigravity', label: 'Antigravity', available: false, unavailable_reason: { message: 'agy is not installed' }, setup: { notes: ['Exit agy after signing in'] } },
  { id: 'claude', label: 'Claude', available: true, version: '2.1.295', setup: {
    auth_modes: [
      { value: 'login', label: 'Browser sign-in', recommended: true },
      { value: 'token', label: 'Setup token', help: 'For CI', secret: { label: 'Claude setup token', optional: true } },
    ],
    seed: { source: '~/.claude' },
  } },
  { id: 'codex', label: 'Codex', available: true, version: '0.162.1', setup: {
    auth_modes: [
      { value: 'chatgpt', label: 'Browser sign-in' },
      { value: 'device', label: 'Device code', recommended: true, help: 'Works over SSH' },
      { value: 'api-key', label: 'API key', secret: { label: 'OpenAI API key', placeholder: 'sk-…' } },
    ],
    seed: { source: '~/.codex' },
  } },
]
const work: AgentAccount = { id: 'codex-work', provider: 'codex', name: 'work', auth_mode: 'chatgpt', auth_status: 'logged_out' }
const created: AgentAccount = { id: 'codex-new', provider: 'codex', name: 'ci', auth_mode: 'api-key', identity: 'API key', auth_status: 'logged_in' }
const job = (rest: Partial<SetupJob> = {}): SetupJob => ({ id: 'job-1', kind: 'create', provider: 'codex', name: 'ci', state: 'running', warnings: [], created_at: '2026-10-12T12:00:00Z', ...rest })

const props = (rest: Partial<AddProfileDialogProps> = {}): AddProfileDialogProps => ({
  target: { kind: 'create' }, providers, accounts: [work], onClose: vi.fn(), onSignedIn: vi.fn(), onStartAgent: vi.fn(), onSetDefaults: vi.fn(), ...rest,
})
const dialog = (name = 'Add profile') => screen.getByRole('dialog', { name })
const nameInput = () => screen.getByRole('textbox', { name: 'Profile name' })
const submit = () => fireEvent.click(screen.getByRole('button', { name: 'Sign in' }))
const pushStatus = (next: SetupJob) => act(() => terminal.status!(next))

beforeEach(() => {
  terminal.jobs = []
  terminal.status = undefined
  sessionStorage.clear()
  useBonsaiStore.setState({ ...useBonsaiStore.getInitialState(), agents: [], projects: [], worktrees: [] }, true)
})
afterEach(() => { cleanup(); vi.clearAllMocks() })

describe('details step', () => {
  it('validates the name and a required secret before anything is sent', async () => {
    render(<AddProfileDialog {...props()} />)
    const cards = within(screen.getByRole('list', { name: 'Providers' }))
    expect(cards.getByRole('button', { name: /Antigravity/ })).toBeDisabled()
    fireEvent.click(cards.getByRole('button', { name: /Codex/ }))

    // The recommended method is preselected and labelled; the seed toggle names its source.
    expect(screen.getByRole('radio', { name: /Device code/ })).toBeChecked()
    expect(within(screen.getByRole('radio', { name: /Device code/ }).closest('label')!).getByText('Recommended')).toBeInTheDocument()
    expect(screen.getByRole('checkbox', { name: /Copy settings from ~\/.codex/ })).toBeChecked()

    submit()
    expect(screen.getByRole('alert')).toHaveTextContent('Enter a name for the profile.')
    fireEvent.change(nameInput(), { target: { value: '-bad' } })
    expect(screen.getByRole('alert')).toHaveTextContent(/letter or digit/)
    fireEvent.change(nameInput(), { target: { value: 'work' } })
    expect(screen.getByRole('alert')).toHaveTextContent(/already has this name/)
    fireEvent.change(nameInput(), { target: { value: 'ci' } })
    fireEvent.click(screen.getByRole('radio', { name: /API key/ }))
    expect(screen.getByLabelText('OpenAI API key')).toHaveAttribute('type', 'password')
    submit()
    expect(screen.getByRole('alert')).toHaveTextContent('Paste the OpenAI API key.')
    expect(startProfileSetup).not.toHaveBeenCalled()

    // Back returns to the provider cards.
    fireEvent.click(screen.getByRole('button', { name: 'Back' }))
    expect(screen.getByRole('list', { name: 'Providers' })).toBeInTheDocument()
  })

  it('clears the secret on submit and keeps it out of the request key', async () => {
    vi.mocked(startProfileSetup).mockRejectedValueOnce(new Error('Network down')).mockResolvedValueOnce(job({ auth_mode: 'api-key' }))
    render(<AddProfileDialog {...props({ target: { kind: 'create', provider: 'codex' } })} />)
    fireEvent.change(nameInput(), { target: { value: 'ci' } })
    fireEvent.click(screen.getByRole('radio', { name: /API key/ }))
    fireEvent.change(screen.getByLabelText('OpenAI API key'), { target: { value: 'sk-first-secret' } })
    submit()
    expect(screen.getByLabelText('OpenAI API key')).toHaveValue('')
    expect(await screen.findByText('Network down')).toBeInTheDocument()
    const [firstBody, firstKey] = vi.mocked(startProfileSetup).mock.calls[0]
    expect(firstBody).toEqual({ provider: 'codex', name: 'ci', auth_mode: 'api-key', seed: true, secret: 'sk-first-secret' })

    // The retry pastes again; the same choices reuse the key, as the API fingerprints without the secret.
    fireEvent.change(screen.getByLabelText('OpenAI API key'), { target: { value: 'sk-second-secret' } })
    submit()
    await screen.findByTestId('setup-terminal')
    const [secondBody, secondKey] = vi.mocked(startProfileSetup).mock.calls[1]
    expect(secondBody.secret).toBe('sk-second-secret')
    expect(secondKey).toBe(firstKey)
    // Only the job ID is remembered for a reattach.
    expect(JSON.parse(sessionStorage.getItem('bonsai.agents.setupJob')!)).toEqual({ id: 'job-1', kind: 'create' })
    expect(JSON.stringify({ ...sessionStorage })).not.toContain('secret')
  })

  it('leaves a provider without seeding or methods to its notes and sends neither', async () => {
    vi.mocked(startProfileSetup).mockResolvedValue(job({ provider: 'antigravity' }))
    const available = providers.map(item => item.id === 'antigravity' ? { ...item, available: true } : item)
    render(<AddProfileDialog {...props({ providers: available, target: { kind: 'create', provider: 'antigravity' } })} />)
    expect(screen.queryByRole('radio')).toBeNull()
    expect(screen.queryByRole('checkbox')).toBeNull()
    expect(screen.getByRole('list', { name: 'Provider notes' })).toHaveTextContent('Exit agy after signing in')
    fireEvent.change(nameInput(), { target: { value: 'main' } })
    submit()
    await screen.findByTestId('setup-terminal')
    expect(vi.mocked(startProfileSetup).mock.calls[0][0]).toEqual({ provider: 'antigravity', name: 'main' })
  })

  it('shows the CLI command where the browser cannot sign in', async () => {
    vi.mocked(startProfileSetup).mockRejectedValue(new AgentProfileError('needs a terminal', 501, 'terminal_unsupported'))
    render(<AddProfileDialog {...props({ target: { kind: 'create', provider: 'codex' } })} />)
    fireEvent.change(nameInput(), { target: { value: 'ci' } })
    submit()
    expect(await screen.findByText('bonsai agent account add codex ci --auth device')).toBeInTheDocument()
  })

  it('shows the API refusal verbatim', async () => {
    vi.mocked(startProfileSetup).mockRejectedValue(new AgentProfileError('Two sign-ins are already running; finish or cancel one first', 409, 'setup_limit'))
    render(<AddProfileDialog {...props({ target: { kind: 'create', provider: 'codex' } })} />)
    fireEvent.change(nameInput(), { target: { value: 'ci' } })
    submit()
    expect(await screen.findByText('Two sign-ins are already running; finish or cancel one first')).toBeInTheDocument()
  })
})

describe('sign-in step', () => {
  const startClaudeToken = async (rest: Partial<AddProfileDialogProps> = {}) => {
    vi.mocked(startProfileSetup).mockResolvedValue(job({ provider: 'claude', auth_mode: 'token' }))
    const view = render(<AddProfileDialog {...props({ target: { kind: 'create', provider: 'claude' }, ...rest })} />)
    fireEvent.change(nameInput(), { target: { value: 'ci' } })
    fireEvent.click(screen.getByRole('radio', { name: /Setup token/ }))
    expect(screen.getByText('Leave empty to generate one in the terminal.')).toBeInTheDocument()
    submit()
    await screen.findByTestId('setup-terminal')
    expect(vi.mocked(startProfileSetup).mock.calls[0][0]).toEqual({ provider: 'claude', name: 'ci', auth_mode: 'token', seed: true })
    return view
  }

  it('answers a secret prompt, then shows the signed-in profile', async () => {
    vi.mocked(answerSetupSecret).mockResolvedValue()
    vi.mocked(setupJob).mockResolvedValue(job({ provider: 'claude', state: 'succeeded', account_id: created.id, warnings: ['Same login as profile "home"'], account: created }))
    const onSignedIn = vi.fn(), onStartAgent = vi.fn(), onClose = vi.fn()
    await startClaudeToken({ onSignedIn, onStartAgent, onClose })
    expect(screen.getByRole('status', { name: 'Sign-in status' })).toHaveTextContent('Waiting for the sign-in to finish')

    pushStatus(job({ provider: 'claude', state: 'waiting_secret', prompt: { label: 'Claude setup token' } }))
    const prompt = screen.getByRole('form', { name: 'Secret prompt' })
    const field = within(prompt).getByLabelText('Claude setup token')
    expect(field).toHaveAttribute('type', 'password')
    fireEvent.change(field, { target: { value: 'sk-ant-oat-123' } })
    fireEvent.click(within(prompt).getByRole('button', { name: 'Send' }))
    expect(field).toHaveValue('')
    await waitFor(() => expect(answerSetupSecret).toHaveBeenCalledWith('job-1', 'sk-ant-oat-123'))
    await waitFor(() => expect(screen.queryByRole('form', { name: 'Secret prompt' })).toBeNull())

    pushStatus(job({ provider: 'claude', state: 'verifying' }))
    expect(screen.getByRole('status', { name: 'Sign-in status' })).toHaveTextContent('Checking the sign-in')
    pushStatus(job({ provider: 'claude', state: 'succeeded', account_id: created.id }))
    expect(await screen.findByText('ci is ready.')).toBeInTheDocument()
    expect(setupJob).toHaveBeenCalledWith('job-1')
    expect(onSignedIn).toHaveBeenCalledWith(created)
    expect(screen.getByText('API key')).toBeInTheDocument()
    expect(screen.getByRole('list', { name: 'Warnings' })).toHaveTextContent('Same login as profile "home"')
    expect(sessionStorage.getItem('bonsai.agents.setupJob')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Start agent with this profile' }))
    expect(onClose).toHaveBeenCalled()
    expect(onStartAgent).toHaveBeenCalledWith(created.id)
  })

  it('cancels after a confirmation and offers another try', async () => {
    vi.mocked(cancelSetupJob).mockResolvedValue(job({ provider: 'claude', state: 'canceled' }))
    vi.mocked(startProfileSetup).mockResolvedValue(job({ provider: 'claude' }))
    await startClaudeToken()
    fireEvent.click(screen.getByRole('button', { name: 'Cancel sign-in…' }))
    const confirm = screen.getByRole('alertdialog', { name: 'Cancel sign-in' })
    expect(confirm).toHaveTextContent('the new profile is removed')
    fireEvent.click(within(confirm).getByRole('button', { name: 'Keep signing in' }))
    expect(cancelSetupJob).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Cancel sign-in…' }))
    fireEvent.click(screen.getByRole('button', { name: 'Cancel sign-in' }))
    await waitFor(() => expect(cancelSetupJob).toHaveBeenCalledWith('job-1'))
    expect(await screen.findByText('The sign-in was canceled.')).toBeInTheDocument()

    // Another try is a new request.
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    submit()
    await waitFor(() => expect(startProfileSetup).toHaveBeenCalledTimes(2))
    expect(vi.mocked(startProfileSetup).mock.calls[1][1]).not.toBe(vi.mocked(startProfileSetup).mock.calls[0][1])
  })

  it('asks before closing a running sign-in and cancels it when told to', async () => {
    vi.mocked(cancelSetupJob).mockResolvedValue(job({ provider: 'claude', state: 'canceled' }))
    const onClose = vi.fn()
    await startClaudeToken({ onClose })
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(onClose).not.toHaveBeenCalled()
    fireEvent.click(within(screen.getByRole('alertdialog', { name: 'Cancel sign-in' })).getByRole('button', { name: 'Cancel sign-in' }))
    await waitFor(() => expect(onClose).toHaveBeenCalled())
    expect(cancelSetupJob).toHaveBeenCalledWith('job-1')
  })

  it('shows a failure with the job error', async () => {
    await startClaudeToken()
    pushStatus(job({ provider: 'claude', state: 'failed', error: 'claude setup-token exited with status 1' }))
    expect(screen.getByRole('alert')).toHaveTextContent('claude setup-token exited with status 1')
    expect(screen.getByTestId('setup-terminal')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Close' })).toBeInTheDocument()
  })
})

describe('sign in again', () => {
  it('starts on the method picker and sends a method only when it changes', async () => {
    vi.mocked(startProfileLogin).mockResolvedValue(job({ kind: 'login', account_id: work.id, name: 'work' }))
    render(<AddProfileDialog {...props({ target: { kind: 'login', account: work } })} />)
    expect(dialog('Sign in to work again')).toBeInTheDocument()
    expect(screen.queryByRole('textbox', { name: 'Profile name' })).toBeNull()
    expect(screen.queryByRole('checkbox')).toBeNull()
    expect(screen.getByRole('radio', { name: /Browser sign-in/ })).toBeChecked()
    submit()
    await screen.findByTestId('setup-terminal')
    expect(vi.mocked(startProfileLogin).mock.calls[0].slice(0, 2)).toEqual(['codex-work', { secret: undefined }])
    expect(JSON.parse(sessionStorage.getItem('bonsai.agents.setupJob')!)).toEqual({ id: 'job-1', kind: 'login', accountId: 'codex-work' })
    cleanup()

    render(<AddProfileDialog {...props({ target: { kind: 'login', account: work } })} />)
    fireEvent.click(screen.getByRole('radio', { name: /Device code/ }))
    submit()
    await waitFor(() => expect(startProfileLogin).toHaveBeenCalledTimes(2))
    expect(vi.mocked(startProfileLogin).mock.calls[1][1]).toEqual({ auth_mode: 'device', secret: undefined })
  })
})

describe('reattach', () => {
  const renderPage = () => render(<AgentsPage profileId="codex-work" onSelect={vi.fn()} onTabChange={vi.fn()} onShowOnCanvas={vi.fn()} />)
  beforeEach(() => {
    vi.mocked(agentProviders).mockResolvedValue(providers)
    vi.mocked(agentAccountsIfChanged).mockResolvedValue({ accounts: [work] })
    vi.mocked(agentUsage).mockResolvedValue([])
  })

  it('reopens a running sign-in kept in sessionStorage', async () => {
    sessionStorage.setItem('bonsai.agents.setupJob', JSON.stringify({ id: 'job-7', kind: 'login', accountId: 'codex-work' }))
    vi.mocked(setupJob).mockResolvedValue(job({ id: 'job-7', kind: 'login', account_id: 'codex-work', name: 'work', state: 'waiting_secret', prompt: { label: 'OpenAI API key' } }))
    renderPage()
    expect(await screen.findByRole('dialog', { name: 'Sign in to work again' })).toBeInTheDocument()
    expect(setupJob).toHaveBeenCalledWith('job-7')
    expect(await screen.findByTestId('setup-terminal')).toHaveTextContent('job-7')
    expect(screen.getByRole('form', { name: 'Secret prompt' })).toBeInTheDocument()
  })

  it('forgets a sign-in the API no longer has', async () => {
    sessionStorage.setItem('bonsai.agents.setupJob', JSON.stringify({ id: 'job-gone', kind: 'create' }))
    vi.mocked(setupJob).mockRejectedValue(new AgentProfileError('Setup job not found', 404, 'not_found'))
    renderPage()
    expect(await screen.findByText('This sign-in has ended and is no longer available.')).toBeInTheDocument()
    expect(sessionStorage.getItem('bonsai.agents.setupJob')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('works when sessionStorage is unavailable', async () => {
    const getItem = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('blocked') })
    renderPage()
    await screen.findByRole('region', { name: 'Fleet summary' })
    expect(screen.queryByRole('dialog')).toBeNull()
    getItem.mockRestore()
  })
})
