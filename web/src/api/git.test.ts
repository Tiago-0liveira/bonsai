import { beforeEach, describe, expect, it } from 'vitest'
import { useBonsaiStore } from '../stores/bonsai'
import { __resetGitSyncForTests, applySnapshot, reconcileCatalog, projectForGitHubRepository, type Snapshot } from './git'
import { fileTree, flattenFiles } from './files'

describe('canonical Git snapshots', () => {
  beforeEach(() => {
    __resetGitSyncForTests()
    useBonsaiStore.setState({ projects: [{ id: 'repo', workspaceId: 'workspace', name: 'repo', repository: 'owner/repo', description: '', health: 'idle', defaultBranch: 'main', worktreeIds: [], openPrCount: 0 }], worktrees: [], pullRequests: [], gitBranches: {}, gitOnline: {}, syncFreshness: {}, processes: [], agents: [] })
  })
  it('keeps fetched refs distinct from newer GitHub branch heads', () => {
    const snapshot: Snapshot = { repository: { id: 'repo', workspace_id: 'workspace', full_name: 'owner/repo', default_branch: 'main' }, online: true, sequence: 1, metadata: {}, local: { branches: [{ name: 'origin/main', remote: true, local_remote_ref_sha: 'fetched' }], worktrees: [{ id: 'wt', repository_id: 'repo', branch: 'main', main: true, local_head_sha: 'local' }] }, remote: { repository: { id: 123, full_name: 'owner/repo', default_branch: 'main' }, branches: [{ name: 'main', remote_head_sha: 'newer-remote' }], pull_requests: [] } }
    applySnapshot(snapshot)
    expect(useBonsaiStore.getState().gitBranches.repo[0]).toMatchObject({ local_remote_ref_sha: 'fetched', remote_head_sha: 'newer-remote' })
    expect(useBonsaiStore.getState().worktrees[0].dirtyFiles).toBe(0)
    applySnapshot({ ...snapshot, sequence: 2, online: false })
    expect(useBonsaiStore.getState().worktrees[0].status).toBe('idle')
  })
  it('constructs recursive file trees without fabricated contents', () => {
    const tree = fileTree([{ path: 'src/nested/a.ts', status: '.M' }, { path: 'new.txt', status: '??' }])
    expect(flattenFiles(tree).filter(f => f.type === 'file').map(f => [f.path, f.gitStatus, f.content])).toEqual([['src/nested/a.ts', 'modified', undefined], ['new.txt', 'untracked', undefined]])
  })
})

describe('catalog reconciliation', () => {
  it('migrates local project selection and preserves worktree IDs and layout', () => {
    useBonsaiStore.setState({ projects: [], worktrees: [], activeProjectId: 'local', selection: { type: 'worktree', id: 'stable-tree' }, dockWorktreeId: 'stable-tree', nodePlacements: { local: { x: 10, y: 20, mode: 'manual' }, 'stable-tree': { x: 30, y: 40, mode: 'manual' } } })
    reconcileCatalog([{ id: 'project-v1-launch', launch: true, workspace_id: 'local', full_name: 'launch', default_branch: 'main' }])
    expect(useBonsaiStore.getState().activeProjectId).toBe('project-v1-launch')
    expect(useBonsaiStore.getState().selection).toEqual({ type: 'worktree', id: 'stable-tree' })
    expect(useBonsaiStore.getState().dockWorktreeId).toBe('stable-tree')
    expect(useBonsaiStore.getState().nodePlacements['project-v1-launch']).toMatchObject({ x: 10, y: 20 })
    expect(useBonsaiStore.getState().nodePlacements['stable-tree']).toMatchObject({ x: 30, y: 40 })
  })
  it('removes absent projects and repairs active selection', () => {
    useBonsaiStore.setState({ projects: [], activeProjectId: 'removed', selection: { type: 'project', id: 'removed' }, worktrees: [], gitBranches: { removed: [] }, gitOnline: { removed: true } })
    reconcileCatalog([{ id: 'remaining', workspace_id: 'local', full_name: 'remaining', default_branch: 'main' }])
    expect(useBonsaiStore.getState().activeProjectId).toBe('remaining')
    expect(useBonsaiStore.getState().selection).toEqual({ type: 'project', id: 'remaining' })
    expect(useBonsaiStore.getState().gitBranches.removed).toBeUndefined()
  })
  it('maps one provider repository to multiple local clones and discards stale snapshots', () => {
    const repos = ['one', 'two'].map(id => ({ id, workspace_id: 'local', full_name: 'owner/shared', default_branch: 'main' }))
    reconcileCatalog(repos)
    const snapshot = (id: string): Snapshot => ({ repository: repos.find(p => p.id === id)!, sequence: 1, online: true, metadata: {}, local: { branches: [], worktrees: [] }, remote: { repository: { id: 999, full_name: 'owner/shared', default_branch: 'main' }, branches: [], pull_requests: [] } })
    applySnapshot(snapshot('one')); applySnapshot(snapshot('two'))
    expect(projectForGitHubRepository(999).sort()).toEqual(['one', 'two'])
    reconcileCatalog([repos[1]])
    applySnapshot(snapshot('one'))
    expect(projectForGitHubRepository(999)).toEqual(['two'])
    expect(useBonsaiStore.getState().projects.map(p => p.id)).toEqual(['two'])
  })
})


