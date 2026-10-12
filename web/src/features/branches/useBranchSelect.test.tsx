import { beforeEach, describe, expect, it, vi } from 'vitest'
import { renderHook } from '@testing-library/react'
import type { ReactNode } from 'react'
import { useBranchSelect } from './useBranchSelect'
import { ShellRouteProvider, type ShellPage } from '../../app/shellRoute'
import { useBonsaiStore } from '../../stores/bonsai'
import { DEFAULT_CANVAS_PREFERENCES, type CanvasPreferences } from '../../types'
import { worktrees } from '../../test/fixtures/worktrees'
import { agents } from '../../mock/agents'

const showCanvas = vi.fn(), showPullRequest = vi.fn()

function select(page: ShellPage, preferences: Partial<CanvasPreferences> = {}) {
  useBonsaiStore.setState({ canvasPreferences: { ...DEFAULT_CANVAS_PREFERENCES, ...preferences } })
  const wrapper = ({ children }: { children: ReactNode }) => <ShellRouteProvider value={{ page, showCanvas, showPullRequest }}>{children}</ShellRouteProvider>
  return renderHook(() => useBranchSelect(), { wrapper }).result.current
}
const reveal = () => useBonsaiStore.getState().canvasReveal

describe('Branches selection', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    useBonsaiStore.setState({ activeProjectId: 'bonsai', worktrees, agents: [{ ...agents[0], worktreeId: 'wt-web' }], notice: '', canvasReveal: { projectId: '', nodeId: '', nonce: 0 } })
  })

  it('centres the selected worktree on the canvas', () => {
    select('canvas')('worktree', 'wt-web')
    expect(useBonsaiStore.getState().selection).toEqual({ type: 'worktree', id: 'wt-web' })
    expect(reveal()).toMatchObject({ projectId: 'bonsai', nodeId: 'wt-web', mode: 'center', nonce: 1 })
  })

  it('follows the worktree and agent focus preferences separately', () => {
    select('canvas', { focusOnBranchSelect: false })('worktree', 'wt-web')
    expect(reveal().nonce).toBe(0)
    select('canvas', { focusOnBranchSelect: false })('agent', agents[0].id)
    expect(reveal()).toMatchObject({ nodeId: agents[0].id, mode: 'center' })
    select('canvas', { focusAgentsOnBranchSelect: false })('agent', agents[0].id)
    expect(reveal().nonce).toBe(1)
  })

  it("shows the worktree's pull request while on the GitHub tab", () => {
    select('github')('worktree', 'wt-web')
    expect(showPullRequest).toHaveBeenCalledWith('bonsai:24')
    expect(showCanvas).not.toHaveBeenCalled()
    expect(reveal().nonce).toBe(0)
    select('github')('agent', agents[0].id)
    expect(showPullRequest).toHaveBeenLastCalledWith('bonsai:24')
  })

  it('keeps the current pull request and says so when the worktree has none', () => {
    select('github')('worktree', 'wt-main')
    expect(showPullRequest).not.toHaveBeenCalled()
    expect(useBonsaiStore.getState().notice).toBe('No pull request for main')
  })

  it('can switch from GitHub to the canvas and focus instead', () => {
    select('github', { githubBranchSelect: 'open-canvas' })('worktree', 'wt-web')
    expect(showCanvas).toHaveBeenCalledTimes(1)
    expect(showPullRequest).not.toHaveBeenCalled()
    expect(reveal()).toMatchObject({ nodeId: 'wt-web', mode: 'center' })
  })

  it('only selects on other pages', () => {
    select('other')('worktree', 'wt-web')
    expect(useBonsaiStore.getState().selection).toEqual({ type: 'worktree', id: 'wt-web' })
    expect(reveal().nonce).toBe(0)
    expect(showCanvas).not.toHaveBeenCalled()
    expect(showPullRequest).not.toHaveBeenCalled()
  })
})
