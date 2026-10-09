import type { ListItem, Nodes, Root, RootContent } from 'mdast'
import remarkGfm from 'remark-gfm'
import remarkParse from 'remark-parse'
import { unified } from 'unified'

export interface TaskItem {
  text: string
  done: boolean
}

export interface TaskList {
  done: number
  total: number
  items: TaskItem[]
}

export interface PrSection {
  /** Heading text; absent for text that precedes the first `##` or has none. */
  heading?: string
  /** Original markdown below the heading, trimmed. */
  markdown: string
  /** Present when the section contains GitHub task list items. */
  tasks?: TaskList
}

export interface ParsedPrBody {
  summary?: PrSection
  testPlan?: PrSection
  /** Everything else, in document order. Holds the whole body when neither known section exists. */
  rest: PrSection[]
}

const SECTION_DEPTH = 2
const SUMMARY = new Set(['summary'])
const TEST_PLAN = new Set(['test plan', 'testing'])

const processor = unified().use(remarkParse).use(remarkGfm)

/**
 * Splits a PR body on `##` headings. The tree decides what is a heading (so
 * `##` inside code fences is left alone) and sections are sliced from the
 * original text by node offsets. Only the first Summary and the first Test
 * plan / Testing heading are promoted; duplicates stay in `rest`.
 */
export function parsePrBody(body: string | null | undefined): ParsedPrBody {
  const source = typeof body === 'string' ? body : ''
  if (!source.trim()) return { rest: [] }

  const tree = processor.parse(source)
  const sections = splitSections(source, tree)

  const result: ParsedPrBody = { rest: [] }
  for (const section of sections) {
    const key = section.heading === undefined ? '' : normalizeHeading(section.heading)
    if (!result.summary && SUMMARY.has(key)) result.summary = section
    else if (!result.testPlan && TEST_PLAN.has(key)) result.testPlan = section
    else result.rest.push(section)
  }

  if (!result.summary && !result.testPlan) {
    return { rest: [{ markdown: source.trim(), ...taskFields(taskItems(tree)) }] }
  }
  return result
}

interface Draft {
  heading?: string
  start: number
  nodes: RootContent[]
}

function splitSections(source: string, tree: Root): PrSection[] {
  const drafts: Draft[] = []
  let current: Draft | undefined
  for (const node of tree.children) {
    const offsets = offsetsOf(node)
    if (node.type === 'heading' && node.depth === SECTION_DEPTH) {
      current = { heading: textOf(node).trim(), start: offsets.end, nodes: [] }
      drafts.push(current)
      continue
    }
    if (!current) {
      current = { start: offsets.start, nodes: [] }
      drafts.push(current)
    }
    current.nodes.push(node)
  }

  return drafts.flatMap((draft) => {
    const last = draft.nodes[draft.nodes.length - 1]
    const lastEnd = last ? offsetsOf(last).end : undefined
    // Sections run until the last node they own, so trailing blank lines and
    // the next heading never leak in; an empty section keeps an empty body.
    const markdown = lastEnd === undefined ? '' : source.slice(draft.start, lastEnd).trim()
    if (draft.heading === undefined && !markdown) return []
    return [{
      ...(draft.heading !== undefined ? { heading: draft.heading } : {}),
      markdown,
      ...taskFields(draft.nodes.flatMap((node) => taskItems(node))),
    }]
  })
}

function taskFields(items: TaskItem[]): { tasks?: TaskList } {
  if (!items.length) return {}
  return { tasks: { done: items.filter((item) => item.done).length, total: items.length, items } }
}

function taskItems(node: Nodes): TaskItem[] {
  const items: TaskItem[] = []
  const visit = (current: Nodes) => {
    if (current.type === 'listItem' && typeof current.checked === 'boolean') {
      items.push({ text: itemText(current), done: current.checked })
    }
    if ('children' in current) for (const child of current.children) visit(child)
  }
  visit(node)
  return items
}

/** Text of the item itself, not of any nested lists. */
function itemText(item: ListItem): string {
  return item.children
    .filter((child) => child.type !== 'list')
    .map(textOf)
    .join(' ')
    .replace(/\s+/g, ' ')
    .trim()
}

function textOf(node: Nodes): string {
  if ('value' in node && typeof node.value === 'string') return node.value
  if ('children' in node) return (node.children as Nodes[]).map(textOf).join('')
  return ''
}

/** remark-parse records a position, with offsets, on every node it creates. */
function offsetsOf(node: Nodes): { start: number; end: number } {
  const { start, end } = node.position as NonNullable<Nodes['position']>
  return { start: start.offset as number, end: end.offset as number }
}

function normalizeHeading(heading: string): string {
  return heading.toLowerCase().replace(/[:\s_-]+/g, ' ').trim()
}
