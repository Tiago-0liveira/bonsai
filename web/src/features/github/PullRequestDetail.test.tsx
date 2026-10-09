import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { useBonsaiStore } from '../../stores/bonsai'
import { stackPr, stackRepository } from '../../test/fixtures/prStacks'
import { bodyWithoutHeadings, pr47Body } from '../../test/fixtures/prBodies'
import { worktrees } from '../../test/fixtures/worktrees'
import type { Agent, PullRequest, Worktree } from '../../types'
import { PullRequestDetail, type PullRequestDetailProps } from './PullRequestDetail'

afterEach(cleanup)

const setStatus = vi.fn()
const addReview = vi.fn()
const setSelection = vi.fn()

beforeEach(() => {
  setStatus.mockReset()
  addReview.mockReset()
  setSelection.mockReset()
  useBonsaiStore.setState({
    processes: [], agents: [], worktrees: [],
    setPullRequestStatus: setStatus, addPullRequestReview: addReview, setSelection,
  })
})

const check = (name: string, status: 'success' | 'running' | 'failed') => ({ name, status })

function setup(pr: PullRequest, options: Partial<PullRequestDetailProps> & { others?: PullRequest[] } = {}) {
  const { others = [], ...props } = options
  const all = [pr, ...others]
  const onSelect = vi.fn()
  const onShowOnCanvas = vi.fn()
  render(
    <PullRequestDetail
      pr={pr}
      pullRequests={all}
      orderedIds={all.map((item) => item.id)}
      repository={stackRepository}
      projectId="bonsai"
      worktrees={[]}
      onSelect={onSelect}
      onShowOnCanvas={onShowOnCanvas}
      {...props}
    />,
  )
  return { onSelect, onShowOnCanvas }
}

const stacked = () => {
  const base = stackPr(46, 'feat/theme-canvas', 'main', { id: 'p:46', title: 'Canvas' })
  const top = stackPr(47, 'feat/theme-screens', 'feat/theme-canvas', {
    id: 'p:47',
    title: 'Theme Phase 5',
    author: 'Tiago-Oliveira',
    description: pr47Body,
    updatedAt: '2025-10-08T22:15:00',
    mergeable: true,
    checks: [check('build', 'success'), check('test', 'running'), check('verify', 'failed')],
    commits: [1, 2, 3, 4].map((n) => ({ sha: `abcdef${n}0`, message: `commit ${n}`, author: 'a', time: '2025-10-08T22:05:00' })),
    files: [{ path: 'a.ts', additions: 30, deletions: 10, diff: ['+ added', '- removed', ' context'] }],
    conversation: [{ author: 'Reviewer', body: 'Looks good', time: '1h ago', kind: 'review' }],
  })
  return { base, top }
}

