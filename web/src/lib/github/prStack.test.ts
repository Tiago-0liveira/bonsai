import type { StackablePr } from './prStack'
import { buildPrStacks } from './prStack'

let n = 0
const pr = (number: number, branch: string, base: string, status: StackablePr['status'] = 'Open'): StackablePr => ({
  id: `pr-${number}-${n++}`,
  number,
  branch,
  base,
  status,
})

const numbers = (prs: StackablePr[]) => prs.map((p) => p.number)

describe('buildPrStacks', () => {
  it('orders a 3-deep stack root first', () => {
    const c = pr(3, 'c', 'b')
    const a = pr(1, 'a', 'main')
    const b = pr(2, 'b', 'a')
    const result = buildPrStacks([c, a, b])

    expect(result.stacks).toHaveLength(1)
    expect(result.stacks[0].root).toBe(a)
    expect(numbers(result.stacks[0].prs)).toEqual([1, 2, 3])
    expect(result.standalone).toEqual([])
    expect(numbers(result.chainOf(c))).toEqual([1, 2, 3])
    expect(numbers(result.chainOf(a))).toEqual([1])
    expect(result.parentOf(c)).toBe(b)
    expect(result.parentOf(a)).toBeUndefined()
    expect(result.isStacked(c)).toBe(true)
    expect(result.isStacked(a)).toBe(false)
    expect(result.stackOf(b)).toBe(result.stacks[0])
  })

  it('puts PRs based on main in standalone, in input order', () => {
    const x = pr(5, 'x', 'main')
    const y = pr(4, 'y', 'main', 'Draft')
    const result = buildPrStacks([x, y])
    expect(result.stacks).toEqual([])
    expect(result.standalone).toEqual([x, y])
    expect(result.isStacked(x)).toBe(false)
    expect(result.stackOf(x)).toBeUndefined()
    expect(result.chainOf(x)).toEqual([x])
  })

  it('treats a draft parent as open but not a merged or closed one', () => {
    const draft = pr(1, 'a', 'main', 'Draft')
    const child = pr(2, 'b', 'a')
    expect(buildPrStacks([draft, child]).isStacked(child)).toBe(true)

    for (const status of ['Merged', 'Closed'] as const) {
      const parent = pr(1, 'a', 'main', status)
      const result = buildPrStacks([parent, child])
      expect(result.isStacked(child)).toBe(false)
      expect(result.standalone).toEqual([parent, child])
    }
  })

  it('keeps branching stacks in depth-first order', () => {
    const a = pr(1, 'a', 'main')
    const b = pr(2, 'b', 'a')
    const c = pr(3, 'c', 'a')
    const d = pr(4, 'd', 'b')
    const result = buildPrStacks([a, b, c, d])
    expect(numbers(result.stacks[0].prs)).toEqual([1, 2, 4, 3])
  })

  it('does not crash on a cycle and roots it at the lowest number', () => {
    const a = pr(7, 'a', 'b')
    const b = pr(3, 'b', 'a')
    const tail = pr(9, 'tail', 'a')
    const result = buildPrStacks([a, b, tail])

    expect(result.stacks).toHaveLength(1)
    expect(result.stacks[0].root).toBe(b)
    expect(numbers(result.stacks[0].prs)).toEqual([3, 7, 9])
    expect(numbers(result.chainOf(tail))).toEqual([3, 7, 9])
    expect(result.isStacked(b)).toBe(false)
  })

  it('cuts a cycle at its lowest number regardless of input order', () => {
    const low = pr(1, 'a', 'b')
    const high = pr(8, 'b', 'a')
    const result = buildPrStacks([high, low])
    expect(result.stacks[0].root).toBe(low)
    expect(numbers(result.stacks[0].prs)).toEqual([1, 8])
  })

  it('handles a PR whose base is its own branch', () => {
    const loop = pr(1, 'a', 'a')
    const result = buildPrStacks([loop])
    expect(result.standalone).toEqual([loop])
    expect(result.chainOf(loop)).toEqual([loop])
  })

  it('treats an orphan base as standalone', () => {
    const orphan = pr(2, 'b', 'deleted-branch')
    const result = buildPrStacks([orphan])
    expect(result.standalone).toEqual([orphan])
    expect(result.parentOf(orphan)).toBeUndefined()
  })

  it('prefers the lowest-numbered open PR when branches collide', () => {
    const first = pr(9, 'a', 'main')
    const second = pr(2, 'a', 'main')
    const child = pr(10, 'b', 'a')
    expect(buildPrStacks([first, second, child]).parentOf(child)).toBe(second)
  })

  it('survives malformed input', () => {
    const blank = { ...pr(1, '', ''), branch: undefined, base: undefined } as unknown as StackablePr
    const dup = pr(2, 'a', 'main')
    const result = buildPrStacks([blank, dup, { ...dup }, null as unknown as StackablePr])
    expect(result.standalone).toEqual([blank, dup])
    expect(buildPrStacks(undefined).stacks).toEqual([])
    expect(result.chainOf(pr(99, 'zz', 'main'))).toEqual([])
  })
})
