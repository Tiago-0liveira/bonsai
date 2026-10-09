import type { PullRequest } from '../../types'
import { summarizeChecks } from './prChecks'
import { buildPrStacks, type PrStack } from './prStack'

export const PR_LIST_FILTERS = ['all', 'stacked', 'draft', 'failing'] as const
export type PrListFilter = typeof PR_LIST_FILTERS[number]

export interface PrListRow {
  pr: PullRequest
  stacked: boolean
  /** Position among the visible rows of the group, for drawing the rail. */
  first: boolean
  last: boolean
}

export interface PrListGroup {
  key: string
  kind: 'stack' | 'standalone'
  title: string
  detail: string
  rows: PrListRow[]
}

export interface PrList {
  groups: PrListGroup[]
  /** Matches per filter, after the search query but before the filter. */
  counts: Record<PrListFilter, number>
  /** Rows currently shown, in display order. */
  visible: PullRequest[]
  /** Every PR in display order (stacks first), ignoring query and filter. */
  ordered: PullRequest[]
}

export interface PrListOptions {
  repository?: string
  query?: string
  filter?: PrListFilter
}

/** Matches number, title, branches and author. A leading `#` on the query is ignored. */
export function matchesQuery(pr: PullRequest, query: string): boolean {
  const needle = query.trim().replace(/^#/, '').toLowerCase()
  if (!needle) return true
  return `${pr.number} ${pr.title} ${pr.branch} ${pr.base} ${pr.author ?? ''}`.toLowerCase().includes(needle)
}

/**
 * Names a stack after the leading words its branches share
 * (`feat/theme-layer`, `feat/theme-shell` → `theme`), falling back to the
 * root's branch name.
 */
export function stackName(stack: PrStack<PullRequest>): string {
  const leaf = (branch: string) => branch.split('/').pop() || branch
  const words = stack.prs.map((pr) => leaf(pr.branch).split(/[-_.]/))
  const shared: string[] = []
  for (const [index, word] of words[0].entries()) {
    if (!word || words.some((other) => other[index] !== word)) break
    shared.push(word)
  }
  return shared.join('-') || leaf(stack.root.branch)
}

export function buildPrList(prs: readonly PullRequest[] | null | undefined, options: PrListOptions = {}): PrList {
  const { repository, query = '', filter = 'all' } = options
  const { stacks, standalone, stackOf } = buildPrStacks(prs, { repository })
  const ordered = [...stacks.flatMap((stack) => stack.prs), ...standalone]

  const predicates: Record<PrListFilter, (pr: PullRequest) => boolean> = {
    all: () => true,
    stacked: (pr) => stackOf(pr) !== undefined,
    draft: (pr) => pr.status === 'Draft',
    failing: (pr) => summarizeChecks(pr.checks).failed > 0,
  }

  const searched = ordered.filter((pr) => matchesQuery(pr, query))
  const counts = Object.fromEntries(
    PR_LIST_FILTERS.map((name) => [name, searched.filter(predicates[name]).length]),
  ) as Record<PrListFilter, number>
  const keep = new Set(searched.filter(predicates[filter]).map((pr) => pr.id))

  const toRows = (list: readonly PullRequest[], stacked: boolean): PrListRow[] => {
    const shown = list.filter((pr) => keep.has(pr.id))
    return shown.map((pr, index) => ({ pr, stacked, first: index === 0, last: index === shown.length - 1 }))
  }

  const groups: PrListGroup[] = []
  for (const stack of stacks) {
    const rows = toRows(stack.prs, true)
    if (!rows.length) continue
    groups.push({
      key: `stack:${stack.root.id}`,
      kind: 'stack',
      title: `Stack · ${stackName(stack)}`,
      detail: `${stack.prs.length} PRs → ${stack.root.base}`,
      rows,
    })
  }
  const loose = toRows(standalone, false)
  if (loose.length) {
    const drafts = loose.filter((row) => row.pr.status === 'Draft').length
    groups.push({
      key: 'standalone',
      kind: 'standalone',
      title: 'Standalone',
      detail: drafts ? `${drafts} ${drafts === 1 ? 'draft' : 'drafts'}` : `${loose.length} ${loose.length === 1 ? 'PR' : 'PRs'}`,
      rows: loose,
    })
  }

  return { groups, counts, visible: groups.flatMap((group) => group.rows.map((row) => row.pr)), ordered }
}
