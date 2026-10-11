import { StrictMode } from 'react'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { WorkspaceVisibility } from '../../components/ui/WorkspaceVisibility'
import { RuntimeWorkspace } from './BottomWorkspace'
import { useBonsaiStore } from '../../stores/bonsai'
import { projects } from '../../test/fixtures/projects'
import { worktrees } from '../../test/fixtures/worktrees'
import type { Process } from '../../types'
import { agents } from '../../mock/agents'

const attachments = vi.hoisted(() => ({ open: new Set<string>(), detach: vi.fn() }))
vi.mock('./ProcessTerminal', async () => {
  const { useEffect } = await import('react')
  return { ProcessTerminal: ({ process }: { process: Process }) => {
    useEffect(() => {
      attachments.open.add(process.id)
      return () => { attachments.open.delete(process.id); attachments.detach(process.id) }
    }, [process.id])
    return <div data-testid={process.id}>{process.lifecycleStatus}</div>
  } }
})
const process = (id: string, worktreeId = 'wt-web', projectId = 'bonsai'): Process => ({
  id, worktreeId, projectId, daemonId: 1, name: id, command: 'pnpm dev', status: 'healthy', lifecycleStatus: 'running',
})
describe('explicit runtime views', () => {
  beforeEach(() => {
    vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} })
    attachments.open.clear(); attachments.detach.mockClear()
    useBonsaiStore.setState({ ...useBonsaiStore.getInitialState(), projects, worktrees, activeProjectId: 'bonsai', dockWorktreeId: 'wt-web' }, true)
  })
  afterEach(() => { cleanup(); vi.unstubAllGlobals() })

  it('shows provider marks in the terminal picker without changing option names', async () => {
    useBonsaiStore.setState({ agents: ['Claude', 'Codex', 'Antigravity', 'Gemini'].map((provider, index) => ({
      ...agents[index], id: `logo-${index}`, provider: provider as typeof agents[number]['provider'],
      projectId: 'bonsai', worktreeId: 'wt-web', name: `${provider} session`, profileName: 'Work', presentation: 'canvas',
    })) })
    render(<RuntimeWorkspace />)
    fireEvent.click(screen.getByRole('button', { name: 'Open runtime' }))
    for (const provider of ['Claude', 'Codex', 'Antigravity', 'Gemini']) {
      const option = await screen.findByRole('option', { name: `${provider} session ${provider} · Work` })
      expect(option.querySelector('svg')).toHaveAttribute('data-provider-logo', provider.toLowerCase())
    }
  })

  it('stays empty with three discovered runtimes under StrictMode and repeated status updates', () => {
    render(<StrictMode><RuntimeWorkspace /></StrictMode>)
    for (let i = 0; i < 5; i++) act(() => useBonsaiStore.setState({ processes: ['first', 'second', 'third'].map(id => process(id)) }))
    expect(useBonsaiStore.getState()).toMatchObject({ dockRuntimeId: '', openRuntimeIds: [] })
    expect(attachments.open.size).toBe(0)
    expect(screen.getByText(/Open an agent terminal or managed process/)).toBeVisible()
    expect(screen.getByRole('button', { name: 'Open runtime' })).toBeVisible()
  })

  it('closing the only open view releases its attachment and leaves two closed views closed', async () => {
    useBonsaiStore.setState({ processes: ['first', 'second', 'third'].map(id => process(id)) })
    useBonsaiStore.getState().openRuntime('second')
    render(<StrictMode><RuntimeWorkspace /></StrictMode>)
    await waitFor(() => expect([...attachments.open]).toEqual(['second']))
    fireEvent.click(screen.getByRole('button', { name: 'Close runtime card' }))
    expect(useBonsaiStore.getState()).toMatchObject({ openRuntimeIds: [], dockRuntimeId: '' })
    expect(attachments.open.size).toBe(0)
    expect(attachments.detach).toHaveBeenCalledWith('second')
    act(() => useBonsaiStore.setState({ processes: ['first', 'second', 'third'].map(id => process(id)) }))
    expect(attachments.open.size).toBe(0)
    expect(useBonsaiStore.getState().processes.every(process => process.lifecycleStatus === 'running')).toBe(true)
    act(() => useBonsaiStore.getState().openRuntime('second'))
    await waitFor(() => expect([...attachments.open]).toEqual(['second']))
  })

  it('focuses only a surviving already-open view in the same worktree', async () => {
    useBonsaiStore.setState({ processes: [process('other', 'wt-daemon'), process('first'), process('second'), process('closed')] })
    useBonsaiStore.getState().openRuntime('other')
    useBonsaiStore.getState().openRuntime('first')
    useBonsaiStore.getState().openRuntime('second')
    render(<RuntimeWorkspace />)
    act(() => useBonsaiStore.getState().closeRuntime('second'))
    expect(useBonsaiStore.getState().dockRuntimeId).toBe('first')
    act(() => useBonsaiStore.getState().closeRuntime('first'))
    expect(useBonsaiStore.getState().dockRuntimeId).toBe('')
    expect(attachments.open.size).toBe(0)
    act(() => useBonsaiStore.getState().setDockWorktreeId('wt-daemon'))
    await waitFor(() => expect([...attachments.open]).toEqual(['other']))
  })

  it('does not open arrivals in a new project or worktree', () => {
    const secondTree = { ...worktrees[0], id: 'second-tree', projectId: 'sprout-lab' }
    useBonsaiStore.setState({ worktrees: [...worktrees, secondTree], processes: [process('first')] })
    render(<RuntimeWorkspace />)
    act(() => useBonsaiStore.getState().setActiveProject('sprout-lab'))
    act(() => useBonsaiStore.setState({ processes: [process('first'), process('second', 'second-tree', 'sprout-lab')] }))
    expect(useBonsaiStore.getState()).toMatchObject({ dockRuntimeId: '', dockWorktreeId: 'second-tree' })
    expect(attachments.open.size).toBe(0)
  })

  it('restores saved views when delayed canonical entities arrive without opening discovered siblings', async () => {
    useBonsaiStore.setState({ terminalViewPreferences: { bonsai: { lastWorktreeId: 'wt-web', reopening: 'restore', worktrees: { 'wt-web': { open: [{ kind: 'process', id: 'saved' }], active: { kind: 'process', id: 'saved' } } } } } })
    useBonsaiStore.getState().setDockWorktreeId('wt-web')
    render(<StrictMode><RuntimeWorkspace /></StrictMode>)
    expect(attachments.open.size).toBe(0)
    act(() => { useBonsaiStore.setState({ processes: [process('discovered'), process('saved')], processAuthorityReady: { bonsai: true }, syncFreshness: { bonsai: { processes: { state: 'ready' } } } }); useBonsaiStore.getState().setDockWorktreeId('wt-web') })
    await waitFor(() => expect([...attachments.open]).toEqual(['saved']))
  })

  it('defers hidden terminals and keeps their attachment when collapsed after activation', async () => {
    useBonsaiStore.setState({ processes: [process('first')] })
    useBonsaiStore.getState().openRuntime('first')
    const view = render(<WorkspaceVisibility.Provider value={false}><RuntimeWorkspace /></WorkspaceVisibility.Provider>)
    expect(attachments.open.size).toBe(0)
    view.rerender(<WorkspaceVisibility.Provider value={true}><RuntimeWorkspace /></WorkspaceVisibility.Provider>)
    await waitFor(() => expect([...attachments.open]).toEqual(['first']))
    attachments.detach.mockClear()
    view.rerender(<WorkspaceVisibility.Provider value={false}><RuntimeWorkspace /></WorkspaceVisibility.Provider>)
    expect([...attachments.open]).toEqual(['first'])
    expect(attachments.detach).not.toHaveBeenCalled()
  })

  it('opening and focusing an already selected process are idempotent', () => {
    useBonsaiStore.setState({ processes: [process('first', 'wt-daemon')] })
    useBonsaiStore.getState().openRuntime('first')
    const notify = vi.fn(), unsubscribe = useBonsaiStore.subscribe(notify)
    useBonsaiStore.getState().openRuntime('first')
    useBonsaiStore.getState().openRuntime('missing')
    useBonsaiStore.getState().focusRuntime('first')
    useBonsaiStore.getState().focusRuntime('missing')
    expect(notify).not.toHaveBeenCalled()
    unsubscribe()
  })
})
