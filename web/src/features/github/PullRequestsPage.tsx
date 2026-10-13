import { lazy, useEffect, useMemo, useState } from 'react'
import { loadPullRequest } from '../../api/git'
import { buildPrList } from '../../lib/github/prList'
import { useBonsaiStore } from '../../stores/bonsai'
import { FeatureBoundary } from '../../app/chunkRecovery'
import { PullRequestListPane } from './PullRequestListPane'
import { usePullRequestCatalog } from './usePullRequestCatalog'

const PullRequestDetail = lazy(() => import('./PullRequestDetail').then(module => ({ default: module.PullRequestDetail })))

export interface PullRequestsPageProps {
  /** Selected PR id from the URL; falls back to local state, the inspector, then the first PR. */
  selectedId?: string
  onSelect?: (id: string) => void
  /** Called after the worktree is selected, to bring the canvas into view. */
  onShowOnCanvas?: () => void
}

export function PullRequestsPage({ selectedId: routeSelectedId, onSelect, onShowOnCanvas }: PullRequestsPageProps = {}) {
  const projects = useBonsaiStore((state) => state.projects)
  const activeProjectId = useBonsaiStore((state) => state.activeProjectId)
  const worktrees = useBonsaiStore((state) => state.worktrees)
  const setSelection = useBonsaiStore((state) => state.setSelection)
  const { rows: pullRequests, tab, setTab, openCount, closedCount, state, message, stale, retry } = usePullRequestCatalog(activeProjectId)
  const project = projects.find((item) => item.id === activeProjectId)
  const inspectedId = useBonsaiStore((state) => state.inspectedPullRequestId)
  const [localId, setLocalId] = useState<string>()
  const repository = project?.repository
  const preferredId = routeSelectedId ?? localId ?? inspectedId ?? undefined
  const orderedIds = useMemo(() => buildPrList(pullRequests, { repository }).ordered.map((pr) => pr.id), [pullRequests, repository])
  const selected = pullRequests.find((pr) => pr.id === preferredId) ?? pullRequests.find((pr) => pr.id === orderedIds[0])
  const [visibleIds, setVisibleIds] = useState<readonly string[]>()
  const select = (id: string) => { setLocalId(id); onSelect?.(id) }
  const showOnCanvas = (worktreeId: string) => {
    setSelection({ type: 'worktree', id: worktreeId })
    onShowOnCanvas?.()
    useBonsaiStore.getState().requestCanvasFocus(activeProjectId, worktreeId)
  }

  useEffect(() => { if (selected?.id) void loadPullRequest(selected.id) }, [selected?.id, selected?.updatedAt])

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
        stale={stale}
        onRetry={retry}
        repository={repository}
        selectedId={selected?.id}
        onSelect={select}
        onVisibleChange={setVisibleIds}
      />

      {selected ? (
        <div className="flex min-h-0 min-w-0 flex-1 flex-col">
          <FeatureBoundary key={selected.id} loading="Loading pull request…">
            <PullRequestDetail
              pr={selected}
              pullRequests={pullRequests}
              orderedIds={visibleIds ?? orderedIds}
              repository={repository}
              projectId={activeProjectId}
              worktrees={worktrees}
              onSelect={select}
              onShowOnCanvas={showOnCanvas}
            />
          </FeatureBoundary>
        </div>
      ) : (
        <div className="island grid flex-1 place-items-center text-[11px] text-muted">{message}</div>
      )}
    </div>
  )
}
