import { cleanup, render } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { ProviderBadge } from './ProviderBadge'
import { CLAUDE_CORAL } from './ClaudeLogo'

afterEach(cleanup)

it('draws the Claude mark instead of a monogram', () => {
  const { container } = render(<ProviderBadge provider="Claude" size={20} />)
  expect(container.textContent).toBe('')
  const svg = container.querySelector('svg')!
  expect(svg).toHaveAttribute('aria-hidden', 'true')
  expect(svg).toHaveAttribute('fill', CLAUDE_CORAL)
  expect(svg.querySelector('path')!.getAttribute('d')!.length).toBeGreaterThan(500)
})

it.each([['Antigravity', 'AG'], ['Codex', 'CX'], ['Gemini', 'GM']] as const)('keeps the %s monogram', (provider, letters) => {
  const { container } = render(<ProviderBadge provider={provider} />)
  expect(container.textContent).toBe(letters)
  expect(container.querySelector('svg')).toBeNull()
})