describe('synchronized snapshot ordering', () => {
  beforeEach(() => {
    __resetGitSyncForTests()
    useBonsaiStore.setState({
      projects: [{ id: 'repo', workspaceId: 'workspace', name: 'repo', repository: 'owner/repo', description: '', health: 'idle', defaultBranch: 'main', worktreeIds: [], openPrCount: 0 }],
      worktrees: [],
      processes: [],
      pullRequests: [],
      gitBranches: {},
      gitOnline: {},
      syncFreshness: {},
      agents: [],
    })
  })

  const repository = { id: 'repo', workspace_id: 'workspace', full_name: 'owner/repo', default_branch: 'main' }

  it('discards an older projection that resolves after a newer sequence', () => {
    const newer: Snapshot = {
      epoch: 'epoch-a',
      repository,
      sequence: 2,
      online: true,
      metadata: {},
      local: { branches: [], worktrees: [{ id: 'wt', repository_id: 'repo', branch: 'newer', main: true, local_head_sha: 'two' }] },
    }
    const older: Snapshot = {
      ...newer,
      sequence: 1,
      local: { branches: [], worktrees: [{ id: 'wt', repository_id: 'repo', branch: 'older', main: true, local_head_sha: 'one' }] },
    }
    expect(applySnapshot(newer)).toBe(true)
    expect(applySnapshot(older)).toBe(false)
    expect(useBonsaiStore.getState().worktrees[0].branch).toBe('newer')
  })

  it('uses backend PR association, SHA-specific CI, partial status errors, and live processes', () => {
    const pr = {
      number: 42,
      title: 'Feature',
      body: '',
      state: 'open',
      draft: false,
      head: 'feature',
      head_repository: 'owner/repo',
      base: 'main',
      head_sha: 'remote-feature',
      author: 'dev',
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    }
    const snapshot: Snapshot = {
      epoch: 'epoch-a',
      repository,
      sequence: 3,
      online: true,
      metadata: {},
      freshness: {
        local: { state: 'ready', updated_at: '2026-01-01T00:00:00Z' },
        processes: { state: 'ready', updated_at: '2026-01-01T00:00:00Z' },
        provider: { state: 'ready', updated_at: '2026-01-01T00:00:00Z' },
      },
      local: {
        branches: [],
        worktrees: [{
          id: 'wt',
          repository_id: 'repo',
          branch: 'feature',
          main: false,
          local_head_sha: 'local-feature',
          status_error: { code: 'status_unavailable', message: 'worktree disappeared' },
          status: {
            ahead: 7,
            behind: 4,
            staged: 0,
            modified: 0,
            untracked: 0,
            files: [],
            git_state: 'normal',
            dirty: false,
            upstream: 'origin/feature',
            divergence_available: false,
          },
        }],
      },
      remote: {
        repository: { id: 123, full_name: 'owner/repo', default_branch: 'main' },
        branches: [],
        pull_requests: [pr],
      },
      worktree_state: {
        wt: {
          pull_request: pr,
          ci: {
            status: 'none',
            checked_sha: 'remote-feature',
            checks: [],
            freshness: { state: 'ready', updated_at: '2026-01-01T00:00:00Z' },
          },
        },
      },
      processes: [{
        id: 'repo:7',
        daemon_id: 7,
        project_id: 'repo',
        worktree_id: 'wt',
        label: 'dev server',
        command: 'pnpm dev',
        status: 'orphan',
        pid: 321,
        expected_port: 5173,
      }],
    }
    expect(applySnapshot(snapshot)).toBe(true)
    const state = useBonsaiStore.getState()
    expect(state.worktrees[0]).toMatchObject({
      prNumber: 42,
      ciStatus: 'none',
      checkedSha: 'remote-feature',
      divergenceAvailable: false,
      ahead: 0,
      behind: 0,
      gitStatusError: 'worktree disappeared',
    })
    expect(state.processes[0]).toMatchObject({
      id: 'repo:7',
      daemonId: 7,
      worktreeId: 'wt',
      lifecycleStatus: 'orphan',
      status: 'warning',
      port: 5173,
    })
  })

  it('does not rewrite Zustand or increment gitRevision for an identical newer snapshot', () => {
    const snapshot: Snapshot = {
      epoch: 'epoch-a',
      repository,
      sequence: 1,
      online: true,
      metadata: {},
      freshness: { local: { state: 'ready', updated_at: '2026-01-01T00:00:00Z' } },
      local: { branches: [], worktrees: [{ id: 'wt', repository_id: 'repo', branch: 'main', main: true, local_head_sha: 'one' }] },
    }
    expect(applySnapshot(snapshot)).toBe(true)
    const revision = useBonsaiStore.getState().gitRevision
    expect(applySnapshot({
      ...snapshot,
      sequence: 2,
      freshness: { local: { state: 'ready', updated_at: '2026-01-02T00:00:00Z' } },
    })).toBe(false)
    expect(useBonsaiStore.getState().gitRevision).toBe(revision)
  })

  it('does not associate a same-named PR when the backend left the worktree unassociated', () => {
    const snapshot: Snapshot = {
      epoch: 'epoch-a',
      repository,
      sequence: 1,
      online: true,
      metadata: {},
      local: { branches: [], worktrees: [{ id: 'wt', repository_id: 'repo', branch: 'feature', main: false, local_head_sha: 'local' }] },
      remote: {
        repository: { id: 123, full_name: 'owner/repo', default_branch: 'main' },
        branches: [],
        pull_requests: [{
          number: 9,
          title: 'Fork PR',
          body: '',
          state: 'open',
          draft: false,
          head: 'feature',
          head_repository: 'fork/repo',
          base: 'main',
          head_sha: 'fork-head',
          author: 'forker',
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
        }],
      },
      worktree_state: {
        wt: { ci: { status: 'unknown', checks: [], freshness: { state: 'unavailable' } } },
      },
    }
    applySnapshot(snapshot)
    expect(useBonsaiStore.getState().worktrees[0].prNumber).toBeUndefined()
  })
})
