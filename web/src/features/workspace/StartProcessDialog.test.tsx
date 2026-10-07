import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { StartProcessDialog } from './StartProcessDialog'
import { useBonsaiStore } from '../../stores/bonsai'
import { applyProcessSummary, previewProcess, processCommands, startProcess, type ProcessCatalog } from '../../api/processes'
import { prepareProcessStream } from '../../api/processStream'
import { worktrees } from '../../test/fixtures/worktrees'
vi.mock('../../components/ui/BonsaiSelect', () => ({ BonsaiSelect: ({ ariaLabel, value, options, onChange, disabled }: { ariaLabel: string; value: string; options: { value: string; label: string }[]; onChange(value: string): void; disabled?: boolean }) => <select aria-label={ariaLabel} value={value} disabled={disabled} onChange={event => onChange(event.target.value)}><option value="">Choose</option>{options.map(option => <option key={option.value} value={option.value}>{option.label}</option>)}</select> }))
vi.mock('../../api/processes', () => ({ processCommands: vi.fn(), previewProcess: vi.fn(), startProcess: vi.fn(), applyProcessSummary: vi.fn() }))
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
})
afterEach(() => { cleanup(); useBonsaiStore.setState({ startProcessDialogOpen: false }) })
async function chooseCommand(id = 'one') {
  await waitFor(() => expect(screen.getByLabelText('Process command')).toBeEnabled())
  fireEvent.change(screen.getByLabelText('Process command'), { target: { value: id } })
  await waitFor(() => expect(screen.getByRole('button', { name: 'Start process' })).toBeEnabled())
}
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
  fireEvent.change(screen.getByLabelText('Restart policy'), { target: { value: 'always' } })
  fireEvent.change(screen.getByLabelText('Maximum retries'), { target: { value: '0' } })
  fireEvent.change(screen.getByLabelText('Process command'), { target: { value: 'two' } })
  await waitFor(() => expect(screen.getByRole('button', { name: 'Start process' })).toBeEnabled())
  fireEvent.click(screen.getByRole('button', { name: 'Refresh' }))
  await chooseCommand('two')
  expect(screen.getByLabelText('Restart policy')).toHaveValue('always')
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
  expect(screen.getByLabelText('Process command')).toHaveValue('one')
  fireEvent.change(screen.getByLabelText('Restart policy'), { target: { value: 'on-failure' } })
  fireEvent.change(screen.getByLabelText('Maximum retries'), { target: { value: '-1' } })
  expect(screen.getByRole('button', { name: 'Start process' })).toBeDisabled()
})
