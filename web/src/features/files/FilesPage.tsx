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
  return <div className="island flex h-full min-h-0 overflow-hidden">
    <FileNavigation files={files} />
    <FileContent files={files} />
  </div>
}

const FileNavigation = memo(function FileNavigation({ files }: { files: RepoFile[] }) {
  const setSelectedPath = useBonsaiStore(state => state.setSelectedFilePath)
  return <aside className="w-64 shrink-0 overflow-auto border-r border-border-subtle">
    <div className="island-title gap-2"><FileCode2 size={13} /> Files</div>
    <div className="p-2">{files.map(node => <TreeNode key={node.id} node={node} depth={0} onSelect={setSelectedPath} />)}</div>
  </aside>
})

const FileContent = memo(function FileContent({ files }: { files: RepoFile[] }) {
  const selectedPath = useBonsaiStore(state => state.selectedFilePath)
  const flat = useMemo(() => flattenFiles(files), [files])
  const file = flat.find(item => item.path === selectedPath) ?? flat.find(item => item.type === 'file')
  const content = useFileContent(file?.path ?? '')
  return <section className="min-w-0 flex-1 overflow-auto">
    <div className="sticky top-0 flex h-[30px] items-center border-b border-border-subtle bg-panel px-3 font-mono text-[10.5px] text-muted">
      {file?.path ?? 'No file selected'}{file?.language && <span className="ml-auto uppercase text-muted-2">{file.language}</span>}
    </div>
    <pre className="min-h-full p-5 font-mono text-[12px] leading-5 text-muted">{content || 'Select a file from the repository tree.'}</pre>
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
        className={`bonsai-focus flex w-full items-center gap-1.5 rounded-[7px] py-1.5 pr-2 text-left text-[12px] ${
          selected ? 'bg-accent/12 text-text' : 'text-muted hover:bg-panel-3'
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
