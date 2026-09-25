import { useState, type FormEvent } from 'react'
import {
  Activity,
  Boxes,
  Github,
  PanelLeftClose,
  PanelLeftOpen,
  Plus,
  Search,
  Settings,
  Sparkles,
  X,
} from 'lucide-react'
import { useBonsaiStore } from '../../stores/bonsai'

const links = [
  { label: 'GitHub', icon: Github },
  { label: 'Templates', icon: Sparkles },
  { label: 'Activity', icon: Activity },
  { label: 'Settings', icon: Settings },
]

export function ProjectSidebar() {
  const [creating, setCreating] = useState(false)
  const [name, setName] = useState('')
  const query = useBonsaiStore((state) => state.projectQuery)
  const setQuery = useBonsaiStore((state) => state.setProjectQuery)
  const projects = useBonsaiStore((state) => state.projects)
  const activeWorkspaceId = useBonsaiStore((state) => state.activeWorkspaceId)
  const activeProjectId = useBonsaiStore((state) => state.activeProjectId)
  const setActiveProject = useBonsaiStore((state) => state.setActiveProject)
  const createMockProject = useBonsaiStore((state) => state.createMockProject)
  const sidebarCollapsed = useBonsaiStore((state) => state.sidebarCollapsed)
  const toggleSidebar = useBonsaiStore((state) => state.toggleSidebar)

  const filtered = projects.filter(
    (project) =>
      project.workspaceId === activeWorkspaceId &&
      project.name.toLowerCase().includes(query.toLowerCase()),
  )

  const beginCreate = () => {
    if (sidebarCollapsed) toggleSidebar()
    setCreating(true)
  }

  const submitProject = (event: FormEvent) => {
    event.preventDefault()
    if (!name.trim()) return
    createMockProject(name)
    setName('')
    setCreating(false)
  }

  return (
    <aside
      className={
        sidebarCollapsed
          ? 'desktop-sidebar flex w-12 shrink-0 flex-col border-r border-[rgb(var(--border))] bg-[rgb(var(--panel))] transition-[width]'
          : 'desktop-sidebar flex w-[216px] shrink-0 flex-col border-r border-[rgb(var(--border))] bg-[rgb(var(--panel))] transition-[width]'
      }
    >
      <div className="flex h-10 items-center border-b border-[rgb(var(--border))] px-2">
        {!sidebarCollapsed && <span className="px-1 text-[11px] font-semibold">Projects</span>}
        <div className="ml-auto flex items-center gap-1">
          <button
            onClick={beginCreate}
            title="Add project"
            className="bonsai-focus grid h-7 w-7 place-items-center rounded-md text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))] hover:text-[rgb(var(--text))]"
          >
            <Plus size={14} />
          </button>
          <button
            onClick={toggleSidebar}
            title={sidebarCollapsed ? 'Expand sidebar' : 'Collapse sidebar'}
            className="bonsai-focus grid h-7 w-7 place-items-center rounded-md text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))] hover:text-[rgb(var(--text))]"
          >
            {sidebarCollapsed ? <PanelLeftOpen size={14} /> : <PanelLeftClose size={14} />}
          </button>
        </div>
      </div>

      {!sidebarCollapsed && (
        <div className="p-2.5 pb-1.5">
          <div className="flex h-8 items-center gap-2 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-2 text-[rgb(var(--muted))]">
            <Search size={13} />
            <input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder="Search projects..."
              className="min-w-0 flex-1 bg-transparent text-[12px] text-[rgb(var(--text))] outline-none placeholder:text-[rgb(var(--muted-2))]"
            />
          </div>
        </div>
      )}

      {!sidebarCollapsed && creating && (
        <form onSubmit={submitProject} className="mx-2 mb-2 rounded-md border border-[rgb(var(--purple)/.35)] bg-[rgb(var(--panel-2))] p-2">
          <div className="flex items-center gap-1.5">
            <input
              autoFocus
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder="Project name"
              className="min-w-0 flex-1 bg-transparent text-[11px] outline-none placeholder:text-[rgb(var(--muted-2))]"
            />
            <button type="submit" className="rounded bg-[rgb(var(--purple)/.16)] px-2 py-1 text-[10px] text-[rgb(var(--text))]">
              Add
            </button>
            <button type="button" onClick={() => setCreating(false)} className="grid h-6 w-6 place-items-center text-[rgb(var(--muted))]">
              <X size={12} />
            </button>
          </div>
        </form>
      )}

      <div className={sidebarCollapsed ? 'px-1.5 pt-2' : 'px-2'}>
        {filtered.map((project) => {
          const selected = project.id === activeProjectId
          return (
            <button
              key={project.id}
              title={sidebarCollapsed ? project.name : undefined}
              onClick={() => setActiveProject(project.id)}
              className={
                'bonsai-focus mb-0.5 flex w-full items-center rounded-md text-left transition-colors ' +
                (sidebarCollapsed ? 'justify-center px-1 py-2.5 ' : 'gap-2 px-2 py-2 ') +
                (selected
                  ? 'bg-[rgb(var(--purple)/.10)] text-[rgb(var(--text))]'
                  : 'text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]')
              }
            >
              <span
                className={
                  'h-2 w-2 shrink-0 rounded-full ' +
                  (project.health === 'healthy'
                    ? 'bg-[rgb(var(--green))]'
                    : project.health === 'warning'
                      ? 'bg-[rgb(var(--orange))]'
                      : 'bg-[rgb(var(--muted-2))]')
                }
              />
              {!sidebarCollapsed && (
                <>
                  <span className="min-w-0 flex-1 truncate font-medium">{project.name}</span>
                  <span className="text-[10px] text-[rgb(var(--muted-2))]">{project.worktreeIds.length}</span>
                </>
              )}
            </button>
          )
        })}
      </div>

      <div className="mt-auto border-t border-[rgb(var(--border))] p-2">
        {links.map(({ label, icon: Icon }) => (
          <button
            key={label}
            title={sidebarCollapsed ? label : undefined}
            className={
              'bonsai-focus flex w-full items-center rounded-md py-2 text-left text-[12px] text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))] hover:text-[rgb(var(--text))] ' +
              (sidebarCollapsed ? 'justify-center px-1' : 'gap-2 px-2')
            }
          >
            <Icon size={14} />
            {!sidebarCollapsed && label}
          </button>
        ))}
        {!sidebarCollapsed && (
          <div className="mt-2 flex items-center gap-2 rounded-md border border-[rgb(var(--border))] px-2 py-2 text-[11px] text-[rgb(var(--muted-2))]">
            <Boxes size={13} />
            Frontend prototype
          </div>
        )}
      </div>
    </aside>
  )
}
