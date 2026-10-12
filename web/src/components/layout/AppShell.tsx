import { lazy, useEffect, useRef } from 'react'
import { OverlayBoundary } from '../../app/chunkRecovery'
import {
  Panel,
  PanelGroup,
  PanelResizeHandle,
  type ImperativePanelHandle,
} from 'react-resizable-panels'
import * as Tooltip from '@radix-ui/react-tooltip'
import { ChevronUp, TerminalSquare } from 'lucide-react'
import { BranchesIsland } from '../../features/branches/BranchesIsland'
import { Inspector } from '../../features/inspector/Inspector'
import { BottomWorkspace } from '../../features/terminal/BottomWorkspace'
import { useOpenRuntimeEntries } from '../../features/terminal/openRuntimeEntries'
import { useBonsaiStore } from '../../stores/bonsai'
import { TopBar } from './TopBar'
import { WorkspaceNotice } from './WorkspaceNotice'
import { WorkspaceVisibility } from '../ui/WorkspaceVisibility'
import { ProviderBadge } from '../ui/ProviderBadge'

const CommandPalette = lazy(() => import('../../features/command-palette/CommandPalette').then(module => ({ default: module.CommandPalette })))
const CreateWorktreeDialog = lazy(() => import('../../features/workspace/CreateWorktreeDialog').then(module => ({ default: module.CreateWorktreeDialog })))
const DeleteWorktreeDialog = lazy(() => import('../../features/workspace/DeleteWorktreeDialog').then(module => ({ default: module.DeleteWorktreeDialog })))
const EnvEditor = lazy(() => import('../../features/workspace/EnvEditor').then(module => ({ default: module.EnvEditor })))
const EditorPreferenceDialog = lazy(() => import('../../features/files/EditorPreferenceDialog').then(module => ({ default: module.EditorPreferenceDialog })))
const StartAgentDialog = lazy(() => import('../../features/workspace/StartAgentDialog').then(module => ({ default: module.StartAgentDialog })))
const StartProcessDialog = lazy(() => import('../../features/workspace/StartProcessDialog').then(module => ({ default: module.StartProcessDialog })))

function MainWorkspace({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-full min-h-0 overflow-hidden rounded-[14px]">
      <main className="min-w-0 flex-1">{children}</main>
    </div>
  )
}

function runtimeDot(entry: ReturnType<typeof useOpenRuntimeEntries>['openEntries'][number]) {
  if (entry.type === 'agent') return entry.agent.state === 'running' ? 'bg-accent-solid' : entry.agent.state === 'idle' ? 'bg-warn-solid' : 'bg-muted-2'
  return entry.process.status === 'healthy' ? 'bg-accent-solid' : entry.process.status === 'warning' ? 'bg-warn-solid' : 'bg-muted-2'
}

// Rendered only while the dock is collapsed so the open-runtime lookup does not re-render the shell otherwise.
function CollapsedTerminalsBar({ reopenRef }: { reopenRef: React.RefObject<HTMLButtonElement> }) {
  const { openEntries } = useOpenRuntimeEntries()
  const openRuntimeIds = useBonsaiStore((state) => state.openRuntimeIds)
  const focusRuntime = useBonsaiStore((state) => state.focusRuntime)
  const setDockState = useBonsaiStore((state) => state.setDockState)
  return (
    <div className="island mt-3 flex h-[34px] shrink-0 items-center gap-2 rounded-[12px] px-3">
      <TerminalSquare size={13} className="shrink-0 text-ok" />
      <span className="font-mono text-[10.5px] uppercase tracking-[.08em] text-muted">Terminals</span>
      <span className="island-count">{openRuntimeIds.length}</span>
      <div className="flex min-w-0 items-center gap-1 overflow-hidden">
        {openEntries.map((entry) => {
          const name = entry.type === 'agent' ? entry.agent.name : entry.process.name
          return (
            <button
              key={entry.id}
              type="button"
              onClick={() => { focusRuntime(entry.id); setDockState('normal') }}
              className="bonsai-focus flex h-6 min-w-0 max-w-[170px] items-center gap-1.5 rounded-md px-2 text-left text-[12px] text-text hover:bg-panel-2"
            >
              {entry.type === 'agent' ? <ProviderBadge provider={entry.agent.provider} size={16} /> : <TerminalSquare size={12} className="shrink-0 text-muted" />}
              <span className="min-w-0 truncate">{name}</span>
              <span className={'h-1.5 w-1.5 shrink-0 rounded-full ' + runtimeDot(entry)} />
            </button>
          )
        })}
      </div>
      <span className="flex-1" />
      <button ref={reopenRef} type="button" onClick={() => setDockState('normal')} className="bonsai-focus btn-ghost shrink-0 text-[11px]">
        <ChevronUp size={12} /> Open workspace
      </button>
    </div>
  )
}

