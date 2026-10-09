import { usePullRequestCatalog } from './usePullRequestCatalog'
import { PullRequestListPane } from './PullRequestListPane'
import { buildPrList } from '../../lib/github/prList'
import { loadPullRequest } from '../../api/git'
import { useEffect, useMemo, useState } from 'react'
import {
  Check,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  CircleDot,
  ExternalLink,
  GitCommitHorizontal,
  GitMerge,
  GitPullRequest,
  MessageSquare,
  Send,
  X,
  XCircle,
} from 'lucide-react'
import { useBonsaiStore } from '../../stores/bonsai'
import type { PullRequest } from '../../types'

function CheckRow({ name, status }: PullRequest['checks'][number]) {
  return (
    <div className="flex items-center gap-2 rounded-md px-2 py-1.5 text-[11px]">
      {status === 'success' ? (
        <CheckCircle2 size={13} className="text-ok" />
      ) : status === 'failed' ? (
        <XCircle size={13} className="text-danger" />
      ) : (
        <CircleDot size={13} className="text-warn" />
      )}
      <span className="min-w-0 flex-1 truncate">{name}</span>
      <span className="font-mono text-[10px] capitalize text-muted-2">{status}</span>
    </div>
  )
}

function PrOperations({ pr }: { pr: PullRequest }) {
  const setStatus = useBonsaiStore((state) => state.setPullRequestStatus)
  return (
    <div className="flex flex-wrap items-center gap-2">
      {pr.status === 'Draft' && (
        <button onClick={() => setStatus(pr.id, 'Open')} className="bonsai-focus flex h-8 items-center gap-1.5 btn-accent-tint rounded-[7px] px-2.5 text-[11px]">
          <GitPullRequest size={11} /> Mark ready
        </button>
      )}
      {pr.status === 'Open' && (
        <>
          <button
            disabled={!pr.mergeable}
            onClick={() => setStatus(pr.id, 'Merged')}
            className="bonsai-focus flex h-8 items-center gap-1.5 btn-accent-tint rounded-[7px] px-2.5 text-[11px] disabled:cursor-not-allowed disabled:opacity-35"
          >
            <GitMerge size={11} /> Merge
          </button>
          <button onClick={() => setStatus(pr.id, 'Closed')} className="bonsai-focus flex h-8 items-center gap-1.5 btn-danger-tint rounded-[7px] px-2.5 text-[11px]">
            <X size={11} /> Close
          </button>
        </>
      )}
      {pr.status === 'Closed' && (
        <button onClick={() => setStatus(pr.id, 'Open')} className="bonsai-focus flex h-8 items-center gap-1.5 btn-accent-tint rounded-[7px] px-2.5 text-[11px]">
          <GitPullRequest size={11} /> Reopen
        </button>
      )}
    </div>
  )
}

export interface PullRequestsPageProps {
  /** Selected PR id from the URL; falls back to local state, the inspector, then the first PR. */
  selectedId?: string
  onSelect?: (id: string) => void
}

