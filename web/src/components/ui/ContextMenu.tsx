import * as ContextMenu from '@radix-ui/react-context-menu'
import { ChevronRight, type LucideIcon } from 'lucide-react'
import { useEffect, useState, type ReactNode } from 'react'

const itemClass = 'flex cursor-default select-none items-center gap-2 rounded-md px-2 py-1.5 text-[12px] outline-none data-[disabled]:opacity-40 data-[highlighted]:bg-accent/12 '
const toneClass = { normal: 'text-muted data-[highlighted]:text-text', danger: 'text-danger data-[highlighted]:bg-danger-solid/14' }
const surfaceClass = 'z-[100] min-w-48 rounded-[10px] border border-border-strong bg-panel-3 p-1 shadow-overlay'

export function MenuContent(props: ContextMenu.ContextMenuContentProps) {
  return (
    <ContextMenu.Portal>
      <SettledContent {...props} />
    </ContextMenu.Portal>
  )
}

// Radix's context menu trigger keeps the click point in state and hands the
// anchor to the popper in an effect, so the first positioned frame uses the
// previous point: (0, 0) on the first open, which flashes the menu in the top
// left corner. Keep the menu transparent (still focusable) until the popper
// has repositioned on the real point.
function SettledContent({ className = '', style, ...props }: ContextMenu.ContextMenuContentProps) {
  const [settled, setSettled] = useState(false)
  useEffect(() => {
    let second = 0
    const first = requestAnimationFrame(() => { second = requestAnimationFrame(() => setSettled(true)) })
    return () => {
      cancelAnimationFrame(first)
      cancelAnimationFrame(second)
    }
  }, [])
  return <ContextMenu.Content {...props} style={settled ? style : { ...style, opacity: 0 }} className={surfaceClass + ' ' + className} />
}

/**
 * One action. `hint` is muted text after the label, part of its accessible
 * name, and explains why a disabled item cannot run (e.g. "No PR linked").
 */
export function MenuItem({ icon: Icon, children, hint, disabled = false, danger = false, onSelect }: {
  icon?: LucideIcon
  children: ReactNode
  hint?: string
  disabled?: boolean
  danger?: boolean
  onSelect?: () => void
}) {
  return (
    <ContextMenu.Item onSelect={onSelect} disabled={disabled} className={itemClass + toneClass[danger ? 'danger' : 'normal']}>
      {Icon && <Icon size={13} className="shrink-0" />}
      <span className="min-w-0 flex-1 truncate">{children}</span>
      {hint && <span className="ml-3 shrink-0 text-[9.5px] text-muted-2"> {hint}</span>}
    </ContextMenu.Item>
  )
}

export function MenuSub({ icon: Icon, label, disabled = false, hint, children }: {
  icon?: LucideIcon
  label: string
  disabled?: boolean
  hint?: string
  children: ReactNode
}) {
  return (
    <ContextMenu.Sub>
      <ContextMenu.SubTrigger disabled={disabled} className={itemClass + toneClass.normal + ' data-[state=open]:bg-accent/12 data-[state=open]:text-text'}>
        {Icon && <Icon size={13} className="shrink-0" />}
        <span className="min-w-0 flex-1 truncate">{label}</span>
        {hint && <span className="ml-3 shrink-0 text-[9.5px] text-muted-2"> {hint}</span>}
        <ChevronRight size={12} className="ml-2 shrink-0 text-muted-2" />
      </ContextMenu.SubTrigger>
      <ContextMenu.Portal>
        <ContextMenu.SubContent sideOffset={6} alignOffset={-5} className={surfaceClass}>{children}</ContextMenu.SubContent>
      </ContextMenu.Portal>
    </ContextMenu.Sub>
  )
}

export function MenuSeparator() {
  return <ContextMenu.Separator className="my-1 h-px bg-border-subtle" />
}

export function MenuLabel({ children }: { children: ReactNode }) {
  return <ContextMenu.Label className="px-2 pb-1 pt-1.5 font-mono text-[9.5px] uppercase tracking-[.08em] text-muted-2">{children}</ContextMenu.Label>
}