export function AppShell({ children }: { children: React.ReactNode }) {
  const gitError = useBonsaiStore(state => state.gitError)
  const dockRef = useRef<ImperativePanelHandle>(null)
  const dockState = useBonsaiStore((state) => state.dockState)
  const setDockState = useBonsaiStore((state) => state.setDockState)
  const dockHeight = useBonsaiStore((state) => state.dockHeight)
  const setDockHeight = useBonsaiStore((state) => state.setDockHeight)
  const initialDockSize = useRef(dockState === 'collapsed' ? 0 : dockState === 'maximized' ? 68 : dockHeight).current
  const applyingLayout = useRef(false)
  const dockContentRef = useRef<HTMLDivElement>(null)
  const reopenRef = useRef<HTMLButtonElement>(null)
  const dragging = useRef(false)
  const normalHeight = useRef(dockHeight)

  useEffect(() => {
    const panel = dockRef.current
    if (!panel) return
    // Imperative resize callbacks must not overwrite the saved normal height.
    normalHeight.current = dockHeight
    applyingLayout.current = true
    try {
      if (dockState === 'collapsed') panel.collapse()
      else panel.resize(dockState === 'maximized' ? 68 : dockHeight)
    } finally {
      applyingLayout.current = false
    }
    if (dockState === 'collapsed' && dockContentRef.current?.contains(document.activeElement)) {
      reopenRef.current?.focus()
    }
  }, [dockHeight, dockState])


  return (
    <Tooltip.Provider delayDuration={250}>
      <div className="shell-ground relative h-full w-full overflow-hidden">
        <TopBar />
        <div className="pointer-events-none absolute inset-0">
          <aside className="desktop-rail pointer-events-auto absolute bottom-4 left-4 top-[68px] flex w-[400px] flex-col gap-3">
            <Inspector />
            <BranchesIsland />
          </aside>
          <div className="shell-main pointer-events-auto absolute bottom-4 left-[428px] right-4 top-[68px] flex flex-col">
            {gitError && <div role="status" className="island mb-3 shrink-0 px-3 py-2 text-sm text-warn">{gitError}</div>}
            <div className="min-h-0 flex-1">
              <PanelGroup id="workspace-layout" direction="vertical">
                <Panel id="main-workspace" order={1} minSize={24} defaultSize={100 - initialDockSize}>
                  <MainWorkspace>{children}</MainWorkspace>
                </Panel>
                <PanelResizeHandle
                  id="workspace-resize"
                  onDragging={active => {
                    dragging.current = active
                    if (!active) setDockHeight(normalHeight.current)
                  }}
                  disabled={dockState === 'collapsed'}
                  tabIndex={dockState === 'collapsed' ? -1 : 0}
                  style={{ display: dockState === 'collapsed' ? 'none' : undefined }}
                  className="group relative h-3 shrink-0 cursor-row-resize bg-transparent"
                >
                  <div className="absolute left-1/2 top-1/2 h-px w-10 -translate-x-1/2 -translate-y-1/2 bg-border-strong opacity-0 transition-opacity group-hover:opacity-100" />
                </PanelResizeHandle>
                <Panel
                  ref={dockRef}
                  id="bottom-workspace"
                  order={2}
                  collapsible
                  collapsedSize={0}
                  defaultSize={initialDockSize}
                  minSize={14}
                  maxSize={72}
                  onResize={(size) => {
                    if (applyingLayout.current) return
                    const currentState = useBonsaiStore.getState().dockState
                    if (size === 0) setDockState('collapsed')
                    else if (currentState === 'normal') {
                      normalHeight.current = size
                      if (!dragging.current) setDockHeight(size)
                    }
                  }}
                >
                  <div
                    ref={dockContentRef}
                    className="h-full min-h-0 overflow-hidden pt-3"
                    aria-hidden={dockState === 'collapsed'}
                    {...(dockState === 'collapsed' ? { inert: '' } : {})}
                    style={{ visibility: dockState === 'collapsed' ? 'hidden' : undefined }}
                  >
                    <WorkspaceVisibility.Provider value={dockState !== 'collapsed'}>
                      <BottomWorkspace />
                    </WorkspaceVisibility.Provider>
                  </div>
                </Panel>
              </PanelGroup>
            </div>
            {dockState === 'collapsed' && <CollapsedTerminalsBar reopenRef={reopenRef} />}
          </div>
        </div>

        <WorkspaceNotice />

        <ShellOverlays />
      </div>
    </Tooltip.Provider>
  )
}

