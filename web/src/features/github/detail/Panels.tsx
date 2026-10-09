import { useState } from 'react'
import { Check, ChevronDown, ChevronRight, GitCommitHorizontal, MessageSquare, Send, XCircle } from 'lucide-react'
import type { PullRequest } from '../../../types'
import { useBonsaiStore } from '../../../stores/bonsai'

export function CommitsPanel({ commits }: { commits: PullRequest['commits'] }) {
  if (!commits.length) return <p className="text-[11.5px] text-muted-2">No commits loaded.</p>
  return (
    <div className="rounded-[10px] border border-border bg-panel-2 px-3">
      {commits.map((commit) => (
        <div key={commit.sha} className="flex items-center gap-3 border-b border-border-subtle py-2.5 text-[11px] last:border-0">
          <GitCommitHorizontal size={11} className="flex-none text-muted-2" aria-hidden="true" />
          <span className="min-w-0 flex-1 truncate">{commit.message}</span>
          <span className="flex-none font-mono text-[10px] text-muted-2">{commit.author}{commit.time ? ` · ${commit.time}` : ''}</span>
          <span className="flex-none font-mono text-[10px] text-accent">{commit.sha}</span>
        </div>
      ))}
    </div>
  )
}

const diffTone = (line: string) => (line.startsWith('+') ? 'text-ok' : line.startsWith('-') ? 'text-danger' : 'text-muted')

export function FilesPanel({ files }: { files: PullRequest['files'] }) {
  const [open, setOpen] = useState<Set<string>>(new Set())
  if (!files.length) return <p className="text-[11.5px] text-muted-2">No changed files loaded.</p>
  const toggle = (path: string) => setOpen((current) => {
    const next = new Set(current)
    if (!next.delete(path)) next.add(path)
    return next
  })
  return (
    <div className="flex flex-col gap-2">
      {files.map((file) => {
        const expanded = open.has(file.path)
        return (
          <div key={file.path} className="overflow-hidden rounded-[10px] border border-border bg-panel-2">
            <button type="button" aria-expanded={expanded} onClick={() => toggle(file.path)} className="bonsai-focus flex h-9 w-full items-center gap-2 px-3 text-left text-[11px]">
              {expanded ? <ChevronDown size={11} aria-hidden="true" /> : <ChevronRight size={11} aria-hidden="true" />}
              <span className="min-w-0 flex-1 truncate font-mono" title={file.path}>{file.path}</span>
              <span className="flex-none font-mono text-[10px] text-ok">+{file.additions}</span>
              <span className="flex-none font-mono text-[10px] text-danger">−{file.deletions}</span>
            </button>
            {expanded && (
              <pre className="m-0 overflow-x-auto border-t border-border-subtle bg-well px-3 py-2 font-mono text-[11px] leading-[17px]">
                {file.diff.length ? file.diff.map((line, index) => <div key={index} className={diffTone(line)}>{line || ' '}</div>) : <span className="text-muted-2">No diff available.</span>}
              </pre>
            )}
          </div>
        )
      })}
    </div>
  )
}

export function ConversationPanel({ pr }: { pr: PullRequest }) {
  const addReview = useBonsaiStore((state) => state.addPullRequestReview)
  const [review, setReview] = useState('')
  const submit = (kind: 'comment' | 'approve' | 'request-changes') => {
    addReview(pr.id, review, kind)
    setReview('')
  }
  return (
    <section aria-label="Conversation" className="space-y-3">
      {!pr.conversation.length && <p className="flex items-center gap-2 text-[11.5px] text-muted-2"><MessageSquare size={12} aria-hidden="true" /> No comments yet.</p>}
      {pr.conversation.map((comment, index) => (
        <div key={index} className="rounded-[10px] border border-border bg-panel-2">
          <div className="flex items-center gap-2 border-b border-border-subtle px-3 py-2 font-mono text-[10px] text-muted-2">
            <span className="font-medium text-text">{comment.author}</span>
            <span>{comment.kind === 'review' ? 'reviewed' : 'commented'} {comment.time}</span>
          </div>
          <div className="break-words px-3 py-3 text-[12px] leading-5 text-muted">{comment.body}</div>
        </div>
      ))}

      <div className="rounded-[10px] border border-border-strong bg-panel-2 p-3">
        <label htmlFor={`review-${pr.id}`} className="mb-2 block text-[10px] font-medium">Add a review</label>
        <textarea
          id={`review-${pr.id}`}
          value={review}
          onChange={(event) => setReview(event.target.value)}
          rows={5}
          placeholder="Leave a comment or review…"
          className="bonsai-focus w-full resize-y rounded-[7px] border border-border bg-well px-3 py-2.5 text-[12px] leading-5 outline-none placeholder:text-muted-2 focus:border-accent/55"
        />
        <div className="mt-2 flex flex-wrap justify-end gap-2">
          <button type="button" onClick={() => submit('request-changes')} className="bonsai-focus btn-danger-tint flex h-8 items-center gap-1.5 rounded-[7px] px-2.5 text-[11px]"><XCircle size={11} aria-hidden="true" /> Request changes</button>
          <button type="button" onClick={() => submit('approve')} className="bonsai-focus flex h-8 items-center gap-1.5 rounded-[7px] border border-ok/35 bg-ok/[.12] px-2.5 text-[11px] text-ok"><Check size={11} aria-hidden="true" /> Approve</button>
          <button type="button" disabled={!review.trim()} onClick={() => submit('comment')} className="bonsai-focus btn-primary flex h-8 items-center gap-1.5 px-3 text-[11px] disabled:opacity-35"><Send size={11} aria-hidden="true" /> Comment</button>
        </div>
      </div>
    </section>
  )
}

