import { ProviderBadge } from '../../components/ui/ProviderBadge'
import { useEffect, useRef, useState } from 'react'
import {
  DndContext,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
} from '@dnd-kit/core'
import {
  SortableContext,
  rectSortingStrategy,
  useSortable,
} from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import {
  ExternalLink,
  FileCode2,
  GripVertical,
  Maximize2,
  Minus,
  TerminalSquare,
  X,
} from 'lucide-react'
import { BonsaiSelect } from '../../components/ui/BonsaiSelect'
import { useBonsaiStore } from '../../stores/bonsai'
import { StatusDot } from '../branches/BranchesIsland'
import { useOpenRuntimeEntries, type RuntimeEntry } from './openRuntimeEntries'
import type { EditorPreference } from '../../types'
import { AgentTerminal } from './AgentTerminal'
import { ProcessTerminal } from './ProcessTerminal'

function SortableRuntimeTile({ runtime }: { runtime: RuntimeEntry }) {
  const [terminalActions, setTerminalActions] = useState<HTMLDivElement | null>(null)
  const active = useBonsaiStore((state) => state.dockRuntimeId === runtime.id)
  const closeRuntime = useBonsaiStore((state) => state.closeRuntime)
  const setDockRuntimeId = useBonsaiStore((state) => state.setDockRuntimeId)
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: runtime.id })
  const style = { transform: CSS.Transform.toString(transform), transition, opacity: isDragging ? 0.55 : 1 }

  return (
    <section
      ref={setNodeRef}
      style={style}
      onClick={() => setDockRuntimeId(runtime.id)}
      className={"runtime-tile flex h-full min-h-0 min-w-0 flex-col overflow-hidden " + (active ? "runtime-tile-active" : "")}
    >
      <div
        {...attributes}
        {...listeners}
        className="runtime-heading flex h-[26px] shrink-0 cursor-grab items-center gap-2 px-2 active:cursor-grabbing"
        title="Drag runtime card"
      >
        <GripVertical size={9} className="shrink-0 text-muted-2" />
        {runtime.type === 'agent' ? (
          <ProviderBadge provider={runtime.agent.provider} size={18} />
        ) : (
          <span className="grid h-[18px] w-[18px] shrink-0 place-items-center rounded-[5px] bg-panel-4 text-muted"><TerminalSquare size={10} /></span>
        )}
        <div className="flex min-w-0 flex-1 items-baseline gap-2">
          <span className="shrink-0 truncate text-[12px] font-semibold text-text">{runtime.type === 'agent' ? runtime.agent.name : runtime.process.name}</span>
          <span className="min-w-0 truncate font-mono text-[10px] text-muted-2">
            {runtime.type === 'agent'
              ? runtime.agent.profileName ?? (runtime.agent.model + ' · ' + runtime.agent.reasoningEffort + (runtime.agent.fastMode ? ' · Fast' : ''))
              : runtime.process.command}
          </span>
        </div>
        {runtime.type === 'agent' && <div ref={setTerminalActions} className="flex shrink-0 items-center gap-2" onPointerDown={event => event.stopPropagation()} onClick={event => event.stopPropagation()} />}
        <StatusDot status={runtime.type === 'agent' ? runtime.agent.state : runtime.process.status} />
        <button
          type="button"
          onPointerDown={(event) => event.stopPropagation()}
          onClick={(event) => {
            event.stopPropagation()
            closeRuntime(runtime.id)
          }}
          className="bonsai-focus icon-btn-20 shrink-0 hover:text-text"
          title="Close runtime card"
          aria-label="Close runtime card"
        >
          <X size={9} />
        </button>
      </div>
      <div className="min-h-0 flex-1 overflow-hidden">
        {runtime.type === 'agent' ? (
          <AgentTerminal agent={runtime.agent} actionsHost={terminalActions} />
        ) : (
          <ProcessTerminal process={runtime.process} />
        )}
      </div>
    </section>
  )
}

