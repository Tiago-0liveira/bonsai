import { useEffect, useRef, useState, type ReactNode } from 'react'
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

function StatusDot({ warm = false }: { warm?: boolean }) {
  return <span className={'status-dot' + (warm ? ' warm' : '')} aria-hidden="true" />
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

function TopologyCard({
  className,
  accent = 'green',
  children,
}: {
  className: string
  accent?: 'green' | 'warm' | 'sage'
  children: ReactNode
}) {
  return <div className={'topology-card ' + className + ' accent-' + accent}>{children}</div>
}

function TopologyScene() {
  return (
    <div className="topology-shell">
      <div className="topology-grid" />
      <div className="topology-orbit orbit-one" />
      <div className="topology-orbit orbit-two" />

      <svg className="topology-lines" viewBox="0 0 1200 620" aria-hidden="true">
        <path className="line-main" d="M600 344C503 309 395 250 282 184" />
        <path className="line-review" d="M600 344C711 302 831 245 938 184" />
        <path className="line-agent" d="M600 344C529 392 490 444 447 497" />
        <path className="line-process" d="M600 344C683 389 738 436 788 493" />
        <circle cx="600" cy="344" r="4" />
        <circle cx="282" cy="184" r="3" />
        <circle cx="938" cy="184" r="3" />
        <circle cx="447" cy="497" r="3" />
        <circle cx="788" cy="493" r="3" />
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
          ____||||____
        _/____||||____\\_
           ___||||___
              ||`}</pre>
        <div className="root-status"><span>roots</span><i /><span>active</span></div>
      </div>

      <TopologyCard className="worktree-a">
        <div className="card-kicker"><span>[worktree:01]</span><code>● :3801</code></div>
        <strong>feat/auth-passkeys</strong>
        <div className="card-meta"><span>+142 / -18</span><b>ready</b></div>
      </TopologyCard>

      <TopologyCard className="worktree-b" accent="warm">
        <div className="card-kicker"><span>[worktree:02]</span><code>:5432-pg</code></div>
        <strong>pr/128-review</strong>
        <div className="card-meta"><span>HEAD == origin</span><b>review</b></div>
      </TopologyCard>

      <TopologyCard className="agent-card" accent="sage">
        <div className="runtime-head">
          <span className="icon-box"><AgentGlyph /></span>
          <div><StatusDot /><strong>Claude · Sonnet 4.5</strong></div>
          <em>RUNNING</em>
        </div>
        <p>Implementing WebAuthn recovery flow</p>
        <div className="runtime-foot"><span>tests 28 / 32</span><b>coding…</b></div>
      </TopologyCard>

      <TopologyCard className="process-card">
        <div className="runtime-head">
          <span className="icon-box"><TerminalGlyph /></span>
          <div><StatusDot /><strong>npm run dev</strong></div>
          <em>LIVE</em>
        </div>
        <p>VITE v6.1 · ready in 412ms</p>
        <a className="local-link" tabIndex={-1}>http://localhost:5173 <b>↗</b></a>
      </TopologyCard>

      <div className="core-chip"><BrandMark /><strong>bonsai core</strong><span>4 live nodes</span></div>
    </div>
  )
}

function ArchitectureSection() {
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
        <article>
          <div className="architecture-icon"><BranchGlyph /></div>
          <span>01</span>
          <h3>Isolated worktrees</h3>
          <p>Each branch keeps its own filesystem, Git state, environment, and process space.</p>
          <code>feat/auth-passkeys</code>
        </article>
        <article>
          <div className="architecture-icon"><AgentGlyph /></div>
          <span>02</span>
          <h3>Agents stay running</h3>
          <p>Move on while Claude, Astra, or another agent continues working in the background.</p>
          <code>Claude · coding…</code>
        </article>
        <article>
          <div className="architecture-icon"><TerminalGlyph /></div>
          <span>03</span>
          <h3>Processes stay live</h3>
          <p>Run branch-local services and jump straight into the detected localhost URL.</p>
          <code>localhost:5173 ↗</code>
        </article>
        <article>
          <div className="architecture-icon"><ReviewGlyph /></div>
          <span>04</span>
          <h3>PR state in context</h3>
          <p>Review, CI, comments, and branch status live beside the worktree they belong to.</p>
          <code>PR #128 · CI ✓</code>
        </article>
      </div>
    </section>
  )
}

function ParallelSection() {
  return (
    <section className="parallel-section" id="parallel">
      <div className="parallel-copy">
        <div className="eyebrow">[ PARALLEL BY DEFAULT ]</div>
        <h2>Not switching tasks.<br /><span>Running them together.</span></h2>
      </div>
      <div className="parallel-terminal">
        <div className="terminal-titlebar"><span>bonsai / live activity</span><code>4 active</code></div>
        <div className="terminal-row"><StatusDot /><code>feat/auth-passkeys</code><span>Claude</span><b>coding…</b></div>
        <div className="terminal-row"><StatusDot /><code>feat/auth-passkeys</code><span>npm run dev</span><a>:5173 ↗</a></div>
        <div className="terminal-row"><StatusDot warm /><code>pr/128-review</code><span>Astra</span><b className="warm-text">reviewing…</b></div>
        <div className="terminal-row"><StatusDot /><code>tests/refactor</code><span>vitest</span><b>42 / 42 ✓</b></div>
      </div>
    </section>
  )
}

export function ParallaxLanding() {
  const rootRef = useRef<HTMLDivElement>(null)
  const [platform, setPlatform] = useState<InstallPlatform>('unix')
  const [copied, setCopied] = useState(false)
  const [quickCopied, setQuickCopied] = useState(false)

  useEffect(() => {
    const root = rootRef.current
    if (!root) return

    document.documentElement.classList.add('bonsai-landing-html')
    document.body.classList.add('bonsai-landing-body')

    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)')
    let frame = 0

    const render = () => {
      frame = 0
      const showcase = root.querySelector<HTMLElement>('[data-showcase]')
      if (!showcase) return

      const rect = showcase.getBoundingClientRect()
      const span = window.innerHeight + rect.height
      const raw = Math.max(0, Math.min(1, (window.innerHeight - rect.top) / Math.max(1, span)))
      const progress = reduced.matches ? 0.5 : raw

      root.style.setProperty('--showcase-progress', progress.toFixed(4))
      root.style.setProperty('--showcase-drift', ((progress - 0.5) * 18).toFixed(2) + 'px')
      root.style.setProperty('--showcase-depth', (1 + Math.sin(progress * Math.PI) * 0.025).toFixed(4))
    }

    const requestRender = () => {
      if (!frame) frame = requestAnimationFrame(render)
    }

    render()
    window.addEventListener('scroll', requestRender, { passive: true })
    window.addEventListener('resize', requestRender)
    reduced.addEventListener('change', requestRender)

    return () => {
      if (frame) cancelAnimationFrame(frame)
      window.removeEventListener('scroll', requestRender)
      window.removeEventListener('resize', requestRender)
      reduced.removeEventListener('change', requestRender)
      document.documentElement.classList.remove('bonsai-landing-html')
      document.body.classList.remove('bonsai-landing-body')
    }
  }, [])

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
    <div className="bonsai-landing" ref={rootRef}>
      <header className="site-nav">
        <div className="nav-left">
          <a className="brand-link" href="#top"><span className="brand-box"><BrandMark /></span><b>BONSAI</b></a>
          <span className="branch-state"><StatusDot /> [worktree:pruned]</span>
        </div>

        <nav className="nav-center" aria-label="Landing page">
          <a className="active" href="#showcase">Showcase</a>
          <a href="#architecture">Visual Architecture</a>
          <a href="#parallel">Parallelism</a>
          <a href="#install">Install</a>
        </nav>

        <div className="nav-right">
          <a className="github-link" href="https://github.com/Tiago-0liveira/bonsai">GitHub ↗</a>
          <a className="app-link" href="/app" aria-label="Open Bonsai app">/app</a>
        </div>
      </header>

      <main>
        <section className="hero" id="top">
          <div className="hero-status-row">
            <span><StatusDot /> DAEMON ACTIVE <i>/</i> 3 worktrees linked</span>
            <button type="button" onClick={() => copy(INSTALL_COMMANDS.unix, true)}>
              <b>❯</b> curl …/install.sh | bash <em>{quickCopied ? 'copied' : 'copy'}</em>
            </button>
          </div>

          <div className="hero-eyebrow">[ LOCAL WORKTREE ORCHESTRATOR ] <span>—</span> ZERO-OVERHEAD CONCURRENCY</div>
          <h1>Run every branch.<br /><span>Keep every process alive.</span></h1>

          <div className="hero-pills">
            <span><b>0ms</b> context switching</span>
            <span><b>Isolated</b> ports &amp; envs</span>
            <span><b>Realtime</b> agents + CI</span>
          </div>
        </section>

        <section className="showcase-section" id="showcase" data-showcase>
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
