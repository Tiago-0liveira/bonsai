import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { RebaseDialog } from './RebaseDialog'
import { useBonsaiStore } from '../../stores/bonsai'
import { projects } from '../../test/fixtures/projects'
import { worktrees } from '../../test/fixtures/worktrees'

const { localFetch } = vi.hoisted(() => ({ localFetch: vi.fn() }))
vi.mock('../../api/local', () => ({ localFetch, openLocalEvents: vi.fn(), sendEventFocus: vi.fn(), invalidateLocalSession: vi.fn(), markLocalConnectionLost: vi.fn() }))

beforeEach(() => {
  localFetch.mockReset().mockResolvedValue(new Response(JSON.stringify({ result: { state: 'completed' } }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
  useBonsaiStore.setState({
    projects, worktrees, notice: '', rebaseWorktreeId: 'wt-docs',
    gitBranches: { bonsai: [{ name: 'main', remote: false }, { name: 'feat/web-workspace', remote: false }, { name: 'feat/workspace-docs', remote: false }, { name: 'origin/release', ref: 'refs/remotes/origin/release', remote: true }] },
  })
})
afterEach(cleanup)

it('rebases onto any branch, starting from the merge target', async () => {
  const docs = worktrees.find(tree => tree.id === 'wt-docs')!
  render(<RebaseDialog />)
  expect(screen.getByRole('button', { name: 'Rebase onto' })).toHaveTextContent(docs.mergeTargetBranch)
  fireEvent.click(screen.getByRole('button', { name: 'Rebase onto' }))
  // The worktree's own branch is not a target.
  expect(screen.queryByRole('option', { name: new RegExp('^' + docs.branch) })).toBeNull()
  fireEvent.click(screen.getByRole('option', { name: /^origin\/release/ }))
  fireEvent.click(screen.getByRole('button', { name: 'Rebase onto origin/release' }))
  await waitFor(() => expect(useBonsaiStore.getState().rebaseWorktreeId).toBe(''))
  const [path, init] = localFetch.mock.calls[0]
  expect(path).toBe('/api/worktrees/wt-docs/rebase')
  expect(JSON.parse(init.body)).toEqual({ target: 'refs/remotes/origin/release' })
  expect(useBonsaiStore.getState().notice).toBe('rebase completed')
})
