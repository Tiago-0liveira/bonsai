import { expect, it } from 'vitest'
import { terminalUrls } from './terminalLinks'

it('finds a URL on one row with 1-based inclusive cells', () => {
  expect(terminalUrls(['Open https://auth.example.com/device and enter the code.'], 80, 3)).toEqual([
    { url: 'https://auth.example.com/device', start: { x: 6, y: 3 }, end: { x: 36, y: 3 } },
  ])
})

it('follows a URL that wraps over several rows and drops trailing punctuation', () => {
  const url = 'https://claude.example.com/oauth/authorize?code=true&client_id=abc&state=xyz'
  const text = `Visit: ${url}.`
  const cols = 30
  const rows = [text.slice(0, 30), text.slice(30, 60), text.slice(60)]
  const [link] = terminalUrls(rows, cols, 10)
  expect(link.url).toBe(url)
  expect(link.start).toEqual({ x: 8, y: 10 })
  // "Visit: " is 7 cells and the URL 76: its last character is at offset 82, row 12, column 23.
  expect(link.end).toEqual({ x: 23, y: 12 })
})

it('keeps balanced brackets and trims an unbalanced closing one', () => {
  expect(terminalUrls(['(see https://example.com/a_(b))'], 80, 1).map(link => link.url)).toEqual(['https://example.com/a_(b)'])
  expect(terminalUrls(['x https://a.example/one y http://b.example/two'], 80, 1).map(link => link.url)).toEqual(['https://a.example/one', 'http://b.example/two'])
  expect(terminalUrls(['no links, just https:// here'], 80, 1)).toEqual([])
})
