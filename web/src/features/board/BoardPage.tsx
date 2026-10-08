import { useMemo, useState } from 'react'
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  closestCenter,
  useDraggable,
  useDroppable,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragStartEvent,
} from '@dnd-kit/core'
import { CSS } from '@dnd-kit/utilities'
import {
  ArrowDown,
  ArrowUp,
  Bug,
  CircleDot,
  GripVertical,
  Lightbulb,
  ListTodo,
  Plus,
  Settings2,
  Sparkles,
  Tag,
  Trash2,
  TriangleAlert,
  X,
  type LucideIcon,
} from 'lucide-react'
import { BonsaiSelect } from '../../components/ui/BonsaiSelect'
import { useBonsaiStore } from '../../stores/bonsai'
import type { BoardItem, BoardList, BoardStatus, TagColor } from '../../types'
import { tagPalette } from '../workspace/tagStyles'

const colors: TagColor[] = ['purple', 'blue', 'green', 'orange', 'red', 'cyan', 'pink']

function iconForKind(kind: string): LucideIcon {
  const normalized = kind.toLowerCase()
  if (normalized.includes('bug')) return Bug
  if (normalized.includes('idea')) return Lightbulb
  if (normalized.includes('problem')) return TriangleAlert
  if (normalized.includes('feature')) return Sparkles
  return ListTodo
}

export function BoardPage() {
  const items = useBonsaiStore((state) => state.boardItems)
  const lists = useBonsaiStore((state) => state.boardLists)
  const moveBoardItem = useBonsaiStore((state) => state.moveBoardItem)
  const [activeId, setActiveId] = useState<string | null>(null)
  const [configOpen, setConfigOpen] = useState(false)
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }))
  const activeItem = items.find((item) => item.id === activeId)
  const sortedLists = useMemo(() => [...lists].filter((list) => !list.archived).sort((a, b) => a.order - b.order), [lists])

  const onDragStart = (event: DragStartEvent) => setActiveId(String(event.active.id))
  const onDragEnd = (event: DragEndEvent) => {
    setActiveId(null)
    const over = event.over?.id ? String(event.over.id) : ''
    if (!over.startsWith('column:')) return
    moveBoardItem(String(event.active.id), over.replace('column:', '') as BoardStatus)
  }

  return (
    <div className="island relative flex h-full min-h-0 flex-col overflow-hidden">
      <div className="island-title h-11 shrink-0 !px-4">
        <ListTodo size={14} className="mr-2 text-muted" />
        <span>Tables</span>
        <span className="ml-3 normal-case tracking-normal text-muted-2">{sortedLists.length} user-defined lists · {items.length} items</span>
        <button
          type="button"
          onClick={() => setConfigOpen(true)}
          className="bonsai-focus btn-bordered ml-auto flex h-8 items-center gap-1.5 rounded-[7px] px-2.5 text-[12px] normal-case tracking-normal text-muted hover:text-text"
        >
          <Settings2 size={12} /> Configure table
        </button>
      </div>

      <DndContext sensors={sensors} collisionDetection={closestCenter} onDragStart={onDragStart} onDragEnd={onDragEnd}>
        <div className="flex min-h-0 flex-1 gap-3 overflow-auto p-3">
          {sortedLists.map((list) => (
            <BoardColumn key={list.id} list={list} items={items.filter((item) => item.status === list.id)} />
          ))}
          {!sortedLists.length && (
            <button
              type="button"
              onClick={() => setConfigOpen(true)}
              className="grid min-w-[280px] place-items-center rounded-xl border border-dashed border-border text-[12px] text-muted-2"
            >
              Configure your first list
            </button>
          )}
        </div>
        <DragOverlay>{activeItem ? <BoardCardContent item={activeItem} overlay /> : null}</DragOverlay>
      </DndContext>

      {configOpen && <TableConfig onClose={() => setConfigOpen(false)} />}
    </div>
  )
}

function BoardColumn({ list, items }: { list: BoardList; items: BoardItem[] }) {
  const { setNodeRef, isOver } = useDroppable({ id: 'column:' + list.id })

  return (
    <section
      ref={setNodeRef}
      className={
        'w-[285px] min-w-[285px] self-start overflow-hidden rounded-xl border bg-panel-2 transition-colors ' +
        (isOver ? 'border-accent' : 'border-border')
      }
    >
      <div className="flex h-11 items-center border-b border-border-subtle px-3">
        <Tag size={12} className="mr-2" style={{ color: tagPalette[list.color].foreground }} />
        <span className="text-[13px] font-semibold">{list.name}</span>
        <span className="island-count ml-2">{items.length}</span>
        <span className="ml-auto font-mono text-[10px] text-muted-2">{list.priority} · {list.itemType}</span>
      </div>
      <div className="space-y-2 p-2">
        {items.map((item) => <DraggableCard key={item.id} item={item} />)}
        {!items.length && <div className="rounded-[10px] border border-dashed border-border p-5 text-center text-[11px] text-muted-2">Drop items here</div>}
      </div>
    </section>
  )
}

