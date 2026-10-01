import { useEffect, useState } from 'react'
import { create } from 'zustand'
import { fetchClosedPullRequests } from '../../api/git'
import { useBonsaiStore } from '../../stores/bonsai'
import type { PullRequest } from '../../types'

type ClosedCatalog = { rows: PullRequest[]; loading: boolean; error?: string; loadedAt?: number }
const useClosedCatalog = create<Record<string, ClosedCatalog>>(() => ({}))

async function loadClosed(key: string, projectId: string) {
  const previous = useClosedCatalog.getState()[key]
  if (previous?.loading || (previous?.loadedAt && Date.now() - previous.loadedAt < 60_000)) return
  useClosedCatalog.setState({ [key]: { rows: previous?.rows ?? [], loading: true } })
  try {
    const rows = await fetchClosedPullRequests(projectId)
    useClosedCatalog.setState({ [key]: { rows, loading: false, loadedAt: Date.now() } })
  } catch (error) {
    useClosedCatalog.setState({ [key]: { rows: previous?.rows ?? [], loading: false, error: error instanceof Error ? error.message : String(error) } })
  }
}

export function usePullRequestCatalog(projectId: string) {
  const [tab, setTab] = useState<'open' | 'closed'>('open')
  const repository = useBonsaiStore(state => state.projects.find(project => project.id === projectId)?.repository)
  const key = `${projectId}:${repository}`
  const closed = useClosedCatalog(state => state[key])
  const all = useBonsaiStore(state => state.pullRequests)
  const freshness = useBonsaiStore(state => state.syncFreshness[projectId]?.provider)
  useEffect(() => { setTab('open') }, [key])
  useEffect(() => { if (tab === 'closed') void loadClosed(key, projectId) }, [tab, key, projectId])
  const rows = tab === 'open'
    ? all.filter(pr => pr.id.startsWith(`${projectId}:`) && (pr.status === 'Open' || pr.status === 'Draft'))
    : (closed?.rows ?? []).map(pr => all.find(detail => detail.id === pr.id && (detail.status === 'Closed' || detail.status === 'Merged')) ?? pr)
  const loading = tab === 'closed' ? !closed || closed.loading : !freshness || freshness.state === 'loading'
  const error = tab === 'closed' ? closed?.error : freshness?.error?.message
  return { tab, setTab, rows, message: error ?? (loading ? 'Loading pull requests…' : `No ${tab} pull requests.`), retry: () => void loadClosed(key, projectId) }
}
