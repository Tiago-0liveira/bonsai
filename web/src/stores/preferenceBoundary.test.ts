import { createStore, type StoreApi } from 'zustand'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useBonsaiStore, type BonsaiState } from './bonsai'
import { createPreferenceBoundary, PREFERENCE_WRITE_DELAY } from './preferenceBoundary'
import { createPanelPreferences, PANEL_STORAGE_KEY } from './panelPreferences'
import { useWorkspaceStorageStatus, workspaceStorage, WORKSPACE_STORAGE_KEY } from './workspacePersistence'
import { worktrees } from '../test/fixtures/worktrees'

describe('dedicated preference writer', () => {
  let store: StoreApi<BonsaiState>
  let writer: ReturnType<typeof createPreferenceBoundary>
  beforeEach(() => {
    vi.useFakeTimers()
    localStorage.clear()
    useWorkspaceStorageStatus.setState({ error: '' })
    store = createStore<BonsaiState>(() => ({ ...useBonsaiStore.getInitialState(), worktrees }))
    writer = createPreferenceBoundary(store)
  })
  afterEach(() => { writer.dispose(); vi.restoreAllMocks(); vi.useRealTimers() })

  it('does not invoke serialization/storage for process or CI collections', () => {
    const write = vi.spyOn(workspaceStorage, 'setItem')
    const disk = vi.spyOn(Storage.prototype, 'setItem')
    store.setState({ processes: [{ id: 'real', daemonId: 1, projectId: 'bonsai', worktreeId: 'wt-web', name: 'serve', command: 'serve', status: 'healthy', lifecycleStatus: 'running' }] })
    store.setState({ worktrees: worktrees.map(tree => ({ ...tree, ciStatus: 'passed' })) })
    store.setState({ notice: 'status changed' })
    vi.runAllTimers()
    expect(write).not.toHaveBeenCalled()
    expect(disk).not.toHaveBeenCalled()
  })

  it('skips unchanged values and equal serialized preferences even when a new object is passed', () => {
    const disk = vi.spyOn(Storage.prototype, 'setItem')
    store.setState({ dockHeight: store.getState().dockHeight, viewport: { ...store.getState().viewport } })
    vi.runAllTimers()
    expect(disk).not.toHaveBeenCalled()
  })

  it('coalesces movement changes and saves the latest value once after interaction settles', () => {
    const disk = vi.spyOn(Storage.prototype, 'setItem')
    for (const dockHeight of [31, 32, 38, 45]) { store.setState({ dockHeight }); vi.advanceTimersByTime(20) }
    expect(disk).not.toHaveBeenCalled()
    vi.advanceTimersByTime(PREFERENCE_WRITE_DELAY)
    expect(disk).toHaveBeenCalledTimes(1)
    expect(JSON.parse(localStorage.getItem(WORKSPACE_STORAGE_KEY)!).state.dockHeight).toBe(45)
    writer.rehydrate()
    expect(store.getState().dockHeight).toBe(45)
    expect(disk).toHaveBeenCalledTimes(1)
  })

  it.each(['pagehide', 'beforeunload', 'teardown'])('flushes pending final preferences on %s', event => {
    const stop = writer.attachLifecycle()
    store.setState({ dockHeight: 46, viewport: { x: 1, y: 2, zoom: 0.6 } })
    if (event === 'teardown') stop()
    else { window.dispatchEvent(new Event(event)); stop() }
    expect(JSON.parse(localStorage.getItem(WORKSPACE_STORAGE_KEY)!).state).toMatchObject({ dockHeight: 46, viewport: { x: 1, y: 2, zoom: 0.6 } })
  })

  it('saves board edits immediately along with any pending layout, without a stale later write', () => {
    const disk = vi.spyOn(Storage.prototype, 'setItem')
    store.setState({ dockHeight: 40 })
    store.setState({ boardItems: store.getState().boardItems.map((item, index) => index === 0 ? { ...item, title: 'My edited card' } : item) })
    expect(disk).toHaveBeenCalledTimes(1)
    const saved = JSON.parse(localStorage.getItem(WORKSPACE_STORAGE_KEY)!).state
    expect(saved.dockHeight).toBe(40)
    expect(saved.boardItems[0].title).toBe('My edited card')
    vi.runAllTimers()
    expect(disk).toHaveBeenCalledTimes(1)
  })

  it('deduplicates direct retry writes against the actual serialized storage record', () => {
    const disk = vi.spyOn(Storage.prototype, 'setItem')
    const value = { state: { dockHeight: 42 }, version: 1 }
    workspaceStorage.setItem(WORKSPACE_STORAGE_KEY, value)
    workspaceStorage.setItem(WORKSPACE_STORAGE_KEY, value)
    expect(disk).toHaveBeenCalledTimes(1)
  })

  it('coalesces horizontal panel layouts and flushes the latest layout before the library autosave timer', () => {
    const panels = createPanelPreferences()
    const disk = vi.spyOn(Storage.prototype, 'setItem')
    panels.remember(['branches', 'runtime', 'files', 'prs'], [14, 50, 18, 18])
    panels.remember(['branches', 'runtime', 'files', 'prs'], [18, 46, 18, 18])
    expect(disk).not.toHaveBeenCalled()
    panels.flush()
    expect(disk).toHaveBeenCalledTimes(1)
    expect(JSON.parse(localStorage.getItem(PANEL_STORAGE_KEY)!)['branches,files,prs,runtime'].layout).toEqual([18, 46, 18, 18])
    panels.remember(['branches', 'runtime', 'files', 'prs'], [18, 46, 18, 18])
    vi.runAllTimers()
    expect(disk).toHaveBeenCalledTimes(1)
  })
})
