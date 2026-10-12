export type Health = 'healthy' | 'warning' | 'error' | 'idle'
export type WorktreeKind = 'Production' | 'Feature' | 'Bug' | 'Refactor' | 'Chore'
export type WorktreeSourceType = 'existing' | 'origin' | 'new'
export type AgentState = 'running' | 'idle' | 'finished'
export type AgentPresentation = 'canvas' | 'history' | 'archived'
export type PrStatus = 'Draft' | 'Open' | 'Closed' | 'Merged'
export type CiStatus = 'unknown' | 'none' | 'running' | 'passed' | 'failed' | 'waiting'
export type DockPanelKey = 'files' | 'prs'
export type StackPreference = 'auto' | 'never'
export type FileGitStatus = 'modified' | 'untracked' | 'added' | 'deleted' | 'committed'

export interface Workspace {
  id: string
  name: string
  projectIds: string[]
}

export interface DefaultBranchInfo {
  commitSha: string
  commitMessage: string
  lastActivity: string
  releaseTag?: string
  prNumber?: number
  prTitle?: string
  ciStatus: CiStatus
  cdStatus?: CiStatus
  deploymentTarget?: string
  deployedAt?: string
}

export interface Project {
  path?: string
  rootId?: string
  available?: boolean
  id: string
  workspaceId: string
  name: string
  repository: string
  description: string
  health: Health
  defaultBranch: string
  defaultBranchInfo?: DefaultBranchInfo
  worktreeIds: string[]
  openPrCount: number
}

export interface Worktree {
	path?: string
	main?: boolean
  missing?: boolean
	headSha?: string
	connection?: { state: 'linked' | 'unlinked' | 'unknown'; reason?: string; statusUnknown?: boolean }
  id: string
  projectId: string
  branch: string
  kind: WorktreeKind
  sourceType: WorktreeSourceType
  remoteBranch?: string
  mergeTargetBranch: string
  stackPreference?: StackPreference
  status: Health
  agentIds: string[]
  prNumber?: number
  prStatus?: PrStatus
  ciStatus: CiStatus
  ciFailed: number
  /** The CI result has not been confirmed since it was restored or last read. */
  ciStale?: boolean
  /** The PR link comes from a provider catalog that is not confirmed yet. */
  prStale?: boolean
  checkedSha?: string
  upstream?: string
  divergenceAvailable?: boolean
  gitStatusError?: string
  ahead: number
  behind: number
  dirtyFiles: number
  lastActivity: string
  gitState?: string
}

export interface CreateWorktreeInput {
	projectId?: string
  sourceType: WorktreeSourceType
  sourceRef: string
  branchName?: string
  mergeTargetBranch: string
}

export interface EnvVariable {
  id: string
  key: string
  value: string
  secret: boolean
}

export type AgentProvider = 'Claude' | 'Codex' | 'Gemini' | 'Antigravity'
/** Provider IDs the local API can launch; other providers are display-only. */
export type AgentProviderId = 'antigravity' | 'claude' | 'codex'

export interface Agent {
  projectId?: string
  profileName?: string
  providerId?: AgentProviderId
  lifecycleState?: 'starting' | 'running' | 'stopping' | 'exited' | 'failed'
  id: string
  worktreeId: string
  name: string
  provider: AgentProvider
  model: string
  reasoningEffort: string
  fastMode?: boolean
  workType: string
  prompt: string
  archived: boolean
  presentation: AgentPresentation
  state: AgentState
  task: string
  runtime: string
  terminalId: string
  createdAt: string
  finishedAt?: string
}

export interface StartAgentInput {
  /** Start request fields from the provider's launch fields, sent as given. */
  options?: Record<string, string | boolean>
  accountId?: string
  requestKey?: string
  worktreeId: string
  name: string
  provider: AgentProvider
  model: string
  reasoningEffort: string
  fastMode: boolean
  workType: string
  prompt: string
}

export type ProcessLifecycleStatus = 'starting' | 'running' | 'backoff' | 'stopping' | 'stopped' | 'done' | 'failed' | 'lost' | 'orphan'

