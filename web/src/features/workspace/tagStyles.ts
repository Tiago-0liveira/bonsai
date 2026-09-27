import type { TagColor, WorktreeTag } from '../../types'

const palette: Record<TagColor, { foreground: string; border: string; background: string }> = {
  purple: { foreground: '#b79cff', border: 'rgba(151,109,255,.42)', background: 'rgba(151,109,255,.12)' },
  blue: { foreground: '#7eb0ff', border: 'rgba(92,157,255,.42)', background: 'rgba(92,157,255,.12)' },
  green: { foreground: '#65dfa0', border: 'rgba(75,214,140,.42)', background: 'rgba(75,214,140,.11)' },
  orange: { foreground: '#f7b969', border: 'rgba(245,166,75,.42)', background: 'rgba(245,166,75,.11)' },
  red: { foreground: '#f58a94', border: 'rgba(243,103,114,.42)', background: 'rgba(243,103,114,.11)' },
  cyan: { foreground: '#72d8df', border: 'rgba(82,199,207,.42)', background: 'rgba(82,199,207,.11)' },
  pink: { foreground: '#e89bd0', border: 'rgba(224,121,190,.42)', background: 'rgba(224,121,190,.11)' },
}

export function getTagPresentation(tag: WorktreeTag | undefined) {
  return palette[tag?.color ?? 'purple']
}
