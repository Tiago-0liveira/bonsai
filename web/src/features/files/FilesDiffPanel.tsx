import { useEffect, useState } from 'react'
import * as Tabs from '@radix-ui/react-tabs'
import { ChevronDown, ChevronRight, FileCode2, Files, Folder, ListTree, X } from 'lucide-react'
import { flattenFiles, useFiles, useLocalDiff } from '../../api/files'
import { loadPullRequest } from '../../api/git'
import { useBonsaiStore } from '../../stores/bonsai'
import type { RepoFile } from '../../types'

function GitStatus({ status }: { status?: RepoFile['gitStatus'] }) {
  if (!status || status === 'committed') return null
  const label = status === 'modified' ? 'M' : status === 'untracked' ? 'U' : status === 'added' ? 'A' : 'D'
  const tone = status === 'deleted' ? 'text-[rgb(var(--danger))]' : status === 'untracked' ? 'text-[rgb(var(--accent))]' : 'text-[rgb(var(--warn))]'
  return <span className={'ml-auto shrink-0 font-mono text-[8px] ' + tone}>{label}</span>
}

function countChanged(nodes: RepoFile[]): number {
  return nodes.reduce((total, node) => {
    if (node.type === 'file') return total + (node.gitStatus && node.gitStatus !== 'committed' ? 1 : 0)
    return total + countChanged(node.children ?? [])
  }, 0)
}

function filterTree(nodes: RepoFile[], mode: 'changed' | 'committed'): RepoFile[] {
  return nodes.flatMap((node) => {
    if (node.type === 'file') {
      const changed = Boolean(node.gitStatus && node.gitStatus !== 'committed')
      return (mode === 'changed' ? changed : !changed) ? [node] : []
    }
    const children = filterTree(node.children ?? [], mode)
    return children.length ? [{ ...node, children }] : []
  })
}

function FileTreeRows({ nodes, depth = 0 }: { nodes: RepoFile[]; depth?: number }) {
  const requestOpenFile = useBonsaiStore((state) => state.requestOpenFile)
  const [openFolders, setOpenFolders] = useState<string[]>(['src', 'features', 'workspace'])
  return (
    <>
      {nodes.map((node) => {
        if (node.type === 'folder') {
          const open = openFolders.includes(node.id)
          return (
            <div key={node.id}>
              <button
                type="button"
                onClick={() => setOpenFolders((items) => open ? items.filter((id) => id !== node.id) : [...items, node.id])}
                style={{ paddingLeft: 6 + depth * 12 }}
                className="flex h-6 w-full items-center gap-1.5 rounded pr-2 text-left text-[9px] text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]"
              >
                {open ? <ChevronDown size={9} /> : <ChevronRight size={9} />}
                <Folder size={10} />
                <span className="min-w-0 flex-1 truncate">{node.name}</span>
                {countChanged(node.children ?? []) > 0 && <span className="text-[7px] text-[rgb(var(--warn))]">{countChanged(node.children ?? [])}</span>}
              </button>
              {open && node.children && <FileTreeRows nodes={node.children} depth={depth + 1} />}
            </div>
          )
        }
        return (
          <button
            type="button"
            key={node.id}
            onClick={() => requestOpenFile(node.path)}
            style={{ paddingLeft: 20 + depth * 12 }}
            className="flex h-6 w-full items-center gap-1.5 rounded pr-2 text-left font-mono text-[8px] text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))] hover:text-[rgb(var(--text))]"
          >
            <FileCode2 size={9} />
            <span className="min-w-0 flex-1 truncate">{node.name}</span>
            <GitStatus status={node.gitStatus} />
          </button>
        )
      })}
    </>
  )
}

