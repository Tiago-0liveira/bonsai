import { useEffect, type ReactNode } from 'react'
import { watchThemeStorage } from './themeStore'

/** Re-reads stored preferences when another tab changes them. Hooks read the module store, so there is no context. */
export function ThemeProvider({ children }: { children: ReactNode }) {
  useEffect(() => watchThemeStorage(), [])
  return <>{children}</>
}
