import { CheckCircle2, CircleAlert, GitMerge, GitPullRequest, LoaderCircle, TriangleAlert, X } from 'lucide-react'
import type { PullRequest } from '../../../types'
import { describeMerge, type MergeRequirement, type Tone } from '../../../lib/github/prDetail'
import { useBonsaiStore } from '../../../stores/bonsai'
import { TONE_TEXT } from './Card'

const BORDER: Record<Tone, string> = {
  ok: 'border-accent/40 shadow-[0_0_0_3px_rgb(var(--accent)/.06)]',
  warn: 'border-warn/40',
  danger: 'border-danger/40',
  muted: 'border-border',
}

function RequirementIcon({ row }: { row: MergeRequirement }) {
  const tone = TONE_TEXT[row.tone]
  if (row.pending) return <LoaderCircle size={13} className={`flex-none animate-spin ${tone}`} aria-hidden="true" />
  if (row.tone === 'ok') return <CheckCircle2 size={13} className={`flex-none ${tone}`} aria-hidden="true" />
  if (row.tone === 'muted') return <CircleAlert size={13} className={`flex-none ${tone}`} aria-hidden="true" />
  return <TriangleAlert size={13} className={`flex-none ${tone}`} aria-hidden="true" />
}

export function MergeCard({ pr, parent }: { pr: PullRequest; parent?: PullRequest }) {
  const setStatus = useBonsaiStore((state) => state.setPullRequestStatus)
  const merge = describeMerge({ pr, parent })
  const TitleIcon = merge.tone === 'ok' ? CheckCircle2 : merge.tone === 'muted' ? CircleAlert : TriangleAlert
  const action = 'bonsai-focus inline-flex h-[30px] items-center justify-center gap-[7px] px-3 text-[12.5px]'

  return (
    <section aria-label="Merge" className={`overflow-hidden rounded-xl border bg-panel-2 ${BORDER[merge.tone]}`}>
      <div className="flex items-center gap-2 px-3 pb-1 pt-2 text-[13px] font-semibold text-text">
        <TitleIcon size={15} className={TONE_TEXT[merge.tone]} aria-hidden="true" />
        {merge.title}
      </div>
      <ul className="px-3">
        {merge.requirements.map((row) => (
          <li key={row.key} className="flex h-[19px] items-center gap-2 text-[11.5px] text-muted">
            <RequirementIcon row={row} />
            <span className="min-w-0 truncate" title={row.text}>{row.text}</span>
          </li>
        ))}
      </ul>
      <div className="flex gap-1.5 px-3 pb-2.5 pt-2">
        {pr.status === 'Draft' && (
          <button type="button" onClick={() => setStatus(pr.id, 'Open')} className={`${action} btn-primary flex-1`}>
            <GitPullRequest size={13} aria-hidden="true" /> Mark ready
          </button>
        )}
        {pr.status === 'Open' && (
          <>
            <button
              type="button"
              disabled={!!merge.blocker}
              title={merge.blocker}
              onClick={() => setStatus(pr.id, 'Merged')}
              className={`${action} btn-primary flex-1 disabled:cursor-not-allowed disabled:opacity-35`}
            >
              <GitMerge size={13} aria-hidden="true" /> Merge
            </button>
            <button type="button" onClick={() => setStatus(pr.id, 'Closed')} className={`${action} btn-danger-tint rounded-lg font-semibold`}>
              <X size={12} aria-hidden="true" /> Close
            </button>
          </>
        )}
        {pr.status === 'Closed' && (
          <button type="button" onClick={() => setStatus(pr.id, 'Open')} className={`${action} btn-accent-tint flex-1 rounded-lg`}>
            <GitPullRequest size={13} aria-hidden="true" /> Reopen
          </button>
        )}
      </div>
    </section>
  )
}
