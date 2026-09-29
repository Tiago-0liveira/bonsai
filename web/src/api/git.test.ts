import { beforeEach, describe, expect, it } from 'vitest'
import { useBonsaiStore } from '../stores/bonsai'
import { applySnapshot, reconcileCatalog, projectForGitHubRepository, type Snapshot } from './git'
import { fileTree, flattenFiles } from './files'

describe('canonical Git snapshots', () => {
  beforeEach(() => useBonsaiStore.setState({ projects: [{ id: 'repo', workspaceId: 'workspace', name: 'repo', repository: 'owner/repo', description: '', health: 'idle', defaultBranch: 'main', worktreeIds: [], openPrCount: 0 }], worktrees: [], pullRequests: [], gitBranches: {}, gitOnline: {}, agents: [] }))
  it('keeps fetched refs distinct from newer GitHub branch heads', () => {
    const snapshot: Snapshot = { repository: { id: 'repo', workspace_id: 'workspace', full_name: 'owner/repo', default_branch: 'main' }, online: true, sequence: 1, metadata: {}, local: { branches: [{ name: 'origin/main', remote: true, local_remote_ref_sha: 'fetched' }], worktrees: [{ id: 'wt', repository_id: 'repo', branch: 'main', main: true, local_head_sha: 'local' }] }, remote: { repository: { id: 123, full_name: 'owner/repo', default_branch: 'main' }, branches: [{ name: 'main', remote_head_sha: 'newer-remote' }], pull_requests: [] } }
    applySnapshot(snapshot)
    expect(useBonsaiStore.getState().gitBranches.repo[0]).toMatchObject({ local_remote_ref_sha: 'fetched', remote_head_sha: 'newer-remote' })
    expect(useBonsaiStore.getState().worktrees[0].dirtyFiles).toBe(0)
    applySnapshot({ ...snapshot, online: false })
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
