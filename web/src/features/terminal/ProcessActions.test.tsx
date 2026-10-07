import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { ProcessActions } from './ProcessActions'
import { useBonsaiStore } from '../../stores/bonsai'
import { restartProcess, stopProcess } from '../../api/processes'
import { projects } from '../../test/fixtures/projects'
import { worktrees } from '../../test/fixtures/worktrees'
import type { Process } from '../../types'

vi.mock('../../api/processes', () => ({ restartProcess: vi.fn(), stopProcess: vi.fn() }))
const process: Process = { id: 'bonsai:42', projectId: 'bonsai', daemonId: 42, worktreeId: 'wt-web', name: 'server', command: 'run', lifecycleStatus: 'running', status: 'healthy' }
beforeEach(() => {
  vi.clearAllMocks()
  useBonsaiStore.setState({ ...useBonsaiStore.getInitialState(), projects, worktrees, processes: [process], activeProjectId: 'bonsai', dockWorktreeId: 'wt-web' }, true)
})
afterEach(cleanup)

it('opens the exact output only on an explicit action and selects its process node', () => {
  render(<ProcessActions process={process} />)
  expect(useBonsaiStore.getState().openRuntimeIds).toEqual([])
  fireEvent.click(screen.getByRole('button', { name: 'Open output' }))
  expect(useBonsaiStore.getState()).toMatchObject({ selection: { type: 'process', id: process.id }, dockRuntimeId: process.id, openRuntimeIds: [process.id] })
})

it('disables repeated actions while pending and restarts without opening a closed view', async () => {
  let finish: (value: never) => void = () => {}
  vi.mocked(restartProcess).mockReturnValue(new Promise(resolve => { finish = resolve }))
  render(<ProcessActions process={process} />)
  fireEvent.click(screen.getByRole('button', { name: 'Restart' }))
  expect(screen.getByRole('button', { name: 'Restarting…' })).toBeDisabled()
  expect(screen.getByRole('button', { name: 'Stop' })).toBeDisabled()
  expect(restartProcess).toHaveBeenCalledOnce()
  expect(restartProcess).toHaveBeenCalledWith('bonsai', 42)
  await act(async () => finish(undefined as never))
  expect(screen.getByRole('button', { name: 'Restart' })).toBeEnabled()
  expect(useBonsaiStore.getState().openRuntimeIds).toEqual([])
})

it('retains the process and reports a failed control request', async () => {
  vi.mocked(stopProcess).mockRejectedValue(new Error('daemon unavailable'))
  render(<ProcessActions process={process} />)
  fireEvent.click(screen.getByRole('button', { name: 'Stop' }))
  await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('daemon unavailable'))
  expect(useBonsaiStore.getState().processes).toEqual([process])
  expect(useBonsaiStore.getState().openRuntimeIds).toEqual([])
})

it('keeps retained failures available for output and restart while disabling stop', () => {
  render(<ProcessActions process={{ ...process, lifecycleStatus: 'failed' }} />)
  expect(screen.getByRole('button', { name: 'Stop' })).toBeDisabled()
  expect(screen.getByRole('button', { name: 'Restart' })).toBeEnabled()
  expect(screen.getByRole('button', { name: 'Open output' })).toBeEnabled()
})
