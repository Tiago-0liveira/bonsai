import { Plus } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { PROVIDER_LABELS, type AgentCapability } from '../../api/agents'
import { accountAddCommand } from '../../api/agentsOverview'
import { ProviderLogo } from '../../components/ui/ProviderLogo'
import type { AgentProviderId } from '../../types'
import { CopyCommand } from './CopyCommand'

const isKnown = (id: string): id is AgentProviderId => Object.hasOwn(PROVIDER_LABELS, id)

/** The CLI command for each provider that can have profiles, or why it cannot yet. */
export function AddProfileCommands({ providers }: { providers: AgentCapability[] }) {
  const known = providers.filter(provider => isKnown(provider.id))
  return (
    <ul className="space-y-3">
      {known.map(provider => (
        <li key={provider.id} className="space-y-1.5">
          <div className="flex items-center gap-2 text-[12px] font-medium text-text">
            <ProviderLogo provider={provider.id} size={14} className="shrink-0" />
            {provider.label}
          </div>
          {provider.available && isKnown(provider.id)
            ? <CopyCommand command={accountAddCommand(provider.id)} label={`Copy ${provider.label} command`} />
            : (
              <div className="space-y-1 text-[11px] text-muted-2">
                <p>{provider.unavailable_reason?.message || 'Not available'}</p>
                {provider.install_hint && <p><code className="select-all font-mono">{provider.install_hint}</code></p>}
              </div>
            )}
        </li>
      ))}
    </ul>
  )
}

/** Until profiles can be created in the browser, this lists the exact CLI commands. */
export function AddProfile({ providers }: { providers: AgentCapability[] }) {
  const [open, setOpen] = useState(false)
  const root = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const close = (event: PointerEvent) => { if (!root.current?.contains(event.target as Node)) setOpen(false) }
    const escape = (event: KeyboardEvent) => { if (event.key === 'Escape') setOpen(false) }
    window.addEventListener('pointerdown', close)
    window.addEventListener('keydown', escape)
    return () => { window.removeEventListener('pointerdown', close); window.removeEventListener('keydown', escape) }
  }, [open])
  return (
    <div ref={root} className="relative">
      <button type="button" aria-haspopup="dialog" aria-expanded={open} onClick={() => setOpen(value => !value)} className="bonsai-focus btn-accent-tint inline-flex h-7 items-center gap-1.5 rounded-[7px] px-2.5 text-[11px] font-medium">
        <Plus size={12} /> Add profile
      </button>
      {open && (
        <div role="dialog" aria-label="Add profile" className="absolute right-0 top-[calc(100%+6px)] z-[60] w-[min(420px,calc(100vw-48px))] space-y-3 rounded-[12px] border border-border-strong bg-panel-3 p-3 shadow-overlay">
          <p className="text-[11px] text-muted">Run one of these in a terminal, then come back; the list updates on its own.</p>
          <AddProfileCommands providers={providers} />
        </div>
      )}
    </div>
  )
}
