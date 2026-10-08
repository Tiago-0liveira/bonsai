import type { HexColor } from './tokens'

const HEX = /^#[0-9a-f]{6}$/i

export function isHexColor(value: unknown): value is HexColor {
  return typeof value === 'string' && HEX.test(value)
}

function channels(hex: string): [number, number, number] {
  return [1, 3, 5].map(index => parseInt(hex.slice(index, index + 2), 16)) as [number, number, number]
}

/** '#1c110b' -> '28 17 11', the space-separated form used by `rgb(var(--token) / alpha)`. */
export function hexToTriplet(hex: string): string {
  return channels(hex).join(' ')
}

/** WCAG 2.x relative luminance. */
export function relativeLuminance(hex: string): number {
  const [r, g, b] = channels(hex).map(channel => {
    const value = channel / 255
    return value <= 0.03928 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4
  }) as [number, number, number]
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

/** WCAG 2.x contrast ratio, 1 to 21. */
export function contrastRatio(a: string, b: string): number {
  const [light, dark] = [relativeLuminance(a), relativeLuminance(b)].sort((x, y) => y - x) as [number, number]
  return (light + 0.05) / (dark + 0.05)
}
