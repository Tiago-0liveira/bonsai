import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { Segmented } from './Segmented'

afterEach(cleanup)
const options = [{ value: 'a', label: 'Alpha' }, { value: 'b', label: 'Beta' }, { value: 'c', label: 'Gamma', disabled: true }]

it('marks the chosen option pressed and slides the highlight to it', () => {
  const onChange = vi.fn()
  const { container, rerender } = render(<Segmented ariaLabel="Letters" options={options} value="a" onChange={onChange} />)
  const pill = container.querySelector('.seg-pill') as HTMLElement
  expect(screen.getByRole('button', { name: 'Alpha' })).toHaveAttribute('aria-pressed', 'true')
  expect(pill.style.transform).toBe('translateX(0%)')
  fireEvent.click(screen.getByRole('button', { name: 'Beta' }))
  expect(onChange).toHaveBeenCalledWith('b')
  rerender(<Segmented ariaLabel="Letters" options={options} value="b" onChange={onChange} />)
  expect(pill.style.transform).toBe('translateX(100%)')
})

it('hides the highlight without a chosen option and ignores disabled ones', () => {
  const onChange = vi.fn()
  const { container } = render(<Segmented ariaLabel="Letters" options={options} value="" onChange={onChange} />)
  expect((container.querySelector('.seg-pill') as HTMLElement).style.opacity).toBe('0')
  fireEvent.click(screen.getByRole('button', { name: 'Gamma' }))
  expect(onChange).not.toHaveBeenCalled()
})
