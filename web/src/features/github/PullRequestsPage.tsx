import { useMemo, useState } from 'react'
import * as Tabs from '@radix-ui/react-tabs'
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from '@tanstack/react-table'
import {
  CheckCircle2,
  CircleDot,
  GitCommitHorizontal,
  GitPullRequest,
  MessageSquare,
  XCircle,
} from 'lucide-react'
import { pullRequests } from '../../mock/pullRequests'
import type { PullRequest } from '../../types'

const columns: ColumnDef<PullRequest>[] = [
  {
    accessorKey: 'number',
    header: '#',
    cell: ({ row }) => <span className="font-mono text-[rgb(var(--muted-2))]">#{row.original.number}</span>,
  },
  {
    accessorKey: 'title',
    header: 'Pull request',
    cell: ({ row }) => (
      <div>
        <div className="font-medium text-[rgb(var(--text))]">{row.original.title}</div>
        <div className="mt-1 font-mono text-[10px] text-[rgb(var(--muted-2))]">{row.original.branch} → {row.original.base}</div>
      </div>
    ),
  },
  {
    accessorKey: 'status',
    header: 'Status',
    cell: ({ row }) => (
      <span className="inline-flex items-center gap-1.5 rounded border border-[rgb(var(--border))] px-1.5 py-0.5 text-[10px]">
        <CircleDot size={10} className={row.original.status === 'Open' ? 'text-[rgb(var(--green))]' : 'text-[rgb(var(--orange))]'} />
        {row.original.status}
      </span>
    ),
  },
  {
    id: 'checks',
    header: 'Checks',
    cell: ({ row }) => {
      const success = row.original.checks.filter((check) => check.status === 'success').length
      return <span className="text-[11px] text-[rgb(var(--muted))]">{success}/{row.original.checks.length}</span>
    },
  },
]

