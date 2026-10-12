import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { StartProcessDialog } from './StartProcessDialog'
import { useBonsaiStore } from '../../stores/bonsai'
import { applyProcessSummary, previewCustomProcess, previewProcess, processCommands, startCustomProcess, startProcess, type ProcessCatalog } from '../../api/processes'
import { prepareProcessStream } from '../../api/processStream'
import { worktrees } from '../../test/fixtures/worktrees'
vi.mock('../../components/ui/BonsaiSelect', () => ({ BonsaiSelect: ({ ariaLabel, value, options, onChange, disabled }: { ariaLabel: string; value: string; options: { value: string; label: string }[]; onChange(value: string): void; disabled?: boolean }) => <select aria-label={ariaLabel} value={value} disabled={disabled} onChange={event => onChange(event.target.value)}><option value="">Choose</option>{options.map(option => <option key={option.value} value={option.value}>{option.label}</option>)}</select> }))
vi.mock('../../api/processes', () => ({ processCommands: vi.fn(), previewProcess: vi.fn(), startProcess: vi.fn(), previewCustomProcess: vi.fn(), startCustomProcess: vi.fn(), applyProcessSummary: vi.fn() }))
vi.mock('../../api/processStream', () => ({ prepareProcessStream: vi.fn() }))
const catalog: ProcessCatalog = {
  location: { input_dir: '/repo' }, providers: [{ id: 'node:pnpm', name: 'pnpm', root: '/repo' }],
  default_policies: { one: { mode: 'on-failure', max_restarts: 5 } },
  commands: ['one', 'two'].map(id => ({ id, name: id, provider: 'node:pnpm', invocation: { program: 'pnpm', prefix: ['run', id], working_dir: '/repo', pass_through: 'append' }, args: [{ id: 'args', name: 'arguments', kind: 'passthrough', type: 'unknown', variadic: true }] })),
}
beforeEach(() => {
  vi.clearAllMocks()
  useBonsaiStore.setState({ startProcessDialogOpen: true, startProcessTargetProjectId: 'bonsai', startProcessTargetWorktreeId: 'wt-web', worktrees })
  vi.mocked(processCommands).mockResolvedValue(catalog)
  vi.mocked(previewProcess).mockImplementation(async (_p, _w, command, values) => ({ program: 'pnpm', args: ['run', command, ...(values.args ?? [])], dir: '/repo', default_policy: { mode: 'on-failure', max_restarts: 5 } }))
  vi.mocked(previewCustomProcess).mockImplementation(async (_p, _w, custom) => ({ program: 'sh', args: ['-c', custom.command], dir: '/repo' + (custom.dir ? '/' + custom.dir : ''), default_policy: { mode: 'no', max_restarts: 0 } }))
})
afterEach(() => { cleanup(); useBonsaiStore.setState({ startProcessDialogOpen: false }) })
async function chooseCommand(id = 'one') {
  fireEvent.click(await screen.findByRole('option', { name: id }))
  await waitFor(() => expect(screen.getByRole('button', { name: 'Start process' })).toBeEnabled())
}
const policy = (name: string) => screen.getByRole('button', { name })
it('shows authoritative argument preview, omits inherited policy and records launch intent without a detached stream', async () => {
  const summary = { id: 'bonsai:1', project_id: 'bonsai', daemon_id: 1, label: 'one', command: 'pnpm run one', status: 'failed' as const }
  vi.mocked(startProcess).mockResolvedValue(summary)
  render(<StartProcessDialog />)
  await chooseCommand()
  fireEvent.change(screen.getByLabelText('arguments'), { target: { value: 'dev\nargument with spaces' } })
  await waitFor(() => expect(screen.getByText('pnpm run one dev "argument with spaces"')).toBeVisible())
  fireEvent.click(screen.getByRole('button', { name: 'Start process' }))
  await waitFor(() => expect(startProcess).toHaveBeenCalledWith('bonsai', 'wt-web', 'one', { args: ['dev', 'argument with spaces'] }, undefined))
  expect(applyProcessSummary).toHaveBeenCalledWith(summary, true)
  expect(prepareProcessStream).not.toHaveBeenCalled()
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
})
it('keeps explicit policy through command/discovery changes and sends an explicit zero retry limit', async () => {
  vi.mocked(startProcess).mockRejectedValue(new Error('maximum retries rejected by server'))
  render(<StartProcessDialog />)
  await chooseCommand()
  fireEvent.click(policy('Always'))
  fireEvent.change(screen.getByLabelText('Maximum retries'), { target: { value: '0' } })
  fireEvent.click(screen.getByRole('option', { name: 'two' }))
  await waitFor(() => expect(screen.getByRole('button', { name: 'Start process' })).toBeEnabled())
  fireEvent.click(screen.getByRole('button', { name: 'Refresh' }))
  await chooseCommand('two')
  expect(policy('Always')).toHaveAttribute('aria-pressed', 'true')
  expect(screen.getByLabelText('Maximum retries')).toHaveValue(0)
  fireEvent.click(screen.getByRole('button', { name: 'Start process' }))
  await waitFor(() => expect(startProcess).toHaveBeenCalledWith('bonsai', 'wt-web', 'two', {}, { mode: 'always', max_restarts: 0 }))
  await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('maximum retries rejected by server'))
  expect(screen.getByRole('dialog')).toBeVisible()
  expect(applyProcessSummary).not.toHaveBeenCalled()
})
it('does not discard discovery when reselecting the current worktree and rejects invalid retry limits', async () => {
  render(<StartProcessDialog />)
  await chooseCommand()
  fireEvent.change(screen.getByLabelText('Process worktree and branch'), { target: { value: 'wt-web' } })
  expect(screen.getByRole('option', { name: 'one' })).toHaveAttribute('aria-selected', 'true')
  fireEvent.click(policy('On failure'))
  fireEvent.change(screen.getByLabelText('Maximum retries'), { target: { value: '-1' } })
  expect(screen.getByRole('button', { name: 'Start process' })).toBeDisabled()
})

