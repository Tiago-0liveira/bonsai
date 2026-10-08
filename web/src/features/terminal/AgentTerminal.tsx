import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Square, Wifi, WifiOff } from 'lucide-react'
import type { Terminal } from '@xterm/xterm'
import type { FitAddon } from '@xterm/addon-fit'
import { connectAgentTerminal, type TerminalConnection } from '../../api/agentTerminal'
import { stopAgent } from '../../api/agents'
import type { Agent } from '../../types'
import { TERMINAL_FONT, bindTerminalAppearance, terminalTheme } from '../../theme/terminal'
import { getActiveTheme } from '../../theme/themeStore'

export function AgentTerminal({ agent, actionsHost }: { agent: Agent; actionsHost: HTMLElement | null }) {
  const host = useRef<HTMLDivElement>(null)
  const [connection, setConnection] = useState<TerminalConnection>('connecting')
  const [stopping, setStopping] = useState(false)
  const [error, setError] = useState('')
  const [lifecycle, setLifecycle] = useState(agent.lifecycleState ?? 'starting')
  useEffect(() => {
    const element = host.current
    if (!element || !agent.projectId) return
    let terminal: Terminal | undefined
    let fit: FitAddon | undefined
    let controller: ReturnType<typeof connectAgentTerminal> | undefined
    let input: { dispose(): void } | undefined
    let unbind: (() => void) | undefined
    let disposed = false
    const resize = () => {
      if (!terminal || !fit || !element.isConnected || element.closest('[inert]') || element.clientWidth < 2 || element.clientHeight < 2) return
      try { fit.fit(); controller?.resize(terminal.cols, terminal.rows) } catch { /* panel transition */ }
    }
    const observer = new ResizeObserver(resize)
    observer.observe(element)
    // Delay opening until the effect survives StrictMode's initial teardown.
    const frame = requestAnimationFrame(() => {
      void Promise.all([import('@xterm/xterm'), import('@xterm/addon-fit'), document.fonts?.load('11px "JetBrains Mono Variable"').catch(() => undefined)]).then(([xterm, addon]) => {
        if (disposed) return
        terminal = new xterm.Terminal({ cursorBlink: true, fontSize: 11, fontFamily: TERMINAL_FONT, scrollback: 2000, theme: terminalTheme(getActiveTheme()) })
        fit = new addon.FitAddon()
        terminal.loadAddon(fit)
        terminal.open(element)
        unbind = bindTerminalAppearance(terminal, resize)
        resize()
        controller = connectAgentTerminal(agent.projectId!, agent.id, {
          connection: setConnection,
          output: bytes => terminal?.write(bytes),
          reset: gap => { terminal?.reset(); if (gap) terminal?.writeln('[Recent output was truncated; terminal display may be incomplete.]') },
          ready: resize,
          status: summary => { setLifecycle(summary.state); if (summary.state === 'running') resize(); if (summary.error) setError(summary.error) },
          error: setError,
        })
        input = terminal.onData(data => controller?.input(data))
      }).catch(() => { if (!disposed) setError('Unable to load the terminal. Reload and try again.') })
    })
    return () => {
      disposed = true
      cancelAnimationFrame(frame)
      observer.disconnect()
      input?.dispose()
      unbind?.()
      controller?.dispose()
      requestAnimationFrame(() => requestAnimationFrame(() => terminal?.dispose()))
    }
  }, [agent.id, agent.projectId])
  return <div className="flex h-full min-h-0 flex-col">
    {actionsHost && createPortal(<>
      <span title={`Session ${lifecycle}; terminal ${connection}`} className={'flex h-6 items-center gap-1.5 rounded-md border px-2 text-[9px] ' + (connection === 'connected' ? 'border-[rgb(var(--accent)/.2)] bg-[rgb(var(--accent)/.06)] text-[rgb(var(--accent))]' : 'border-[rgb(var(--border))] bg-[rgb(var(--bg))] text-[rgb(var(--muted))]')}>
        {connection === 'connected' ? <Wifi size={10} /> : <WifiOff size={10} />}
        {lifecycle} · {connection}
      </span>
      <button type="button" className="bonsai-focus flex h-6 items-center gap-1.5 rounded-md border border-[rgb(var(--danger)/.25)] bg-[rgb(var(--danger)/.08)] px-2 text-[9px] font-medium text-[rgb(var(--danger))] transition-colors hover:border-[rgb(var(--danger)/.5)] hover:bg-[rgb(var(--danger)/.15)] disabled:cursor-not-allowed disabled:opacity-40" disabled={stopping || lifecycle === 'exited' || lifecycle === 'failed' || lifecycle === 'stopping'} onClick={() => {
        if (!agent.projectId || stopping) return
        setStopping(true)
        void stopAgent(agent.projectId, agent.id, `stop-${agent.id}`).catch(error => setError(String(error.message))).finally(() => setStopping(false))
      }}><Square size={9} fill="currentColor" />{stopping || lifecycle === 'stopping' ? 'Stopping…' : 'Stop agent'}</button>
    </>, actionsHost)}
    {error && <p role="alert" className="px-2 text-xs text-red-400">{error}</p>}
    <div ref={host} className="min-h-0 flex-1 overflow-hidden" />
  </div>
}
