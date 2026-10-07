import { beforeEach, expect, it, vi } from 'vitest'
import { applyProcessSummary, startProcess } from './processes'
import { useBonsaiStore } from '../stores/bonsai'
import { localFetch } from './localClient'
import { reconcileSnapshotEntities } from './snapshotReconciliation'
import type { ProcessSummary, Snapshot } from './git'
vi.mock('./localClient', () => ({ localFetch: vi.fn(), LOCAL_API_HTTP: 'http://localhost' }))
const summary: ProcessSummary = { id: 'other:7', project_id: 'other', daemon_id: 7, worktree_id: 'target', label: 'same command', command: 'pnpm run tauri dev', status: 'failed', revision: 4, exit_code: 2, policy: { mode: 'on-failure', max_restarts: 1 } }
beforeEach(() => useBonsaiStore.setState(useBonsaiStore.getInitialState(), true))
it('opens a launch in the active owning scope and selects its process node', () => {
  useBonsaiStore.setState({ activeProjectId: 'other', processes: [], dockState: 'collapsed' })
  applyProcessSummary(summary, true)
  expect(useBonsaiStore.getState()).toMatchObject({ activeProjectId: 'other', dockWorktreeId: 'target', dockRuntimeId: summary.id, dockState: 'normal', openRuntimeIds: [summary.id], selection: { type: 'process', id: summary.id }, processes: [{ id: summary.id, exitCode: 2, lifecycleStatus: 'failed' }] })
  applyProcessSummary({ ...summary, revision: 3, status: 'running' })
  expect(useBonsaiStore.getState().processes[0].lifecycleStatus).toBe('failed')
  applyProcessSummary({ ...summary, id: 'other:8', daemon_id: 8 })
  expect(useBonsaiStore.getState().processes).toHaveLength(2)
})
it('omits an inherited policy and preserves explicit zero retries', async () => {
  vi.mocked(localFetch).mockResolvedValue({ ok: true, json: async () => summary } as Response)
  await startProcess('other', 'target', 'command', { args: ['space argument'] })
  expect(JSON.parse(String(vi.mocked(localFetch).mock.calls.at(-1)?.[1]?.body))).not.toHaveProperty('policy')
  await startProcess('other', 'target', 'command', {}, { mode: 'always', max_restarts: 0 })
  expect(JSON.parse(String(vi.mocked(localFetch).mock.calls.at(-1)?.[1]?.body))).toHaveProperty('policy', { mode: 'always', max_restarts: 0 })
})
it('protects a launch from older snapshots and preserves newer streamed lifecycle versions', () => {
  useBonsaiStore.setState({ processes: [], projects: [], worktrees: [], agents: [], pullRequests: [] })
  applyProcessSummary(summary)
  const snapshot = { repository: { id: 'other', full_name: 'owner/repo', default_branch: 'main' }, online: true, metadata: {}, process_visibility: { cutoffs: {}, deleted: {} }, processes: [] } as unknown as Snapshot
  const missing = reconcileSnapshotEntities(snapshot, useBonsaiStore.getState())
  expect(missing.processes ?? useBonsaiStore.getState().processes).toHaveLength(1)
  const older = reconcileSnapshotEntities({ ...snapshot, processes: [{ ...summary, revision: 2, status: 'running' }] }, useBonsaiStore.getState())
  expect(older.processes?.[0]).toMatchObject({ revision: 4, lifecycleStatus: 'failed', pendingSnapshot: false })
})
