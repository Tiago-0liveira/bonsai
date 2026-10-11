import { useId } from 'react'
import { Bot } from 'lucide-react'

/**
 * Inline marks optimized once with SVGO 4 (convertPathData/cleanupNumericValues,
 * precision 2). No runtime asset requests. Licenses: ./logos/NOTICE.md.
 * Claude: simple-icons@16.34.0, CC0; coral #D97757.
 * https://github.com/simple-icons/simple-icons/blob/16.34.0/icons/claude.svg
 * Gemini: simple-icons@16.34.0, CC0; brand blue #3186FF (display only).
 * https://github.com/simple-icons/simple-icons/blob/16.34.0/icons/googlegemini.svg
 * Codex: @lobehub/icons-static-svg@1.95.1, MIT; currentColor.
 * https://github.com/lobehub/lobe-icons/blob/master/packages/static-svg/icons/codex.svg
 * Antigravity: @lobehub/icons-static-svg@1.95.1, MIT; mono path + Google gradient.
 * https://github.com/lobehub/lobe-icons/blob/master/packages/static-svg/icons/antigravity.svg
 * Pinned source blob SHAs (Git objects, not commit refs):
 * Codex 4c648aff4a9188c36d32a706f1aa7813b8992f2c;
 * Antigravity 784fc63dbedff371173225c050cabdd72aae2762.
 */
export const CLAUDE_CORAL = '#D97757'

const MARKS: Record<string, { viewBox: string; path: string; fill: string }> = {
  claude: { viewBox: '0 0 24 24', fill: CLAUDE_CORAL, path: 'm4.71 15.96 4.72-2.65.08-.23-.08-.13H9.2l-.79-.05-2.7-.07-2.33-.1-2.27-.12-.57-.12-.53-.7.05-.36.48-.32.69.06 1.52.1 2.27.16 1.66.1 2.44.25h.4l.05-.15-.14-.1-.1-.1-2.36-1.6-2.55-1.68-1.33-.97-.73-.5L2 6.22l-.16-1 .66-.73.88.06.22.06.9.69 1.9 1.48L8.9 8.6l.36.3.14-.1.02-.07-.16-.28L7.9 6.02l-1.44-2.5L5.8 2.5l-.17-.62c-.06-.26-.1-.47-.1-.73L6.29.13 6.7 0l1 .13.41.37.62 1.41 1 2.23 1.56 3.03.45.9.25.83.09.26h.16V9l.12-1.7.24-2.1.23-2.7.08-.76.38-.9.74-.5.59.28.48.69-.07.44-.29 1.85-.55 2.9-.37 1.95h.21l.25-.25.98-1.3 1.65-2.07.73-.81.85-.9.55-.44h1.03l.76 1.13-.34 1.16-1.06 1.35-.89 1.14-1.26 1.7-.79 1.36.08.11.18-.02 2.86-.6 1.54-.28 1.84-.32.83.4.1.39-.34.8-1.96.49-2.31.46-3.44.81-.04.03.05.07 1.55.14.66.04h1.62l3.02.22.79.52.47.64-.08.49-1.21.62-1.64-.4-3.83-.9-1.3-.33h-.19v.1l1.1 1.08 2 1.8 2.5 2.34.13.57-.32.46-.34-.05-2.2-1.66-.85-.74-1.93-1.62h-.13v.17l.45.65 2.34 3.52.12 1.08-.17.35-.6.21-.67-.12-1.38-1.92-1.41-2.17-1.14-1.94-.14.08-.68 7.25-.31.37-.73.28-.6-.46-.33-.75.32-1.47.4-1.93.3-1.53.3-1.9.16-.63v-.04l-.15.02-1.43 1.96-2.18 2.95-1.73 1.84-.4.17-.72-.37.06-.66.4-.6 2.39-3.03 1.44-1.88.93-1.09v-.16h-.06l-6.34 4.12-1.13.15-.49-.46.06-.75.23-.24 1.91-1.31Z' },
  codex: { viewBox: '0 0 24 24', fill: 'currentColor', path: 'M8.09.46a6 6 0 0 1 3.04-.42q2 .24 3.57 1.7a.1.1 0 0 0 .1.03 6 6 0 0 1 4.06.37l.07.03.15.07a6.1 6.1 0 0 1 3.34 5.33 6 6 0 0 1-.18 1.63.2.2 0 0 0 .04.15 6 6 0 0 1 1.58 2.9 6 6 0 0 1-1.19 5.13l-.18.22a6 6 0 0 1-2.93 1.85.2.2 0 0 0-.1.1A6.05 6.05 0 0 1 13.52 24a6 6 0 0 1-4.22-1.73.14.14 0 0 0-.14-.03c-.52.17-1.04.2-1.6.19a6 6 0 0 1-2.6-.63 6 6 0 0 1-2.15-1.78q-.31-.39-.55-.82a8 8 0 0 1-.5-1.28 6 6 0 0 1-.01-3.06v-.08l-.03-.06a6 6 0 0 1-1.38-2.2 5 5 0 0 1-.33-1.6A7 7 0 0 1 .2 8.79a6.3 6.3 0 0 1 3.38-3.93q.44-.18.86-.3a.1.1 0 0 0 .1-.09 6 6 0 0 1 1.1-2.15A6 6 0 0 1 8.09.46m-.8 7.85a.85.85 0 0 0-1.48.84l1.7 2.96-1.7 2.85a.85.85 0 0 0 1.47.87l1.94-3.28a.9.9 0 0 0 0-.85zm5.44 6.24a.85.85 0 0 0 0 1.7h4.85a.85.85 0 0 0 0-1.7z' },
  antigravity: { viewBox: '0 0 24 24', fill: 'gradient', path: 'M21.75 22.6c1.34 1.01 3.35.34 1.5-1.5C17.74 15.74 18.9 1 12.05 1S6.34 15.74.82 21.1c-2.01 2 .16 2.51 1.5 1.5 5.2-3.51 4.86-9.7 9.72-9.7 4.85 0 4.52 6.19 9.71 9.7' },
  gemini: { viewBox: '0 0 24 24', fill: '#3186FF', path: 'M11.04 19.32Q12 21.51 12 24q0-2.49.93-4.68.96-2.19 2.58-3.81t3.81-2.55Q21.51 12 24 12q-2.49 0-4.68-.93a12 12 0 0 1-3.81-2.58 12 12 0 0 1-2.58-3.81Q12 2.49 12 0q0 2.49-.96 4.68-.93 2.19-2.55 3.81a12 12 0 0 1-3.81 2.58Q2.49 12 0 12q2.49 0 4.68.96 2.19.93 3.81 2.55t2.55 3.81' },
}

