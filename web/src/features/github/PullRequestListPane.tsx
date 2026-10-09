import { useEffect, useMemo, useRef, useState } from 'react'
import type { KeyboardEvent } from 'react'
import { CheckCircle2, GitPullRequest, GitPullRequestDraft, Loader2, RefreshCw, Search, XCircle } from 'lucide-react'
import type { PullRequest } from '../../types'
import { buildPrList, PR_LIST_FILTERS, type PrListFilter, type PrListRow } from '../../lib/github/prList'
import { summarizeChecks } from '../../lib/github/prChecks'
import { formatPrTime } from '../../lib/github/prTime'
import { ChecksBar } from './ChecksBar'

export type PullRequestTab = 'open' | 'closed'

export interface PullRequestListPaneProps {
  tab: PullRequestTab
  onTabChange: (tab: PullRequestTab) => void
  openCount: number
  /** Unknown until the closed list has been loaded. */
  closedCount?: number
  rows: readonly PullRequest[]
  state: 'loading' | 'error' | 'ready'
  message: string
  onRetry: () => void
  repository?: string
  selectedId?: string
  onSelect: (id: string) => void
  /** Reports the rows currently shown, in order, after search and filter. */
  onVisibleChange?: (ids: readonly string[]) => void
}

const FILTER_LABELS: Record<PrListFilter, string> = { all: 'All', stacked: 'Stacked', draft: 'Draft', failing: 'Failing' }
const SKELETON_ROWS = [0, 1, 2, 3, 4]

const isEditable = (target: EventTarget | null) =>
  target instanceof HTMLElement && (target.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName))

function CheckStatusIcon({ summary }: { summary: ReturnType<typeof summarizeChecks> }) {
  if (summary.failed) return <XCircle size={11} className="text-danger" aria-label="Checks failing" />
  if (summary.running) return <Loader2 size={11} className="animate-spin text-accent" aria-label="Checks running" />
  return <CheckCircle2 size={11} className="text-ok" aria-label="Checks passed" />
}

function Row({ row, selected, tabbable, register, onSelect }: {
  row: PrListRow
  selected: boolean
  tabbable: boolean
  register: (id: string, element: HTMLElement | null) => void
  onSelect: (id: string) => void
}) {
  const { pr } = row
  const summary = summarizeChecks(pr.checks)
  const ratio = summary.failed ? 'text-danger' : summary.running ? 'text-accent' : 'text-ok'
  return (
    <div
      ref={(element) => register(pr.id, element)}
      role="option"
      aria-selected={selected}
      tabIndex={tabbable ? 0 : -1}
      onClick={() => onSelect(pr.id)}
      onKeyDown={(event) => {
        if (event.target !== event.currentTarget || (event.key !== 'Enter' && event.key !== ' ')) return
        event.preventDefault()
        onSelect(pr.id)
      }}
      className={
        'bonsai-focus relative flex cursor-pointer gap-2 rounded-lg py-2 pr-3 transition-colors ' +
        (row.stacked ? 'pl-2 ' : 'pl-3 ') +
        (selected ? 'bg-accent/[.09]' : 'hover:bg-panel-2')
      }
    >
      {selected && <span className="absolute bottom-2 left-0 top-2 w-0.5 rounded-sm bg-accent" />}
      {row.stacked ? (
        <span className="relative block w-4 flex-none self-stretch" aria-hidden="true">
          <span className={'absolute left-[7px] w-0.5 bg-border-strong ' + (row.first ? 'top-[19px] ' : '-top-[9px] ') + (row.last ? 'bottom-[calc(100%-19px)]' : '-bottom-[9px]')} />
          <span className={'absolute left-[3px] top-[9px] box-border h-2.5 w-2.5 rounded-full border-2 ' + (selected ? 'border-accent bg-accent' : 'border-faint bg-panel')} />
        </span>
      ) : (
        <span className="mt-0.5 flex flex-none text-muted" aria-hidden="true">
          {pr.status === 'Draft' ? <GitPullRequestDraft size={14} /> : <GitPullRequest size={14} />}
        </span>
      )}
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-baseline gap-1.5">
          <span className={'flex-none font-mono text-[10.5px] ' + (selected ? 'text-accent' : 'text-muted')}>#{pr.number}</span>
          <span className={'min-w-0 truncate text-[12.5px] leading-[18px] text-text ' + (selected ? 'font-semibold' : 'font-medium')} title={pr.title}>{pr.title}</span>
        </div>
        <div className="mt-px truncate font-mono text-[10px] leading-[15px] text-muted" title={`${pr.branch} → ${pr.base}`}>
          <span className="text-text">{pr.branch}</span> → {pr.base}
          {pr.status === 'Draft' && <> · <span className="text-warn">draft</span></>}
        </div>
        <div className="mt-[5px] flex items-center gap-2">
          {summary.total ? (
            <>
              <span className="flex flex-none"><CheckStatusIcon summary={summary} /></span>
              <ChecksBar checks={pr.checks} />
              <span className={'w-[34px] flex-none text-right font-mono text-[10px] ' + ratio}>{summary.passed}/{summary.total}</span>
            </>
          ) : (
            <span className="flex-1 font-mono text-[10px] text-muted-2">no checks</span>
          )}
          <span className="w-[42px] flex-none truncate text-right font-mono text-[10px] text-muted-2" title={pr.updatedAt}>{formatPrTime(pr.updatedAt)}</span>
        </div>
      </div>
    </div>
  )
}

