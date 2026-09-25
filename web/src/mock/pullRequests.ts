import type { PullRequest } from '../types'

export const pullRequests: PullRequest[] = [
  {
    id: 'pr-24',
    number: 24,
    title: 'feat(web): prototype visual workspace',
    branch: 'feat/web-workspace',
    base: 'main',
    status: 'Open',
    checks: [
      { name: 'frontend / typecheck', status: 'success' },
      { name: 'frontend / unit', status: 'success' },
      { name: 'frontend / e2e', status: 'running' },
    ],
    commits: [
      { sha: '94d7bd2', message: 'feat(web): add workspace shell', author: 'Tiago' },
      { sha: 'a20fe91', message: 'feat(web): add interactive graph', author: 'UI builder' },
      { sha: '10c8c2e', message: 'test(web): cover board state', author: 'Test runner' },
    ],
    conversation: [
      { author: 'Tiago', body: 'Keep node density low and move detail into the inspector.', time: '42m ago' },
      { author: 'UI builder', body: 'Updated the graph hierarchy and dock behavior for the design checkpoint.', time: '18m ago' },
    ],
    files: [
      { path: 'web/src/features/workspace/canvas/BonsaiCanvas.tsx', additions: 214, deletions: 0, diff: ['+ React Flow canvas', '+ persisted viewport', '+ ELK auto layout'] },
      { path: 'web/src/stores/bonsai.ts', additions: 136, deletions: 0, diff: ['+ mock workspace state', '+ command actions', '+ board mutations'] },
    ],
  },
  {
    id: 'pr-23',
    number: 23,
    title: 'fix(daemon): stabilize lifecycle cleanup',
    branch: 'fix/daemon-lifecycle',
    base: 'main',
    status: 'Draft',
    checks: [
      { name: 'go test ./...', status: 'failed' },
      { name: 'windows', status: 'success' },
    ],
    commits: [
      { sha: '3e91e15', message: 'fix: clear stale process markers', author: 'Tiago' },
      { sha: '8b264aa', message: 'test: reproduce interrupted shutdown', author: 'Lifecycle fix' },
    ],
    conversation: [
      { author: 'Review pass', body: 'The Windows cleanup path still needs one deterministic assertion.', time: '1h ago' },
    ],
    files: [
      { path: 'internal/daemon/server/proc.go', additions: 38, deletions: 14, diff: ['+ cleanup guarded by pid state', '- eager marker deletion'] },
    ],
  },
  {
    id: 'pr-22',
    number: 22,
    title: 'chore: tighten release automation',
    branch: 'chore/release-automation',
    base: 'main',
    status: 'Open',
    checks: [
      { name: 'release tests', status: 'success' },
      { name: 'snapshot', status: 'success' },
    ],
    commits: [
      { sha: 'f20bc71', message: 'chore: verify snapshot artifacts', author: 'Tiago' },
    ],
    conversation: [
      { author: 'Release helper', body: 'Snapshot names now mirror the final publish path.', time: '3h ago' },
    ],
    files: [
      { path: '.github/workflows/release.yml', additions: 18, deletions: 7, diff: ['+ snapshot verification', '+ artifact naming guard'] },
    ],
  },
]
