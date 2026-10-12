import { useEffect } from 'react'
import {
  AppWindow,
  ArrowDownToLine,
  ArrowUpFromLine,
  Ban,
  Bot,
  Copy,
  ExternalLink,
  FolderOpen,
  GitBranch,
  GitPullRequest,
  GitPullRequestArrow,
  GitPullRequestCreate,
  Layers3,
  Link,
  ListChecks,
  PanelRight,
  Play,
  RefreshCw,
  SquareCode,
  StepForward,
  Trash2,
} from 'lucide-react'
import { fileManagerAction } from '../../../api/editors'
import { branchPath, githubUrl, localCommand, openGitHub, report } from '../../../api/git'
import { useShellRoute } from '../../../app/shellRoute'
import { MenuItem, MenuSeparator, MenuSub } from '../../../components/ui/ContextMenu'
import { useBonsaiStore } from '../../../stores/bonsai'

const NO_PR = 'No PR linked'

function copy(text: string, what: string) {
  void navigator.clipboard.writeText(text).then(
    () => useBonsaiStore.getState().setNotice('Copied ' + what),
    report,
  )
}

/**
 * The actions for one worktree, shared by worktree nodes and stack rows.
 * Rendered inside a context menu's content, so it mounts only while open.
 */
export function WorktreeMenuItems({ worktreeId, groupId, groupCount = 0 }: { worktreeId: string; groupId?: string; groupCount?: number }) {
  const worktree = useBonsaiStore(state => state.worktrees.find(tree => tree.id === worktreeId))
  const project = useBonsaiStore(state => state.projects.find(item => item.id === (worktree?.projectId ?? state.activeProjectId)))
  const fileManager = useBonsaiStore(state => state.editorSettings?.file_manager?.label)
  const { showPullRequest } = useShellRoute()
  useEffect(() => {
    const state = useBonsaiStore.getState()
    if (!state.editorSettings && !state.editorSettingsLoading) void state.loadEditorSettings().catch(() => undefined)
  }, [])
  const store = useBonsaiStore.getState

  const branch = worktree?.branch ?? ''
  const target = worktree?.mergeTargetBranch || project?.defaultBranch || ''
  const rebaseTarget = !!target && target !== branch
  const pr = worktree?.prNumber
  const projectId = worktree?.projectId ?? project?.id ?? ''
  const onGitHub = !!githubUrl('', projectId)
  const missing = !worktree || !!worktree.missing
  // Git keeps a merge or rebase in progress until it is continued or aborted.
  const operation = worktree?.gitState === 'rebase' || worktree?.gitState === 'merge' ? worktree.gitState : undefined
  const gitHint = missing ? 'Worktree missing' : operation ? (operation === 'rebase' ? 'Rebase' : 'Merge') + ' in progress' : undefined
  const run = (action: string, args: Record<string, unknown> = {}) => { void localCommand(action, args, worktreeId) }

  return <>
    <MenuItem icon={PanelRight} onSelect={() => store().setSelection({ type: 'worktree', id: worktreeId })}>Open inspector</MenuItem>
    <MenuItem icon={Bot} onSelect={() => store().openStartAgentDialog(worktreeId)}>Start agent</MenuItem>
    <MenuItem icon={Play} onSelect={() => store().openStartProcessDialog(worktreeId)}>Start process</MenuItem>
    <MenuSeparator />
    <MenuItem icon={SquareCode} disabled={missing} onSelect={() => { void store().requestOpenFolder(worktreeId) }}>Open in editor</MenuItem>
    <MenuItem icon={FolderOpen} disabled={missing} onSelect={() => { void store().revealFolder(worktreeId) }}>{fileManagerAction(fileManager)}</MenuItem>
    <MenuSeparator />
    <MenuSub icon={GitBranch} label="Git">
      <MenuItem icon={ArrowDownToLine} disabled={!!gitHint} hint={gitHint} onSelect={() => run('pull')}>Pull</MenuItem>
      <MenuItem icon={ArrowUpFromLine} disabled={!!gitHint} hint={gitHint} onSelect={() => run('push', worktree?.upstream ? {} : { set_upstream: true })}>Push</MenuItem>
      <MenuItem icon={RefreshCw} onSelect={() => run('fetch')}>Fetch</MenuItem>
      {(rebaseTarget || operation) && <MenuSeparator />}
      {rebaseTarget && <MenuItem icon={GitPullRequestArrow} disabled={!!gitHint} hint={gitHint} onSelect={() => run('rebase', { target })}>Rebase onto {target}</MenuItem>}
      {operation && <>
        <MenuItem icon={StepForward} onSelect={() => run('continue')}>Continue {operation}</MenuItem>
        <MenuItem icon={Ban} onSelect={() => run('abort')}>Abort {operation}</MenuItem>
      </>}
      <MenuSeparator />
      <MenuItem icon={Copy} disabled={!branch} onSelect={() => copy(branch, 'branch name')}>Copy branch name</MenuItem>
    </MenuSub>
    <MenuSub icon={GitPullRequest} label="GitHub" disabled={!onGitHub} hint={onGitHub ? undefined : 'Not on GitHub'}>
      <MenuItem icon={ExternalLink} disabled={!pr} hint={pr ? '#' + pr : NO_PR} onSelect={() => openGitHub('pull/' + pr, projectId)}>Open PR on GitHub</MenuItem>
      <MenuItem icon={AppWindow} disabled={!pr} onSelect={() => showPullRequest(`${projectId}:${pr}`)}>Show in GitHub tab</MenuItem>
      <MenuItem icon={Link} disabled={!pr} onSelect={() => copy(githubUrl('pull/' + pr, projectId) ?? '', 'pull request link')}>Copy PR link</MenuItem>
      <MenuItem icon={ListChecks} disabled={!pr} onSelect={() => openGitHub(`pull/${pr}/checks`, projectId)}>Open checks</MenuItem>
      <MenuSeparator />
      {!pr && rebaseTarget && <MenuItem icon={GitPullRequestCreate} disabled={!branch} onSelect={() => openGitHub(`compare/${branchPath(target)}...${branchPath(branch)}?expand=1`, projectId)}>Create pull request</MenuItem>}
      <MenuItem icon={GitBranch} disabled={!branch} onSelect={() => openGitHub('tree/' + branchPath(branch), projectId)}>Open branch on GitHub</MenuItem>
    </MenuSub>
    {groupId && groupCount > 1 && <MenuItem icon={Layers3} onSelect={() => store().toggleAutomaticGroup(groupId)}>Toggle Local / unlinked stack</MenuItem>}
    <MenuSeparator />
    <MenuItem icon={Trash2} danger onSelect={() => store().setDeleteWorktreeId(worktreeId)}>Delete worktree</MenuItem>
  </>
}