describe('header', () => {
  it('shows state, branches, author and the stacked-on chip', () => {
    const { base, top } = stacked()
    const { onSelect } = setup(top, { others: [base] })
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('Theme Phase 5 #47')
    expect(screen.getByText('Open')).toBeInTheDocument()
    expect(screen.getByText('feat/theme-screens')).toBeInTheDocument()
    expect(screen.getAllByText('feat/theme-canvas').length).toBeGreaterThan(0)
    expect(screen.getByText('Tiago-Oliveira')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'stacked on #46' }))
    expect(onSelect).toHaveBeenCalledWith('p:46')
  })

  it('omits the stacked chip and stack card for a PR on main', () => {
    const pr = stackPr(32, 'feat/solo', 'main', { id: 'p:32', status: 'Draft' })
    setup(pr)
    expect(screen.queryByText(/stacked on/)).not.toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Stack' })).not.toBeInTheDocument()
    expect(screen.getByText('Draft')).toBeInTheDocument()
  })

  it('links out only when the repository is known', () => {
    const open = vi.spyOn(window, 'open').mockImplementation(() => null)
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    setup(stackPr(9, 'feat/x', 'main', { id: 'p:9' }))
    fireEvent.click(screen.getByRole('button', { name: /Open on GitHub/ }))
    expect(open).toHaveBeenCalledWith(`https://github.com/${stackRepository}/pull/9`, '_blank', 'noopener,noreferrer')
    fireEvent.click(screen.getByRole('button', { name: 'Copy link' }))
    expect(writeText).toHaveBeenCalledWith(`https://github.com/${stackRepository}/pull/9`)
    open.mockRestore()
    cleanup()

    setup(stackPr(9, 'feat/x', 'main', { id: 'p:9' }), { repository: 'local' })
    expect(screen.getByRole('button', { name: /Open on GitHub/ })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Copy link' })).toBeDisabled()
  })

  it('moves to the previous and next PR in list order', () => {
    const [a, b, c] = [1, 2, 3].map((n) => stackPr(n, `feat/${n}`, 'main', { id: `p:${n}` }))
    const { onSelect } = setup(b, { pullRequests: [a, b, c], orderedIds: ['p:1', 'p:2', 'p:3'] })
    fireEvent.click(screen.getByRole('button', { name: 'Previous pull request' }))
    fireEvent.click(screen.getByRole('button', { name: 'Next pull request' }))
    expect(onSelect.mock.calls).toEqual([['p:1'], ['p:3']])
    cleanup()
    setup(a, { pullRequests: [a, b, c], orderedIds: ['p:1', 'p:2', 'p:3'] })
    expect(screen.getByRole('button', { name: 'Previous pull request' })).toBeDisabled()
  })
})

describe('tiles', () => {
  it('summarize checks, reviews, changes and commits', () => {
    const { base, top } = stacked()
    setup(top, { others: [base] })
    expect(screen.getByText('1/3')).toBeInTheDocument()
    expect(screen.getByText('1 running · 1 failing')).toBeInTheDocument()
    expect(screen.getByText('reviewer')).toBeInTheDocument()
    expect(screen.getByText('+30')).toBeInTheDocument()
    expect(screen.getByText('−10')).toBeInTheDocument()
    expect(screen.getByText('1 file')).toBeInTheDocument()
  })

  it('degrade for a PR with nothing loaded', () => {
    setup(stackPr(5, 'feat/e', 'main', { id: 'p:5' }))
    expect(screen.getByText('no checks')).toBeInTheDocument()
    expect(screen.getByText('no approvals')).toBeInTheDocument()
    expect(screen.getByText('0 files')).toBeInTheDocument()
    expect(screen.getByText('none yet')).toBeInTheDocument()
  })
})

