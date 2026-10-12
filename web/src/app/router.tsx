import { lazy } from 'react'
import { FeatureBoundary } from './chunkRecovery'
import {
  Outlet,
  createRootRoute,
  createRoute,
  createRouter,
} from '@tanstack/react-router'
import { AppShell } from '../components/layout/AppShell'
import { validatePullRequestsSearch } from '../features/github/pullRequestsSearch'
import { validateAgentsSearch } from '../features/agents/agentsSearch'

const WorkspacePage = lazy(() => import('../features/workspace/WorkspacePage').then(module => ({ default: module.WorkspacePage })))
const FilesPage = lazy(() => import('../features/files/FilesPage').then(module => ({ default: module.FilesPage })))
const LogsPage = lazy(() => import('../features/logs/LogsPage').then(module => ({ default: module.LogsPage })))
const SettingsPage = lazy(() => import('../features/settings/SettingsPage').then(module => ({ default: module.SettingsPage })))
const AgentsRoute = lazy(() => import('../features/agents/AgentsRoute').then(module => ({ default: module.AgentsRoute })))
const PullRequestsRoute = lazy(() => import('../features/github/PullRequestsRoute').then(module => ({ default: module.PullRequestsRoute })))

const rootRoute = createRootRoute({
  component: () => (
    <AppShell>
      <Outlet />
    </AppShell>
  ),
})

const workspaceRoute = createRoute({ getParentRoute: () => rootRoute, path: '/', component: () => <FeatureBoundary loading="Loading workspace…"><WorkspacePage /></FeatureBoundary> })
const worktreesRoute = createRoute({ getParentRoute: () => rootRoute, path: '/worktrees', component: () => <FeatureBoundary loading="Loading workspace…"><WorkspacePage focus="worktrees" /></FeatureBoundary> })
const agentsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/agents', component: () => <FeatureBoundary loading="Loading agents…"><AgentsRoute /></FeatureBoundary>, validateSearch: validateAgentsSearch })
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
