import { beforeEach, expect, it } from 'vitest'
import { useBonsaiStore } from '../stores/bonsai'
import { __resetGitSyncForTests, applySnapshot, type Snapshot } from './git'

beforeEach(() => {
  __resetGitSyncForTests()
  useBonsaiStore.setState({
    projects: [{ id: 'repo', workspaceId: 'workspace', name: 'repo', repository: 'owner/repo', description: '', health: 'idle', defaultBranch: 'main', worktreeIds: [], openPrCount: 0 }],
    worktrees: [], processes: [], agents: [], pullRequests: [], gitBranches: {}, gitOnline: {}, syncFreshness: {}, gitRevision: 0,
  })
})

function snapshot(): Snapshot {
  return {
    epoch: 'epoch', sequence: 1, online: true, metadata: {},
    repository: { id: 'repo', workspace_id: 'workspace', full_name: 'owner/repo', default_branch: 'main' },
    local: {
      branches: [],
      worktrees: [{ id: 'wt', repository_id: 'repo', branch: 'main', main: true, local_head_sha: 'sha', status: {
        ahead: 0, behind: 0, staged: 0, modified: 1, untracked: 0,
        files: [{ path: 'one.ts', status: '.M' }], git_state: '', dirty: true,
      } }],
    },
    worktree_state: { wt: { ci: { status: 'running', checked_sha: 'sha', checks: [], freshness: { state: 'ready' } } } },
  }
}

it('updates CI and processes without invalidating local file data', () => {
  const first = snapshot()
  applySnapshot(first)
  const revision = useBonsaiStore.getState().gitRevision
  applySnapshot({ ...first, sequence: 2, worktree_state: { wt: { ci: { status: 'passed', checked_sha: 'sha', checks: [], freshness: { state: 'ready' } } } } })
  expect(useBonsaiStore.getState().worktrees[0].ciStatus).toBe('passed')
  expect(useBonsaiStore.getState().gitRevision).toBe(revision)

  applySnapshot({ ...first, sequence: 3, processes: [{ id: 'repo:1', daemon_id: 1, project_id: 'repo', worktree_id: 'wt', label: 'server', command: 'serve', status: 'running' }] })
  expect(useBonsaiStore.getState().processes).toHaveLength(1)
  expect(useBonsaiStore.getState().gitRevision).toBe(revision)
})

it('invalidates files when changed paths change with identical dirty counts', () => {
  const first = snapshot()
  applySnapshot(first)
  const revision = useBonsaiStore.getState().gitRevision
  const local = structuredClone(first.local!)
  local.worktrees[0].status!.files = [{ path: 'two.ts', status: '.M' }]
  expect(applySnapshot({ ...first, sequence: 2, local })).toBe(true)
  expect(useBonsaiStore.getState().worktrees[0].dirtyFiles).toBe(1)
  expect(useBonsaiStore.getState().gitRevision).toBe(revision + 1)
  applySnapshot({ ...first, sequence: 3, local })
  expect(useBonsaiStore.getState().gitRevision).toBe(revision + 1)
})