/** Accepts both display labels and API IDs; unknown providers get a neutral icon. */
export function ProviderLogo({ provider, size = 16, className, title }: {
  provider: string
  size?: number
  className?: string
  title?: string
}) {
  const id = useId()
  const key = provider.toLowerCase()
  const mark = Object.hasOwn(MARKS, key) ? MARKS[key] : undefined
  const gradientId = `${id}-gradient`
  const accessibility = {
    'aria-hidden': title ? undefined : true,
    role: title ? 'img' : undefined,
    'aria-labelledby': title ? `${id}-title` : undefined,
    focusable: false,
  } as const
  const label = title ? <title id={`${id}-title`}>{title}</title> : null
  if (!mark) return <Bot {...accessibility} size={size} className={className} data-provider-logo="unknown">{label}</Bot>
  return (
    <svg {...accessibility} viewBox={mark.viewBox} width={size} height={size} className={className}
      data-provider-logo={key} fill={mark.fill === 'gradient' ? `url(#${gradientId})` : mark.fill} fillRule="evenodd">
      {label}
      {mark.fill === 'gradient' && (
        <defs>
          <linearGradient id={gradientId} x1="0%" y1="0%" x2="100%" y2="100%">
            <stop offset="0%" stopColor="#3186FF" />
            <stop offset="33%" stopColor="#00B95C" />
            <stop offset="67%" stopColor="#FBBC04" />
            <stop offset="100%" stopColor="#FC413D" />
          </linearGradient>
        </defs>
      )}
      <path d={mark.path} />
    </svg>
  )
}
