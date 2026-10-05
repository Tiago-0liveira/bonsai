import { Profiler } from 'react'
import { act, cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { Inspector } from './Inspector'
import { useBonsaiStore } from '../../stores/bonsai'
import { projects } from '../../test/fixtures/projects'
import { worktrees } from '../../test/fixtures/worktrees'

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
