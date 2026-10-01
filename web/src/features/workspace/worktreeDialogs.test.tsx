import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useBonsaiStore } from '../../stores/bonsai'
import { projects } from '../../test/fixtures/projects'
import { worktrees } from '../../test/fixtures/worktrees'
import { CreateWorktreeDialog } from './CreateWorktreeDialog'
import { DeleteWorktreeDialog } from './DeleteWorktreeDialog'
import * as gitAPI from '../../api/git'

const realCreate = useBonsaiStore.getState().createWorktree
describe('worktree dialogs', () => {
  beforeEach(() => {
    useBonsaiStore.setState({ projects, activeProjectId: 'bonsai', worktrees: [], agents: [], processes: [], pullRequests: [], branchCandidates: {}, gitBranches: { bonsai: [{ name: 'main', remote: false }, { name: 'origin/feature', remote: true }] }, worktreeDialogOpen: false, worktreeDialogTarget: null, deleteWorktreeId: '' })
  })
  afterEach(() => { cleanup(); vi.restoreAllMocks(); useBonsaiStore.setState({ createWorktree: realCreate }) })
  it('prefills creation from a branch candidate and preserves the source through live inventory changes', () => {
    const candidate = { id: 'refs/remotes/origin/feature', ref: 'refs/remotes/origin/feature', name: 'feature', source: 'remote' as const, remote: 'origin', worktree_ids: [], pull_requests: [], creation_mode: 'remote' as const, source_ref: 'origin/feature' }
    useBonsaiStore.getState().openCreateWorktree('bonsai', candidate)
    render(<CreateWorktreeDialog />)
    expect(screen.getByRole('button', { name: 'Remote branch' })).toHaveTextContent('origin/feature')
    act(() => { useBonsaiStore.setState({ gitBranches: { bonsai: [{ name: 'origin/another', remote: true }, { name: 'origin/feature', remote: true }] } }) })
    expect(screen.getByRole('button', { name: 'Remote branch' })).toHaveTextContent('origin/feature')
  })
  it('blocks duplicate submission and shows actionable inline errors', async () => {
    let reject!: (reason: Error) => void
    const create = vi.fn(() => new Promise<string>((_, fail) => { reject = fail }))
    useBonsaiStore.setState({ createWorktree: create })
    useBonsaiStore.getState().setWorktreeDialogOpen(true)
    render(<CreateWorktreeDialog />)
    fireEvent.click(screen.getByRole('button', { name: 'Create worktree' }))
    expect(screen.getByRole('button', { name: 'Creating…' })).toBeDisabled()
    await act(async () => reject(new Error('branch is already checked out')))
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('branch is already checked out'))
    expect(create).toHaveBeenCalledTimes(1)
  })
  it('requires explicit discard for dirty deletion and protects the main copy', () => {
    const tree = { ...worktrees[0], id: 'dirty', main: false, dirtyFiles: 2, gitState: 'normal', path: '/trees/dirty' }
    useBonsaiStore.setState({ worktrees: [tree], deleteWorktreeId: tree.id })
    render(<DeleteWorktreeDialog />)
    expect(screen.getByRole('button', { name: 'Delete worktree' })).toBeDisabled()
    fireEvent.click(screen.getByRole('checkbox'))
    expect(screen.getByRole('button', { name: 'Delete worktree' })).toBeEnabled()
    act(() => { useBonsaiStore.setState({ worktrees: [{ ...tree, main: true }] }) })
    expect(screen.getByRole('button', { name: 'Delete worktree' })).toBeDisabled()
  })
  it('reuses deletion identity after a lost response', async () => {
    const tree = { ...worktrees[0], id: 'retry-delete', main: false, dirtyFiles: 0, gitState: 'normal', path: '/trees/retry' }
    useBonsaiStore.setState({ worktrees: [tree], deleteWorktreeId: tree.id })
    const remove = vi.spyOn(gitAPI, 'deleteWorktree').mockRejectedValueOnce(new TypeError('connection lost')).mockRejectedValueOnce(new gitAPI.APIError('outcome_unknown', 'Confirming deletion'))
    render(<DeleteWorktreeDialog />)
    fireEvent.click(screen.getByRole('button', { name: 'Delete worktree' }))
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('connection lost'))
    fireEvent.click(screen.getByRole('button', { name: 'Delete worktree' }))
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('Confirming deletion'))
    expect(remove).toHaveBeenCalledTimes(2)
    expect(remove.mock.calls[1]).toEqual(remove.mock.calls[0])
  })
})
