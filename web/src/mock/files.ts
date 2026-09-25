import type { RepoFile } from '../types'

export const repoFiles: RepoFile[] = [
  {
    id: 'src', name: 'src', path: 'src', type: 'folder', children: [
      {
        id: 'features', name: 'features', path: 'src/features', type: 'folder', children: [
          {
            id: 'workspace', name: 'workspace', path: 'src/features/workspace', type: 'folder', children: [
              {
                id: 'canvas', name: 'BonsaiCanvas.tsx', path: 'src/features/workspace/canvas/BonsaiCanvas.tsx', type: 'file', language: 'tsx', gitStatus: 'modified',
                content: 'export function BonsaiCanvas() {\n  return <ReactFlow nodes={nodes} edges={edges} />\n}',
              },
              {
                id: 'nodes', name: 'BonsaiNode.tsx', path: 'src/features/workspace/nodes/BonsaiNode.tsx', type: 'file', language: 'tsx', gitStatus: 'modified',
                content: 'export function BonsaiNode({ data }: NodeProps) {\n  return <NodeCard data={data} />\n}',
              },
            ],
          },
          {
            id: 'terminal', name: 'terminal', path: 'src/features/terminal', type: 'folder', children: [
              {
                id: 'fake-terminal', name: 'FakeTerminal.tsx', path: 'src/features/terminal/FakeTerminal.tsx', type: 'file', language: 'tsx', gitStatus: 'untracked',
                content: 'const terminal = new Terminal({ cursorBlink: true })',
              },
            ],
          },
        ],
      },
      {
        id: 'mock', name: 'mock', path: 'src/mock', type: 'folder', children: [
          {
            id: 'mock-agents', name: 'agents.ts', path: 'src/mock/agents.ts', type: 'file', language: 'ts', gitStatus: 'committed',
            content: 'export const agents = []',
          },
        ],
      },
    ],
  },
  {
    id: 'pkg', name: 'package.json', path: 'package.json', type: 'file', language: 'json', gitStatus: 'committed',
    content: '{\n  "name": "bonsai-web"\n}',
  },
  {
    id: 'readme', name: 'README.md', path: 'README.md', type: 'file', language: 'markdown', gitStatus: 'committed',
    content: '# Bonsai web prototype',
  },
]

export function flattenFiles(nodes: RepoFile[]): RepoFile[] {
  return nodes.flatMap((node) => [node, ...(node.children ? flattenFiles(node.children) : [])])
}