// Keep shortcuts and open-state subscriptions eager, without re-rendering the shell.
function ShellOverlays() {
  const worktree = useBonsaiStore(state => state.worktreeDialogOpen)
  const deletion = useBonsaiStore(state => state.deleteWorktreeId)
  const agent = useBonsaiStore(state => state.startAgentDialogOpen)
  const process = useBonsaiStore(state => state.startProcessDialogOpen)
  const environment = useBonsaiStore(state => state.envEditorOpen)
  const editor = useBonsaiStore(state => state.editorPromptOpen)
  const palette = useBonsaiStore(state => state.paletteOpen)
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault()
        const state = useBonsaiStore.getState()
        state.setPaletteOpen(!state.paletteOpen)
      } else if (event.key === 'Escape') {
        useBonsaiStore.getState().setPaletteOpen(false)
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [])
  return <>
    {worktree && <OverlayBoundary loading="Loading worktree dialog…" onDismiss={() => useBonsaiStore.getState().setWorktreeDialogOpen(false)}><CreateWorktreeDialog /></OverlayBoundary>}
    {deletion && <OverlayBoundary loading="Loading delete dialog…" onDismiss={() => useBonsaiStore.getState().setDeleteWorktreeId('')}><DeleteWorktreeDialog /></OverlayBoundary>}
    {agent && <OverlayBoundary loading="Loading agent dialog…" onDismiss={() => useBonsaiStore.getState().setStartAgentDialogOpen(false)}><StartAgentDialog /></OverlayBoundary>}
    {process && <OverlayBoundary loading="Loading process dialog…" onDismiss={() => useBonsaiStore.getState().setStartProcessDialogOpen(false)}><StartProcessDialog /></OverlayBoundary>}
    {environment && <OverlayBoundary loading="Loading environment editor…" onDismiss={() => useBonsaiStore.getState().setEnvEditorOpen(false)}><EnvEditor /></OverlayBoundary>}
    {editor && <OverlayBoundary loading="Loading editor chooser…" onDismiss={() => useBonsaiStore.getState().closeEditorPrompt()}><EditorPreferenceDialog /></OverlayBoundary>}
    {palette && <OverlayBoundary loading="Loading commands…" onDismiss={() => useBonsaiStore.getState().setPaletteOpen(false)}><CommandPalette /></OverlayBoundary>}
  </>
}