export function RuntimeWorkspace() {
  const hostRef = useRef<HTMLElement | null>(null)
  const [wideHeader, setWideHeader] = useState(false)
  const dockRuntimeId = useBonsaiStore((state) => state.dockRuntimeId)
  const openRuntimeIds = useBonsaiStore((state) => state.openRuntimeIds)
  const openRuntime = useBonsaiStore((state) => state.openRuntime)
  const focusRuntime = useBonsaiStore(state => state.focusRuntime)
  const reorderOpenRuntime = useBonsaiStore((state) => state.reorderOpenRuntime)
  const dockState = useBonsaiStore((state) => state.dockState)
  const setDockState = useBonsaiStore((state) => state.setDockState)
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }))
  const { worktree, available, openEntries } = useOpenRuntimeEntries()

  useEffect(() => {
    const host = hostRef.current
    if (!host) return
    const observer = new ResizeObserver(([entry]) => setWideHeader(entry.contentRect.width >= 690))
    observer.observe(host)
    return () => observer.disconnect()
  }, [])

  const onDragEnd = (event: DragEndEvent) => {
    const active = String(event.active.id)
    const over = event.over?.id ? String(event.over.id) : ''
    if (over) reorderOpenRuntime(active, over)
  }

  const runtimeOptions = available.map((runtime) => ({
    value: runtime.id,
    label: runtime.type === 'agent' ? runtime.agent.name : runtime.process.name,
    description: runtime.type === 'agent' ? runtime.agent.provider + ' · ' + (runtime.agent.profileName ?? runtime.agent.model) : runtime.process.command,
    meta: openRuntimeIds.includes(runtime.id) ? 'open' : undefined,
  }))

  return (
    <section ref={hostRef} className="island dock-pane flex h-full min-h-0 min-w-0 flex-col">
      <div className="dock-heading flex shrink-0 items-center gap-2 px-3">
        <TerminalSquare size={13} className="shrink-0 text-ok" />
        <span className="dock-title">Terminals</span>
        <span className="island-count">{openRuntimeIds.length}</span>
        {wideHeader ? (
          <>
            <div className="flex min-w-0 flex-1 items-center gap-1 overflow-hidden">
              {openEntries.map((runtime) => (
                <button
                  key={runtime.id}
                  type="button"
                  onClick={() => focusRuntime(runtime.id)}
                  className={
                    'bonsai-focus flex h-6 min-w-0 max-w-[170px] items-center gap-1.5 rounded-md px-2 text-left text-[12px] ' +
                    'border bg-bg ' + (dockRuntimeId === runtime.id ? 'border-accent/40 text-text' : 'border-border-subtle text-muted hover:border-border hover:text-text')
                  }
                >
                  {runtime.type === 'agent' ? (
                    <ProviderBadge provider={runtime.agent.provider} size={16} />
                  ) : <TerminalSquare size={12} className="shrink-0" />}
                  <span className="min-w-0 truncate">{runtime.type === 'agent' ? runtime.agent.name : runtime.process.name}</span>
                </button>
              ))}
            </div>
            <div className="w-[142px] shrink-0">
              <BonsaiSelect ariaLabel="Open runtime" compact searchable value="" onChange={openRuntime} placeholder="+ Open" options={runtimeOptions} />
            </div>
          </>
        ) : (
          <div className="min-w-0 flex-1">
            <BonsaiSelect ariaLabel="Open runtime" compact searchable value={dockRuntimeId} onChange={openRuntime} placeholder="Open agent / process…" options={runtimeOptions} />
          </div>
        )}

        <div className="ml-auto flex shrink-0 items-center gap-1">
          <span className="mx-0.5 h-4 w-px bg-border" />
          <button onClick={() => setDockState('collapsed')} className="bonsai-focus btn-ghost h-6 w-6 justify-center px-0" title="Minimize workspace"><Minus size={11} /></button>
          <button onClick={() => setDockState(dockState === 'maximized' ? 'normal' : 'maximized')} className="bonsai-focus btn-ghost h-6 w-6 justify-center px-0" title="Maximize workspace"><Maximize2 size={11} /></button>
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-hidden p-2">
        {openEntries.length ? (
          <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
            <SortableContext items={openEntries.map((item) => item.id)} strategy={rectSortingStrategy}>
              <div className="grid h-full min-h-0 auto-rows-fr grid-cols-[repeat(auto-fit,minmax(min(210px,100%),1fr))] gap-2">
                {openEntries.map((runtime) => <SortableRuntimeTile key={runtime.id} runtime={runtime} />)}
              </div>
            </SortableContext>
          </DndContext>
        ) : (
          <div className="grid h-full place-items-center rounded-md border border-dashed border-[rgb(var(--border))] text-[9px] text-[rgb(var(--muted-2))]">
            Open an agent terminal or managed process above. Generic shells are unavailable.
          </div>
        )}
      </div>
    </section>
  )
}

