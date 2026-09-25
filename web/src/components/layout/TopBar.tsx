import { Link } from '@tanstack/react-router'
import { Bell, Command, Search, Sprout } from 'lucide-react'
import { useBonsaiStore } from '../../stores/bonsai'

const nav = [
  { label: 'Workspace', to: '/' },
  { label: 'Worktrees', to: '/worktrees' },
  { label: 'Agents', to: '/agents' },
  { label: 'GitHub', to: '/github' },
  { label: 'Tables', to: '/tables' },
  { label: 'Logs', to: '/logs' },
] as const

export function TopBar() {
  const setPaletteOpen = useBonsaiStore((state) => state.setPaletteOpen)

  return (
    <header className="flex h-11 shrink-0 items-center border-b border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-3">
      <Link to="/" className="flex items-center gap-2 pr-4 font-semibold tracking-tight">
        <span className="grid h-6 w-6 place-items-center rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--panel-2))]">
          <Sprout size={14} className="text-[rgb(var(--green))]" />
        </span>
        <span>bonsai</span>
        <span className="text-[rgb(var(--muted-2))]">/</span>
        <span className="max-w-40 truncate font-normal text-[rgb(var(--muted))]">bonsai</span>
      </Link>

      <nav className="top-nav-secondary flex h-full items-center gap-1">
        {nav.map((item) => (
          <Link
            key={item.label}
            to={item.to}
            className="relative flex h-full items-center px-2.5 text-[12px] text-[rgb(var(--muted))] transition-colors hover:text-[rgb(var(--text))]"
            activeProps={{
              className:
                "relative flex h-full items-center px-2.5 text-[12px] text-[rgb(var(--text))] after:absolute after:bottom-0 after:left-2 after:right-2 after:h-px after:bg-[rgb(var(--purple))]",
            }}
          >
            {item.label}
          </Link>
        ))}
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
