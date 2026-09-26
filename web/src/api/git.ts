import { useBonsaiStore } from '../stores/bonsai'
import type { CreateWorktreeInput, Project, PullRequest, Worktree } from '../types'

export interface Branch { name: string; remote: boolean; local_head_sha?: string; local_remote_ref_sha?: string; remote_head_sha?: string }
interface Repository { id: string; workspace_id: string; full_name: string; default_branch: string }
interface RemotePR { number: number; title: string; body: string; state: string; draft: boolean; head: string; base: string; head_sha: string; author: string; created_at: string; updated_at: string; mergeable?: string; comments?: { author: string; body: string; created_at: string }[]; reviews?: { author: string; body: string; submitted_at: string }[]; commits?: { sha: string; message: string; author: string; created_at: string }[]; files?: { path: string; additions: number; deletions: number; patch: string }[] }
interface LocalWorktree { id: string; repository_id: string; branch: string; main: boolean; local_head_sha: string; status?: { ahead: number; behind: number; staged: number; modified: number; untracked: number; files: unknown[]; git_state: string; dirty: boolean; last_commit?: { when: string; subject: string; sha: string } } }
export interface Snapshot { repository: Repository; online: boolean; sequence: number; local?: { branches: Branch[]; worktrees: LocalWorktree[] }; remote?: { repository: Repository; branches: { name: string; remote_head_sha: string }[]; pull_requests: RemotePR[] }; metadata: Record<string, { tag: string; merge_target_branch: string; stack_preference: 'auto' | 'never' }> }
export class APIError extends Error { constructor(public code: string, message: string) { super(message) } }
export async function request<T>(path: string, body?: unknown, method = body === undefined ? 'GET' : 'POST'): Promise<T> {
  const response = await fetch(path, { method, credentials: 'same-origin', headers: { 'Content-Type': 'application/json', ...(method !== 'GET' ? { 'Idempotency-Key': crypto.randomUUID() } : {}) }, body: body === undefined ? undefined : JSON.stringify(body) })
  if (!response.ok) { const data = await response.json().catch(() => ({})); throw new APIError(data.error?.code ?? 'request_failed', data.error?.message ?? `Request failed (${response.status})`) }
  if (response.status === 204) return undefined as T
  return response.json() as Promise<T>
}
export function report(error: unknown) { useBonsaiStore.setState({ notice: error instanceof Error ? error.message : String(error) }) }
function project(r: Repository): Project { return { id: r.id, workspaceId: r.workspace_id, name: r.full_name.split('/').at(-1) ?? r.id, repository: r.full_name, description: '', health: 'idle', defaultBranch: r.default_branch, worktreeIds: [], openPrCount: 0 } }
function pullRequest(repo: string, p: RemotePR): PullRequest {
  return { id: `${repo}:${p.number}`, number: p.number, title: p.title, description: p.body ?? '', branch: p.head, base: p.base, status: p.state === 'merged' ? 'Merged' : p.state === 'closed' ? 'Closed' : p.draft ? 'Draft' : 'Open', author: p.author, createdAt: p.created_at, updatedAt: p.updated_at, mergeable: p.mergeable === 'mergeable', checks: [], commits: (p.commits ?? []).map(c => ({ sha: c.sha, message: c.message, author: c.author, time: c.created_at })), conversation: [...(p.comments ?? []).map(c => ({ author: c.author, body: c.body, time: c.created_at, kind: 'comment' as const })), ...(p.reviews ?? []).map(c => ({ author: c.author, body: c.body, time: c.submitted_at, kind: 'review' as const }))], files: (p.files ?? []).map(f => ({ path: f.path, additions: f.additions, deletions: f.deletions, diff: (f.patch ?? '').split('\n') })) }
}
const heads = new Map<string, string>()
export function applySnapshot(snapshot: Snapshot) {
  const state = useBonsaiStore.getState(), id = snapshot.repository.id
  const remote = snapshot.remote?.pull_requests ?? []
  const prs = remote.map(p => { const mapped = pullRequest(id, p); heads.set(mapped.id, p.head_sha); const previous = state.pullRequests.find(v => v.id === mapped.id); return previous?.updatedAt === mapped.updatedAt ? { ...previous, status: mapped.status } : mapped })
  const trees: Worktree[] = (snapshot.local?.worktrees ?? []).map(w => {
    const meta = snapshot.metadata[w.id], st = w.status, pr = prs.find(p => p.branch === w.branch && (p.status === 'Open' || p.status === 'Draft'))
    return { id: w.id, projectId: id, branch: w.branch, kind: w.main ? 'Production' : 'Feature', tag: meta?.tag || (w.main ? 'production' : 'untagged'), sourceType: 'existing', mergeTargetBranch: pr?.base ?? (meta?.merge_target_branch || snapshot.repository.default_branch), stackPreference: meta?.stack_preference || 'auto', status: !snapshot.online ? 'idle' : st?.dirty ? 'warning' : 'healthy', agentIds: state.agents.filter(a => a.worktreeId === w.id).map(a => a.id), prNumber: pr?.number, prStatus: pr?.status, ciStatus: 'waiting', ciFailed: 0, ahead: st?.ahead ?? 0, behind: st?.behind ?? 0, dirtyFiles: st?.files?.length ?? 0, lastActivity: st?.last_commit?.when ?? '', gitState: st?.git_state }
  })
  const p = project(snapshot.repository), main = snapshot.local?.worktrees.find(w => w.branch === p.defaultBranch)?.status?.last_commit
  p.health = snapshot.online ? 'healthy' : 'idle'; p.worktreeIds = trees.map(w => w.id); p.openPrCount = prs.filter(p => p.status === 'Open' || p.status === 'Draft').length
  if (main) p.defaultBranchInfo = { commitSha: main.sha, commitMessage: main.subject, lastActivity: main.when, ciStatus: 'waiting' }
  const branches = [...(snapshot.local?.branches ?? [])]
  for (const b of snapshot.remote?.branches ?? []) { const local = branches.find(v => v.name === `origin/${b.name}`); if (local) local.remote_head_sha = b.remote_head_sha; else branches.push({ name: `origin/${b.name}`, remote: true, remote_head_sha: b.remote_head_sha }) }
  useBonsaiStore.setState({ projects: state.projects.map(v => v.id === id ? p : v), worktrees: [...state.worktrees.filter(w => w.projectId !== id), ...trees], pullRequests: [...state.pullRequests.filter(v => !v.id.startsWith(`${id}:`)), ...prs], gitBranches: { ...state.gitBranches, [id]: branches }, gitOnline: { ...state.gitOnline, [id]: snapshot.online }, gitRevision: state.gitRevision + 1, dockWorktreeId: state.dockWorktreeId || trees[0]?.id || '' })
}
const refreshing = new Map<string, Promise<Snapshot>>()
const refreshAgain = new Set<string>()
export function refreshProject(id: string, fresh = false): Promise<Snapshot> {
  const existing = refreshing.get(id); if (existing) { refreshAgain.add(id); return existing }
  const pending = request<Snapshot>(`/api/projects/${encodeURIComponent(id)}/git${fresh ? '?fresh=true' : ''}`).then(snapshot => { applySnapshot(snapshot); return snapshot }).finally(() => { refreshing.delete(id); if (refreshAgain.delete(id)) void refreshProject(id).catch(report) })
  refreshing.set(id, pending); return pending
}
export async function createWorktree(input?: CreateWorktreeInput | string) {
  const state = useBonsaiStore.getState(), p = state.projects.find(p => p.id === state.activeProjectId)
  if (!p) return
  if (!input || typeof input === 'string') { useBonsaiStore.setState({ worktreeDialogOpen: true }); return }
  try {
    const response = await request<{ result: LocalWorktree; snapshot: Snapshot }>(`/api/projects/${encodeURIComponent(p.id)}/worktrees`, { mode: input.sourceType === 'origin' ? 'remote' : input.sourceType, branch: input.sourceType === 'new' ? input.branchName : input.sourceRef.replace(/^origin\//, ''), base: input.sourceRef })
    applySnapshot(response.snapshot)
    await updateMetadata(response.result.id, { merge_target_branch: input.mergeTargetBranch, tag: state.worktreeTags.find(t => t.id === input.tagId)?.name ?? '' })
    useBonsaiStore.setState({ selection: { type: 'worktree', id: response.result.id }, dockWorktreeId: response.result.id, worktreeDialogOpen: false, notice: 'Worktree created' })
  } catch (error) { report(error) }
}
export async function updateMetadata(id: string, patch: Record<string, string>) {
  const wt = useBonsaiStore.getState().worktrees.find(w => w.id === id); if (!wt) return
  await request(`/api/worktrees/${encodeURIComponent(id)}/metadata`, patch, 'PATCH'); await refreshProject(wt.projectId)
}
export async function loadPullRequest(id: string) {
  const index = id.lastIndexOf(':'), repo = id.slice(0, index), number = id.slice(index + 1); if (index < 0) return
  try {
    const p = await request<RemotePR>(`/api/projects/${encodeURIComponent(repo)}/pull-requests/${number}`), mapped = pullRequest(repo, p)
    heads.set(id, p.head_sha)
    const checks = await request<{ name: string; status: string; conclusion: string }[]>(`/api/projects/${encodeURIComponent(repo)}/checks/${p.head_sha}`)
    mapped.checks = checks.map(c => ({ name: c.name, status: c.status !== 'completed' ? 'running' : ['success', 'neutral', 'skipped'].includes(c.conclusion) ? 'success' : 'failed' }))
    useBonsaiStore.setState(state => ({ pullRequests: state.pullRequests.map(v => v.id === id ? mapped : v) }))
  } catch (error) { report(error) }
}
export async function changePullRequest(id: string, status: PullRequest['status']) {
  const current = useBonsaiStore.getState().pullRequests.find(p => p.id === id); if (!current) return
  const repo = id.slice(0, id.lastIndexOf(':')), action = status === 'Merged' ? 'merge' : status === 'Closed' ? 'close' : current.status === 'Draft' ? 'ready' : 'reopen'
  try { await request(`/api/projects/${encodeURIComponent(repo)}/pull-requests/${current.number}/${action}`, action === 'merge' ? { method: 'merge', head_sha: heads.get(id) } : {}); await refreshProject(repo); await loadPullRequest(id) } catch (error) { report(error) }
}
export async function reviewPullRequest(id: string, body: string, kind: 'comment' | 'approve' | 'request-changes') {
  const repo = id.slice(0, id.lastIndexOf(':')), p = useBonsaiStore.getState().pullRequests.find(p => p.id === id); if (!p) return
  try { await request(`/api/projects/${encodeURIComponent(repo)}/pull-requests/${p.number}/reviews`, { body, event: kind === 'approve' ? 'APPROVE' : kind === 'request-changes' ? 'REQUEST_CHANGES' : 'COMMENT', commit_id: heads.get(id) }); await loadPullRequest(id) } catch (error) { report(error) }
}
export function startGitBackend() {
  let closed = false, events: EventSource | undefined
  const connect = async () => {
    try {
      const repos = await request<Repository[]>('/api/projects'); if (closed) return
      const state = useBonsaiStore.getState(), active = repos.find(r => r.id === state.activeProjectId) ?? repos[0]
      useBonsaiStore.setState({ projects: repos.map(project), activeProjectId: active?.id ?? '', activeWorkspaceId: active?.workspace_id ?? '', gitError: '', worktrees: [], pullRequests: [] })
      const snapshots = await Promise.all(repos.map(r => refreshProject(r.id)))
      if (closed) return
      const cursor = snapshots.length ? Math.min(...snapshots.map(s => s.sequence)) : 0
      events?.close(); events = new EventSource(`/api/events?after=${cursor}`)
      events.addEventListener('git', event => { const data = JSON.parse((event as MessageEvent).data) as { project_id: string }; void refreshProject(data.project_id).catch(report) })
      events.addEventListener('reset', () => { events?.close(); void connect() })
      events.onopen = () => { for (const r of repos) void refreshProject(r.id, useBonsaiStore.getState().gitOnline[r.id]).catch(report) }
    } catch (error) { if (!closed) useBonsaiStore.setState({ gitError: error instanceof APIError && error.code === 'unauthorized' ? 'Sign in with GitHub to connect your repositories.' : String(error) }) }
  }
  void connect()
  return () => { closed = true; events?.close() }
}

export async function localCommand(action: string, args: Record<string, unknown> = {}) {
  const id = useBonsaiStore.getState().dockWorktreeId
  if (!id) { report(new Error('Select a connected worktree first')); return }
  try {
    const response = await request<{ result?: { state?: string; conflicted_paths?: string[]; error?: string }; snapshot: Snapshot }>(`/api/worktrees/${encodeURIComponent(id)}/${action}`, args)
    applySnapshot(response.snapshot)
    const op = response.result
    useBonsaiStore.setState({ notice: op?.state === 'conflict' ? `Conflicts: ${(op.conflicted_paths ?? []).join(', ')}. Resolve locally, then continue or abort.` : op?.error || `${action} ${op?.state ?? 'completed'}` })
  } catch (error) { report(error) }
}

export function openGitHub(path: string, projectId = useBonsaiStore.getState().activeProjectId) {
  const p = useBonsaiStore.getState().projects.find(p => p.id === projectId)
  if (p) window.open(`https://github.com/${p.repository}/${path}`, '_blank', 'noopener,noreferrer')
}
