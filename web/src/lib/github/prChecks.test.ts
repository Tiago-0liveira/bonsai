import type { PrCheck } from './prChecks'
import { sortChecks, summarizeChecks } from './prChecks'

const checks: PrCheck[] = [
  { name: 'a', status: 'success' },
  { name: 'b', status: 'failed' },
  { name: 'c', status: 'running' },
  { name: 'd', status: 'success' },
  { name: 'e', status: 'running' },
  { name: 'f', status: 'failed' },
]

describe('summarizeChecks', () => {
  it('counts every bucket', () => {
    expect(summarizeChecks(checks)).toEqual({ passed: 2, running: 2, failed: 2, total: 6 })
  })

  it.each([[[]], [undefined], [null]])('returns zeros for %j', (input) => {
    expect(summarizeChecks(input as PrCheck[] | undefined)).toEqual({ passed: 0, running: 0, failed: 0, total: 0 })
  })

  it('ignores unknown statuses so buckets add up to total', () => {
    const odd = [...checks, { name: 'x', status: 'skipped' }, null] as unknown as PrCheck[]
    const summary = summarizeChecks(odd)
    expect(summary.total).toBe(6)
    expect(summary.passed + summary.running + summary.failed).toBe(summary.total)
  })
})

describe('sortChecks', () => {
  it('orders running, failed, passed and keeps input order within a group', () => {
    expect(sortChecks(checks).map((check) => check.name)).toEqual(['c', 'e', 'b', 'f', 'a', 'd'])
  })

  it('does not mutate the input and tolerates missing input', () => {
    const copy = [...checks]
    sortChecks(checks)
    expect(checks).toEqual(copy)
    expect(sortChecks(undefined)).toEqual([])
  })

  it('drops unknown statuses', () => {
    const odd = [{ name: 'x', status: 'skipped' }, checks[0]] as unknown as PrCheck[]
    expect(sortChecks(odd)).toEqual([checks[0]])
  })
})