function EditorPreferenceDialog() {
  const open = useBonsaiStore((state) => state.editorPromptOpen)
  const path = useBonsaiStore((state) => state.pendingOpenFile)
  const setEditorPreference = useBonsaiStore((state) => state.setEditorPreference)
  const close = useBonsaiStore((state) => state.closeEditorPrompt)
  if (!open) return null

  const choices: Array<{ id: EditorPreference; label: string; description: string }> = [
    { id: 'vscode', label: 'Visual Studio Code', description: 'code --goto <file>' },
    { id: 'cursor', label: 'Cursor', description: 'cursor --goto <file>' },
    { id: 'zed', label: 'Zed', description: 'zed <file>' },
    { id: 'system', label: 'System default', description: 'Use the operating-system file handler' },
  ]

  return (
    <div className="fixed inset-0 z-[120] grid place-items-center bg-well/70 p-6 backdrop-blur-[2px]">
      <div className="w-full max-w-[430px] overflow-hidden rounded-xl border border-[rgb(var(--border-strong))] bg-[rgb(var(--panel))] shadow-2xl">
        <div className="flex h-11 items-center border-b border-[rgb(var(--border))] px-3">
          <FileCode2 size={13} className="mr-2 text-[rgb(var(--accent))]" />
          <div>
            <div className="text-[11px] font-semibold">Open files with…</div>
            <div className="max-w-[300px] truncate font-mono text-[8px] text-[rgb(var(--muted-2))]">{path}</div>
          </div>
          <button onClick={close} className="ml-auto grid h-6 w-6 place-items-center rounded text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]"><X size={11} /></button>
        </div>
        <div className="space-y-1 p-2">
          {choices.map((choice) => (
            <button key={choice.id} onClick={() => setEditorPreference(choice.id)} className="flex w-full items-center gap-3 rounded-lg border border-transparent px-3 py-2.5 text-left hover:border-[rgb(var(--border))] hover:bg-[rgb(var(--panel-2))]">
              <span className="grid h-8 w-8 place-items-center rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--bg))]"><ExternalLink size={12} /></span>
              <span className="min-w-0">
                <span className="block text-[10px] font-medium">{choice.label}</span>
                <span className="mt-0.5 block text-[8px] text-[rgb(var(--muted-2))]">{choice.description}</span>
              </span>
            </button>
          ))}
        </div>
        <div className="border-t border-[rgb(var(--border))] px-3 py-2 text-[8px] text-[rgb(var(--muted-2))]">This saves your editor preference. Opening an external editor is unavailable in the connected app.</div>
      </div>
    </div>
  )
}

export function BottomWorkspace() {
  return (
    <>
      <div className="bottom-workspace h-full min-h-0">
        <RuntimeWorkspace />
      </div>
      <EditorPreferenceDialog />
    </>
  )
}