export function PullRequestsPage({ selectedId: routeSelectedId, onSelect }: PullRequestsPageProps = {}) {
  const projects = useBonsaiStore((state) => state.projects)
  const activeProjectId = useBonsaiStore((state) => state.activeProjectId)
  const addReview = useBonsaiStore((state) => state.addPullRequestReview)
  const { rows: pullRequests, tab, setTab, openCount, closedCount, state, message, retry } = usePullRequestCatalog(activeProjectId)
  const project = projects.find((item) => item.id === activeProjectId)
  const inspectedId = useBonsaiStore((state) => state.inspectedPullRequestId)
  const [localId, setLocalId] = useState<string>()
  const [descriptionOpen, setDescriptionOpen] = useState(true)
  const [commitsOpen, setCommitsOpen] = useState(false)
  const [review, setReview] = useState('')
  const repository = project?.repository
  const preferredId = routeSelectedId ?? localId ?? inspectedId ?? undefined
  const firstId = useMemo(() => buildPrList(pullRequests, { repository }).ordered[0]?.id, [pullRequests, repository])
  const selected = pullRequests.find((pr) => pr.id === preferredId) ?? pullRequests.find((pr) => pr.id === firstId)
  const select = (id: string) => { setLocalId(id); onSelect?.(id) }

  useEffect(() => { if (selected?.id) void loadPullRequest(selected.id) }, [selected?.id, selected?.updatedAt])

  const checksPassed = selected?.checks.filter((check) => check.status === 'success').length ?? 0

  const submitReview = (kind: 'comment' | 'approve' | 'request-changes') => {
    if (!selected) return
    addReview(selected.id, review, kind)
    setReview('')
  }

  return (
    <div className="flex h-full min-h-0 gap-3">
      <PullRequestListPane
        tab={tab}
        onTabChange={setTab}
        openCount={openCount}
        closedCount={closedCount}
        rows={pullRequests}
        state={state}
        message={message}
        onRetry={retry}
        repository={repository}
        selectedId={selected?.id}
        onSelect={select}
      />

      {selected ? <main className="island min-w-0 flex-1 overflow-auto">
        <div className="mx-auto max-w-[920px] px-6 py-5">
          <header className="border-b border-border-subtle pb-4">
            <div className="flex items-start gap-3">
              <span className="mt-1 grid h-8 w-8 shrink-0 place-items-center rounded-full border border-accent/35 bg-accent/10 text-accent">
                <GitPullRequest size={15} />
              </span>
              <div className="min-w-0 flex-1">
                <h1 className="text-[17px] font-semibold leading-6">{selected.title} <span className="font-normal text-muted-2">#{selected.number}</span></h1>
                <div className="mt-1.5 flex flex-wrap items-center gap-2 text-[10px] text-muted">
                  <span className={'rounded-full px-2 py-1 font-mono text-[10px] ' + (selected.status === 'Open' ? 'bg-accent/12 text-accent' : selected.status === 'Merged' ? 'bg-ok/12 text-ok' : selected.status === 'Draft' ? 'bg-panel-3 text-muted' : 'bg-panel-3 text-muted-2')}>{selected.status}</span>
                  <span>{selected.author ?? 'unknown'} wants to merge</span>
                  <span className="rounded bg-panel-2 px-1.5 py-0.5 font-mono">{selected.branch}</span>
                  <span>into</span>
                  <span className="rounded bg-panel-2 px-1.5 py-0.5 font-mono">{selected.base}</span>
                  <span>· updated {selected.updatedAt}</span>
                </div>
              </div>
              <button
                onClick={() => project?.repository.includes('/') && window.open('https://github.com/' + project.repository + '/pull/' + selected.number, '_blank', 'noopener,noreferrer')}
                className="bonsai-focus grid h-8 w-8 place-items-center rounded-md border border-border text-muted hover:bg-panel-3 hover:text-text"
                title="Open on GitHub"
              >
                <ExternalLink size={12} />
              </button>
            </div>
            <div className="mt-3 flex justify-end"><PrOperations pr={selected} /></div>
          </header>

          <section className="mt-4 rounded-[10px] border border-border bg-panel-2">
            <button onClick={() => setDescriptionOpen((value) => !value)} className="flex h-10 w-full items-center gap-2 px-3 text-left text-[11px] font-medium">
              {descriptionOpen ? <ChevronDown size={11} /> : <ChevronRight size={11} />}
              Description
            </button>
            {descriptionOpen && <div className="border-t border-border-subtle px-4 py-3 text-[12px] leading-5 text-muted">{selected.description}</div>}
          </section>

          <section className="mt-3 rounded-[10px] border border-border bg-panel-2">
            <button onClick={() => setCommitsOpen((value) => !value)} className="flex h-10 w-full items-center gap-2 px-3 text-left text-[10px] font-medium">
              {commitsOpen ? <ChevronDown size={11} /> : <ChevronRight size={11} />}
              <GitCommitHorizontal size={11} />
              {selected.commits.length} commits
              <span className="ml-auto font-mono text-[10px] text-muted-2">latest {selected.commits.at(-1)?.time ?? selected.updatedAt}</span>
            </button>
            {commitsOpen && (
              <div className="border-t border-border-subtle px-3">
                {selected.commits.map((commit) => (
                  <div key={commit.sha} className="flex items-center gap-3 border-b border-border-subtle py-2.5 text-[11px] last:border-0">
                    <GitCommitHorizontal size={11} className="text-muted-2" />
                    <span className="min-w-0 flex-1 truncate">{commit.message}</span>
                    <span className="font-mono text-[10px] text-muted-2">{commit.author} · {commit.time}</span>
                    <span className="font-mono text-[10px] text-accent">{commit.sha}</span>
                  </div>
                ))}
              </div>
            )}
          </section>

          <section className="mt-5">
            <div className="mb-2 flex items-center gap-2 text-[10px] font-semibold"><MessageSquare size={12} /> Conversation</div>
            <div className="space-y-3">
              {selected.conversation.map((comment, index) => (
                <div key={index} className="rounded-[10px] border border-border bg-panel-2">
                  <div className="flex items-center gap-2 border-b border-border-subtle px-3 py-2 font-mono text-[10px] text-muted-2">
                    <span className="font-medium text-text">{comment.author}</span>
                    <span>{comment.kind === 'review' ? 'reviewed' : 'commented'} {comment.time}</span>
                  </div>
                  <div className="px-3 py-3 text-[12px] leading-5 text-muted">{comment.body}</div>
                </div>
              ))}

              <div className="rounded-[10px] border border-border bg-panel-2">
                <div className="flex items-center border-b border-border-subtle px-3 py-2 text-[11px] font-medium">
                  <CheckCircle2 size={12} className="mr-2 text-ok" />
                  Checks
                  <span className="ml-auto font-mono text-[10px] text-muted-2">{checksPassed}/{selected.checks.length} successful</span>
                </div>
                <div className="p-1.5">{selected.checks.map((check, index) => <CheckRow key={check.id || `${check.name}:${index}`} {...check} />)}</div>
              </div>

              <div className="rounded-[10px] border border-border-strong bg-panel-2 p-3">
                <div className="mb-2 text-[10px] font-medium">Add a review</div>
                <textarea
                  value={review}
                  onChange={(event) => setReview(event.target.value)}
                  rows={5}
                  placeholder="Leave a comment or review…"
                  className="bonsai-focus w-full resize-y rounded-[7px] border border-border bg-well px-3 py-2.5 text-[12px] leading-5 outline-none placeholder:text-muted-2 focus:border-accent/55"
                />
                <div className="mt-2 flex flex-wrap justify-end gap-2">
                  <button onClick={() => submitReview('request-changes')} className="bonsai-focus btn-danger-tint flex h-8 items-center gap-1.5 rounded-[7px] px-2.5 text-[11px]"><XCircle size={11} /> Request changes</button>
                  <button onClick={() => submitReview('approve')} className="bonsai-focus flex h-8 items-center gap-1.5 rounded-[7px] border border-ok/35 bg-ok/12 px-2.5 text-[11px] text-ok"><Check size={11} /> Approve</button>
                  <button disabled={!review.trim()} onClick={() => submitReview('comment')} className="bonsai-focus btn-primary flex h-8 items-center gap-1.5 px-3 text-[11px] disabled:opacity-35"><Send size={11} /> Comment</button>
                </div>
              </div>
            </div>
          </section>
        </div>
      </main> : <div className="island grid flex-1 place-items-center text-[11px] text-muted">{message}</div>}
    </div>
  )
}
