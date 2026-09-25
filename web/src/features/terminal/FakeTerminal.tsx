import { useEffect, useRef } from 'react'
import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'
import { useBonsaiStore } from '../../stores/bonsai'

const EMPTY_LINES: string[] = []

export function FakeTerminal() {
  const hostRef = useRef<HTMLDivElement | null>(null)
  const terminalRef = useRef<Terminal | null>(null)
  const fitRef = useRef<FitAddon | null>(null)
  const activeTerminalId = useBonsaiStore((state) => state.activeTerminalId)
  const terminalLines = useBonsaiStore((state) => state.terminalOutput[activeTerminalId])
  const lines = terminalLines ?? EMPTY_LINES

  useEffect(() => {
    const host = hostRef.current
    if (!host) return

    const terminal = new Terminal({
      cursorBlink: true,
      fontFamily: 'JetBrains Mono, SFMono-Regular, Consolas, monospace',
      fontSize: 12,
      lineHeight: 1.35,
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

    terminal.loadAddon(fit)
    terminal.open(host)
    terminalRef.current = terminal
    fitRef.current = fit

    const fitSafely = () => {
      if (disposed || !host.isConnected || terminalRef.current !== terminal) return
      try {
        fit.fit()
      } catch {
        // The viewport may be between mount/unmount phases while the dock resizes.
      }
    }

    const observer = new ResizeObserver(() => fitSafely())
    observer.observe(host)
    const frame = requestAnimationFrame(fitSafely)

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
    const terminal = terminalRef.current
    const fit = fitRef.current
    if (!terminal || !fit) return

    terminal.reset()
    lines.forEach((line) => terminal.writeln(line))

    const frame = requestAnimationFrame(() => {
      if (terminalRef.current !== terminal || fitRef.current !== fit) return
      try {
        fit.fit()
        terminal.scrollToBottom()
      } catch {
        // Ignore transient xterm resize races while switching worktrees.
      }
    })

    return () => cancelAnimationFrame(frame)
  }, [activeTerminalId, lines])

  return <div ref={hostRef} className="h-full min-h-[120px] w-full bg-[#0c0e11]" />
}
