import { useEffect, useMemo, useState } from 'react'
import { loadPullRequest } from '../../api/git'
import { buildPrList } from '../../lib/github/prList'
import { useBonsaiStore } from '../../stores/bonsai'
import { PullRequestDetail } from './PullRequestDetail'
import { PullRequestListPane } from './PullRequestListPane'
import { usePullRequestCatalog } from './usePullRequestCatalog'

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
  const { rows: pullRequests, tab, setTab, openCount, closedCount, state, message, retry } = usePullRequestCatalog(activeProjectId)
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
        onRetry={retry}
        repository={repository}
        selectedId={selected?.id}
        onSelect={select}
        onVisibleChange={setVisibleIds}
      />

      {selected ? (
        <PullRequestDetail
          key={selected.id}
          pr={selected}
          pullRequests={pullRequests}
          orderedIds={visibleIds ?? orderedIds}
          repository={repository}
          projectId={activeProjectId}
          worktrees={worktrees}
          onSelect={select}
          onShowOnCanvas={showOnCanvas}
        />
      ) : (
        <div className="island grid flex-1 place-items-center text-[11px] text-muted">{message}</div>
      )}
    </div>
  )
}
