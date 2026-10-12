import { ProviderBadge } from '../../components/ui/ProviderBadge'
import { lazy, useContext, useEffect, useRef, useState } from 'react'
import { FeatureBoundary } from '../../app/chunkRecovery'
import { WorkspaceVisibility } from '../../components/ui/WorkspaceVisibility'
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

const AgentTerminal = lazy(() => import('./AgentTerminal').then(module => ({ default: module.AgentTerminal })))
const ProcessTerminal = lazy(() => import('./ProcessTerminal').then(module => ({ default: module.ProcessTerminal })))

function SortableRuntimeTile({ runtime }: { runtime: RuntimeEntry }) {
  const visible = useContext(WorkspaceVisibility)
  const [activated, setActivated] = useState(visible)
  useEffect(() => { if (visible) setActivated(true) }, [visible])
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
        {(visible || activated) && (
          <FeatureBoundary loading="Loading terminal…">
            {runtime.type === 'agent' ? (
              <AgentTerminal agent={runtime.agent} actionsHost={terminalActions} />
            ) : (
              <ProcessTerminal process={runtime.process} />
            )}
          </FeatureBoundary>
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
    icon: runtime.type === 'agent' ? <ProviderBadge provider={runtime.agent.provider} size={16} /> : undefined,
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

export function BottomWorkspace() {
  return (
    <>
      <div className="bottom-workspace h-full min-h-0">
        <RuntimeWorkspace />
      </div>
    </>
  )
}
