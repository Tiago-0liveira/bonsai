import { render, screen } from '@testing-library/react'
import { expect, it } from 'vitest'
import { USAGE_RANKS, UsageMeter, usageRank } from './UsageMeter'

it('ranks remaining headroom into ten steps, the fuller the higher', () => {
  expect(USAGE_RANKS).toBe(10)
  expect([0, 0.09, 0.1, 0.19, 0.5, 0.89, 0.9, 1].map(usageRank)).toEqual([1, 1, 2, 2, 6, 9, 10, 10])
  const ranks = Array.from({ length: 101 }, (_, percent) => usageRank(percent / 100))
  expect(new Set(ranks).size).toBe(10)
  expect([...ranks].sort((a, b) => a - b)).toEqual(ranks)
})

it('puts the rank on the fill, and none when the headroom is unknown', () => {
  const { rerender } = render(<UsageMeter label="Weekly" fraction={0.72} />)
  expect(screen.getByRole('meter', { name: 'Weekly remaining' }).firstElementChild).toHaveAttribute('data-rank', '8')
  rerender(<UsageMeter label="Weekly" />)
  const meter = screen.getByRole('meter', { name: 'Weekly remaining' })
  expect(meter.firstElementChild).not.toHaveAttribute('data-rank')
  expect(meter).toHaveAttribute('aria-valuetext', 'unknown')
})
