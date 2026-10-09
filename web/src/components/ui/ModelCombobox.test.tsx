import { useState } from 'react'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { ModelCombobox, type ModelSuggestion } from './ModelCombobox'

afterEach(cleanup)

const suggestions: ModelSuggestion[] = [
  { id: 'opus', label: 'Opus', source: 'alias' },
  { id: 'sonnet', label: 'Sonnet', source: 'alias' },
  { id: 'claude-sonnet-5-5', label: 'Claude Sonnet 5.5', description: 'claude-sonnet-5-5', source: 'api' },
]

function Harness({ onValue = () => {}, list = suggestions }: { onValue?: (value: string) => void; list?: ModelSuggestion[] }) {
  const [value, setValue] = useState('')
  return <ModelCombobox ariaLabel="Model" value={value} onChange={next => { setValue(next); onValue(next) }} suggestions={list} />
}

it('filters while typing and accepts a suggestion with the mouse', () => {
  const seen = vi.fn()
  render(<Harness onValue={seen} />)
  const input = screen.getByRole('combobox', { name: 'Model' })
  fireEvent.focus(input)
  expect(screen.getAllByRole('option')).toHaveLength(3)
  fireEvent.change(input, { target: { value: 'son' } })
  expect(screen.getAllByRole('option').map(option => option.textContent)).toEqual([expect.stringContaining('Sonnet'), expect.stringContaining('Claude Sonnet 5.5')])
  fireEvent.click(screen.getByRole('option', { name: /^Sonnet\s*latest/ }))
  expect(input).toHaveValue('sonnet')
  expect(seen).toHaveBeenLastCalledWith('sonnet')
  expect(screen.queryByRole('listbox')).toBeNull()
})

it('supports the keyboard and Escape', () => {
  render(<Harness />)
  const input = screen.getByRole('combobox', { name: 'Model' })
  fireEvent.keyDown(input, { key: 'ArrowDown' })
  fireEvent.keyDown(input, { key: 'ArrowDown' })
  fireEvent.keyDown(input, { key: 'ArrowDown' })
  fireEvent.keyDown(input, { key: 'Enter' })
  expect(input).toHaveValue('claude-sonnet-5-5')
  fireEvent.keyDown(input, { key: 'ArrowUp' })
  expect(screen.getByRole('listbox')).toBeInTheDocument()
  expect(screen.getAllByRole('option')).toHaveLength(3) // an exact value shows every model again
  fireEvent.keyDown(input, { key: 'Escape' })
  expect(screen.queryByRole('listbox')).toBeNull()
})

it('keeps free text when nothing matches and with no suggestions', () => {
  render(<Harness />)
  const input = screen.getByRole('combobox', { name: 'Model' })
  fireEvent.change(input, { target: { value: 'my-private-model' } })
  expect(screen.queryByRole('listbox')).toBeNull()
  expect(input).toHaveValue('my-private-model')
  cleanup()
  render(<Harness list={[]} />)
  fireEvent.focus(screen.getByRole('combobox', { name: 'Model' }))
  expect(screen.queryByRole('listbox')).toBeNull()
})

it('closes on an outside click', () => {
  render(<Harness />)
  fireEvent.focus(screen.getByRole('combobox', { name: 'Model' }))
  expect(screen.getByRole('listbox')).toBeInTheDocument()
  fireEvent.pointerDown(document.body)
  expect(screen.queryByRole('listbox')).toBeNull()
})
