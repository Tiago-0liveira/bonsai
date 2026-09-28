import { useEffect, useRef, useState, type ReactNode } from 'react'
import './parallax.css'

type InstallPlatform = 'unix' | 'windows'

const INSTALL_COMMANDS: Record<InstallPlatform, string> = {
  unix: 'curl -fsSL https://raw.githubusercontent.com/Tiago-0liveira/bonsai/main/install.sh | bash',
  windows: 'irm https://raw.githubusercontent.com/Tiago-0liveira/bonsai/main/install.ps1 | iex',
}

const clamp = (value: number) => Math.max(0, Math.min(1, value))
const lerp = (from: number, to: number, t: number) => from + (to - from) * t
const smootherstep = (value: number) => {
  const t = clamp(value)
  return t * t * t * (t * (t * 6 - 15) + 10)
}
const range = (progress: number, start: number, end: number) =>
  smootherstep((progress - start) / Math.max(0.0001, end - start))
const band = (progress: number, inStart: number, inEnd: number, outStart: number, outEnd: number) =>
  range(progress, inStart, inEnd) * (1 - range(progress, outStart, outEnd))

function BrandMark() {
  return (
    <svg viewBox="0 0 28 28" aria-hidden="true">
      <path d="M14 23V10M14 15l-5-4M14 18l6-5M14 10l3-4" />
      <circle cx="9" cy="10.5" r="2.4" />
      <circle cx="20" cy="12.5" r="2.4" />
      <circle cx="17.5" cy="6" r="2.2" />
    </svg>
  )
}

function LiveDot() {
  return <span className="live-dot" aria-hidden="true" />
}

function AppPreview() {
  return (
    <div className="app-preview">
      <div className="app-topbar">
        <div className="app-brand">
          <BrandMark />
          <strong>Bonsai</strong>
        </div>
        <div className="app-project-switcher">
          <span>bonsai</span>
          <small>/ main</small>
        </div>
        <nav className="app-tabs" aria-label="Preview navigation">
          <b>Canvas</b>
          <span>Table</span>
          <span>GitHub</span>
          <span>Logs</span>
        </nav>
        <div className="app-local-status"><LiveDot /> local</div>
      </div>

      <div className="app-main">
        <aside className="app-sidebar">
          <small>Projects</small>
          <div className="project-row selected"><span className="project-glyph">B</span><b>bonsai</b><em>6</em></div>
          <div className="project-row"><span className="project-glyph">O</span><b>orchard-api</b><em>2</em></div>
          <div className="project-row"><span className="project-glyph">D</span><b>docs-site</b><em>1</em></div>
          <small className="sidebar-label">Views</small>
          <div className="sidebar-link active">Workspace</div>
          <div className="sidebar-link">Worktrees</div>
          <div className="sidebar-link">Agents</div>
          <div className="sidebar-link">Pull requests</div>
        </aside>

        <div className="app-canvas">
          <div className="canvas-heading">
            <div><strong>Workspace</strong><span>6 active worktrees</span></div>
            <button type="button" tabIndex={-1}>+ Worktree</button>
          </div>

          <div className="canvas-map">
            <svg viewBox="0 0 560 270" className="canvas-lines" aria-hidden="true">
              <path d="M278 143C231 133 205 101 166 82" />
              <path d="M283 137C329 117 357 92 404 74" />
              <path d="M288 148C339 156 371 178 420 194" />
              <path d="M273 151C235 174 208 194 169 211" />
            </svg>
            <div className="canvas-root">
              <BrandMark />
              <div><b>bonsai</b><span>main</span></div>
            </div>
            <div className="canvas-chip chip-auth"><LiveDot /><b>feat/auth-passkeys</b><span>Claude</span></div>
            <div className="canvas-chip chip-pr"><LiveDot /><b>pr/128-review</b><span>Astra</span></div>
            <div className="canvas-chip chip-runtime"><LiveDot /><b>feat/live-preview</b><span>3 processes</span></div>
            <div className="canvas-chip chip-tests"><LiveDot /><b>tests/refactor</b><span>42 / 42</span></div>
          </div>
        </div>

        <aside className="app-inspector">
          <small>Inspector</small>
          <div className="inspector-branch"><LiveDot /><div><b>feat/auth-passkeys</b><span>running</span></div></div>
          <dl>
            <div><dt>Agent</dt><dd>Claude</dd></div>
            <div><dt>Files</dt><dd>12 changed</dd></div>
            <div><dt>PR</dt><dd>#128</dd></div>
            <div><dt>CI</dt><dd className="good">passing</dd></div>
          </dl>
          <div className="inspector-action">Open worktree</div>
        </aside>
      </div>

      <div className="app-dock">
        <div className="dock-tab"><LiveDot /><b>web</b><code>:5173</code></div>
        <div className="dock-tab"><LiveDot /><b>api</b><code>:7001</code></div>
        <div className="dock-tab"><span className="check-dot">✓</span><b>tests</b><code>42 / 42</code></div>
        <span className="dock-spacer" />
        <span className="dock-log">18:42:12 &nbsp; GitHub state refreshed</span>
      </div>
    </div>
  )
}

