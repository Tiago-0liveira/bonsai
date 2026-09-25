import type { PullRequest } from '../types'

export const pullRequests: PullRequest[] = [
  {
    id: 'pr-24',
    number: 24,
    title: 'feat(web): prototype visual workspace',
    description: 'Build the visual workspace prototype with canvas hierarchy, runtime panels, PR visibility, and interaction coverage.',
    branch: 'feat/web-workspace',
    base: 'main',
    status: 'Open',
    author: 'Tiago',
    createdAt: '2d ago',
    updatedAt: '8m ago',
    mergeable: true,
    checks: [
      { name: 'frontend / typecheck', status: 'success' },
      { name: 'frontend / unit', status: 'success' },
      { name: 'frontend / e2e', status: 'running' },
    ],
    commits: [
      { sha: '94d7bd2', message: 'feat(web): add workspace shell', author: 'Tiago', time: '2d ago' },
      { sha: 'a20fe91', message: 'feat(web): add interactive graph', author: 'UI builder', time: '4h ago' },
      { sha: '10c8c2e', message: 'test(web): cover board state', author: 'Test runner', time: '8m ago' },
    ],
    conversation: [
      { author: 'Tiago', body: 'Keep node density low and move detail into the inspector.', time: '42m ago', kind: 'comment' },
      { author: 'UI builder', body: 'Updated the graph hierarchy and dock behavior for the design checkpoint.', time: '18m ago', kind: 'comment' },
    ],
    files: [
      { path: 'web/src/features/workspace/canvas/BonsaiCanvas.tsx', additions: 214, deletions: 0, diff: ['+ React Flow canvas', '+ persisted viewport', '+ recursive layout'] },
      { path: 'web/src/stores/bonsai.ts', additions: 136, deletions: 0, diff: ['+ mock workspace state', '+ command actions', '+ board mutations'] },
    ],
  },
  {
    id: 'pr-25',
    number: 25,
    title: 'docs: workspace branch relationships',
    description: 'Document how nested worktrees merge into feature branches before reaching main.',
    branch: 'feat/workspace-docs',
    base: 'feat/web-workspace',
    status: 'Draft',
    author: 'UI builder',
    createdAt: '5h ago',
    updatedAt: '14m ago',
    mergeable: true,
    checks: [
      { name: 'docs / lint', status: 'success' },
      { name: 'links', status: 'success' },
    ],
    commits: [
      { sha: 'a741bd1', message: 'docs: describe nested worktrees', author: 'UI builder', time: '14m ago' },
    ],
    conversation: [
      { author: 'UI builder', body: 'Draft is ready for a content pass.', time: '14m ago', kind: 'comment' },
    ],
    files: [
      { path: 'docs/worktrees.md', additions: 42, deletions: 2, diff: ['+ nested merge targets', '+ PR relationship diagram'] },
    ],
  },
  {
    id: 'pr-23',
    number: 23,
    title: 'fix(daemon): stabilize lifecycle cleanup',
    description: 'Make daemon shutdown cleanup deterministic across interrupted and Windows shutdown paths.',
    branch: 'fix/daemon-lifecycle',
    base: 'main',
    status: 'Draft',
    author: 'Tiago',
    createdAt: '1d ago',
    updatedAt: '1h ago',
    mergeable: false,
    checks: [
      { name: 'go test ./...', status: 'failed' },
      { name: 'windows', status: 'success' },
    ],
    commits: [
      { sha: '3e91e15', message: 'fix: clear stale process markers', author: 'Tiago', time: '2h ago' },
      { sha: '8b264aa', message: 'test: reproduce interrupted shutdown', author: 'Lifecycle fix', time: '1h ago' },
    ],
    conversation: [
      { author: 'Review pass', body: 'The Windows cleanup path still needs one deterministic assertion.', time: '1h ago', kind: 'review' },
    ],
    files: [
      { path: 'internal/daemon/server/proc.go', additions: 38, deletions: 14, diff: ['+ cleanup guarded by pid state', '- eager marker deletion'] },
    ],
  },
  {
    id: 'pr-22',
    number: 22,
    title: 'chore: tighten release automation',
    description: 'Verify snapshot artifacts and align release naming with final publish paths.',
    branch: 'chore/release-automation',
    base: 'main',
    status: 'Open',
    author: 'Tiago',
    createdAt: '4d ago',
    updatedAt: '3h ago',
    mergeable: true,
    checks: [
      { name: 'release tests', status: 'success' },
      { name: 'snapshot', status: 'success' },
    ],
    commits: [
      { sha: 'f20bc71', message: 'chore: verify snapshot artifacts', author: 'Tiago', time: '3h ago' },
    ],
    conversation: [
      { author: 'Release helper', body: 'Snapshot names now mirror the final publish path.', time: '3h ago', kind: 'comment' },
    ],
    files: [
      { path: '.github/workflows/release.yml', additions: 18, deletions: 7, diff: ['+ snapshot verification', '+ artifact naming guard'] },
    ],
  },
]
