import type { AgentSummary } from '../src/api/agents'
import type { Locator, Page } from '@playwright/test'

type Metadata = {
  merge_target_branch: string
  stack_preference: 'auto' | 'never'
}

const repositories = [
  { id: 'bonsai', workspace_id: 'personal', full_name: 'Tiago-0liveira/bonsai', default_branch: 'main' },
  { id: 'sprout-lab', workspace_id: 'personal', full_name: 'Tiago-0liveira/sprout-lab', default_branch: 'main' },
]

const pullRequests = [
  {
    number: 24,
    title: 'feat(web): prototype visual workspace',
    body: 'Build the visual workspace prototype.',
    state: 'open',
    draft: false,
    head: 'feat/web-workspace',
    base: 'main',
    head_sha: 'sha-pr-24',
    author: 'Tiago',
    created_at: '2026-09-24T10:00:00Z',
    updated_at: '2026-09-27T00:00:00Z',
    mergeable: 'mergeable',
    comments: [],
    reviews: [],
    commits: [{ sha: '10c8c2e', message: 'test(web): cover board state', author: 'Test runner', created_at: '2026-09-27T00:00:00Z' }],
    files: [{ path: 'web/src/stores/bonsai.ts', additions: 10, deletions: 2, patch: '+ test fixture' }],
  },
  {
    number: 25,
    title: 'docs: workspace branch relationships',
    body: 'Document nested worktrees.',
    state: 'open',
    draft: true,
    head: 'feat/workspace-docs',
    base: 'feat/web-workspace',
    head_sha: 'sha-pr-25',
    author: 'UI builder',
    created_at: '2026-09-26T10:00:00Z',
    updated_at: '2026-09-27T00:00:00Z',
    mergeable: 'mergeable',
    comments: [],
    reviews: [],
    commits: [{ sha: 'a741bd1', message: 'docs: describe nested worktrees', author: 'UI builder', created_at: '2026-09-27T00:00:00Z' }],
    files: [{ path: 'docs/worktrees.md', additions: 20, deletions: 1, patch: '+ nested merge targets' }],
  },
  {
    number: 23,
    title: 'fix(daemon): stabilize lifecycle cleanup',
    body: 'Make daemon shutdown cleanup deterministic.',
    state: 'open',
    draft: true,
    head: 'fix/daemon-lifecycle',
    base: 'main',
    head_sha: 'sha-pr-23',
    author: 'Tiago',
    created_at: '2026-09-25T10:00:00Z',
    updated_at: '2026-09-27T00:00:00Z',
    mergeable: 'conflicting',
    comments: [],
    reviews: [{ author: 'Review pass', body: 'The cleanup path needs one deterministic assertion.', submitted_at: '2026-09-27T00:00:00Z' }],
    commits: [
      { sha: '3e91e15', message: 'fix: clear stale process markers', author: 'Tiago', created_at: '2026-09-26T22:00:00Z' },
      { sha: '8b264aa', message: 'test: reproduce interrupted shutdown', author: 'Lifecycle fix', created_at: '2026-09-26T23:00:00Z' },
    ],
    files: [{ path: 'internal/daemon/server/proc.go', additions: 38, deletions: 14, patch: '+ deterministic cleanup' }],
  },
  {
    number: 22,
    title: 'chore: tighten release automation',
    body: 'Verify snapshot artifacts.',
    state: 'open',
    draft: false,
    head: 'chore/release-automation',
    base: 'main',
    head_sha: 'sha-pr-22',
    author: 'Tiago',
    created_at: '2026-09-23T10:00:00Z',
    updated_at: '2026-09-27T00:00:00Z',
    mergeable: 'mergeable',
    comments: [],
    reviews: [],
    commits: [{ sha: 'f20bc71', message: 'chore: verify snapshot artifacts', author: 'Tiago', created_at: '2026-09-27T00:00:00Z' }],
    files: [{ path: '.github/workflows/release.yml', additions: 18, deletions: 7, patch: '+ verification' }],
  },
  {
    number: 21,
    title: 'review: canvas density',
    body: 'Review canvas density.',
    state: 'closed',
    draft: false,
    head: 'review/canvas-density',
    base: 'chore/release-automation',
    head_sha: 'sha-pr-21',
    author: 'Review pass',
    created_at: '2026-09-22T10:00:00Z',
    updated_at: '2026-09-27T00:00:00Z',
    mergeable: 'mergeable',
    comments: [],
    reviews: [],
    commits: [],
    files: [],
  },
]

const checks: Record<string, { name: string; status: string; conclusion: string }[]> = {
  'sha-pr-23': [
    { name: 'go test ./...', status: 'completed', conclusion: 'failure' },
    { name: 'windows', status: 'completed', conclusion: 'success' },
  ],
  'sha-pr-24': [{ name: 'frontend', status: 'completed', conclusion: 'success' }],
  'sha-pr-25': [{ name: 'docs', status: 'completed', conclusion: 'success' }],
  'sha-pr-22': [{ name: 'release tests', status: 'completed', conclusion: 'success' }],
  'sha-pr-21': [],
}

function status(ahead: number, behind: number, files: number, gitState: string) {
  return {
    ahead,
    behind,
    divergence_available: true,
    staged: 0,
    modified: files,
    untracked: 0,
    files: Array.from({ length: files }, (_, index) => ({ path: `file-${index}.ts`, status: ' M' })),
    git_state: gitState,
    dirty: files > 0,
    last_commit: { when: '8m ago', subject: 'test fixture commit', sha: 'abc1234' },
  }
}

function ciRollup(values: { status: string; conclusion: string }[]) {
  if (!values.length) return 'none'
  if (values.some(value => value.status !== 'completed')) return 'running'
  if (values.some(value => !['success', 'neutral', 'skipped'].includes(value.conclusion))) return 'failed'
  return 'passed'
}

