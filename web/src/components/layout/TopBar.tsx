import { useEffect, useRef, useState, useSyncExternalStore } from 'react'
import { Link } from '@tanstack/react-router'
import { Bell, Check, ChevronDown, Command, Folder, GitBranch, LayoutGrid, ScrollText, Search, SlidersHorizontal, Sprout } from 'lucide-react'
import { useBonsaiStore } from '../../stores/bonsai'
import { getRelayConnectionSnapshot, RELAY_ENABLED, relayLoginURL, subscribeRelayConnection } from '../../api/relayClient'

const nav = [
  { label: 'Canvas', to: '/', icon: LayoutGrid },
  { label: 'GitHub', to: '/github', icon: GitBranch },
  { label: 'Logs', to: '/logs', icon: ScrollText },
  { label: 'Settings', to: '/settings', icon: SlidersHorizontal },
] as const

function HeaderSelect({
  label,
  value,
  options,
  onChange,
  leading,
}: {
  label: string
  value: string
  options: { value: string; label: string }[]
  onChange: (value: string) => void
  leading?: React.ReactNode
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
          'bonsai-focus flex h-[26px] max-w-44 items-center gap-1.5 rounded-lg border bg-panel-2 px-2.5 text-[12px] font-medium transition-colors ' +
          (open
            ? 'border-border-strong text-text'
            : 'border-border text-muted hover:border-border-strong hover:text-text')
        }
      >
        {leading}
        <span className="truncate">{selected?.label ?? label}</span>
        <ChevronDown
          size={12}
          className={'shrink-0 text-muted-2 transition-transform ' + (open ? 'rotate-180' : '')}
        />
      </button>

      {open && (
        <div
          role="listbox"
          aria-label={label}
          className="absolute left-0 top-[calc(100%+6px)] z-[80] min-w-[190px] overflow-hidden rounded-[10px] border border-border-strong bg-panel-3 p-1 shadow-overlay"
        >
          <div className="px-2 py-1.5 text-[9px] font-semibold uppercase tracking-[.12em] text-muted-2">
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
                    ? 'bg-accent/[.12] text-text'
                    : 'text-muted hover:bg-panel-4 hover:text-text')
                }
              >
                <span className="min-w-0 flex-1 truncate">{option.label}</span>
                {active && <Check size={12} className="text-accent" />}
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}

function RelayStatus() {
  const relay = useSyncExternalStore(
    subscribeRelayConnection,
    getRelayConnectionSnapshot,
    getRelayConnectionSnapshot,
  )
  const label = relay.status === 'connected'
    ? 'GitHub realtime connected'
    : relay.status === 'authorization-expired'
      ? 'Connect GitHub'
      : relay.status === 'connecting'
        ? 'GitHub realtime connecting'
        : relay.status === 'offline'
          ? 'GitHub realtime offline'
          : 'GitHub realtime disconnected'
  const dot = relay.status === 'connected'
    ? 'bg-accent-solid'
    : relay.status === 'offline'
      ? 'bg-warn-solid'
      : 'bg-muted-2'
  const content = <><span className={`h-1.5 w-1.5 rounded-full ${dot}`} /><span className="hidden min-[1180px]:inline">{label}</span></>
  return relay.status === 'authorization-expired'
    ? <a href={relayLoginURL()} title={relay.message ?? label} className="bonsai-focus btn-ghost font-mono text-[11px]">{content}</a>
    : <span title={relay.message ?? label} className="flex h-[26px] items-center gap-2 px-2 font-mono text-[11px] text-muted">{content}</span>
}

export function TopBar() {
  const allWorkspaceProjects = useBonsaiStore(state => state.projects)
  const workspaces = [...new Set(allWorkspaceProjects.map(p => p.workspaceId))].map(id => ({ id, name: id }))

  const projects = useBonsaiStore((state) => state.projects)
  const activeWorkspaceId = useBonsaiStore((state) => state.activeWorkspaceId)
  const activeProjectId = useBonsaiStore((state) => state.activeProjectId)
  const setActiveWorkspace = useBonsaiStore((state) => state.setActiveWorkspace)
  const setActiveProject = useBonsaiStore((state) => state.setActiveProject)
  const setPaletteOpen = useBonsaiStore((state) => state.setPaletteOpen)
  const setNotice = useBonsaiStore((state) => state.setNotice)

  const workspaceProjects = projects.filter((project) => project.workspaceId === activeWorkspaceId)

  return (
    <header className="pointer-events-none absolute inset-x-4 top-4 z-30 grid h-10 grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] gap-3">
      <div className="island island-topbar pointer-events-auto flex h-10 min-w-0 items-center gap-2.5 justify-self-start px-1.5">
        <Link to="/" className="flex shrink-0 items-center gap-2">
          <span
            className="grid h-[30px] w-[30px] place-items-center rounded-lg text-accent-fg"
            style={{ background: 'linear-gradient(145deg, rgb(var(--accent)), rgb(var(--accent-solid)) 48%, rgb(var(--accent-solid) / .55))' }}
          >
            <Sprout size={17} />
          </span>
          <span className="flex flex-col leading-none">
            <span className="text-[13px] font-semibold tracking-tight text-text">bonsai</span>
            <span className="mt-[3px] font-mono text-[8.5px] uppercase tracking-[.14em] text-muted-2">WEB CLIENT</span>
          </span>
        </Link>
        <HeaderSelect
          label="Workspace"
          value={activeWorkspaceId}
          options={workspaces.map((workspace) => ({ value: workspace.id, label: workspace.name }))}
          onChange={setActiveWorkspace}
          leading={<span className="h-1.5 w-1.5 shrink-0 rounded-full bg-accent-solid" />}
        />
        <span className="-mx-1 text-muted-2">›</span>
        <HeaderSelect
          label="Project"
          value={activeProjectId}
          options={workspaceProjects.map((project) => ({ value: project.id, label: project.name }))}
          onChange={setActiveProject}
          leading={<Folder size={12} className="shrink-0 text-muted-2" />}
        />
      </div>

      <nav className="top-nav-secondary island island-topbar pointer-events-auto flex h-10 items-center gap-1 justify-self-center px-1.5">
        {nav.map((item) => (
          <Link
            key={item.label}
            to={item.to}
            className="bonsai-focus flex h-[30px] items-center gap-1.5 rounded-lg px-3 text-[12px] text-muted transition-colors hover:bg-panel-2 hover:text-text"
            activeProps={{
              className:
                'bonsai-focus flex h-[30px] items-center gap-1.5 rounded-lg bg-panel-3 px-3 text-[12px] font-medium text-text',
            }}
          >
            <item.icon size={14} className="shrink-0" />
            {item.label}
          </Link>
        ))}
      </nav>

      <div className="island island-topbar pointer-events-auto flex h-10 items-center gap-2.5 justify-self-end px-1.5">
        {RELAY_ENABLED && <RelayStatus />}
        <button className="bonsai-focus btn-ghost" onClick={() => setPaletteOpen(true)}>
          <Search size={13} />
          <span className="hidden min-[980px]:inline">Search</span>
          <span className="bonsai-kbd hidden min-[980px]:inline-flex"><Command size={10} />K</span>
        </button>
        <button aria-label="Notifications" className="bonsai-focus btn-ghost w-[26px] justify-center px-0">
          <Bell size={14} />
        </button>
        <button className="bonsai-focus grid h-[26px] w-[26px] place-items-center rounded-full bg-accent-solid text-[10px] font-semibold text-accent-fg">
          TO
        </button>
      </div>
    </header>
  )
}
