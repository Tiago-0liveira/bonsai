import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useBonsaiStore } from '../stores/bonsai'
import { flattenFiles, useFileContent, useFiles, useLocalDiff } from './files'

const { localFetch } = vi.hoisted(() => ({ localFetch: vi.fn() }))
vi.mock('./local', () => ({ localFetch }))

function response(value: unknown) {
  return new Response(JSON.stringify(value), { headers: { 'Content-Type': 'application/json' } })
}
function deferredResponse() {
  let resolve!: (value: Response) => void
  const promise = new Promise<Response>(done => { resolve = done })
  return { promise, resolve }
}
async function finishRefresh() {
  await act(async () => { await vi.advanceTimersByTimeAsync(100) })
}

beforeEach(() => {
  vi.useFakeTimers()
  localFetch.mockReset()
  useBonsaiStore.setState({ dockWorktreeId: 'first', gitRevision: 0, notice: '' })
})
afterEach(() => { vi.useRealTimers() })

describe('background file refreshes', () => {
  it('keeps the tree mounted through refreshes and failures, and clears it for another worktree', async () => {
    localFetch.mockResolvedValueOnce(response([{ path: 'src/main.ts', status: '' }]))
    const { result } = renderHook(() => useFiles())
    await finishRefresh()
    const previous = result.current
    const pending = deferredResponse()
    localFetch.mockReturnValueOnce(pending.promise)
    act(() => useBonsaiStore.setState({ gitRevision: 1 }))
    expect(result.current).toBe(previous)
    await finishRefresh()
    expect(result.current).toBe(previous)
    await act(async () => { pending.resolve(response([{ path: 'src/main.ts', status: '.M' }])) })
    expect(flattenFiles(result.current).at(-1)?.gitStatus).toBe('modified')

    const refreshed = result.current
    localFetch.mockRejectedValueOnce(new Error('temporary failure'))
    act(() => useBonsaiStore.setState({ gitRevision: 2 }))
    await finishRefresh()
    expect(result.current).toBe(refreshed)
    expect(useBonsaiStore.getState().notice).toBe('temporary failure')

    localFetch.mockResolvedValueOnce(response([{ path: 'other.ts', status: '' }]))
    act(() => useBonsaiStore.setState({ dockWorktreeId: 'second' }))
    expect(result.current).toEqual([])
    await finishRefresh()
    expect(result.current[0].path).toBe('other.ts')
  })

  it('keeps content during refresh and ignores a response for a previously selected file', async () => {
    localFetch.mockResolvedValueOnce(response({ content: 'old content', binary: false }))
    const { result, rerender } = renderHook(({ path }) => useFileContent(path), { initialProps: { path: 'one.ts' } })
    await finishRefresh()
    const pending = deferredResponse()
    localFetch.mockReturnValueOnce(pending.promise)
    act(() => useBonsaiStore.setState({ gitRevision: 1 }))
    expect(result.current).toBe('old content')
    await act(async () => { pending.resolve(response({ content: 'new content', binary: false })) })
    expect(result.current).toBe('new content')

    const obsolete = deferredResponse()
    localFetch.mockReturnValueOnce(obsolete.promise)
    act(() => useBonsaiStore.setState({ gitRevision: 2 }))
    localFetch.mockResolvedValueOnce(response({ content: 'second file', binary: false }))
    rerender({ path: 'two.ts' })
    expect(result.current).toBe('')
    await finishRefresh()
    await act(async () => { obsolete.resolve(response({ content: 'obsolete content', binary: false })) })
    expect(result.current).toBe('second file')

    const nextWorktree = deferredResponse()
    localFetch.mockReturnValueOnce(nextWorktree.promise)
    act(() => useBonsaiStore.setState({ dockWorktreeId: 'second' }))
    expect(result.current).toBe('')
  })

  it('keeps the diff visible until its replacement loads and hides it on a worktree switch', async () => {
    localFetch.mockResolvedValueOnce(response({ patch: 'old diff' }))
    const { result, rerender } = renderHook(({ id }) => useLocalDiff(id), { initialProps: { id: 'first' } })
    await finishRefresh()
    const pending = deferredResponse()
    localFetch.mockReturnValueOnce(pending.promise)
    act(() => useBonsaiStore.setState({ gitRevision: 1 }))
    await finishRefresh()
    expect(result.current).toBe('old diff')
    await act(async () => { pending.resolve(response({ patch: 'new diff' })) })
    expect(result.current).toBe('new diff')
    rerender({ id: 'second' })
    expect(result.current).toBe('')
  })
})
