import { StrictMode } from 'react'
import { act, cleanup, render } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { RuntimeWorkspace } from './BottomWorkspace'
import { useBonsaiStore } from '../../stores/bonsai'
import { projects } from '../../test/fixtures/projects'
import { worktrees } from '../../test/fixtures/worktrees'
import type { Process } from '../../types'

const openRuntime = useBonsaiStore.getState().openRuntime
const process = (id: string, worktreeId = 'wt-web', projectId = 'bonsai'): Process => ({
  id, worktreeId, projectId, daemonId: 1, name: id, command: 'pnpm dev', status: 'healthy', lifecycleStatus: 'running',
})
describe('asynchronous runtime selection', () => {
  beforeEach(() => {
    vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} })
    useBonsaiStore.setState({ projects, worktrees, activeProjectId: 'bonsai', dockWorktreeId: 'wt-web', agents: [], processes: [], dockRuntimeId: '', openRuntimeIds: [], openRuntime: vi.fn(openRuntime) })
  })
  afterEach(() => { cleanup(); vi.unstubAllGlobals(); useBonsaiStore.setState({ openRuntime }) })

  it('opens the first asynchronously arriving process once and ignores equivalent updates', () => {
    render(<StrictMode><RuntimeWorkspace /></StrictMode>)
    expect(useBonsaiStore.getState().openRuntime).not.toHaveBeenCalled()
    act(() => useBonsaiStore.setState({ processes: [process('first')] }))
    expect(useBonsaiStore.getState()).toMatchObject({ dockRuntimeId: 'first', openRuntimeIds: ['first'], dockWorktreeId: 'wt-web' })
    for (let i = 0; i < 5; i++) act(() => useBonsaiStore.setState({ processes: [process('first')] }))
    expect(useBonsaiStore.getState().openRuntime).toHaveBeenCalledTimes(1)
  })

  it('keeps the user choice when new processes arrive and selects a surviving open runtime on removal', () => {
    render(<RuntimeWorkspace />)
    act(() => useBonsaiStore.setState({ processes: [process('first'), process('chosen')] }))
    act(() => useBonsaiStore.getState().openRuntime('chosen'))
    act(() => useBonsaiStore.setState({ processes: [process('first'), process('chosen'), process('new')] }))
    expect(useBonsaiStore.getState().dockRuntimeId).toBe('chosen')
    act(() => useBonsaiStore.setState({ processes: [process('first'), process('new')] }))
    expect(useBonsaiStore.getState().dockRuntimeId).toBe('first')
    act(() => useBonsaiStore.setState({ processes: [] }))
    act(() => useBonsaiStore.setState({ processes: [process('replacement')] }))
    expect(useBonsaiStore.getState().dockRuntimeId).toBe('replacement')
  })

  it('waits for a process in the new project without selecting an inactive project runtime', () => {
    const secondTree = { ...worktrees[0], id: 'second-tree', projectId: 'sprout-lab' }
    useBonsaiStore.setState({ worktrees: [...worktrees, secondTree], processes: [process('first')] })
    render(<RuntimeWorkspace />)
    act(() => useBonsaiStore.getState().setActiveProject('sprout-lab'))
    expect(useBonsaiStore.getState().dockRuntimeId).toBe('')
    act(() => useBonsaiStore.setState({ processes: [process('first'), process('second', 'second-tree', 'sprout-lab')] }))
    expect(useBonsaiStore.getState()).toMatchObject({ dockRuntimeId: 'second', dockWorktreeId: 'second-tree' })
  })

  it('opening an already selected process produces no store update', () => {
    useBonsaiStore.setState({ processes: [process('first', 'wt-daemon')] })
    openRuntime('first')
    expect(useBonsaiStore.getState().dockWorktreeId).toBe('wt-daemon')
    const notify = vi.fn()
    const unsubscribe = useBonsaiStore.subscribe(notify)
    openRuntime('first')
    openRuntime('missing')
    expect(notify).not.toHaveBeenCalled()
    unsubscribe()
  })
})
