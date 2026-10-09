import { CheckCircle2, LoaderCircle, XCircle } from 'lucide-react'
import type { PullRequest } from '../../../types'
import type { PrStack } from '../../../lib/github/prStack'
import { summarizeChecks } from '../../../lib/github/prChecks'
import { Card } from './Card'

function Status({ pr }: { pr: PullRequest }) {
  const summary = summarizeChecks(pr.checks)
  if (summary.failed) return <XCircle size={12} className="flex-none text-danger" aria-label="Checks failing" />
  if (summary.running) return <LoaderCircle size={12} className="flex-none animate-spin text-accent" aria-label="Checks running" />
  return <CheckCircle2 size={12} className="flex-none text-ok" aria-label={summary.total ? 'Checks passed' : 'No checks'} />
}

/** The whole stack, base first; every entry selects that PR. */
export function StackCard({ stack, currentId, onSelect }: {
  stack: PrStack<PullRequest>
  currentId: string
  onSelect: (id: string) => void
}) {
  return (
    <Card title="Stack" aside={<span className="font-mono">{stack.prs.length} PRs → {stack.root.base}</span>}>
      <ol className="px-3 py-2">
        <li className="flex h-[18px] items-center gap-2 font-mono text-[11px] font-semibold text-text">
          <span className="h-2.5 w-2.5 flex-none rounded-full bg-ok" aria-hidden="true" />
          {stack.root.base}
        </li>
        {stack.prs.map((pr) => {
          const current = pr.id === currentId
          return (
            <li key={pr.id}>
              <button
                type="button"
                aria-current={current ? 'true' : undefined}
                onClick={() => onSelect(pr.id)}
                className={'bonsai-focus group flex h-[18px] w-full items-center gap-2 rounded text-left font-mono text-[11px] ' + (current ? 'text-text' : 'text-muted hover:text-text')}
              >
                <span className={'h-2.5 w-2.5 flex-none rounded-full border-2 ' + (current ? 'border-ok bg-ok' : 'border-faint bg-panel-2')} aria-hidden="true" />
                <span className="w-7 flex-none text-muted-2">#{pr.number}</span>
                <span className={'min-w-0 flex-1 truncate ' + (current ? 'font-semibold' : '')} title={pr.title}>{pr.branch.split('/').pop()}</span>
                <Status pr={pr} />
              </button>
            </li>
          )
        })}
      </ol>
    </Card>
  )
}
