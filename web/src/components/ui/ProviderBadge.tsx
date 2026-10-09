import type { AgentProvider } from '../../types'
import { ClaudeLogo } from './ClaudeLogo'

const LABELS: Record<AgentProvider, string> = {
  Claude: 'CL',
  Antigravity: 'AG',
  Codex: 'CX',
  Gemini: 'GM',
}

/** Claude gets its real mark; other providers keep a neutral monogram until their logos ship. */
export function ProviderBadge({ provider, size = 18 }: { provider: AgentProvider; size?: 16 | 18 | 20 }) {
  return (
    <span
      aria-hidden="true"
      style={{ width: size, height: size, fontSize: size * (8.5 / 18) }}
      className="grid shrink-0 place-items-center rounded-[5px] bg-panel-4 font-mono font-bold text-muted"
    >
      {provider === 'Claude' ? <ClaudeLogo size={Math.round(size * 0.7)} /> : LABELS[provider]}
    </span>
  )
}
