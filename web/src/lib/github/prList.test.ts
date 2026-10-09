import { describe, expect, it } from 'vitest'
import { stackPr, threeDeepStack } from '../../test/fixtures/prStacks'
import { buildPrList, matchesQuery, stackName } from './prList'
import { buildPrStacks } from './prStack'

const failing = { checks: [{ name: 'ci', status: 'failed' as const }] }

describe('matchesQuery', () => {
  const pr = stackPr(47, 'feat/theme-screens', 'feat/theme-canvas', { title: 'Remove Table', author: 'Ada' })

  it('matches everything for an empty query', () => {
    expect(matchesQuery(pr, '')).toBe(true)
    expect(matchesQuery(pr, '  ')).toBe(true)
    expect(matchesQuery(pr, '#')).toBe(true)
  })

  it.each(['table', 'THEME-SCREENS', 'theme-canvas', 'ada', '47', '#47'])('matches %s', (query) => {
    expect(matchesQuery(pr, query)).toBe(true)
  })

  it('rejects unrelated text and tolerates a missing author', () => {
    expect(matchesQuery(pr, 'nope')).toBe(false)
    expect(matchesQuery({ ...pr, author: undefined }, 'undefined')).toBe(false)
  })
})

describe('stackName', () => {
  const stackOf = (...branches: string[]) => {
    const prs = branches.map((branch, index) => stackPr(index + 1, branch, index ? branches[index - 1] : 'main'))
    return buildPrStacks(prs).stacks[0]
  }

  it('uses the words the branches share', () => {
    expect(stackName(stackOf('feat/theme-foundation', 'feat/theme-layer', 'feat/theme-shell'))).toBe('theme')
    expect(stackName(stackOf('a/auth-api-v1', 'a/auth-api-v2'))).toBe('auth-api')
  })

  it('falls back to the root branch when nothing is shared', () => {
    expect(stackName(stackOf('feat/alpha', 'feat/beta'))).toBe('alpha')
  })

  it('stops at an empty word and survives a trailing slash', () => {
    expect(stackName(stackOf('x/a--b', 'x/a--c'))).toBe('a')
    expect(stackName(stackOf('feat/', 'feat/two'))).toBe('feat/')
  })
})

describe('buildPrList', () => {
  const { prs, a, b, c } = threeDeepStack()
  const draft = stackPr(10, 'feat/draft', 'main', { status: 'Draft' })
  const broken = stackPr(11, 'feat/broken', 'main', failing)
  const all = [draft, ...prs, broken]

  it('groups stacks root first, then standalone', () => {
    const list = buildPrList(all)
    expect(list.groups.map((group) => [group.key.split(':')[0], group.title, group.detail])).toEqual([
      ['stack', 'Stack · a', '3 PRs → main'],
      ['standalone', 'Standalone', '1 draft'],
    ])
    expect(list.groups[0].rows.map((row) => row.pr)).toEqual([a, b, c])
    expect(list.groups[0].rows.map((row) => [row.stacked, row.first, row.last])).toEqual([
      [true, true, false],
      [true, false, false],
      [true, false, true],
    ])
    expect(list.groups[1].rows.map((row) => row.pr)).toEqual([draft, broken])
    expect(list.groups[1].rows.every((row) => !row.stacked)).toBe(true)
    expect(list.visible).toEqual([a, b, c, draft, broken])
    expect(list.ordered).toEqual(list.visible)
  })

  it('counts each filter live', () => {
    expect(buildPrList(all).counts).toEqual({ all: 5, stacked: 3, draft: 1, failing: 1 })
    expect(buildPrList(all, { query: 'feat/b' }).counts).toEqual({ all: 3, stacked: 2, draft: 0, failing: 1 })
  })

  it.each([
    ['stacked', [a, b, c]],
    ['draft', [draft]],
    ['failing', [broken]],
    ['all', [a, b, c, draft, broken]],
  ] as const)('filters by %s', (filter, expected) => {
    expect(buildPrList(all, { filter }).visible).toEqual(expected)
  })

  it('applies the query inside groups and drops empty ones', () => {
    const list = buildPrList(all, { query: '#2' })
    expect(list.groups).toHaveLength(1)
    expect(list.groups[0].rows.map((row) => row.pr.number)).toEqual([2])
    expect(list.groups[0].rows[0]).toMatchObject({ first: true, last: true })
    expect(list.ordered).toHaveLength(5)
  })

  it('keeps stacks intact when the filter hides their parent', () => {
    const list = buildPrList(all, { filter: 'failing' })
    expect(list.groups.map((group) => group.kind)).toEqual(['standalone'])
    const stacked = buildPrList([a, stackPr(2, 'feat/b', 'feat/a', failing)], { filter: 'failing' })
    expect(stacked.groups[0].title).toBe('Stack · a')
    expect(stacked.groups[0].detail).toBe('2 PRs → main')
    expect(stacked.visible.map((pr) => pr.number)).toEqual([2])
  })

  it('pluralises the standalone summary', () => {
    expect(buildPrList([broken]).groups[0].detail).toBe('1 PR')
    expect(buildPrList([broken, stackPr(12, 'feat/x', 'main')]).groups[0].detail).toBe('2 PRs')
    const drafts = [draft, stackPr(12, 'feat/y', 'main', { status: 'Draft' })]
    expect(buildPrList(drafts).groups[0].detail).toBe('2 drafts')
  })

  it('handles empty and missing input', () => {
    for (const input of [[], null, undefined]) {
      expect(buildPrList(input)).toEqual({
        groups: [],
        counts: { all: 0, stacked: 0, draft: 0, failing: 0 },
        visible: [],
        ordered: [],
      })
    }
  })

  it('does not stack PRs from forks when the repository is given', () => {
    const fork = stackPr(20, 'feat/a', 'main', { headRepository: 'someone/fork' })
    const child = stackPr(21, 'feat/c', 'feat/a')
    const list = buildPrList([fork, child], { repository: 'acme/widgets' })
    expect(list.groups.map((group) => group.kind)).toEqual(['standalone'])
  })
})
