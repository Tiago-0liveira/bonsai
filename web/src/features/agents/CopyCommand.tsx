import { useEffect, useState } from 'react'
import { Check, Copy } from 'lucide-react'

/** A shell command with a copy button; the text stays selectable when the clipboard is unavailable. */
export function CopyCommand({ command, label }: { command: string; label: string }) {
  const [copied, setCopied] = useState(false)
  useEffect(() => {
    if (!copied) return
    const timer = setTimeout(() => setCopied(false), 1500)
    return () => clearTimeout(timer)
  }, [copied])
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(command)
      setCopied(true)
    } catch { /* the command is still selectable */ }
  }
  return (
    <div className="flex items-center gap-2 rounded-[8px] border border-border bg-well py-1 pl-2.5 pr-1">
      <code className="min-w-0 flex-1 select-all break-all font-mono text-[11px] text-text">{command}</code>
      <button type="button" onClick={() => void copy()} aria-label={copied ? 'Copied' : label} title={copied ? 'Copied' : label} className="bonsai-focus btn-ghost h-6 w-6 shrink-0 justify-center px-0">
        {copied ? <Check size={12} className="text-accent" /> : <Copy size={12} />}
      </button>
    </div>
  )
}
