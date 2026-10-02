import { act } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'
import { __resetGitSyncForTests, loadPullRequest, startGitBackend } from './git'
import { useBonsaiStore } from '../stores/bonsai'

const { localFetch, openLocalEvents } = vi.hoisted(() => ({ localFetch: vi.fn(), openLocalEvents: vi.fn() }))
vi.mock('./local', () => ({ localFetch, openLocalEvents, invalidateLocalSession: vi.fn(), markLocalConnectionLost: vi.fn() }))
const response = (value: unknown) => new Response(JSON.stringify(value), { status: 200, headers: { 'Content-Type': 'application/json' } })
beforeEach(() => { __resetGitSyncForTests(); vi.clearAllMocks() })

it('shares concurrent PR detail requests and caches successful loads', async () => {
  localFetch.mockResolvedValueOnce(response({ number: 1, state: 'open', head_sha: 'sha', head: 'feature', base: 'main', updated_at: 'today' })).mockResolvedValueOnce(response([]))
  await Promise.all([loadPullRequest('repo:1'), loadPullRequest('repo:1')])
  await loadPullRequest('repo:1')
  expect(localFetch).toHaveBeenCalledTimes(2)
  localFetch.mockResolvedValueOnce(response({ number: 1, state: 'closed', head_sha: 'sha', head: 'feature', base: 'main' })).mockResolvedValueOnce(response([]))
  await loadPullRequest('repo:1', true)
  expect(localFetch).toHaveBeenCalledTimes(4)
})

it('retains provider IDs for same-name checks loaded with PR details', async () => {
  localFetch.mockResolvedValueOnce(response({ number: 7, state: 'open', head_sha: 'sha', head: 'feature', base: 'main' }))
    .mockResolvedValueOnce(response([
      { id: 101, name: 'verify', status: 'completed', conclusion: 'success' },
      { id: 102, name: 'verify', status: 'completed', conclusion: 'failure' },
    ]))
  await loadPullRequest('repo:7')
  expect(useBonsaiStore.getState().pullRequests.find(pr => pr.id === 'repo:7')?.checks).toEqual([
    { id: 101, name: 'verify', status: 'success' },
    { id: 102, name: 'verify', status: 'failed' },
  ])
})

it('reconnects the event stream and applies updates after bootstrap', async () => {
  vi.useFakeTimers()
  const sockets = [0, 1].map(() => ({ close: vi.fn(), onclose: null as null | ((event: { code: number }) => void), onerror: null }))
  let receive!: (event: any) => void
  openLocalEvents.mockImplementation(async callback => { receive = callback; return { socket: sockets[openLocalEvents.mock.calls.length - 1], epoch: 'epoch' } })
  const stop = startGitBackend()
  try {
    await act(async () => {})
    sockets[0].onclose?.({ code: 1006 })
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    expect(openLocalEvents).toHaveBeenCalledTimes(2)
    const repository = { id: 'stream', workspace_id: 'local', full_name: 'owner/repo' }
    receive({ type: 'catalog', projects: [repository] })
    receive({ type: 'project_snapshot', snapshot: { repository, epoch: 'epoch', sequence: 1, online: true, metadata: {}, local: { branches: [], worktrees: [] } } })
    receive({ type: 'bootstrap_complete' })
    receive({ type: 'project_update', snapshot: { repository, epoch: 'epoch', sequence: 2, online: true, metadata: {}, local: { branches: [{ name: 'live', remote: false }], worktrees: [] } } })
    expect(useBonsaiStore.getState().gitBranches.stream[0].name).toBe('live')
    stop()
    await vi.advanceTimersByTimeAsync(80_000)
    expect(openLocalEvents).toHaveBeenCalledTimes(2)
  } finally { stop(); vi.useRealTimers() }
})
