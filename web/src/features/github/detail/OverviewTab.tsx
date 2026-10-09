import { useMemo, useState } from 'react'
import type { PullRequest, Worktree } from '../../../types'
import type { PrStack } from '../../../lib/github/prStack'
import { parsePrBody, type PrSection } from '../../../lib/github/prBody'
import { formatPrTime } from '../../../lib/github/prTime'
import { Markdown } from '../../../components/Markdown'
import { Card } from './Card'
import { ChecksCard } from './ChecksCard'
import { MergeCard } from './MergeCard'
import { StackCard } from './StackCard'
import { WorktreeCard } from './WorktreeCard'

const CLAMP_CHARS = 700
const CLAMP_LINES = 14

/**
 * Markdown that folds behind "Show more" when it is long. Summary and Test
 * plan pass `fold={false}`: they show in full and the column scrolls instead.
 */
function ClampedMarkdown({ children, fold = true }: { children: string; fold?: boolean }) {
  const [expanded, setExpanded] = useState(false)
  const long = fold && (children.length > CLAMP_CHARS || children.split('\n').length > CLAMP_LINES)
  const clamped = long && !expanded
  return (
    <div className="px-3 py-[9px]">
      <div className={clamped ? 'relative max-h-[200px] overflow-hidden' : undefined}>
        <Markdown>{children}</Markdown>
        {clamped && <span className="pointer-events-none absolute inset-x-0 bottom-0 h-10 bg-gradient-to-t from-panel-2 to-transparent" aria-hidden="true" />}
      </div>
      {long && (
        <button type="button" aria-expanded={expanded} onClick={() => setExpanded((value) => !value)} className="bonsai-focus mt-1.5 rounded font-mono text-[10.5px] text-accent hover:underline">
          {expanded ? 'Show less' : 'Show more'}
        </button>
      )}
    </div>
  )
}

function BodyCards({ body }: { body: string }) {
  const parsed = useMemo(() => parsePrBody(body), [body])
  const rest = parsed.rest.filter((section: PrSection) => section.markdown)
  if (!parsed.summary && !parsed.testPlan && !rest.length) {
    return <Card title="Description"><p className="px-3 py-2.5 text-[12px] text-muted-2">No description provided.</p></Card>
  }
  return (
    <>
      {parsed.summary?.markdown && <Card title="Summary"><ClampedMarkdown fold={false}>{parsed.summary.markdown}</ClampedMarkdown></Card>}
      {parsed.testPlan?.markdown && (
        <Card title="Test plan" aside={parsed.testPlan.tasks && <span className="font-mono">{parsed.testPlan.tasks.done} of {parsed.testPlan.tasks.total}</span>}>
          <ClampedMarkdown fold={false}>{parsed.testPlan.markdown}</ClampedMarkdown>
        </Card>
      )}
      {rest.map((section, index) => (
        <Card key={`${section.heading ?? ''}:${index}`} title={section.heading || 'Description'}>
          <ClampedMarkdown>{section.markdown}</ClampedMarkdown>
        </Card>
      ))}
    </>
  )
}

function CommitsCard({ pr, onShowAll }: { pr: PullRequest; onShowAll: () => void }) {
  const latest = pr.commits.slice(-3)
  const last = pr.commits.at(-1)?.time
  return (
    <Card title="Commits" aside={<span className="font-mono">{pr.commits.length}{last ? ` · latest ${formatPrTime(last)}` : ''}</span>}>
      {latest.length ? (
        <ul className="py-1">
          {latest.map((commit) => (
            <li key={commit.sha} className="flex items-center gap-2.5 px-3 py-[3px] text-[12px] text-text">
              <span className="flex-none rounded bg-well px-1.5 font-mono text-[10.5px] leading-[18px] text-accent">{commit.sha.slice(0, 7)}</span>
              <span className="min-w-0 flex-1 truncate" title={commit.message}>{commit.message}</span>
              <span className="flex-none font-mono text-[10px] text-muted-2">{commit.time ? formatPrTime(commit.time) : ''}</span>
            </li>
          ))}
        </ul>
      ) : (
        <p className="px-3 py-2.5 text-[11.5px] text-muted-2">No commits loaded.</p>
      )}
      {pr.commits.length > latest.length && (
        <div className="border-t border-border-subtle px-3 py-1.5">
          <button type="button" onClick={onShowAll} className="bonsai-focus rounded font-mono text-[10.5px] text-accent hover:underline">View all {pr.commits.length} commits</button>
        </div>
      )}
    </Card>
  )
}

export interface OverviewTabProps {
  pr: PullRequest
  parent?: PullRequest
  stack?: PrStack<PullRequest>
  worktree?: Worktree
  onSelect: (id: string) => void
  onShowCommits: () => void
  onShowOnCanvas: (worktreeId: string) => void
}

/** Side by side when the island is wide enough; each column scrolls on its own when it is also tall enough. */
export function OverviewTab({ pr, parent, stack, worktree, onSelect, onShowCommits, onShowOnCanvas }: OverviewTabProps) {
  return (
    <div className="flex flex-col gap-3 pr-wide:flex-row pr-split:min-h-0 pr-split:flex-1">
      <div className="flex min-w-0 flex-1 flex-col gap-2.5 pr-split:min-h-0 pr-split:overflow-y-auto pr-split:pr-1">
        <BodyCards body={pr.description} />
        <CommitsCard pr={pr} onShowAll={onShowCommits} />
      </div>
      <div className="flex w-full flex-none flex-col gap-2.5 pr-wide:w-[292px] pr-split:min-h-0 pr-split:overflow-y-auto pr-split:pr-1">
        <MergeCard pr={pr} parent={parent} />
        <ChecksCard checks={pr.checks} />
        {stack && <StackCard stack={stack} currentId={pr.id} onSelect={onSelect} />}
        {worktree && <WorktreeCard worktree={worktree} onShowOnCanvas={onShowOnCanvas} />}
      </div>
    </div>
  )
}