it('shows the inherited policy read-only and hides retries when the policy is never', async () => {
  render(<StartProcessDialog />)
  await chooseCommand()
  expect(screen.getByLabelText('Maximum retries')).toHaveValue(5)
  expect(screen.getByLabelText('Maximum retries')).toHaveAttribute('readonly')
  expect(screen.getByText('Project default: on-failure · maximum 5 retries')).toBeVisible()
  fireEvent.click(policy('Never'))
  expect(screen.queryByLabelText('Maximum retries')).not.toBeInTheDocument()
})
it('runs a typed command in a chosen directory with the previewed default policy', async () => {
  const summary = { id: 'bonsai:7', project_id: 'bonsai', daemon_id: 7, label: 'Dev server', command: 'pnpm dev --port 4000', status: 'failed' as const }
  vi.mocked(startCustomProcess).mockResolvedValue(summary)
  vi.mocked(processCommands).mockResolvedValue({ ...catalog, providers: [{ id: 'node:pnpm', name: 'pnpm', root: '/repo' }, { id: 'go', name: 'go', root: '/repo/web' }] })
  render(<StartProcessDialog />)
  await screen.findByRole('option', { name: 'one' }).catch(() => undefined)
  fireEvent.click(screen.getByRole('button', { name: /Custom command/ }))
  const start = screen.getByRole('button', { name: 'Start process' })
  expect(start).toBeDisabled()
  fireEvent.change(screen.getByLabelText('Command'), { target: { value: '  pnpm dev --port 4000  ' } })
  fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Dev server' } })
  fireEvent.click(await screen.findByRole('radio', { name: 'web' }))
  expect(screen.getByLabelText('Directory')).toHaveValue('web')
  await waitFor(() => expect(previewCustomProcess).toHaveBeenLastCalledWith('bonsai', 'wt-web', { command: 'pnpm dev --port 4000', name: 'Dev server', dir: 'web' }))
  await waitFor(() => expect(screen.getByText('Project default: no · maximum 0 retries')).toBeVisible())
  expect(screen.getByText('pnpm dev --port 4000', { selector: 'code' })).toBeVisible()
  await waitFor(() => expect(start).toBeEnabled())
  fireEvent.click(start)
  await waitFor(() => expect(startCustomProcess).toHaveBeenCalledWith('bonsai', 'wt-web', { command: 'pnpm dev --port 4000', name: 'Dev server', dir: 'web' }, undefined))
  expect(startProcess).not.toHaveBeenCalled()
  expect(applyProcessSummary).toHaveBeenCalledWith(summary, true)
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
})
it('refuses a typed command the server rejects and sends an explicit policy with it', async () => {
  vi.mocked(previewCustomProcess).mockRejectedValueOnce(new Error('directory must stay inside the worktree'))
  vi.mocked(startCustomProcess).mockRejectedValue(new Error('process launcher unavailable'))
  render(<StartProcessDialog />)
  fireEvent.click(screen.getByRole('button', { name: /Custom command/ }))
  fireEvent.change(screen.getByLabelText('Command'), { target: { value: 'ls' } })
  fireEvent.change(screen.getByLabelText('Directory'), { target: { value: '../x' } })
  await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('directory must stay inside the worktree'))
  expect(screen.getByRole('button', { name: 'Start process' })).toBeDisabled()
  fireEvent.change(screen.getByLabelText('Directory'), { target: { value: '' } })
  await waitFor(() => expect(screen.getByRole('button', { name: 'Start process' })).toBeEnabled())
  fireEvent.click(policy('Always'))
  fireEvent.change(screen.getByLabelText('Maximum retries'), { target: { value: '3' } })
  fireEvent.click(screen.getByRole('button', { name: 'Start process' }))
  await waitFor(() => expect(startCustomProcess).toHaveBeenCalledWith('bonsai', 'wt-web', { command: 'ls', name: undefined, dir: undefined }, { mode: 'always', max_restarts: 3 }))
  await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('process launcher unavailable'))
  expect(screen.getByRole('dialog')).toBeVisible()
})