describe('overview', () => {
  it('renders Summary and Test plan cards from the body, with task progress', () => {
    const { base, top } = stacked()
    setup(top, { others: [base] })
    const summary = screen.getByRole('region', { name: 'Summary' })
    expect(within(summary).getByText(/Remove the Table \(board\) section:/)).toBeInTheDocument()
    const plan = screen.getByRole('region', { name: 'Test plan' })
    expect(within(plan).getByText('3 of 4')).toBeInTheDocument()
    expect(within(plan).getAllByRole('checkbox')).toHaveLength(4)
    expect(screen.getByRole('region', { name: 'Behavior changes to review' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'Stacking' })).toBeInTheDocument()
  })

  it('shows one Description card, clamped, when the body has no sections', () => {
    const long = `${bodyWithoutHeadings}\n\n${'More words. '.repeat(80)}`
    setup(stackPr(5, 'feat/e', 'main', { id: 'p:5', description: long }))
    const card = screen.getByRole('region', { name: 'Description' })
    expect(screen.queryByRole('region', { name: 'Summary' })).not.toBeInTheDocument()
    const toggle = within(card).getByRole('button', { name: 'Show more' })
    fireEvent.click(toggle)
    expect(within(card).getByRole('button', { name: 'Show less' })).toHaveAttribute('aria-expanded', 'true')
  })

  it('does not clamp a short body', () => {
    setup(stackPr(5, 'feat/e', 'main', { id: 'p:5', description: bodyWithoutHeadings }))
    expect(screen.queryByRole('button', { name: 'Show more' })).not.toBeInTheDocument()
  })

  it('says so for an empty body', () => {
    setup(stackPr(5, 'feat/e', 'main', { id: 'p:5', description: '' }))
    expect(screen.getByText('No description provided.')).toBeInTheDocument()
  })

  it('skips sections that exist but are empty, and a test plan without tasks has no counter', () => {
    setup(stackPr(5, 'feat/e', 'main', { id: 'p:5', description: '## Summary\n\n## Test plan\nRun it by hand.' }))
    expect(screen.queryByRole('region', { name: 'Summary' })).not.toBeInTheDocument()
    expect(within(screen.getByRole('region', { name: 'Test plan' })).queryByText(/ of /)).not.toBeInTheDocument()
  })

  it('shows the latest three commits and links to the full tab', () => {
    const { base, top } = stacked()
    setup(top, { others: [base] })
    const card = screen.getByRole('region', { name: 'Commits' })
    expect(within(card).queryByText('commit 1')).not.toBeInTheDocument()
    expect(within(card).getByText('commit 4')).toBeInTheDocument()
    fireEvent.click(within(card).getByRole('button', { name: 'View all 4 commits' }))
    expect(screen.getByRole('tab', { name: /Commits/ })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByText('commit 1')).toBeInTheDocument()
  })

  it('says so when there are no commits', () => {
    setup(stackPr(5, 'feat/e', 'main', { id: 'p:5' }))
    expect(within(screen.getByRole('region', { name: 'Commits' })).getByText('No commits loaded.')).toBeInTheDocument()
  })
})

describe('merge card', () => {
  it('merges and closes an open PR', () => {
    const { base, top } = stacked()
    setup(top, { others: [base] })
    const card = screen.getByRole('region', { name: 'Merge' })
    expect(within(card).getByText('Checks failing')).toBeInTheDocument()
    expect(within(card).getByText('Base is #46, not feat/theme-canvas · merges into the stack')).toBeInTheDocument()
    fireEvent.click(within(card).getByRole('button', { name: /Merge/ }))
    fireEvent.click(within(card).getByRole('button', { name: /Close/ }))
    expect(setStatus.mock.calls).toEqual([['p:47', 'Merged'], ['p:47', 'Closed']])
  })

  it('disables Merge and says why when the PR conflicts', () => {
    setup(stackPr(5, 'feat/e', 'main', { id: 'p:5', mergeable: false }))
    const merge = screen.getByRole('button', { name: /Merge/ })
    expect(merge).toBeDisabled()
    expect(merge).toHaveAttribute('title', 'Conflicts with main')
    expect(screen.getByText('Cannot merge')).toBeInTheDocument()
    fireEvent.click(merge)
    expect(setStatus).not.toHaveBeenCalled()
  })

  it('marks a draft ready', () => {
    setup(stackPr(5, 'feat/e', 'main', { id: 'p:5', status: 'Draft', mergeable: true }))
    fireEvent.click(screen.getByRole('button', { name: /Mark ready/ }))
    expect(setStatus).toHaveBeenCalledWith('p:5', 'Open')
    expect(screen.queryByRole('button', { name: /Merge/ })).not.toBeInTheDocument()
  })

  it('reopens a closed PR and offers nothing for a merged one', () => {
    setup(stackPr(5, 'feat/e', 'main', { id: 'p:5', status: 'Closed' }))
    fireEvent.click(screen.getByRole('button', { name: /Reopen/ }))
    expect(setStatus).toHaveBeenCalledWith('p:5', 'Open')
    cleanup()
    setup(stackPr(6, 'feat/f', 'main', { id: 'p:6', status: 'Merged' }))
    const card = screen.getByRole('region', { name: 'Merge' })
    expect(within(card).getByText('Merged')).toBeInTheDocument()
    expect(within(card).queryByRole('button')).not.toBeInTheDocument()
  })
})

