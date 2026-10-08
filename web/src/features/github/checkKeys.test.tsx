import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, render, screen } from '@testing-library/react'
import { useBonsaiStore } from '../../stores/bonsai'
import { projects } from '../../test/fixtures/projects'
import { worktrees } from '../../test/fixtures/worktrees'
import { pullRequests } from '../../test/fixtures/pullRequests'
import { Inspector } from '../inspector/Inspector'
import { PullRequestsPage } from './PullRequestsPage'

vi.mock('../../api/git', async importOriginal => ({
  ...await importOriginal<typeof import('../../api/git')>(),
  loadPullRequest: vi.fn(),
}))

const scrollIntoView = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'scrollIntoView')
beforeAll(() => Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', { configurable: true, value: vi.fn() }))
afterAll(() => {
  if (scrollIntoView) Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', scrollIntoView)
  else Reflect.deleteProperty(HTMLElement.prototype, 'scrollIntoView')
})

beforeEach(() => {
  vi.spyOn(console, 'error').mockImplementation(() => {})
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
})

afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals() })

describe.each([
  ['GitHub page', PullRequestsPage],
  ['inspector', Inspector],
] as const)('%s check keys', (_, Component) => {
  it.each([true, false])('renders same-name checks without warnings (provider IDs: %s)', withIds => {
    const pr = {
      ...pullRequests[0], id: 'bonsai:24',
      checks: [
        { id: withIds ? 101 : undefined, name: 'verify', status: 'success' as const },
        { id: withIds ? 102 : undefined, name: 'verify', status: 'failed' as const },
      ],
    }
    useBonsaiStore.setState({
      projects, activeProjectId: 'bonsai', worktrees: [worktrees[1]],
      selection: { type: 'worktree', id: worktrees[1].id },
      pullRequests: [pr], inspectedPullRequestId: pr.id,
      agents: [], processes: [], rightPanels: { files: false, prs: true },
    })
    const view = render(<Component />)
    expect(screen.getAllByText('verify', { exact: true })).toHaveLength(2)
    act(() => useBonsaiStore.setState({ pullRequests: [{ ...pr, checks: [...pr.checks].reverse() }] }))
    view.rerender(<Component />)
    expect(screen.getAllByText('verify', { exact: true })).toHaveLength(2)
    expect(vi.mocked(console.error).mock.calls.filter(args => args.some(arg => String(arg).includes('same key')))).toEqual([])
  })
})
