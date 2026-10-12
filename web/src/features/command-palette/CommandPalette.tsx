import { useEffect, useRef, useState } from 'react'
import { localCommand } from '../../api/git'
import { isLiveAgent } from '../../api/agents'
import { Command } from 'cmdk'
import { Title as DialogTitle, Description as DialogDescription } from '@radix-ui/react-dialog'
import { useNavigate } from '@tanstack/react-router'
import {
  ArrowLeft,
  Check,
  Palette,
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
  TerminalSquare,
  UploadCloud,
  DownloadCloud,
  LocateFixed,
  type LucideIcon,
} from 'lucide-react'
import { useBonsaiStore } from '../../stores/bonsai'
import { setActiveTheme, useActiveTheme, useThemes } from '../../theme/themeStore'

export function CommandPalette() {
  const open = useBonsaiStore((state) => state.paletteOpen)
  const setOpen = useBonsaiStore((state) => state.setPaletteOpen)
  const requestCanvasAction = useBonsaiStore((state) => state.requestCanvasAction)
  const setWorktreeDialogOpen = useBonsaiStore(state => state.setWorktreeDialogOpen)
  const navigate = useNavigate()
  const selection = useBonsaiStore(s => s.selection)
  const agent = useBonsaiStore(s => s.agents.find(a => selection.type === 'agent' && a.id === selection.id))
  const process = useBonsaiStore(s => s.processes.find(p => selection.type === 'process' && p.id === selection.id))
  const openAgent = useBonsaiStore(s => s.openStartAgentDialog)
  const openProcess = useBonsaiStore(s => s.openStartProcessDialog)
  const openTerminal = useBonsaiStore(s => s.openTerminal)
  const setAgentState = useBonsaiStore(s => s.setAgentState)

  const themes = useThemes()
  const activeTheme = useActiveTheme()
  const [page, setPage] = useState<'commands' | 'themes'>('commands')
  const [search, setSearch] = useState('')
  const [selected, setSelected] = useState('')
  const rootSearch = useRef('')
  const input = useRef<HTMLInputElement>(null)

  const goBack = () => {
    setPage('commands')
    setSearch(rootSearch.current)
    setSelected('Change theme…')
    input.current?.focus()
  }

  // Escape is captured before Radix's document handler, so a nested page
  // navigates back instead of dismissing the dialog. The shell handles loading
  // fallbacks; Radix handles Escape at the root and outside-pointer dismissal.
  useEffect(() => {
    if (!open || page !== 'themes') return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' || (event.key === 'Backspace' && !search && event.target === input.current)) {
        event.preventDefault()
        event.stopPropagation()
        setPage('commands')
        setSearch(rootSearch.current)
        setSelected('Change theme…')
        input.current?.focus()
      }
    }
    window.addEventListener('keydown', onKeyDown, true)
    return () => window.removeEventListener('keydown', onKeyDown, true)
  }, [open, page, search])

  // cmdk selects its first result after filtering. Highlight the current theme
  // once the nested list mounts, without applying anything on keyboard focus.
  useEffect(() => {
    if (page !== 'themes') return
    const frame = requestAnimationFrame(() => {
      if (!input.current?.value) setSelected(activeTheme.id)
    })
    return () => cancelAnimationFrame(frame)
  }, [page, activeTheme.id])

  if (!open) return null

  const run = (action: () => void | Promise<void>) => {
    setOpen(false)
    void action()
  }

  return (
    <Command.Dialog open={open} onOpenChange={setOpen} label="Command menu" loop vimBindings={false}
      value={selected} onValueChange={setSelected}
      overlayClassName="fixed inset-0 z-[100] bg-well/70 backdrop-blur-[2px]"
      contentClassName="fixed left-1/2 top-[10vh] z-[101] w-[calc(100%-32px)] max-w-[620px] -translate-x-1/2 overflow-hidden rounded-[14px] border border-border-strong bg-panel shadow-overlay"
    >
        <DialogTitle className="sr-only">Command menu</DialogTitle>
        <DialogDescription className="sr-only">Search commands or choose an appearance theme.</DialogDescription>
        <div className="flex items-center gap-2 border-b border-border-subtle px-3">
          {page === 'themes'
            ? <button type="button" aria-label="Back to commands" className="bonsai-focus btn-ghost h-7 px-1" onClick={goBack}><ArrowLeft size={15} /></button>
            : <Search size={15} className="text-muted-2" />}
          <Command.Input
            ref={input}
            autoFocus
            value={search}
            onValueChange={setSearch}
            placeholder={page === 'themes' ? 'Search themes…' : 'Search commands and views…'}
            className="h-11 flex-1 bg-transparent text-[13px] outline-none placeholder:text-muted-2"
          />
          <span className="bonsai-kbd">Esc</span>
        </div>
        <p className="px-3 py-2 font-mono text-[10px] text-muted-2">
          {page === 'themes' ? 'Appearance · Select a theme to apply it immediately.' : 'Agent profiles support interactive terminals. Generic shells are unavailable.'}
        </p>
        <Command.List className="max-h-[420px] overflow-y-auto p-2">
          <Command.Empty className="p-8 text-center text-[12px] text-muted">
            {page === 'themes' ? 'No theme found.' : 'No command found.'}
          </Command.Empty>

          {page === 'themes' ? <Command.Group heading="Themes" className="[&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:font-mono [&_[cmdk-group-heading]]:text-[10px] [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:text-muted-2">
            {themes.map(theme => <Command.Item key={theme.id} value={theme.id} keywords={[theme.name, theme.mode, 'theme']}
              onSelect={() => run(() => setActiveTheme(theme.id))}
              className="flex h-10 cursor-default select-none items-center gap-3 rounded-[7px] px-2.5 text-[12px] text-muted outline-none aria-selected:bg-accent/[.12] aria-selected:text-text">
              <span aria-hidden="true" className="flex overflow-hidden rounded border border-border">
                {[theme.colors.bg, theme.colors.panel, theme.colors['accent-solid']].map((color, index) => <span key={index} className="h-4 w-3" style={{ backgroundColor: color }} />)}
              </span>
              <span>{theme.name}</span>
              {theme.id === activeTheme.id && <span className="ml-auto inline-flex items-center gap-1 text-[10px] text-muted"><Check size={12} aria-hidden="true" />Current</span>}
            </Command.Item>)}
          </Command.Group> : <>
          <Command.Group heading="Appearance" className="[&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:font-mono [&_[cmdk-group-heading]]:text-[10px] [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:text-muted-2">
            <CommandItem icon={Palette} label="Change theme…" keywords={['appearance', 'colors', 'light', 'dark', ...themes.map(theme => theme.name)]}
              onSelect={() => {
                rootSearch.current = search
                setPage('themes')
                setSearch('')
                setSelected(activeTheme.id)
                input.current?.focus()
              }} />
          </Command.Group>

          <Command.Group heading="Workspace" className="[&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:font-mono [&_[cmdk-group-heading]]:text-[10px] [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-[.12em] [&_[cmdk-group-heading]]:text-muted-2">
            <CommandItem icon={LocateFixed} label="Fit canvas" onSelect={() => run(() => requestCanvasAction('fit'))} />
            <CommandItem icon={Network} label="Auto-layout canvas" onSelect={() => run(() => requestCanvasAction('layout'))} />
            <CommandItem icon={GitBranch} label="Create worktree" onSelect={() => run(() => setWorktreeDialogOpen(true))} />
            <CommandItem icon={Bot} label="Start agent" onSelect={() => run(() => openAgent(selection.type === 'worktree' ? selection.id : agent?.worktreeId))} />
            <CommandItem icon={Play} label="Start process" onSelect={() => run(() => openProcess(selection.type === 'worktree' ? selection.id : agent?.worktreeId ?? process?.worktreeId))} />
            <CommandItem icon={Square} label="Stop agent" disabled={!isLiveAgent(agent) || agent?.state === 'finished'} onSelect={() => run(() => setAgentState(agent!.id, 'finished'))} />
            <CommandItem icon={TerminalSquare} label="Open terminal" disabled={!isLiveAgent(agent)} onSelect={() => run(() => openTerminal(agent!.id))} />
          </Command.Group>

          <Command.Group heading="Commands" className="[&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:font-mono [&_[cmdk-group-heading]]:text-[10px] [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-[.12em] [&_[cmdk-group-heading]]:text-muted-2">
            <CommandItem icon={Play} label="Run pnpm dev" disabled hint="unavailable" />
            <CommandItem icon={Play} label="Run pnpm test" disabled hint="unavailable" />
            <CommandItem icon={Play} label="Run cargo test" disabled hint="unavailable" />
            <CommandItem icon={Play} label="Run make test" disabled hint="unavailable" />
            <CommandItem icon={DownloadCloud} label="Pull" onSelect={() => run(() => localCommand('pull'))} />
            <CommandItem icon={UploadCloud} label="Push" onSelect={() => run(() => localCommand('push'))} />
            <CommandItem icon={GitCommitHorizontal} label="Commit staged changes" onSelect={() => run(() => { const message = window.prompt('Commit message'); if (message?.trim()) void localCommand('commit', { message }) })} />
          </Command.Group>

          <Command.Group heading="Navigate" className="[&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:font-mono [&_[cmdk-group-heading]]:text-[10px] [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-[.12em] [&_[cmdk-group-heading]]:text-muted-2">
            <CommandItem icon={GitPullRequest} label="Open PRs" onSelect={() => run(() => navigate({ to: '/pull-requests' }))} />
            <CommandItem icon={FileCode2} label="Open Files" onSelect={() => run(() => navigate({ to: '/files' }))} />
            <CommandItem icon={Settings} label="Open Settings" onSelect={() => run(() => void navigate({ to: '/settings' }))} />
          </Command.Group>
          </>}
        </Command.List>
    </Command.Dialog>
  )
}

function CommandItem({
  icon: Icon,
  label,
  hint,
  keywords,
  onSelect,
  disabled = false,
}: {
  icon: LucideIcon
  label: string
  hint?: string
  keywords?: string[]
  onSelect?: () => void
  disabled?: boolean
}) {
  return (
    <Command.Item
      value={label}
      keywords={keywords}
      onSelect={onSelect}
      disabled={disabled}
      className="data-[disabled=true]:opacity-40 flex cursor-default select-none items-center gap-2 h-8 rounded-[7px] px-2.5 text-[12px] text-muted outline-none aria-selected:bg-accent/[.12] aria-selected:text-text"
    >
      <Icon size={14} />
      <span>{label}</span>
      {hint && <span className="ml-auto font-mono text-[10px] text-muted-2">{hint}</span>}
    </Command.Item>
  )
}