function BranchNetwork() {
  return (
    <svg className="branch-network" viewBox="0 0 1100 720" aria-hidden="true">
      <g className="network-base">
        <path d="M550 360C470 317 412 249 312 176" />
        <path d="M550 354C632 307 692 261 802 195" />
        <path d="M558 369C660 383 730 415 856 466" />
        <path d="M544 373C453 404 389 444 284 502" />
        <path d="M552 379C557 440 548 492 556 582" />
      </g>
      <g className="network-secondary">
        <path d="M311 176C268 151 232 145 193 151" />
        <path d="M802 195C850 179 892 181 931 203" />
        <path d="M856 466C900 470 938 492 966 520" />
        <path d="M284 502C237 512 203 536 178 563" />
        <path d="M556 582C599 607 630 635 655 670" />
      </g>
      <g className="network-active active-claude">
        <path d="M550 360C470 317 412 249 312 176" />
      </g>
      <g className="network-active active-astra">
        <path d="M550 354C632 307 692 261 802 195" />
      </g>
      <g className="network-active active-runtime">
        <path d="M558 369C660 383 730 415 856 466" />
      </g>
      <g className="network-active active-parallel">
        <path d="M544 373C453 404 389 444 284 502" />
        <path d="M552 379C557 440 548 492 556 582" />
      </g>
      <g className="network-projects">
        <path d="M550 360C451 314 333 302 202 327" />
        <path d="M550 360C651 317 779 309 930 340" />
      </g>
    </svg>
  )
}

function WorktreeNode({
  className,
  branch,
  meta,
  state,
  children,
}: {
  className: string
  branch: string
  meta: string
  state: string
  children?: ReactNode
}) {
  return (
    <div className={'worktree-node ' + className}>
      <div className="node-head">
        <LiveDot />
        <b>{branch}</b>
        <span>{state}</span>
      </div>
      <div className="node-meta">{meta}</div>
      <div className="node-detail">{children}</div>
    </div>
  )
}

function WorktreeNodes() {
  return (
    <div className="worktree-layer">
      <WorktreeNode className="node-claude" branch="feat/auth-passkeys" meta="Claude · Sonnet 4.5" state="coding…">
        <p>Implementing WebAuthn recovery</p>
        <div className="node-progress"><i /><span>tests 28 / 32</span></div>
      </WorktreeNode>

      <WorktreeNode className="node-astra" branch="PR #128" meta="ChatGPT Astra · reviewing" state="reviewing…">
        <p>4 comments · 2 suggested fixes</p>
        <div className="node-checks"><span>CI ✓</span><span>diff +214 −39</span></div>
      </WorktreeNode>

      <WorktreeNode className="node-runtime" branch="feat/live-preview" meta="Branch runtime" state="3 live">
        <div className="runtime-row"><span>web</span><code>:5173</code></div>
        <div className="runtime-row"><span>api</span><code>:7001</code></div>
        <div className="runtime-row"><span>tests</span><code>42 / 42 ✓</code></div>
      </WorktreeNode>

      <WorktreeNode className="node-tests" branch="tests/refactor" meta="Vitest · watch" state="passing">
        <p>42 / 42 · watching files</p>
      </WorktreeNode>

      <WorktreeNode className="node-events" branch="github/events" meta="Realtime relay" state="live">
        <p>PRs · checks · branch state</p>
      </WorktreeNode>
    </div>
  )
}

