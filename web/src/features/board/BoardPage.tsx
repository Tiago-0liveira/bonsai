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

const colors: TagColor[] = ['purple', 'blue', 'green', 'orange', 'red', 'cyan', 'pink']

function iconForKind(kind: string): LucideIcon {
  const normalized = kind.toLowerCase()
  if (normalized.includes('bug')) return Bug
  if (normalized.includes('idea')) return Lightbulb
  if (normalized.includes('problem')) return TriangleAlert
  if (normalized.includes('feature')) return Sparkles
  return ListTodo
}

function colorClass(color: TagColor) {
  const map: Record<TagColor, string> = {
    purple: 'text-[rgb(var(--purple))]',
    blue: 'text-[rgb(var(--blue))]',
    green: 'text-[rgb(var(--green))]',
    orange: 'text-[rgb(var(--orange))]',
    red: 'text-[rgb(var(--red))]',
    cyan: 'text-cyan-400',
    pink: 'text-pink-400',
  }
  return map[color]
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
    <div className="relative flex h-full min-h-0 flex-col bg-[rgb(var(--bg))]">
      <div className="flex h-12 shrink-0 items-center border-b border-[rgb(var(--border))] px-4">
        <ListTodo size={15} className="mr-2 text-[rgb(var(--muted))]" />
        <span className="font-medium">Tables</span>
        <span className="ml-3 text-[11px] text-[rgb(var(--muted-2))]">{sortedLists.length} user-defined lists · {items.length} items</span>
        <button
          type="button"
          onClick={() => setConfigOpen(true)}
          className="bonsai-focus ml-auto flex h-8 items-center gap-1.5 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--panel))] px-2.5 text-[10px] text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))] hover:text-[rgb(var(--text))]"
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
              className="grid min-w-[280px] place-items-center rounded-lg border border-dashed border-[rgb(var(--border))] text-[11px] text-[rgb(var(--muted-2))]"
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
        'w-[285px] min-w-[285px] self-start overflow-hidden rounded-lg border bg-[rgb(var(--panel))] transition-colors ' +
        (isOver ? 'border-[rgb(var(--purple))]' : 'border-[rgb(var(--border))]')
      }
    >
      <div className="flex h-11 items-center border-b border-[rgb(var(--border))] px-3">
        <Tag size={12} className={'mr-2 ' + colorClass(list.color)} />
        <span className="font-medium">{list.name}</span>
        <span className="ml-2 rounded-full bg-[rgb(var(--panel-3))] px-1.5 py-0.5 text-[9px] text-[rgb(var(--muted))]">{items.length}</span>
        <span className="ml-auto text-[8px] text-[rgb(var(--muted-2))]">{list.priority} · {list.itemType}</span>
      </div>
      <div className="space-y-2 p-2">
        {items.map((item) => <DraggableCard key={item.id} item={item} />)}
        {!items.length && <div className="rounded-md border border-dashed border-[rgb(var(--border))] p-5 text-center text-[10px] text-[rgb(var(--muted-2))]">Drop items here</div>}
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
    <article className={'rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--panel-2))] p-3 ' + (overlay ? 'w-[280px] shadow-2xl' : 'cursor-grab active:cursor-grabbing')}>
      <div className="flex items-start gap-2">
        <Icon size={13} className="mt-0.5 shrink-0 text-[rgb(var(--muted))]" />
        <div className="min-w-0 flex-1">
          <div className="text-[12px] leading-5 text-[rgb(var(--text))]">{item.title}</div>
          <div className="mt-2 flex items-center gap-2 text-[9px] text-[rgb(var(--muted-2))]">
            <span className="rounded border border-[rgb(var(--border))] px-1.5 py-0.5">{item.kind}</span>
            <span className="flex items-center gap-1"><CircleDot size={9} />{item.priority}</span>
            <span className="ml-auto truncate">{item.assignee}</span>
          </div>
        </div>
        <GripVertical size={13} className="text-[rgb(var(--muted-2))]" />
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
    <div className="absolute inset-0 z-50 flex justify-end bg-black/45 backdrop-blur-[1px]">
      <aside className="flex h-full w-[520px] max-w-[92vw] flex-col border-l border-[rgb(var(--border-strong))] bg-[rgb(var(--panel))] shadow-2xl">
        <div className="flex h-12 shrink-0 items-center border-b border-[rgb(var(--border))] px-4">
          <Settings2 size={14} className="mr-2 text-[rgb(var(--purple))]" />
          <div>
            <div className="text-[12px] font-semibold">Configure table</div>
            <div className="text-[9px] text-[rgb(var(--muted-2))]">Lists, priorities and types are project workflow primitives.</div>
          </div>
          <button onClick={onClose} className="bonsai-focus ml-auto grid h-7 w-7 place-items-center rounded-md text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]"><X size={13} /></button>
        </div>

        <div className="min-h-0 flex-1 space-y-6 overflow-auto p-4">
          <section>
            <div className="mb-2 flex items-center">
              <div className="text-[9px] font-semibold uppercase tracking-[.12em] text-[rgb(var(--muted-2))]">Lists</div>
              <button onClick={addList} className="bonsai-focus ml-auto flex h-7 items-center gap-1 rounded-md border border-[rgb(var(--border))] px-2 text-[9px] text-[rgb(var(--muted))] hover:text-[rgb(var(--text))]"><Plus size={10} /> Add list</button>
            </div>
            <div className="space-y-2">
              {sorted.map((list, index) => (
                <div key={list.id} className="rounded-lg border border-[rgb(var(--border))] bg-[rgb(var(--bg))] p-2.5">
                  <div className="grid grid-cols-[1.2fr_.85fr_.85fr_.65fr_auto] gap-2">
                    <input value={list.name} onChange={(event) => updateList(list.id, { name: event.target.value })} className="bonsai-focus h-8 min-w-0 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--panel-2))] px-2 text-[10px] outline-none" />
                    <BonsaiSelect ariaLabel={'Priority for ' + list.name} compact value={list.priority} onChange={(value) => updateList(list.id, { priority: value })} options={priorities.map((item) => ({ value: item.name, label: item.name }))} />
                    <BonsaiSelect ariaLabel={'Type for ' + list.name} compact value={list.itemType} onChange={(value) => updateList(list.id, { itemType: value })} options={types.map((item) => ({ value: item.name, label: item.name }))} />
                    <BonsaiSelect ariaLabel={'Color for ' + list.name} compact value={list.color} onChange={(value) => updateList(list.id, { color: value as TagColor })} options={colors.map((color) => ({ value: color, label: color }))} />
                    <div className="flex gap-1">
                      <button disabled={index === 0} onClick={() => moveList(list.id, -1)} className="bonsai-focus grid h-8 w-7 place-items-center rounded border border-[rgb(var(--border))] text-[rgb(var(--muted))] disabled:opacity-30"><ArrowUp size={10} /></button>
                      <button disabled={index === sorted.length - 1} onClick={() => moveList(list.id, 1)} className="bonsai-focus grid h-8 w-7 place-items-center rounded border border-[rgb(var(--border))] text-[rgb(var(--muted))] disabled:opacity-30"><ArrowDown size={10} /></button>
                      <button onClick={() => { setPendingDelete(list.id); setMoveTo(sorted.find((item) => item.id !== list.id)?.id ?? '') }} className="bonsai-focus grid h-8 w-7 place-items-center rounded border border-[rgb(var(--border))] text-[rgb(var(--muted))] hover:text-[rgb(var(--red))]"><Trash2 size={10} /></button>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          </section>

          <section>
            <div className="mb-2 text-[9px] font-semibold uppercase tracking-[.12em] text-[rgb(var(--muted-2))]">Priorities</div>
            <div className="flex flex-wrap gap-1.5">
              {priorities.map((priority) => (
                <span key={priority.id} className="inline-flex items-center gap-1 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-2 py-1 text-[9px]">
                  {priority.name}
                  <button onClick={() => removePriority(priority.id)} className="text-[rgb(var(--muted-2))] hover:text-[rgb(var(--red))]"><X size={9} /></button>
                </span>
              ))}
            </div>
            <div className="mt-2 flex gap-2">
              <input value={priorityDraft} onChange={(event) => setPriorityDraft(event.target.value)} placeholder="New priority" className="bonsai-focus h-8 min-w-0 flex-1 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-2 text-[10px] outline-none" />
              <button onClick={() => { addPriority(priorityDraft); setPriorityDraft('') }} className="bonsai-focus rounded-md border border-[rgb(var(--border))] px-2 text-[9px]">Add</button>
            </div>
          </section>

          <section>
            <div className="mb-2 text-[9px] font-semibold uppercase tracking-[.12em] text-[rgb(var(--muted-2))]">Item types</div>
            <div className="flex flex-wrap gap-1.5">
              {types.map((type) => (
                <span key={type.id} className="inline-flex items-center gap-1 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-2 py-1 text-[9px]">
                  {type.name}
                  <button onClick={() => removeType(type.id)} className="text-[rgb(var(--muted-2))] hover:text-[rgb(var(--red))]"><X size={9} /></button>
                </span>
              ))}
            </div>
            <div className="mt-2 flex gap-2">
              <input value={typeDraft} onChange={(event) => setTypeDraft(event.target.value)} placeholder="New type" className="bonsai-focus h-8 min-w-0 flex-1 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-2 text-[10px] outline-none" />
              <button onClick={() => { addType(typeDraft); setTypeDraft('') }} className="bonsai-focus rounded-md border border-[rgb(var(--border))] px-2 text-[9px]">Add</button>
            </div>
          </section>
        </div>

        {pendingDelete && deleteList && (
          <div className="border-t border-[rgb(var(--red)/.3)] bg-[rgb(var(--red)/.05)] p-3">
            <div className="text-[10px] font-medium">Remove “{deleteList.name}”?</div>
            {deleteHasItems && (
              <div className="mt-2">
                <div className="mb-1 text-[8px] text-[rgb(var(--muted-2))]">Move its cards to:</div>
                <BonsaiSelect ariaLabel="Move cards to list" compact value={moveTo} onChange={setMoveTo} options={alternatives.map((list) => ({ value: list.id, label: list.name }))} />
              </div>
            )}
            <div className="mt-2 flex justify-end gap-2">
              <button onClick={() => setPendingDelete('')} className="rounded-md px-2 py-1.5 text-[9px] text-[rgb(var(--muted))]">Cancel</button>
              <button
                disabled={(deleteHasItems && !moveTo) || alternatives.length === 0}
                onClick={() => { removeList(pendingDelete, moveTo || alternatives[0]?.id || ''); setPendingDelete('') }}
                className="rounded-md border border-[rgb(var(--red)/.35)] bg-[rgb(var(--red)/.1)] px-2 py-1.5 text-[9px] text-[rgb(var(--red))] disabled:opacity-35"
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
