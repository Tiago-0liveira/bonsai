import { useEffect, useRef } from 'react'
import {
  Panel,
  PanelGroup,
  PanelResizeHandle,
  type ImperativePanelHandle,
} from 'react-resizable-panels'
import * as Tooltip from '@radix-ui/react-tooltip'
import { PanelBottomOpen } from 'lucide-react'
import { CommandPalette } from '../../features/command-palette/CommandPalette'
import { Inspector } from '../../features/inspector/Inspector'
import { BottomWorkspace } from '../../features/terminal/BottomWorkspace'
import { CreateWorktreeDialog } from '../../features/workspace/CreateWorktreeDialog'
import { DeleteWorktreeDialog } from '../../features/workspace/DeleteWorktreeDialog'
import { EnvEditor } from '../../features/workspace/EnvEditor'
import { StartAgentDialog } from '../../features/workspace/StartAgentDialog'
import { StartProcessDialog } from '../../features/workspace/StartProcessDialog'
import { useBonsaiStore } from '../../stores/bonsai'
import { relayLoginURL } from '../../api/relayClient'
import { TopBar } from './TopBar'
import { WorkspaceNotice } from './WorkspaceNotice'
import { WorkspaceVisibility } from '../ui/WorkspaceVisibility'

function MainWorkspace({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-full min-h-0 overflow-hidden rounded-[14px]">
      <main className="min-w-0 flex-1">{children}</main>
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
            {/* Branches island slot (P3-7). */}
          </aside>
          <div className="shell-main pointer-events-auto absolute bottom-4 left-[428px] right-4 top-[68px] flex flex-col">
            {gitError && <div role="status" className="island mb-3 shrink-0 px-3 py-2 text-sm text-warn">{gitError}{gitError.includes('Sign in with GitHub') && <> <a href={relayLoginURL()} className="underline">Sign in with GitHub</a></>}</div>}
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
          </div>
        </div>

        {dockState === 'collapsed' && (
          <button
            ref={reopenRef}
            type="button"
            onClick={() => setDockState('normal')}
            className="bonsai-focus absolute bottom-3 left-1/2 z-40 flex -translate-x-1/2 items-center gap-1.5 rounded-full border border-[rgb(var(--border-strong))] bg-[rgb(var(--panel-2))] px-3 py-1.5 text-[9px] text-[rgb(var(--muted))] shadow-xl hover:bg-[rgb(var(--panel-3))] hover:text-[rgb(var(--text))]"
          >
            <PanelBottomOpen size={11} /> Open workspace
          </button>
        )}

        <WorkspaceNotice />

        <CreateWorktreeDialog />
        <DeleteWorktreeDialog />
        <StartAgentDialog />
        <StartProcessDialog />
        <EnvEditor />
        <CommandPalette />
      </div>
    </Tooltip.Provider>
  )
}
