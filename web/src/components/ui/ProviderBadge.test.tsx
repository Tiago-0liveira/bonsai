import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { ProviderBadge } from './ProviderBadge'
import { CLAUDE_CORAL, ProviderLogo } from './ProviderLogo'

afterEach(cleanup)

it('draws the Claude mark instead of a monogram', () => {
  const { container } = render(<ProviderBadge provider="Claude" size={20} />)
  expect(container.textContent).toBe('')
  const svg = container.querySelector('svg')!
  expect(svg).toHaveAttribute('aria-hidden', 'true')
  expect(svg).toHaveAttribute('fill', CLAUDE_CORAL)
  expect(svg.querySelector('path')!.getAttribute('d')!.length).toBeGreaterThan(500)
})

it.each(['Claude', 'Antigravity', 'Codex', 'Gemini'])('draws the %s mark without text nodes', (provider) => {
  const { container } = render(<ProviderBadge provider={provider} />)
  expect(container.textContent).toBe('')
  expect(container.querySelector('svg')).toHaveAttribute('data-provider-logo', provider.toLowerCase())
  expect(container.querySelector('svg')).toHaveAttribute('aria-hidden', 'true')
  expect(container.querySelector('svg')).toHaveAttribute('width', '13')
  const walker = document.createTreeWalker(container, NodeFilter.SHOW_TEXT)
  expect(walker.nextNode()).toBeNull()
  expect(container.querySelector('filter, mask')).toBeNull()
})

it('uses the Bot fallback for an unknown provider', () => {
  const { container } = render(<ProviderBadge provider="Other" />)
  expect(container.textContent).toBe('')
  expect(container.querySelector('svg')).toHaveClass('lucide-bot')
})

it('gives each Antigravity logo a unique gradient and its own fill reference', () => {
  const { container } = render(<><ProviderLogo provider="Antigravity" /><ProviderLogo provider="antigravity" /></>)
  const svgs = [...container.querySelectorAll('svg')]
  const ids = svgs.map(svg => svg.querySelector('linearGradient')!.id)
  expect(new Set(ids).size).toBe(2)
  svgs.forEach((svg, index) => {
    expect(svg).toHaveAttribute('fill', `url(#${ids[index]})`)
    expect([...svg.querySelectorAll('stop')].map(stop => stop.getAttribute('stop-color')))
      .toEqual(['#3186FF', '#00B95C', '#FBBC04', '#FC413D'])
  })
})

it.each(['Claude', 'Codex', 'Antigravity', 'Gemini', 'Other'])('exposes a standalone %s logo with an accessible title', (provider) => {
  render(<ProviderLogo provider={provider} title={`${provider} provider`} size={24} className="text-text" />)
  const svg = screen.getByRole('img', { name: `${provider} provider` })
  expect(svg).not.toHaveAttribute('aria-hidden')
  expect(svg).toHaveAttribute('width', '24')
  expect(svg).toHaveClass('text-text')
})

it('uses currentColor for Codex so the mark follows the surrounding theme', () => {
  const { container } = render(<ProviderLogo provider="codex" />)
  expect(container.querySelector('svg')).toHaveAttribute('fill', 'currentColor')
})
