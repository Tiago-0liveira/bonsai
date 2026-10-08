import { useState } from 'react'
import { ChevronDown, ChevronRight, FileCode2, Files, Folder, ListTree } from 'lucide-react'
import { flattenFiles, useFiles } from '../../api/files'
import { useBonsaiStore } from '../../stores/bonsai'
import type { RepoFile } from '../../types'

function GitStatus({ status }: { status?: RepoFile['gitStatus'] }) {
  if (!status || status === 'committed') return null
  const label = status === 'modified' ? 'M' : status === 'untracked' ? 'U' : status === 'added' ? 'A' : 'D'
  const tone = status === 'deleted' ? 'text-danger' : status === 'untracked' ? 'text-accent' : 'text-warn'
  return <span className={'ml-auto shrink-0 font-mono text-[10px] ' + tone}>{label}</span>
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
                className="flex h-7 w-full items-center gap-1.5 rounded-md pr-2 text-left text-[11px] text-muted hover:bg-panel-2"
              >
                {open ? <ChevronDown size={10} /> : <ChevronRight size={10} />}
                <Folder size={11} />
                <span className="min-w-0 flex-1 truncate">{node.name}</span>
                {countChanged(node.children ?? []) > 0 && <span className="font-mono text-[9.5px] text-warn">{countChanged(node.children ?? [])}</span>}
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
            className="flex h-7 w-full items-center gap-1.5 rounded-md pr-2 text-left font-mono text-[11px] text-muted hover:bg-panel-2 hover:text-text"
          >
            <FileCode2 size={11} className="shrink-0" />
            <span className="min-w-0 flex-1 truncate">{node.name}</span>
            <GitStatus status={node.gitStatus} />
          </button>
        )
      })}
    </>
  )
}

const sectionLabel = 'px-2 pb-1 pt-2 font-mono text-[9.5px] font-medium uppercase tracking-[.08em] text-muted-2'

export function FilesPanel() {
  const repoFiles = useFiles()
  const requestOpenFile = useBonsaiStore((state) => state.requestOpenFile)
  const [view, setView] = useState<'flat' | 'tree'>('tree')
  const files = flattenFiles(repoFiles).filter((item) => item.type === 'file')
  const changed = files.filter((item) => item.gitStatus && item.gitStatus !== 'committed')
  const committed = files.filter((item) => !item.gitStatus || item.gitStatus === 'committed')
  const changedTree = filterTree(repoFiles, 'changed')
  const committedTree = filterTree(repoFiles, 'committed')

  return (
    <div className="flex h-full min-h-0 min-w-0 flex-col">
      <div className="flex h-9 shrink-0 items-center gap-2 border-b border-border-subtle px-3">
        <span className="font-mono text-[10px] text-muted-2">{changed.length} changed · {files.length} files</span>
        <button type="button" onClick={() => setView(view === 'tree' ? 'flat' : 'tree')} className="bonsai-focus btn-ghost ml-auto h-6 w-6 justify-center px-0" title={view === 'tree' ? 'Flat file list' : 'File tree'} aria-label={view === 'tree' ? 'Flat file list' : 'File tree'}>
          {view === 'tree' ? <Files size={12} /> : <ListTree size={12} />}
        </button>
      </div>
      <div className="min-h-0 flex-1 overflow-auto p-2">
        <div className={sectionLabel}>Changed</div>
        {view === 'tree'
          ? (changedTree.length ? <FileTreeRows nodes={changedTree} /> : <div className="px-2 py-2 text-[11px] text-muted-2">No changed files</div>)
          : (changed.length
            ? changed.map((file) => (
              <button type="button" key={file.id} onClick={() => requestOpenFile(file.path)} className="flex h-7 w-full items-center gap-1.5 rounded-md px-2 text-left font-mono text-[11px] text-muted hover:bg-panel-2 hover:text-text">
                <FileCode2 size={11} className="shrink-0" /><span className="min-w-0 flex-1 truncate">{file.path}</span><GitStatus status={file.gitStatus} />
              </button>
            ))
            : <div className="px-2 py-2 text-[11px] text-muted-2">No changed files</div>)}
        <div className={sectionLabel + ' mt-2 border-t border-border-subtle pt-3'}>Repository</div>
        {view === 'tree'
          ? <FileTreeRows nodes={committedTree} />
          : committed.map((file) => (
            <button type="button" key={file.id} onClick={() => requestOpenFile(file.path)} className="flex h-7 w-full items-center gap-1.5 rounded-md px-2 text-left font-mono text-[11px] text-muted-2 hover:bg-panel-2 hover:text-text">
              <FileCode2 size={11} className="shrink-0" /><span className="min-w-0 flex-1 truncate">{file.path}</span>
            </button>
          ))}
      </div>
    </div>
  )
}
