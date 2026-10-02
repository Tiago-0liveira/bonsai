import { useBonsaiStore } from '../stores/bonsai'
import type {
  CiStatus,
  CreateWorktreeInput,
  Process,
  ProcessLifecycleStatus,
  Project,
  PullRequest,
  SyncFreshness,
  Worktree,
} from '../types'
import {
  invalidateLocalSession,
  localFetch,
  markLocalConnectionLost,
  openLocalEvents,
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
interface RemoteCheck {
  id?: number
  name: string
  status: string
  conclusion: string
  url?: string
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
  comments?: { author: string; body: string; created_at: string }[]
  reviews?: { author: string; body: string; submitted_at: string }[]
  commits?: { sha: string; message: string; author: string; created_at: string }[]
  files?: { path: string; additions: number; deletions: number; patch: string }[]
}
interface WireFreshness {
  state: SyncFreshness['state']
  updated_at?: string
  error?: { code: string; message: string }
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
interface ProcessSummary {
  id: string
  daemon_id: number
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
interface WorktreeProjection {
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
  metadata: Record<string, { tag: string; merge_target_branch: string; stack_preference: 'auto' | 'never' }>
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
function project(r: Repository): Project {
  return {
    id: r.id,
    path: r.path,
    rootId: r.root_id,
    available: r.available,
    workspaceId: r.workspace_id,
    name: r.name ?? r.full_name.split('/').at(-1) ?? r.id,
    repository: r.full_name,
    description: '',
    health: 'idle',
    defaultBranch: r.default_branch,
    worktreeIds: [],
    openPrCount: 0,
  }
}
export function pullRequest(repo: string, p: RemotePR): PullRequest {
  return {
    id: `${repo}:${p.number}`,
    number: p.number,
    title: p.title,
    description: p.body ?? '',
    branch: p.head,
    headRepository: p.head_repository,
    base: p.base,
    status: p.state === 'merged' ? 'Merged' : p.state === 'closed' ? 'Closed' : p.draft ? 'Draft' : 'Open',
    author: p.author,
    createdAt: p.created_at,
    updatedAt: p.updated_at,
    mergeable: p.mergeable === 'mergeable',
    checks: [],
    commits: (p.commits ?? []).map(c => ({ sha: c.sha, message: c.message, author: c.author, time: c.created_at })),
    conversation: [
      ...(p.comments ?? []).map(c => ({ author: c.author, body: c.body, time: c.created_at, kind: 'comment' as const })),
      ...(p.reviews ?? []).map(c => ({ author: c.author, body: c.body, time: c.submitted_at, kind: 'review' as const })),
    ],
    files: (p.files ?? []).map(f => ({
      path: f.path,
      additions: f.additions,
      deletions: f.deletions,
      diff: (f.patch ?? '').split('\n'),
    })),
  }
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

function wireFreshness(value: WireFreshness | undefined): SyncFreshness {
  return {
    state: value?.state ?? 'loading',
    updatedAt: value?.updated_at,
    error: value?.error,
  }
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

function processHealth(status: ProcessLifecycleStatus): Process['status'] {
  switch (status) {
    case 'running':
    case 'starting':
      return 'healthy'
    case 'backoff':
    case 'stopping':
    case 'orphan':
      return 'warning'
    case 'failed':
    case 'lost':
      return 'error'
    default:
      return 'idle'
  }
}

function mapProcess(value: ProcessSummary): Process {
  return {
    id: value.id,
    projectId: value.project_id,
    daemonId: value.daemon_id,
    worktreeId: value.worktree_id ?? '',
    name: value.label || value.command || `Process #${value.daemon_id}`,
    command: value.command,
    status: processHealth(value.status),
    lifecycleStatus: value.status,
    pid: value.pid,
    port: value.expected_port || undefined,
    url: value.url || undefined,
    startedAt: value.started_at || undefined,
    exitCode: value.exit_code,
    exitError: value.exit_error || undefined,
    serveGroup: value.serve_group || undefined,
    serveName: value.serve_name || undefined,
  }
}

function checkStatus(check: RemoteCheck): 'success' | 'running' | 'failed' {
  if (check.status !== 'completed') return 'running'
  return ['success', 'neutral', 'skipped'].includes(check.conclusion) ? 'success' : 'failed'
}

function ciStatus(value: WorktreeProjection['ci'] | undefined): CiStatus {
  return value?.status ?? 'unknown'
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

  const remoteByNumber = new Map<number, RemotePR>()
  for (const value of snapshot.remote?.pull_requests ?? []) remoteByNumber.set(value.number, value)
  for (const projection of Object.values(snapshot.worktree_state ?? {})) {
    if (projection.pull_request) remoteByNumber.set(projection.pull_request.number, projection.pull_request)
  }
  const prs = [...remoteByNumber.values()].map(p => {
    const mapped = pullRequest(id, p)
    heads.set(mapped.id, p.head_sha)
    const previous = state.pullRequests.find(v => v.id === mapped.id)
    const projection = Object.values(snapshot.worktree_state ?? {}).find(v => v.pull_request?.number === p.number)
    if (projection?.ci?.checks) {
      mapped.checks = projection.ci.checks.map(c => ({ id: c.id, name: c.name, status: checkStatus(c) }))
    } else if (previous?.updatedAt === mapped.updatedAt) {
      mapped.checks = previous.checks
    }
    return previous?.updatedAt === mapped.updatedAt
      ? { ...previous, ...mapped, status: mapped.status, checks: mapped.checks }
      : mapped
  })

  const freshness = Object.fromEntries(
    Object.entries(snapshot.freshness ?? {}).map(([component, value]) => [component, wireFreshness(value)]),
  )
  const localFreshness = freshness.local
  const trees: Worktree[] = (snapshot.local?.worktrees ?? []).map(w => {
    const meta = snapshot.metadata[w.id]
    const st = w.status
    const projection = snapshot.worktree_state?.[w.id]
    const associated = projection?.pull_request
    const pr = associated ? prs.find(item => item.number === associated.number) : undefined
    const ci = projection?.ci
    const failedChecks = (ci?.checks ?? []).filter(check => checkStatus(check) === 'failed').length
    const status = !snapshot.online
      ? 'idle'
      : w.status_error || localFreshness?.state === 'error'
        ? 'warning'
        : st?.dirty
          ? 'warning'
          : st
            ? 'healthy'
            : 'idle'
    return {
      id: w.id,
      projectId: id,
      branch: w.branch,
      path: w.path,
      main: w.main,
      missing: w.missing,
      headSha: w.local_head_sha,
      connection: w.connection ? { state: w.connection.state, reason: w.connection.reason, statusUnknown: w.connection.status_unknown } : undefined,
      kind: w.main ? 'Production' : 'Feature',
      tag: meta?.tag || (w.main ? 'production' : 'untagged'),
      sourceType: 'existing',
      mergeTargetBranch: pr?.base ?? (meta?.merge_target_branch || snapshot.repository.default_branch),
      stackPreference: meta?.stack_preference || 'auto',
      status,
      agentIds: state.agents.filter(a => a.worktreeId === w.id).map(a => a.id),
      prNumber: pr?.number,
      prStatus: pr?.status,
      ciStatus: ciStatus(ci),
      ciFailed: failedChecks,
      checkedSha: ci?.checked_sha,
      upstream: st?.upstream,
      divergenceAvailable: st?.divergence_available,
      gitStatusError: w.status_error?.message,
      ahead: st?.divergence_available ? st.ahead : 0,
      behind: st?.divergence_available ? st.behind : 0,
      dirtyFiles: st?.files?.length ?? 0,
      lastActivity: st?.last_commit?.when ?? '',
      gitState: st?.git_state,
    }
  })

  const p = project(snapshot.repository)
  const mainTree = (snapshot.local?.worktrees ?? []).find(w => w.main)
  const mainCommit = mainTree?.status?.last_commit
  const mainProjection = mainTree ? snapshot.worktree_state?.[mainTree.id] : undefined
  p.health = !snapshot.online
    ? 'idle'
    : localFreshness?.state === 'error'
      ? 'error'
      : localFreshness?.state === 'stale'
        ? 'warning'
        : 'healthy'
  p.worktreeIds = trees.map(w => w.id)
  p.openPrCount = prs.filter(value => value.status === 'Open' || value.status === 'Draft').length
  if (mainCommit) {
    p.defaultBranchInfo = {
      commitSha: mainCommit.sha,
      commitMessage: mainCommit.subject,
      lastActivity: mainCommit.when,
      ciStatus: ciStatus(mainProjection?.ci),
    }
  }

  const branches = snapshot.local?.branches ?? []
  const candidates = snapshot.branch_candidates ?? []
  const groups = snapshot.local?.groups ?? []
  const sync = snapshot.sync ?? state.repositorySync[id]

  const nextProcesses = (snapshot.processes ?? []).map(mapProcess)
  const allTrees = [...state.worktrees.filter(w => w.projectId !== id), ...trees]
  const allProcesses = [...state.processes.filter(process => process.projectId !== id), ...nextProcesses]
  const removedWorktreeIds = new Set(state.worktrees.filter(w => w.projectId === id && !trees.some(tree => tree.id === w.id)).map(w => w.id))
  const agents = state.agents.filter(agent => !removedWorktreeIds.has(agent.worktreeId))
  const pendingCreatedId = pendingCreatedWorktrees.get(id)
  const createdVisible = pendingCreatedId && trees.some(w => w.id === pendingCreatedId)
  if (createdVisible) pendingCreatedWorktrees.delete(id)
  const selection = createdVisible && state.activeProjectId === id
    ? { type: 'worktree' as const, id: pendingCreatedId }
    : state.selection.type === 'agent' && !agents.some(agent => agent.id === state.selection.id)
      ? { type: 'project' as const, id: state.activeProjectId }
      : state.selection.type === 'worktree'
    && state.worktrees.some(w => w.id === state.selection.id && w.projectId === id)
    && !trees.some(w => w.id === state.selection.id)
    ? { type: 'project' as const, id }
    : state.selection
  const dockWorktreeId = createdVisible && state.activeProjectId === id
    ? pendingCreatedId
    : state.dockWorktreeId && allTrees.some(w => w.id === state.dockWorktreeId)
    ? state.dockWorktreeId
    : allTrees.find(w => w.projectId === state.activeProjectId)?.id ?? ''
  const liveRuntimeIds = new Set([...agents.map(agent => agent.id), ...allProcesses.map(process => process.id)])
  const openRuntimeIds = state.openRuntimeIds.filter(runtimeId => liveRuntimeIds.has(runtimeId))
  const dockRuntimeId = state.dockRuntimeId && liveRuntimeIds.has(state.dockRuntimeId) ? state.dockRuntimeId : ''

  const patch: Partial<StoreState> = {
    selection,
    agents,
    nodePlacements: Object.fromEntries(Object.entries(state.nodePlacements).filter(([nodeId]) => !removedWorktreeIds.has(nodeId) && !state.agents.some(agent => agent.id === nodeId && removedWorktreeIds.has(agent.worktreeId)))),
    detachedStackWorktreeIds: [...new Set([...state.detachedStackWorktreeIds.filter(nodeId => !removedWorktreeIds.has(nodeId)), ...(createdVisible ? [pendingCreatedId] : [])])],
    expandedHistoryWorktreeIds: state.expandedHistoryWorktreeIds.filter(nodeId => !removedWorktreeIds.has(nodeId)),
    terminalSessions: state.terminalSessions.filter(session => !session.agentId || agents.some(agent => agent.id === session.agentId)),
    branchCandidates: { ...state.branchCandidates, [id]: candidates },
    worktreeGroups: { ...state.worktreeGroups, [id]: groups },
    repositorySync: sync ? { ...state.repositorySync, [id]: sync } : state.repositorySync,
    projects: state.projects.map(value => value.id === id ? p : value),
    worktrees: allTrees,
    processes: allProcesses,
    pullRequests: [...state.pullRequests.filter(value => !value.id.startsWith(`${id}:`)), ...prs],
    gitBranches: { ...state.gitBranches, [id]: branches },
    gitOnline: { ...state.gitOnline, [id]: snapshot.online },
    syncFreshness: { ...state.syncFreshness, [id]: freshness },
    dockWorktreeId,
    dockRuntimeId,
    openRuntimeIds,
  }
  const semanticFreshness = (value: typeof state.syncFreshness) => Object.fromEntries(
    Object.entries(value).map(([projectId, components]) => [
      projectId,
      Object.fromEntries(Object.entries(components).map(([component, freshness]) => [
        component,
        { state: freshness.state, error: freshness.error },
      ])),
    ]),
  )
  const currentSemantic = {
    selection: state.selection,
    agents: state.agents,
    detachedStackWorktreeIds: state.detachedStackWorktreeIds,
    worktreeGroups: state.worktreeGroups,
    projects: state.projects,
    worktrees: state.worktrees,
    processes: state.processes,
    pullRequests: state.pullRequests,
    gitBranches: state.gitBranches,
    gitOnline: state.gitOnline,
    syncFreshness: semanticFreshness(state.syncFreshness),
    dockWorktreeId: state.dockWorktreeId,
    dockRuntimeId: state.dockRuntimeId,
    openRuntimeIds: state.openRuntimeIds,
  }
  const nextSemantic = {
    selection,
    agents: patch.agents,
    detachedStackWorktreeIds: patch.detachedStackWorktreeIds,
    worktreeGroups: patch.worktreeGroups,
    projects: patch.projects,
    worktrees: patch.worktrees,
    processes: patch.processes,
    pullRequests: patch.pullRequests,
    gitBranches: patch.gitBranches,
    gitOnline: patch.gitOnline,
    syncFreshness: semanticFreshness(patch.syncFreshness as typeof state.syncFreshness),
    dockWorktreeId,
    dockRuntimeId,
    openRuntimeIds,
  }
  guard.applied = Math.max(guard.applied, snapshot.sequence)
  // Provider and process updates do not invalidate local files or diffs. Include
  // raw file status entries so path changes with unchanged counts still reload.
  const localWorktrees = JSON.stringify(snapshot.local?.worktrees ?? [])
  const filesChanged = guard.localWorktrees !== localWorktrees
  guard.localWorktrees = localWorktrees
  if (JSON.stringify(currentSemantic) === JSON.stringify(nextSemantic)) {
    const updates = { syncFreshness: patch.syncFreshness, repositorySync: patch.repositorySync, branchCandidates: patch.branchCandidates, ...(filesChanged ? { gitRevision: state.gitRevision + 1 } : {}) }
    if (JSON.stringify(updates) === JSON.stringify({ syncFreshness: state.syncFreshness, repositorySync: state.repositorySync, branchCandidates: state.branchCandidates })) return undefined
    return updates
  }
  if (filesChanged) patch.gitRevision = state.gitRevision + 1
  return patch
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
    await updateMetadata(worktreeId, { merge_target_branch: input.mergeTargetBranch, tag: state.worktreeTags.find(t => t.id === input.tagId)?.name ?? '' })
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
    mapped.checks = checks.map(check => ({ id: check.id, name: check.name, status: checkStatus(check) }))
    pullRequestLoadedAt.set(id, Date.now())
    useBonsaiStore.setState(state => ({
      pullRequests: [...state.pullRequests.filter(value => value.id !== id), mapped],
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
  const unchanged = repos.length === state.projects.length && repos.every(repo => {
    const current = state.projects.find(project => project.id === repo.id)
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
  const liveRuntimeIds = new Set([...state.agents.map(agent => agent.id), ...processes.map(value => value.id)])
  useBonsaiStore.setState({
    nodePlacements,
    collapsedBranchIds: state.collapsedBranchIds.map(migrateKey),
    collapsedTagGroups: state.collapsedTagGroups.map(key => launch && key.startsWith('local:') ? launch.id + key.slice(5) : key),
    projects: repos.map(r => {
      const previous = state.projects.find(p => p.id === r.id)
      return previous
        ? {
            ...previous,
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
    dockWorktreeId: worktrees.some(w => w.id === state.dockWorktreeId) || !state.worktrees.length
      ? state.dockWorktreeId
      : worktrees.find(w => w.projectId === active?.id)?.id ?? '',
    dockRuntimeId: state.dockRuntimeId && liveRuntimeIds.has(state.dockRuntimeId) ? state.dockRuntimeId : '',
    openRuntimeIds: state.openRuntimeIds.filter(id => liveRuntimeIds.has(id)),
    gitError: '',
  })
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
      const connection = await openLocalEvents(onEvent)
      if (closed || generation !== activeGeneration) {
        connection.socket.close()
        return
      }
      socket = connection.socket
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
