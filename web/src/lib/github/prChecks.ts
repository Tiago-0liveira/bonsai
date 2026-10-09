import type { PullRequest } from '../../types'

export type PrCheck = PullRequest['checks'][number]

export interface ChecksSummary {
  passed: number
  running: number
  failed: number
  total: number
}

const ORDER: Record<PrCheck['status'], number> = { running: 0, failed: 1, success: 2 }

/** Checks with an unrecognized status are ignored so the buckets always add up to `total`. */
export function summarizeChecks(checks: readonly PrCheck[] | null | undefined): ChecksSummary {
  const summary: ChecksSummary = { passed: 0, running: 0, failed: 0, total: 0 }
  for (const check of checks ?? []) {
    if (check?.status === 'success') summary.passed++
    else if (check?.status === 'running') summary.running++
    else if (check?.status === 'failed') summary.failed++
    else continue
    summary.total++
  }
  return summary
}

/** Running first, then failed, then passed. Stable within each group. */
export function sortChecks(checks: readonly PrCheck[] | null | undefined): PrCheck[] {
  return (checks ?? [])
    .filter((check) => check && Object.hasOwn(ORDER, check.status))
    .map((check, index) => ({ check, index }))
    .sort((a, b) => ORDER[a.check.status] - ORDER[b.check.status] || a.index - b.index)
    .map(({ check }) => check)
}
