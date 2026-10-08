import { useMemo } from 'react'
import { act, cleanup, render } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useBonsaiStore } from '../stores/bonsai'
import { useProjectAgents, useProjectCanvasPreferences, useProjectWorktrees } from '../stores/projectSelectors'
import { usePullRequestCatalog } from '../features/github/usePullRequestCatalog'
import { __resetGitSyncForTests, applySnapshot, reconcileCatalog, type Snapshot } from './git'
import { reconcileSnapshotEntities } from './snapshotReconciliation'
import { agents as recordedAgents } from '../mock/agents'

function snapshot(id = 'a', sequence = 1): Snapshot {
  const pr = { number: 1, title: 'Feature', body: 'description', state: 'open', draft: false, head: 'feature', base: 'main', head_sha: 'sha', author: 'dev', created_at: 'today', updated_at: 'today' }
  return {
    repository: { id, workspace_id: 'workspace', full_name: `owner/${id}`, default_branch: 'main' }, epoch: 'epoch', sequence, online: true, metadata: {},
    local: { branches: [{ name: 'main', remote: false }], worktrees: ['main', 'feature', 'other'].map(branch => ({
      id: `${id}-${branch}`, repository_id: id, branch, main: branch === 'main', local_head_sha: 'sha', connection: { state: 'linked' },
      status: { ahead: 0, behind: 0, staged: 0, modified: 1, untracked: 0, files: [{ path: 'one.ts', status: '.M' }], git_state: '', dirty: true },
    })) },
    remote: { repository: { id: id === 'a' ? 1 : 2, full_name: `owner/${id}`, default_branch: 'main' }, branches: [], pull_requests: [pr] },
    worktree_state: { [`${id}-feature`]: { pull_request: pr, ci: { status: 'running', checked_sha: 'sha', checks: [{ id: 11, name: 'verify', status: 'in_progress', conclusion: '' }], freshness: { state: 'ready' } } } },
    freshness: { local: { state: 'ready', updated_at: 'today' }, provider: { state: 'ready', updated_at: 'today' } },
    process_visibility: { cutoffs: {}, deleted: {} },
    processes: [{ id: `${id}:process`, project_id: id, daemon_id: 1, worktree_id: `${id}-feature`, label: 'serve', command: 'serve', status: 'running' }],
  }
}

