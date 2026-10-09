import { useEffect, useState } from 'react'
import { ChevronDown, ChevronUp, Copy, ExternalLink, GitBranch, GitPullRequest, GitPullRequestDraft } from 'lucide-react'
import type { PullRequest } from '../../../types'
import { authorInitials } from '../../../lib/github/prDetail'
import { formatPrTime } from '../../../lib/github/prTime'

const STATE_CHIP: Record<PullRequest['status'], string> = {
  Open: 'border-accent/40 bg-accent/[.14] text-accent',
  Draft: 'border-border-strong bg-panel-3 text-muted',
  Merged: 'border-ok/40 bg-ok/[.14] text-ok',
  Closed: 'border-danger/40 bg-danger-solid/20 text-danger',
}

const ICON_BUTTON = 'bonsai-focus grid h-[22px] w-[22px] place-items-center rounded-md text-muted-2 transition-colors hover:bg-panel-3 hover:text-text disabled:pointer-events-none disabled:opacity-35'

export interface DetailHeaderProps {
  pr: PullRequest
  /** The PR this one is stacked on. */
  parent?: PullRequest
  url?: string
  prevId?: string
  nextId?: string
  onSelect: (id: string) => void
}

export function DetailHeader({ pr, parent, url, prevId, nextId, onSelect }: DetailHeaderProps) {
  const [copied, setCopied] = useState(false)
  useEffect(() => {
    if (!copied) return
    const timer = window.setTimeout(() => setCopied(false), 1500)
    return () => window.clearTimeout(timer)
  }, [copied])

  const copy = () => {
    if (!url) return
    void navigator.clipboard?.writeText(url).then(() => setCopied(true), () => undefined)
  }
  const Icon = pr.status === 'Draft' ? GitPullRequestDraft : GitPullRequest

  return (
    <>
      <div className="island-title flex-none justify-between pr-2">
        <span className="inline-flex items-center gap-[7px]">
          <GitPullRequest size={12} className="text-muted-2" aria-hidden="true" />
          <span>Pull request <span className="text-faint">/</span> #{pr.number}</span>
        </span>
        <span className="inline-flex items-center gap-0.5">
          <button type="button" aria-label="Previous pull request" title="Previous pull request" disabled={!prevId} onClick={() => prevId && onSelect(prevId)} className={ICON_BUTTON}><ChevronUp size={12} /></button>
          <button type="button" aria-label="Next pull request" title="Next pull request" disabled={!nextId} onClick={() => nextId && onSelect(nextId)} className={ICON_BUTTON}><ChevronDown size={12} /></button>
          <span className="mx-1 h-3 w-px bg-border" aria-hidden="true" />
          <button type="button" aria-label={copied ? 'Link copied' : 'Copy link'} title={copied ? 'Link copied' : 'Copy link'} disabled={!url} onClick={copy} className={ICON_BUTTON}><Copy size={12} /></button>
          <button
            type="button"
            disabled={!url}
            onClick={() => url && window.open(url, '_blank', 'noopener,noreferrer')}
            className="bonsai-focus btn-ghost h-[22px] gap-1.5 px-1.5 text-[10.5px] normal-case tracking-normal disabled:pointer-events-none disabled:opacity-35"
          >
            <ExternalLink size={11} aria-hidden="true" /> Open on GitHub
          </button>
        </span>
      </div>

      <div className="flex gap-3.5 px-4 pb-2.5 pt-3">
        <span className={'grid h-[42px] w-[42px] flex-none place-items-center rounded-xl border ' + (pr.status === 'Open' ? 'border-accent/35 bg-accent/[.12] text-accent' : 'border-border bg-panel-2 text-muted')}>
          <Icon size={20} aria-hidden="true" />
        </span>
        <div className="min-w-0 flex-1">
          <h1 className="break-words text-[20px] font-semibold leading-[26px] tracking-[-0.01em] text-text">
            {pr.title} <span className="font-mono text-[15px] font-normal text-muted-2">#{pr.number}</span>
          </h1>
          <div className="mt-2 flex flex-wrap items-center gap-2">
            <span className={'inline-flex h-5 flex-none items-center gap-[5px] rounded-[5px] border px-[7px] font-mono text-[10.5px] font-semibold ' + STATE_CHIP[pr.status]}>
              <span className="h-1.5 w-1.5 rounded-full bg-current" aria-hidden="true" />
              {pr.status}
            </span>
            <span className="inline-flex h-[22px] min-w-0 max-w-full items-center gap-1.5 rounded-md border border-border bg-well px-2 font-mono text-[11px]" title={`${pr.branch} → ${pr.base}`}>
              <GitBranch size={11} className="flex-none text-muted-2" aria-hidden="true" />
              <span className="min-w-0 truncate text-text">{pr.branch}</span>
              <span className="flex-none text-accent" aria-hidden="true">→</span>
              <span className="min-w-0 truncate text-ok">{pr.base}</span>
            </span>
            {parent && (
              <button
                type="button"
                onClick={() => onSelect(parent.id)}
                title={`Stacked on #${parent.number}: ${parent.title}`}
                className="bonsai-focus inline-flex h-5 flex-none items-center rounded-[5px] border border-warn/40 bg-warn-solid/[.14] px-[7px] font-mono text-[10.5px] text-warn hover:bg-warn-solid/25"
              >
                stacked on #{parent.number}
              </button>
            )}
            <span className="inline-flex min-w-0 items-center gap-1.5 text-[12px] text-muted">
              <span className="grid h-[18px] w-[18px] flex-none place-items-center rounded-full bg-accent-solid font-mono text-[7px] font-semibold text-accent-fg" aria-hidden="true">{authorInitials(pr.author)}</span>
              <span className="truncate">{pr.author ?? 'unknown'}</span>
              <span className="text-faint" aria-hidden="true">·</span>
              <span className="whitespace-nowrap font-mono text-[10.5px] text-muted-2" title={pr.updatedAt}>updated {formatPrTime(pr.updatedAt) || 'recently'}</span>
            </span>
          </div>
        </div>
      </div>
    </>
  )
}
