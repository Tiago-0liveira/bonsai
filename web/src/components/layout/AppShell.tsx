import { useEffect, useRef } from 'react'
import {
  Panel,
  PanelGroup,
  PanelResizeHandle,
  type ImperativePanelHandle,
} from 'react-resizable-panels'
import * as Tooltip from '@radix-ui/react-tooltip'
import { CheckCircle2, PanelBottomOpen } from 'lucide-react'
import { CommandPalette } from '../../features/command-palette/CommandPalette'
import { Inspector } from '../../features/inspector/Inspector'
import { BottomWorkspace } from '../../features/terminal/BottomWorkspace'
import { CreateWorktreeDialog } from '../../features/workspace/CreateWorktreeDialog'
import { EnvEditor } from '../../features/workspace/EnvEditor'
import { StartAgentDialog } from '../../features/workspace/StartAgentDialog'
import { useBonsaiStore } from '../../stores/bonsai'
import { TopBar } from './TopBar'

function MainWorkspace({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-full min-h-0">
      <main className="min-w-0 flex-1">{children}</main>
      <Inspector />
    </div>
  )
}

export function AppShell({ children }: { children: React.ReactNode }) {
  const dockRef = useRef<ImperativePanelHandle>(null)
  const dockState = useBonsaiStore((state) => state.dockState)
  const setDockState = useBonsaiStore((state) => state.setDockState)
  const dockHeight = useBonsaiStore((state) => state.dockHeight)
  const setDockHeight = useBonsaiStore((state) => state.setDockHeight)
  const notice = useBonsaiStore((state) => state.notice)
  const setNotice = useBonsaiStore((state) => state.setNotice)

  useEffect(() => {
    if (dockState === 'collapsed') return
    const panel = dockRef.current
    if (!panel) return
    panel.resize(dockState === 'maximized' ? 68 : dockHeight)
  }, [dockHeight, dockState])

  useEffect(() => {
    if (!notice) return
    const timer = window.setTimeout(() => setNotice(''), 2600)
    return () => window.clearTimeout(timer)
  }, [notice, setNotice])

  return (
    <Tooltip.Provider delayDuration={250}>
      <div className="relative flex h-full w-full flex-col overflow-hidden bg-[rgb(var(--bg))]">
        <TopBar />
        <div className="min-h-0 flex-1">
          {dockState === 'collapsed' ? (
            <MainWorkspace>{children}</MainWorkspace>
          ) : (
            <PanelGroup direction="vertical">
              <Panel minSize={24} defaultSize={100 - dockHeight}>
                <MainWorkspace>{children}</MainWorkspace>
              </Panel>
              <PanelResizeHandle className="group relative h-1.5 shrink-0 cursor-row-resize border-y border-[rgb(var(--border))] bg-[rgb(var(--bg))]">
                <div className="absolute left-1/2 top-1/2 h-px w-10 -translate-x-1/2 -translate-y-1/2 bg-[rgb(var(--border-strong))] opacity-0 transition-opacity group-hover:opacity-100" />
              </PanelResizeHandle>
              <Panel
                ref={dockRef}
                defaultSize={dockHeight}
                minSize={14}
                maxSize={72}
                onResize={(size) => {
                  if (dockState === 'normal') setDockHeight(size)
                }}
              >
                <BottomWorkspace />
              </Panel>
            </PanelGroup>
          )}
        </div>

        {dockState === 'collapsed' && (
          <button
            type="button"
            onClick={() => setDockState('normal')}
            className="bonsai-focus absolute bottom-3 left-1/2 z-40 flex -translate-x-1/2 items-center gap-1.5 rounded-full border border-[rgb(var(--border-strong))] bg-[rgb(var(--panel-2))] px-3 py-1.5 text-[9px] text-[rgb(var(--muted))] shadow-xl hover:bg-[rgb(var(--panel-3))] hover:text-[rgb(var(--text))]"
          >
            <PanelBottomOpen size={11} /> Open workspace
          </button>
        )}

        {notice && (
          <div className="pointer-events-none absolute bottom-3 right-3 z-50 flex items-center gap-2 rounded-md border border-[rgb(var(--border-strong))] bg-[rgb(var(--panel-2))] px-3 py-2 text-[11px] shadow-xl">
            <CheckCircle2 size={13} className="text-[rgb(var(--green))]" />
            {notice}
          </div>
        )}

        <CreateWorktreeDialog />
        <StartAgentDialog />
        <EnvEditor />
        <CommandPalette />
      </div>
    </Tooltip.Provider>
  )
}
