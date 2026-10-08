import React, { Suspense } from 'react'
import ReactDOM from 'react-dom/client'
import '@fontsource-variable/hanken-grotesk'
import '@fontsource-variable/jetbrains-mono'
import './styles/globals.css'
import './styles/surfaces.css'

const applicationPath = window.location.pathname === '/app' || window.location.pathname.startsWith('/app/')
const Surface = React.lazy(() => applicationPath
  ? import('./app/ApplicationRoot').then(module => ({ default: module.ApplicationRoot }))
  : import('./marketing/LandingPage').then(module => ({ default: module.LandingPage })))

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <Suspense fallback={<div className="surface-loading" aria-live="polite">Loading Bonsai…</div>}>
      <Surface />
    </Suspense>
  </React.StrictMode>,
)
