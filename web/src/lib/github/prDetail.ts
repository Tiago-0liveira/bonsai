import type { PullRequest, Worktree } from '../../types'
import { summarizeChecks } from './prChecks'

export type Tone = 'ok' | 'warn' | 'danger' | 'muted'

/** `owner/name` becomes a github.com pull request URL; anything else has no link. */
export function pullRequestUrl(repository: string | null | undefined, number: number): string | undefined {
  return repository?.includes('/') ? `https://github.com/${repository}/pull/${number}` : undefined
}

export function authorInitials(author: string | null | undefined): string {
  const parts = (author ?? '').split(/[\s._-]+/).filter(Boolean)
  if (!parts.length) return '?'
  const letters = parts.length > 1 ? parts[0][0] + parts[1][0] : parts[0].slice(0, 2)
  return letters.toUpperCase()
}

/**
 * The list response carries no commits, files or totals. A real PR always has
 * a commit, so none of the three means the detail has not arrived yet.
 */
export function isDetailLoaded(pr: Pick<PullRequest, 'totals' | 'files' | 'commits'>): boolean {
  return pr.totals !== undefined || pr.files.length > 0 || pr.commits.length > 0
}

export interface ReviewSummary {
  /** Entries of kind `review`. */
  reviews: number
  /** Standing approvals when the provider summary is loaded, else distinct reviewers. */
  approvals: number
  changesRequested: number
  /** Reviewers whose review is still awaited. */
  requested: string[]
  /** False when `approvals` is a guess from the conversation. */
  exact: boolean
}

/**
 * Prefers the provider's verdict summary; without it the conversation only
 * says who reviewed, not what they decided, so approvals are an upper bound.
 */
export function summarizeReviews(
  conversation: PullRequest['conversation'] | null | undefined,
  verdicts?: PullRequest['reviews'],
): ReviewSummary {
  const reviewers = new Set<string>()
  let reviews = 0
  for (const entry of conversation ?? []) {
    if (entry?.kind !== 'review') continue
    reviews++
    reviewers.add(entry.author)
  }
  if (!verdicts) return { reviews, approvals: reviewers.size, changesRequested: 0, requested: [], exact: false }
  return { reviews, approvals: verdicts.approvals, changesRequested: verdicts.changesRequested, requested: verdicts.requested, exact: true }
}

export interface ChangeTotals {
  additions: number
  deletions: number
  files: number
  /** Share of additions among all changed lines, 0 to 1; 0 when nothing changed. */
  additionShare: number
}

/** Provider totals win: they stay right when the file list is partial or not loaded yet. */
export function summarizeChanges(files: PullRequest['files'] | null | undefined, totals?: PullRequest['totals']): ChangeTotals {
  let additions = 0
  let deletions = 0
  const list = files ?? []
  for (const file of list) {
    additions += file.additions || 0
    deletions += file.deletions || 0
  }
  let count = list.length
  if (totals) ({ additions, deletions, changedFiles: count } = totals)
  const lines = additions + deletions
  return { additions, deletions, files: count, additionShare: lines ? additions / lines : 0 }
}

export interface MergeRequirement {
  key: string
  tone: Tone
  /** Spinner instead of a static icon. */
  pending?: boolean
  text: string
}

export interface MergeState {
  title: string
  tone: Tone
  requirements: MergeRequirement[]
  /** Why Merge is unavailable; absent when it is allowed. */
  blocker?: string
}

export interface MergeInput {
  pr: Pick<PullRequest, 'status' | 'base' | 'mergeable' | 'checks'> & Partial<Pick<PullRequest, 'behindBy' | 'reviews'>>
  /** The PR this one is stacked on, when it is. */
  parent?: Pick<PullRequest, 'number'>
}

/**
 * What the merge card shows. Only conflicts block the Merge action (as before
 * the redesign); failing or running checks are reported but the merge stays
 * the user's call.
 */
export function describeMerge({ pr, parent }: MergeInput): MergeState {
  const checks = summarizeChecks(pr.checks)
  const requirements: MergeRequirement[] = []

  if (pr.mergeable === true) requirements.push({ key: 'conflicts', tone: 'ok', text: `No conflicts with ${pr.base}` })
  else if (pr.mergeable === false) requirements.push({ key: 'conflicts', tone: 'danger', text: `Conflicts with ${pr.base}` })
  else requirements.push({ key: 'conflicts', tone: 'warn', text: 'Mergeability unknown' })

  if (!checks.total) requirements.push({ key: 'checks', tone: 'muted', text: 'No checks reported' })
  else if (checks.failed) requirements.push({ key: 'checks', tone: 'danger', text: `${checks.failed} of ${checks.total} ${checks.failed === 1 ? 'check' : 'checks'} failing` })
  else if (checks.running) requirements.push({ key: 'checks', tone: 'ok', pending: true, text: `${checks.running} of ${checks.total} ${checks.running === 1 ? 'check' : 'checks'} still running` })
  else requirements.push({ key: 'checks', tone: 'ok', text: `All ${checks.total} ${checks.total === 1 ? 'check' : 'checks'} passed` })

  if (pr.reviews?.changesRequested) {
    requirements.push({ key: 'reviews', tone: 'danger', text: `${pr.reviews.changesRequested} ${pr.reviews.changesRequested === 1 ? 'reviewer requests' : 'reviewers request'} changes` })
  }

  if (pr.behindBy && pr.behindBy > 0) {
    requirements.push({ key: 'behind', tone: 'warn', text: `Behind ${pr.base} by ${pr.behindBy} ${pr.behindBy === 1 ? 'commit' : 'commits'}` })
  }

  if (parent) requirements.push({ key: 'stack', tone: 'warn', text: `Base is #${parent.number}, not ${pr.base} · merges into the stack` })

  const blocker = pr.mergeable === true ? undefined : pr.mergeable === false ? `Conflicts with ${pr.base}` : 'Mergeability unknown'

  let title: string
  let tone: Tone
  if (pr.status === 'Merged') [title, tone] = ['Merged', 'ok']
  else if (pr.status === 'Closed') [title, tone] = ['Closed', 'muted']
  else if (pr.status === 'Draft') [title, tone] = ['Draft · not ready', 'warn']
  else if (pr.mergeable === false) [title, tone] = ['Cannot merge', 'danger']
  else if (pr.reviews?.changesRequested) [title, tone] = ['Changes requested', 'danger']
  else if (checks.failed) [title, tone] = ['Checks failing', 'danger']
  else if (checks.running) [title, tone] = ['Waiting on checks', 'warn']
  else [title, tone] = ['Ready to merge', 'ok']

  return { title, tone, requirements, ...(blocker ? { blocker } : {}) }
}

/**
 * The worktree that holds the PR's branch. A match on number and branch wins
 * over a match on branch alone (a branch can be reused by a later PR).
 */
export function findPullRequestWorktree<T extends Pick<Worktree, 'projectId' | 'branch' | 'prNumber'>>(
  worktrees: readonly T[],
  pr: Pick<PullRequest, 'number' | 'branch'>,
  projectId: string,
): T | undefined {
  const inProject = worktrees.filter((tree) => tree.projectId === projectId && tree.branch === pr.branch)
  return inProject.find((tree) => tree.prNumber === pr.number) ?? inProject[0]
}