function bonsaiSnapshot(metadata: Record<string, Metadata>) {
  const repository = repositories[0]
  const associations: Array<[string, (typeof pullRequests)[number] | undefined]> = [
    ['wt-web', pullRequests.find(pr => pr.number === 24)],
    ['wt-docs', pullRequests.find(pr => pr.number === 25)],
    ['wt-daemon', pullRequests.find(pr => pr.number === 23)],
    ['wt-release', pullRequests.find(pr => pr.number === 22)],
    ['wt-review', pullRequests.find(pr => pr.number === 21)],
  ]
  const worktreeState = Object.fromEntries(associations.map(([id, pull]) => {
    const values = pull ? (checks[pull.head_sha] ?? []) : []
    return [id, {
      ...(pull ? { pull_request: { ...pull, head_repository: repository.full_name } } : {}),
      ci: {
        status: ciRollup(values),
        checked_sha: pull?.head_sha,
        checks: values,
        freshness: { state: 'ready', updated_at: '2026-09-27T00:00:00Z' },
      },
    }]
  }))
  return {
    epoch: 'e2e-epoch',
    repository,
    online: true,
    sequence: 1,
    metadata,
    local: {
      branches: [
        { name: 'main', remote: false },
        { name: 'feat/web-workspace', remote: false },
        { name: 'feat/workspace-docs', remote: false },
        { name: 'fix/daemon-lifecycle', remote: false },
        { name: 'chore/release-automation', remote: false },
        { name: 'review/canvas-density', remote: false },
      ],
      worktrees: [
        { id: 'wt-main', repository_id: 'bonsai', branch: 'main', main: true, local_head_sha: 'main', status: status(0, 0, 0, 'clean') },
        { id: 'wt-web', repository_id: 'bonsai', branch: 'feat/web-workspace', main: false, local_head_sha: 'web', status: status(7, 0, 3, '3 modified') },
        { id: 'wt-docs', repository_id: 'bonsai', branch: 'feat/workspace-docs', main: false, local_head_sha: 'docs', status: status(2, 0, 1, '1 modified') },
        { id: 'wt-daemon', repository_id: 'bonsai', branch: 'fix/daemon-lifecycle', main: false, local_head_sha: 'daemon', status: status(3, 1, 2, '1 conflict') },
        { id: 'wt-release', repository_id: 'bonsai', branch: 'chore/release-automation', main: false, local_head_sha: 'release', status: status(2, 2, 0, 'clean') },
        { id: 'wt-review', repository_id: 'bonsai', branch: 'review/canvas-density', main: false, local_head_sha: 'review', status: status(0, 1, 0, 'clean') },
      ],
    },
    remote: {
      repository,
      branches: [],
      pull_requests: pullRequests.map(pr => ({ ...pr, head_repository: repository.full_name })),
      updated_at: '2026-09-27T00:00:00Z',
    },
    freshness: {
      local: { state: 'ready', updated_at: '2026-09-27T00:00:00Z' },
      processes: { state: 'ready', updated_at: '2026-09-27T00:00:00Z' },
      provider: { state: 'ready', updated_at: '2026-09-27T00:00:00Z' },
    },
    worktree_state: worktreeState,
    process_visibility: { cutoffs: {}, deleted: {} },
    processes: [
      { id: 'bonsai:1', daemon_id: 1, project_id: 'bonsai', worktree_id: 'wt-web', label: 'Vite', command: 'pnpm dev', status: 'running', pid: 1001, expected_port: 5173 },
      { id: 'bonsai:2', daemon_id: 2, project_id: 'bonsai', worktree_id: 'wt-daemon', label: 'bonsaid', command: 'go run . daemon', status: 'backoff', pid: 1002 },
    ],
  }
}

function emptySnapshot(projectId: string) {
  const repository = repositories.find((item) => item.id === projectId)!
  return {
    epoch: 'e2e-epoch',
    repository,
    online: true,
    sequence: 1,
    metadata: {},
    local: { branches: [{ name: 'main', remote: false }], worktrees: [] },
    remote: { repository, branches: [], pull_requests: [], updated_at: '2026-09-27T00:00:00Z' },
    freshness: {
      local: { state: 'ready', updated_at: '2026-09-27T00:00:00Z' },
      processes: { state: 'ready', updated_at: '2026-09-27T00:00:00Z' },
      provider: { state: 'ready', updated_at: '2026-09-27T00:00:00Z' },
    },
    worktree_state: {},
    process_visibility: { cutoffs: {}, deleted: {} },
    processes: [],
  }
}

type MockWireSnapshot = Record<string, unknown>

// The default fixture serves the UI and API from one loopback origin.
export const DEFAULT_API_ORIGIN = 'http://127.0.0.1:7011'

const escapeRegExp = (value: string) => value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
const webSocketOrigin = (origin: string) => origin.replace(/^http/, 'ws')
// Paths the Go API answers with the embedded UI rather than JSON.
const isUIPath = (path: string) => path === '/' || path === '/app' || path.startsWith('/app/') || path.startsWith('/assets/')

export async function mockLocalEventSocket(
  page: Page,
  bootstrap: () => { projects: typeof repositories; snapshots: MockWireSnapshot[] } = () => ({ projects: [], snapshots: [] }),
  delayedUpdate?: () => MockWireSnapshot | undefined,
  apiOrigin = DEFAULT_API_ORIGIN,
) {
  let sendEvent: ((event: Record<string, unknown>) => void) | undefined

  const publishCatalog = (projects: typeof repositories, snapshots: MockWireSnapshot[] = []) => {
    sendEvent?.({ type: 'catalog', epoch: 'e2e-epoch', projects })
    for (const snapshot of snapshots) {
      const repository = snapshot.repository as { id?: string } | undefined
      sendEvent?.({
        type: 'project_update',
        project_id: repository?.id,
        component: 'local',
        epoch: 'e2e-epoch',
        sequence: snapshot.sequence,
        snapshot,
      })
    }
  }

  const publishUpdate = (snapshot: MockWireSnapshot, component = 'local') => {
    const repository = snapshot.repository as { id?: string } | undefined
    sendEvent?.({
      type: 'project_update',
      project_id: repository?.id,
      component,
      epoch: 'e2e-epoch',
      sequence: snapshot.sequence,
      snapshot,
    })
  }

  await page.routeWebSocket(`${webSocketOrigin(apiOrigin)}/events`, socket => {
    sendEvent = event => socket.send(JSON.stringify(event))
    socket.onMessage(message => {
      try {
        const payload = JSON.parse(String(message)) as { type?: string }
        if (payload.type === 'authenticate') {
          sendEvent?.({ type: 'ready', epoch: 'e2e-epoch' })
          const initial = bootstrap()
          sendEvent?.({ type: 'catalog', epoch: 'e2e-epoch', projects: initial.projects })
          for (const snapshot of initial.snapshots) {
            const repository = snapshot.repository as { id?: string } | undefined
            sendEvent?.({
              type: 'project_snapshot',
              project_id: repository?.id,
              epoch: 'e2e-epoch',
              sequence: snapshot.sequence,
              snapshot,
            })
          }
          sendEvent?.({ type: 'bootstrap_complete', epoch: 'e2e-epoch' })
          if (delayedUpdate) {
            setTimeout(() => {
              const snapshot = delayedUpdate()
              if (snapshot) publishUpdate(snapshot, 'provider')
            }, 1_000)
          }
        }
      } catch {
        // Invalid fixture messages are ignored just like production events.
      }
    })
  })

  // Global events such as agents_changed carry no snapshot.
  const publishEvent = (event: Record<string, unknown>) => sendEvent?.(event)

  return { publishCatalog, publishUpdate, publishEvent }
}

/** Fixture data for the Agents tab: the default fixture keeps the profiles other specs rely on. */
export interface AgentsTabFixture {
  /** Replaces profiles, providers and sessions with the Agents tab fixture. */
  enabled: boolean
  /** Provider cleanup warnings the API returns when a profile is removed. */
  removeWarnings?: string[]
  /** Answers setup routes with `501 terminal_unsupported`, as on native Windows. */
  setupUnsupported?: boolean
}

