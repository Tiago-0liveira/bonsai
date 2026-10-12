import type { HexColor, Theme } from './tokens'

export const DEFAULT_THEME_ID = 'bonsai'

export const bonsaiTheme: Theme = {
  id: DEFAULT_THEME_ID,
  name: 'Bonsai',
  mode: 'dark',
  colors: {
    bg: '#1c110b',
    well: '#160c07',
    panel: '#251913',
    'panel-2': '#291d17',
    'panel-3': '#342721',
    'panel-4': '#3f322b',
    'border-subtle': '#2f211a',
    border: '#3d271d',
    'border-strong': '#4a3328',
    text: '#f5ded4',
    muted: '#bccabb',
    'muted-2': '#869486',
    faint: '#6b5348',
    accent: '#6bfb9a',
    'accent-solid': '#4ade80',
    'accent-fg': '#00210c',
    ok: '#a4f0cd',
    warn: '#ffb694',
    'warn-solid': '#d97746',
    danger: '#ffb4ab',
    'danger-solid': '#93000a',
  },
}

/** Keep named terminal colors independent of the UI's blue action accent. */
function modernTerminal(colors: Theme['colors'], magenta: HexColor, cyan: HexColor): Theme['terminal'] {
  return {
    black: colors['muted-2'], brightBlack: colors.muted,
    red: colors.danger, brightRed: colors.danger,
    green: colors.ok, brightGreen: colors.ok,
    yellow: colors.warn, brightYellow: colors.warn,
    blue: colors.accent, brightBlue: colors.accent,
    magenta: magenta, brightMagenta: magenta,
    cyan: cyan, brightCyan: cyan,
    white: colors.muted, brightWhite: colors.text,
  }
}

const modernLightColors: Theme['colors'] = {
  'bg': '#f4f4f5',
  'well': '#ededf0',
  'panel': '#ffffff',
  'panel-2': '#fafafa',
  'panel-3': '#f4f4f5',
  'panel-4': '#e4e4e7',
  'border-subtle': '#e4e4e7',
  'border': '#d4d4d8',
  'border-strong': '#a1a1aa',
  'text': '#18181b',
  'muted': '#52525b',
  'muted-2': '#62626c',
  'faint': '#71717a',
  'accent': '#1d4ed8',
  'accent-solid': '#2563eb',
  'accent-fg': '#ffffff',
  'ok': '#166534',
  'warn': '#92400e',
  'warn-solid': '#b45309',
  'danger': '#991b1b',
  'danger-solid': '#b91c1c',
}

export const modernLightTheme: Theme = {
  id: 'modern-light',
  name: 'Modern Light',
  mode: 'light',
  colors: modernLightColors,
  terminal: modernTerminal(modernLightColors, '#7c3aed', '#0e7490'),
}

const modernDarkColors: Theme['colors'] = {
  'bg': '#09090b',
  'well': '#050507',
  'panel': '#18181b',
  'panel-2': '#1f1f23',
  'panel-3': '#27272a',
  'panel-4': '#3f3f46',
  'border-subtle': '#27272a',
  'border': '#3f3f46',
  'border-strong': '#52525b',
  'text': '#fafafa',
  'muted': '#d4d4d8',
  'muted-2': '#a1a1aa',
  'faint': '#94949e',
  'accent': '#60a5fa',
  'accent-solid': '#2563eb',
  'accent-fg': '#ffffff',
  'ok': '#4ade80',
  'warn': '#fbbf24',
  'warn-solid': '#b45309',
  'danger': '#fca5a5',
  'danger-solid': '#991b1b',
}

export const modernDarkTheme: Theme = {
  id: 'modern-dark',
  name: 'Modern Dark',
  mode: 'dark',
  colors: modernDarkColors,
  terminal: modernTerminal(modernDarkColors, '#c084fc', '#22d3ee'),
}

export const builtInThemes: readonly Theme[] = [bonsaiTheme, modernLightTheme, modernDarkTheme]