describe('provider enrichments', () => {
  it('show requested reviewers, verdicts and provider totals when loaded', () => {
    const { base, top } = stacked()
    setup({
      ...top,
      reviews: { requested: ['ana', 'bo'], approvals: 2, changesRequested: 0 },
      totals: { additions: 900, deletions: 100, changedFiles: 14 },
      behindBy: 3,
    }, { others: [base] })
    expect(screen.getByText('approvals')).toBeInTheDocument()
    expect(screen.getByText('requested: ana, bo')).toBeInTheDocument()
    expect(screen.getByText('+900')).toBeInTheDocument()
    expect(screen.getByText('14 files')).toBeInTheDocument()
    expect(within(screen.getByRole('region', { name: 'Merge' })).getByText('Behind feat/theme-canvas by 3 commits')).toBeInTheDocument()
  })

  it('call out requested changes', () => {
    const { base, top } = stacked()
    setup({ ...top, reviews: { requested: [], approvals: 0, changesRequested: 1 } }, { others: [base] })
    expect(screen.getAllByText(/requesting changes|requests changes/)).toHaveLength(2)
  })

  it('show durations only when checks carry timing', () => {
    const timed = (name: string, status: 'success' | 'running' | 'failed', seconds?: number) => ({
      ...check(name, status),
      ...(seconds !== undefined && { startedAt: '2026-01-01T10:00:00Z', completedAt: new Date(Date.UTC(2026, 0, 1, 10, 0, seconds)).toISOString() }),
    })
    setup(stackPr(5, 'feat/e', 'main', { id: 'p:5', checks: [timed('build', 'success', 128), timed('queued', 'running')] }))
    const items = within(screen.getByRole('region', { name: 'Checks' })).getAllByRole('listitem').map((item) => item.textContent)
    expect(items).toEqual(['queued', 'build2m 08s'])
    cleanup()
    setup(stackPr(6, 'feat/f', 'main', { id: 'p:6', checks: [check('plain', 'success')] }))
    expect(within(screen.getByRole('region', { name: 'Checks' })).getAllByRole('listitem').map((item) => item.textContent)).toEqual(['plain'])
  })
})

describe('checks card', () => {
  it('lists running, then failed, then passed', () => {
    const { base, top } = stacked()
    setup(top, { others: [base] })
    const items = within(screen.getByRole('region', { name: 'Checks' })).getAllByRole('listitem').map((item) => item.textContent)
    expect(items).toEqual(['test', 'verify', 'build'])
  })

  it('renders 40 checks and none', () => {
    const many = Array.from({ length: 40 }, (_, index) => check(`job ${index}`, index % 5 ? 'success' : 'running'))
    setup(stackPr(5, 'feat/e', 'main', { id: 'p:5', checks: many }))
    expect(within(screen.getByRole('region', { name: 'Checks' })).getAllByRole('listitem')).toHaveLength(40)
    cleanup()
    setup(stackPr(6, 'feat/f', 'main', { id: 'p:6' }))
    expect(screen.getByText('No checks reported.')).toBeInTheDocument()
  })
})

