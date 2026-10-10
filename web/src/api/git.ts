import { project, pullRequest, mapCheck, reconcileSnapshotEntities, snapshotRuntimeAuthority } from './snapshotReconciliation'
import { remapProcessReferences } from '../stores/processProjection'
import { activeRuntimePatch, reconcileRuntimePreferences, switchRuntimeScope } from '../stores/runtimePreferences'
import { changedPatch, replaceScope } from '../stores/reconciliation'
export { pullRequest } from './snapshotReconciliation'
import { useBonsaiStore } from '../stores/bonsai'
import type {
  CiStatus,
  CreateWorktreeInput,
  ProcessLifecycleStatus,
  PullRequest,
  SyncFreshness,
} from '../types'
import {
  invalidateLocalSession,
  localFetch,
  markLocalConnectionLost,
  openLocalEvents,
  sendEventFocus,
  type LocalEvent,
} from './local'

export interface Branch {
  name: string
  remote: boolean
  ref?: string
  remote_name?: string
  upstream_ref?: string
  last_commit_at?: string
  upstream?: string
  local_head_sha?: string
  local_remote_ref_sha?: string
  remote_head_sha?: string
}
export interface WorktreeGroup { id: string; kind: string; worktree_ids: string[] }
export interface SyncOutcome { state: 'ready' | 'skipped' | 'error' | 'running'; reason?: string; error?: string; completed_at?: string }
export interface RepositorySync { fetch: SyncOutcome; pull: SyncOutcome }
export interface BranchCandidate {
  id: string; ref: string; name: string; source: 'local' | 'remote' | 'provider'; remote?: string
  local_branch?: string; upstream_ref?: string; head_sha?: string; last_commit_at?: string; pull_requests: RemotePR[]
  worktree_ids: string[]; creation_mode?: 'existing' | 'remote'; source_ref?: string; unavailable_reason?: string
}
export interface Repository {
  launch?: boolean
  available?: boolean
  name?: string
  path?: string
  root_id?: string
  id: string
  workspace_id: string
  full_name: string
  default_branch: string
}
interface GitHubRepository {
  id: number
  full_name: string
  default_branch: string
  private?: boolean
}
export interface RemoteCheck {
  id?: number
  name: string
  status: string
  conclusion: string
  url?: string
  started_at?: string
  completed_at?: string
}
export interface RemotePR {
  number: number
  title: string
  body: string
  state: string
  draft: boolean
  head: string
  head_repository?: string
  base: string
  head_sha: string
  author: string
  created_at: string
  updated_at: string
  mergeable?: string
  additions?: number
  deletions?: number
  changed_files?: number
  requested_reviewers?: string[]
  review_summary?: { approvals: number; changes_requested: number }
  behind_by?: number
  comments?: { author: string; body: string; created_at: string }[]
  reviews?: { author: string; body: string; submitted_at: string }[]
  commits?: { sha: string; message: string; author: string; created_at: string }[]
  files?: { path: string; additions: number; deletions: number; patch: string }[]
}
export interface WireFreshness {
  state: SyncFreshness['state']
  updated_at?: string
  // reset_at accompanies code rate_limited: when GitHub's rate-limit window resets.
  error?: { code: string; message: string; reset_at?: string }
}
interface LocalStatus {
  branch?: string
  head_state?: 'branch' | 'detached' | 'unborn'
  local_head_sha?: string
  upstream?: string
  local_remote_ref_sha?: string
  divergence_available?: boolean
  ahead: number
  behind: number
  staged: number
  modified: number
  untracked: number
  files: unknown[]
  git_state: string
  dirty: boolean
  last_commit?: { when: string; subject: string; sha: string }
}
interface LocalWorktree {
  id: string
  repository_id: string
  path?: string
  branch: string
  main: boolean
  missing?: boolean
  local_head_sha: string
  status?: LocalStatus
  status_error?: { code: string; message: string }
  connection?: { state: 'linked' | 'unlinked' | 'unknown'; reason?: string; status_unknown?: boolean }
}
export interface ProcessSummary {
  command_key?: string
  execution_order?: number
  id: string
  daemon_id: number
  revision?: number
  policy?: { mode: 'no' | 'on-failure' | 'always'; max_restarts: number }
  restarts?: number
  retry_count?: number
  attempt?: number
  retry_at?: string
  project_id: string
  worktree_id?: string
  label: string
  command: string
  status: ProcessLifecycleStatus
  pid?: number
  expected_port?: number
  url?: string
  started_at?: string
  exit_code?: number
  exit_error?: string
  serve_group?: string
  serve_name?: string
}
export interface WorktreeProjection {
  pull_request?: RemotePR
  pr_diagnostic?: string
  ci?: {
    status: Exclude<CiStatus, 'waiting'>
    checked_sha?: string
    checks: RemoteCheck[]
    freshness: WireFreshness
  }
}
export interface Snapshot {
  process_visibility?: { cutoffs: Record<string, number>; deleted: Record<string, boolean> }
  agents?: import('./agents').AgentSummary[]
  epoch?: string
  branch_candidates?: BranchCandidate[]
  sync?: RepositorySync
  repository: Repository
  online: boolean
  sequence: number
  local?: {
    groups?: WorktreeGroup[]
    default_branch?: string
    branches: Branch[]
    worktrees: LocalWorktree[]
    remotes?: { name: string; host?: string; full_name?: string }[]
  }
  remote?: {
    repository: GitHubRepository
    branches: { name: string; remote_head_sha: string }[]
    pull_requests: RemotePR[]
    updated_at?: string
    pr_catalog_complete?: boolean
    pr_catalog_loading?: boolean
  }
  metadata: Record<string, { merge_target_branch: string; stack_preference: 'auto' | 'never' }>
  processes?: ProcessSummary[]
  freshness?: Record<string, WireFreshness>
  worktree_state?: Record<string, WorktreeProjection>
}
export class APIError extends Error {
  constructor(public code: string, message: string) {
    super(message)
  }
}
export async function request<T>(path: string, body?: unknown, method = body === undefined ? 'GET' : 'POST', idempotencyKey: string = crypto.randomUUID()): Promise<T> {
  const response = await localFetch(path, {
    method,
    headers: {
      ...(body === undefined ? {} : { 'Content-Type': 'application/json' }),
      ...(method !== 'GET' ? { 'Idempotency-Key': idempotencyKey } : {}),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const contentType = response.headers.get('content-type') ?? ''
  if (response.status !== 204 && !contentType.includes('application/json')) {
    throw new APIError('backend_unavailable', 'Bonsai API is unavailable.')
  }
  if (!response.ok) {
    const data = await response.json().catch(() => ({}))
    throw new APIError(data.error?.code ?? 'request_failed', data.error?.message ?? `Request failed (${response.status})`)
  }
  if (response.status === 204) return undefined as T
  return response.json() as Promise<T>
}
export function report(error: unknown) {
  useBonsaiStore.setState({ notice: error instanceof Error ? error.message : String(error) })
}
const pendingCreatedWorktrees = new Map<string, string>()
const heads = new Map<string, string>()
const githubRepositoryProjects = new Map<number, Set<string>>()
interface SequenceGuard {
  epoch: string
  applied: number
  localWorktrees?: string
}
const sequenceGuards = new Map<string, SequenceGuard>()
let activeEpoch = ''
let activeGeneration = 0
type StoreState = ReturnType<typeof useBonsaiStore.getState>

export function projectForGitHubRepository(repositoryId: number) {
  return [...(githubRepositoryProjects.get(repositoryId) ?? [])]
}

function setEpoch(epoch: string) {
  if (!epoch || activeEpoch === epoch) return
  activeEpoch = epoch
  sequenceGuards.clear()
}

function guardFor(projectId: string, epoch: string) {
  const current = sequenceGuards.get(projectId)
  if (current?.epoch === epoch) return current
  const next: SequenceGuard = { epoch, applied: -1 }
  sequenceGuards.set(projectId, next)
  return next
}

function snapshotPatch(snapshot: Snapshot, state: StoreState, generation = activeGeneration): Partial<StoreState> | undefined {
  const id = snapshot.repository.id
  if (!state.projects.some(p => p.id === id)) return undefined
  if (generation !== activeGeneration) return undefined

  const epoch = snapshot.epoch || activeEpoch || 'legacy'
  if (activeEpoch && snapshot.epoch && snapshot.epoch !== activeEpoch) return undefined
  const guard = guardFor(id, epoch)
  if (snapshot.sequence <= guard.applied) return undefined

  for (const projects of githubRepositoryProjects.values()) projects.delete(id)
  if (snapshot.remote?.repository.id) {
    const repositoryId = snapshot.remote.repository.id
    const projects = githubRepositoryProjects.get(repositoryId) ?? new Set<string>()
    projects.add(id)
    githubRepositoryProjects.set(repositoryId, projects)
  }

  const entityPatch = reconcileSnapshotEntities(snapshot, state)
  const authority = snapshotRuntimeAuthority(snapshot)
  const trees = (entityPatch.worktrees ?? state.worktrees).filter(w => w.projectId === id)
  const allTrees = entityPatch.worktrees ?? state.worktrees
  const allProcesses = entityPatch.processes ?? state.processes
  for (const value of snapshot.remote?.pull_requests ?? []) heads.set(`${id}:${value.number}`, value.head_sha)
  for (const value of Object.values(snapshot.worktree_state ?? {})) {
    if (value.pull_request) heads.set(`${id}:${value.pull_request.number}`, value.pull_request.head_sha)
  }
  const removedWorktreeIds = new Set(state.worktrees.filter(w => w.projectId === id && !trees.some(tree => tree.id === w.id)).map(w => w.id))
  const mappedAgents = entityPatch.agents ?? state.agents
  const agents = removedWorktreeIds.size ? mappedAgents.filter(agent => !removedWorktreeIds.has(agent.worktreeId)) : mappedAgents
  const removedRuntimeIds = new Set([
    ...state.agents.filter(agent => !agents.some(value => value.id === agent.id)
      && (agent.projectId === id || state.worktrees.some(tree => tree.projectId === id && tree.id === agent.worktreeId))).map(agent => agent.id),
    ...state.processes.filter(process => authority.processes && process.projectId === id && !allProcesses.some(value => value.id === process.id)).map(process => process.id),
    ...Object.values(state.terminalViewPreferences[id]?.worktrees ?? {}).flatMap(view => view.open.filter(ref =>
      ref.kind === 'process' ? authority.processes && !allProcesses.some(process => process.id === ref.id)
        : authority.agents && !agents.some(agent => agent.id === ref.id)).map(ref => ref.id)),
  ])
  const pendingCreatedId = pendingCreatedWorktrees.get(id)
  const createdVisible = pendingCreatedId && trees.some(w => w.id === pendingCreatedId)
  if (createdVisible) pendingCreatedWorktrees.delete(id)
  const selection = createdVisible && state.activeProjectId === id
    ? { type: 'worktree' as const, id: pendingCreatedId }
    : state.activeProjectId === id && state.selection.type === 'agent' && (authority.agents || state.agents.some(agent => agent.id === state.selection.id && removedWorktreeIds.has(agent.worktreeId))) && !agents.some(agent => agent.id === state.selection.id)
      ? { type: 'project' as const, id: state.activeProjectId }
      : state.activeProjectId === id && state.selection.type === 'process' && authority.processes && !allProcesses.some(process => process.id === state.selection.id)
      ? { type: 'project' as const, id }
      : state.selection.type === 'worktree'
    && authority.worktrees
    && (state.activeProjectId === id || state.worktrees.some(w => w.id === state.selection.id && w.projectId === id))
    && !allTrees.some(w => w.id === state.selection.id)
    ? { type: 'project' as const, id }
    : state.selection
  let working = { ...state, ...entityPatch, agents }
  if (authority.processes) working = { ...working, ...remapProcessReferences(working, state, id) }
  if (state.activeProjectId === id && (createdVisible || (!state.dockWorktreeId && !state.terminalViewPreferences[id] && authority.worktrees))) {
    working = { ...working, ...switchRuntimeScope(working, id, createdVisible ? pendingCreatedId : undefined) }
  }
  working = { ...working, ...reconcileRuntimePreferences(working, id, authority) }

  const patch = changedPatch(state, {
    ...entityPatch,
    selection,
    agents,
    nodePlacements: removedWorktreeIds.size || removedRuntimeIds.size ? Object.fromEntries(Object.entries(state.nodePlacements).filter(([nodeId]) => !removedWorktreeIds.has(nodeId) && !removedRuntimeIds.has(nodeId))) : state.nodePlacements,
    detachedStackWorktreeIds: removedWorktreeIds.size || createdVisible ? [...new Set([...state.detachedStackWorktreeIds.filter(nodeId => !removedWorktreeIds.has(nodeId)), ...(createdVisible ? [pendingCreatedId] : [])])] : state.detachedStackWorktreeIds,
    expandedHistoryWorktreeIds: removedWorktreeIds.size ? state.expandedHistoryWorktreeIds.filter(nodeId => !removedWorktreeIds.has(nodeId)) : state.expandedHistoryWorktreeIds,
    collapsedBranchIds: removedWorktreeIds.size ? state.collapsedBranchIds.filter(nodeId => !removedWorktreeIds.has(nodeId)) : state.collapsedBranchIds,
    terminalSessions: agents === state.agents ? state.terminalSessions : state.terminalSessions.filter(session => !session.agentId || agents.some(agent => agent.id === session.agentId)),
    terminalViewPreferences: working.terminalViewPreferences,
    dockWorktreeId: working.dockWorktreeId,
    dockRuntimeId: working.dockRuntimeId,
    openRuntimeIds: working.openRuntimeIds,
    activeTerminalId: working.activeTerminalId,
  })
  if (authority.processes) {
    Object.assign(patch, remapProcessReferences({ ...state, ...patch }, state, id))
    Object.assign(patch, reconcileRuntimePreferences({ ...state, ...patch }, id, authority))
  }
  guard.applied = Math.max(guard.applied, snapshot.sequence)
  // Provider and process updates do not invalidate local files or diffs. Include
  // raw file status entries so path changes with unchanged counts still reload.
  const localWorktrees = JSON.stringify(snapshot.local?.worktrees ?? [])
  const filesChanged = guard.localWorktrees !== localWorktrees
  guard.localWorktrees = localWorktrees
  if (filesChanged) patch.gitRevision = state.gitRevision + 1
  return Object.keys(patch).length ? patch : undefined
}

export function applySnapshot(snapshot: Snapshot, generation = activeGeneration): boolean {
  const state = useBonsaiStore.getState()
  const patch = snapshotPatch(snapshot, state, generation)
  if (!patch) return false
  useBonsaiStore.setState(patch)
  return true
}

function applySnapshots(snapshots: Snapshot[], generation = activeGeneration): boolean {
  let working = useBonsaiStore.getState()
  let changed = false
  for (const snapshot of snapshots) {
    const patch = snapshotPatch(snapshot, working, generation)
    if (!patch) continue
    working = { ...working, ...patch }
    changed = true
  }
  if (changed) useBonsaiStore.setState(working)
  return changed
}

async function requestRefresh(id: string, scope: 'local' | 'provider' | 'all'): Promise<void> {
  await request<{ accepted: boolean }>(`/api/projects/${encodeURIComponent(id)}/refresh`, { scope })
}

export async function requestProjectRefresh(id: string, scope: 'local' | 'provider' | 'all' = 'all'): Promise<void> {
  await requestRefresh(id, scope)
}

// Explicit/manual recovery helper. Live synchronization never calls this path.
export async function refreshProject(id: string, fresh = false): Promise<Snapshot | undefined> {
  if (fresh) {
    await requestRefresh(id, 'all')
    return undefined
  }
  const snapshot = await request<Snapshot>(`/api/projects/${encodeURIComponent(id)}/git`)
  applySnapshot(snapshot)
  return snapshot
}
export function syncProject(id: string, initial = false) {
  return request<{ accepted: boolean }>(`/api/projects/${encodeURIComponent(id)}/sync`, { initial })
}
export class WorktreeMetadataError extends Error {
  constructor(public worktreeId: string, cause: unknown) {
    super(`Worktree created. Settings could not be saved: ${cause instanceof Error ? cause.message : String(cause)}`)
  }
}
export async function createWorktree(input: CreateWorktreeInput, options: { idempotencyKey?: string; worktreeId?: string } = {}): Promise<string> {
  const state = useBonsaiStore.getState()
  const projectId = input.projectId ?? state.activeProjectId
  if (!state.projects.some(value => value.id === projectId)) throw new Error('Project is unavailable')
  let worktreeId = options.worktreeId
  if (!worktreeId) {
    const response = await request<{ result: LocalWorktree }>(
      `/api/projects/${encodeURIComponent(projectId)}/worktrees`,
      { mode: input.sourceType === 'origin' ? 'remote' : input.sourceType,
        branch: input.branchName || (input.sourceType === 'origin' ? input.sourceRef.split('/').slice(1).join('/') : input.sourceRef),
        base: input.sourceRef }, 'POST', options.idempotencyKey,
    )
    worktreeId = response.result.id
  }
  pendingCreatedWorktrees.set(projectId, worktreeId)
  const canonical = useBonsaiStore.getState()
  if (canonical.worktrees.some(worktree => worktree.id === worktreeId)) {
    pendingCreatedWorktrees.delete(projectId)
    useBonsaiStore.setState({ detachedStackWorktreeIds: [...new Set([...canonical.detachedStackWorktreeIds, worktreeId])] })
    if (canonical.activeProjectId === projectId) canonical.setSelection({ type: 'worktree', id: worktreeId })
  }
  try {
    await updateMetadata(worktreeId, { merge_target_branch: input.mergeTargetBranch })
  } catch (error) {
    throw new WorktreeMetadataError(worktreeId, error)
  }
  // Selection is applied by the canonical snapshot only when this project is
  // still active. An in-flight request cannot redirect a project switch.
  await refreshProject(projectId).catch(report)
  return worktreeId
}
export async function deleteWorktree(id: string, confirmDiscard = false, idempotencyKey?: string) {
  return request<{ metadata_error?: string }>(`/api/worktrees/${encodeURIComponent(id)}`, { confirm_discard: confirmDiscard }, 'DELETE', idempotencyKey)
}
export async function stopWorktreeProcesses(projectId: string, daemonIds: number[]) {
  for (const id of daemonIds) await request(`/api/projects/${encodeURIComponent(projectId)}/processes/${id}`, {}, 'DELETE')
}
export async function updateMetadata(id: string, patch: Record<string, string>) {
  await request(`/api/worktrees/${encodeURIComponent(id)}/metadata`, patch, 'PATCH')
}
export async function fetchClosedPullRequests(projectId: string): Promise<PullRequest[]> {
  const rows = await request<RemotePR[]>(`/api/projects/${encodeURIComponent(projectId)}/pull-requests?state=closed`)
  return (rows ?? []).map(row => pullRequest(projectId, row))
}

const pullRequestLoads = new Map<string, Promise<void>>()
const pullRequestLoadedAt = new Map<string, number>()
const pullRequestLoadedVersion = new Map<string, string>()
function pullRequestVersion(id: string) {
  return `${heads.get(id)}:${useBonsaiStore.getState().pullRequests.find(pr => pr.id === id)?.updatedAt}`
}

export function loadPullRequest(id: string, force = false): Promise<void> {
  const pending = pullRequestLoads.get(id)
  if (pending) return pending
  if (!force && pullRequestLoadedVersion.get(id) === pullRequestVersion(id) && Date.now() - (pullRequestLoadedAt.get(id) ?? 0) < 60_000) return Promise.resolve()
  const loading = fetchPullRequest(id).finally(() => pullRequestLoads.delete(id))
  pullRequestLoads.set(id, loading)
  return loading
}

async function fetchPullRequest(id: string) {
  const index = id.lastIndexOf(':')
  const repo = id.slice(0, index)
  const number = id.slice(index + 1)
  if (index < 0) return
  const headBefore = heads.get(id)
  try {
    const remote = await request<RemotePR>(`/api/projects/${encodeURIComponent(repo)}/pull-requests/${number}`)
    if (headBefore && heads.get(id) !== headBefore) return
    const mapped = pullRequest(repo, remote)
    const checks = await request<RemoteCheck[]>(`/api/projects/${encodeURIComponent(repo)}/checks/${remote.head_sha}`)
    if (heads.get(id) && heads.get(id) !== remote.head_sha) return
    heads.set(id, remote.head_sha)
    mapped.checks = checks.map(mapCheck)
    pullRequestLoadedAt.set(id, Date.now())
    useBonsaiStore.setState(state => ({
      ...changedPatch(state, { pullRequests: replaceScope(state.pullRequests, [mapped], value => value.id === id) }),
    }))
    pullRequestLoadedVersion.set(id, pullRequestVersion(id))
  } catch (error) {
    report(error)
  }
}
export async function changePullRequest(id: string, status: PullRequest['status']) {
  const current = useBonsaiStore.getState().pullRequests.find(p => p.id === id)
  if (!current) return
  const repo = id.slice(0, id.lastIndexOf(':'))
  const action = status === 'Merged'
    ? 'merge'
    : status === 'Closed'
      ? 'close'
      : current.status === 'Draft'
        ? 'ready'
        : 'reopen'
  try {
    await request(
      `/api/projects/${encodeURIComponent(repo)}/pull-requests/${current.number}/${action}`,
      action === 'merge' ? { method: 'merge', head_sha: heads.get(id) } : {},
    )
    await requestProjectRefresh(repo, 'provider')
    await loadPullRequest(id, true)
  } catch (error) {
    report(error)
  }
}
export async function reviewPullRequest(id: string, body: string, kind: 'comment' | 'approve' | 'request-changes') {
  const repo = id.slice(0, id.lastIndexOf(':'))
  const p = useBonsaiStore.getState().pullRequests.find(value => value.id === id)
  if (!p) return
  try {
    await request(`/api/projects/${encodeURIComponent(repo)}/pull-requests/${p.number}/reviews`, {
      body,
      event: kind === 'approve' ? 'APPROVE' : kind === 'request-changes' ? 'REQUEST_CHANGES' : 'COMMENT',
      commit_id: heads.get(id),
    })
    await requestProjectRefresh(repo, 'provider')
    await loadPullRequest(id, true)
  } catch (error) {
    report(error)
  }
}
export function reconcileCatalog(repos: Repository[]) {
  const state = useBonsaiStore.getState()
  const unchanged = repos.length === state.projects.length && repos.every((repo, index) => {
    const current = state.projects[index]?.id === repo.id ? state.projects[index] : undefined
    return current
      && current.path === repo.path
      && current.rootId === repo.root_id
      && current.available === repo.available
      && current.workspaceId === repo.workspace_id
      && current.name === (repo.name ?? current.name)
      && current.repository === (current.repository || repo.full_name)
      && current.defaultBranch === (current.defaultBranch || repo.default_branch)
  })
  if (unchanged) return
  const ids = new Set(repos.map(r => r.id))
  const removedProjects = state.projects.filter(project => !ids.has(project.id) && project.id !== 'local')
  const removedGroups = new Set(removedProjects.flatMap(project => (state.worktreeGroups[project.id] ?? []).map(group => group.id)))
  const launch = repos.find(r => r.launch)
  const previousId = state.activeProjectId === 'local' ? launch?.id : state.activeProjectId
  const active = repos.find(r => r.id === previousId) ?? repos[0]
  const worktrees = state.worktrees
    .filter(w => ids.has(w.projectId) || (w.projectId === 'local' && launch))
    .map(w => w.projectId === 'local' && launch ? { ...w, projectId: launch.id } : w)
  let selection = state.selection
  if (selection.type === 'project' && selection.id === 'local' && launch) selection = { type: 'project', id: launch.id }
  if (
    (selection.type === 'project' && !ids.has(selection.id))
    || (selection.type === 'worktree' && state.worktrees.some(w => w.id === selection.id) && !worktrees.some(w => w.id === selection.id))
    || (state.activeProjectId !== 'local' && !ids.has(state.activeProjectId))
  ) {
    selection = { type: 'project', id: active?.id ?? '' }
  }
  for (const projects of githubRepositoryProjects.values()) {
    for (const id of projects) if (!ids.has(id)) projects.delete(id)
  }
  for (const id of [...sequenceGuards.keys()]) if (!ids.has(id)) sequenceGuards.delete(id)
  const migrateKey = (key: string) => {
    if (!launch) return key
    if (key === 'local') return launch.id
    for (const prefix of ['default:', 'env:', 'stack:']) {
      if (key === `${prefix}local` || key.startsWith(`${prefix}local:`)) {
        return `${prefix}${launch.id}${key.slice(prefix.length + 5)}`
      }
    }
    return key
  }
  const nodePlacements = Object.fromEntries(
    Object.entries(state.nodePlacements).map(([key, value]) => [migrateKey(key), state.nodePlacements[migrateKey(key)] ?? value]),
  )
  const processes = state.processes.filter(value => ids.has(value.projectId))
  const removedTrees = new Set(state.worktrees.filter(tree => !worktrees.some(value => value.id === tree.id)).map(tree => tree.id))
  const agents = state.agents.filter(agent => !removedTrees.has(agent.worktreeId))
  const removedNodes = new Set([
    ...removedTrees,
    ...removedProjects.flatMap(project => [project.id, `default:${project.id}`, `env:${project.id}`]),
    ...[...removedGroups].map(id => `stack:${id}`),
    ...state.agents.filter(agent => removedTrees.has(agent.worktreeId)).map(agent => agent.id),
    ...state.processes.filter(process => !ids.has(process.projectId)).map(process => process.id),
    ...removedProjects.map(project => `process-shelf:${project.id}`),
    ...Object.entries(state.terminalViewPreferences).filter(([projectId]) => !ids.has(projectId) && !(projectId === 'local' && launch))
      .flatMap(([, project]) => Object.values(project.worktrees).flatMap(view => view.open.map(ref => ref.id))),
  ])
  if (selection.type === 'agent' && state.agents.some(agent => agent.id === selection.id && removedTrees.has(agent.worktreeId))) selection = { type: 'project', id: active?.id ?? '' }
  if (selection.type === 'process' && state.processes.some(process => process.id === selection.id && !ids.has(process.projectId))) selection = { type: 'project', id: active?.id ?? '' }
  const patch = changedPatch(state, {
    nodePlacements: Object.fromEntries(Object.entries(nodePlacements).filter(([id]) => !removedNodes.has(id) && !removedProjects.some(project => id.startsWith(`stack:${project.id}:`)))),
    agents,
    terminalSessions: state.terminalSessions.filter(session => !session.agentId || agents.some(agent => agent.id === session.agentId)),
    detachedStackWorktreeIds: state.detachedStackWorktreeIds.filter(id => !removedTrees.has(id)),
    expandedHistoryWorktreeIds: state.expandedHistoryWorktreeIds.filter(id => !removedTrees.has(id)),
    collapsedBranchIds: state.collapsedBranchIds.filter(id => !removedTrees.has(id)).map(migrateKey),
    expandedAutomaticGroups: state.expandedAutomaticGroups.filter(id => !removedGroups.has(id)),
    projects: repos.map(r => {
      const previous = state.projects.find(p => p.id === r.id)
      return previous
        ? {
            ...previous,
            workspaceId: r.workspace_id,
            path: r.path,
            rootId: r.root_id,
            available: r.available,
            name: r.name ?? previous.name,
            repository: previous.repository || r.full_name,
            defaultBranch: previous.defaultBranch || r.default_branch,
            health: r.available === false ? 'idle' : previous.health,
          }
        : project(r)
    }),
    activeProjectId: active?.id ?? '',
    activeWorkspaceId: active?.workspace_id ?? '',
    selection,
    worktrees,
    processes,
    pullRequests: state.pullRequests.filter(p => ids.has(p.id.slice(0, p.id.lastIndexOf(':')))),
    branchCandidates: Object.fromEntries(Object.entries(state.branchCandidates).filter(([id]) => ids.has(id))),
    worktreeGroups: Object.fromEntries(Object.entries(state.worktreeGroups).filter(([id]) => ids.has(id))),
    repositorySync: Object.fromEntries(Object.entries(state.repositorySync).filter(([id]) => ids.has(id))),
    gitBranches: Object.fromEntries(Object.entries(state.gitBranches).filter(([id]) => ids.has(id))),
    gitOnline: Object.fromEntries(repos.map(r => [r.id, r.available !== false && (state.gitOnline[r.id] ?? false)])),
    syncFreshness: Object.fromEntries(Object.entries(state.syncFreshness).filter(([id]) => ids.has(id))),
    terminalViewPreferences: Object.fromEntries(Object.entries(state.terminalViewPreferences).map(([id, value]) => [id === 'local' && launch ? launch.id : id, value]).filter(([id]) => ids.has(id as string))),
    gitError: '',
  })
  const working = { ...state, ...patch }
  if (active && (active.id !== state.activeProjectId || (!working.dockWorktreeId && working.terminalViewPreferences[active.id]))) {
    Object.assign(patch, switchRuntimeScope(working, active.id, state.activeProjectId === 'local' && state.dockWorktreeId ? state.dockWorktreeId : undefined))
  } else Object.assign(patch, activeRuntimePatch(working))
  if (Object.keys(patch).length) useBonsaiStore.setState(patch)
}

let catalogRequest: Promise<void> | undefined
export function refreshCatalog(loadSnapshots = false): Promise<void> {
  if (catalogRequest) return catalogRequest
  const generation = activeGeneration
  catalogRequest = (async () => {
    const repos = await request<Repository[]>('/api/projects')
    if (generation !== activeGeneration) return
    reconcileCatalog(repos)
    if (loadSnapshots) {
      const snapshots = await Promise.all(
        repos.filter(repo => repo.available !== false).map(repo => request<Snapshot>(`/api/projects/${encodeURIComponent(repo.id)}/git`)),
      )
      if (generation === activeGeneration) applySnapshots(snapshots, generation)
    }
  })().finally(() => {
    catalogRequest = undefined
  })
  return catalogRequest
}
function markConnectionStateStale() {
  useBonsaiStore.setState(state => ({
    projects: state.projects.map(project => ({ ...project, health: project.health === 'error' ? 'error' : 'idle' })),
    syncFreshness: Object.fromEntries(Object.entries(state.syncFreshness).map(([projectId, components]) => [
      projectId,
      Object.fromEntries(Object.entries(components).map(([component, value]) => [
        component,
        value.state === 'ready' ? { ...value, state: 'stale' as const } : value,
      ])),
    ])),
  }))
}

export function startGitBackend() {
  let closed = false
  let socket: WebSocket | undefined
  let reconnectTimer: ReturnType<typeof setTimeout> | undefined
  let heartbeatTimer: ReturnType<typeof setTimeout> | undefined
  let retryDelay = 1000
  let bootstrapping = true
  let bootstrapCatalog: Repository[] = []
  const bootstrapSnapshots = new Map<string, Snapshot>()
  const pendingSnapshots = new Map<string, Snapshot>()
  const generation = ++activeGeneration
  // The project last reported to the backend as in view.
  let reportedFocus: string | undefined

  const focusedProject = () => {
    if (typeof document !== 'undefined' && document.visibilityState === 'hidden') return ''
    const id = useBonsaiStore.getState().activeProjectId
    return id && id !== 'local' ? id : ''
  }
  const reportFocus = () => {
    if (closed || !socket) return
    const next = focusedProject()
    if (next !== reportedFocus && sendEventFocus(socket, next)) reportedFocus = next
  }
  const unsubscribeFocus = useBonsaiStore.subscribe(reportFocus)
  if (typeof document !== 'undefined') document.addEventListener('visibilitychange', reportFocus)

  const onEvent = (data: LocalEvent) => {
    if (closed || generation !== activeGeneration) return
    clearTimeout(heartbeatTimer)
    heartbeatTimer = setTimeout(() => socket?.close(), 75_000)
    if (data.type === 'ready' && data.epoch) {
      setEpoch(data.epoch)
      return
    }
    if (data.type === 'catalog' && Array.isArray(data.projects)) {
      const repos = data.projects as Repository[]
      if (bootstrapping) {
        bootstrapCatalog = repos
      } else {
        reconcileCatalog(repos)
        for (const [projectId, snapshot] of pendingSnapshots) {
          if (!useBonsaiStore.getState().projects.some(project => project.id === projectId)) continue
          pendingSnapshots.delete(projectId)
          applySnapshot(snapshot, generation)
        }
      }
      return
    }
    if ((data.type === 'project_snapshot' || data.type === 'project_update') && data.snapshot) {
      const snapshot = data.snapshot as Snapshot
      if (data.epoch && snapshot.epoch && data.epoch !== snapshot.epoch) return
      if (bootstrapping) {
        const previous = bootstrapSnapshots.get(snapshot.repository.id)
        if (!previous || snapshot.sequence >= previous.sequence) bootstrapSnapshots.set(snapshot.repository.id, snapshot)
      } else if (!useBonsaiStore.getState().projects.some(project => project.id === snapshot.repository.id)) {
        const previous = pendingSnapshots.get(snapshot.repository.id)
        if (!previous || snapshot.sequence >= previous.sequence) pendingSnapshots.set(snapshot.repository.id, snapshot)
      } else {
        applySnapshot(snapshot, generation)
      }
      return
    }
    if (data.type === 'bootstrap_complete') {
      reconcileCatalog(bootstrapCatalog)
      applySnapshots([...bootstrapSnapshots.values()], generation)
      bootstrapSnapshots.clear()
      bootstrapping = false
    }
  }

  const scheduleReconnect = () => {
    if (closed || generation !== activeGeneration) return
    clearTimeout(reconnectTimer)
    reconnectTimer = setTimeout(() => { void connect() }, retryDelay)
    retryDelay = Math.min(retryDelay * 2, 30_000)
  }

  const connect = async () => {
    try {
      bootstrapping = true
      bootstrapCatalog = []
      bootstrapSnapshots.clear()
      pendingSnapshots.clear()
      // The persisted active project is the one worth refreshing first. '' and
      // 'local' are placeholders before the catalog has loaded.
      const activeProjectId = useBonsaiStore.getState().activeProjectId
      const connection = await openLocalEvents(onEvent, activeProjectId && activeProjectId !== 'local' ? activeProjectId : undefined)
      if (closed || generation !== activeGeneration) {
        connection.socket.close()
        return
      }
      socket = connection.socket
      // authenticate already named the active project; a hidden tab corrects it.
      reportedFocus = activeProjectId && activeProjectId !== 'local' ? activeProjectId : ''
      reportFocus()
      retryDelay = 1000
      useBonsaiStore.setState({ gitError: '' })
      clearTimeout(heartbeatTimer)
      heartbeatTimer = setTimeout(() => socket?.close(), 75_000)
      setEpoch(connection.epoch)
      socket.onclose = event => {
        if (closed || generation !== activeGeneration) return
        markConnectionStateStale()
        clearTimeout(heartbeatTimer)
        if (event.code === 1008) {
          invalidateLocalSession()
          markLocalConnectionLost('The local event session expired. Reconnect to create a fresh capability.')
        } else {
          useBonsaiStore.setState({ gitError: 'Local updates disconnected. Reconnecting…' })
          scheduleReconnect()
        }
      }
      socket.onerror = () => {
        if (!closed) socket?.close()
      }
    } catch (error) {
      if (!closed && generation === activeGeneration) {
        useBonsaiStore.setState({ gitError: error instanceof Error ? error.message : String(error) })
        scheduleReconnect()
      }
    }
  }
  void connect()
  return () => {
    closed = true
    unsubscribeFocus()
    if (typeof document !== 'undefined') document.removeEventListener('visibilitychange', reportFocus)
    clearTimeout(reconnectTimer)
    clearTimeout(heartbeatTimer)
    if (generation === activeGeneration) activeGeneration++
    if (socket) {
      socket.onclose = null
      socket.close()
    }
  }
}
export async function localCommand(action: string, args: Record<string, unknown> = {}) {
  const id = useBonsaiStore.getState().dockWorktreeId
  if (!id) {
    report(new Error('Select a connected worktree first'))
    return
  }
  try {
    const response = await request<{ result?: { state?: string; conflicted_paths?: string[]; error?: string } }>(
      `/api/worktrees/${encodeURIComponent(id)}/${action}`,
      args,
    )
    const op = response.result
    useBonsaiStore.setState({
      notice: op?.state === 'conflict'
        ? `Conflicts: ${(op.conflicted_paths ?? []).join(', ')}. Resolve locally, then continue or abort.`
        : op?.error || `${action} ${op?.state ?? 'completed'}`,
    })
  } catch (error) {
    report(error)
  }
}

export function openGitHub(path: string, projectId = useBonsaiStore.getState().activeProjectId) {
  const p = useBonsaiStore.getState().projects.find(project => project.id === projectId)
  if (p && /^[^/\s]+\/[^/\s]+$/.test(p.repository)) {
    window.open(`https://github.com/${p.repository}/${path}`, '_blank', 'noopener,noreferrer')
  }
}

export function __resetGitSyncForTests() {
  activeEpoch = ''
  activeGeneration = 0
  sequenceGuards.clear()
  githubRepositoryProjects.clear()
  heads.clear()
  pullRequestLoads.clear()
  pullRequestLoadedAt.clear()
  pullRequestLoadedVersion.clear()
  pendingCreatedWorktrees.clear()
  catalogRequest = undefined
}
