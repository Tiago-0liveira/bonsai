import { useEffect, useRef } from 'react'
import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'
import { useBonsaiStore } from '../../stores/bonsai'

const EMPTY_LINES: string[] = []

export function FakeTerminal({ terminalId }: { terminalId?: string }) {
  const hostRef = useRef<HTMLDivElement | null>(null)
  const terminalRef = useRef<Terminal | null>(null)
  const fitRef = useRef<FitAddon | null>(null)
  const activeTerminalId = useBonsaiStore((state) => state.activeTerminalId)
  const resolvedTerminalId = terminalId ?? activeTerminalId
  const terminalLines = useBonsaiStore((state) => state.terminalOutput[resolvedTerminalId])
  const lines = terminalLines ?? EMPTY_LINES
  const linesRef = useRef(lines)

  useEffect(() => {
    const host = hostRef.current
    if (!host) return

    const terminal = new Terminal({
      cursorBlink: true,
      fontFamily: 'JetBrains Mono, SFMono-Regular, Consolas, monospace',
      fontSize: 10,
      lineHeight: 1.25,
      theme: {
        background: '#0c0e11',
        foreground: '#d7d9df',
        cursor: '#976dff',
        black: '#0c0e11',
        brightBlack: '#626774',
        green: '#4bd68c',
        blue: '#5c9dff',
        yellow: '#f5a64b',
        red: '#f36772',
        magenta: '#976dff',
      },
      allowTransparency: false,
      scrollback: 2000,
    })
    const fit = new FitAddon()
    let disposed = false

    const fitSafely = () => {
      if (disposed || !host.isConnected || terminalRef.current !== terminal) return
      try {
        fit.fit()
      } catch {
        // Ignore transient xterm layout races while panels are resizing.
      }
    }

    const observer = new ResizeObserver(() => fitSafely())
    // StrictMode can dispose an effect before xterm's deferred viewport setup runs.
    // Open only once the host survives to a frame, so that setup has a live renderer.
    const frame = requestAnimationFrame(() => {
      if (disposed || !host.isConnected) return
      terminal.loadAddon(fit)
      terminal.open(host)
      terminalRef.current = terminal
      fitRef.current = fit
      linesRef.current.forEach((line) => terminal.writeln(line))
      observer.observe(host)
      fitSafely()
    })

    return () => {
      disposed = true
      cancelAnimationFrame(frame)
      observer.disconnect()
      terminalRef.current = null
      fitRef.current = null
      terminal.dispose()
    }
  }, [])

  useEffect(() => {
    linesRef.current = lines
    const terminal = terminalRef.current
    const fit = fitRef.current
    if (!terminal || !fit) return
    terminal.write('\x1b[2J\x1b[3J\x1b[H')
    lines.forEach((line) => terminal.writeln(line))
    const frame = requestAnimationFrame(() => {
      if (terminalRef.current !== terminal || fitRef.current !== fit) return
      try {
        fit.fit()
        terminal.scrollToBottom()
      } catch {
        // Ignore transient xterm resize races.
      }
    })
    return () => cancelAnimationFrame(frame)
  }, [resolvedTerminalId, lines])

  return <div ref={hostRef} className="h-full min-h-0 w-full overflow-hidden bg-[#0c0e11]" />
}