function ProjectClusters() {
  return (
    <div className="project-clusters">
      <div className="project-cluster cluster-orchard">
        <small>orchard-api</small>
        <span><LiveDot /> Claude</span>
        <span><LiveDot /> dev server :7001</span>
      </div>
      <div className="project-cluster cluster-docs">
        <small>docs-site</small>
        <span><LiveDot /> preview :4173</span>
      </div>
    </div>
  )
}

function GithubActivity() {
  return (
    <div className="github-activity">
      <div className="github-title"><span>GitHub realtime</span><LiveDot /></div>
      <div><time>18:42:07</time><b>CI passed</b><span>feat/auth</span></div>
      <div><time>18:42:11</time><b>PR #128 updated</b><span>pr/review</span></div>
      <div><time>18:42:12</time><b>Bonsai refreshed</b><span className="good">✓</span></div>
    </div>
  )
}

function SurfaceRail() {
  return (
    <div className="surface-rail">
      <div className="surface-web">
        <small>Web</small>
        <b>Supervise everything.</b>
        <span>/app</span>
      </div>
      <div>
        <small>TUI</small>
        <b>Keyboard-first focus.</b>
      </div>
      <div>
        <small>CLI</small>
        <b>Automate it.</b>
      </div>
    </div>
  )
}

function ProductScene() {
  return (
    <div className="product-stage" aria-hidden="true">
      <div className="product-camera">
        <div className="scene-halo" />
        <svg className="branch-ghost" viewBox="0 0 1100 720">
          <path d="M550 359C466 312 398 242 303 177M550 355C630 312 704 251 803 195M558 370C655 382 741 423 856 467M543 373C446 405 377 455 284 502M552 378C555 443 548 505 556 582" />
        </svg>
        <BranchNetwork />
        <AppPreview />
        <WorktreeNodes />
        <ProjectClusters />
        <GithubActivity />
        <SurfaceRail />
      </div>
    </div>
  )
}

function StoryCard({
  eyebrow,
  title,
  body,
  className = '',
}: {
  eyebrow: string
  title: ReactNode
  body: ReactNode
  className?: string
}) {
  return (
    <article className={'story-card ' + className}>
      <small>{eyebrow}</small>
      <h2>{title}</h2>
      <p>{body}</p>
    </article>
  )
}

