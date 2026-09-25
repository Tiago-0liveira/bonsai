import { useState } from 'react'
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
import { Bug, CircleDot, GripVertical, Lightbulb, ListTodo, Sparkles, TriangleAlert, type LucideIcon } from 'lucide-react'
import { useBonsaiStore } from '../../stores/bonsai'
import type { BoardItem, BoardKind, BoardStatus } from '../../types'

const columns: { id: BoardStatus; label: string }[] = [
  { id: 'todo', label: 'To Do' },
  { id: 'progress', label: 'In Progress' },
  { id: 'done', label: 'Done' },
]

const kindIcon: Record<BoardKind, LucideIcon> = {
  Idea: Lightbulb,
  Feature: Sparkles,
  Bug,
  Problem: TriangleAlert,
  Task: ListTodo,
}

export function BoardPage() {
  const items = useBonsaiStore((state) => state.boardItems)
  const moveBoardItem = useBonsaiStore((state) => state.moveBoardItem)
  const [activeId, setActiveId] = useState<string | null>(null)
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }))
  const activeItem = items.find((item) => item.id === activeId)

  const onDragStart = (event: DragStartEvent) => setActiveId(String(event.active.id))
  const onDragEnd = (event: DragEndEvent) => {
    setActiveId(null)
    const over = event.over?.id ? String(event.over.id) : ''
    if (!over.startsWith('column:')) return
    moveBoardItem(String(event.active.id), over.replace('column:', '') as BoardStatus)
  }

  return (
    <div className="flex h-full min-h-0 flex-col bg-[rgb(var(--bg))]">
      <div className="flex h-12 shrink-0 items-center border-b border-[rgb(var(--border))] px-4">
        <ListTodo size={15} className="mr-2 text-[rgb(var(--muted))]" />
        <span className="font-medium">Project Table</span>
        <span className="ml-3 text-[11px] text-[rgb(var(--muted-2))]">{items.length} items · drag cards between columns</span>
      </div>

      <DndContext sensors={sensors} collisionDetection={closestCenter} onDragStart={onDragStart} onDragEnd={onDragEnd}>
        <div className="grid min-h-0 flex-1 grid-cols-3 gap-3 overflow-auto p-3">
          {columns.map((column) => (
            <BoardColumn key={column.id} status={column.id} label={column.label} items={items.filter((item) => item.status === column.id)} />
          ))}
        </div>
        <DragOverlay>{activeItem ? <BoardCardContent item={activeItem} overlay /> : null}</DragOverlay>
      </DndContext>
    </div>
  )
}

function BoardColumn({
  status,
  label,
  items,
}: {
  status: BoardStatus
  label: string
  items: BoardItem[]
}) {
  const { setNodeRef, isOver } = useDroppable({ id: `column:${status}` })

  return (
    <section
      ref={setNodeRef}
      className={`min-w-[240px] rounded-lg border bg-[rgb(var(--panel))] transition-colors ${
        isOver ? 'border-[rgb(var(--purple))]' : 'border-[rgb(var(--border))]'
      }`}
    >
      <div className="flex h-10 items-center border-b border-[rgb(var(--border))] px-3">
        <span className="font-medium">{label}</span>
        <span className="ml-2 rounded-full bg-[rgb(var(--panel-3))] px-1.5 py-0.5 text-[9px] text-[rgb(var(--muted))]">{items.length}</span>
      </div>
      <div className="space-y-2 p-2">
        {items.map((item) => <DraggableCard key={item.id} item={item} />)}
        {!items.length && (
          <div className="rounded-md border border-dashed border-[rgb(var(--border))] p-5 text-center text-[11px] text-[rgb(var(--muted-2))]">
            Drop items here
          </div>
        )}
      </div>
    </section>
  )
}

function DraggableCard({ item }: { item: BoardItem }) {
  const { attributes, listeners, setNodeRef, transform, isDragging } = useDraggable({ id: item.id })

  return (
    <div
      ref={setNodeRef}
      style={{ transform: CSS.Translate.toString(transform) }}
      {...listeners}
      {...attributes}
      className={`touch-none ${
        isDragging ? 'opacity-25' : ''
      }`}
    >
      <BoardCardContent item={item} />
    </div>
  )
}

function BoardCardContent({ item, overlay = false }: { item: BoardItem; overlay?: boolean }) {
  const Icon = kindIcon[item.kind]
  return (
    <article className={`rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--panel-2))] p-3 ${
      overlay ? 'w-[280px] shadow-2xl' : 'cursor-grab active:cursor-grabbing'
    }`}>
      <div className="flex items-start gap-2">
        <Icon size={13} className="mt-0.5 shrink-0 text-[rgb(var(--muted))]" />
        <div className="min-w-0 flex-1">
          <div className="text-[12px] leading-5 text-[rgb(var(--text))]">{item.title}</div>
          <div className="mt-2 flex items-center gap-2 text-[9px] text-[rgb(var(--muted-2))]">
            <span className="rounded border border-[rgb(var(--border))] px-1.5 py-0.5">{item.kind}</span>
            <span className="flex items-center gap-1"><CircleDot size={9} />{item.priority}</span>
            <span className="ml-auto">{item.assignee}</span>
          </div>
        </div>
        <GripVertical size={13} className="text-[rgb(var(--muted-2))]" />
      </div>
    </article>
  )
}
