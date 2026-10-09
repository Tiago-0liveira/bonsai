import { describe, expect, it } from 'vitest'
import type { PullRequest } from '../../types'
import {
  authorInitials,
  describeMerge,
  findPullRequestWorktree,
  pullRequestUrl,
  summarizeChanges,
  summarizeReviews,
} from './prDetail'

type Check = PullRequest['checks'][number]
const checks = (...statuses: Check['status'][]): Check[] => statuses.map((status, index) => ({ name: `c${index}`, status }))
const merge = (overrides: Partial<Parameters<typeof describeMerge>[0]['pr']> = {}, parent?: { number: number }) =>
  describeMerge({ pr: { status: 'Open', base: 'main', mergeable: true, checks: [], ...overrides }, parent })

describe('pullRequestUrl', () => {
  it('links owner/name repositories only', () => {
    expect(pullRequestUrl('acme/widgets', 7)).toBe('https://github.com/acme/widgets/pull/7')
    expect(pullRequestUrl('local', 7)).toBeUndefined()
    expect(pullRequestUrl('', 7)).toBeUndefined()
    expect(pullRequestUrl(undefined, 7)).toBeUndefined()
    expect(pullRequestUrl(null, 7)).toBeUndefined()
  })
})

describe('authorInitials', () => {
  it('uses the first letters of the first two words', () => {
    expect(authorInitials('Tiago-Oliveira')).toBe('TO')
    expect(authorInitials('review pass')).toBe('RP')
    expect(authorInitials('claude')).toBe('CL')
    expect(authorInitials('x')).toBe('X')
  })

  it('falls back to a question mark', () => {
    expect(authorInitials(undefined)).toBe('?')
    expect(authorInitials(null)).toBe('?')
    expect(authorInitials(' - ')).toBe('?')
  })
})

describe('summarizeReviews', () => {
  it('counts review entries and distinct reviewers', () => {
    expect(summarizeReviews([
      { author: 'a', body: '', time: '', kind: 'review' },
      { author: 'a', body: '', time: '', kind: 'review' },
      { author: 'b', body: '', time: '', kind: 'review' },
      { author: 'c', body: '', time: '', kind: 'comment' },
      { author: 'd', body: '', time: '' },
    ])).toEqual({ reviews: 3, approvals: 2, changesRequested: 0, requested: [], exact: false })
  })

  it('tolerates missing input', () => {
    const none = { reviews: 0, approvals: 0, changesRequested: 0, requested: [], exact: false }
    expect(summarizeReviews(undefined)).toEqual(none)
    expect(summarizeReviews(null)).toEqual(none)
    expect(summarizeReviews([undefined as never])).toEqual(none)
  })

  it('prefers the provider verdicts over the conversation', () => {
    const conversation = [
      { author: 'a', body: '', time: '', kind: 'review' as const },
      { author: 'b', body: '', time: '', kind: 'review' as const },
    ]
    expect(summarizeReviews(conversation, { approvals: 1, changesRequested: 1, requested: ['c'] })).toEqual({
      reviews: 2, approvals: 1, changesRequested: 1, requested: ['c'], exact: true,
    })
  })
})

describe('summarizeChanges', () => {
  it('totals lines and files', () => {
    expect(summarizeChanges([
      { path: 'a', additions: 30, deletions: 10, diff: [] },
      { path: 'b', additions: 30, deletions: 10, diff: [] },
    ])).toEqual({ additions: 60, deletions: 20, files: 2, additionShare: 0.75 })
  })

  it('prefers provider totals, including when no files are loaded', () => {
    const totals = { additions: 900, deletions: 100, changedFiles: 14 }
    expect(summarizeChanges([{ path: 'a', additions: 1, deletions: 1, diff: [] }], totals)).toEqual({ additions: 900, deletions: 100, files: 14, additionShare: 0.9 })
    expect(summarizeChanges(undefined, totals)).toMatchObject({ additions: 900, files: 14 })
    expect(summarizeChanges([], { additions: 0, deletions: 0, changedFiles: 0 })).toEqual({ additions: 0, deletions: 0, files: 0, additionShare: 0 })
  })

  it('reports no share when nothing changed or input is missing', () => {
    expect(summarizeChanges([{ path: 'a', additions: 0, deletions: 0, diff: [] }])).toMatchObject({ files: 1, additionShare: 0 })
    expect(summarizeChanges([{ path: 'a', additions: undefined as never, deletions: NaN, diff: [] }])).toMatchObject({ additions: 0, deletions: 0 })
    expect(summarizeChanges(undefined)).toEqual({ additions: 0, deletions: 0, files: 0, additionShare: 0 })
    expect(summarizeChanges(null)).toMatchObject({ files: 0 })
  })
})

