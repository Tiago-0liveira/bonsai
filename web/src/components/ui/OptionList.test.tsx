import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { useState } from 'react'
import { OptionList, type ListOption } from './OptionList'

const OPTIONS: ListOption[] = [
  { value: 'main', label: 'main', description: 'default branch' },
  { value: 'feat/web', label: 'feat/web' },
  { value: 'feat/api', label: 'feat/api' },
  { value: 'fix/daemon', label: 'fix/daemon', disabled: true },
]

function Harness({ options = OPTIONS, onSubmit = () => {}, onPick = () => {} }: { options?: ListOption[]; onSubmit?: () => void; onPick?: (value: string) => void }) {
  const [value, setValue] = useState('main')
  return (
    <form onSubmit={event => { event.preventDefault(); onSubmit() }}>
      <OptionList searchable ariaLabel="Branch" filterLabel="Filter branches" options={options} value={value} onChange={next => { setValue(next); onPick(next) }} />
    </form>
  )
}
afterEach(cleanup)
const names = () => within(screen.getByRole('listbox', { name: 'Branch' })).getAllByRole('option').map(option => option.textContent)

describe('OptionList', () => {
  it('lists every option, marks the chosen one and filters by label and description', () => {
    render(<Harness />)
    expect(names()).toHaveLength(4)
    expect(screen.getByRole('option', { name: /main/ })).toHaveAttribute('aria-selected', 'true')
    fireEvent.change(screen.getByLabelText('Filter branches'), { target: { value: 'feat' } })
    expect(names()).toEqual(['feat/web', 'feat/api'])
    fireEvent.change(screen.getByLabelText('Filter branches'), { target: { value: 'default' } })
    expect(names()).toEqual(['maindefault branch'])
    fireEvent.change(screen.getByLabelText('Filter branches'), { target: { value: 'zzz' } })
    expect(screen.getByText('Nothing matches.')).toBeInTheDocument()
  })

  it('picks with the arrow keys and Enter from the filter box without submitting the form', () => {
    const onSubmit = vi.fn()
    const onPick = vi.fn()
    render(<Harness onSubmit={onSubmit} onPick={onPick} />)
    const filter = screen.getByLabelText('Filter branches')
    fireEvent.keyDown(filter, { key: 'ArrowDown' })
    fireEvent.keyDown(filter, { key: 'Enter' })
    expect(onPick).toHaveBeenLastCalledWith('feat/web')
    expect(onSubmit).not.toHaveBeenCalled()
    // Up twice wraps round to the disabled last option, which Enter leaves alone.
    fireEvent.keyDown(filter, { key: 'ArrowUp' })
    fireEvent.keyDown(filter, { key: 'ArrowUp' })
    fireEvent.keyDown(filter, { key: 'Enter' })
    expect(onPick).toHaveBeenCalledTimes(1)
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it('clears the filter on Escape and keeps disabled options unpickable', () => {
    const onPick = vi.fn()
    render(<Harness onPick={onPick} />)
    const filter = screen.getByLabelText('Filter branches')
    fireEvent.change(filter, { target: { value: 'fix' } })
    fireEvent.click(screen.getByRole('option', { name: /fix\/daemon/ }))
    expect(onPick).not.toHaveBeenCalled()
    fireEvent.keyDown(filter, { key: 'Escape' })
    expect(filter).toHaveValue('')
    expect(names()).toHaveLength(4)
  })

  it('renders only a bounded slice of a very long list and says so', () => {
    const many = Array.from({ length: 400 }, (_, index) => ({ value: `b${index}`, label: `branch-${index}` }))
    render(<Harness options={many} />)
    expect(names()).toHaveLength(150)
    expect(screen.getByText('+250 more. Keep typing to narrow.')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Filter branches'), { target: { value: 'branch-39' } })
    expect(names()).toHaveLength(11)
  })
})
