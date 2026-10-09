import {
  bodyWithDuplicateHeading,
  bodyWithoutHeadings,
  pr47Body,
  pr47BodyCrlf,
  pr47BodyFlattened,
} from '../../test/fixtures/prBodies'
import { parsePrBody } from './prBody'

describe('parsePrBody', () => {
  it('splits the real #47 body into summary, test plan and the rest', () => {
    const parsed = parsePrBody(pr47Body)

    expect(parsed.summary?.heading).toBe('Summary')
    expect(parsed.summary?.markdown.startsWith('- **Phase 5 of `docs/theme-plan.md`:**')).toBe(true)
    expect(parsed.summary?.markdown).toContain('Remove worktree tags everywhere')
    expect(parsed.summary?.markdown).not.toContain('Behavior changes')

    expect(parsed.testPlan?.heading).toBe('Test plan')
    expect(parsed.testPlan?.tasks).toMatchObject({ done: 3, total: 4 })
    expect(parsed.testPlan?.tasks?.items.map((item) => item.done)).toEqual([true, true, true, false])
    expect(parsed.testPlan?.tasks?.items[0].text).toBe('pnpm typecheck && pnpm lint && pnpm test (293 tests)')
    expect(parsed.testPlan?.markdown).toContain('Generated with')

    expect(parsed.rest.map((section) => section.heading)).toEqual(['Behavior changes to review', 'Stacking'])
    expect(parsed.rest[1].markdown).toBe('Based on `feat/theme-canvas` (#46), not `main`.')
  })

  it('parses CRLF bodies the same way', () => {
    const lf = parsePrBody(pr47Body)
    const crlf = parsePrBody(pr47BodyCrlf)
    expect(crlf.testPlan?.tasks?.items.map((item) => item.done)).toEqual(lf.testPlan?.tasks?.items.map((item) => item.done))
    expect(crlf.rest.map((section) => section.heading)).toEqual(['Behavior changes to review', 'Stacking'])
    expect(crlf.summary?.markdown).not.toContain('\r\n## ')
  })

  it('falls back to the whole body when line breaks were flattened away', () => {
    const parsed = parsePrBody(pr47BodyFlattened)
    expect(parsed.summary).toBeUndefined()
    expect(parsed.testPlan).toBeUndefined()
    expect(parsed.rest).toHaveLength(1)
    expect(parsed.rest[0].markdown).toBe(pr47BodyFlattened.trim())
  })

  it.each([undefined, null, '', '   \n\n  '])('returns nothing for an empty body (%j)', (body) => {
    expect(parsePrBody(body)).toEqual({ rest: [] })
  })

  it('keeps a body with no headings whole in rest', () => {
    const parsed = parsePrBody(bodyWithoutHeadings)
    expect(parsed.summary).toBeUndefined()
    expect(parsed.testPlan).toBeUndefined()
    expect(parsed.rest).toEqual([{ markdown: bodyWithoutHeadings }])
  })

  it('keeps the whole body when only unknown sections exist', () => {
    const body = '## Context\nWhy.\n\n## Rollout\n- [x] done'
    const parsed = parsePrBody(body)
    expect(parsed.rest).toHaveLength(1)
    expect(parsed.rest[0].markdown).toBe(body)
    expect(parsed.rest[0].tasks).toMatchObject({ done: 1, total: 1 })
  })

  it('promotes the first duplicate heading and leaves the rest', () => {
    const parsed = parsePrBody(bodyWithDuplicateHeading)
    expect(parsed.summary?.markdown).toBe('First summary.')
    expect(parsed.testPlan?.heading).toBe('Testing')
    expect(parsed.testPlan?.tasks).toMatchObject({ done: 1, total: 1 })
    expect(parsed.rest.map((section) => [section.heading, section.markdown])).toEqual([
      ['Summary', 'Second summary.'],
      ['Test plan', '- [ ] second'],
    ])
  })

  it('matches headings case-insensitively and ignores a trailing colon', () => {
    const parsed = parsePrBody('## SUMMARY:\nHello\n\n## test PLAN\n- [ ] a')
    expect(parsed.summary?.markdown).toBe('Hello')
    expect(parsed.testPlan?.tasks).toMatchObject({ done: 0, total: 1 })
  })

  it('keeps text before the first heading in rest', () => {
    const parsed = parsePrBody('Fixes #12\n\n## Summary\nDone.')
    expect(parsed.summary?.markdown).toBe('Done.')
    expect(parsed.rest).toEqual([{ markdown: 'Fixes #12' }])
  })

  it('drops a preamble that is only invisible whitespace', () => {
    expect(parsePrBody(' \n\n## Summary\nDone.')).toEqual({ summary: { heading: 'Summary', markdown: 'Done.' }, rest: [] })
  })

  it('does not split on ## inside code fences, deeper headings or blockquotes', () => {
    const body = '## Summary\nText\n\n```md\n## Test plan\n- [x] fake\n```\n\n### Details\nmore\n\n> ## quoted'
    const parsed = parsePrBody(body)
    expect(parsed.testPlan).toBeUndefined()
    expect(parsed.summary?.markdown).toBe(body.slice('## Summary\n'.length))
    expect(parsed.summary?.tasks).toBeUndefined()
  })

  it('counts nested task items and ignores plain list items', () => {
    const parsed = parsePrBody('## Test plan\n- plain\n- [x] one\n  - [ ] nested **bold**\n  - not a task\n- [ ] `three`')
    expect(parsed.testPlan?.tasks).toEqual({
      done: 1,
      total: 3,
      items: [
        { text: 'one', done: true },
        { text: 'nested bold', done: false },
        { text: 'three', done: false },
      ],
    })
  })

  it('reads task text around images and line breaks', () => {
    const parsed = parsePrBody('## Test plan\n- [x] see ![shot](https://example.com/a.png)\\\nnext')
    expect(parsed.testPlan?.tasks?.items).toEqual([{ text: 'see next', done: true }])
  })

  it('keeps an empty section with an empty body', () => {
    const parsed = parsePrBody('## Summary\n\n## Test plan\n- [x] a')
    expect(parsed.summary).toEqual({ heading: 'Summary', markdown: '' })
  })

  it('never renders html into section text: it is passed through as markdown source', () => {
    const parsed = parsePrBody('## Summary\n<script>alert(1)</script>')
    expect(parsed.summary?.markdown).toBe('<script>alert(1)</script>')
  })
})
