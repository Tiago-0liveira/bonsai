import {
  Outlet,
  createRootRoute,
  createRoute,
  createRouter,
} from '@tanstack/react-router'
import { AppShell } from '../components/layout/AppShell'
import { BoardPage } from '../features/board/BoardPage'
import { FilesPage } from '../features/files/FilesPage'
import { PullRequestsPage } from '../features/github/PullRequestsPage'
import { LogsPage } from '../features/logs/LogsPage'
import { WorkspacePage } from '../features/workspace/WorkspacePage'

const rootRoute = createRootRoute({
  component: () => (
    <AppShell>
      <Outlet />
    </AppShell>
  ),
})

const workspaceRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  component: () => <WorkspacePage />,
})

const worktreesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/worktrees',
  component: () => <WorkspacePage focus="worktrees" />,
})

const agentsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/agents',
  component: () => <WorkspacePage focus="agents" />,
})

const githubRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/github',
  component: PullRequestsPage,
})

const prsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/pull-requests',
  component: PullRequestsPage,
})

const tablesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/tables',
  component: BoardPage,
})

const filesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/files',
  component: FilesPage,
})

const logsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/logs',
  component: LogsPage,
})

const routeTree = rootRoute.addChildren([
  workspaceRoute,
  worktreesRoute,
  agentsRoute,
  githubRoute,
  prsRoute,
  tablesRoute,
  filesRoute,
  logsRoute,
])

export const router = createRouter({ routeTree })

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}