export function PullRequestsPage() {
  const [selectedId, setSelectedId] = useState(pullRequests[0].id)
  const selected = pullRequests.find((pr) => pr.id === selectedId) ?? pullRequests[0]
  const table = useReactTable({
    data: pullRequests,
    columns,
    getCoreRowModel: getCoreRowModel(),
  })

  const totals = useMemo(
    () => ({
      open: pullRequests.filter((pr) => pr.status === 'Open').length,
      draft: pullRequests.filter((pr) => pr.status === 'Draft').length,
    }),
    [],
  )

  return (
    <div className="flex h-full min-h-0 flex-col bg-[rgb(var(--bg))]">
      <div className="flex h-12 shrink-0 items-center border-b border-[rgb(var(--border))] px-4">
        <GitPullRequest size={15} className="mr-2 text-[rgb(var(--muted))]" />
        <span className="font-medium">Pull Requests</span>
        <span className="ml-3 text-[11px] text-[rgb(var(--muted-2))]">{totals.open} open · {totals.draft} draft</span>
      </div>

      <div className="grid min-h-0 flex-1 grid-cols-[minmax(330px,42%)_1fr]">
        <div className="overflow-auto border-r border-[rgb(var(--border))]">
          <table className="w-full border-collapse">
            <thead className="sticky top-0 z-10 bg-[rgb(var(--panel))]">
              {table.getHeaderGroups().map((headerGroup) => (
                <tr key={headerGroup.id} className="border-b border-[rgb(var(--border))]">
                  {headerGroup.headers.map((header) => (
                    <th key={header.id} className="px-3 py-2 text-left text-[10px] font-medium uppercase tracking-wider text-[rgb(var(--muted-2))]">
                      {flexRender(header.column.columnDef.header, header.getContext())}
                    </th>
                  ))}
                </tr>
              ))}
            </thead>
            <tbody>
              {table.getRowModel().rows.map((row) => (
                <tr
                  key={row.id}
                  onClick={() => setSelectedId(row.original.id)}
                  className={`cursor-pointer border-b border-[rgb(var(--border))] transition-colors ${
                    row.original.id === selected.id ? 'bg-[rgb(var(--purple)/.08)]' : 'hover:bg-[rgb(var(--panel))]'
                  }`}
                >
                  {row.getVisibleCells().map((cell) => (
                    <td key={cell.id} className="px-3 py-3 align-top text-[11px]">
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        <div className="min-w-0 overflow-auto">
          <div className="border-b border-[rgb(var(--border))] p-4">
            <div className="flex items-start gap-3">
              <div className="mt-0.5 grid h-8 w-8 place-items-center rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--panel-2))]">
                <GitPullRequest size={15} />
              </div>
              <div>
                <h1 className="text-[15px] font-medium">{selected.title}</h1>
                <div className="mt-1 text-[11px] text-[rgb(var(--muted))]">
                  #{selected.number} · {selected.branch} → {selected.base}
                </div>
              </div>
            </div>
          </div>

          <Tabs.Root defaultValue="conversation">
            <Tabs.List className="flex h-9 border-b border-[rgb(var(--border))] px-3">
              {[
                ['conversation', 'Conversation'],
                ['commits', 'Commits'],
                ['checks', 'Checks'],
                ['files', 'Files'],
              ].map(([value, label]) => (
                <Tabs.Trigger key={value} value={value} className="relative px-3 text-[11px] text-[rgb(var(--muted))] data-[state=active]:text-[rgb(var(--text))] data-[state=active]:after:absolute data-[state=active]:after:bottom-0 data-[state=active]:after:left-3 data-[state=active]:after:right-3 data-[state=active]:after:h-px data-[state=active]:after:bg-[rgb(var(--purple))]">
                  {label}
                </Tabs.Trigger>
              ))}
            </Tabs.List>

            <Tabs.Content value="conversation" className="p-4 outline-none">
              <div className="space-y-3">
                {selected.conversation.map((comment, index) => (
                  <div key={index} className="rounded-md border border-[rgb(var(--border))] bg-[rgb(var(--panel))]">
                    <div className="flex items-center gap-2 border-b border-[rgb(var(--border))] px-3 py-2 text-[10px] text-[rgb(var(--muted-2))]">
                      <MessageSquare size={11} /> <span className="text-[rgb(var(--text))]">{comment.author}</span><span>commented {comment.time}</span>
                    </div>
                    <div className="p-3 text-[12px] leading-5 text-[rgb(var(--muted))]">{comment.body}</div>
                  </div>
                ))}
              </div>
            </Tabs.Content>

            <Tabs.Content value="commits" className="p-4 outline-none">
              {selected.commits.map((commit) => (
                <div key={commit.sha} className="flex items-center gap-3 border-b border-[rgb(var(--border))] py-3 text-[11px] last:border-0">
                  <GitCommitHorizontal size={13} className="text-[rgb(var(--muted-2))]" />
                  <span>{commit.message}</span>
                  <span className="ml-auto font-mono text-[rgb(var(--purple))]">{commit.sha}</span>
                </div>
              ))}
            </Tabs.Content>

            <Tabs.Content value="checks" className="p-4 outline-none">
              {selected.checks.map((check) => (
                <div key={check.name} className="flex items-center gap-2 border-b border-[rgb(var(--border))] py-3 text-[11px] last:border-0">
                  {check.status === 'success' ? (
                    <CheckCircle2 size={14} className="text-[rgb(var(--green))]" />
                  ) : check.status === 'failed' ? (
                    <XCircle size={14} className="text-[rgb(var(--red))]" />
                  ) : (
                    <CircleDot size={14} className="text-[rgb(var(--orange))]" />
                  )}
                  {check.name}
                  <span className="ml-auto capitalize text-[rgb(var(--muted-2))]">{check.status}</span>
                </div>
              ))}
            </Tabs.Content>

            <Tabs.Content value="files" className="p-4 outline-none">
              <div className="space-y-3">
                {selected.files.map((file) => (
                  <div key={file.path} className="overflow-hidden rounded-md border border-[rgb(var(--border))]">
                    <div className="flex items-center border-b border-[rgb(var(--border))] bg-[rgb(var(--panel))] px-3 py-2 font-mono text-[10px]">
                      {file.path}
                      <span className="ml-auto"><span className="text-[rgb(var(--green))]">+{file.additions}</span> <span className="text-[rgb(var(--red))]">-{file.deletions}</span></span>
                    </div>
                    <pre className="overflow-auto p-3 font-mono text-[10px] leading-5 text-[rgb(var(--muted))]">{file.diff.join('\n')}</pre>
                  </div>
                ))}
              </div>
            </Tabs.Content>
          </Tabs.Root>
        </div>
      </div>
    </div>
  )
}
