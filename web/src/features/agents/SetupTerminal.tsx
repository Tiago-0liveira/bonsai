import '@xterm/xterm/css/xterm.css'
import { useEffect, useRef, useState } from 'react'
import type { IBufferLine, Terminal } from '@xterm/xterm'
import type { FitAddon } from '@xterm/addon-fit'
import { acknowledgePreloadFailure, ChunkRecovery } from '../../app/chunkRecovery'
import { connectTerminal, setupTerminalPath, type TerminalConnection } from '../../api/agentTerminal'
import type { SetupJob } from '../../api/agentSetup'
import { TERMINAL_FONT, bindTerminalAppearance, terminalTheme } from '../../theme/terminal'
import { getActiveTheme } from '../../theme/themeStore'
import { terminalUrls } from './terminalLinks'

const openUrl = (url: string) => { window.open(url, '_blank', 'noopener,noreferrer') }

/** Makes sign-in URLs in the output clickable, including ones that wrap over several rows. */
function registerUrlLinks(terminal: Terminal) {
  return terminal.registerLinkProvider({
    provideLinks(row, callback) {
      const buffer = terminal.buffer.active
      let first = row
      while (first > 1 && buffer.getLine(first - 1)?.isWrapped) first--
      const rows: string[] = []
      for (let y = first; ; y++) {
        const line: IBufferLine | undefined = buffer.getLine(y - 1)
        if (!line || (y > first && !line.isWrapped)) break
        rows.push(line.translateToString(false))
      }
      if (rows.length) rows[rows.length - 1] = rows[rows.length - 1].trimEnd()
      const links = terminalUrls(rows, terminal.cols, first)
        .filter(link => link.start.y <= row && link.end.y >= row)
        .map(link => ({ range: { start: link.start, end: link.end }, text: link.url, activate: () => openUrl(link.url) }))
      callback(links.length ? links : undefined)
    },
  })
}

/**
 * The terminal of a setup job: the provider's login commands run here. Status
 * frames carry the job, which the dialog follows.
 */
export function SetupTerminal({ jobId, onStatus }: { jobId: string; onStatus: (job: SetupJob) => void }) {
  const host = useRef<HTMLDivElement>(null)
  const status = useRef(onStatus)
  status.current = onStatus
  const [connection, setConnection] = useState<TerminalConnection>('connecting')
  const [error, setError] = useState('')
  const [loadFailed, setLoadFailed] = useState(false)
  useEffect(() => {
    const element = host.current
    if (!element) return
    let terminal: Terminal | undefined
    let fit: FitAddon | undefined
    let controller: ReturnType<typeof connectTerminal<SetupJob>> | undefined
    const disposables: { dispose(): void }[] = []
    let unbind: (() => void) | undefined
    let disposed = false
    const resize = () => {
      if (!terminal || !fit || !element.isConnected || element.clientWidth < 2 || element.clientHeight < 2) return
      try { fit.fit(); controller?.resize(terminal.cols, terminal.rows) } catch { /* dialog transition */ }
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
        disposables.push(registerUrlLinks(terminal))
        unbind = bindTerminalAppearance(terminal, resize)
        resize()
        controller = connectTerminal<SetupJob>(setupTerminalPath(jobId), {
          connection: setConnection,
          output: bytes => terminal?.write(bytes),
          reset: gap => { terminal?.reset(); if (gap) terminal?.writeln('[Earlier output was truncated.]') },
          ready: () => { setError(''); resize() },
          status: job => status.current(job),
          error: setError,
        })
        disposables.push(terminal.onData(data => controller?.input(data)))
      }).catch(() => { if (!disposed) { setLoadFailed(true); acknowledgePreloadFailure() } })
    })
    return () => {
      disposed = true
      cancelAnimationFrame(frame)
      observer.disconnect()
      for (const item of disposables) item.dispose()
      unbind?.()
      controller?.dispose()
      requestAnimationFrame(() => requestAnimationFrame(() => terminal?.dispose()))
    }
  }, [jobId])
  if (loadFailed) return <ChunkRecovery />
  return (
    <div className="flex min-h-0 flex-col gap-1">
      <div ref={host} data-testid="setup-terminal" aria-label="Sign-in terminal" className="h-[260px] min-h-0 overflow-hidden rounded-[10px] border border-border bg-well p-1.5" />
      <div className="flex items-center gap-2 font-mono text-[10px] text-muted-2">
        <span>{connection === 'connected' ? 'Terminal connected' : connection === 'read-only' ? 'Read only: this sign-in is open in another tab' : connection === 'connecting' ? 'Connecting…' : 'Reconnecting…'}</span>
        {error && <span role="alert" className="text-danger">{error}</span>}
      </div>
    </div>
  )
}
