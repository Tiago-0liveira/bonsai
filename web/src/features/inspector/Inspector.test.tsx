import { Profiler } from 'react'
import { act, cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { Inspector } from './Inspector'
import { useBonsaiStore } from '../../stores/bonsai'
import { projects } from '../../test/fixtures/projects'
import { worktrees } from '../../test/fixtures/worktrees'
import { pullRequests } from '../../test/fixtures/pullRequests'

afterEach(cleanup)

it('ignores unrelated group preferences and terminal output while reacting to the selected group', () => {
  useBonsaiStore.setState({ projects, worktrees, agents: [], pullRequests: [], activeProjectId: 'bonsai',
    selection: { type: 'worktree', id: 'wt-web' }, collapsedTagGroups: [], expandedAutomaticGroups: [], worktreeGroups: {}, terminalOutput: {} })
  const commits = vi.fn()
  render(<Profiler id="inspector" onRender={commits}><Inspector /></Profiler>)
  const baseline = commits.mock.calls.length
  act(() => useBonsaiStore.setState({ collapsedTagGroups: ['sprout-lab:feat'], expandedAutomaticGroups: ['other-group'], terminalOutput: { other: ['new output'] } }))
  expect(commits).toHaveBeenCalledTimes(baseline)
  const tree = worktrees.find(tree => tree.id === 'wt-web')!
  act(() => useBonsaiStore.setState({ collapsedTagGroups: ['sprout-lab:feat', `bonsai:${tree.tag}`] }))
  expect(commits.mock.calls.length).toBeGreaterThan(baseline)
  expect(screen.getByRole('complementary', { name: 'Inspector' })).toBeInTheDocument()
})

it('inspects retained process failures and their worktree without opening a terminal on render', () => {
  useBonsaiStore.setState({ ...useBonsaiStore.getInitialState(), projects, worktrees, activeProjectId: 'bonsai',
    selection: { type: 'process', id: 'bonsai:1' }, processes: [{ id: 'bonsai:1', projectId: 'bonsai', daemonId: 1, worktreeId: 'wt-web', name: 'Broken server', command: 'pnpm run server', status: 'error', lifecycleStatus: 'failed', exitCode: 2, exitError: 'Port in use', retryCount: 3, attempt: 4 }] }, true)
  render(<Inspector />)
  const inspector = screen.getByRole('complementary', { name: 'Inspector' })
  expect(inspector).toHaveTextContent('Broken server')
  expect(inspector).toHaveTextContent('pnpm run server')
  expect(screen.getByRole('alert')).toHaveTextContent('Port in use')
  expect(screen.getByRole('button', { name: 'Stop' })).toBeDisabled()
  expect(screen.getByRole('button', { name: 'Restart' })).toBeEnabled()
  expect(useBonsaiStore.getState().openRuntimeIds).toEqual([])
})

it.each([
  [true, 'no conflicts'],
  [false, 'conflicts'],
  [undefined, 'checking…'],
])('shows PR mergeability %s as "%s"', (mergeable, text) => {
  const pr = { ...pullRequests[0], id: 'bonsai:24', mergeable }
  useBonsaiStore.setState({ ...useBonsaiStore.getInitialState(), projects, worktrees: [worktrees[1]], activeProjectId: 'bonsai', pullRequests: [pr],
    selection: { type: 'worktree', id: worktrees[1].id } }, true)
  render(<Inspector />)
  expect(screen.getByRole('button', { name: /Pull request/ })).toHaveTextContent(new RegExp(`${text}$`))
  expect(screen.queryByText('Resolve merge conflicts') === null).toBe(mergeable !== false)
})