/** Sign-in methods the Agents tab fixture providers declare, as the Go providers do. */
const SETUP_SPECS: Record<string, Record<string, unknown>> = {
  antigravity: { notes: ['Exit agy after signing in to save the profile'] },
  claude: { auth_modes: [
    { value: 'login', label: 'Browser sign-in', help: 'Sign in with your Claude account in a browser', recommended: true },
    { value: 'token', label: 'Setup token', help: 'A long-lived token from claude setup-token', secret: { label: 'Claude setup token', placeholder: 'sk-ant-oat01-…', optional: true } },
  ], seed: { source: '~/.claude' } },
  codex: { auth_modes: [
    { value: 'chatgpt', label: 'Browser sign-in', help: 'Sign in with ChatGPT in a browser on this computer' },
    { value: 'device', label: 'Device code', help: 'Sign in on any device with a one-time code', recommended: true },
    { value: 'api-key', label: 'API key', help: 'Use an OpenAI API key', secret: { label: 'OpenAI API key', placeholder: 'sk-…' } },
  ], seed: { source: '~/.codex' } },
}

/** A scripted sign-in: what the login prints, then how it ends. */
interface SetupScript { output: string; end: 'succeed' | 'fail' | 'prompt'; error?: string; /** How long the login runs after printing. */ holdMs?: number }
interface MockSetupJob { job: Record<string, unknown>; script: SetupScript; send?: (frame: Record<string, unknown>) => void; ran: boolean }

/** Usage of the Agents tab fixture profiles, keyed by profile ID. */
const AGENTS_TAB_USAGE: Record<string, Record<string, unknown>> = {
  'codex-personal': {
    account_id: 'codex-personal', provider: 'codex',
    usage: { provider: 'codex', account_id: 'codex-personal', fetched_at: '2026-10-12T11:58:00Z', plan: 'plus', limits: [] },
    headline: {
      primary: { id: 'five_hour', label: '5 hour', window: '5h', remaining_fraction: 0.72, resets_at: '2026-10-12T14:15:00Z' },
      secondary: { id: 'weekly', label: 'Weekly', window: 'weekly', remaining_fraction: 0.38, resets_at: '2026-10-15T16:00:00Z' },
      extra: [
        { id: 'spark_five_hour', group: 'Codex Spark', label: '5 hour', remaining_fraction: 0.9, resets_at: '2026-10-12T14:30:00Z' },
        { id: 'spark_weekly', group: 'Codex Spark', label: 'Weekly', remaining_fraction: 0.64, resets_at: '2026-10-15T16:00:00Z' },
      ],
      score: 0.38, next_reset: '2026-10-12T14:15:00Z',
    },
  },
  'codex-team': { account_id: 'codex-team', provider: 'codex', error: { code: 'not_authenticated', message: 'Not signed in' } },
  'claude-profile': {
    account_id: 'claude-profile', provider: 'claude',
    usage: { provider: 'claude', account_id: 'claude-profile', fetched_at: '2026-10-12T11:59:00Z', plan: 'max', limits: [] },
    headline: {
      primary: { id: 'five_hour', label: '5 hour', window: '5h', remaining_fraction: 0.15, resets_at: '2026-10-12T13:00:00Z' },
      secondary: { id: 'weekly', label: 'Weekly', window: 'weekly', remaining_fraction: 0.55, resets_at: '2026-10-16T09:00:00Z' },
      extra: [
        { id: 'seven_day_opus', label: 'Opus (weekly)', remaining_fraction: 0.4, resets_at: '2026-10-16T09:00:00Z' },
        { id: 'seven_day_sonnet', label: 'Sonnet (weekly)', remaining_fraction: 0.8, resets_at: '2026-10-16T09:00:00Z' },
      ],
      score: 0.15, low: true, next_reset: '2026-10-12T13:00:00Z',
    },
  },
  'claude-token': { account_id: 'claude-token', provider: 'claude', error: { code: 'unsupported', message: 'Setup tokens cannot read plan usage' } },
}

