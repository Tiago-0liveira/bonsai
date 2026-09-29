import { useState, type ReactNode } from 'react'
import './parallax.css'

type InstallPlatform = 'unix' | 'windows'

const INSTALL_COMMANDS: Record<InstallPlatform, string> = {
  unix: 'curl -fsSL https://raw.githubusercontent.com/Tiago-0liveira/bonsai/main/install.sh | bash',
  windows: 'irm https://raw.githubusercontent.com/Tiago-0liveira/bonsai/main/install.ps1 | iex',
}

function BrandMark() {
  return (
    <svg className="brand-mark" viewBox="0 0 28 28" aria-hidden="true">
      <path d="M14 23V10M14 15l-5-4M14 18l6-5M14 10l3-4" />
      <circle cx="9" cy="10.5" r="2.35" />
      <circle cx="20" cy="12.5" r="2.35" />
      <circle cx="17.5" cy="6" r="2.15" />
    </svg>
  )
}

function StatusDot() {
  return <span className="status-dot" aria-hidden="true" />
}

function StarIcon() {
  return (
    <svg viewBox="0 0 18 18" aria-hidden="true">
      <path d="m9 2 2 4 4.5.7-3.2 3.1.8 4.5L9 12.2l-4.1 2.1.8-4.5-3.2-3.1L7 6z" />
    </svg>
  )
}

function UserIcon() {
  return (
    <svg viewBox="0 0 18 18" aria-hidden="true">
      <circle cx="9" cy="6" r="2.5" />
      <path d="M4.5 14c.8-2.3 2.3-3.5 4.5-3.5s3.7 1.2 4.5 3.5" />
    </svg>
  )
}

function BranchGlyph() {
  return (
    <svg viewBox="0 0 18 18" aria-hidden="true">
      <path d="M5 3v8.2A2.8 2.8 0 0 0 7.8 14H12" />
      <circle cx="5" cy="3" r="1.5" />
      <circle cx="13" cy="14" r="1.5" />
      <path d="M10 5.5 13 3l3 2.5M13 3v6" />
    </svg>
  )
}

function AgentGlyph() {
  return (
    <svg viewBox="0 0 18 18" aria-hidden="true">
      <rect x="3.5" y="5.5" width="11" height="8" rx="2" />
      <path d="M9 3v2.5M6.5 9h.01M11.5 9h.01M6.5 11.5h5" />
    </svg>
  )
}

function TerminalGlyph() {
  return (
    <svg viewBox="0 0 18 18" aria-hidden="true">
      <rect x="2.5" y="3.5" width="13" height="11" rx="1.5" />
      <path d="m5 7 2 2-2 2M9.5 11h3" />
    </svg>
  )
}

function ReviewGlyph() {
  return (
    <svg viewBox="0 0 18 18" aria-hidden="true">
      <path d="M4 2.8h7l3 3V15H4z" />
      <path d="M11 2.8V6h3M6.5 9h5M6.5 11.5h3.5" />
    </svg>
  )
}

function WorktreeCard({
  className,
  index,
  port,
  branch,
  detail,
  state,
  warm = false,
}: {
  className: string
  index: string
  port: string
  branch: string
  detail: string
  state: string
  warm?: boolean
}) {
  return (
    <div className={'topology-card ' + className + (warm ? ' warm' : '')}>
      <div className="worktree-label-row">
        <span>[worktree:{index}]</span>
        <code>{port}</code>
      </div>
      <strong>{branch}</strong>
      <div className="worktree-meta">
        <span>{detail}</span>
        <b>{state}</b>
      </div>
    </div>
  )
}

