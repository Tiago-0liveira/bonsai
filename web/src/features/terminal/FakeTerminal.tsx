import { useEffect, useRef } from 'react'
import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'
import { useBonsaiStore } from '../../stores/bonsai'

export function FakeTerminal() {
  const hostRef = useRef<HTMLDivElement | null>(null)
  const activeTerminalId = useBonsaiStore((state) => state.activeTerminalId)
  const lines = useBonsaiStore((state) => state.terminalOutput[activeTerminalId] ?? [])

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
    terminal.loadAddon(fit)
    terminal.open(host)

    const render = () => {
      terminal.reset()
      lines.forEach((line) => terminal.writeln(line))
      fit.fit()
      terminal.scrollToBottom()
    }

    render()
    const observer = new ResizeObserver(() => fit.fit())
    observer.observe(host)

    return () => {
      observer.disconnect()
      terminal.dispose()
    }
  }, [activeTerminalId, lines])

  return <div ref={hostRef} className="h-full min-h-[120px] w-full bg-[#0c0e11]" />
}
