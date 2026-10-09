import { processDeleted } from '../stores/processProjection'
import { mapAgent } from './agents'
import type { CiStatus, Process, ProcessLifecycleStatus, Project, PullRequest, PullRequestCheck, SyncFreshness, Worktree } from '../types'
import type { Snapshot, Repository, RemotePR, RemoteCheck, WireFreshness, ProcessSummary, WorktreeProjection } from './git'
import type { BonsaiState } from '../stores/bonsai'
import { changedPatch, replaceScope, shareEqual } from '../stores/reconciliation'
import type { RuntimeAuthority } from '../stores/runtimePreferences'

export function snapshotRuntimeAuthority(snapshot: Snapshot): RuntimeAuthority {
  const successful = snapshot.online && snapshot.repository.available !== false
  const ready = (component: string, present: boolean) => successful && present
    && (!snapshot.freshness?.[component] || snapshot.freshness[component].state === 'ready')
  return {
    worktrees: ready('local', snapshot.local != null),
    agents: ready('agents', snapshot.agents !== undefined),
    processes: ready('processes', snapshot.processes !== undefined && snapshot.process_visibility != null),
  }
}

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
    // GitHub reports mergeability lazily: anything but an explicit answer stays unknown.
    mergeable: p.mergeable === 'mergeable' ? true : p.mergeable === 'conflicting' ? false : undefined,
    checks: [],
    // Only the detail response carries these; leaving the keys out keeps a
    // list refresh from erasing what the detail already loaded.
    ...(p.changed_files !== undefined && { totals: { additions: p.additions ?? 0, deletions: p.deletions ?? 0, changedFiles: p.changed_files } }),
    ...(p.review_summary !== undefined && {
      reviews: { requested: p.requested_reviewers ?? [], approvals: p.review_summary.approvals, changesRequested: p.review_summary.changes_requested },
    }),
    ...(typeof p.behind_by === 'number' && { behindBy: p.behind_by }),
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

export function mapProcess(value: ProcessSummary): Process {
  return {
    commandKey: value.command_key,
    executionOrder: value.execution_order,
    id: value.id,
    projectId: value.project_id,
    daemonId: value.daemon_id,
    revision: value.revision,
    policy: value.policy,
    restarts: value.restarts,
    retryCount: value.retry_count,
    attempt: value.attempt,
    retryAt: value.retry_at,
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

export function mapCheck(check: RemoteCheck): PullRequestCheck {
  return {
    id: check.id,
    name: check.name,
    status: checkStatus(check),
    ...(check.started_at && { startedAt: check.started_at }),
    ...(check.completed_at && { completedAt: check.completed_at }),
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
  const authority = snapshotRuntimeAuthority(snapshot)
  const nextAgents = snapshot.agents?.map(a => mapAgent(a, state.agents.find(old => old.id === a.id)))
  if (nextAgents && !authority.agents) {
    for (const old of state.agents) {
      if ((old.projectId === id || state.worktrees.some(tree => tree.projectId === id && tree.id === old.worktreeId)) && !nextAgents.some(agent => agent.id === old.id)) nextAgents.push(old)
    }
  }
  const scopedAgents = nextAgents ?? state.agents.filter(a => a.projectId === id || state.worktrees.some(w => w.id === a.worktreeId && w.projectId === id))
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
      mapped.checks = projection.ci.checks.map(mapCheck)
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
      sourceType: 'existing',
      mergeTargetBranch: pr?.base ?? (meta?.merge_target_branch || snapshot.repository.default_branch),
      stackPreference: meta?.stack_preference || 'auto',
      status,
      agentIds: scopedAgents.filter(a => a.worktreeId === w.id).map(a => a.id),
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
  if (!authority.worktrees) {
    for (const old of state.worktrees) {
      if (old.projectId === id && !trees.some(tree => tree.id === old.id)) trees.push(old)
    }
  }

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

  const previousVisibility = state.processVisibility?.[id]
  const incomingVisibility = authority.processes ? snapshot.process_visibility : undefined
  const visibility = {
    cutoffs: { ...previousVisibility?.cutoffs }, deleted: { ...previousVisibility?.deleted, ...incomingVisibility?.deleted },
  }
  for (const [key, order] of Object.entries(incomingVisibility?.cutoffs ?? {})) visibility.cutoffs[key] = Math.max(order, visibility.cutoffs[key] ?? 0)
  const nextProcesses: Process[] = (snapshot.processes ?? []).filter(value => !visibility.deleted[value.daemon_id]).map(value => {
    const mapped = mapProcess(value)
    const old = state.processes.find(p => p.id === mapped.id)
    return old && (old.revision ?? 0) > (mapped.revision ?? 0) ? { ...old, pendingSnapshot: authority.processes ? false : old.pendingSnapshot } : { ...mapped, pendingSnapshot: authority.processes ? false : old?.pendingSnapshot }
  })
  for (const old of state.processes) {
    if (old.projectId === id && !processDeleted({ ...state, processVisibility: { ...state.processVisibility, [id]: visibility } }, id, old.daemonId) && (old.pendingSnapshot || !authority.processes) && !nextProcesses.some(p => p.id === old.id)) nextProcesses.push(old)
  }

  return changedPatch(state, {
    processAuthorityReady: { ...state.processAuthorityReady, [id]: authority.processes },
    processVisibility: { ...state.processVisibility, [id]: visibility },
    agents: nextAgents ? replaceScope(state.agents, nextAgents, a => a.projectId === id || state.worktrees.some(w => w.id === a.worktreeId && w.projectId === id)) : state.agents,
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
