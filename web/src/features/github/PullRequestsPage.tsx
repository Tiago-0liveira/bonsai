import { useMemo, useState } from 'react'
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
  Search,
  Send,
  X,
  XCircle,
} from 'lucide-react'
import { useBonsaiStore } from '../../stores/bonsai'
import type { PullRequest } from '../../types'

function CheckRow({ name, status }: PullRequest['checks'][number]) {
  return (
    <div className="flex items-center gap-2 rounded-md px-2 py-1.5 text-[10px]">
      {status === 'success' ? (
        <CheckCircle2 size={13} className="text-[rgb(var(--green))]" />
      ) : status === 'failed' ? (
        <XCircle size={13} className="text-[rgb(var(--red))]" />
      ) : (
        <CircleDot size={13} className="text-[rgb(var(--orange))]" />
      )}
      <span className="min-w-0 flex-1 truncate">{name}</span>
      <span className="capitalize text-[rgb(var(--muted-2))]">{status}</span>
    </div>
  )
}

function PrOperations({ pr }: { pr: PullRequest }) {
  const setStatus = useBonsaiStore((state) => state.setPullRequestStatus)
  return (
    <div className="flex flex-wrap items-center gap-2">
      {pr.status === 'Draft' && (
        <button onClick={() => setStatus(pr.id, 'Open')} className="bonsai-focus flex h-8 items-center gap-1.5 rounded-md border border-[rgb(var(--green)/.35)] bg-[rgb(var(--green)/.08)] px-2.5 text-[10px] text-[rgb(var(--green))]">
          <GitPullRequest size={11} /> Mark ready
        </button>
      )}
      {pr.status === 'Open' && (
        <>
          <button
            disabled={!pr.mergeable}
            onClick={() => setStatus(pr.id, 'Merged')}
            className="bonsai-focus flex h-8 items-center gap-1.5 rounded-md border border-[rgb(var(--purple)/.4)] bg-[rgb(var(--purple)/.12)] px-2.5 text-[10px] text-[rgb(var(--purple))] disabled:cursor-not-allowed disabled:opacity-35"
          >
            <GitMerge size={11} /> Merge
          </button>
          <button onClick={() => setStatus(pr.id, 'Closed')} className="bonsai-focus flex h-8 items-center gap-1.5 rounded-md border border-[rgb(var(--red)/.3)] px-2.5 text-[10px] text-[rgb(var(--red))]">
            <X size={11} /> Close
          </button>
        </>
      )}
      {pr.status === 'Closed' && (
        <button onClick={() => setStatus(pr.id, 'Open')} className="bonsai-focus flex h-8 items-center gap-1.5 rounded-md border border-[rgb(var(--green)/.35)] px-2.5 text-[10px] text-[rgb(var(--green))]">
          <GitPullRequest size={11} /> Reopen
        </button>
      )}
    </div>
  )
}