export function FilesDiffPanel({ embedded = false }: { embedded?: boolean }) {
  const repoFiles = useFiles()
  const setRightPanel = useBonsaiStore((state) => state.setRightPanel)
  const dockWorktreeId = useBonsaiStore((state) => state.dockWorktreeId)
  const worktree = useBonsaiStore(state => state.worktrees.find(item => item.id === dockWorktreeId))
  const pr = useBonsaiStore(state => state.pullRequests.find(item => item.id === `${worktree?.projectId}:${worktree?.prNumber}`))
  const requestOpenFile = useBonsaiStore((state) => state.requestOpenFile)
  const [view, setView] = useState<'flat' | 'tree'>('tree')
  const patch = useLocalDiff(dockWorktreeId)
  useEffect(() => { if (pr?.id) void loadPullRequest(pr.id) }, [pr?.id])
  const Wrapper = embedded ? 'div' : 'aside'
  const files = flattenFiles(repoFiles).filter((item) => item.type === 'file')
  const changed = files.filter((item) => item.gitStatus && item.gitStatus !== 'committed')
  const committed = files.filter((item) => !item.gitStatus || item.gitStatus === 'committed')
  const changedTree = filterTree(repoFiles, 'changed')
  const committedTree = filterTree(repoFiles, 'committed')

  return (
    <Wrapper className={embedded ? 'flex h-full min-h-0 min-w-0 flex-col' : 'dock-pane flex h-full min-w-0 flex-col'}>
      <Tabs.Root defaultValue="files" className="flex min-h-0 flex-1 flex-col">
        <div className={'flex shrink-0 items-center px-2 ' + (embedded ? 'h-9 border-b border-border-subtle' : 'dock-heading')}>
          <Tabs.List className="dock-tabs flex h-full items-center gap-1">
            <Tabs.Trigger value="files" className="bonsai-focus dock-tab">Files</Tabs.Trigger>
            <Tabs.Trigger value="diff" className="bonsai-focus dock-tab">Git Diff</Tabs.Trigger>
          </Tabs.List>
          <div className="ml-auto flex items-center gap-1">
            <button onClick={() => setView(view === 'tree' ? 'flat' : 'tree')} className="bonsai-focus grid h-6 w-6 place-items-center rounded text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]" title={view === 'tree' ? 'Flat file list' : 'File tree'}>
              {view === 'tree' ? <Files size={11} /> : <ListTree size={11} />}
            </button>
            {!embedded && <button onClick={() => setRightPanel('files', false)} className="bonsai-focus grid h-6 w-6 place-items-center rounded text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]" title="Close Files / Git Diff"><X size={11} /></button>}
          </div>
        </div>

        <Tabs.Content value="files" className="min-h-0 flex-1 overflow-auto p-1.5 outline-none">
          {view === 'tree' ? (
            <>
              <div className="px-2 pb-1 pt-1 text-[7px] font-semibold uppercase tracking-[.12em] text-[rgb(var(--muted-2))]">Changed</div>
              {changedTree.length ? <FileTreeRows nodes={changedTree} /> : <div className="px-2 py-2 text-[8px] text-[rgb(var(--muted-2))]">No changed files</div>}
              <div className="mt-2 border-t border-[rgb(var(--border))] px-2 pb-1 pt-2 text-[7px] font-semibold uppercase tracking-[.12em] text-[rgb(var(--muted-2))]">Repository</div>
              <FileTreeRows nodes={committedTree} />
            </>
          ) : (
            <>
              <div className="px-2 pb-1 pt-1 text-[7px] font-semibold uppercase tracking-[.12em] text-[rgb(var(--muted-2))]">Changed</div>
              {changed.map((file) => (
                <button key={file.id} onClick={() => requestOpenFile(file.path)} className="flex h-6 w-full items-center gap-1.5 rounded px-2 text-left font-mono text-[8px] text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))] hover:text-[rgb(var(--text))]">
                  <FileCode2 size={9} /><span className="min-w-0 flex-1 truncate">{file.path}</span><GitStatus status={file.gitStatus} />
                </button>
              ))}
              <div className="mt-2 border-t border-[rgb(var(--border))] px-2 pb-1 pt-2 text-[7px] font-semibold uppercase tracking-[.12em] text-[rgb(var(--muted-2))]">Repository</div>
              {committed.map((file) => (
                <button key={file.id} onClick={() => requestOpenFile(file.path)} className="flex h-6 w-full items-center gap-1.5 rounded px-2 text-left font-mono text-[8px] text-[rgb(var(--muted-2))] hover:bg-[rgb(var(--panel-2))] hover:text-[rgb(var(--text))]">
                  <FileCode2 size={9} /><span className="truncate">{file.path}</span>
                </button>
              ))}
            </>
          )}
        </Tabs.Content>

        <Tabs.Content value="diff" className="min-h-0 flex-1 overflow-auto p-2.5 outline-none">
          {patch && <pre className="overflow-auto whitespace-pre font-mono text-[9px]">{patch}</pre>}
          {pr?.files.length ? pr.files.map((file) => (
            <div key={file.path} className="mb-2 overflow-hidden rounded-md border border-[rgb(var(--border))]">
              <div className="flex items-center border-b border-[rgb(var(--border))] bg-[rgb(var(--bg))] px-2 py-1.5 font-mono text-[8px]">
                <span className="min-w-0 flex-1 truncate">{file.path}</span>
                <span className="text-[rgb(var(--accent))]">+{file.additions}</span>
                <span className="ml-1 text-[rgb(var(--danger))]">-{file.deletions}</span>
              </div>
              <pre className="overflow-auto p-2 font-mono text-[8px] leading-4 text-[rgb(var(--muted))]">{file.diff.join('\n')}</pre>
            </div>
          )) : (
            <div className="rounded-md border border-dashed border-[rgb(var(--border))] p-4 text-center text-[9px] text-[rgb(var(--muted-2))]">No PR diff linked to this worktree.</div>
          )}
        </Tabs.Content>
      </Tabs.Root>
    </Wrapper>
  )
}
