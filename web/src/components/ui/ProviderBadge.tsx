import type { AgentProvider } from '../../types'

const LABELS: Record<AgentProvider, string> = {
  Claude: 'CL',
  Antigravity: 'AG',
  Codex: 'CX',
  Gemini: 'GM',
}

/** Placeholder monogram until real provider logos ship as assets. Neutral tokens only. */
export function ProviderBadge({ provider, size = 18 }: { provider: AgentProvider; size?: 16 | 18 | 20 }) {
  return (
    <span
      aria-hidden="true"
      style={{ width: size, height: size, fontSize: size * (8.5 / 18) }}
      className="grid shrink-0 place-items-center rounded-[5px] bg-panel-4 font-mono font-bold text-muted"
    >
      {LABELS[provider]}
    </span>
  )
}
