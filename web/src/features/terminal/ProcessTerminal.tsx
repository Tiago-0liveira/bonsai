import { useEffect, useRef, useState } from 'react'
import type { Terminal } from '@xterm/xterm'
import type { FitAddon } from '@xterm/addon-fit'
import type { Process } from '../../types'
import { TERMINAL_FONT, bindTerminalAppearance, terminalTheme } from '../../theme/terminal'
import { getActiveTheme } from '../../theme/themeStore'
import { prepareProcessStream, disposeProcessStream, ProcessMarkerRenderer, type ProcessConnection } from '../../api/processStream'
import { processHistory, restartProcess, stopProcess } from '../../api/processes'

export function ProcessTerminal({ process }: { process: Process }) {
  const host = useRef<HTMLDivElement>(null)
  const [connection, setConnection] = useState<ProcessConnection>('connecting')
  const [pending, setPending] = useState('')
  const [error, setError] = useState('')
  const [attachment, setAttachment] = useState(0)
  useEffect(() => {
    const element = host.current
    if (!element) return
    setConnection('connecting'); setError('')
    let terminal: Terminal | undefined, fit: FitAddon | undefined
    let disposed = false, queued = 0
    let unbind: (() => void) | undefined
    const renderer = new ProcessMarkerRenderer()
    const stream = prepareProcessStream(process.projectId, process.daemonId)
    const resize = () => {
      if (!terminal || !fit || !element.isConnected || element.closest('[inert]') || element.clientWidth < 2 || element.clientHeight < 2) return
      try { fit.fit() } catch { /* panel transition */ }
    }
    const observer = new ResizeObserver(resize)
    observer.observe(element)
    const frame = requestAnimationFrame(() => {
      void Promise.all([import('@xterm/xterm'), import('@xterm/addon-fit'), document.fonts?.load('11px "JetBrains Mono Variable"').catch(() => undefined)]).then(([xterm, addon]) => {
        if (disposed) return
        terminal = new xterm.Terminal({ disableStdin: true, convertEol: true, fontSize: 11, fontFamily: TERMINAL_FONT, scrollback: 5000, theme: terminalTheme(getActiveTheme()) })
        fit = new addon.FitAddon(); terminal.loadAddon(fit); terminal.open(element); unbind = bindTerminalAppearance(terminal, resize); resize()
        stream.attach({
          connection: setConnection, error: setError,
          gap() { renderer.reset(); terminal?.writeln('\r\n[Retained output has a gap; earlier history is unavailable.]') },
          output(bytes) {
            if (queued + bytes.length > 1024 * 1024) return false
            queued += bytes.length
            const output = renderer.render(bytes)
            // xterm preserves viewport position while scrolled up and follows
            // automatically when the viewport is at the bottom.
            terminal?.write(output, () => { queued -= bytes.length })
          },
        })
      }).catch(() => { if (!disposed) setError('Unable to load the process terminal.') })
    })
    return () => {
      disposed = true; cancelAnimationFrame(frame); observer.disconnect(); unbind?.()
      disposeProcessStream(process.projectId, process.daemonId)
      requestAnimationFrame(() => requestAnimationFrame(() => terminal?.dispose()))
    }
  }, [process.id, process.projectId, process.daemonId, attachment])
  const action = async (name: string, run: () => Promise<unknown>) => {
    if (pending) return
    setPending(name); setError('')
    try { await run() } catch (error) { setError(error instanceof Error ? error.message : String(error)) }
    finally { setPending('') }
  }
  const download = async () => {
    const history = await processHistory(process.projectId, process.daemonId)
    const url = URL.createObjectURL(new Blob([history.content], { type: 'text/plain;charset=utf-8' }))
    const anchor = document.createElement('a'); anchor.href = url; anchor.download = `process-${process.daemonId}.log`; anchor.click()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
  }
  const terminalStatus = ['done', 'failed', 'stopped', 'lost'].includes(process.lifecycleStatus)
  return <div data-process-id={process.id} className="flex h-full min-h-0 flex-col">
    {(error || process.exitError) && <p role="alert" className="shrink-0 break-all px-2 pt-1 text-[10px] text-danger">{error || process.exitError}</p>}
    <div ref={host} className="min-h-0 flex-1 overflow-hidden" />
    <div className="flex min-h-[26px] shrink-0 flex-wrap items-center gap-x-3 gap-y-0.5 border-t border-border-subtle px-2 font-mono text-[10px] text-muted-2">
      <span role="status">{process.lifecycleStatus} · {connection}{process.exitCode !== undefined ? ` · exit ${process.exitCode}` : ''}</span>
      <span title={process.retryAt ? `Next retry: ${process.retryAt}` : undefined}>{process.policy?.mode} · retries {process.retryCount ?? 0}/{process.policy?.max_restarts ?? 0} · attempt {process.attempt ?? 1}</span>
      <span className="ml-auto flex flex-wrap items-center gap-1">
        {(connection === 'disconnected' || (connection !== 'connected' && error)) && <button className="bonsai-focus btn-ghost !h-5 px-2 text-[10px] disabled:cursor-not-allowed disabled:opacity-40" onClick={() => setAttachment(value => value + 1)}>Reconnect output</button>}
        <button className="bonsai-focus btn-ghost !h-5 px-2 text-[10px] disabled:cursor-not-allowed disabled:opacity-40" disabled={!!pending || terminalStatus || process.lifecycleStatus === 'stopping'} onClick={() => void action('stop', () => stopProcess(process.projectId, process.daemonId))}>{pending === 'stop' ? 'Stopping…' : 'Stop process'}</button>
        <button className="bonsai-focus btn-ghost !h-5 px-2 text-[10px] disabled:cursor-not-allowed disabled:opacity-40" disabled={!!pending} onClick={() => void action('restart', () => restartProcess(process.projectId, process.daemonId))}>{pending === 'restart' ? 'Restarting…' : 'Restart process'}</button>
        <button className="bonsai-focus btn-ghost !h-5 px-2 text-[10px] disabled:cursor-not-allowed disabled:opacity-40" disabled={!!pending} onClick={() => void action('history', download)}>Download retained logs</button>
      </span>
    </div>
  </div>
}
