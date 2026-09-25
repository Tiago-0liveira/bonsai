import {
  Activity,
  Boxes,
  Github,
  Search,
  Settings,
  Sparkles,
} from 'lucide-react'
import { projects } from '../../mock/projects'
import { useBonsaiStore } from '../../stores/bonsai'

const links = [
  { label: 'GitHub', icon: Github },
  { label: 'Templates', icon: Sparkles },
  { label: 'Activity', icon: Activity },
  { label: 'Settings', icon: Settings },
]

export function ProjectSidebar() {
  const query = useBonsaiStore((state) => state.projectQuery)
  const setQuery = useBonsaiStore((state) => state.setProjectQuery)
  const selection = useBonsaiStore((state) => state.selection)
  const setSelection = useBonsaiStore((state) => state.setSelection)

  const filtered = projects.filter((project) =>
    project.name.toLowerCase().includes(query.toLowerCase()),
  )

  return (
    <aside className="desktop-sidebar flex w-[216px] shrink-0 flex-col border-r border-[rgb(var(--border))] bg-[rgb(var(--panel))]">
      <div className="p-2.5">
        <div className="flex h-8 items-center gap-2 rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-2 text-[rgb(var(--muted))]">
          <Search size={13} />
          <input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Projects"
            className="min-w-0 flex-1 bg-transparent text-[12px] text-[rgb(var(--text))] outline-none placeholder:text-[rgb(var(--muted-2))]"
          />
        </div>
      </div>

      <div className="px-2">
        <div className="px-2 pb-1.5 text-[10px] font-semibold uppercase tracking-[0.12em] text-[rgb(var(--muted-2))]">
          Projects
        </div>
        {filtered.map((project) => {
          const selected = selection.type === 'project' && selection.id === project.id
          return (
            <button
              key={project.id}
              onClick={() => setSelection({ type: 'project', id: project.id })}
              className={`bonsai-focus flex w-full items-center gap-2 rounded-md px-2 py-2 text-left transition-colors ${
                selected
                  ? 'bg-[rgb(var(--purple)/.10)] text-[rgb(var(--text))]'
                  : 'text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]'
              }`}
            >
              <span className="h-2 w-2 rounded-full bg-[rgb(var(--green))] shadow-[0_0_0_3px_rgb(var(--green)/.08)]" />
              <span className="min-w-0 flex-1 truncate font-medium">{project.name}</span>
              <span className="text-[10px] text-[rgb(var(--muted-2))]">{project.worktreeIds.length}</span>
            </button>
          )
        })}
      </div>

      <div className="mt-auto border-t border-[rgb(var(--border))] p-2">
        {links.map(({ label, icon: Icon }) => (
          <button
            key={label}
            className="bonsai-focus flex w-full items-center gap-2 rounded-md px-2 py-2 text-left text-[12px] text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))] hover:text-[rgb(var(--text))]"
          >
            <Icon size={14} />
            {label}
          </button>
        ))}
        <div className="mt-2 flex items-center gap-2 rounded-md border border-[rgb(var(--border))] px-2 py-2 text-[11px] text-[rgb(var(--muted-2))]">
          <Boxes size={13} />
          Frontend prototype
        </div>
      </div>
    </aside>
  )
}
