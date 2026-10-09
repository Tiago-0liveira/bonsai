import { render, screen } from '@testing-library/react'
import { Markdown } from './Markdown'

describe('Markdown', () => {
  it('renders the common elements', () => {
    const { container } = render(
      <Markdown>{'**Lead.** text with `code`\n\n- a\n- b\n\n1. one\n\n```\nblock\n```\n\n> quote\n\n| h |\n|---|\n| c |'}</Markdown>,
    )
    expect(screen.getByText('Lead.').tagName).toBe('STRONG')
    expect(screen.getByText('code').tagName).toBe('CODE')
    expect(container.querySelectorAll('ul > li')).toHaveLength(2)
    expect(container.querySelector('ol > li')).toHaveTextContent('one')
    expect(container.querySelector('pre code')).toHaveTextContent('block')
    expect(container.querySelector('blockquote')).toHaveTextContent('quote')
    expect(screen.getByRole('columnheader', { name: 'h' })).toBeInTheDocument()
  })

  it('renders read-only task checkboxes', () => {
    render(<Markdown>{'- [x] done\n- [ ] todo'}</Markdown>)
    const boxes = screen.getAllByRole('checkbox')
    expect(boxes.map((box) => box.getAttribute('aria-checked'))).toEqual(['true', 'false'])
    expect(boxes[0]).toHaveAttribute('aria-readonly', 'true')
  })

  it('opens https links in a new tab without opener access', () => {
    render(<Markdown>{'[docs](https://example.com/a) and https://example.com/auto'}</Markdown>)
    for (const link of screen.getAllByRole('link')) {
      expect(link).toHaveAttribute('target', '_blank')
      expect(link).toHaveAttribute('rel', 'noopener noreferrer')
    }
    expect(screen.getByRole('link', { name: 'docs' })).toHaveAttribute('href', 'https://example.com/a')
    expect(screen.getAllByRole('link')).toHaveLength(2)
  })

  it.each([
    ['javascript:alert(1)'],
    ['http://example.com'],
    ['data:text/html,x'],
    ['/relative'],
    ['#anchor'],
  ])('does not link %s', (url) => {
    render(<Markdown>{`[click](${url})`}</Markdown>)
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
    expect(screen.getByText('click')).toBeInTheDocument()
  })

  it('never renders raw html', () => {
    const { container } = render(<Markdown>{'<script>window.x=1</script><img src=x onerror=alert(1)><b>bold</b>'}</Markdown>)
    expect(container.querySelector('script')).toBeNull()
    expect(container.querySelector('img')).toBeNull()
    expect(container.querySelector('b')).toBeNull()
  })

  it('degrades images to https links', () => {
    const { container } = render(<Markdown>{'![shot](https://example.com/a.png) ![bad](http://example.com/b.png)'}</Markdown>)
    expect(container.querySelector('img')).toBeNull()
    expect(screen.getByRole('link', { name: 'shot' })).toHaveAttribute('href', 'https://example.com/a.png')
    expect(screen.queryByRole('link', { name: 'bad' })).not.toBeInTheDocument()
  })
})
