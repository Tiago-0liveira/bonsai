import { beforeEach, describe, expect, it } from 'vitest'
import { useBonsaiStore } from '../stores/bonsai'
import { applySnapshot, type Snapshot } from './git'
import { fileTree, flattenFiles } from './files'

describe('canonical Git snapshots', () => {
  beforeEach(() => useBonsaiStore.setState({ projects: [{ id: 'repo', workspaceId: 'workspace', name: 'repo', repository: 'owner/repo', description: '', health: 'idle', defaultBranch: 'main', worktreeIds: [], openPrCount: 0 }], worktrees: [], pullRequests: [], gitBranches: {}, gitOnline: {}, agents: [] }))
  it('keeps fetched refs distinct from newer GitHub branch heads', () => {
    const snapshot: Snapshot = { repository: { id: 'repo', workspace_id: 'workspace', full_name: 'owner/repo', default_branch: 'main' }, online: true, sequence: 1, metadata: {}, local: { branches: [{ name: 'origin/main', remote: true, local_remote_ref_sha: 'fetched' }], worktrees: [{ id: 'wt', repository_id: 'repo', branch: 'main', main: true, local_head_sha: 'local' }] }, remote: { repository: { id: 'repo', workspace_id: 'workspace', full_name: 'owner/repo', default_branch: 'main' }, branches: [{ name: 'main', remote_head_sha: 'newer-remote' }], pull_requests: [] } }
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