export interface Process {
  commandKey?: string
  executionOrder?: number
  id: string
  projectId: string
  daemonId: number
  worktreeId: string
  name: string
  command: string
  status: Health
  lifecycleStatus: ProcessLifecycleStatus
  revision?: number
  pendingSnapshot?: boolean
  policy?: { mode: 'no' | 'on-failure' | 'always'; max_restarts: number }
  restarts?: number
  retryCount?: number
  attempt?: number
  retryAt?: string
  pid?: number
  port?: number
  url?: string
  startedAt?: string
  exitCode?: number
  exitError?: string
  serveGroup?: string
  serveName?: string
}

export type CanvasProcess = Pick<Process, 'id' | 'projectId' | 'worktreeId' | 'name' | 'command' | 'status' | 'lifecycleStatus'>

export type SyncFreshnessState = 'loading' | 'ready' | 'stale' | 'error' | 'unavailable'

export interface SyncFreshness {
  state: SyncFreshnessState
  updatedAt?: string
  error?: { code: string; message: string }
  /** 'disk' while restored from the API's warm-start cache and not yet revalidated. */
  source?: 'disk'
}

export interface PullRequestCheck {
  id?: number
  name: string
  status: 'success' | 'running' | 'failed'
  /** ISO times; absent for commit statuses and runs that have not started or finished. */
  startedAt?: string
  completedAt?: string
}

export interface PullRequest {
	headRepository?: string
  id: string
  number: number
  title: string
  description: string
  branch: string
  base: string
  status: PrStatus
  author?: string
  createdAt: string
  updatedAt: string
  mergeable?: boolean
  checks: PullRequestCheck[]
  /** Provider totals; absent until the pull request detail has loaded. */
  totals?: { additions: number; deletions: number; changedFiles: number }
  /** Reviewer standing; absent until the pull request detail has loaded. */
  reviews?: { requested: string[]; approvals: number; changesRequested: number }
  /** Base commits missing from the branch; absent when unknown. */
  behindBy?: number
  commits: { sha: string; message: string; author: string; time?: string }[]
  conversation: { author: string; body: string; time: string; kind?: 'comment' | 'review' | 'system' | 'checks' }[]
  files: { path: string; additions: number; deletions: number; diff: string[] }[]
}

export interface RepoFile {
  id: string
  name: string
  path: string
  type: 'file' | 'folder'
  language?: string
  content?: string
  gitStatus?: FileGitStatus
  children?: RepoFile[]
}

export interface ActivityItem {
  id: string
  type: 'agent' | 'git' | 'pr' | 'process'
  title: string
  detail: string
  time: string
  status: Health
}

export type Selection =
  | { type: 'project'; id: string }
  | { type: 'worktree'; id: string }
  | { type: 'agent'; id: string }
  | { type: 'process'; id: string }

export interface RuntimeReference {
  kind: 'agent' | 'process'
  id: string
  commandKey?: string
}

export interface RuntimeViewPreference {
  open: RuntimeReference[]
  active: RuntimeReference | null
}

export type TerminalViewPreferences = Record<string, {
  lastWorktreeId: string
  reopening?: 'keep_closed' | 'restore'
  worktrees: Record<string, RuntimeViewPreference>
}>

export type DockState = 'collapsed' | 'normal' | 'maximized'
export type DockTab = 'agent' | 'terminal' | 'tests' | 'files' | 'pr' | 'checks' | 'logs'

export interface ViewportState {
  x: number
  y: number
  zoom: number
}

export interface CanvasPreferences {
  /** Centre the canvas on a worktree selected in the Branches island. */
  focusOnBranchSelect: boolean
  /** Centre the canvas on an agent selected in the Branches island. */
  focusAgentsOnBranchSelect: boolean
  /** On the GitHub tab, a Branches selection shows its pull request or switches to the canvas. */
  githubBranchSelect: 'show-pr' | 'open-canvas'
}

export const DEFAULT_CANVAS_PREFERENCES: CanvasPreferences = {
  focusOnBranchSelect: true,
  focusAgentsOnBranchSelect: true,
  githubBranchSelect: 'show-pr',
}

export type NodePlacementMode = 'manual' | 'generated'

export interface NodePlacement {
  x: number
  y: number
  mode: NodePlacementMode
}
