import type { Config } from 'tailwindcss'
import { THEME_TOKENS } from './src/theme/tokens'

const token = (name: string) => `rgb(var(--${name}) / <alpha-value>)`

export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    // Replaces Tailwind's default palette so only theme tokens can be used.
    colors: {
      transparent: 'transparent',
      current: 'currentColor',
      inherit: 'inherit',
      ...Object.fromEntries(THEME_TOKENS.map((name) => [name, token(name)])),
    },
    extend: {
      fontFamily: {
        sans: ['"Hanken Grotesk Variable"', '"Hanken Grotesk"', 'ui-sans-serif', 'system-ui', 'sans-serif'],
        mono: ['"JetBrains Mono Variable"', '"JetBrains Mono"', 'ui-monospace', 'SFMono-Regular', 'Consolas', 'monospace'],
      },
      borderColor: { DEFAULT: token('border') },
      ringColor: { DEFAULT: token('accent') },
      boxShadow: {
        island: 'var(--shadow-island)',
        topbar: 'var(--shadow-topbar)',
        card: 'var(--shadow-card)',
        overlay: 'var(--shadow-overlay)',
      },
    },
  },
  plugins: [],
} satisfies Config
