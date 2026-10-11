import { Component, Suspense, useEffect, useRef, useSyncExternalStore, type ReactNode } from 'react'

let preloadFailed = false
const listeners = new Set<() => void>()
function notify() { for (const listener of listeners) listener() }
export function acknowledgePreloadFailure() { preloadFailed = false; notify() }

// Install in the eager entry before any surface imports. Keep Vite's rejection:
// React boundaries (and terminal effect handlers) must still receive the error.
export function registerChunkRecovery() {
  const onPreloadError = () => { preloadFailed = true; notify() }
  window.addEventListener('vite:preloadError', onPreloadError)
  return () => window.removeEventListener('vite:preloadError', onPreloadError)
}

export function ChunkRecovery() {
  return <div role="alert" className="island flex h-full min-h-[100px] flex-col items-center justify-center gap-3 p-4 text-sm">
    <p>This screen could not load</p>
    <p className="text-xs text-muted">Reload to get the current app files, then try again.</p>
    <button type="button" className="bonsai-focus btn-primary" onClick={() => window.location.reload()}>Reload app</button>
  </div>
}

// Covers imports outside React rendering, including library imports in effects.
// A feature boundary acknowledges a failure once its own recovery is visible.
export function PreloadRecoveryNotice() {
  const failed = useSyncExternalStore(listener => {
    listeners.add(listener)
    return () => { listeners.delete(listener) }
  }, () => preloadFailed, () => false)
  return failed ? <aside className="fixed right-4 top-4 z-[200] max-w-sm"><ChunkRecovery /></aside> : null
}

class FeatureErrorBoundary extends Component<{ children: ReactNode; recovery?: ReactNode }, { failed: boolean }> {
  state = { failed: false }
  static getDerivedStateFromError() { return { failed: true } }
  componentDidCatch() { acknowledgePreloadFailure() }
  render() { return this.state.failed ? this.props.recovery ?? <ChunkRecovery /> : this.props.children }
}

export function FeatureBoundary({ children, loading, fallback, recovery }: { children: ReactNode; loading: string; fallback?: ReactNode; recovery?: ReactNode }) {
  return <FeatureErrorBoundary recovery={recovery}>
    <Suspense fallback={fallback ?? <div role="status" className="grid h-full min-h-[100px] place-items-center p-4 text-sm text-muted">{loading}</div>}>
      {children}
    </Suspense>
  </FeatureErrorBoundary>
}

function OverlayFallback({ children, onDismiss }: { children: ReactNode; onDismiss: () => void }) {
  return <div role="dialog" aria-label="Loading screen" aria-modal="true" className="fixed inset-0 z-[150] grid place-items-center bg-well/70 p-6">
    <div className="island flex max-w-md flex-col gap-3 p-4">
      {children}
      <button type="button" autoFocus className="bonsai-focus btn-bordered" onClick={onDismiss}>Close</button>
    </div>
  </div>
}

export function OverlayBoundary({ children, loading, onDismiss }: { children: ReactNode; loading: string; onDismiss: () => void }) {
  const trigger = useRef(document.activeElement)
  const mounted = useRef(false)
  useEffect(() => {
    mounted.current = true
    const element = trigger.current
    return () => {
      mounted.current = false
      // StrictMode immediately sets up the effect again; restore only on close.
      queueMicrotask(() => {
        if (!mounted.current && element instanceof HTMLElement && element.isConnected) element.focus()
      })
    }
  }, [])
  return <FeatureBoundary loading={loading}
    fallback={<OverlayFallback onDismiss={onDismiss}><p role="status">{loading}</p></OverlayFallback>}
    recovery={<OverlayFallback onDismiss={onDismiss}><ChunkRecovery /></OverlayFallback>}
  >{children}</FeatureBoundary>
}
