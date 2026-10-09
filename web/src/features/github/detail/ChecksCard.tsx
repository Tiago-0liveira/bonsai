import { CheckCircle2, LoaderCircle, XCircle } from 'lucide-react'
import type { PullRequest } from '../../../types'
import { sortChecks, summarizeChecks } from '../../../lib/github/prChecks'
import { formatCheckDuration } from '../../../lib/github/prTime'
import { ChecksBar } from '../ChecksBar'
import { Card } from './Card'

const STATUS_ICON = {
  success: <CheckCircle2 size={13} className="flex-none text-ok" aria-label="passed" />,
  running: <LoaderCircle size={13} className="flex-none animate-spin text-accent" aria-label="running" />,
  failed: <XCircle size={13} className="flex-none text-danger" aria-label="failed" />,
} as const

export function ChecksCard({ checks }: { checks: PullRequest['checks'] }) {
  const summary = summarizeChecks(checks)
  const sorted = sortChecks(checks)
  const tone = summary.failed ? 'text-danger' : summary.running ? 'text-accent' : 'text-ok'
  const durations = sorted.map((check) => formatCheckDuration(check.startedAt, check.completedAt))
  const showDurations = durations.some(Boolean)
  const detail = [summary.running && `${summary.running} running`, summary.failed && `${summary.failed} failing`].filter(Boolean).join(' · ')

  return (
    <Card title="Checks" aside={summary.total ? <span className={`font-mono ${tone}`}>{summary.passed}/{summary.total}{detail && ` · ${detail}`}</span> : undefined}>
      {summary.total ? (
        <>
          <div className="flex px-3 pb-[3px] pt-[7px]"><ChecksBar checks={checks} /></div>
          <ul className="pb-1.5">
            {sorted.map((check, index) => (
              <li key={check.id || `${check.name}:${index}`} className="flex h-5 items-center gap-2 px-3 text-[12px] text-text">
                {STATUS_ICON[check.status]}
                <span className="min-w-0 flex-1 truncate" title={check.name}>{check.name}</span>
                {showDurations && <span className="flex-none font-mono text-[10.5px] text-muted-2">{durations[index]}</span>}
              </li>
            ))}
          </ul>
        </>
      ) : (
        <p className="px-3 py-2.5 text-[11.5px] text-muted-2">No checks reported.</p>
      )}
    </Card>
  )
}