export function ParallaxLanding() {
  const rootRef = useRef<HTMLDivElement>(null)
  const [platform, setPlatform] = useState<InstallPlatform>('unix')
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    const root = rootRef.current
    if (!root) return

    document.documentElement.classList.add('bonsai-landing-html')
    document.body.classList.add('bonsai-landing-body')

    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)')
    let frame = 0

    const setNumber = (name: string, value: number) => {
      root.style.setProperty(name, value.toFixed(4))
    }
    const setDimension = (name: string, value: number, unit: 'vw' | 'vh') => {
      root.style.setProperty(name, value.toFixed(3) + unit)
    }

    const render = () => {
      frame = 0
      const maxScroll = Math.max(1, root.scrollHeight - window.innerHeight)
      const localScroll = clamp((window.scrollY - root.offsetTop) / maxScroll)
      const progress = localScroll
      const mobile = window.innerWidth < 820

      const network = range(progress, 0.18, 0.31)
      const claude = band(progress, 0.32, 0.38, 0.44, 0.50)
      const astra = band(progress, 0.44, 0.50, 0.56, 0.62)
      const runtime = band(progress, 0.56, 0.62, 0.68, 0.74)
      const parallel = range(progress, 0.65, 0.75)
      const multi = range(progress, 0.77, 0.86)
      const github = range(progress, 0.79, 0.87)
      const reveal = range(progress, 0.87, 0.95)
      const install = range(progress, 0.955, 0.985)

      const settle = range(progress, 0.04, 0.18)
      const focusZoom = Math.max(claude, astra, runtime)
      let cameraX = lerp(18, 0, settle)
      let cameraY = claude * 4.5 - runtime * 4.5
      let cameraScale = lerp(0.86, 1, settle) + focusZoom * 0.13 - multi * 0.12 + reveal * 0.08

      if (progress > 0.18) {
        cameraX += claude * -1.6 + astra * 1.2 + runtime * 1.8
      }

      if (mobile) {
        cameraX = 0
        cameraY = lerp(11, 3, settle) + claude * 2 - runtime * 2
        cameraScale = lerp(0.7, 0.82, settle) + focusZoom * 0.05 - multi * 0.04 + reveal * 0.04
      }

      if (reduced.matches) {
        cameraX = 0
        cameraY = mobile ? 5 : 0
        cameraScale = mobile ? 0.78 : 0.96
      }

      const baseNode = network * 0.28 + parallel * 0.44
      const claudeOpacity = clamp(baseNode + claude * 0.72)
      const astraOpacity = clamp(baseNode + astra * 0.72)
      const runtimeOpacity = clamp(baseNode + runtime * 0.72)
      const testsOpacity = clamp(baseNode * 0.9 + parallel * 0.2)
      const eventsOpacity = clamp(baseNode * 0.9 + parallel * 0.2 + github * 0.25)

      setNumber('--page-progress', progress)
      setDimension('--camera-x-vw', cameraX, 'vw')
      setDimension('--camera-y-vh', cameraY, 'vh')
      setNumber('--camera-scale', cameraScale)
      setNumber('--network-opacity', reduced.matches ? 0.9 : network)
      setNumber('--claude-focus', reduced.matches ? 0.18 : claude)
      setNumber('--astra-focus', reduced.matches ? 0.18 : astra)
      setNumber('--runtime-focus', reduced.matches ? 0.18 : runtime)
      setNumber('--parallel', reduced.matches ? 0.9 : parallel)
      setNumber('--multi', reduced.matches ? 0.9 : multi)
      setNumber('--github', reduced.matches ? 0.9 : github)
      setNumber('--reveal', reduced.matches ? 0.9 : reveal)
      setNumber('--stage-opacity', reduced.matches ? 1 - install : 1 - install)
      setNumber('--app-opacity', clamp(1 - focusZoom * 0.18 + reveal * 0.18))
      setNumber('--claude-o', reduced.matches ? 0.7 : claudeOpacity)
      setNumber('--astra-o', reduced.matches ? 0.7 : astraOpacity)
      setNumber('--runtime-o', reduced.matches ? 0.7 : runtimeOpacity)
      setNumber('--tests-o', reduced.matches ? 0.62 : testsOpacity)
      setNumber('--events-o', reduced.matches ? 0.62 : eventsOpacity)
      setNumber('--claude-s', 0.94 + (reduced.matches ? 0 : claude) * 0.08)
      setNumber('--astra-s', 0.94 + (reduced.matches ? 0 : astra) * 0.08)
      setNumber('--runtime-s', 0.94 + (reduced.matches ? 0 : runtime) * 0.08)
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

  const copyInstall = async () => {
    try {
      await navigator.clipboard.writeText(INSTALL_COMMANDS[platform])
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1400)
    } catch {
      setCopied(false)
    }
  }

  return (
    <div className="bonsai-landing" ref={rootRef}>
      <ProductScene />

      <header className="site-nav">
        <a className="nav-island nav-logo" href="#top" aria-label="Bonsai home">
          <BrandMark />
          <b>Bonsai</b>
        </a>
        <nav className="nav-island nav-links" aria-label="Landing page">
          <a href="#product">Product</a>
          <a href="#worktrees">Worktrees</a>
          <a href="#install">Install</a>
        </nav>
        <div className="nav-actions">
          <a className="nav-island nav-github" href="https://github.com/Tiago-0liveira/bonsai">GitHub ↗</a>
          <a className="nav-island nav-app" href="/app">Open app</a>
        </div>
      </header>

      <main>
        <section className="hero-moment" id="top">
          <div className="hero-copy">
            <div className="eyebrow"><LiveDot /> Local developer workspace</div>
            <h1>Run every branch at once.</h1>
            <p>
              Bonsai connects the browser to your local server so worktrees, agents, processes,
              changed files, PRs, CI, logs, and projects stay visible in one place.
            </p>
            <div className="hero-actions">
              <a className="button-primary" href="/app">Open /app <span>→</span></a>
              <a className="button-secondary" href="#install">Install Bonsai</a>
            </div>
            <div className="hero-capabilities" aria-label="Bonsai capabilities">
              <span>worktrees + agents</span>
              <span>processes + logs</span>
              <span>PRs + CI</span>
              <span>multiple projects</span>
            </div>
          </div>
        </section>

        <section className="one-workspace moment" id="product">
          <div className="story-pin">
            <StoryCard
              eyebrow="One workspace"
              title={<>The product becomes<br />the branch map.</>}
              body={<>The <strong>/app</strong> canvas stays at the center. As you scroll, its live worktrees grow outward as a quiet branch system instead of a separate diagram.</>}
            />
          </div>
        </section>

        <section className="parallel-moment moment" id="worktrees">
          <div className="parallel-step">
            <div className="story-pin">
              <StoryCard
                eyebrow="Worktree · agent"
                title={<>Claude keeps coding.<br /><em>You keep moving.</em></>}
                body={<>An isolated branch keeps its agent, filesystem, process, and Git state while the rest of the workspace remains visible.</>}
              />
            </div>
          </div>

          <div className="parallel-step">
            <div className="story-pin align-right">
              <StoryCard
                eyebrow="Pull request review"
                title={<>Astra reviews the PR.<br /><em>Claude is still running.</em></>}
                body={<>Review, checks, comments, and suggested fixes stay attached to their own worktree. Background work shrinks to a live state—it never disappears.</>}
              />
            </div>
          </div>

          <div className="parallel-step">
            <div className="story-pin">
              <StoryCard
                eyebrow="Branch runtime"
                title={<>Every worktree can run<br /><em>its own stack.</em></>}
                body={<>Dev servers, tests, and long-running processes stay bound to the branch that started them, with detected local URLs reachable from the web client.</>}
              />
            </div>
          </div>

          <div className="parallel-step parallel-payoff">
            <div className="story-pin centered-copy">
              <StoryCard
                eyebrow="Parallel by default"
                title={<>Not switching tasks.<br /><em>Running them together.</em></>}
                body={<>Coding, review, servers, tests, and GitHub events remain alive at the same time. The camera changes focus; the work keeps going.</>}
              />
            </div>
          </div>
        </section>

        <section className="scale-moment moment" id="projects">
          <div className="scale-step">
            <div className="story-pin">
              <StoryCard
                eyebrow="Multiple projects"
                title={<>One browser.<br /><em>Every worktree still alive.</em></>}
                body={<>Pull back from one repository and supervise Bonsai, an API, and a docs site from the same local control surface. GitHub realtime updates land inside the workspace instead of becoming a separate slide.</>}
              />
            </div>
          </div>

          <div className="scale-step final-reveal">
            <div className="story-pin centered-copy">
              <StoryCard
                eyebrow="Bonsai / app"
                title={<>Everything that&apos;s running.<br /><em>One place to see it.</em></>}
                body={<>Use the web client for the overview, the TUI for keyboard-first focus, and the CLI for automation. They share the same local worktree model.</>}
              />
            </div>
          </div>
        </section>

        <section className="install-moment" id="install">
          <div className="install-panel">
            <div className="eyebrow"><LiveDot /> Local first</div>
            <h2>Start growing branches.</h2>
            <p>Install the release binary, start the local server, then open the web client. Go is not required.</p>

            <div className="platform-switch" role="group" aria-label="Operating system">
              <button
                className={platform === 'unix' ? 'selected' : ''}
                type="button"
                onClick={() => setPlatform('unix')}
              >
                Linux / macOS
              </button>
              <button
                className={platform === 'windows' ? 'selected' : ''}
                type="button"
                onClick={() => setPlatform('windows')}
              >
                Windows
              </button>
            </div>

            <div className="install-command">
              <span>$</span>
              <code>{INSTALL_COMMANDS[platform]}</code>
              <button type="button" onClick={copyInstall}>{copied ? 'Copied' : 'Copy'}</button>
            </div>

            <div className="serve-command"><span>then</span><code>bonsai serve</code></div>

            <div className="install-actions">
              <a className="button-primary" href="/app">Open /app <span>→</span></a>
              <a className="button-secondary" href="https://github.com/Tiago-0liveira/bonsai">View on GitHub ↗</a>
            </div>
          </div>
        </section>
      </main>

      <footer>
        <span>Bonsai</span>
        <span>Local worktrees · parallel agents · one workspace</span>
        <a href="https://github.com/Tiago-0liveira/bonsai">GitHub ↗</a>
      </footer>
    </div>
  )
}

export default ParallaxLanding
