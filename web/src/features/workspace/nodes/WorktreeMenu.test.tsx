import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import * as ContextMenu from '@radix-ui/react-context-menu'
import { WorktreeMenuItems } from './WorktreeMenu'
import { MenuContent } from '../../../components/ui/ContextMenu'
import { ShellRouteProvider } from '../../../app/shellRoute'
import { useBonsaiStore } from '../../../stores/bonsai'
import { projects } from '../../../test/fixtures/projects'
import { worktrees } from '../../../test/fixtures/worktrees'

const { localFetch } = vi.hoisted(() => ({ localFetch: vi.fn() }))
vi.mock('../../../api/local', () => ({ localFetch, openLocalEvents: vi.fn(), sendEventFocus: vi.fn(), invalidateLocalSession: vi.fn(), markLocalConnectionLost: vi.fn() }))
const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } })
const settings = { environment: { id: 'wsl:host:Ubuntu', label: 'WSL (Ubuntu)' }, revision: 0, editors: [], preferred: '', saved_preferred: '', file_manager: { label: 'Explorer' } }
const showPullRequest = vi.fn()

function openMenu(worktreeId: string) {
  cleanup()
  render(<ShellRouteProvider value={{ page: 'canvas', showCanvas: vi.fn(), showPullRequest }}>
    <ContextMenu.Root>
      <ContextMenu.Trigger>node</ContextMenu.Trigger>
      <MenuContent><WorktreeMenuItems worktreeId={worktreeId} /></MenuContent>
    </ContextMenu.Root>
  </ShellRouteProvider>)
  fireEvent.contextMenu(screen.getByText('node'))
}
function openSub(name: string) {
  fireEvent.keyDown(screen.getByRole('menuitem', { name }), { key: 'ArrowRight' })
}
const item = (name: string) => screen.getByRole('menuitem', { name, exact: true } as never)
const calls = () => localFetch.mock.calls.map(([path, init]) => [path, init?.body ? JSON.parse(init.body) : undefined])

describe('worktree context menu', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    localFetch.mockImplementation(async (path: string) => path === '/api/settings/editors' ? json(settings) : json({ result: { state: 'completed' } }))
    useBonsaiStore.setState({ projects, activeProjectId: 'bonsai', worktrees, editorSettings: null, editorSettingsLoading: false, notice: '' })
  })
  afterEach(cleanup)

  it('names the environment file manager and reveals the worktree there', async () => {
    openMenu('wt-web')
    expect(await screen.findByRole('menuitem', { name: 'Reveal in Explorer' })).toBeEnabled()
    fireEvent.click(item('Reveal in Explorer'))
    await waitFor(() => expect(calls()).toContainEqual(['/api/worktrees/wt-web/open-file-manager', { environment_id: 'wsl:host:Ubuntu' }]))
  })

  it('runs Git operations on the right-clicked worktree, not the dock worktree', async () => {
    useBonsaiStore.setState({ dockWorktreeId: 'wt-main' })
    openMenu('wt-web')
    openSub('Git')
    expect(await screen.findByRole('menuitem', { name: 'Rebase onto main' })).toBeEnabled()
    expect(screen.queryByRole('menuitem', { name: /Continue/ })).toBeNull()
    fireEvent.click(item('Pull'))
    await waitFor(() => expect(calls()).toContainEqual(['/api/worktrees/wt-web/pull', {}]))
  })

  it('fetches the project rather than a worktree route', async () => {
    openMenu('wt-web')
    openSub('Git')
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Fetch' }))
    await waitFor(() => expect(calls()).toContainEqual(['/api/projects/bonsai/fetch', {}]))
  })

  it('offers continue and abort while a rebase is in progress, and blocks new operations', async () => {
    useBonsaiStore.setState({ worktrees: worktrees.map(tree => tree.id === 'wt-web' ? { ...tree, gitState: 'rebase' } : tree) })
    openMenu('wt-web')
    openSub('Git')
    expect(await screen.findByRole('menuitem', { name: 'Pull Rebase in progress' })).toHaveAttribute('aria-disabled', 'true')
    fireEvent.click(item('Abort rebase'))
    await waitFor(() => expect(calls()).toContainEqual(['/api/worktrees/wt-web/operations/abort', { operation_id: 'recover' }]))
  })

  it('opens, shows and copies a linked pull request', async () => {
    const open = vi.spyOn(window, 'open').mockReturnValue(null)
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    openMenu('wt-web')
    openSub('GitHub')
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Open PR on GitHub #24' }))
    expect(open).toHaveBeenCalledWith('https://github.com/Tiago-0liveira/bonsai/pull/24', '_blank', 'noopener,noreferrer')
    expect(screen.queryByRole('menuitem', { name: 'Create pull request' })).toBeNull()

    openMenu('wt-web')
    openSub('GitHub')
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Show in GitHub tab' }))
    expect(showPullRequest).toHaveBeenCalledWith('bonsai:24')

    openMenu('wt-web')
    openSub('GitHub')
    await act(async () => { fireEvent.click(await screen.findByRole('menuitem', { name: 'Copy PR link' })) })
    expect(writeText).toHaveBeenCalledWith('https://github.com/Tiago-0liveira/bonsai/pull/24')
    expect(useBonsaiStore.getState().notice).toBe('Copied pull request link')
    open.mockRestore()
  })

  it('disables pull request actions without a PR and offers to create one', async () => {
    const open = vi.spyOn(window, 'open').mockReturnValue(null)
    useBonsaiStore.setState({ worktrees: worktrees.map(tree => tree.id === 'wt-web' ? { ...tree, prNumber: undefined, branch: 'feat/new thing' } : tree) })
    openMenu('wt-web')
    openSub('GitHub')
    expect(await screen.findByRole('menuitem', { name: 'Open PR on GitHub No PR linked' })).toHaveAttribute('aria-disabled', 'true')
    for (const name of ['Show in GitHub tab', 'Copy PR link', 'Open checks']) expect(item(name)).toHaveAttribute('aria-disabled', 'true')
    fireEvent.click(item('Create pull request'))
    expect(open).toHaveBeenCalledWith('https://github.com/Tiago-0liveira/bonsai/compare/main...feat/new%20thing?expand=1', '_blank', 'noopener,noreferrer')
    open.mockRestore()
  })
})