function TopologyScene() {
  return (
    <div className="topology-shell">
      <div className="topology-grid" />
      <div className="topology-orbit orbit-one" />
      <div className="topology-orbit orbit-two" />

      <svg className="topology-lines" viewBox="0 0 1200 500" aria-hidden="true">
        <path className="green-line" d="M600 287C478 240 355 190 207 145" />
        <path className="green-line" d="M600 287C487 336 381 388 264 440" />
        <path className="warm-line" d="M600 287C735 236 862 188 1012 147" />
        <path className="green-line" d="M600 287C710 338 793 382 884 430" />
        <circle cx="600" cy="287" r="4" />
      </svg>

      <div className="ascii-bonsai" aria-hidden="true">
        <pre>{String.raw`
                 .o00o.
              .888888888.
             888888888888.
          .8888888888888888.
         888888888888888888.
       (8888888/  \\88888888)
          88888{    }88888
            888 \\  / 888
              \\ || /
               \\||/
             ___||||___
          __/___||||___\\__
             __||||__
                ||`}</pre>
        <div className="ascii-core">[bonsai-core]</div>
        <div className="root-status"><span>roots</span><i /><span>active</span></div>
      </div>

      <WorktreeCard
        className="worktree-a"
        index="01"
        port="● :3801"
        branch="feat/auth-passkeys"
        detail="+142 / -18"
        state="ready"
      />

      <WorktreeCard
        className="worktree-b"
        index="02"
        port=":5432-pg"
        branch="fix/replica-lag-pool"
        detail="HEAD == origin"
        state="STAGED"
        warm
      />

      <div className="sandbox-chip">
        <span className="sandbox-icon"><BrandMark /></span>
        <strong>spike/wasm-jit</strong>
        <b>clean</b>
        <i />
        <span>detached sandbox</span>
      </div>
    </div>
  )
}

function ArchitectureSection() {
  const items: { icon: ReactNode; index: string; title: string; body: string; code: string }[] = [
    {
      icon: <BranchGlyph />,
      index: '01',
      title: 'Isolated worktrees',
      body: 'Each branch keeps its own filesystem, Git state, environment, and process space.',
      code: 'feat/auth-passkeys',
    },
    {
      icon: <AgentGlyph />,
      index: '02',
      title: 'Agents stay running',
      body: 'Move on while Claude, Astra, or another agent continues working in the background.',
      code: 'Claude · coding…',
    },
    {
      icon: <TerminalGlyph />,
      index: '03',
      title: 'Processes stay live',
      body: 'Run branch-local services and jump straight into the detected localhost URL.',
      code: 'localhost:5173 ↗',
    },
    {
      icon: <ReviewGlyph />,
      index: '04',
      title: 'PR state in context',
      body: 'Review, CI, comments, and branch status live beside the worktree they belong to.',
      code: 'PR #128 · CI ✓',
    },
  ]

  return (
    <section className="architecture-section" id="architecture">
      <div className="section-heading">
        <div className="eyebrow">[ VISUAL ARCHITECTURE ]</div>
        <h2>Everything stays attached<br />to the branch that owns it.</h2>
        <p>
          Worktrees are first-class. Agents, terminals, dev servers, tests, PRs, and logs
          stay bound to the branch that started them.
        </p>
      </div>

      <div className="architecture-grid">
        {items.map((item) => (
          <article key={item.index}>
            <div className="architecture-icon">{item.icon}</div>
            <span>{item.index}</span>
            <h3>{item.title}</h3>
            <p>{item.body}</p>
            <code>{item.code}</code>
          </article>
        ))}
      </div>
    </section>
  )
}

function ParallelSection() {
  return (
    <section className="parallel-section" id="parallel">
      <div className="parallel-copy">
        <div className="eyebrow">[ TUI CANVAS ]</div>
        <h2>Not switching tasks.<br /><span>Running them together.</span></h2>
      </div>
      <div className="parallel-terminal">
        <div className="terminal-titlebar"><span>bonsai / live activity</span><code>4 active</code></div>
        <div className="terminal-row"><StatusDot /><code>feat/auth-passkeys</code><span>Claude</span><b>coding…</b></div>
        <div className="terminal-row"><StatusDot /><code>feat/auth-passkeys</code><span>npm run dev</span><a>:5173 ↗</a></div>
        <div className="terminal-row warm-row"><span className="warm-dot" /><code>pr/128-review</code><span>Astra</span><b>reviewing…</b></div>
        <div className="terminal-row"><StatusDot /><code>tests/refactor</code><span>vitest</span><b>42 / 42 ✓</b></div>
      </div>
    </section>
  )
}

