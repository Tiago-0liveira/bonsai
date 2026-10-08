import { memo, useMemo, useState } from 'react'
import {
  ChevronDown,
  ChevronRight,
  File,
  FileCode2,
  Folder,
  FolderOpen,
} from 'lucide-react'
import { flattenFiles, useFiles, useFileContent } from '../../api/files'
import { useBonsaiStore } from '../../stores/bonsai'
import type { RepoFile } from '../../types'

export function FilesPage() {
  const files = useFiles()
  return <div className="flex h-full min-h-0 bg-[rgb(var(--bg))]">
    <FileNavigation files={files} />
    <FileContent files={files} />
  </div>
}

const FileNavigation = memo(function FileNavigation({ files }: { files: RepoFile[] }) {
  const setSelectedPath = useBonsaiStore(state => state.setSelectedFilePath)
  return <aside className="w-64 shrink-0 overflow-auto border-r border-[rgb(var(--border))] bg-[rgb(var(--panel))]">
    <div className="flex h-10 items-center gap-2 border-b border-[rgb(var(--border))] px-3 font-medium"><FileCode2 size={14} /> Files</div>
    <div className="p-2">{files.map(node => <TreeNode key={node.id} node={node} depth={0} onSelect={setSelectedPath} />)}</div>
  </aside>
})

const FileContent = memo(function FileContent({ files }: { files: RepoFile[] }) {
  const selectedPath = useBonsaiStore(state => state.selectedFilePath)
  const flat = useMemo(() => flattenFiles(files), [files])
  const file = flat.find(item => item.path === selectedPath) ?? flat.find(item => item.type === 'file')
  const content = useFileContent(file?.path ?? '')
  return <section className="min-w-0 flex-1 overflow-auto">
    <div className="sticky top-0 flex h-10 items-center border-b border-[rgb(var(--border))] bg-[rgb(var(--panel))] px-3 font-mono text-[10px] text-[rgb(var(--muted))]">
      {file?.path ?? 'No file selected'}{file?.language && <span className="ml-auto uppercase text-[rgb(var(--muted-2))]">{file.language}</span>}
    </div>
    <pre className="min-h-full p-5 font-mono text-[11px] leading-5 text-[rgb(var(--muted))]">{content || 'Select a file from the repository tree.'}</pre>
  </section>
})

const TreeNode = memo(function TreeNode({
  node,
  depth,
  onSelect,
}: {
  node: RepoFile
  depth: number
  onSelect: (path: string) => void
}) {
  const selected = useBonsaiStore(state => state.selectedFilePath === node.path)
  const [open, setOpen] = useState(depth < 1)
  const isFolder = node.type === 'folder'
  return (
    <div>
      <button
        onClick={() => {
          if (isFolder) setOpen(!open)
          else onSelect(node.path)
        }}
        style={{ paddingLeft: 8 + depth * 14 }}
        className={`bonsai-focus flex w-full items-center gap-1.5 rounded py-1.5 pr-2 text-left text-[11px] ${
          selected ? 'bg-[rgb(var(--accent)/.10)] text-[rgb(var(--text))]' : 'text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]'
        }`}
      >
        {isFolder ? open ? <ChevronDown size={11} /> : <ChevronRight size={11} /> : <span className="w-[11px]" />}
        {isFolder ? open ? <FolderOpen size={13} /> : <Folder size={13} /> : <File size={13} />}
        <span className="truncate">{node.name}</span>
      </button>
      {isFolder && open && node.children?.map((child) => (
        <TreeNode key={child.id} node={child} depth={depth + 1} onSelect={onSelect} />
      ))}
    </div>
  )
})
