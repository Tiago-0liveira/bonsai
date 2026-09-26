import { useEffect } from 'react'
import { Command } from 'cmdk'
import { useNavigate } from '@tanstack/react-router'
import {
  Bot,
  GitBranch,
  FileCode2,
  GitCommitHorizontal,
  GitPullRequest,
  Network,
  Play,
  Search,
  Settings,
  Square,
  Table2,
  TerminalSquare,
  UploadCloud,
  DownloadCloud,
  LocateFixed,
  type LucideIcon,
} from 'lucide-react'
import { useBonsaiStore } from '../../stores/bonsai'

export function CommandPalette() {
  const open = useBonsaiStore((state) => state.paletteOpen)
  const setOpen = useBonsaiStore((state) => state.setPaletteOpen)
  const requestCanvasAction = useBonsaiStore((state) => state.requestCanvasAction)
  const createMockWorktree = useBonsaiStore((state) => state.createMockWorktree)
  const startMockAgent = useBonsaiStore((state) => state.startMockAgent)
  const selection = useBonsaiStore((state) => state.selection)
  const agents = useBonsaiStore((state) => state.agents)
  const setAgentState = useBonsaiStore((state) => state.setAgentState)
  const openTerminal = useBonsaiStore((state) => state.openTerminal)
  const appendTerminalCommand = useBonsaiStore((state) => state.appendTerminalCommand)
  const setNotice = useBonsaiStore((state) => state.setNotice)
  const navigate = useNavigate()

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault()
        setOpen(!open)
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [open, setOpen])

  if (!open) return null

  const stopAgent = () => {
    const agent =
      selection.type === 'agent'
        ? agents.find((item) => item.id === selection.id)
        : agents.find((item) => item.state === 'running')
    if (agent) setAgentState(agent.id, 'finished')
  }

  const run = (action: () => void | Promise<void>) => {
    setOpen(false)
    void action()
  }

  return (
    <div
      className="fixed inset-0 z-[100] bg-black/55 p-[10vh_16px] backdrop-blur-[2px]"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) setOpen(false)
      }}
    >
      <Command className="mx-auto w-full max-w-[620px] overflow-hidden rounded-lg border border-[rgb(var(--border-strong))] bg-[rgb(var(--panel))] shadow-2xl">
        <div className="flex items-center gap-2 border-b border-[rgb(var(--border))] px-3">
          <Search size={15} className="text-[rgb(var(--muted-2))]" />
          <Command.Input
            autoFocus
            placeholder="Search commands, views, and mock actions…"
            className="h-11 flex-1 bg-transparent text-[13px] outline-none placeholder:text-[rgb(var(--muted-2))]"
          />
          <span className="bonsai-kbd">Esc</span>
        </div>
        <Command.List className="max-h-[420px] overflow-y-auto p-2">
          <Command.Empty className="p-8 text-center text-[12px] text-[rgb(var(--muted))]">
            No command found.
          </Command.Empty>

          <Command.Group heading="Workspace" className="[&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-[10px] [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-[.12em] [&_[cmdk-group-heading]]:text-[rgb(var(--muted-2))]">
            <CommandItem icon={LocateFixed} label="Fit canvas" onSelect={() => run(() => requestCanvasAction('fit'))} />
            <CommandItem icon={Network} label="Auto-layout canvas" onSelect={() => run(() => requestCanvasAction('layout'))} />
            <CommandItem icon={GitBranch} label="Create worktree" onSelect={() => run(createMockWorktree)} />
            <CommandItem icon={Bot} label="Start agent" onSelect={() => run(startMockAgent)} />
            <CommandItem icon={Square} label="Stop agent" onSelect={() => run(stopAgent)} />
            <CommandItem icon={TerminalSquare} label="Open terminal" onSelect={() => run(() => openTerminal(selection.type === 'agent' ? selection.id : undefined))} />
          </Command.Group>

          <Command.Group heading="Commands" className="[&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-[10px] [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-[.12em] [&_[cmdk-group-heading]]:text-[rgb(var(--muted-2))]">
            <CommandItem icon={Play} label="Run pnpm dev" hint="mock" onSelect={() => run(() => appendTerminalCommand('pnpm dev'))} />
            <CommandItem icon={Play} label="Run pnpm test" hint="mock" onSelect={() => run(() => appendTerminalCommand('pnpm test'))} />
            <CommandItem icon={Play} label="Run cargo test" hint="mock" onSelect={() => run(() => appendTerminalCommand('cargo test'))} />
            <CommandItem icon={Play} label="Run make test" hint="mock" onSelect={() => run(() => appendTerminalCommand('make test'))} />
            <CommandItem icon={DownloadCloud} label="Pull" hint="mock" onSelect={() => run(() => appendTerminalCommand('git pull'))} />
            <CommandItem icon={UploadCloud} label="Push" hint="mock" onSelect={() => run(() => appendTerminalCommand('git push'))} />
            <CommandItem icon={GitCommitHorizontal} label="Commit" hint="mock" onSelect={() => run(() => appendTerminalCommand('git commit -m "prototype update"'))} />
          </Command.Group>

          <Command.Group heading="Navigate" className="[&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-[10px] [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-[.12em] [&_[cmdk-group-heading]]:text-[rgb(var(--muted-2))]">
            <CommandItem icon={GitPullRequest} label="Open PRs" onSelect={() => run(() => navigate({ to: '/pull-requests' }))} />
            <CommandItem icon={Table2} label="Open Tables" onSelect={() => run(() => navigate({ to: '/tables' }))} />
            <CommandItem icon={FileCode2} label="Open Files" onSelect={() => run(() => navigate({ to: '/files' }))} />
            <CommandItem icon={Settings} label="Open Settings" hint="mock" onSelect={() => run(() => setNotice('Settings are mocked in this prototype'))} />
          </Command.Group>
        </Command.List>
      </Command>
    </div>
  )
}

function CommandItem({
  icon: Icon,
  label,
  hint,
  onSelect,
}: {
  icon: LucideIcon
  label: string
  hint?: string
  onSelect: () => void
}) {
  return (
    <Command.Item
      value={label}
      onSelect={onSelect}
      className="flex cursor-default select-none items-center gap-2 rounded-md px-2.5 py-2 text-[12px] text-[rgb(var(--muted))] outline-none data-[selected=true]:bg-[rgb(var(--purple)/.12)] data-[selected=true]:text-[rgb(var(--text))]"
    >
      <Icon size={14} />
      <span>{label}</span>
      {hint && <span className="ml-auto text-[10px] text-[rgb(var(--muted-2))]">{hint}</span>}
    </Command.Item>
  )
}