function DraggableCard({ item }: { item: BoardItem }) {
  const { attributes, listeners, setNodeRef, transform, isDragging } = useDraggable({ id: item.id })
  return (
    <div ref={setNodeRef} style={{ transform: CSS.Translate.toString(transform) }} {...listeners} {...attributes} className={'touch-none ' + (isDragging ? 'opacity-25' : '')}>
      <BoardCardContent item={item} />
    </div>
  )
}

function BoardCardContent({ item, overlay = false }: { item: BoardItem; overlay?: boolean }) {
  const Icon = iconForKind(item.kind)
  return (
    <article className={'rounded-[10px] border border-border bg-panel p-3 ' + (overlay ? 'w-[280px] shadow-overlay' : 'shadow-card cursor-grab active:cursor-grabbing')}>
      <div className="flex items-start gap-2">
        <Icon size={13} className="mt-0.5 shrink-0 text-muted" />
        <div className="min-w-0 flex-1">
          <div className="text-[12px] leading-5 text-text">{item.title}</div>
          <div className="mt-2 flex items-center gap-2 font-mono text-[10px] text-muted-2">
            <span className="rounded border border-border-subtle bg-panel-2 px-1.5 py-0.5">{item.kind}</span>
            <span className="flex items-center gap-1"><CircleDot size={9} />{item.priority}</span>
            <span className="ml-auto truncate">{item.assignee}</span>
          </div>
        </div>
        <GripVertical size={13} className="text-muted-2" />
      </div>
    </article>
  )
}