export function ParallaxLanding() {
  const [platform, setPlatform] = useState<InstallPlatform>('unix')
  const [copied, setCopied] = useState(false)
  const [quickCopied, setQuickCopied] = useState(false)

  const copy = async (value: string, quick = false) => {
    try {
      await navigator.clipboard.writeText(value)
      if (quick) {
        setQuickCopied(true)
        window.setTimeout(() => setQuickCopied(false), 1400)
      } else {
        setCopied(true)
        window.setTimeout(() => setCopied(false), 1400)
      }
    } catch {
      setCopied(false)
      setQuickCopied(false)
    }
  }

  return (
    <div className="bonsai-landing">
      <header className="site-nav">
        <div className="nav-left">
          <a className="brand-link" href="#top">
            <span className="brand-box"><BrandMark /></span>
            <b>BONSAI</b>
          </a>
          <span className="branch-state"><StatusDot /> [worktree:pruned]</span>
        </div>

        <nav className="nav-center" aria-label="Landing page">
          <a className="active" href="#showcase">Showcase</a>
          <a href="#architecture">Visual Architecture</a>
          <a href="#parallel">TUI Canvas</a>
          <a href="#install">Install</a>
        </nav>

        <div className="nav-right">
          <a className="star-chip" href="https://github.com/Tiago-0liveira/bonsai"><StarIcon /><span>4.9k</span></a>
          <a className="app-link" href="/app" aria-label="Open Bonsai app"><UserIcon /></a>
        </div>
      </header>

      <main>
        <section className="hero" id="top">
          <div className="hero-status-row">
            <span><StatusDot /><b>DAEMON ACTIVE</b><i>/</i> 3 worktrees linked</span>
            <button type="button" onClick={() => copy(INSTALL_COMMANDS.unix, true)}>
              <b>❯</b>
              <code>curl …/install.sh | bash</code>
              <em>{quickCopied ? 'copied' : 'copy'}</em>
            </button>
          </div>

          <div className="hero-eyebrow">[ GIT-WORKTREE-CULTIVATOR ] <span>—</span> ZERO-OVERHEAD CONCURRENCY</div>

          <h1>Grow branches.<br /><span>Prune friction.</span></h1>

          <div className="hero-pills">
            <span><b>0ms</b> stash penalty</span>
            <span><b>Isolated</b> ports &amp; envs</span>
            <span><b>Auto</b> garbage cleanup</span>
          </div>
        </section>

        <section className="showcase-section" id="showcase">
          <TopologyScene />
          <div className="showcase-footer">
            <span>↓ TOPOLOGY LAYER DOWNWARD</span>
            <span>[ESC] reset perspective</span>
          </div>
        </section>

        <ArchitectureSection />
        <ParallelSection />

        <section className="install-section" id="install">
          <div className="install-copy">
            <div className="eyebrow">[ INSTALL ]</div>
            <h2>One command.<br /><span>Then open /app.</span></h2>
            <p>Install the release binary, start the local server, and supervise every worktree from the browser. Go is not required.</p>
          </div>

          <div className="install-terminal">
            <div className="platform-switch" role="group" aria-label="Operating system">
              <button type="button" className={platform === 'unix' ? 'selected' : ''} onClick={() => setPlatform('unix')}>Linux / macOS</button>
              <button type="button" className={platform === 'windows' ? 'selected' : ''} onClick={() => setPlatform('windows')}>Windows</button>
            </div>
            <div className="install-line"><span>❯</span><code>{INSTALL_COMMANDS[platform]}</code><button type="button" onClick={() => copy(INSTALL_COMMANDS[platform])}>{copied ? 'copied' : 'copy'}</button></div>
            <div className="install-line secondary-line"><span>❯</span><code>bonsai serve</code><small>starts the local web server</small></div>
            <div className="install-actions"><a className="primary-action" href="/app">Open /app →</a><a href="https://github.com/Tiago-0liveira/bonsai">View GitHub ↗</a></div>
          </div>
        </section>
      </main>

      <footer>
        <span>BONSAI</span>
        <span>local worktrees / parallel agents / live processes</span>
        <a href="https://github.com/Tiago-0liveira/bonsai">GitHub ↗</a>
      </footer>
    </div>
  )
}

export default ParallaxLanding
