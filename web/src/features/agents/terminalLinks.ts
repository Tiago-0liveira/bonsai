/** A link in terminal rows: 1-based cells, end inclusive, as xterm's link provider wants them. */
export interface TerminalLinkRange { url: string; start: { x: number; y: number }; end: { x: number; y: number } }

const URL_RE = /https?:\/\/[^\s"'<>`]+/g
// Sentence punctuation after a URL is not part of it; a closing bracket is only when unbalanced.
function trimUrl(url: string): string {
  let out = url.replace(/[.,;:!?]+$/, '')
  while (/[)\]}]$/.test(out)) {
    const close = out.at(-1)!, open = ({ ')': '(', ']': '[', '}': '{' } as const)[close as ')' | ']' | '}']
    if (out.split(open).length >= out.split(close).length) break
    out = out.slice(0, -1).replace(/[.,;:!?]+$/, '')
  }
  return out
}

/**
 * URLs in one logical terminal line that wraps over `rows` (each row `cols`
 * cells wide, the first at buffer row `firstRow`, 1-based). Login commands
 * print long sign-in URLs, so a link can span several rows.
 */
export function terminalUrls(rows: string[], cols: number, firstRow: number): TerminalLinkRange[] {
  const text = rows.map((row, index) => index < rows.length - 1 ? row.padEnd(cols).slice(0, cols) : row).join('')
  const cell = (offset: number) => ({ x: (offset % cols) + 1, y: firstRow + Math.floor(offset / cols) })
  const links: TerminalLinkRange[] = []
  for (const match of text.matchAll(URL_RE)) {
    const url = trimUrl(match[0])
    if (url.length <= 'https://'.length) continue
    links.push({ url, start: cell(match.index), end: cell(match.index + url.length - 1) })
  }
  return links
}