function TableConfig({ onClose }: { onClose: () => void }) {
  const lists = useBonsaiStore((state) => state.boardLists)
  const priorities = useBonsaiStore((state) => state.boardPriorities)
  const types = useBonsaiStore((state) => state.boardTypes)
  const items = useBonsaiStore((state) => state.boardItems)
  const addList = useBonsaiStore((state) => state.addBoardList)
  const updateList = useBonsaiStore((state) => state.updateBoardList)
  const removeList = useBonsaiStore((state) => state.removeBoardList)
  const moveList = useBonsaiStore((state) => state.moveBoardList)
  const addPriority = useBonsaiStore((state) => state.addBoardPriority)
  const removePriority = useBonsaiStore((state) => state.removeBoardPriority)
  const addType = useBonsaiStore((state) => state.addBoardType)
  const removeType = useBonsaiStore((state) => state.removeBoardType)
  const [priorityDraft, setPriorityDraft] = useState('')
  const [typeDraft, setTypeDraft] = useState('')
  const [pendingDelete, setPendingDelete] = useState('')
  const [moveTo, setMoveTo] = useState('')

  const sorted = [...lists].sort((a, b) => a.order - b.order)
  const deleteList = sorted.find((list) => list.id === pendingDelete)
  const deleteHasItems = items.some((item) => item.status === pendingDelete)
  const alternatives = sorted.filter((list) => list.id !== pendingDelete)

  return (
    <div className="absolute inset-0 z-50 flex justify-end bg-well/70 backdrop-blur-[1px]">
      <aside className="flex h-full w-[520px] max-w-[92vw] flex-col border-l border-border-strong bg-panel shadow-overlay">
        <div className="flex h-11 shrink-0 items-center border-b border-border-subtle px-4">
          <Settings2 size={14} className="mr-2 text-accent" />
          <div>
            <div className="text-[13px] font-semibold">Configure table</div>
            <div className="font-mono text-[10px] text-muted-2">Lists, priorities and types are project workflow primitives.</div>
          </div>
          <button onClick={onClose} className="bonsai-focus ml-auto grid h-7 w-7 place-items-center rounded-md text-muted hover:bg-panel-3 hover:text-text"><X size={13} /></button>
        </div>

        <div className="min-h-0 flex-1 space-y-6 overflow-auto p-4">
          <section>
            <div className="mb-2 flex items-center">
              <div className="font-mono text-[10px] font-semibold uppercase tracking-[.12em] text-muted-2">Lists</div>
              <button onClick={addList} className="bonsai-focus ml-auto btn-bordered flex h-7 items-center gap-1 rounded-[7px] px-2 text-[11px] text-muted hover:text-text"><Plus size={10} /> Add list</button>
            </div>
            <div className="space-y-2">
              {sorted.map((list, index) => (
                <div key={list.id} className="rounded-[10px] border border-border bg-well p-2.5">
                  <div className="grid grid-cols-[1.2fr_.85fr_.85fr_.65fr_auto] gap-2">
                    <input value={list.name} onChange={(event) => updateList(list.id, { name: event.target.value })} className="bonsai-focus h-8 min-w-0 rounded-[7px] border border-border bg-well px-2.5 text-[12px] outline-none placeholder:text-muted-2 focus:border-accent/55" />
                    <BonsaiSelect ariaLabel={'Priority for ' + list.name} compact value={list.priority} onChange={(value) => updateList(list.id, { priority: value })} options={priorities.map((item) => ({ value: item.name, label: item.name }))} />
                    <BonsaiSelect ariaLabel={'Type for ' + list.name} compact value={list.itemType} onChange={(value) => updateList(list.id, { itemType: value })} options={types.map((item) => ({ value: item.name, label: item.name }))} />
                    <BonsaiSelect ariaLabel={'Color for ' + list.name} compact value={list.color} onChange={(value) => updateList(list.id, { color: value as TagColor })} options={colors.map((color) => ({ value: color, label: color }))} />
                    <div className="flex gap-1">
                      <button disabled={index === 0} onClick={() => moveList(list.id, -1)} className="bonsai-focus grid h-8 w-7 place-items-center rounded-md border border-border bg-panel-2 text-muted disabled:opacity-30"><ArrowUp size={10} /></button>
                      <button disabled={index === sorted.length - 1} onClick={() => moveList(list.id, 1)} className="bonsai-focus grid h-8 w-7 place-items-center rounded-md border border-border bg-panel-2 text-muted disabled:opacity-30"><ArrowDown size={10} /></button>
                      <button onClick={() => { setPendingDelete(list.id); setMoveTo(sorted.find((item) => item.id !== list.id)?.id ?? '') }} className="bonsai-focus grid h-8 w-7 place-items-center rounded-md border border-border bg-panel-2 text-muted hover:text-danger"><Trash2 size={10} /></button>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          </section>

          <section>
            <div className="mb-2 font-mono text-[10px] font-semibold uppercase tracking-[.12em] text-muted-2">Priorities</div>
            <div className="flex flex-wrap gap-1.5">
              {priorities.map((priority) => (
                <span key={priority.id} className="inline-flex items-center gap-1 rounded-md border border-border bg-well px-2 py-1 text-[11px]">
                  {priority.name}
                  <button onClick={() => removePriority(priority.id)} className="text-muted-2 hover:text-danger"><X size={9} /></button>
                </span>
              ))}
            </div>
            <div className="mt-2 flex gap-2">
              <input value={priorityDraft} onChange={(event) => setPriorityDraft(event.target.value)} placeholder="New priority" className="bonsai-focus h-8 min-w-0 flex-1 rounded-[7px] border border-border bg-well px-2.5 text-[12px] outline-none placeholder:text-muted-2 focus:border-accent/55" />
              <button onClick={() => { addPriority(priorityDraft); setPriorityDraft('') }} className="bonsai-focus btn-bordered rounded-[7px] px-3 text-[12px]">Add</button>
            </div>
          </section>

          <section>
            <div className="mb-2 font-mono text-[10px] font-semibold uppercase tracking-[.12em] text-muted-2">Item types</div>
            <div className="flex flex-wrap gap-1.5">
              {types.map((type) => (
                <span key={type.id} className="inline-flex items-center gap-1 rounded-md border border-border bg-well px-2 py-1 text-[11px]">
                  {type.name}
                  <button onClick={() => removeType(type.id)} className="text-muted-2 hover:text-danger"><X size={9} /></button>
                </span>
              ))}
            </div>
            <div className="mt-2 flex gap-2">
              <input value={typeDraft} onChange={(event) => setTypeDraft(event.target.value)} placeholder="New type" className="bonsai-focus h-8 min-w-0 flex-1 rounded-[7px] border border-border bg-well px-2.5 text-[12px] outline-none placeholder:text-muted-2 focus:border-accent/55" />
              <button onClick={() => { addType(typeDraft); setTypeDraft('') }} className="bonsai-focus btn-bordered rounded-[7px] px-3 text-[12px]">Add</button>
            </div>
          </section>
        </div>

        {pendingDelete && deleteList && (
          <div className="border-t border-danger/30 bg-danger-solid/20 p-3">
            <div className="text-[12px] font-medium">Remove “{deleteList.name}”?</div>
            {deleteHasItems && (
              <div className="mt-2">
                <div className="mb-1 font-mono text-[10px] text-muted-2">Move its cards to:</div>
                <BonsaiSelect ariaLabel="Move cards to list" compact value={moveTo} onChange={setMoveTo} options={alternatives.map((list) => ({ value: list.id, label: list.name }))} />
              </div>
            )}
            <div className="mt-2 flex justify-end gap-2">
              <button onClick={() => setPendingDelete('')} className="btn-bordered rounded-[7px] px-3 py-1.5 text-[11px] text-muted">Cancel</button>
              <button
                disabled={(deleteHasItems && !moveTo) || alternatives.length === 0}
                onClick={() => { removeList(pendingDelete, moveTo || alternatives[0]?.id || ''); setPendingDelete('') }}
                className="btn-danger-tint rounded-[7px] px-3 py-1.5 text-[11px] disabled:opacity-35"
              >
                Remove list
              </button>
            </div>
          </div>
        )}
      </aside>
    </div>
  )
}
