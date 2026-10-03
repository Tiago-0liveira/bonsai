import type { CiStatus, Process, ProcessLifecycleStatus, Project, PullRequest, SyncFreshness, Worktree } from '../types'
import type { Snapshot, Repository, RemotePR, RemoteCheck, WireFreshness, ProcessSummary, WorktreeProjection } from './git'
import type { BonsaiState } from '../stores/bonsai'
import { changedPatch, replaceScope, shareEqual } from '../stores/reconciliation'

export function project(r: Repository): Project {
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

function wireFreshness(value: WireFreshness | undefined): SyncFreshness {
  return {
    state: value?.state ?? 'loading',
    updatedAt: value?.updated_at,
    error: value?.error,
  }
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

export function checkStatus(check: RemoteCheck): 'success' | 'running' | 'failed' {
  if (check.status !== 'completed') return 'running'
  return ['success', 'neutral', 'skipped'].includes(check.conclusion) ? 'success' : 'failed'
}

function ciStatus(value: WorktreeProjection['ci'] | undefined): CiStatus {
  return value?.status ?? 'unknown'
}

// Mapping has no transport, ordering, store writes or selection side effects.
export function reconcileSnapshotEntities(snapshot: Snapshot, state: BonsaiState): Partial<BonsaiState> {
  const id = snapshot.repository.id
  const remoteByNumber = new Map<number, RemotePR>()
  for (const value of snapshot.remote?.pull_requests ?? []) remoteByNumber.set(value.number, value)
  for (const projection of Object.values(snapshot.worktree_state ?? {})) {
    if (projection.pull_request) remoteByNumber.set(projection.pull_request.number, projection.pull_request)
  }
  const prs = [...remoteByNumber.values()].map(p => {
    const mapped = pullRequest(id, p)
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

  return changedPatch(state, {
    projects: state.projects.map(value => value.id === id ? shareEqual(value, p) : value),
    worktrees: replaceScope(state.worktrees, trees, value => value.projectId === id),
    processes: replaceScope(state.processes, nextProcesses, value => value.projectId === id),
    pullRequests: replaceScope(state.pullRequests, prs, value => value.id.startsWith(`${id}:`)),
    branchCandidates: { ...state.branchCandidates, [id]: candidates },
    worktreeGroups: { ...state.worktreeGroups, [id]: groups },
    repositorySync: sync ? { ...state.repositorySync, [id]: sync } : state.repositorySync,
    gitBranches: { ...state.gitBranches, [id]: branches },
    gitOnline: { ...state.gitOnline, [id]: snapshot.online },
    syncFreshness: { ...state.syncFreshness, [id]: freshness },
  })
}
