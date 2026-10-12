import { lazy } from 'react'
import { FeatureBoundary } from './chunkRecovery'
import {
  Outlet,
  createRootRoute,
  createRoute,
  createRouter,
  useRouterState,
} from '@tanstack/react-router'
import { AppShell } from '../components/layout/AppShell'
import type { CanvasRoute } from '../features/workspace/WorkspacePage'
import { validatePullRequestsSearch } from '../features/github/pullRequestsSearch'

const FilesPage = lazy(() => import('../features/files/FilesPage').then(module => ({ default: module.FilesPage })))
const LogsPage = lazy(() => import('../features/logs/LogsPage').then(module => ({ default: module.LogsPage })))
const SettingsPage = lazy(() => import('../features/settings/SettingsPage').then(module => ({ default: module.SettingsPage })))
const PullRequestsRoute = lazy(() => import('../features/github/PullRequestsRoute').then(module => ({ default: module.PullRequestsRoute })))

const CANVAS_ROUTES: Record<string, CanvasRoute> = { '/': 'canvas', '/worktrees': 'worktrees', '/agents': 'agents' }

function RootLayout() {
  // The canvas is a shell layer, not a page: canvas routes only tell the shell which focus to apply.
  const canvasRoute = useRouterState({ select: state => state.matches.map(match => CANVAS_ROUTES[match.routeId]).find(Boolean) ?? null })
  return (
    <AppShell canvasRoute={canvasRoute}>
      <Outlet />
    </AppShell>
  )
}

const rootRoute = createRootRoute({ component: RootLayout })

const workspaceRoute = createRoute({ getParentRoute: () => rootRoute, path: '/', component: () => null })
const worktreesRoute = createRoute({ getParentRoute: () => rootRoute, path: '/worktrees', component: () => null })
const agentsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/agents', component: () => null })
const githubRoute = createRoute({ getParentRoute: () => rootRoute, path: '/github', component: () => <FeatureBoundary loading="Loading pull requests…"><PullRequestsRoute /></FeatureBoundary>, validateSearch: validatePullRequestsSearch })
const prsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/pull-requests', component: () => <FeatureBoundary loading="Loading pull requests…"><PullRequestsRoute /></FeatureBoundary>, validateSearch: validatePullRequestsSearch })
const filesRoute = createRoute({ getParentRoute: () => rootRoute, path: '/files', component: () => <FeatureBoundary loading="Loading files…"><FilesPage /></FeatureBoundary> })
const logsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/logs', component: () => <FeatureBoundary loading="Loading logs…"><LogsPage /></FeatureBoundary> })

const settingsRoute = createRoute({ getParentRoute: () => rootRoute, path: "/settings", component: () => <FeatureBoundary loading="Loading settings…"><SettingsPage /></FeatureBoundary> })

const routeTree = rootRoute.addChildren([
  settingsRoute, workspaceRoute, worktreesRoute, agentsRoute, githubRoute, prsRoute, filesRoute, logsRoute,
])

export const router = createRouter({ routeTree, basepath: '/app' })

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}