describe('stable snapshot reconciliation and consumers', () => {
  beforeEach(() => {
    __resetGitSyncForTests()
    useBonsaiStore.setState({ projects: [], worktrees: [], processes: [], pullRequests: [], agents: [], branchCandidates: {}, worktreeGroups: {}, repositorySync: {}, gitBranches: {}, gitOnline: {}, syncFreshness: {}, nodePlacements: {}, openRuntimeIds: [], dockRuntimeId: '', dockWorktreeId: '', selection: { type: 'project', id: 'a' }, activeProjectId: 'a', detachedStackWorktreeIds: [], expandedHistoryWorktreeIds: [], collapsedBranchIds: [], terminalSessions: [] })
    reconcileCatalog([snapshot('a').repository, snapshot('b').repository])
    applySnapshot(snapshot('a'))
    applySnapshot(snapshot('b'))
  })
  afterEach(cleanup)

  it('retains authoritative sessions across bootstrap and scopes removal to one project', () => {
    const a = snapshot('a', 2)
    a.agents = [{ id: 'session-a', project_id: 'a', worktree_id: 'a-feature', account_id: 'account', provider: 'antigravity', profile_name: 'Profile', name: 'Agent', state: 'starting', created_at: 'today' }]
    applySnapshot(a)
    expect(useBonsaiStore.getState().agents[0]).toMatchObject({ id: 'session-a', lifecycleState: 'starting' })
    expect(useBonsaiStore.getState().worktrees.find(w => w.id === 'a-feature')?.agentIds).toEqual(['session-a'])
    const b = snapshot('b', 2); b.agents = []
    applySnapshot(b)
    expect(useBonsaiStore.getState().agents).toHaveLength(1)
    applySnapshot({ ...a, sequence: 3, agents: [{ ...a.agents[0], state: 'running' }] })
    expect(useBonsaiStore.getState().agents[0].lifecycleState).toBe('running')
    applySnapshot({ ...a, sequence: 4, agents: [] })
    expect(useBonsaiStore.getState().agents).toHaveLength(0)
  })

  it('returns a narrow process patch and preserves unrelated collection and entity identity', () => {
    const before = useBonsaiStore.getState()
    const next = snapshot('a', 2)
    next.processes![0].status = 'backoff'
    expect(Object.keys(reconcileSnapshotEntities(next, before))).toEqual(['processes'])
    applySnapshot(next)
    const after = useBonsaiStore.getState()
    for (const field of ['projects', 'worktrees', 'agents', 'nodePlacements', 'pullRequests', 'openRuntimeIds', 'terminalSessions'] as const) expect(after[field]).toBe(before[field])
    expect(after.gitRevision).toBe(before.gitRevision)
    expect(after.processes.find(value => value.projectId === 'b')).toBe(before.processes.find(value => value.projectId === 'b'))
  })

  it('emits no observable update for an identical higher sequence and still rejects older data', () => {
    const notify = vi.fn()
    const unsubscribe = useBonsaiStore.subscribe(notify)
    expect(applySnapshot(snapshot('a', 9))).toBe(false)
    expect(applySnapshot({ ...snapshot('a', 8), online: false })).toBe(false)
    expect(notify).not.toHaveBeenCalled()
    unsubscribe()
  })

  it('changes nested checks with unchanged timestamps, retaining unrelated entities and nested fields', () => {
    const before = useBonsaiStore.getState()
    const next = snapshot('a', 2)
    next.worktree_state!['a-feature'].ci!.checks[0].status = 'completed'
    next.worktree_state!['a-feature'].ci!.checks[0].conclusion = 'failure'
    next.worktree_state!['a-feature'].ci!.status = 'failed'
    applySnapshot(next)
    const after = useBonsaiStore.getState()
    const changed = after.worktrees.find(tree => tree.id === 'a-feature')!
    expect(changed.ciFailed).toBe(1)
    expect(changed).not.toBe(before.worktrees.find(tree => tree.id === changed.id))
    for (const tree of before.worktrees.filter(tree => tree.id !== changed.id)) expect(after.worktrees.find(value => value.id === tree.id)).toBe(tree)
    expect(after.pullRequests[0].checks[0].status).toBe('failed')
    expect(after.pullRequests[0].commits).toBe(before.pullRequests[0].commits)
    expect(after.projects).toBe(before.projects)
  })

  it('compares connection and freshness contents and raw changed paths independently', () => {
    const before = useBonsaiStore.getState()
    const next = snapshot('a', 2)
    next.local!.worktrees[1].connection = { state: 'unknown', status_unknown: true, reason: 'missing ref' }
    next.local!.worktrees[1].status!.files = [{ path: 'two.ts', status: '.M' }]
    next.freshness!.provider = { state: 'error', updated_at: 'today', error: { code: 'offline', message: 'offline' } }
    applySnapshot(next)
    expect(useBonsaiStore.getState().worktrees.find(tree => tree.id === 'a-feature')?.connection).toMatchObject({ statusUnknown: true, reason: 'missing ref' })
    expect(useBonsaiStore.getState().syncFreshness.a.provider.error?.code).toBe('offline')
    expect(useBonsaiStore.getState().gitRevision).toBe(before.gitRevision + 1)
  })

  it('keeps project subsets and memoized graph/catalog derivation quiet for process and inactive updates', () => {
    const graph = vi.fn()
    const renderProbe = vi.fn()
    let rows: unknown
    function Probe() {
      const project = useBonsaiStore(state => state.projects.find(project => project.id === 'a'))
      const worktrees = useProjectWorktrees('a')
      const agents = useProjectAgents('a')
      const preferences = useProjectCanvasPreferences('a')
      const catalog = usePullRequestCatalog('a')
      useMemo(() => graph(project, worktrees, agents, preferences), [project, worktrees, agents, preferences])
      rows = catalog.rows
      renderProbe()
      return null
    }
    render(<Probe />)
    const baseline = { graph: graph.mock.calls.length, renders: renderProbe.mock.calls.length, rows }
    const process = snapshot('a', 2)
    process.processes![0].status = 'backoff'
    act(() => { applySnapshot(process); applySnapshot({ ...snapshot('b', 2), online: false }) })
    expect(graph).toHaveBeenCalledTimes(baseline.graph)
    expect(renderProbe).toHaveBeenCalledTimes(baseline.renders)
    expect(rows).toBe(baseline.rows)
  })

  it('cleans selection, agents, runtimes and placements on worktree and project deletion', () => {
    const agent = { ...recordedAgents[0], id: 'recorded', worktreeId: 'a-feature', terminalId: 'recorded-terminal' }
    useBonsaiStore.setState({ agents: [agent], selection: { type: 'agent', id: 'recorded' }, dockWorktreeId: 'a-feature', dockRuntimeId: 'recorded', openRuntimeIds: ['recorded'], nodePlacements: { 'a-feature': { x: 1, y: 1, mode: 'manual' }, recorded: { x: 2, y: 2, mode: 'manual' }, 'b-other': { x: 3, y: 3, mode: 'manual' } } })
    const next = snapshot('a', 2)
    next.local!.worktrees = next.local!.worktrees.filter(tree => tree.id !== 'a-feature')
    next.processes = []
    applySnapshot(next)
    expect(useBonsaiStore.getState()).toMatchObject({ agents: [], selection: { type: 'project', id: 'a' }, dockRuntimeId: '', openRuntimeIds: [], nodePlacements: { 'b-other': { x: 3, y: 3 } } })
    useBonsaiStore.setState({ worktreeGroups: { b: [{ id: 'unlinked:b', kind: 'unlinked', worktree_ids: ['b-other'] }] }, expandedAutomaticGroups: ['unlinked:b'], nodePlacements: { ...useBonsaiStore.getState().nodePlacements, 'stack:unlinked:b': { x: 2, y: 2, mode: 'manual' } } })
    reconcileCatalog([snapshot('a').repository])
    expect(useBonsaiStore.getState().nodePlacements).toEqual({})
    expect(useBonsaiStore.getState().expandedAutomaticGroups).toEqual([])
  })

  it('retains surviving entity identities when worktrees are reordered and removed', () => {
    const before = useBonsaiStore.getState()
    const next = snapshot('a', 2)
    next.local!.worktrees = [next.local!.worktrees[2], next.local!.worktrees[0]]
    next.worktree_state = {}
    next.processes = []
    applySnapshot(next)
    const after = useBonsaiStore.getState()
    expect(after.worktrees.filter(tree => tree.projectId === 'a').map(tree => tree.id)).toEqual(['a-other', 'a-main'])
    for (const tree of after.worktrees) expect(tree).toBe(before.worktrees.find(value => value.id === tree.id))
    expect(after.projects.find(project => project.id === 'b')).toBe(before.projects.find(project => project.id === 'b'))
    const notify = vi.fn()
    const unsubscribe = useBonsaiStore.subscribe(notify)
    applySnapshot({ ...next, sequence: 3 })
    expect(notify).not.toHaveBeenCalled()
    unsubscribe()
  })

  it('removes deleted worktree collapse preferences while retaining other projects and surviving placements', () => {
    useBonsaiStore.setState({
      collapsedBranchIds: ['a-feature', 'a-other', 'b-other'],
      nodePlacements: { 'a-feature': { x: 1, y: 1, mode: 'manual' }, 'b-other': { x: 2, y: 2, mode: 'manual' } },
    })
    const before = useBonsaiStore.getState()
    const next = snapshot('a', 2)
    next.local!.worktrees = next.local!.worktrees.filter(tree => tree.id !== 'a-feature')
    next.worktree_state = {}
    next.processes = []
    applySnapshot(next)
    expect(useBonsaiStore.getState().collapsedBranchIds).toEqual(['a-other', 'b-other'])
    expect(useBonsaiStore.getState().nodePlacements).toEqual({ 'b-other': before.nodePlacements['b-other'] })
    expect(useBonsaiStore.getState().nodePlacements['b-other']).toBe(before.nodePlacements['b-other'])
  })
})
