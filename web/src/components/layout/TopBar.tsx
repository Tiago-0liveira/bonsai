import { Link } from '@tanstack/react-router'
import { Bell, ChevronDown, Command, Search, Sprout } from 'lucide-react'
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
  return (
    <label className="relative flex max-w-40 items-center">
      <span className="sr-only">{label}</span>
      <select
        aria-label={label}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        className="bonsai-focus h-7 max-w-40 appearance-none truncate rounded-md border border-transparent bg-transparent py-0 pl-2 pr-7 text-[12px] font-medium text-[rgb(var(--muted))] outline-none transition-colors hover:border-[rgb(var(--border))] hover:bg-[rgb(var(--panel-2))] hover:text-[rgb(var(--text))]"
      >
        {options.map((option) => (
          <option key={option.value} value={option.value} className="bg-[rgb(var(--panel-2))] text-[rgb(var(--text))]">
            {option.label}
          </option>
        ))}
      </select>
      <ChevronDown size={11} className="pointer-events-none absolute right-2 text-[rgb(var(--muted-2))]" />
    </label>
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
    <header className="flex h-12 shrink-0 items-center border-b border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-3">
      <div className="flex min-w-0 items-center">
        <Link to="/" className="flex items-center gap-2 pr-2 font-semibold tracking-tight">
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

      <nav className="top-nav-secondary ml-8 flex h-full items-center gap-2">
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

      <div className="ml-auto flex items-center gap-1.5">
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
