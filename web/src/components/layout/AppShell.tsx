import { useEffect, useRef } from 'react'
import {
  Panel,
  PanelGroup,
  PanelResizeHandle,
  type ImperativePanelHandle,
} from 'react-resizable-panels'
import * as Tooltip from '@radix-ui/react-tooltip'
import { CheckCircle2 } from 'lucide-react'
import { CommandPalette } from '../../features/command-palette/CommandPalette'
import { Inspector } from '../../features/inspector/Inspector'
import { BottomWorkspace } from '../../features/terminal/BottomWorkspace'
import { useBonsaiStore } from '../../stores/bonsai'
import { ProjectSidebar } from './ProjectSidebar'
import { TopBar } from './TopBar'

export function AppShell({ children }: { children: React.ReactNode }) {
  const dockRef = useRef<ImperativePanelHandle>(null)
  const dockState = useBonsaiStore((state) => state.dockState)
  const setDockState = useBonsaiStore((state) => state.setDockState)
  const notice = useBonsaiStore((state) => state.notice)
  const setNotice = useBonsaiStore((state) => state.setNotice)

  useEffect(() => {
    const panel = dockRef.current
    if (!panel) return
    if (dockState === 'collapsed') panel.collapse()
    else {
      panel.expand()
      panel.resize(dockState === 'maximized' ? 68 : 30)
    }
  }, [dockState])

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
          <PanelGroup direction="vertical">
            <Panel minSize={24} defaultSize={70}>
              <div className="flex h-full min-h-0">
                <ProjectSidebar />
                <main className="min-w-0 flex-1">{children}</main>
                <Inspector />
              </div>
            </Panel>
            <PanelResizeHandle className="group relative h-1.5 shrink-0 cursor-row-resize border-y border-[rgb(var(--border))] bg-[rgb(var(--bg))]">
              <div className="absolute left-1/2 top-1/2 h-px w-10 -translate-x-1/2 -translate-y-1/2 bg-[rgb(var(--border-strong))] opacity-0 transition-opacity group-hover:opacity-100" />
            </PanelResizeHandle>
            <Panel
              ref={dockRef}
              defaultSize={30}
              minSize={8}
              maxSize={72}
              collapsible
              collapsedSize={5}
              onCollapse={() => setDockState('collapsed')}
            >
              <BottomWorkspace />
            </Panel>
          </PanelGroup>
        </div>

        {notice && (
          <div className="pointer-events-none absolute bottom-3 right-3 z-50 flex items-center gap-2 rounded-md border border-[rgb(var(--border-strong))] bg-[rgb(var(--panel-2))] px-3 py-2 text-[11px] shadow-xl">
            <CheckCircle2 size={13} className="text-[rgb(var(--green))]" />
            {notice}
          </div>
        )}

        <CommandPalette />
      </div>
    </Tooltip.Provider>
  )
}
