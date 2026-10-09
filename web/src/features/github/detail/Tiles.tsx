import type { ReactNode } from 'react'
import type { PullRequest } from '../../../types'
import { summarizeChecks } from '../../../lib/github/prChecks'
import { summarizeChanges, summarizeReviews } from '../../../lib/github/prDetail'
import { formatPrTime } from '../../../lib/github/prTime'
import { ChecksBar } from '../ChecksBar'

function Tile({ label, accent, children }: { label: string; accent: string; children: ReactNode }) {
  return (
    <div className="relative min-w-0 overflow-hidden rounded-[10px] border border-border bg-well px-3 pb-[9px] pt-2">
      <span className={`absolute inset-x-0 top-0 h-0.5 ${accent}`} aria-hidden="true" />
      <div className="font-mono text-[9.5px] uppercase tracking-[0.08em] text-muted-2">{label}</div>
      {children}
    </div>
  )
}

const Figure = ({ children }: { children: ReactNode }) => <span className="font-mono text-[17px] font-semibold leading-[22px] text-text">{children}</span>
const Sub = ({ tone = 'text-muted-2', children }: { tone?: string; children: ReactNode }) => <span className={`truncate font-mono text-[10.5px] ${tone}`}>{children}</span>
const Row = ({ children }: { children: ReactNode }) => <div className="mt-[3px] flex items-baseline gap-2 whitespace-nowrap">{children}</div>

export function Tiles({ pr }: { pr: PullRequest }) {
  const checks = summarizeChecks(pr.checks)
  const reviews = summarizeReviews(pr.conversation)
  const changes = summarizeChanges(pr.files)
  const latest = pr.commits.at(-1)?.time ?? pr.updatedAt

  const checksTone = checks.failed ? 'text-danger' : checks.running ? 'text-accent' : 'text-ok'
  const checksAccent = checks.failed ? 'bg-danger' : checks.running ? 'bg-accent' : checks.total ? 'bg-ok' : 'bg-border-strong'

  return (
    <div className="grid grid-cols-2 gap-2 px-4 pb-2.5 lg:grid-cols-4">
      <Tile label="Checks" accent={checksAccent}>
        <Row>
          <Figure>{checks.total ? `${checks.passed}/${checks.total}` : '—'}</Figure>
          <Sub tone={checks.total ? checksTone : undefined}>{checks.total ? `${checks.running} running · ${checks.failed} failing` : 'no checks'}</Sub>
        </Row>
        <div className="mt-1.5 flex h-1">{checks.total > 0 && <ChecksBar checks={pr.checks} />}</div>
      </Tile>
      <Tile label="Reviews" accent={reviews.approvals ? 'bg-ok' : 'bg-warn'}>
        <Row>
          <Figure>{reviews.approvals}</Figure>
          <Sub tone={reviews.approvals ? 'text-ok' : 'text-warn'}>{reviews.approvals ? `${reviews.approvals === 1 ? 'reviewer' : 'reviewers'}` : 'no approvals'}</Sub>
        </Row>
      </Tile>
      <Tile label="Changes" accent="bg-ok">
        <Row>
          <Figure><span className="text-ok">+{changes.additions.toLocaleString()}</span> <span className="text-danger">−{changes.deletions.toLocaleString()}</span></Figure>
          <Sub>{changes.files} {changes.files === 1 ? 'file' : 'files'}</Sub>
        </Row>
        <div className="mt-1.5 flex h-1 gap-0.5" aria-hidden="true">
          {changes.additions + changes.deletions > 0 && (
            <>
              <span className="rounded-sm bg-ok/70" style={{ flexGrow: changes.additionShare }} />
              <span className="rounded-sm bg-danger" style={{ flexGrow: 1 - changes.additionShare }} />
            </>
          )}
        </div>
      </Tile>
      <Tile label="Commits" accent="bg-border-strong">
        <Row>
          <Figure>{pr.commits.length}</Figure>
          <Sub>{latest ? `latest ${formatPrTime(latest)}` : 'none yet'}</Sub>
        </Row>
      </Tile>
    </div>
  )
}
