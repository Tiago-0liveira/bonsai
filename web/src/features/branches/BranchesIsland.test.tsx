import { cleanup, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, expect, it } from 'vitest'
import type { ReactNode } from 'react'
import { ShellRouteProvider } from '../../app/shellRoute'
import { useBonsaiStore } from '../../stores/bonsai'
import { projects } from '../../test/fixtures/projects'
import { worktrees } from '../../test/fixtures/worktrees'
import { agents } from '../../mock/agents'
import type { Agent } from '../../types'
import { BranchesIsland } from './BranchesIsland'

const live = (id: string, state: 'working' | 'waiting' | 'idle'): Agent => ({ ...agents[0], id, name: id, worktreeId: 'wt-web', projectId: 'bonsai', providerId: 'claude', provider: 'Claude',
  state: 'running', lifecycleState: 'running', presentation: 'canvas', activity: { state, reason: state === 'waiting' ? 'question' : undefined, since: '2099-01-01T00:00:00Z', source: 'hook' } })
const wrap = ({ children }: { children: ReactNode }) => <ShellRouteProvider value={{ page: 'canvas', showCanvas: () => undefined, showPullRequest: () => undefined }}>{children}</ShellRouteProvider>

beforeEach(() => useBonsaiStore.setState({ ...useBonsaiStore.getInitialState(), projects, worktrees, activeProjectId: 'bonsai', agents: [live('builder', 'working'), live('reviewer', 'waiting')] }, true))
afterEach(cleanup)

it('marks a worktree row that has an agent waiting, and gives each agent row its own status', () => {
  render(<BranchesIsland />, { wrapper: wrap })
  const row = document.querySelector('[data-worktree-row="wt-web"]') as HTMLElement
  expect(within(row).getByRole('img', { name: '1 agent needs input' })).toBeInTheDocument()
  expect(document.querySelector('[data-worktree-row="wt-docs"] [data-needs-input]')).toBeNull()
  expect(screen.getByRole('img', { name: 'Working' })).toBeInTheDocument()
  expect(screen.getByRole('img', { name: 'Needs an answer' })).toBeInTheDocument()
})
