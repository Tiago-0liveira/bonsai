import { useMemo, useState } from 'react'
import type { KeyboardEvent } from 'react'
import type { PullRequest, Worktree } from '../../types'
import { buildPrStacks } from '../../lib/github/prStack'
import { findPullRequestWorktree, isDetailLoaded, pullRequestUrl } from '../../lib/github/prDetail'
import { DetailHeader } from './detail/DetailHeader'
import { OverviewTab } from './detail/OverviewTab'
import { CommitsPanel, ConversationPanel, FilesPanel } from './detail/Panels'
import { Tiles } from './detail/Tiles'

const TABS = ['overview', 'commits', 'files', 'conversation'] as const
type TabKey = typeof TABS[number]

const LABELS: Record<TabKey, string> = { overview: 'Overview', commits: 'Commits', files: 'Files', conversation: 'Conversation' }

export interface PullRequestDetailProps {
  pr: PullRequest
  /** Every PR in the list, used to place this one in its stack. */
  pullRequests: readonly PullRequest[]
  /** Rows currently shown in the list, for previous/next. */
  orderedIds: readonly string[]
  repository?: string
  projectId: string
  worktrees: readonly Worktree[]
  onSelect: (id: string) => void
  onShowOnCanvas: (worktreeId: string) => void
}

export function PullRequestDetail({ pr, pullRequests, orderedIds, repository, projectId, worktrees, onSelect, onShowOnCanvas }: PullRequestDetailProps) {
  const [tab, setTab] = useState<TabKey>('overview')
  const stacks = useMemo(() => buildPrStacks(pullRequests, { repository }), [pullRequests, repository])
  const parent = stacks.parentOf(pr)
  const stack = stacks.stackOf(pr)
  const worktree = useMemo(() => findPullRequestWorktree(worktrees, pr, projectId), [worktrees, pr, projectId])
  const position = orderedIds.indexOf(pr.id)
  const loaded = isDetailLoaded(pr)
  const counts: Record<TabKey, number | undefined> = {
    overview: undefined,
    commits: loaded ? pr.commits.length : undefined,
    files: loaded ? pr.totals?.changedFiles ?? pr.files.length : undefined,
    conversation: loaded ? pr.conversation.length : undefined,
  }

  const onTabKeyDown = (event: KeyboardEvent<HTMLButtonElement>) => {
    const index = TABS.indexOf(tab)
    const next =
      event.key === 'ArrowRight' ? TABS[(index + 1) % TABS.length]
        : event.key === 'ArrowLeft' ? TABS[(index + TABS.length - 1) % TABS.length]
          : event.key === 'Home' ? TABS[0]
            : event.key === 'End' ? TABS[TABS.length - 1]
              : undefined
    if (!next) return
    event.preventDefault()
    setTab(next)
    document.getElementById(`pr-tab-${next}`)?.focus()
  }

  return (
    <main aria-label="Pull request detail" className="island flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
      <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
        <DetailHeader
          pr={pr}
          parent={parent}
          url={pullRequestUrl(repository, pr.number)}
          prevId={position > 0 ? orderedIds[position - 1] : undefined}
          nextId={position >= 0 ? orderedIds[position + 1] : undefined}
          onSelect={onSelect}
        />
        <Tiles pr={pr} />

        <div role="tablist" aria-label="Pull request sections" className="flex h-8 flex-none items-center gap-[22px] overflow-x-auto border-y border-border-subtle px-4">
          {TABS.map((key) => {
            const selected = key === tab
            return (
              <button
                key={key}
                id={`pr-tab-${key}`}
                type="button"
                role="tab"
                aria-selected={selected}
                aria-controls={`pr-panel-${key}`}
                tabIndex={selected ? 0 : -1}
                onClick={() => setTab(key)}
                onKeyDown={onTabKeyDown}
                className={
                  'bonsai-focus inline-flex h-full flex-none items-center font-mono text-[10.5px] font-medium uppercase tracking-[0.08em] ' +
                  (selected ? 'text-text shadow-[inset_0_-2px_0_0_rgb(var(--accent))]' : 'text-muted-2 hover:text-muted')
                }
              >
                {LABELS[key]}
                {counts[key] !== undefined && <span className="ml-1.5 rounded bg-panel-4 px-[5px] text-[9.5px] leading-[15px] tracking-normal text-muted-2">{counts[key]}</span>}
              </button>
            )
          })}
        </div>

        <div role="tabpanel" id={`pr-panel-${tab}`} aria-labelledby={`pr-tab-${tab}`} className="flex flex-col px-4 pb-3 pt-2.5 md:min-h-[320px] md:flex-1 md:overflow-hidden">
          {tab === 'overview' && (
            <OverviewTab pr={pr} parent={parent} stack={stack} worktree={worktree} onSelect={onSelect} onShowCommits={() => setTab('commits')} onShowOnCanvas={onShowOnCanvas} />
          )}
          {tab === 'commits' && <div className="md:min-h-0 md:flex-1 md:overflow-y-auto"><CommitsPanel commits={pr.commits} /></div>}
          {tab === 'files' && <div className="md:min-h-0 md:flex-1 md:overflow-y-auto"><FilesPanel files={pr.files} /></div>}
          {tab === 'conversation' && <div className="md:min-h-0 md:flex-1 md:overflow-y-auto"><ConversationPanel pr={pr} /></div>}
        </div>
      </div>
    </main>
  )
}