function Skeleton() {
  return (
    <div className="flex flex-col gap-1 px-1.5 pt-1" role="status" aria-label="Loading pull requests">
      {SKELETON_ROWS.map((row) => (
        <div key={row} className="flex animate-pulse flex-col gap-1.5 rounded-lg px-3 py-2">
          <span className="h-3 w-4/5 rounded bg-panel-3" />
          <span className="h-2.5 w-3/5 rounded bg-panel-2" />
          <span className="h-[3px] w-full rounded bg-panel-2" />
        </div>
      ))}
    </div>
  )
}

export function PullRequestListPane(props: PullRequestListPaneProps) {
  const { tab, onTabChange, openCount, closedCount, rows, state, message, onRetry, repository, selectedId, onSelect, onVisibleChange } = props
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState<PrListFilter>('all')
  const searchRef = useRef<HTMLInputElement>(null)
  const rowRefs = useRef(new Map<string, HTMLElement>())
  const activeFilter = tab === 'open' ? filter : 'all'
  const list = useMemo(() => buildPrList(rows, { repository, query, filter: activeFilter }), [rows, repository, query, activeFilter])
  const filtered = query.trim() !== '' || activeFilter !== 'all'
  const total = list.ordered.length
  const visibleIds = useMemo(() => list.visible.map((pr) => pr.id), [list.visible])
  // Keep one row in the tab order even when search hides the selected one.
  const tabbableId = selectedId && visibleIds.includes(selectedId) ? selectedId : visibleIds[0]

  useEffect(() => { onVisibleChange?.(visibleIds) }, [visibleIds, onVisibleChange])

  useEffect(() => {
    const onKey = (event: globalThis.KeyboardEvent) => {
      if (event.key !== '/' || event.ctrlKey || event.metaKey || event.altKey || isEditable(event.target)) return
      event.preventDefault()
      searchRef.current?.focus()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  const register = (id: string, element: HTMLElement | null) => {
    if (element) rowRefs.current.set(id, element)
    else rowRefs.current.delete(id)
  }

  const onKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    const fromSearch = event.target === searchRef.current
    if (event.key === 'Escape' && fromSearch) {
      if (query) setQuery('')
      else searchRef.current?.blur()
      return
    }
    const ids = list.visible.map((pr) => pr.id)
    if (!ids.length || event.altKey || event.ctrlKey || event.metaKey) return
    const current = selectedId ? ids.indexOf(selectedId) : -1
    let next: number
    if (event.key === 'ArrowDown') next = Math.min(current + 1, ids.length - 1)
    else if (event.key === 'ArrowUp') next = Math.max(current - 1, 0)
    else if (event.key === 'Home' && !fromSearch) next = 0
    else if (event.key === 'End' && !fromSearch) next = ids.length - 1
    else return
    event.preventDefault()
    onSelect(ids[next])
    if (!fromSearch) rowRefs.current.get(ids[next])?.focus()
    else rowRefs.current.get(ids[next])?.scrollIntoView?.({ block: 'nearest' })
  }

  const counts: Record<PullRequestTab, number | undefined> = { open: openCount, closed: closedCount }
  const showSkeleton = state === 'loading' && !rows.length
  const showError = state === 'error'

  return (
    <section aria-label="Pull requests" onKeyDown={onKeyDown} className="island flex w-[320px] shrink-0 flex-col overflow-hidden">
      <div className="island-title flex-none justify-between pr-2">
        <span className="inline-flex items-center gap-[7px] font-medium">
          <GitPullRequest size={12} className="text-muted-2" aria-hidden="true" />
          Pull requests
          <span className="island-count">{openCount}</span>
        </span>
        <button type="button" onClick={onRetry} aria-label="Refresh pull requests" title="Refresh" className="bonsai-focus btn-ghost !h-[22px] !w-[22px] justify-center !p-0 text-muted-2">
          <RefreshCw size={12} />
        </button>
      </div>

      <div className="flex flex-none flex-col gap-2 px-3 pb-2 pt-2.5">
        <div className="flex gap-0.5 rounded-lg border border-border bg-well p-0.5" aria-label="Pull request state">
          {(['open', 'closed'] as const).map((value) => (
            <button
              key={value}
              type="button"
              aria-pressed={tab === value}
              onClick={() => onTabChange(value)}
              className={
                'bonsai-focus inline-flex h-6 flex-1 items-center justify-center gap-1.5 rounded-md text-[12px] ' +
                (tab === value ? 'bg-panel-4 font-semibold text-text' : 'text-muted-2 hover:text-text')
              }
            >
              {value === 'open' ? 'Open' : 'Closed'}
              {counts[value] !== undefined && (
                <span className={'rounded px-[5px] font-mono text-[9.5px] leading-[15px] ' + (tab === value && value === 'open' ? 'bg-accent/[.16] text-accent' : '')}>{counts[value]}</span>
              )}
            </button>
          ))}
        </div>

        <label className="flex h-[30px] items-center gap-2 rounded-lg border border-border bg-well pl-2.5 pr-2 text-muted-2 focus-within:border-accent/55">
          <Search size={13} aria-hidden="true" />
          <input
            ref={searchRef}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Title, branch or author"
            aria-label="Search pull requests"
            className="min-w-0 flex-1 bg-transparent text-[12px] text-text outline-none placeholder:text-muted-2"
          />
          <kbd className="rounded bg-panel-3 px-[5px] font-mono text-[10px] leading-4 text-muted">/</kbd>
        </label>

        {tab === 'open' && (
          <div className="flex gap-[5px]" role="group" aria-label="Filter pull requests">
            {PR_LIST_FILTERS.map((name) => (
              <button
                key={name}
                type="button"
                aria-pressed={filter === name}
                onClick={() => setFilter(name)}
                className={
                  'bonsai-focus inline-flex h-[22px] items-center gap-[5px] whitespace-nowrap rounded-full border px-2 font-mono text-[10.5px] ' +
                  (filter === name
                    ? 'border-accent/40 bg-accent/[.12] text-accent'
                    : 'border-border bg-bg hover:text-text ' + (name === 'failing' ? 'text-danger' : 'text-muted'))
                }
              >
                {FILTER_LABELS[name]}
                <span className="opacity-70">{list.counts[name]}</span>
              </button>
            ))}
          </div>
        )}
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto border-t border-border-subtle px-1.5 pb-1.5">
        {showError && (
          <div role="alert" className="m-1.5 flex items-start gap-2 rounded-lg border border-danger/35 bg-danger-solid/[.12] p-2.5 text-[11px] text-danger">
            <span className="min-w-0 flex-1 break-words">{message}</span>
            <button type="button" onClick={onRetry} className="bonsai-focus btn-danger-tint h-6 flex-none rounded-md px-2 text-[11px]">Retry</button>
          </div>
        )}
        {showSkeleton && <Skeleton />}
        {!showSkeleton && !list.visible.length && !(showError && !total) && (
          <div className="flex flex-col items-center gap-2 px-4 py-8 text-center text-[11px] text-muted">
            {filtered && total ? (
              <>
                <span>No pull requests match.</span>
                <button type="button" onClick={() => { setQuery(''); setFilter('all') }} className="bonsai-focus btn-accent-tint h-6 rounded-md px-2">Clear filters</button>
              </>
            ) : (
              <span>No {tab} pull requests</span>
            )}
          </div>
        )}
        {list.groups.length > 0 && (
          <div role="listbox" aria-label="Pull requests">
            {list.groups.map((group) => (
              <div key={group.key} role="group" aria-label={group.title}>
                <div className="flex h-6 items-center justify-between px-3 font-mono text-[9.5px] uppercase tracking-[.08em] text-muted-2">
                  <span className="inline-flex items-center gap-1.5">
                    <GitPullRequest size={11} className="text-faint" aria-hidden="true" />
                    {group.title}
                  </span>
                  <span className="normal-case tracking-[.02em] text-faint">{group.detail}</span>
                </div>
                <div className="flex flex-col gap-px">
                  {group.rows.map((row) => (
                    <Row key={row.pr.id} row={row} selected={row.pr.id === selectedId} tabbable={row.pr.id === tabbableId} register={register} onSelect={onSelect} />
                  ))}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </section>
  )
}
