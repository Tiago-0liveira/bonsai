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

const Surface = React.lazy(() => import('./app/ApplicationRoot').then(module => ({ default: module.ApplicationRoot })))

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