describe('describeMerge', () => {
  it('is ready when mergeable with every check passing', () => {
    const state = merge({ checks: checks('success', 'success') })
    expect(state).toMatchObject({ title: 'Ready to merge', tone: 'ok' })
    expect(state.blocker).toBeUndefined()
    expect(state.requirements.map((row) => row.text)).toEqual(['No conflicts with main', 'All 2 checks passed'])
  })

  it('uses singular wording for one check', () => {
    expect(merge({ checks: checks('success') }).requirements[1].text).toBe('All 1 check passed')
    expect(merge({ checks: checks('failed') }).requirements[1].text).toBe('1 of 1 check failing')
    expect(merge({ checks: checks('running') }).requirements[1].text).toBe('1 of 1 check still running')
  })

  it('waits on running checks and marks the row pending', () => {
    const state = merge({ checks: checks('success', 'running', 'running') })
    expect(state).toMatchObject({ title: 'Waiting on checks', tone: 'warn' })
    expect(state.requirements[1]).toMatchObject({ text: '2 of 3 checks still running', pending: true })
  })

  it('flags failing checks without blocking the merge', () => {
    const state = merge({ checks: checks('success', 'failed', 'failed') })
    expect(state).toMatchObject({ title: 'Checks failing', tone: 'danger' })
    expect(state.requirements[1]).toMatchObject({ tone: 'danger', text: '2 of 3 checks failing' })
    expect(state.blocker).toBeUndefined()
  })

  it('notes when no checks were reported', () => {
    expect(merge().requirements[1]).toMatchObject({ tone: 'muted', text: 'No checks reported' })
  })

  it('blocks on conflicts', () => {
    const state = merge({ mergeable: false, checks: checks('failed') })
    expect(state).toMatchObject({ title: 'Cannot merge', tone: 'danger', blocker: 'Conflicts with main' })
    expect(state.requirements[0]).toMatchObject({ tone: 'danger', text: 'Conflicts with main' })
  })

  it('blocks while mergeability is unknown', () => {
    const state = merge({ mergeable: undefined })
    expect(state).toMatchObject({ title: 'Ready to merge', blocker: 'Mergeability unknown' })
    expect(state.requirements[0]).toMatchObject({ tone: 'warn', text: 'Mergeability unknown' })
  })

  it('reports being behind the base without blocking the merge', () => {
    const state = merge({ behindBy: 5 })
    expect(state.requirements.find((row) => row.key === 'behind')).toMatchObject({ tone: 'warn', text: 'Behind main by 5 commits' })
    expect(state.blocker).toBeUndefined()
    expect(merge({ behindBy: 1 }).requirements.find((row) => row.key === 'behind')?.text).toBe('Behind main by 1 commit')
    expect(merge({ behindBy: 0 }).requirements.some((row) => row.key === 'behind')).toBe(false)
    expect(merge().requirements.some((row) => row.key === 'behind')).toBe(false)
  })

  it('reports requested changes', () => {
    const row = (changesRequested: number) => merge({ reviews: { requested: [], approvals: 0, changesRequested } }).requirements.find((item) => item.key === 'reviews')
    expect(row(1)).toMatchObject({ tone: 'danger', text: '1 reviewer requests changes' })
    expect(row(2)?.text).toBe('2 reviewers request changes')
    expect(row(0)).toBeUndefined()
  })

  it('explains a stacked base', () => {
    const state = merge({ base: 'feat/a' }, { number: 46 })
    expect(state.requirements.at(-1)).toMatchObject({ key: 'stack', tone: 'warn', text: 'Base is #46, not feat/a · merges into the stack' })
  })

  it('titles each non-open state', () => {
    expect(merge({ status: 'Draft' })).toMatchObject({ title: 'Draft · not ready', tone: 'warn' })
    expect(merge({ status: 'Merged' })).toMatchObject({ title: 'Merged', tone: 'ok' })
    expect(merge({ status: 'Closed' })).toMatchObject({ title: 'Closed', tone: 'muted' })
  })
})

describe('findPullRequestWorktree', () => {
  const tree = (id: string, branch: string, prNumber?: number, projectId = 'p') => ({ id, projectId, branch, prNumber })
  const pr = { number: 5, branch: 'feat/x' }

  it('prefers the worktree linked to the PR number', () => {
    const trees = [tree('a', 'feat/x', 4), tree('b', 'feat/x', 5)]
    expect(findPullRequestWorktree(trees, pr, 'p')?.id).toBe('b')
  })

  it('falls back to the branch', () => {
    expect(findPullRequestWorktree([tree('a', 'feat/x')], pr, 'p')?.id).toBe('a')
  })

  it('ignores other projects and branches', () => {
    expect(findPullRequestWorktree([tree('a', 'feat/x', 5, 'other'), tree('b', 'feat/y', 5)], pr, 'p')).toBeUndefined()
  })
})
