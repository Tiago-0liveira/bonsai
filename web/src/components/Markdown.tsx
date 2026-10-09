import type { ComponentProps, ReactNode } from 'react'
import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'

/** Untrusted input: only absolute https URLs survive, everything else renders as plain text. */
const safeUrl = (url: string) => (/^https:\/\//i.test(url) ? url : '')

const remarkPlugins = [remarkGfm]

function Link({ href, children }: { href?: string; children?: ReactNode }) {
  if (!href) return <span>{children}</span>
  return (
    <a href={href} target="_blank" rel="noopener noreferrer" className="text-accent underline decoration-accent/40 underline-offset-2 hover:decoration-accent">
      {children}
    </a>
  )
}

const components: Components = {
  p: ({ children }) => <p className="my-0 [&:not(:last-child)]:mb-2">{children}</p>,
  strong: ({ children }) => <strong className="font-semibold text-text">{children}</strong>,
  a: ({ href, children }) => <Link href={href}>{children}</Link>,
  // CSP blocks remote images, so an image degrades to a link to its source.
  img: ({ src, alt }) => (typeof src === 'string' && src ? <Link href={src}>{alt || src}</Link> : <span>{alt}</span>),
  ul: ({ className, children }) => (
    <ul className={`my-0 flex list-none flex-col gap-[5px] p-0 [&:not(:last-child)]:mb-2 ${className?.includes('contains-task-list') ? '' : '[&>li]:pl-[15px]'}`}>{children}</ul>
  ),
  ol: ({ children }) => <ol className="my-0 flex list-decimal flex-col gap-[5px] pl-5 marker:font-mono marker:text-muted-2 [&:not(:last-child)]:mb-2">{children}</ol>,
  li: ({ className, children }) =>
    className?.includes('task-list-item') ? (
      // The box hangs in the gutter so the item text wraps as one run, not as flex columns.
      <li className="relative pl-[23px]">{children}</li>
    ) : (
      <li className="relative before:absolute before:left-0 before:top-[6px] before:h-[6px] before:w-[6px] before:rounded-[2px] before:bg-accent before:content-[''] [ol>&]:pl-0 [ol>&]:before:hidden">
        {children}
      </li>
    ),
  input: ({ type, checked }: ComponentProps<'input'>) =>
    type === 'checkbox' ? (
      <span
        role="checkbox"
        aria-checked={!!checked}
        aria-readonly="true"
        className={`absolute left-0 top-[2px] inline-flex h-[14px] w-[14px] items-center justify-center rounded-[4px] border ${checked ? 'border-ok bg-ok/15 text-ok' : 'border-border-strong text-transparent'}`}
      >
        <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <path d="M5 12.5l4.5 4.5L19 7" />
        </svg>
      </span>
    ) : null,
  code: ({ className, children }) => (
    <code className={`rounded-[4px] bg-well px-1 font-mono text-[11px] text-ok ${className ?? ''}`}>{children}</code>
  ),
  pre: ({ children }) => (
    <pre className="my-0 overflow-x-auto rounded-lg border border-border-subtle bg-well p-3 font-mono text-[11px] leading-[17px] text-text [&:not(:last-child)]:mb-2 [&_code]:bg-transparent [&_code]:p-0 [&_code]:text-text">
      {children}
    </pre>
  ),
  blockquote: ({ children }) => (
    <blockquote className="my-0 border-l-2 border-border-strong pl-3 text-muted-2 [&:not(:last-child)]:mb-2">{children}</blockquote>
  ),
  table: ({ children }) => (
    <div className="overflow-x-auto [&:not(:last-child)]:mb-2">
      <table className="w-full border-collapse text-left text-[12px]">{children}</table>
    </div>
  ),
  th: ({ children }) => <th className="border-b border-border px-2 py-1 font-mono text-[10px] font-semibold uppercase tracking-[0.08em] text-muted">{children}</th>,
  td: ({ children }) => <td className="border-b border-border-subtle px-2 py-1 align-top">{children}</td>,
  h1: ({ children }) => <h4 className="my-0 text-[13px] font-semibold text-text [&:not(:last-child)]:mb-1.5">{children}</h4>,
  h2: ({ children }) => <h4 className="my-0 text-[13px] font-semibold text-text [&:not(:last-child)]:mb-1.5">{children}</h4>,
  h3: ({ children }) => <h5 className="my-0 text-[12px] font-semibold text-text [&:not(:last-child)]:mb-1.5">{children}</h5>,
  h4: ({ children }) => <h5 className="my-0 text-[12px] font-semibold text-text [&:not(:last-child)]:mb-1.5">{children}</h5>,
  h5: ({ children }) => <h5 className="my-0 text-[12px] font-semibold text-text [&:not(:last-child)]:mb-1.5">{children}</h5>,
  h6: ({ children }) => <h5 className="my-0 text-[12px] font-semibold text-text [&:not(:last-child)]:mb-1.5">{children}</h5>,
  hr: () => <hr className="my-2 border-0 border-t border-border-subtle" />,
}

/**
 * Themed renderer for pull request text. Raw HTML is never rendered (no
 * rehype-raw) and URLs are limited to https.
 */
export function Markdown({ children, className = '' }: { children: string; className?: string }) {
  return (
    <div className={`min-w-0 break-words text-[12px] leading-[18px] text-muted ${className}`}>
      <ReactMarkdown remarkPlugins={remarkPlugins} urlTransform={safeUrl} components={components}>
        {children}
      </ReactMarkdown>
    </div>
  )
}
