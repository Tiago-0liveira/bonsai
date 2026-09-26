export type Health = 'healthy' | 'warning' | 'error' | 'idle'
export type WorktreeKind = 'Production' | 'Feature' | 'Bug' | 'Refactor' | 'Chore'
export type WorktreeSourceType = 'existing' | 'origin' | 'new'
export type TagColor = 'purple' | 'blue' | 'green' | 'orange' | 'red' | 'cyan' | 'pink'
export type AgentState = 'running' | 'idle' | 'finished'
export type AgentPresentation = 'canvas' | 'history' | 'archived'
export type BoardStatus = string
export type BoardKind = string
export type PrStatus = 'Draft' | 'Open' | 'Closed' | 'Merged'
export type CiStatus = 'running' | 'passed' | 'failed' | 'waiting'
export type DockPanelKey = 'files' | 'prs'
export type StackPreference = 'auto' | 'never'
export type FileGitStatus = 'modified' | 'untracked' | 'added' | 'deleted' | 'committed'
export type EditorPreference = 'vscode' | 'cursor' | 'zed' | 'system'

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

export interface WorktreeTag {
  id: string
  name: string
  color: TagColor
}

export interface Worktree {
  id: string
  projectId: string
  branch: string
  kind: WorktreeKind
  tag: string
  tagId?: string
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
  ahead: number
  behind: number
  dirtyFiles: number
  lastActivity: string
  gitState?: string
}

export interface CreateWorktreeInput {
  sourceType: WorktreeSourceType
  sourceRef: string
  branchName?: string
  tagId: string
  mergeTargetBranch: string
}

export interface EnvVariable {
  id: string
  key: string
  value: string
  secret: boolean
}

export type AgentProvider = 'Claude' | 'Codex' | 'Gemini'

export interface Agent {
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
  worktreeId: string
  name: string
  provider: AgentProvider
  model: string
  reasoningEffort: string
  fastMode: boolean
  workType: string
  prompt: string
}

export interface Process {
  id: string
  worktreeId: string
  name: string
  command: string
  status: Health
  port?: number
}

export interface PullRequest {
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
  checks: { name: string; status: 'success' | 'running' | 'failed' }[]
  commits: { sha: string; message: string; author: string; time?: string }[]
  conversation: { author: string; body: string; time: string; kind?: 'comment' | 'review' | 'system' | 'checks' }[]
  files: { path: string; additions: number; deletions: number; diff: string[] }[]
}

export interface BoardList {
  id: string
  name: string
  color: TagColor
  priority: string
  itemType: string
  order: number
  archived?: boolean
}

export interface BoardPriority {
  id: string
  name: string
  rank: number
}

export interface BoardType {
  id: string
  name: string
}

export interface BoardItem {
  id: string
  title: string
  kind: BoardKind
  status: BoardStatus
  assignee: string
  priority: string
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

export type DockState = 'collapsed' | 'normal' | 'maximized'
export type DockTab = 'agent' | 'terminal' | 'tests' | 'files' | 'pr' | 'checks' | 'logs'

export interface ViewportState {
  x: number
  y: number
  zoom: number
}

export type NodePlacementMode = 'manual' | 'generated'

export interface NodePlacement {
  x: number
  y: number
  mode: NodePlacementMode
}
