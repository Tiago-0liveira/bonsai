import { StrictMode, useEffect, useState } from 'react'
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AppShell } from './AppShell'
import { useBonsaiStore } from '../../stores/bonsai'
import { BonsaiSelect } from '../ui/BonsaiSelect'

const mounts = vi.hoisted(() => ({ main: 0, dock: 0, canvas: 0 }))
vi.mock('./TopBar', () => ({ TopBar: () => null }))
vi.mock('../../features/inspector/Inspector', () => ({ Inspector: () => null }))
vi.mock('../../features/command-palette/CommandPalette', () => ({ CommandPalette: () => null }))
vi.mock('../../features/workspace/CreateWorktreeDialog', () => ({ CreateWorktreeDialog: () => null }))
vi.mock('../../features/workspace/DeleteWorktreeDialog', () => ({ DeleteWorktreeDialog: () => null }))
vi.mock('../../features/workspace/EnvEditor', () => ({ EnvEditor: () => null }))
vi.mock('../../features/workspace/StartAgentDialog', () => ({ StartAgentDialog: () => null }))
vi.mock('../../features/workspace/WorkspacePage', () => ({ WorkspacePage: ({ route }: { route: string | null }) => {
  useEffect(() => { mounts.canvas++ }, [])
  return <div data-testid="canvas-layer" data-route={route ?? ''} {...(route ? {} : { inert: '' })} />
} }))
vi.mock('../../features/terminal/BottomWorkspace', () => ({ BottomWorkspace: () => {
  useEffect(() => { mounts.dock++ }, [])
  return <Draft label="Dock draft" />
} }))

function Draft({ label }: { label: string }) {
  const [value, setValue] = useState('')
  return <><input aria-label={label} value={value} onChange={event => setValue(event.target.value)} />
    <BonsaiSelect ariaLabel={label + ' select'} value="a" onChange={() => undefined} options={[{ value: 'a', label: 'Choice' }]} /></>
}
function MainProbe() {
  useEffect(() => { mounts.main++ }, [])
  return <Draft label="Main draft" />
}

