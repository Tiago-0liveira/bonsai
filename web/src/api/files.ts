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
  const [result, setResult] = useState<{ id: string; files: RepoFile[] }>({ id: '', files: [] })
  useEffect(() => { let stale = false; if (!id) return
    const timer = setTimeout(() => { void request<{ path: string; status: string }[]>(`/api/worktrees/${encodeURIComponent(id)}/files`).then(v => { if (!stale) setResult({ id, files: fileTree(v) }) }).catch(error => { if (!stale) report(error) }) }, 100)
    return () => { stale = true; clearTimeout(timer) }
  }, [id, revision])
  return result.id === id ? result.files : []
}
export function useFileContent(path: string) {
  const id = useBonsaiStore(s => s.dockWorktreeId), revision = useBonsaiStore(s => s.gitRevision)
  const [result, setResult] = useState({ id: '', path: '', content: '' })
  useEffect(() => { let stale = false; if (!id || !path) return
    void request<{ content: string; binary: boolean }>(`/api/worktrees/${encodeURIComponent(id)}/files/${path.split('/').map(encodeURIComponent).join('/')}`).then(v => { if (!stale) setResult({ id, path, content: v.binary ? 'Binary file' : v.content }) }).catch(error => { if (!stale) setResult({ id, path, content: error instanceof Error ? error.message : String(error) }) })
    return () => { stale = true }
  }, [id, path, revision])
  return result.id === id && result.path === path ? result.content : ''
}
export function useLocalDiff(id: string) {
  const revision = useBonsaiStore(s => s.gitRevision), [result, setResult] = useState({ id: '', patch: '' })
  useEffect(() => { let stale = false; if (!id) return
    const timer = setTimeout(() => { void request<{ patch: string }>(`/api/worktrees/${encodeURIComponent(id)}/diff`).then(v => { if (!stale) setResult({ id, patch: v.patch }) }).catch(error => { if (!stale) report(error) }) }, 100)
    return () => { stale = true; clearTimeout(timer) }
  }, [id, revision])
  return result.id === id ? result.patch : ''
}
