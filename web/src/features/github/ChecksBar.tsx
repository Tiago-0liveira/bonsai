import type { PullRequest } from '../../types'
import { summarizeChecks } from '../../lib/github/prChecks'

/** Above this many checks the bar shows one block per status instead of one per check. */
const MAX_SEGMENTS = 24

const SEGMENT_TONE = {
  passed: 'bg-ok/70',
  running: 'bg-accent animate-pulse',
  failed: 'bg-danger',
} as const

/** Segmented bar of check results: passed first, then running, then failed. */
export function ChecksBar({ checks }: { checks: PullRequest['checks'] }) {
  const summary = summarizeChecks(checks)
  const blocks = (['passed', 'running', 'failed'] as const)
    .filter((status) => summary[status] > 0)
    .flatMap((status) =>
      summary.total > MAX_SEGMENTS
        ? [{ status, weight: summary[status] }]
        : Array.from({ length: summary[status] }, () => ({ status, weight: 1 })),
    )
  return (
    <span className="flex min-w-0 flex-1 gap-0.5" aria-hidden="true">
      {blocks.map((block, index) => (
        <span key={index} style={{ flexGrow: block.weight }} className={'h-[3px] flex-1 basis-0 rounded-sm ' + SEGMENT_TONE[block.status]} />
      ))}
    </span>
  )
}
