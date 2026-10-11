import { ProviderLogo } from './ProviderLogo'

/** A small provider mark on the shared neutral tile. */
export function ProviderBadge({ provider, size = 18 }: { provider: string; size?: 16 | 18 | 20 }) {
  return (
    <span
      aria-hidden="true"
      style={{ width: size, height: size }}
      className="grid shrink-0 place-items-center rounded-[5px] bg-panel-4 text-text"
    >
      <ProviderLogo provider={provider} size={Math.round(size * 0.7)} />
    </span>
  )
}
