import { WorkspaceStorageError } from '../components/layout/WorkspaceStorageError'
import { ProjectRootsSettings } from '../features/settings/ProjectRootsSettings'
import { loadProjectRoots } from '../api/settings'
import { workspacePreferences, useBonsaiStore } from '../stores/bonsai'
import { useEffect, useState, useSyncExternalStore } from 'react'
import { RouterProvider } from '@tanstack/react-router'
import '@xyflow/react/dist/style.css'
import '@xterm/xterm/css/xterm.css'
import { startGitBackend } from '../api/git'
import {
  connectLocalBonsai,
  getLocalConnectionSnapshot,
  resetLocalConnection,
  subscribeLocalConnection,
  type LocalConnectionStatus,
} from '../api/localClient'
import { startRelayInvalidation } from '../api/relayClient'
import { router } from './router'

function statusTitle(status: LocalConnectionStatus) {
  switch (status) {
    case 'requesting-permission': return 'Requesting browser permission…'
    case 'bonsai-not-running': return 'Bonsai is not running on this computer.'
    case 'permission-denied': return 'Local network access was denied.'
    case 'version-incompatible': return 'This Bonsai version is not compatible.'
    case 'unsupported-browser': return 'This browser cannot connect to local Bonsai.'
    case 'connection-lost': return 'The local Bonsai connection was lost.'
    default: return 'Bonsai is not connected on this computer.'
  }
}

function LocalConnectionGate() {
  const connection = useSyncExternalStore(
    subscribeLocalConnection,
    getLocalConnectionSnapshot,
    getLocalConnectionSnapshot,
  )
  const [connecting, setConnecting] = useState(false)

  const connect = async () => {
    setConnecting(true)
    try {
      await connectLocalBonsai()
    } finally {
      setConnecting(false)
    }
  }

  const retryable = !['requesting-permission', 'version-incompatible', 'unsupported-browser'].includes(connection.status)

  return (
    <main className="local-connect" aria-labelledby="local-connect-title">
      <section className="local-connect-card">
        <div className="local-connect-mark" aria-hidden="true">B</div>
        <p className="local-connect-kicker">local workspace</p>
        <h1 id="local-connect-title">{statusTitle(connection.status)}</h1>
        <p className="local-connect-copy">
          {connection.message ?? 'Start Bonsai locally, then connect this browser to the loopback API. The session capability stays in memory and is recreated after a reload.'}
        </p>
        <div className="local-connect-command" aria-label="Command to start Bonsai">
          <span>$</span><code>bonsai serve</code>
        </div>
        <div className="local-connect-actions">
          {retryable && (
            <button type="button" className="local-connect-primary" onClick={connect} disabled={connecting || connection.status === 'requesting-permission'}>
              {connection.status === 'connection-lost' ? 'Reconnect to local Bonsai' : 'Connect to local Bonsai'}
            </button>
          )}
          {connection.status !== 'not-attempted' && connection.status !== 'requesting-permission' && (
            <button type="button" className="local-connect-secondary" onClick={resetLocalConnection}>Reset</button>
          )}
        </div>
        <p className="local-connect-note">
          The browser may ask for Local Network Access when you connect. Bonsai does not request localhost access from the public landing page.
        </p>
      </section>
    </main>
  )
}

function ConnectedApplication() {
  const settings = useBonsaiStore(s => s.rootSettings)
  const [dismissedSetup, setDismissedSetup] = useState<string | null>(null)

  useEffect(() => {
    void loadProjectRoots().catch(() => undefined)
    const stopLocal = startGitBackend()
    const stopRelay = startRelayInvalidation()
    return () => {
      stopRelay()
      stopLocal()
    }
  }, [])

  const setupKey = settings ? `${settings.revision}:${settings.selection_revision}` : ''
  const needsSetup = Boolean(settings && (
    settings.roots.length === 0
    || (settings.repositories.length > 0 && !settings.repositories.some(repository => repository.selected))
  ))

  return <>
    <RouterProvider router={router} />
    {settings && needsSetup && dismissedSetup !== setupKey && <div role="dialog" aria-label="Choose project folders" className="fixed inset-0 z-50 overflow-auto bg-[rgb(var(--bg))]"><ProjectRootsSettings onDismiss={() => setDismissedSetup(setupKey)} /></div>}
  </>
}

export function ApplicationRoot() {
  useEffect(() => {
    const stopLifecycle = workspacePreferences.attachLifecycle()
    const stopNavigation = router.subscribe('onBeforeNavigate', workspacePreferences.flush)
    return () => { stopNavigation(); stopLifecycle() }
  }, [])
  const connection = useSyncExternalStore(
    subscribeLocalConnection,
    getLocalConnectionSnapshot,
    getLocalConnectionSnapshot,
  )
  return (
    <div className="app-surface flex flex-col">
      <WorkspaceStorageError />
      <div className="min-h-0 flex-1">
        {connection.status === 'connected' ? <ConnectedApplication /> : <LocalConnectionGate />}
      </div>
    </div>
  )
}

export default ApplicationRoot