export function PullRequestsPage() {
  const pullRequests = useBonsaiStore((state) => state.pullRequests)
  const projects = useBonsaiStore((state) => state.projects)
  const activeProjectId = useBonsaiStore((state) => state.activeProjectId)
  const addReview = useBonsaiStore((state) => state.addPullRequestReview)
  const project = projects.find((item) => item.id === activeProjectId)
  const [query, setQuery] = useState('')
  const [selectedId, setSelectedId] = useState(pullRequests[0]?.id ?? '')
  const [descriptionOpen, setDescriptionOpen] = useState(true)
  const [commitsOpen, setCommitsOpen] = useState(false)
  const [review, setReview] = useState('')
  const selected = pullRequests.find((pr) => pr.id === selectedId) ?? pullRequests[0]

  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase()
    if (!needle) return pullRequests
    return pullRequests.filter((pr) =>
      (pr.title + ' ' + pr.branch + ' ' + pr.base + ' ' + pr.number).toLowerCase().includes(needle),
    )
  }, [pullRequests, query])

  if (!selected) return <div className="grid h-full place-items-center text-[11px] text-[rgb(var(--muted-2))]">No pull requests.</div>

  const checksPassed = selected.checks.filter((check) => check.status === 'success').length

  const submitReview = (kind: 'comment' | 'approve' | 'request-changes') => {
    addReview(selected.id, review, kind)
    setReview('')
  }

  return (
    <div className="flex h-full min-h-0 bg-[rgb(var(--bg))]">
      <aside className="flex w-[330px] shrink-0 flex-col border-r border-[rgb(var(--border))] bg-[rgb(var(--panel))]">
        <div className="flex h-12 items-center border-b border-[rgb(var(--border))] px-3">
          <GitPullRequest size={14} className="mr-2 text-[rgb(var(--muted))]" />
          <span className="font-medium">Pull Requests</span>
          <span className="ml-auto rounded bg-[rgb(var(--panel-3))] px-1.5 py-0.5 text-[9px] text-[rgb(var(--muted))]">{pullRequests.length}</span>
        </div>
        <div className="border-b border-[rgb(var(--border))] p-2">
          <label className="flex h-8 items-center gap-2 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-2">
            <Search size={11} className="text-[rgb(var(--muted-2))]" />
            <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search pull requests" className="min-w-0 flex-1 bg-transparent text-[10px] outline-none placeholder:text-[rgb(var(--muted-2))]" />
          </label>
        </div>
        <div className="min-h-0 flex-1 overflow-auto">
          {filtered.map((pr) => {
            const success = pr.checks.filter((check) => check.status === 'success').length
            return (
              <button
                key={pr.id}
                type="button"
                onClick={() => setSelectedId(pr.id)}
                className={
                  'w-full border-b border-[rgb(var(--border))] px-3 py-3 text-left transition-colors ' +
                  (pr.id === selected.id ? 'bg-[rgb(var(--purple)/.08)]' : 'hover:bg-[rgb(var(--panel-2))]')
                }
              >
                <div className="flex items-start gap-2">
                  <GitPullRequest size={12} className={pr.status === 'Open' ? 'mt-0.5 text-[rgb(var(--green))]' : pr.status === 'Draft' ? 'mt-0.5 text-[rgb(var(--purple))]' : 'mt-0.5 text-[rgb(var(--muted-2))]'} />
                  <span className="min-w-0 flex-1">
                    <span className="block text-[10px] font-medium leading-4">#{pr.number} {pr.title}</span>
                    <span className="mt-1 block truncate font-mono text-[8px] text-[rgb(var(--muted-2))]">{pr.branch} → {pr.base}</span>
                    <span className="mt-1.5 flex items-center gap-2 text-[8px] text-[rgb(var(--muted-2))]">
                      <span>{pr.status}</span>
                      <span>·</span>
                      <span>{success}/{pr.checks.length} checks</span>
                      <span>·</span>
                      <span>{pr.updatedAt}</span>
                    </span>
                  </span>
                </div>
              </button>
            )
          })}
        </div>
      </aside>

      <main className="min-w-0 flex-1 overflow-auto">
        <div className="mx-auto max-w-[920px] px-6 py-5">
          <header className="border-b border-[rgb(var(--border))] pb-4">
            <div className="flex items-start gap-3">
              <span className="mt-1 grid h-8 w-8 shrink-0 place-items-center rounded-full border border-[rgb(var(--green)/.35)] bg-[rgb(var(--green)/.08)] text-[rgb(var(--green))]">
                <GitPullRequest size={15} />
              </span>
              <div className="min-w-0 flex-1">
                <h1 className="text-[17px] font-semibold leading-6">{selected.title} <span className="font-normal text-[rgb(var(--muted-2))]">#{selected.number}</span></h1>
                <div className="mt-1.5 flex flex-wrap items-center gap-2 text-[10px] text-[rgb(var(--muted))]">
                  <span className={'rounded-full px-2 py-1 text-[9px] ' + (selected.status === 'Open' ? 'bg-[rgb(var(--green)/.12)] text-[rgb(var(--green))]' : selected.status === 'Draft' ? 'bg-[rgb(var(--purple)/.12)] text-[rgb(var(--purple))]' : 'bg-[rgb(var(--panel-3))]')}>{selected.status}</span>
                  <span>{selected.author ?? 'unknown'} wants to merge</span>
                  <span className="rounded bg-[rgb(var(--panel-2))] px-1.5 py-0.5 font-mono">{selected.branch}</span>
                  <span>into</span>
                  <span className="rounded bg-[rgb(var(--panel-2))] px-1.5 py-0.5 font-mono">{selected.base}</span>
                  <span>· updated {selected.updatedAt}</span>
                </div>
              </div>
              <button
                onClick={() => project?.repository.includes('/') && window.open('https://github.com/' + project.repository + '/pull/' + selected.number, '_blank', 'noopener,noreferrer')}
                className="bonsai-focus grid h-8 w-8 place-items-center rounded-md border border-[rgb(var(--border))] text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))] hover:text-[rgb(var(--text))]"
                title="Open on GitHub"
              >
                <ExternalLink size={12} />
              </button>
            </div>
            <div className="mt-3 flex justify-end"><PrOperations pr={selected} /></div>
          </header>

          <section className="mt-4 rounded-lg border border-[rgb(var(--border))] bg-[rgb(var(--panel))]">
            <button onClick={() => setDescriptionOpen((value) => !value)} className="flex h-10 w-full items-center gap-2 px-3 text-left text-[10px] font-medium">
              {descriptionOpen ? <ChevronDown size={11} /> : <ChevronRight size={11} />}
              Description
            </button>
            {descriptionOpen && <div className="border-t border-[rgb(var(--border))] px-4 py-3 text-[11px] leading-5 text-[rgb(var(--muted))]">{selected.description}</div>}
          </section>

          <section className="mt-3 rounded-lg border border-[rgb(var(--border))] bg-[rgb(var(--panel))]">
            <button onClick={() => setCommitsOpen((value) => !value)} className="flex h-10 w-full items-center gap-2 px-3 text-left text-[10px] font-medium">
              {commitsOpen ? <ChevronDown size={11} /> : <ChevronRight size={11} />}
              <GitCommitHorizontal size={11} />
              {selected.commits.length} commits
              <span className="ml-auto text-[8px] text-[rgb(var(--muted-2))]">latest {selected.commits.at(-1)?.time ?? selected.updatedAt}</span>
            </button>
            {commitsOpen && (
              <div className="border-t border-[rgb(var(--border))] px-3">
                {selected.commits.map((commit) => (
                  <div key={commit.sha} className="flex items-center gap-3 border-b border-[rgb(var(--border))] py-2.5 text-[10px] last:border-0">
                    <GitCommitHorizontal size={11} className="text-[rgb(var(--muted-2))]" />
                    <span className="min-w-0 flex-1 truncate">{commit.message}</span>
                    <span className="text-[8px] text-[rgb(var(--muted-2))]">{commit.author} · {commit.time}</span>
                    <span className="font-mono text-[8px] text-[rgb(var(--purple))]">{commit.sha}</span>
                  </div>
                ))}
              </div>
            )}
          </section>

          <section className="mt-5">
            <div className="mb-2 flex items-center gap-2 text-[10px] font-semibold"><MessageSquare size={12} /> Conversation</div>
            <div className="space-y-3">
              {selected.conversation.map((comment, index) => (
                <div key={index} className="rounded-lg border border-[rgb(var(--border))] bg-[rgb(var(--panel))]">
                  <div className="flex items-center gap-2 border-b border-[rgb(var(--border))] px-3 py-2 text-[9px] text-[rgb(var(--muted-2))]">
                    <span className="font-medium text-[rgb(var(--text))]">{comment.author}</span>
                    <span>{comment.kind === 'review' ? 'reviewed' : 'commented'} {comment.time}</span>
                  </div>
                  <div className="px-3 py-3 text-[11px] leading-5 text-[rgb(var(--muted))]">{comment.body}</div>
                </div>
              ))}

              <div className="rounded-lg border border-[rgb(var(--border))] bg-[rgb(var(--panel))]">
                <div className="flex items-center border-b border-[rgb(var(--border))] px-3 py-2 text-[10px] font-medium">
                  <CheckCircle2 size={12} className="mr-2 text-[rgb(var(--green))]" />
                  Checks
                  <span className="ml-auto text-[9px] text-[rgb(var(--muted-2))]">{checksPassed}/{selected.checks.length} successful</span>
                </div>
                <div className="p-1.5">{selected.checks.map((check) => <CheckRow key={check.name} {...check} />)}</div>
              </div>

              <div className="rounded-lg border border-[rgb(var(--border-strong))] bg-[rgb(var(--panel))] p-3">
                <div className="mb-2 text-[10px] font-medium">Add a review</div>
                <textarea
                  value={review}
                  onChange={(event) => setReview(event.target.value)}
                  rows={5}
                  placeholder="Leave a comment or review…"
                  className="bonsai-focus w-full resize-y rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-3 py-2.5 text-[11px] leading-5 outline-none"
                />
                <div className="mt-2 flex flex-wrap justify-end gap-2">
                  <button onClick={() => submitReview('request-changes')} className="bonsai-focus flex h-8 items-center gap-1.5 rounded-md border border-[rgb(var(--red)/.3)] px-2.5 text-[9px] text-[rgb(var(--red))]"><XCircle size={11} /> Request changes</button>
                  <button onClick={() => submitReview('approve')} className="bonsai-focus flex h-8 items-center gap-1.5 rounded-md border border-[rgb(var(--green)/.3)] px-2.5 text-[9px] text-[rgb(var(--green))]"><Check size={11} /> Approve</button>
                  <button disabled={!review.trim()} onClick={() => submitReview('comment')} className="bonsai-focus flex h-8 items-center gap-1.5 rounded-md bg-[rgb(var(--purple))] px-3 text-[9px] font-medium text-white disabled:opacity-35"><Send size={11} /> Comment</button>
                </div>
              </div>
            </div>
          </section>
        </div>
      </main>
    </div>
  )
}
