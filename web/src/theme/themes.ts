import type { Theme } from './tokens'

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

export const builtInThemes: readonly Theme[] = [bonsaiTheme]
