import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { useState } from 'react'
import type { PullRequest } from '../../types'
import { stackPr, threeDeepStack } from '../../test/fixtures/prStacks'
import { PullRequestListPane, type PullRequestListPaneProps } from './PullRequestListPane'

afterEach(cleanup)

const checks = (passed: number, running = 0, failed = 0): PullRequest['checks'] => [
  ...Array.from({ length: passed }, (_, i) => ({ name: `ok-${i}`, status: 'success' as const })),
  ...Array.from({ length: running }, (_, i) => ({ name: `run-${i}`, status: 'running' as const })),
  ...Array.from({ length: failed }, (_, i) => ({ name: `bad-${i}`, status: 'failed' as const })),
]

function fixture() {
  const { a, b, c } = threeDeepStack()
  a.title = 'Add auth API'
  a.checks = checks(3)
  b.title = 'Add auth UI'
  b.checks = checks(2, 1)
  c.title = 'Polish auth copy'
  c.checks = checks(1, 0, 1)
  const draft = stackPr(10, 'feat/landing', 'main', { title: 'Parallax landing', status: 'Draft', author: 'Mira', updatedAt: 'Sep 29' })
  const lone = stackPr(11, 'fix/typo', 'main', { title: 'Fix typo in README', author: 'Ada' })
  return { a, b, c, draft, lone, rows: [draft, c, lone, a, b] }
}

function Harness({ onSelectSpy, ...props }: Partial<PullRequestListPaneProps> & { onSelectSpy?: (id: string) => void }) {
  const [selectedId, setSelectedId] = useState<string | undefined>(props.selectedId)
  const [tab, setTab] = useState<'open' | 'closed'>('open')
  return (
    <PullRequestListPane
      openCount={props.rows?.length ?? 0}
      rows={[]}
      state="ready"
      message="No open pull requests."
      onRetry={() => undefined}
      {...props}
      tab={tab}
      onTabChange={(value) => { setTab(value); props.onTabChange?.(value) }}
      selectedId={selectedId}
      onSelect={(id) => { setSelectedId(id); onSelectSpy?.(id) }}
    />
  )
}

