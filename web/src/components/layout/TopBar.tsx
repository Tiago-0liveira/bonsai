import { useEffect, useRef, useState } from 'react'
import { Link } from '@tanstack/react-router'
import { Bell, Check, ChevronDown, Command, Search, Sprout } from 'lucide-react'
import { workspaces } from '../../mock/projects'
import { useBonsaiStore } from '../../stores/bonsai'

const nav = [
  { label: 'Canvas', to: '/' },
  { label: 'Table', to: '/tables' },
  { label: 'GitHub', to: '/github' },
  { label: 'Logs', to: '/logs' },
] as const

function HeaderSelect({
  label,
  value,
  options,
  onChange,
}: {
  label: string
  value: string
  options: { value: string; label: string }[]
  onChange: (value: string) => void
}) {
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement | null>(null)
  const selected = options.find((option) => option.value === value) ?? options[0]

  useEffect(() => {
    if (!open) return
    const close = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false)
    }
    window.addEventListener('pointerdown', close)
    return () => window.removeEventListener('pointerdown', close)
  }, [open])

  return (
    <div ref={rootRef} className="relative max-w-44">
      <button
        type="button"
        aria-label={label}
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
        className={
          'bonsai-focus flex h-8 max-w-44 items-center gap-2 rounded-md border px-2.5 text-[12px] font-medium transition-colors ' +
          (open
            ? 'border-[rgb(var(--border-strong))] bg-[rgb(var(--panel-2))] text-[rgb(var(--text))]'
            : 'border-transparent text-[rgb(var(--muted))] hover:border-[rgb(var(--border))] hover:bg-[rgb(var(--panel-2))] hover:text-[rgb(var(--text))]')
        }
      >
        <span className="truncate">{selected?.label ?? label}</span>
        <ChevronDown
          size={12}
          className={'shrink-0 text-[rgb(var(--muted-2))] transition-transform ' + (open ? 'rotate-180' : '')}
        />
      </button>

      {open && (
        <div
          role="listbox"
          aria-label={label}
          className="absolute left-0 top-[calc(100%+6px)] z-[80] min-w-[190px] overflow-hidden rounded-lg border border-[rgb(var(--border-strong))] bg-[rgb(var(--panel-2))] p-1 shadow-[0_18px_55px_rgb(0_0_0/.46)]"
        >
          <div className="px-2 py-1.5 text-[9px] font-semibold uppercase tracking-[.12em] text-[rgb(var(--muted-2))]">
            {label}
          </div>
          {options.map((option) => {
            const active = option.value === value
            return (
              <button
                type="button"
                role="option"
                aria-selected={active}
                key={option.value}
                onClick={() => {
                  onChange(option.value)
                  setOpen(false)
                }}
                className={
                  'flex w-full items-center gap-2 rounded-md px-2 py-2 text-left text-[11px] transition-colors ' +
                  (active
                    ? 'bg-[rgb(var(--purple)/.12)] text-[rgb(var(--text))]'
                    : 'text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-3))] hover:text-[rgb(var(--text))]')
                }
              >
                <span className="min-w-0 flex-1 truncate">{option.label}</span>
                {active && <Check size={12} className="text-[rgb(var(--purple))]" />}
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}

export function TopBar() {
  const projects = useBonsaiStore((state) => state.projects)
  const activeWorkspaceId = useBonsaiStore((state) => state.activeWorkspaceId)
  const activeProjectId = useBonsaiStore((state) => state.activeProjectId)
  const setActiveWorkspace = useBonsaiStore((state) => state.setActiveWorkspace)
  const setActiveProject = useBonsaiStore((state) => state.setActiveProject)
  const setPaletteOpen = useBonsaiStore((state) => state.setPaletteOpen)
  const setNotice = useBonsaiStore((state) => state.setNotice)

  const workspaceProjects = projects.filter((project) => project.workspaceId === activeWorkspaceId)

  return (
    <header className="grid h-12 shrink-0 grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center border-b border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-3">
      <div className="flex min-w-0 items-center justify-self-start">
        <Link to="/" className="flex shrink-0 items-center gap-2 pr-2 font-semibold tracking-tight">
          <span className="grid h-7 w-7 place-items-center text-[rgb(var(--green))]">
            <Sprout size={19} />
          </span>
          <span>bonsai</span>
        </Link>
        <span className="px-1 text-[rgb(var(--muted-2))]">/</span>
        <HeaderSelect
          label="Workspace"
          value={activeWorkspaceId}
          options={workspaces.map((workspace) => ({ value: workspace.id, label: workspace.name }))}
          onChange={setActiveWorkspace}
        />
        <span className="px-1 text-[rgb(var(--muted-2))]">/</span>
        <HeaderSelect
          label="Project"
          value={activeProjectId}
          options={workspaceProjects.map((project) => ({ value: project.id, label: project.name }))}
          onChange={setActiveProject}
        />
      </div>

      <nav className="top-nav-secondary flex h-full items-center justify-self-center gap-2">
        {nav.map((item) => (
          <Link
            key={item.label}
            to={item.to}
            className="relative flex h-full items-center px-2.5 text-[12px] text-[rgb(var(--muted))] transition-colors hover:text-[rgb(var(--text))]"
            activeProps={{
              className:
                'relative flex h-full items-center px-2.5 text-[12px] text-[rgb(var(--text))] after:absolute after:bottom-0 after:left-2 after:right-2 after:h-px after:bg-[rgb(var(--purple))]',
            }}
          >
            {item.label}
          </Link>
        ))}
        <button
          onClick={() => setNotice('Settings are mocked in this frontend prototype')}
          className="flex h-full items-center px-2.5 text-[12px] text-[rgb(var(--muted))] transition-colors hover:text-[rgb(var(--text))]"
        >
          Settings
        </button>
      </nav>

      <div className="flex items-center gap-1.5 justify-self-end">
        <button
          className="bonsai-focus flex h-7 items-center gap-2 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--panel))] px-2.5 text-[12px] text-[rgb(var(--muted))] transition-colors hover:bg-[rgb(var(--panel-2))]"
          onClick={() => setPaletteOpen(true)}
        >
          <Search size={13} />
          <span className="hidden min-[980px]:inline">Search</span>
          <span className="bonsai-kbd hidden min-[980px]:inline-flex"><Command size={10} />K</span>
        </button>
        <button className="bonsai-focus grid h-7 w-7 place-items-center rounded-md text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]">
          <Bell size={14} />
        </button>
        <button className="bonsai-focus grid h-7 w-7 place-items-center rounded-full border border-[rgb(var(--border))] bg-[rgb(var(--panel-2))] text-[10px] font-semibold">
          TO
        </button>
      </div>
    </header>
  )
}