describe('workspace panel lifecycle', () => {
  beforeEach(() => {
    mounts.main = mounts.dock = mounts.canvas = 0
    useBonsaiStore.setState({ dockState: 'normal', dockHeight: 37, gitError: '', notice: '' })
  })
  afterEach(cleanup)

  it.each(['normal', 'collapsed', 'maximized'] as const)('preserves mounts, drafts and panels when starting %s in Strict Mode', initialState => {
    useBonsaiStore.setState({ dockState: initialState })
    const { container } = render(<StrictMode><AppShell><MainProbe /></AppShell></StrictMode>)
    const baseline = { ...mounts }
    const mainPanel = container.querySelector('[data-panel-id="main-workspace"]')
    const dockPanel = container.querySelector('[data-panel-id="bottom-workspace"]')
    const mainInput = screen.getByLabelText('Main draft')
    const dockInput = screen.getByLabelText('Dock draft')
    expect(dockPanel).toHaveAttribute('data-panel-size', initialState === 'collapsed' ? '0.0' : initialState === 'maximized' ? '68.0' : '37.0')
    // Open before entering drafts, including when restoring a collapsed session.
    if (initialState === 'collapsed') fireEvent.click(screen.getByRole('button', { name: 'Open workspace' }))
    fireEvent.change(mainInput, { target: { value: 'unsaved main' } })
    fireEvent.change(dockInput, { target: { value: 'unsaved dock' } })

    for (const dockState of ['maximized', 'collapsed', 'normal', 'collapsed', 'normal'] as const) {
      act(() => useBonsaiStore.getState().setDockState(dockState))
      expect(mounts).toEqual(baseline)
      expect(container.querySelector('[data-panel-id="main-workspace"]')).toBe(mainPanel)
      expect(container.querySelector('[data-panel-id="bottom-workspace"]')).toBe(dockPanel)
      expect(mainInput).toHaveValue('unsaved main')
      expect(dockInput).toHaveValue('unsaved dock')
      expect(useBonsaiStore.getState().dockHeight).toBe(37)
      expect(dockPanel).toHaveAttribute('data-panel-size', dockState === 'collapsed' ? '0.0' : dockState === 'maximized' ? '68.0' : '37.0')
    }
  })

  it('makes hidden dock content inert, closes its portal, and moves focus to the reopen button', () => {
    const { container } = render(<AppShell><MainProbe /></AppShell>)
    fireEvent.click(screen.getByLabelText('Dock draft select'))
    expect(screen.getByRole('listbox')).toBeInTheDocument()
    screen.getByLabelText('Dock draft').focus()
    act(() => useBonsaiStore.getState().setDockState('collapsed'))
    expect(container.querySelector('[data-panel-id="bottom-workspace"] [inert]')).toHaveAttribute('aria-hidden', 'true')
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Open workspace' })).toHaveFocus()
    fireEvent.click(screen.getByRole('button', { name: 'Open workspace' }))
    expect(container.querySelector('[inert]')).toBeNull()
  })

  it('ignores equal, floating point noise, and invalid dock height updates', () => {
    const notify = vi.fn()
    const unsubscribe = useBonsaiStore.subscribe(notify)
    for (const height of [37, 37.001, NaN, Infinity]) useBonsaiStore.getState().setDockHeight(height)
    expect(notify).not.toHaveBeenCalled()
    useBonsaiStore.getState().setDockHeight(43)
    expect(notify).toHaveBeenCalledTimes(1)
    expect(useBonsaiStore.getState().dockHeight).toBe(43)
    unsubscribe()
  })

  it('renders notices without updating the directly instantiated workspace content', () => {
    const renders = vi.fn()
    function Content() { renders(); return <span>Main content</span> }
    render(<AppShell><Content /></AppShell>)
    const baseline = renders.mock.calls.length
    act(() => useBonsaiStore.getState().setNotice('Managed process updated'))
    expect(screen.getByRole('status')).toHaveTextContent('Managed process updated')
    expect(renders).toHaveBeenCalledTimes(baseline)
  })
})

describe('canvas layer', () => {
  beforeEach(() => {
    mounts.canvas = 0
    useBonsaiStore.setState({ dockState: 'normal', dockHeight: 37, gitError: '', notice: '' })
  })
  afterEach(cleanup)

  it('lets canvas routes see through the frame to the canvas behind the shell', async () => {
    const { container } = render(<AppShell canvasRoute="canvas"><span /></AppShell>)
    const layer = await screen.findByTestId('canvas-layer')
    expect(layer).not.toHaveAttribute('inert')
    expect(container.querySelector('[data-canvas-frame]')).toHaveClass('pointer-events-none')
    // The layer renders before the shell islands so it paints behind them.
    expect(container.firstElementChild?.querySelector('[data-testid="canvas-layer"]')).toBe(layer)
    expect(layer.compareDocumentPosition(container.querySelector('.desktop-rail')!) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('keeps the canvas mounted and inert behind other pages', async () => {
    const { container, rerender } = render(<AppShell canvasRoute="canvas"><span /></AppShell>)
    await screen.findByTestId('canvas-layer')
    rerender(<AppShell canvasRoute={null}><span>GitHub page</span></AppShell>)
    expect(screen.getByTestId('canvas-layer')).toHaveAttribute('inert')
    expect(container.querySelector('[data-canvas-frame]')).toHaveClass('pointer-events-auto')
    rerender(<AppShell canvasRoute="worktrees"><span /></AppShell>)
    expect(screen.getByTestId('canvas-layer')).toHaveAttribute('data-route', 'worktrees')
    expect(mounts.canvas).toBe(1)
  })

  it('does not load the canvas for a page opened directly', () => {
    render(<AppShell canvasRoute={null}><span>Settings page</span></AppShell>)
    expect(screen.queryByTestId('canvas-layer')).toBeNull()
  })
})
