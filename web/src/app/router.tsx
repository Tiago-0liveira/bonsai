import { SettingsPage } from '../features/settings/SettingsPage'
import {
  Outlet,
  createRootRoute,
  createRoute,
  createRouter,
  lazyRouteComponent,
} from '@tanstack/react-router'
import { AppShell } from '../components/layout/AppShell'
import { FilesPage } from '../features/files/FilesPage'
import { validatePullRequestsSearch } from '../features/github/pullRequestsSearch'
import { LogsPage } from '../features/logs/LogsPage'
import { WorkspacePage } from '../features/workspace/WorkspacePage'

const rootRoute = createRootRoute({
  component: () => (
    <AppShell>
      <Outlet />
    </AppShell>
  ),
})

const workspaceRoute = createRoute({ getParentRoute: () => rootRoute, path: '/', component: () => <WorkspacePage /> })
const worktreesRoute = createRoute({ getParentRoute: () => rootRoute, path: '/worktrees', component: () => <WorkspacePage focus="worktrees" /> })
const agentsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/agents', component: () => <WorkspacePage focus="agents" /> })
// Loaded on demand so the markdown libraries stay out of the main bundle.
const PullRequestsRoute = lazyRouteComponent(() => import('../features/github/PullRequestsRoute'), 'PullRequestsRoute')
const githubRoute = createRoute({ getParentRoute: () => rootRoute, path: '/github', component: PullRequestsRoute, validateSearch: validatePullRequestsSearch })
const prsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/pull-requests', component: PullRequestsRoute, validateSearch: validatePullRequestsSearch })
const filesRoute = createRoute({ getParentRoute: () => rootRoute, path: '/files', component: FilesPage })
const logsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/logs', component: LogsPage })

const settingsRoute = createRoute({ getParentRoute: () => rootRoute, path: "/settings", component: SettingsPage })

const routeTree = rootRoute.addChildren([
  settingsRoute, workspaceRoute, worktreesRoute, agentsRoute, githubRoute, prsRoute, filesRoute, logsRoute,
])

export const router = createRouter({ routeTree, basepath: '/app' })

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}
