import React from 'react'
import ReactDOM from 'react-dom/client'
import '@fontsource-variable/hanken-grotesk'
import '@fontsource-variable/jetbrains-mono'
import './styles/globals.css'
import './styles/surfaces.css'
import { ThemeProvider } from './theme/ThemeProvider'
import { initTheme } from './theme/themeStore'
import { FeatureBoundary, PreloadRecoveryNotice, registerChunkRecovery } from './app/chunkRecovery'

registerChunkRecovery()

const applicationPath = window.location.pathname === '/app' || window.location.pathname.startsWith('/app/')
const Surface = React.lazy(() => applicationPath
  ? import('./app/ApplicationRoot').then(module => ({ default: module.ApplicationRoot }))
  : import('./marketing/LandingPage').then(module => ({ default: module.LandingPage })))

// Apply a stored theme before React's first commit.
initTheme()

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ThemeProvider>
      <PreloadRecoveryNotice />
      <FeatureBoundary loading="Loading Bonsai…">
        <Surface />
      </FeatureBoundary>
    </ThemeProvider>
  </React.StrictMode>,
)