describe('stack card', () => {
  it('lists the stack and selects an entry', () => {
    const { base, top } = stacked()
    const { onSelect } = setup(top, { others: [base] })
    const card = screen.getByRole('region', { name: 'Stack' })
    expect(within(card).getByText('2 PRs → main')).toBeInTheDocument()
    expect(within(card).getByRole('button', { name: /#47/ })).toHaveAttribute('aria-current', 'true')
    fireEvent.click(within(card).getByRole('button', { name: /#46/ }))
    expect(onSelect).toHaveBeenCalledWith('p:46')
  })

  it('reflects each PR check state', () => {
    const { base, top } = stacked()
    setup(top, { others: [{ ...base, checks: [check('x', 'success')] }] })
    const card = screen.getByRole('region', { name: 'Stack' })
    expect(within(card).getByLabelText('Checks passed')).toBeInTheDocument()
    expect(within(card).getByLabelText('Checks failing')).toBeInTheDocument()
  })
})

describe('worktree card', () => {
  const tree: Worktree = { ...worktrees[1], id: 'wt-47', projectId: 'bonsai', branch: 'feat/theme-screens', prNumber: 47 }
  const agent = (overrides: Partial<Agent>): Agent => ({
    id: 'a1', worktreeId: 'wt-47', name: 'Claude', task: 'fixing build', state: 'running', archived: false,
    ...overrides,
  } as Agent)

  it('shows processes and agents and selects the worktree on the canvas', () => {
    const { base, top } = stacked()
    useBonsaiStore.setState({
      processes: [
        { id: 'pr1', worktreeId: 'wt-47', name: 'dev', command: 'pnpm dev', port: 3010, status: 'healthy', lifecycleStatus: 'running' },
        { id: 'pr2', worktreeId: 'wt-47', name: 'tsc -w', command: 'tsc -w', status: 'error', lifecycleStatus: 'failed' },
        { id: 'pr3', worktreeId: 'other', name: 'elsewhere', command: 'x', status: 'idle', lifecycleStatus: 'stopped' },
      ] as never,
      agents: [
        agent({}),
        agent({ id: 'a2', name: 'Reviewer', state: 'finished' }),
        agent({ id: 'a3', name: 'Idle one', state: 'idle' }),
        agent({ id: 'a4', name: 'Archived', archived: true }),
        agent({ id: 'a5', name: 'Elsewhere', worktreeId: 'other' }),
      ],
    })
    const { onShowOnCanvas } = setup(top, { others: [base], worktrees: [tree] })
    const card = screen.getByRole('region', { name: 'Worktree' })
    expect(within(card).getByText('dev')).toBeInTheDocument()
    expect(within(card).getByText(':3010')).toBeInTheDocument()
    expect(within(card).getByText('failed')).toBeInTheDocument()
    expect(within(card).queryByText('elsewhere')).not.toBeInTheDocument()
    expect(within(card).getByText('Claude')).toBeInTheDocument()
    expect(within(card).queryByText('Archived')).not.toBeInTheDocument()
    expect(within(card).queryByText('Elsewhere')).not.toBeInTheDocument()
    fireEvent.click(within(card).getByRole('button', { name: 'Show on canvas' }))
    expect(onShowOnCanvas).toHaveBeenCalledWith('wt-47')
  })

  it('notes an idle worktree and is absent without one', () => {
    const { base, top } = stacked()
    setup(top, { others: [base], worktrees: [tree] })
    expect(within(screen.getByRole('region', { name: 'Worktree' })).getByText('Nothing running here.')).toBeInTheDocument()
    cleanup()
    setup(top, { others: [base] })
    expect(screen.queryByRole('region', { name: 'Worktree' })).not.toBeInTheDocument()
  })
})

describe('tabs', () => {
  it('switch with click and keyboard and show counts', () => {
    const { base, top } = stacked()
    setup(top, { others: [base] })
    const tabs = screen.getAllByRole('tab')
    expect(tabs.map((tab) => tab.textContent)).toEqual(['Overview', 'Commits4', 'Files1', 'Conversation1'])
    expect(tabs[0]).toHaveAttribute('aria-selected', 'true')

    fireEvent.click(screen.getByRole('tab', { name: /Files/ }))
    expect(screen.getByRole('tab', { name: /Files/ })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('tabpanel')).toHaveAttribute('aria-labelledby', 'pr-tab-files')

    fireEvent.keyDown(screen.getByRole('tab', { name: /Files/ }), { key: 'ArrowRight' })
    expect(screen.getByRole('tab', { name: /Conversation/ })).toHaveAttribute('aria-selected', 'true')
    fireEvent.keyDown(screen.getByRole('tab', { name: /Conversation/ }), { key: 'ArrowRight' })
    expect(screen.getByRole('tab', { name: /Overview/ })).toHaveAttribute('aria-selected', 'true')
    fireEvent.keyDown(screen.getByRole('tab', { name: /Overview/ }), { key: 'ArrowLeft' })
    expect(screen.getByRole('tab', { name: /Conversation/ })).toHaveAttribute('aria-selected', 'true')
    fireEvent.keyDown(screen.getByRole('tab', { name: /Conversation/ }), { key: 'Home' })
    expect(screen.getByRole('tab', { name: /Overview/ })).toHaveAttribute('aria-selected', 'true')
    fireEvent.keyDown(screen.getByRole('tab', { name: /Overview/ }), { key: 'End' })
    expect(screen.getByRole('tab', { name: /Conversation/ })).toHaveAttribute('aria-selected', 'true')
    fireEvent.keyDown(screen.getByRole('tab', { name: /Conversation/ }), { key: 'a' })
    expect(screen.getByRole('tab', { name: /Conversation/ })).toHaveAttribute('aria-selected', 'true')
  })

  it('shows changed files with their diff on demand', () => {
    const { base, top } = stacked()
    setup(top, { others: [base] })
    fireEvent.click(screen.getByRole('tab', { name: /Files/ }))
    const row = screen.getByRole('button', { name: /a\.ts/ })
    expect(row).toHaveAttribute('aria-expanded', 'false')
    fireEvent.click(row)
    expect(screen.getByText('+ added')).toHaveClass('text-ok')
    expect(screen.getByText('- removed')).toHaveClass('text-danger')
    fireEvent.click(row)
    expect(screen.queryByText('+ added')).not.toBeInTheDocument()
  })

  it('explains empty commits, files and an empty diff', () => {
    setup(stackPr(5, 'feat/e', 'main', { id: 'p:5', files: [{ path: 'b.ts', additions: 0, deletions: 0, diff: [] }] }))
    fireEvent.click(screen.getByRole('tab', { name: /Commits/ }))
    expect(screen.getByText('No commits loaded.')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('tab', { name: /Files/ }))
    fireEvent.click(screen.getByRole('button', { name: /b\.ts/ }))
    expect(screen.getByText('No diff available.')).toBeInTheDocument()
    cleanup()
    setup(stackPr(6, 'feat/f', 'main', { id: 'p:6' }))
    fireEvent.click(screen.getByRole('tab', { name: /Files/ }))
    expect(screen.getByText('No changed files loaded.')).toBeInTheDocument()
  })

  it('lists comments and submits reviews', () => {
    const { base, top } = stacked()
    setup(top, { others: [base] })
    fireEvent.click(screen.getByRole('tab', { name: /Conversation/ }))
    expect(screen.getByText('Looks good')).toBeInTheDocument()
    expect(screen.getByText(/reviewed 1h ago/)).toBeInTheDocument()
    const comment = screen.getByRole('button', { name: /Comment/ })
    expect(comment).toBeDisabled()
    fireEvent.change(screen.getByLabelText('Add a review'), { target: { value: 'Nice' } })
    fireEvent.click(comment)
    fireEvent.click(screen.getByRole('button', { name: /Approve/ }))
    fireEvent.click(screen.getByRole('button', { name: /Request changes/ }))
    expect(addReview.mock.calls).toEqual([['p:47', 'Nice', 'comment'], ['p:47', '', 'approve'], ['p:47', '', 'request-changes']])
    expect(screen.getByLabelText('Add a review')).toHaveValue('')
  })

  it('invites the first comment', () => {
    setup(stackPr(5, 'feat/e', 'main', { id: 'p:5' }))
    fireEvent.click(screen.getByRole('tab', { name: /Conversation/ }))
    expect(screen.getByText('No comments yet.')).toBeInTheDocument()
  })
})

it('renders a very long title and a PR without author or timestamps', () => {
  const title = 'A'.repeat(300)
  setup(stackPr(5, 'feat/' + 'long-branch-'.repeat(12), 'main', { id: 'p:5', title }))
  expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent(title)
  expect(screen.getByText('unknown')).toBeInTheDocument()
  expect(screen.getByText('updated recently')).toBeInTheDocument()
})
