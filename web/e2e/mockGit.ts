import type { AgentSummary } from '../src/api/agents'
import type { Page } from '@playwright/test'

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

export async function mockLocalEventSocket(
  page: Page,
  bootstrap: () => { projects: typeof repositories; snapshots: MockWireSnapshot[] } = () => ({ projects: [], snapshots: [] }),
  delayedUpdate?: () => MockWireSnapshot | undefined,
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

  await page.routeWebSocket('ws://127.0.0.1:7001/events', socket => {
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

  return { publishCatalog, publishUpdate }
}

export async function mockGitBackend(page: Page, emptyRoots = false, delayedProvider = false, management = false) {
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

  const agentSessions: AgentSummary[] = []
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
    if (projectId !== 'bonsai') return { ...emptySnapshot(projectId), sequence }
    const full = { ...bonsaiSnapshot(metadata), sequence, agents: [...agentSessions] }
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
  )

  await page.routeWebSocket(/ws:\/\/127\.0\.0\.1:7001\/api\/projects\/[^/]+\/agents\/[^/]+\/terminal$/, terminal => {
    terminal.onMessage(message => {
      const frame = JSON.parse(String(message))
      if (frame.type === 'attach') {
        terminal.send(JSON.stringify({ type: 'ready', version: 1, generation: 'e2e-epoch', writer: true }))
        terminal.send(JSON.stringify({ type: 'output', offset: 0, data: Buffer.from('Fixture terminal\r\n').toString('base64') }))
      }
    })
  })
  await page.route('http://127.0.0.1:7001/**', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname

    if (path === '/health') {
      await route.fulfill({ json: { ok: true } })
      return
    }

    if (path === '/api/agents/providers') {
      await route.fulfill({ json: [
        { id: 'antigravity', label: 'Antigravity', available: true },
        { id: 'claude', label: 'Claude', available: true, version: '2.1.295' },
        { id: 'codex', label: 'Codex', available: false, unavailable_reason: { message: 'Not available yet' } },
      ] }); return
    }
    if (path === '/api/agents/providers/claude/models') {
      await route.fulfill({ json: [
        { id: 'opus', label: 'Opus', description: 'Latest Opus', source: 'alias' },
        { id: 'sonnet', label: 'Sonnet', description: 'Latest Sonnet', source: 'alias' },
        { id: 'claude-sonnet-5-5', label: 'Claude Sonnet 5.5', description: 'claude-sonnet-5-5', source: 'api' },
      ] }); return
    }
    if (path.startsWith('/api/agents/providers/') && path.endsWith('/models')) { await route.fulfill({ json: [] }); return }
    if (path === '/api/agents/accounts') {
      await route.fulfill({ json: [
        { id: 'fixture-profile', provider: 'antigravity', name: 'Fixture' },
        { id: 'claude-profile', provider: 'claude', name: 'Work', auth_mode: 'login', identity: 'dev@example.com', warnings: ['Token expires in 12 days'] },
      ] }); return
    }
    if (path === '/api/projects/bonsai/agents' && request.method() === 'POST') {
      const body = request.postDataJSON()
      const claude = body.account_id === 'claude-profile'
      const session: AgentSummary = { id: claude ? `claude-session-${agentSessions.length + 1}` : 'fixture-session', project_id: 'bonsai', worktree_id: body.worktree_id, account_id: body.account_id, provider: claude ? 'claude' : 'antigravity', profile_name: claude ? 'Work' : 'Fixture', name: body.name || (claude ? 'Work' : 'Fixture'), state: 'running', created_at: new Date().toISOString() }
      if (!agentSessions.some(item => item.id === session.id)) agentSessions.push(session)
      socket.publishUpdate(projectSnapshot('bonsai', true), 'agents')
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

  await page.route('https://api.bonsai.dev/**', async (route) => {
    await route.fulfill({ status: 401, body: 'authentication required' })
  })
  return { publish: () => socket.publishUpdate(projectSnapshot('bonsai', true)) }
}

export async function openConnectedApp(page: Page) {
  await page.goto('/app')
  await page.getByRole('button', { name: 'Connect to local Bonsai' }).click()
  await page.locator('.react-flow').waitFor({ state: 'visible' })
}