export async function mockGitBackend(page: Page, emptyRoots = false, delayedProvider = false, management = false, apiOrigin = DEFAULT_API_ORIGIN, agentsTab: AgentsTabFixture = { enabled: false }) {
  const rootSettings: {
    version: number
    revision: number
    selection_revision: number
    roots: { id: string; path: string }[]
    diagnostics: unknown[]
    suggestions: string[]
    repositories: { id: string; root_id: string; name: string; path: string; selected: boolean; available: boolean }[]
  } = {
    version: 1,
    revision: 0,
    selection_revision: 0,
    roots: emptyRoots ? [] : [{ id: 'root-fixture', path: '/projects' }],
    diagnostics: [],
    suggestions: ['/projects'],
    repositories: [],
  }
  const discoveredRepositories = (selectedIds = new Set<string>()) => rootSettings.roots.length
    ? repositories.map(repository => ({
        id: repository.id,
        root_id: rootSettings.roots[0].id,
        name: repository.full_name.split('/').at(-1) ?? repository.id,
        path: `${rootSettings.roots[0].path}/${repository.id}`,
        selected: selectedIds.has(repository.id),
        available: true,
      }))
    : []
  if (!emptyRoots) rootSettings.repositories = discoveredRepositories(new Set(repositories.map(repository => repository.id)))

  const agentSessions: AgentSummary[] = agentsTab.enabled ? [
    { id: 'codex-session-1', project_id: 'bonsai', worktree_id: 'wt-daemon', account_id: 'codex-personal', provider: 'codex', profile_name: 'Personal', name: 'Daemon cleanup', state: 'running', created_at: '2026-10-12T11:50:00Z', model: 'gpt-6.1-sol', options: { effort: 'high', sandbox: 'workspace-write', approval: 'on-request' } },
    { id: 'codex-session-lab', project_id: 'sprout-lab', worktree_id: 'wt-lab', account_id: 'codex-personal', provider: 'codex', profile_name: 'Personal', name: 'Lab experiment', state: 'running', created_at: '2026-10-12T11:40:00Z' },
    { id: 'codex-session-done', project_id: 'bonsai', worktree_id: 'wt-web', account_id: 'codex-personal', provider: 'codex', profile_name: 'Personal', name: 'Docs pass', state: 'exited', created_at: '2026-10-12T09:00:00Z', ended_at: '2026-10-12T09:30:00Z', exit_code: 0 },
    { id: 'claude-session-1', project_id: 'bonsai', worktree_id: 'wt-main', account_id: 'claude-profile', provider: 'claude', profile_name: 'Work', name: 'Release notes', state: 'running', created_at: '2026-10-12T11:55:00Z', options: { permission_mode: 'plan' } },
  ] : []
  // The ETag of the profile list changes when a test reports a profile change.
  let accountsETag = 1
  let agentsRevision = 0
  const usageRequests: { method: string; account_id?: string }[] = []
  const profileRequests: { method: string; id: string; body: unknown }[] = []
  // Every setup request as the page sent it; only a start or a secret answer may carry a secret.
  const setupRequests: { method: string; path: string; body: unknown; key?: string }[] = []
  const setupJobs = new Map<string, MockSetupJob>()
  const setupStatus = (entry: MockSetupJob) => entry.send?.({ type: 'status', offset: 0, status: entry.job })
  const endSetup = (entry: MockSetupJob) => {
    const { job, script } = entry
    if (job.state === 'canceled') return
    job.ended_at = new Date().toISOString()
    delete job.prompt
    if (script.end === 'fail') {
      Object.assign(job, { state: 'failed', error: script.error ?? 'Sign-in failed' })
    } else if (job.kind === 'login') {
      const account = accountList.find(item => item.id === job.account_id)
      if (account) Object.assign(account, { auth_status: 'logged_in', warnings: undefined, updated_at: job.ended_at })
      Object.assign(job, { state: 'succeeded' })
    } else {
      const id = `${job.provider}-${job.name}`
      const identity = job.auth_mode === 'api-key' ? 'API key' : `${job.name}@example.com · plus`
      accountList.push({ id, provider: job.provider, name: job.name, auth_mode: job.auth_mode, identity, auth_status: job.auth_mode === 'token' ? 'token' : 'logged_in', created_at: job.ended_at, updated_at: job.ended_at })
      Object.assign(job, { state: 'succeeded', account_id: id, warnings: job.provider === 'codex' && job.auth_mode !== 'api-key' ? [] : job.auth_mode === 'token' ? ['Token profiles cannot read plan usage'] : [] })
    }
    accountsETag++
    socket.publishEvent({ type: 'agents_changed', revision: ++agentsRevision })
    setupStatus(entry)
  }
  const runSetup = (entry: MockSetupJob) => {
    if (entry.ran) return
    entry.ran = true
    setTimeout(() => {
      entry.send?.({ type: 'output', offset: 0, data: Buffer.from(entry.script.output).toString('base64') })
      setTimeout(() => {
        if (entry.job.state === 'canceled') return
        if (entry.script.end !== 'prompt') { endSetup(entry); return }
        Object.assign(entry.job, { state: 'waiting_secret', prompt: { label: 'Claude setup token' } })
        setupStatus(entry)
      }, entry.script.holdMs ?? 400)
    }, 150)
  }
  const setupJobItem = (job: Record<string, unknown>) => {
    const account = job.state === 'succeeded' ? accountList.find(item => item.id === job.account_id) : undefined
    return account ? { ...job, account } : job
  }
  const accountList: Record<string, unknown>[] = agentsTab.enabled ? [
    { id: 'claude-profile', provider: 'claude', name: 'Work', auth_mode: 'login', identity: 'dev@example.com', auth_status: 'logged_in', effort: 'high', defaults: { effort: 'high' }, created_at: '2026-10-01T09:00:00Z', updated_at: '2026-10-10T09:00:00Z' },
    { id: 'claude-token', provider: 'claude', name: 'CI token', auth_mode: 'token', auth_status: 'token', warnings: ['Token expires in 12 days; plan to replace it'], created_at: '2026-10-02T09:00:00Z', updated_at: '2026-10-02T09:00:00Z' },
    { id: 'codex-personal', provider: 'codex', name: 'Personal', auth_mode: 'chatgpt', identity: 'me@example.com · plus', auth_status: 'logged_in', sandbox: 'workspace-write', approval: 'on-request', model: 'gpt-6.1-sol', defaults: { model: 'gpt-6.1-sol', permissions: 'auto' }, created_at: '2026-10-01T10:00:00Z', updated_at: '2026-10-11T10:00:00Z' },
    { id: 'codex-team', provider: 'codex', name: 'Team', auth_mode: 'device', identity: 'team@example.com · pro', auth_status: 'logged_out', warnings: ['Not logged in; run `bonsai agent run Team -- login`'], created_at: '2026-10-03T10:00:00Z', updated_at: '2026-10-03T10:00:00Z' },
  ] : [
    { id: 'fixture-profile', provider: 'antigravity', name: 'Fixture' },
    { id: 'claude-profile', provider: 'claude', name: 'Work', auth_mode: 'login', identity: 'dev@example.com', warnings: ['Token expires in 12 days'] },
    { id: 'codex-personal', provider: 'codex', name: 'Personal', auth_mode: 'chatgpt', identity: 'me@example.com · plus', auth_status: 'logged_in' },
    { id: 'codex-team', provider: 'codex', name: 'Team', auth_mode: 'device', identity: 'team@example.com · pro', auth_status: 'logged_in', warnings: ['Same login as profile "Personal"; both share its usage limits'] },
  ]
  const metadata: Record<string, Metadata> = {
    'wt-main': { merge_target_branch: 'main', stack_preference: 'auto' },
    'wt-web': { merge_target_branch: 'main', stack_preference: 'auto' },
    'wt-docs': { merge_target_branch: 'feat/web-workspace', stack_preference: 'auto' },
    'wt-daemon': { merge_target_branch: 'main', stack_preference: 'auto' },
    'wt-release': { merge_target_branch: 'main', stack_preference: 'auto' },
    'wt-review': { merge_target_branch: 'chore/release-automation', stack_preference: 'auto' },
  }
  const createdTrees: Array<{ id: string; repository_id: string; path: string; branch: string; main: boolean; local_head_sha: string; status: ReturnType<typeof status>; connection: { state: string; reason?: string } }> = []
  const removedTrees = new Set<string>()
  let syncTime = '2026-09-27T00:00:00Z'
  const unlinkedTrees = ['one', 'two'].map(name => ({ id: 'wt-local-' + name, repository_id: 'bonsai', path: '/trees/local-' + name, branch: 'local/' + name, main: false, local_head_sha: 'sha-local', status: status(0, 0, name === 'one' ? 1 : 0, 'normal'), connection: { state: 'unlinked', reason: 'no_upstream' } }))
  let providerReady = !delayedProvider
  const sequences = new Map(repositories.map(repository => [repository.id, 1]))

  const projectSnapshot = (projectId: string, bump = false): MockWireSnapshot => {
    const current = sequences.get(projectId) ?? 1
    const sequence = bump ? current + 1 : current
    sequences.set(projectId, sequence)
    if (projectId !== 'bonsai') {
      const empty = emptySnapshot(projectId)
      // The Agents tab fixture runs a session in a second project, which therefore needs a worktree.
      const trees = agentsTab.enabled && projectId === 'sprout-lab'
        ? [
            { id: 'wt-lab-main', repository_id: projectId, path: '/trees/lab-main', branch: 'main', main: true, local_head_sha: 'sha-lab-main', status: status(0, 0, 0, 'normal') },
            { id: 'wt-lab', repository_id: projectId, path: '/trees/lab', branch: 'lab/experiment', main: false, local_head_sha: 'sha-lab', status: status(0, 0, 0, 'normal') },
          ]
        : []
      return { ...empty, local: { ...empty.local, worktrees: trees }, sequence, agents: agentSessions.filter(item => item.project_id === projectId) }
    }
    const full = { ...bonsaiSnapshot(metadata), sequence, agents: agentSessions.filter(item => item.project_id === 'bonsai') }
    if (management) {
      const extra = [...unlinkedTrees, ...createdTrees].filter(tree => !removedTrees.has(tree.id))
      const candidates = [{ id: 'refs/remotes/origin/discovered', ref: 'refs/remotes/origin/discovered', name: 'discovered', source: 'remote', remote: 'origin', head_sha: 'sha', last_commit_at: '2026-09-27T00:00:00Z', pull_requests: [], worktree_ids: createdTrees.filter(tree => !removedTrees.has(tree.id)).map(tree => tree.id), creation_mode: createdTrees.some(tree => !removedTrees.has(tree.id)) ? '' : 'remote', source_ref: 'origin/discovered' }]
      return { ...full, branch_candidates: candidates,
        sync: { fetch: { state: 'ready', completed_at: syncTime }, pull: { state: 'skipped', reason: 'dirty_worktree' } },
        local: { ...full.local, branches: [...full.local.branches, { name: 'origin/discovered', ref: 'refs/remotes/origin/discovered', remote: true }], worktrees: [...full.local.worktrees.filter(tree => !removedTrees.has(tree.id)), ...extra], groups: [{ id: 'unlinked:bonsai', kind: 'unlinked', worktree_ids: unlinkedTrees.filter(tree => !removedTrees.has(tree.id)).map(tree => tree.id) }] } }
    }
    if (providerReady) return full
    const localOnlyState = Object.fromEntries(
      (full.local.worktrees as Array<{ id: string }>).map(worktree => [worktree.id, {
        ci: {
          status: 'unknown',
          checks: [],
          freshness: { state: 'unavailable', error: { code: 'provider_unavailable', message: 'GitHub is offline' } },
        },
      }]),
    )
    return {
      ...full,
      remote: undefined,
      worktree_state: localOnlyState,
      freshness: {
        ...full.freshness,
        provider: { state: 'error', error: { code: 'provider_unavailable', message: 'GitHub is offline' } },
      },
    }
  }

  const activeRepositories = () => repositories.filter(repository =>
    rootSettings.repositories.some(candidate => candidate.id === repository.id && candidate.selected),
  )

  const socket = await mockLocalEventSocket(
    page,
    () => {
      const projects = activeRepositories()
      return { projects, snapshots: projects.map(repository => projectSnapshot(repository.id)) }
    },
    delayedProvider
      ? () => {
          providerReady = true
          return projectSnapshot('bonsai', true)
        }
      : undefined,
    apiOrigin,
  )

  await page.routeWebSocket(new RegExp(`^${escapeRegExp(webSocketOrigin(apiOrigin))}/api/projects/[^/]+/agents/[^/]+/terminal$`), terminal => {
    terminal.onMessage(message => {
      const frame = JSON.parse(String(message))
      if (frame.type === 'attach') {
        terminal.send(JSON.stringify({ type: 'ready', version: 1, generation: 'e2e-epoch', writer: true }))
        terminal.send(JSON.stringify({ type: 'output', offset: 0, data: Buffer.from('Fixture terminal\r\n').toString('base64') }))
      }
    })
  })
  await page.routeWebSocket(new RegExp(`^${escapeRegExp(webSocketOrigin(apiOrigin))}/api/agents/setup/[^/]+/terminal$`), terminal => {
    const id = decodeURIComponent(new URL(terminal.url()).pathname.split('/')[4])
    terminal.onMessage(message => {
      const frame = JSON.parse(String(message))
      const entry = setupJobs.get(id)
      if (frame.type !== 'attach' || !entry) return
      entry.send = value => terminal.send(JSON.stringify(value))
      terminal.send(JSON.stringify({ type: 'ready', version: 1, generation: 'e2e-epoch', writer: true }))
      setupStatus(entry)
      runSetup(entry)
    })
  })
  await page.route(url => url.origin === apiOrigin && !isUIPath(url.pathname), async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname

    if (path === '/health') {
      await route.fulfill({ json: { ok: true } })
      return
    }

    if (path === '/api/agents/providers') {
      const providers = [{ id: 'antigravity', label: 'Antigravity', available: true, launch_fields: [
          { id: 'full_access', label: 'Full access', kind: 'toggle', help: 'Allow Antigravity to run tools without permission prompts.', options: [{ value: 'false', label: 'Ask permissions' }, { value: 'true', label: 'Full access', tone: 'danger' }] },
        ], profile_fields: [
          { id: 'model', label: 'Model', kind: 'model' },
          { id: 'full_access', label: 'Full access', kind: 'toggle', help: 'Allow Antigravity to run tools without permission prompts.', options: [{ value: 'false', label: 'Ask permissions' }, { value: 'true', label: 'Full access', tone: 'danger' }] },
        ] },
        { id: 'claude', label: 'Claude', available: true, version: '2.1.295', launch_fields: [
          { id: 'permission_mode', label: 'Permission mode', kind: 'select', options: [{ value: '', label: 'Profile default' }, { value: 'default', label: 'Default' }, { value: 'acceptEdits', label: 'Accept edits' }, { value: 'plan', label: 'Plan' }, { value: 'auto', label: 'Auto' }, { value: 'bypassPermissions', label: 'Bypass permissions', tone: 'danger', help: 'Claude will run tools without asking.' }] },
          { id: 'effort', label: 'Effort', kind: 'select', options: ['', 'low', 'medium', 'high', 'xhigh', 'max'].map(value => ({ value, label: value || 'Profile default' })) },
        ], profile_fields: [
          { id: 'model', label: 'Model', kind: 'model' },
          { id: 'permission_mode', label: 'Permission mode', kind: 'select', options: [{ value: '', label: 'Provider default' }, { value: 'default', label: 'Default' }, { value: 'acceptEdits', label: 'Accept edits' }, { value: 'plan', label: 'Plan' }, { value: 'auto', label: 'Auto' }, { value: 'bypassPermissions', label: 'Bypass permissions', tone: 'danger', help: 'Claude will run tools without asking.' }] },
          { id: 'effort', label: 'Effort', kind: 'select', options: ['', 'low', 'medium', 'high', 'xhigh', 'max'].map(value => ({ value, label: value || 'Provider default' })) },
        ] },
        { id: 'codex', label: 'Codex', available: true, version: '0.162.1', launch_fields: [
          { id: 'permissions', label: 'Permissions', kind: 'select', default: 'auto', options: [
            { value: 'read-only', label: 'Read only', set: { sandbox: 'read-only', approval: 'on-request' } },
            { value: 'auto', label: 'Auto', set: { sandbox: 'workspace-write', approval: 'on-request' } },
            { value: 'full-access', label: 'Full access', tone: 'danger', help: 'Codex can change any file and use the network without asking.', set: { sandbox: 'danger-full-access', approval: 'never' } },
            { value: 'bypass', label: 'Bypass', tone: 'danger', help: 'Codex runs every command without a sandbox and without asking.', set: { sandbox: 'bypass' } },
          ] },
          { id: 'effort', label: 'Effort', kind: 'select', per_model: true, options: ['', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max', 'ultra'].map(value => ({ value, label: value || 'Profile default' })) },
        ], profile_fields: [
          { id: 'model', label: 'Model', kind: 'model' },
          { id: 'permissions', label: 'Permissions', kind: 'select', options: [
            { value: '', label: 'Provider default' },
            { value: 'read-only', label: 'Read only', set: { sandbox: 'read-only', approval: 'on-request' } },
            { value: 'auto', label: 'Auto', set: { sandbox: 'workspace-write', approval: 'on-request' } },
            { value: 'full-access', label: 'Full access', tone: 'danger', help: 'Codex can change any file and use the network without asking.', set: { sandbox: 'danger-full-access', approval: 'never' } },
          ] },
          { id: 'effort', label: 'Effort', kind: 'select', per_model: true, options: ['', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max', 'ultra'].map(value => ({ value, label: value || 'Provider default' })) },
        ] },
      ]
      await route.fulfill({ json: agentsTab.enabled
        ? providers.map(provider => provider.id === 'antigravity'
          ? { id: provider.id, label: provider.label, available: false, unavailable_reason: { message: 'agy is not installed' }, install_hint: 'Install Antigravity and make sure agy is on PATH' }
          : provider.id === 'claude' ? { ...provider, usage_note: 'Read from an undocumented Claude endpoint; it can change without notice.' } : provider)
          .map(provider => ({ ...provider, setup: SETUP_SPECS[provider.id] }))
        : providers }); return
    }
    const setupStart = path === '/api/agents/setup' && request.method() === 'POST'
    const loginStart = path.match(/^\/api\/agents\/accounts\/([^/]+)\/login$/)
    const setupJobPath = path.match(/^\/api\/agents\/setup\/([^/]+)(\/secret)?$/)
    if (setupStart || loginStart || setupJobPath) {
      const body = request.postData() ? request.postDataJSON() : undefined
      setupRequests.push({ method: request.method(), path, body, key: request.headers()['idempotency-key'] })
      if (agentsTab.setupUnsupported) {
        await route.fulfill({ status: 501, json: { error: { code: 'terminal_unsupported', message: 'Signing in from the browser needs a terminal, which this platform does not support. Use the bonsai agent CLI.' } } }); return
      }
      if (setupStart || loginStart) {
        const account = loginStart ? accountList.find(item => item.id === decodeURIComponent(loginStart[1])) : undefined
        if (loginStart && !account) { await route.fulfill({ status: 404, json: { error: { code: 'not_found', message: 'Profile not found' } } }); return }
        const provider = String(account?.provider ?? body.provider)
        const name = String(account?.name ?? body.name)
        const mode = String(body.auth_mode ?? account?.auth_mode ?? (provider === 'codex' ? 'chatgpt' : provider === 'claude' ? 'login' : ''))
        if (!account && accountList.some(item => item.provider === provider && item.name === name)) {
          await route.fulfill({ status: 409, json: { error: { code: 'name_taken', message: 'Another profile of this provider already has this name' } } }); return
        }
        const script: SetupScript = name === 'broken'
          ? { output: 'Starting login…\r\nError: the sign-in was refused\r\n', end: 'fail', error: 'codex login exited with status 1' }
          : body.secret ? { output: 'Signing in with the pasted key…\r\n', end: 'succeed' }
            : mode === 'device' ? { output: 'Follow these steps to sign in:\r\n1. Open https://auth.example.com/codex/device\r\n2. Enter this one-time code: WXYZ-2345\r\n', end: 'succeed', holdMs: 1500 }
              : mode === 'token' ? { output: 'Visit https://claude.example.com/oauth/authorize?code=true to create a token.\r\nToken created.\r\n', end: 'prompt' }
                : { output: 'Opening https://auth.example.com/login in your browser…\r\n', end: 'succeed', holdMs: 5000 }
        const id = `job-${setupJobs.size + 1}`
        const job: Record<string, unknown> = { id, kind: account ? 'login' : 'create', provider, name, auth_mode: body.auth_mode, state: 'running', warnings: [], created_at: new Date().toISOString() }
        if (account) job.account_id = account.id
        setupJobs.set(id, { job, script, ran: false })
        socket.publishEvent({ type: 'agents_changed', revision: ++agentsRevision })
        await route.fulfill({ status: 202, json: job }); return
      }
      const entry = setupJobs.get(decodeURIComponent(setupJobPath![1]))
      if (!entry) { await route.fulfill({ status: 404, json: { error: { code: 'not_found', message: 'Setup job not found' } } }); return }
      if (setupJobPath![2]) {
        if (entry.job.state !== 'waiting_secret') { await route.fulfill({ status: 409, json: { error: { code: 'no_prompt', message: 'The sign-in is not waiting for a secret' } } }); return }
        Object.assign(entry.job, { state: 'running' })
        delete entry.job.prompt
        setupStatus(entry)
        setTimeout(() => endSetup(entry), 300)
        await route.fulfill({ status: 204, body: '' }); return
      }
      if (request.method() === 'DELETE' && ['running', 'waiting_secret', 'preparing', 'verifying'].includes(String(entry.job.state))) {
        Object.assign(entry.job, { state: 'canceled', ended_at: new Date().toISOString() })
        delete entry.job.prompt
        setupStatus(entry)
      }
      await route.fulfill({ json: setupJobItem(entry.job) }); return
    }
    if (path === '/api/agents/providers/claude/models') {
      await route.fulfill({ json: [
        { id: 'opus', label: 'Opus', description: 'Latest Opus', source: 'alias' },
        { id: 'sonnet', label: 'Sonnet', description: 'Latest Sonnet', source: 'alias' },
        { id: 'claude-sonnet-5-5', label: 'Claude Sonnet 5.5', description: 'claude-sonnet-5-5', source: 'api' },
      ] }); return
    }
    if (path === '/api/agents/providers/codex/models') {
      await route.fulfill({ json: [
        { id: 'gpt-6.1-sol', label: 'GPT-6.1-Sol', source: 'api', default: true, efforts: ['low', 'medium', 'high', 'xhigh', 'max', 'ultra'], default_effort: 'low' },
        { id: 'gpt-6-luna', label: 'GPT-6-Luna', source: 'api', efforts: ['low', 'medium', 'high', 'xhigh', 'max'], default_effort: 'medium' },
      ] }); return
    }
    if (path.startsWith('/api/agents/providers/') && path.endsWith('/models')) { await route.fulfill({ json: [] }); return }
    const profileMatch = path.match(/^\/api\/agents\/accounts\/([^/]+)$/)
    if (profileMatch && (request.method() === 'PATCH' || request.method() === 'DELETE')) {
      const id = decodeURIComponent(profileMatch[1])
      const account = accountList.find(item => item.id === id)
      if (!account) { await route.fulfill({ status: 404, json: { error: { code: 'not_found', message: 'Profile not found' } } }); return }
      profileRequests.push({ method: request.method(), id, body: request.postDataJSON() })
      if (request.method() === 'DELETE') {
        const active = agentSessions.filter(item => item.account_id === id && ['starting', 'running', 'stopping'].includes(item.state))
        if (active.length) {
          await route.fulfill({ status: 409, json: { error: { code: 'profile_in_use', message: "Stop the profile's running sessions before removing it", session_ids: active.map(item => item.id) } } }); return
        }
        accountList.splice(accountList.indexOf(account), 1)
        accountsETag++
        socket.publishEvent({ type: 'agents_changed', revision: ++agentsRevision })
        await route.fulfill({ json: { warnings: agentsTab.removeWarnings ?? [] } }); return
      }
      const body = request.postDataJSON() as { name?: string; defaults?: Record<string, string | null> }
      if (body.name !== undefined) {
        if (!/^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/.test(body.name)) {
          await route.fulfill({ status: 400, json: { error: { code: 'invalid_name', message: "Names start with a letter or digit, then use letters, digits, '.', '_' or '-' (at most 64)" } } }); return
        }
        if (accountList.some(item => item.id !== id && item.provider === account.provider && item.name === body.name)) {
          await route.fulfill({ status: 409, json: { error: { code: 'name_taken', message: 'Another profile of this provider already has this name' } } }); return
        }
      }
      const defaults = { ...(account.defaults as Record<string, string> | undefined) }
      for (const [key, value] of Object.entries(body.defaults ?? {})) {
        if (value === null) delete defaults[key]
        else defaults[key] = value
      }
      const presets: Record<string, Record<string, string>> = { 'read-only': { sandbox: 'read-only', approval: 'on-request' }, auto: { sandbox: 'workspace-write', approval: 'on-request' }, 'full-access': { sandbox: 'danger-full-access', approval: 'never' } }
      const options: Record<string, unknown> = { sandbox: undefined, approval: undefined, ...(presets[defaults.permissions] ?? {}), model: defaults.model, effort: defaults.effort, permission_mode: defaults.permission_mode }
      if (account.provider === 'antigravity') options.full_access = defaults.full_access === 'true'
      Object.assign(account, options, { name: body.name ?? account.name, defaults, updated_at: new Date().toISOString() })
      accountsETag++
      socket.publishEvent({ type: 'agents_changed', revision: ++agentsRevision })
      await route.fulfill({ json: account }); return
    }
    if (path === '/api/agents/accounts') {
      const accounts = accountList
      // Like the API: an ETag over the answer, 304 when the page already has it.
      const etag = `"accounts-${accountsETag}"`
      if (request.headers()['if-none-match'] === etag) { await route.fulfill({ status: 304, headers: { ETag: etag } }); return }
      await route.fulfill({ json: accounts, headers: { ETag: etag } }); return
    }
    if (path === '/api/agents/usage' || path === '/api/agents/usage/refresh') {
      const refresh = path.endsWith('/refresh')
      const wanted = refresh ? (request.postDataJSON() as { account_id?: string } | null)?.account_id : url.searchParams.get('account_id') ?? undefined
      const items = Object.values(AGENTS_TAB_USAGE).filter(item => !wanted || item.account_id === wanted)
      usageRequests.push({ method: request.method(), account_id: wanted })
      await route.fulfill({ json: refresh ? items.map(item => ({ ...item, throttled: true })) : items }); return
    }
    const stopMatch = path.match(/^\/api\/projects\/([^/]+)\/agents\/([^/]+)$/)
    if (stopMatch && request.method() === 'DELETE') {
      const session = agentSessions.find(item => item.id === decodeURIComponent(stopMatch[2]))
      if (!session) { await route.fulfill({ status: 404, json: { error: { code: 'not_found', message: 'Agent not found' } } }); return }
      Object.assign(session, { state: 'exited', ended_at: new Date().toISOString(), exit_code: 0 })
      socket.publishUpdate(projectSnapshot(session.project_id, true), 'agents')
      socket.publishEvent({ type: 'agents_changed', revision: ++agentsRevision })
      await route.fulfill({ json: session }); return
    }
    if (path === '/api/projects/bonsai/agents' && request.method() === 'POST') {
      const body = request.postDataJSON()
      const profile = ({ 'claude-profile': { provider: 'claude', name: 'Work' }, 'codex-personal': { provider: 'codex', name: 'Personal' }, 'codex-team': { provider: 'codex', name: 'Team' } } as const)[body.account_id as string] ?? { provider: 'antigravity', name: 'Fixture' }
      const id = profile.provider === 'antigravity' ? 'fixture-session' : `${profile.provider}-session-${agentSessions.filter(item => item.provider === profile.provider).length + 1}`
      // Like the API: the accepted launch choices, never the prompt.
      const options = Object.fromEntries(['permission_mode', 'effort', 'sandbox', 'approval', 'full_access'].filter(key => body[key] !== undefined && body[key] !== '').map(key => [key, String(body[key])]))
      const session: AgentSummary = { id, project_id: 'bonsai', worktree_id: body.worktree_id, account_id: body.account_id, provider: profile.provider, profile_name: profile.name, name: body.name || profile.name, state: 'running', created_at: new Date().toISOString(), model: body.model || undefined, options: Object.keys(options).length ? options : undefined }
      if (!agentSessions.some(item => item.id === session.id)) agentSessions.push(session)
      socket.publishUpdate(projectSnapshot('bonsai', true), 'agents')
      socket.publishEvent({ type: 'agents_changed', revision: ++agentsRevision })
      await route.fulfill({ status: 202, json: session }); return
    }
    if (path === '/version') {
      await route.fulfill({ json: { version: 'e2e', api_version: 3 } })
      return
    }

    if (path === '/api/session') {
      await route.fulfill({ status: 201, json: { token: 'e2e-session', expires_at: new Date(Date.now() + 60_000).toISOString() } })
      return
    }

    if (path.startsWith('/api/settings/project-roots')) {
      const selectedIds = new Set(rootSettings.repositories.filter(repository => repository.selected).map(repository => repository.id))
      if (request.method() === 'POST') {
        const body = request.postDataJSON() as { path: string; revision: number }
        rootSettings.roots.push({ id: `root-${rootSettings.revision}`, path: body.path })
        rootSettings.revision++
      } else if (request.method() === 'DELETE') {
        rootSettings.roots = rootSettings.roots.filter(root => root.id !== path.split('/').at(-1))
        rootSettings.revision++
      }
      rootSettings.repositories = discoveredRepositories(selectedIds)
      await route.fulfill({ json: rootSettings })
      const projects = activeRepositories()
      socket.publishCatalog(projects, projects.map(repository => projectSnapshot(repository.id)))
      return
    }
    if (path === '/api/settings/updates') {
      await route.fulfill({ json: { mode: 'standard', standard_interval_seconds: 120, live: null } })
      return
    }
    if (path === '/api/settings/project-selection' && request.method() === 'POST') {
      const body = request.postDataJSON() as { selection_revision: number; project_ids: string[] }
      rootSettings.selection_revision++
      const selectedIds = new Set(body.project_ids)
      rootSettings.repositories = discoveredRepositories(selectedIds)
      await route.fulfill({ json: rootSettings })
      const projects = activeRepositories()
      socket.publishCatalog(projects, projects.map(repository => projectSnapshot(repository.id)))
      return
    }
    if (path === '/api/projects') {
      await route.fulfill({ json: activeRepositories() })
      return
    }

    const snapshotMatch = path.match(/^\/api\/projects\/([^/]+)\/git$/)
    if (snapshotMatch) {
      const projectId = decodeURIComponent(snapshotMatch[1])
      await route.fulfill({ json: projectSnapshot(projectId) })
      return
    }

    if (/^\/api\/projects\/[^/]+\/sync$/.test(path) && request.method() === 'POST') {
      syncTime = new Date().toISOString()
      await route.fulfill({ status: 202, json: { accepted: true } })
      if (management) socket.publishUpdate(projectSnapshot('bonsai', true), 'sync')
      return
    }

    const refreshMatch = path.match(/^\/api\/projects\/([^/]+)\/refresh$/)
    if (refreshMatch && request.method() === 'POST') {
      await route.fulfill({ status: 202, json: { accepted: true } })
      return
    }

    if (management && path === '/api/projects/bonsai/worktrees' && request.method() === 'POST') {
      const body = request.postDataJSON() as { mode: string; branch: string; base: string }
      const tree = { id: 'wt-created', repository_id: 'bonsai', path: '/trees/' + body.branch, branch: body.branch, main: false, local_head_sha: 'sha-created', status: status(0, 0, 0, 'normal'), connection: { state: 'linked' } }
      createdTrees.push(tree)
      await route.fulfill({ json: { result: tree } })
      socket.publishUpdate(projectSnapshot('bonsai', true))
      return
    }
    if (management && /^\/api\/worktrees\/[^/]+$/.test(path) && request.method() === 'DELETE') {
      const id = path.split('/').at(-1)!
      const tree = [...unlinkedTrees, ...createdTrees].find(tree => tree.id === id)
      const body = request.postDataJSON() as { confirm_discard: boolean }
      if (tree?.status.dirty && !body.confirm_discard) { await route.fulfill({ status: 409, json: { error: { code: 'dirty_worktree', message: 'Worktree contains uncommitted changes' } } }); return }
      removedTrees.add(id)
      await route.fulfill({ json: { result: null } })
      socket.publishUpdate(projectSnapshot('bonsai', true))
      return
    }

    const metadataMatch = path.match(/^\/api\/worktrees\/([^/]+)\/metadata$/)
    if (metadataMatch && request.method() === 'PATCH') {
      const id = decodeURIComponent(metadataMatch[1])
      const patch = request.postDataJSON() as Partial<Metadata>
      metadata[id] = { ...metadata[id], ...patch }
      await route.fulfill({ json: metadata[id] })
      socket.publishUpdate(projectSnapshot('bonsai', true))
      return
    }

    const prMatch = path.match(/^\/api\/projects\/([^/]+)\/pull-requests\/(\d+)$/)
    if (prMatch) {
      const number = Number(prMatch[2])
      const pr = pullRequests.find((item) => item.number === number)
      await route.fulfill(pr ? { json: pr } : { status: 404, json: { error: { code: 'not_found', message: 'Not Found' } } })
      return
    }

    const checksMatch = path.match(/^\/api\/projects\/([^/]+)\/checks\/([^/]+)$/)
    if (checksMatch) {
      await route.fulfill({ json: checks[decodeURIComponent(checksMatch[2])] ?? [] })
      return
    }

    if (/^\/api\/worktrees\/[^/]+\/files$/.test(path)) {
      await route.fulfill({ json: [] })
      return
    }

    if (/^\/api\/worktrees\/[^/]+\/diff$/.test(path)) {
      await route.fulfill({ json: { patch: '', files: [] } })
      return
    }

    await route.fulfill({ status: 404, json: { error: { code: 'not_found', message: 'Not Found' } } })
  })

  return {
    publish: () => socket.publishUpdate(projectSnapshot('bonsai', true)),
    /** Usage requests the page made, in order. */
    usageRequests,
    /** PATCH and DELETE requests on profiles, in order. */
    profileRequests,
    /** Setup and sign-in requests, in order, with their bodies and request keys. */
    setupRequests,
    /** Adds a profile as if the CLI created it; the page learns of it from its next revalidation. */
    addAccount: (account: Record<string, unknown>) => { accountList.push(account); accountsETag++ },
    /** Profiles as the API would list them now. */
    accounts: () => structuredClone(accountList),
    /** The global event the API sends after an account, setup or session change. */
    publishAgentsChanged: () => socket.publishEvent({ type: 'agents_changed', revision: ++agentsRevision }),
  }
}

// mockUpdateSettings answers GET /api/settings/updates with whatever
// settings() returns at the time (routes added later take precedence).
export async function mockUpdateSettings(page: Page, settings: () => unknown, apiOrigin = DEFAULT_API_ORIGIN) {
  await page.route(url => url.origin === apiOrigin && url.pathname === '/api/settings/updates', route => route.fulfill({ json: settings() }))
}

/** Picks a level on the start dialog's effort slider by its caption under the track. */
export async function chooseEffort(dialog: Locator, level: string) {
  // The default level's caption carries a "default" tag, so match on its start.
  await dialog.getByRole('slider', { name: 'Effort' }).locator('..').locator('button.step-label').filter({ hasText: new RegExp(`^${level}`) }).click()
}

/** A select's menu, which renders outside the dialog that owns the select. */
export const selectMenu = (page: Page, label: string) => page.locator(`[data-bonsai-select-menu="${label}"]`)

export async function openConnectedApp(page: Page) {
  await page.goto('/app')
  await page.locator('.react-flow').waitFor({ state: 'visible' })
}

export async function openFullStackApp(page: Page) {
  // Explicit origin of the native API/PTY fixture, also serving its UI.
  await page.goto('http://127.0.0.1:7014/app')
  await page.locator('.react-flow').waitFor({ state: 'visible' })
}
