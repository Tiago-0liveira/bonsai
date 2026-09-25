import type { RepoFile } from '../types'

export const repoFiles: RepoFile[] = [
  {
    id: 'src', name: 'src', path: 'src', type: 'folder', children: [
      {
        id: 'features', name: 'features', path: 'src/features', type: 'folder', children: [
          {
            id: 'workspace', name: 'workspace', path: 'src/features/workspace', type: 'folder', children: [
              {
                id: 'canvas', name: 'BonsaiCanvas.tsx', path: 'src/features/workspace/canvas/BonsaiCanvas.tsx', type: 'file', language: 'tsx',
                content: `export function BonsaiCanvas() {
  return <ReactFlow nodes={nodes} edges={edges} fitView />
}`,
              },
              {
                id: 'nodes', name: 'BonsaiNode.tsx', path: 'src/features/workspace/nodes/BonsaiNode.tsx', type: 'file', language: 'tsx',
                content: `export function BonsaiNode({ data }: NodeProps) {
  return <NodeCard data={data} />
}`,
              },
            ],
          },
          {
            id: 'terminal', name: 'terminal', path: 'src/features/terminal', type: 'folder', children: [
              {
                id: 'fake-terminal', name: 'FakeTerminal.tsx', path: 'src/features/terminal/FakeTerminal.tsx', type: 'file', language: 'tsx',
                content: `const terminal = new Terminal({ cursorBlink: true })
// Prototype only: output is simulated in the browser.`,
              },
            ],
          },
        ],
      },
      {
        id: 'mock', name: 'mock', path: 'src/mock', type: 'folder', children: [
          {
            id: 'mock-agents', name: 'agents.ts', path: 'src/mock/agents.ts', type: 'file', language: 'ts',
            content: `export const agents = [
  { name: 'UI builder', state: 'running' },
  { name: 'Test runner', state: 'running' },
]`,
          },
        ],
      },
    ],
  },
  {
    id: 'pkg', name: 'package.json', path: 'package.json', type: 'file', language: 'json',
    content: `{
  "name": "bonsai-web",
  "scripts": { "dev": "vite", "test": "vitest run" }
}`,
  },
  {
    id: 'readme', name: 'README.md', path: 'README.md', type: 'file', language: 'markdown',
    content: `# Bonsai web prototype

Frontend-only workspace for validating Bonsai's interaction model.`,
  },
]

export function flattenFiles(nodes: RepoFile[]): RepoFile[] {
  return nodes.flatMap((node) => [node, ...(node.children ? flattenFiles(node.children) : [])])
}
