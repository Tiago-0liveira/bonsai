import { useEffect, useState } from 'react'
import { useBonsaiStore } from '../stores/bonsai'
import type { RepoFile } from '../types'
import { request, report } from './git'

export function flattenFiles(nodes: RepoFile[]): RepoFile[] { return nodes.flatMap(node => [node, ...flattenFiles(node.children ?? [])]) }
export function fileTree(entries: { path: string; status: string }[]): RepoFile[] {
  const roots: RepoFile[] = []
  for (const entry of entries) {
    const parts = entry.path.split('/'); let children = roots
    parts.forEach((name, index) => {
      const path = parts.slice(0, index + 1).join('/'), folder = index !== parts.length - 1
      let node = children.find(n => n.path === path)
      if (!node) { node = { id: path, name, path, type: folder ? 'folder' : 'file', ...(folder ? { children: [] } : { gitStatus: entry.status === '??' ? 'untracked' : entry.status.includes('D') ? 'deleted' : entry.status.includes('A') ? 'added' : entry.status ? 'modified' : 'committed' }) }; children.push(node) }
      if (folder) children = node.children!
    })
  }
  return roots
}
export function useFiles(worktreeID?: string) {
  const dock = useBonsaiStore(s => s.dockWorktreeId), revision = useBonsaiStore(s => s.gitRevision)
  const id = worktreeID ?? dock
  const [files, setFiles] = useState<RepoFile[]>([])
  useEffect(() => { let stale = false; setFiles([]); if (!id) return
    const timer = setTimeout(() => { void request<{ path: string; status: string }[]>(`/api/worktrees/${encodeURIComponent(id)}/files`).then(v => { if (!stale) setFiles(fileTree(v)) }).catch(error => { if (!stale) report(error) }) }, 100)
    return () => { stale = true; clearTimeout(timer) }
  }, [id, revision])
  return files
}
export function useFileContent(path: string) {
  const id = useBonsaiStore(s => s.dockWorktreeId), revision = useBonsaiStore(s => s.gitRevision)
  const [content, setContent] = useState('')
  useEffect(() => { let stale = false; setContent(''); if (!id || !path) return
    void request<{ content: string; binary: boolean }>(`/api/worktrees/${encodeURIComponent(id)}/files/${path.split('/').map(encodeURIComponent).join('/')}`).then(v => { if (!stale) setContent(v.binary ? 'Binary file' : v.content) }).catch(error => { if (!stale) setContent(error instanceof Error ? error.message : String(error)) })
    return () => { stale = true }
  }, [id, path, revision])
  return content
}
export function useLocalDiff(id: string) {
  const revision = useBonsaiStore(s => s.gitRevision), [patch, setPatch] = useState('')
  useEffect(() => { let stale = false; setPatch(''); if (!id) return
    const timer = setTimeout(() => { void request<{ patch: string }>(`/api/worktrees/${encodeURIComponent(id)}/diff`).then(v => { if (!stale) setPatch(v.patch) }).catch(error => { if (!stale) report(error) }) }, 100)
    return () => { stale = true; clearTimeout(timer) }
  }, [id, revision])
  return patch
}