const option = (name: RegExp) => screen.getByRole('option', { name })
const titles = () => screen.queryAllByRole('option').map((node) => node.textContent?.match(/#\d+/)?.[0])

describe('PullRequestListPane', () => {
  it('groups the stack above standalone PRs with rails, checks and drafts', () => {
    const { rows, a } = fixture()
    render(<Harness rows={rows} selectedId={a.id} />)
    const stack = screen.getByRole('group', { name: 'Stack · a' })
    expect(within(stack).getAllByRole('option').map((node) => node.textContent?.match(/#\d+/)?.[0])).toEqual(['#1', '#2', '#3'])
    expect(stack).toHaveTextContent('3 PRs → main')
    const standalone = screen.getByRole('group', { name: 'Standalone' })
    expect(standalone).toHaveTextContent('1 draft')
    expect(within(standalone).getByText('draft')).toBeInTheDocument()
    expect(within(standalone).getAllByText('no checks')).toHaveLength(2)
    expect(option(/Add auth API/)).toHaveAttribute('aria-selected', 'true')
    expect(option(/Add auth UI/)).toHaveAttribute('aria-selected', 'false')
    expect(option(/Add auth UI/)).toHaveTextContent('2/3')
    expect(option(/Polish auth copy/)).toHaveTextContent('1/2')
    expect(option(/Polish auth copy/)).toHaveTextContent('feat/c → feat/b')
    expect(screen.getByLabelText('Checks failing')).toBeInTheDocument()
    expect(screen.getByLabelText('Checks running')).toBeInTheDocument()
    expect(screen.getByLabelText('Checks passed')).toBeInTheDocument()
  })

  it('shows the open count and live filter counts', () => {
    const { rows } = fixture()
    render(<Harness rows={rows} />)
    expect(screen.getByRole('button', { name: /^Open 5/ })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: /^Closed/ })).toHaveTextContent('Closed')
    const chip = (name: string) => screen.getByRole('button', { name: new RegExp(`^${name}`) })
    expect(chip('All')).toHaveTextContent('5')
    expect(chip('Stacked')).toHaveTextContent('3')
    expect(chip('Draft')).toHaveTextContent('1')
    expect(chip('Failing')).toHaveTextContent('1')
  })

  it('filters with the chips and clears them from the empty state', () => {
    const { rows } = fixture()
    render(<Harness rows={rows} />)
    fireEvent.click(screen.getByRole('button', { name: /^Draft/ }))
    expect(titles()).toEqual(['#10'])
    expect(screen.getByRole('button', { name: /^Draft/ })).toHaveAttribute('aria-pressed', 'true')
    fireEvent.click(screen.getByRole('button', { name: /^Stacked/ }))
    expect(titles()).toEqual(['#1', '#2', '#3'])
    fireEvent.click(screen.getByRole('button', { name: /^Failing/ }))
    expect(titles()).toEqual(['#3'])
    fireEvent.change(screen.getByLabelText('Search pull requests'), { target: { value: 'zzz' } })
    expect(screen.getByText('No pull requests match.')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }))
    expect(titles()).toHaveLength(5)
    expect(screen.getByLabelText('Search pull requests')).toHaveValue('')
  })

  it('filters by search text across title, branch, author and number', () => {
    const { rows } = fixture()
    render(<Harness rows={rows} />)
    const search = screen.getByLabelText('Search pull requests')
    fireEvent.change(search, { target: { value: 'ada' } })
    expect(titles()).toEqual(['#11'])
    fireEvent.change(search, { target: { value: 'feat/b' } })
    expect(titles()).toEqual(['#2', '#3'])
    fireEvent.change(search, { target: { value: '#10' } })
    expect(titles()).toEqual(['#10'])
    expect(screen.getByRole('button', { name: /^All/ })).toHaveTextContent('1')
  })

  it('reports the visible rows and keeps one row tabbable when search hides the selection', () => {
    const { a, b, c, draft, lone, rows } = fixture()
    const onVisibleChange = vi.fn()
    render(<Harness rows={rows} selectedId={lone.id} onVisibleChange={onVisibleChange} />)
    expect(onVisibleChange).toHaveBeenLastCalledWith([a.id, b.id, c.id, draft.id, lone.id])
    expect(option(/Fix typo/)).toHaveAttribute('tabindex', '0')
    fireEvent.change(screen.getByLabelText('Search pull requests'), { target: { value: 'auth api' } })
    expect(onVisibleChange).toHaveBeenLastCalledWith([a.id])
    expect(screen.getAllByRole('option').map((node) => node.tabIndex)).toEqual([0])
  })

  it('focuses search with / unless typing elsewhere, and Escape clears then blurs', () => {
    const { rows } = fixture()
    render(<Harness rows={rows} />)
    const search = screen.getByLabelText('Search pull requests')
    fireEvent.keyDown(window, { key: '/', ctrlKey: true })
    expect(search).not.toHaveFocus()
    fireEvent.keyDown(window, { key: '/' })
    expect(search).toHaveFocus()
    fireEvent.change(search, { target: { value: 'ada' } })
    fireEvent.keyDown(search, { key: '/' })
    expect(search).toHaveValue('ada')
    fireEvent.keyDown(search, { key: 'Escape' })
    expect(search).toHaveValue('')
    expect(search).toHaveFocus()
    fireEvent.keyDown(search, { key: 'Escape' })
    expect(search).not.toHaveFocus()
  })

  it('moves the selection with the arrow keys in display order and focuses the row', () => {
    const { rows, a, b, c, draft, lone } = fixture()
    const spy = vi.fn()
    render(<Harness rows={rows} selectedId={a.id} onSelectSpy={spy} />)
    fireEvent.keyDown(option(/Add auth API/), { key: 'ArrowDown' })
    expect(spy).toHaveBeenLastCalledWith(b.id)
    expect(option(/Add auth UI/)).toHaveFocus()
    expect(option(/Add auth UI/)).toHaveAttribute('aria-selected', 'true')
    fireEvent.keyDown(option(/Add auth UI/), { key: 'ArrowDown' })
    expect(spy).toHaveBeenLastCalledWith(c.id)
    fireEvent.keyDown(option(/Polish auth copy/), { key: 'ArrowDown' })
    expect(spy).toHaveBeenLastCalledWith(draft.id)
    fireEvent.keyDown(option(/Parallax/), { key: 'End' })
    expect(spy).toHaveBeenLastCalledWith(lone.id)
    fireEvent.keyDown(option(/Fix typo/), { key: 'ArrowDown' })
    expect(spy).toHaveBeenLastCalledWith(lone.id)
    fireEvent.keyDown(option(/Fix typo/), { key: 'Home' })
    expect(spy).toHaveBeenLastCalledWith(a.id)
    fireEvent.keyDown(option(/Add auth API/), { key: 'ArrowUp' })
    expect(spy).toHaveBeenLastCalledWith(a.id)
  })

  it('ignores modified and unrelated keys, and does nothing on an empty list', () => {
    const { rows, a } = fixture()
    const spy = vi.fn()
    const view = render(<Harness rows={rows} selectedId={a.id} onSelectSpy={spy} />)
    fireEvent.keyDown(option(/Add auth API/), { key: 'ArrowDown', ctrlKey: true })
    fireEvent.keyDown(option(/Add auth API/), { key: 'a' })
    expect(spy).not.toHaveBeenCalled()
    view.unmount()
    render(<Harness rows={[]} onSelectSpy={spy} />)
    fireEvent.keyDown(screen.getByLabelText('Search pull requests'), { key: 'ArrowDown' })
    expect(spy).not.toHaveBeenCalled()
  })

  it('arrow keys in the search box select without stealing focus', () => {
    const { rows, a, b } = fixture()
    const spy = vi.fn()
    render(<Harness rows={rows} selectedId={a.id} onSelectSpy={spy} />)
    const search = screen.getByLabelText('Search pull requests')
    search.focus()
    fireEvent.keyDown(search, { key: 'ArrowDown' })
    expect(spy).toHaveBeenLastCalledWith(b.id)
    expect(search).toHaveFocus()
    fireEvent.keyDown(search, { key: 'Home' })
    expect(spy).toHaveBeenCalledTimes(1)
  })

  it('selects with click, Enter and Space; selection starts at the first row when none is set', () => {
    const { rows, a, lone } = fixture()
    const spy = vi.fn()
    render(<Harness rows={rows} onSelectSpy={spy} />)
    fireEvent.keyDown(screen.getByLabelText('Search pull requests'), { key: 'ArrowDown' })
    expect(spy).toHaveBeenLastCalledWith(a.id)
    fireEvent.click(option(/Fix typo/))
    expect(spy).toHaveBeenLastCalledWith(lone.id)
    fireEvent.keyDown(option(/Add auth API/), { key: 'Enter' })
    expect(spy).toHaveBeenLastCalledWith(a.id)
    fireEvent.keyDown(option(/Fix typo/), { key: ' ' })
    expect(spy).toHaveBeenLastCalledWith(lone.id)
    const count = spy.mock.calls.length
    fireEvent.keyDown(option(/Fix typo/), { key: 'x' })
    fireEvent.keyDown(within(option(/Fix typo/)).getByTitle('Fix typo in README'), { key: 'Enter' })
    expect(spy.mock.calls.length).toBe(count)
  })

  it('switches tabs; closed has no filter chips and shows its count once loaded', () => {
    const { rows } = fixture()
    const onTabChange = vi.fn()
    const view = render(<Harness rows={rows} closedCount={24} onTabChange={onTabChange} />)
    expect(screen.getByRole('button', { name: /^Closed/ })).toHaveTextContent('24')
    fireEvent.click(screen.getByRole('button', { name: /^Closed/ }))
    expect(onTabChange).toHaveBeenCalledWith('closed')
    expect(screen.queryByRole('group', { name: 'Filter pull requests' })).not.toBeInTheDocument()
    expect(titles()).toHaveLength(5)
    view.unmount()
  })

  it('shows a loading skeleton until the first rows arrive', () => {
    render(<Harness rows={[]} state="loading" message="Loading pull requests…" />)
    expect(screen.getByRole('status', { name: 'Loading pull requests' })).toBeInTheDocument()
    expect(screen.queryByText('No open pull requests')).not.toBeInTheDocument()
  })

  it('keeps showing rows while a refresh is loading', () => {
    const { rows } = fixture()
    render(<Harness rows={rows} state="loading" />)
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(titles()).toHaveLength(5)
  })

  it('shows the empty state for each tab', () => {
    const view = render(<Harness rows={[]} />)
    expect(screen.getByText('No open pull requests')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /^Closed/ }))
    expect(screen.getByText('No closed pull requests')).toBeInTheDocument()
    view.unmount()
  })

  it('shows the error with retry, instead of the empty state, and keeps loaded rows', () => {
    const onRetry = vi.fn()
    const view = render(<Harness rows={[]} state="error" message="GitHub is down" onRetry={onRetry} />)
    expect(screen.getByRole('alert')).toHaveTextContent('GitHub is down')
    expect(screen.queryByText('No open pull requests')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(onRetry).toHaveBeenCalledTimes(1)
    fireEvent.click(screen.getByRole('button', { name: 'Refresh pull requests' }))
    expect(onRetry).toHaveBeenCalledTimes(2)
    view.unmount()
    render(<Harness rows={fixture().rows} state="error" message="stale" />)
    expect(screen.getByRole('alert')).toHaveTextContent('stale')
    expect(titles()).toHaveLength(5)
  })

  it('renders a very long title, no checks and 40 checks without breaking the row', () => {
    const long = stackPr(30, 'feat/long', 'main', { title: 'x'.repeat(300), checks: checks(30, 5, 5) })
    render(<Harness rows={[long]} />)
    const row = option(/x{20}/)
    expect(within(row).getByTitle('x'.repeat(300))).toHaveClass('truncate')
    expect(row).toHaveTextContent('30/40')
  })
})
