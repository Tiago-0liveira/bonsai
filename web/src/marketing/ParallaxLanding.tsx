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

function BranchIcon() {
  return (
    <svg viewBox="0 0 18 18" aria-hidden="true">
      <path d="M5 3v8.2A2.8 2.8 0 0 0 7.8 14H12" />
      <circle cx="5" cy="3" r="1.5" />
      <circle cx="13" cy="14" r="1.5" />
      <path d="M10 5.5 13 3l3 2.5" />
      <path d="M13 3v6" />
    </svg>
  )
}

function AgentIcon() {
  return (
    <svg viewBox="0 0 18 18" aria-hidden="true">
      <rect x="3.5" y="5.5" width="11" height="8" rx="2" />
      <path d="M9 3v2.5M6.5 9h.01M11.5 9h.01M6.5 11.5h5" />
    </svg>
  )
}

function TerminalIcon() {
  return (
    <svg viewBox="0 0 18 18" aria-hidden="true">
      <rect x="2.5" y="3.5" width="13" height="11" rx="1.5" />
      <path d="m5 7 2 2-2 2M9.5 11h3" />
    </svg>
  )
}

function ReviewIcon() {
  return (
    <svg viewBox="0 0 18 18" aria-hidden="true">
      <path d="M4 2.8h7l3 3V15H4z" />
      <path d="M11 2.8V6h3M6.5 9h5M6.5 11.5h3.5" />
    </svg>
  )
}

function Story({
  eyebrow,
  title,
  children,
  align = 'left',
}: {
  eyebrow: string
  title: ReactNode
  children: ReactNode
  align?: 'left' | 'right' | 'center'
}) {
  return (
    <div className={'story-copy story-' + align}>
      <div className="story-eyebrow">{eyebrow}</div>
      <h2>{title}</h2>
      <p>{children}</p>
    </div>
  )
}

function ProjectNode() {
  return (
    <div className="graph-card project-node">
      <div className="project-head">
        <div className="graph-icon project-icon"><BrandMark /></div>
        <div className="project-name">
          <div><StatusDot /><strong>bonsai</strong></div>
          <code>bonsai</code>
          <span>CI healthy</span>
        </div>
      </div>
      <div className="project-stats">
        <div><b>11</b><span>worktrees</span></div>
        <div><b>2</b><span>running</span></div>
        <div><b>1</b><span>open PR</span></div>
        <div><b>0</b><span>CI failed</span></div>
      </div>
    </div>
  )
}

function MainBranchNode() {
  return (
    <div className="graph-card worktree-node main-worktree">
      <div className="worktree-top">
        <div className="graph-icon branch-icon"><BranchIcon /></div>
        <div className="worktree-title">
          <strong>feat/auth-passkeys</strong>
          <span className="env-tag">production</span>
        </div>
      </div>
      <div className="worktree-target"><span>↳</span> target <code>main</code></div>
      <div className="worktree-state"><span className="state-pill"><StatusDot /> AI running</span></div>
      <div className="worktree-foot"><span>0:14</span><span>1 canvas agent</span></div>
    </div>
  )
}

function ClaudeNode() {
  return (
    <div className="graph-card compact-node claude-node">
      <div className="compact-head">
        <div className="graph-icon"><AgentIcon /></div>
        <div>
          <div className="compact-title"><StatusDot /><strong>Claude</strong></div>
          <code>Sonnet 4.5</code>
        </div>
        <span className="running-label">RUNNING</span>
      </div>
      <p>Implementing WebAuthn recovery flow</p>
      <div className="task-progress">
        <span><i /></span>
        <code>tests 28 / 32</code>
      </div>
    </div>
  )
}

function ProcessNode() {
  return (
    <div className="graph-card compact-node process-node">
      <div className="compact-head">
        <div className="graph-icon"><TerminalIcon /></div>
        <div>
          <div className="compact-title"><StatusDot /><strong>npm run dev</strong></div>
          <code>vite --host 0.0.0.0</code>
        </div>
        <span className="running-label">RUN</span>
      </div>
      <div className="terminal-lines">
        <span>VITE v6.1 ready in 412ms</span>
        <a tabIndex={-1}>http://localhost:5173 <b>↗</b></a>
      </div>
    </div>
  )
}

function TestNode() {
  return (
    <div className="graph-card mini-node test-node">
      <div><StatusDot /><strong>tests</strong><span>watch</span></div>
      <code>42 / 42 ✓</code>
    </div>
  )
}

function ReviewWorktreeNode() {
  return (
    <div className="graph-card worktree-node review-worktree">
      <div className="worktree-top">
        <div className="graph-icon branch-icon warm"><BranchIcon /></div>
        <div className="worktree-title">
          <strong>pr/128-review</strong>
          <span className="review-tag">review</span>
        </div>
      </div>
      <div className="worktree-target"><span>↳</span> target <code>main</code></div>
      <div className="worktree-state"><span className="state-pill warm"><StatusDot warm /> AI reviewing</span></div>
      <div className="worktree-foot"><span>PR #128</span><span>CI ✓</span></div>
    </div>
  )
}

function AstraNode() {
  return (
    <div className="graph-card compact-node astra-node">
      <div className="compact-head">
        <div className="graph-icon warm"><ReviewIcon /></div>
        <div>
          <div className="compact-title"><StatusDot warm /><strong>ChatGPT Astra</strong></div>
          <code>PR review</code>
        </div>
        <span className="reviewing-label">REVIEW</span>
      </div>
      <p>Security + regression pass</p>
      <div className="review-metrics">
        <span>4 comments</span>
        <span>2 suggested fixes</span>
        <span className="ok">CI ✓</span>
      </div>
    </div>
  )
}

function OrchardCluster() {
  return (
    <div className="project-cluster orchard-cluster">
      <div className="cluster-label"><StatusDot /><strong>orchard-api</strong><code>project 02</code></div>
      <div className="cluster-item"><StatusDot /><span>Claude</span><code>coding…</code></div>
      <div className="cluster-item"><StatusDot /><span>api server</span><a tabIndex={-1}>:7001 ↗</a></div>
    </div>
  )
}

function DocsCluster() {
  return (
    <div className="project-cluster docs-cluster">
      <div className="cluster-label"><StatusDot /><strong>docs-site</strong><code>project 03</code></div>
      <div className="cluster-item"><StatusDot /><span>preview</span><a tabIndex={-1}>:4173 ↗</a></div>
    </div>
  )
}

function GithubStrip() {
  return (
    <div className="github-strip">
      <div><time>18:42:07</time><b>CI passed</b><code>feat/auth</code></div>
      <div><time>18:42:11</time><b>PR #128 updated</b><code>pr/review</code></div>
      <div><time>18:42:12</time><b>workspace refresh</b><span>✓</span></div>
    </div>
  )
}

function BonsaiScene() {
  return (
    <div className="bonsai-stage" aria-hidden="true">
      <div className="bonsai-camera">
        <div className="canvas-dots" />

        <svg className="bonsai-art" viewBox="0 0 1200 820" role="presentation">
          <defs>
            <linearGradient id="trunkCedar" x1="0" y1="0" x2="1" y2="1">
              <stop offset="0" stopColor="#44362f" />
              <stop offset=".42" stopColor="#342721" />
              <stop offset="1" stopColor="#251913" />
            </linearGradient>
          </defs>

          <g className="foliage foliage-back">
            <path d="M176 224c31-55 111-71 161-37 57-29 132 0 140 53-18 53-91 65-151 48-69 30-154-3-150-64Z" />
            <path d="M694 164c39-55 121-61 166-19 67-22 126 20 120 72-31 49-108 50-159 28-74 21-142-25-127-81Z" />
            <path d="M790 381c43-47 121-43 156 4 69-9 117 41 102 90-39 42-112 33-157 5-75 8-132-46-101-99Z" />
            <path d="M174 426c39-48 118-50 159-6 67-15 123 31 113 82-35 47-110 44-159 18-73 15-138-37-113-94Z" />
          </g>

          <path className="trunk-shadow" d="M566 772c-19-84-5-153 32-206 40-58 49-109 27-159-22-51-9-98 34-142 44-45 54-88 36-126 29 31 36 74 20 119-18 51-59 84-72 126-13 41 2 80 28 123 34 56 39 121 14 195-12 35-20 58-24 70Z" />
          <path className="trunk" d="M547 771c-9-72 6-138 43-193 37-55 45-104 24-151-22-49-10-95 31-137 43-44 52-82 37-122 26 31 30 69 16 108-16 44-53 79-67 119-15 42-2 81 25 125 34 56 40 118 16 187-13 37-24 58-31 67Z" />

          <g className="bark">
            <path d="M574 733c-2-53 10-105 39-149 29-44 37-85 26-124M625 417c-17-48-6-90 31-127M655 264c24-29 34-56 30-82" />
            <path d="M609 735c8-58 29-108 52-150 23-44 23-83 5-125" />
          </g>

          <g className="natural-branches">
            <path className="branch branch-main" d="M628 415C557 363 492 306 407 264C338 230 272 224 204 244" />
            <path className="twig" d="M435 280C397 246 354 214 303 196" />
            <path className="twig" d="M367 250C323 264 286 290 255 326" />

            <path className="branch branch-runtime" d="M641 447C714 421 783 376 846 327C895 289 946 278 1005 296" />
            <path className="twig" d="M833 337C884 339 928 355 968 383" />
            <path className="twig" d="M902 293C941 269 979 258 1022 263" />

            <path className="branch branch-review" d="M640 357C699 307 762 255 843 221C900 197 952 197 1012 218" />
            <path className="twig" d="M812 234C854 207 886 176 904 139" />

            <path className="branch branch-lower" d="M596 540C527 516 458 494 378 486C308 479 253 497 194 535" />
            <path className="twig" d="M356 488C319 462 280 445 234 441" />

            <path className="branch branch-projects" d="M620 590C683 593 746 613 811 649C862 677 917 686 979 670" />
          </g>

          <g className="graph-connectors">
            <path className="edge edge-project-main" d="M604 608C593 550 591 503 603 458" />
            <path className="edge edge-main-worktree" d="M603 458C531 418 474 369 414 319" />
            <path className="edge edge-claude" d="M414 319C366 286 318 270 267 268" />
            <path className="edge edge-process" d="M414 319C472 304 523 285 572 251" />
            <path className="edge edge-tests" d="M414 319C429 364 448 397 477 430" />

            <path className="edge edge-review-worktree" d="M617 420C683 370 747 320 811 283" />
            <path className="edge edge-astra" d="M811 283C866 250 912 244 963 254" />

            <path className="edge edge-orchard" d="M620 590C707 604 775 628 838 664" />
            <path className="edge edge-docs" d="M620 590C533 614 468 648 409 698" />
          </g>

          <g className="branch-junctions">
            <circle cx="604" cy="608" r="4" />
            <circle cx="603" cy="458" r="4" />
            <circle cx="414" cy="319" r="4" />
            <circle cx="617" cy="420" r="4" />
            <circle cx="811" cy="283" r="4" />
          </g>

          <g className="roots">
            <path d="M574 741C524 750 475 770 431 803M614 741C665 755 713 775 756 807M596 746C579 769 568 791 562 813" />
          </g>

          <g className="pot">
            <path d="M460 743H735L720 775H477Z" />
            <path d="M483 774H714L686 816H511Z" />
          </g>
        </svg>

        <ProjectNode />
        <MainBranchNode />
        <ClaudeNode />
        <ProcessNode />
        <TestNode />
        <ReviewWorktreeNode />
        <AstraNode />
        <OrchardCluster />
        <DocsCluster />
        <GithubStrip />
      </div>
    </div>
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

    const setLength = (name: string, value: number, unit: 'vw' | 'vh') => {
      root.style.setProperty(name, value.toFixed(3) + unit)
    }

    const render = () => {
      frame = 0

      const maxScroll = Math.max(1, root.scrollHeight - window.innerHeight)
      const progress = clamp((window.scrollY - root.offsetTop) / maxScroll)
      const mobile = window.innerWidth < 768

      const settle = range(progress, 0.035, 0.15)
      const approach = range(progress, 0.14, 0.28)
      const claude = band(progress, 0.27, 0.33, 0.43, 0.49)
      const runtime = band(progress, 0.41, 0.47, 0.57, 0.63)
      const parallel = range(progress, 0.55, 0.67)
      const astra = band(progress, 0.67, 0.72, 0.80, 0.84)
      const projects = range(progress, 0.79, 0.89)
      const finalView = range(progress, 0.90, 0.96)
      const install = range(progress, 0.968, 0.995)

      let cameraX = lerp(18, 0, settle)
      let cameraY = approach * 1.5 + claude * 2.5 - runtime * 2.2 - astra * 3.2
      let cameraScale =
        lerp(0.80, 0.94, settle) +
        approach * 0.04 +
        claude * 0.10 +
        runtime * 0.13 -
        parallel * 0.05 +
        astra * 0.07 -
        projects * 0.12

      if (progress >= 0.15) {
        cameraX += -claude * 1.25 + runtime * 1.1 + astra * 1.8 - projects * 0.7
      }

      if (finalView > 0) {
        cameraX = lerp(cameraX, 0, finalView)
        cameraY = lerp(cameraY, 0, finalView)
        cameraScale = lerp(cameraScale, 0.78, finalView)
      }

      if (mobile) {
        cameraX = 0
        cameraY = lerp(8, 0, settle) + claude * 1.5 - runtime * 1.5 - astra * 1
        cameraScale =
          lerp(0.62, 0.72, settle) +
          claude * 0.04 +
          runtime * 0.05 -
          projects * 0.03

        if (finalView > 0) {
          cameraY = lerp(cameraY, 0, finalView)
          cameraScale = lerp(cameraScale, 0.64, finalView)
        }
      }

      const reducedMode = reduced.matches
      if (reducedMode) {
        cameraX = 0
        cameraY = 0
        cameraScale = mobile ? 0.64 : 0.78
      }

      const treeOpacity = reducedMode
        ? 0.78
        : clamp(0.82 - Math.max(claude, runtime, astra) * 0.22 + finalView * 0.18)

      const branchOpacity = reducedMode ? 0.88 : clamp(0.34 + approach * 0.42 + parallel * 0.18 + finalView * 0.12)

      const mainOpacity = reducedMode ? 0.9 : clamp(0.16 + approach * 0.38 + Math.max(claude, runtime, parallel) * 0.54 + projects * 0.18)
      const claudeOpacity = reducedMode ? 0.84 : clamp(0.10 + claude * 0.90 + runtime * 0.56 + parallel * 0.62 + astra * 0.42 + projects * 0.35)
      const processOpacity = reducedMode ? 0.84 : clamp(0.08 + runtime * 0.92 + parallel * 0.68 + astra * 0.36 + projects * 0.34)
      const testsOpacity = reducedMode ? 0.72 : clamp(0.06 + parallel * 0.58 + astra * 0.30 + projects * 0.38)
      const reviewOpacity = reducedMode ? 0.78 : clamp(0.08 + parallel * 0.24 + astra * 0.90 + projects * 0.45)
      const astraOpacity = reducedMode ? 0.78 : clamp(0.06 + parallel * 0.20 + astra * 0.94 + projects * 0.44)
      const clusterOpacity = reducedMode ? 0.66 : clamp(projects * 0.88 + finalView * 0.12)
      const githubOpacity = reducedMode ? 0.58 : clamp(astra * 0.44 + projects * 0.76)

      setNumber('--progress', progress)
      setLength('--camera-x', cameraX, 'vw')
      setLength('--camera-y', cameraY, 'vh')
      setNumber('--camera-scale', cameraScale)
      setNumber('--scene-opacity', 1 - install)
      setNumber('--tree-opacity', treeOpacity)
      setNumber('--branch-opacity', branchOpacity)
      setNumber('--approach', reducedMode ? 0.7 : approach)
      setNumber('--claude-focus', reducedMode ? 0.22 : claude)
      setNumber('--runtime-focus', reducedMode ? 0.22 : runtime)
      setNumber('--parallel', reducedMode ? 0.82 : parallel)
      setNumber('--astra-focus', reducedMode ? 0.22 : astra)
      setNumber('--projects', reducedMode ? 0.72 : projects)
      setNumber('--final-view', reducedMode ? 0.78 : finalView)
      setNumber('--main-opacity', mainOpacity)
      setNumber('--claude-opacity', claudeOpacity)
      setNumber('--process-opacity', processOpacity)
      setNumber('--tests-opacity', testsOpacity)
      setNumber('--review-opacity', reviewOpacity)
      setNumber('--astra-opacity', astraOpacity)
      setNumber('--cluster-opacity', clusterOpacity)
      setNumber('--github-opacity', githubOpacity)
      setNumber('--main-scale', 0.92 + Math.max(approach, claude, runtime) * 0.08)
      setNumber('--claude-scale', 0.90 + claude * 0.10 - runtime * 0.04)
      setNumber('--process-scale', 0.90 + runtime * 0.10)
      setNumber('--review-scale', 0.92 + astra * 0.08)
      setNumber('--astra-scale', 0.90 + astra * 0.10)
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
      <BonsaiScene />

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
            <div className="hero-kicker"><StatusDot /> local workspace / parallel branches</div>
            <h1>Run every branch at once.</h1>
            <p>
              One local workspace for worktrees, AI agents, processes, pull requests,
              and projects that keep running while you move between them.
            </p>
            <div className="hero-actions">
              <a className="button-primary" href="/app">Open /app <span>→</span></a>
              <a className="button-secondary" href="#install">Install Bonsai</a>
            </div>
          </div>
        </section>

        <section className="story-moment approach-moment" id="product">
          <div className="story-sticky">
            <Story
              eyebrow="01 / ONE WORKSPACE"
              title={<>A branch is a workspace,<br />not a tab.</>}
            >
              Each worktree keeps its own files, Git state, agents, and processes.
              The bonsai is the graph: every branch stays attached to the same local core.
            </Story>
          </div>
        </section>

        <section className="story-moment claude-moment" id="worktrees">
          <div className="story-sticky">
            <Story
              eyebrow="02 / AGENT RUNNING"
              title={<>Claude keeps coding.<br /><em>You keep moving.</em></>}
            >
              Start an agent inside an isolated worktree. Its task, model, runtime,
              and test progress stay attached to that branch while the rest of the tree remains alive.
            </Story>
          </div>
        </section>

        <section className="story-moment runtime-moment">
          <div className="story-sticky">
            <Story
              eyebrow="03 / PROCESS RUNNING"
              title={<>The same branch<br /><em>runs its own stack.</em></>}
            >
              Claude can keep working while <code>npm run dev</code> runs beside it.
              Bonsai exposes the detected local URL so the running app is one click away.
            </Story>
          </div>
        </section>

        <section className="story-moment parallel-moment">
          <div className="story-sticky story-sticky-center">
            <Story
              eyebrow="04 / PARALLEL BY DEFAULT"
              title={<>Not switching tasks.<br /><em>Running them together.</em></>}
              align="center"
            >
              Agent, dev server, tests, and background work remain visible at once.
              The camera changes focus; the work does not stop.
            </Story>
          </div>
        </section>

        <section className="story-moment review-moment">
          <div className="story-sticky story-sticky-right">
            <Story
              eyebrow="05 / REVIEW BRANCH"
              title={<>Astra reviews the PR.<br /><em>Claude is still running.</em></>}
              align="right"
            >
              Review lives on another worktree with its own agent and CI state.
              Earlier branches shrink into quiet live states instead of disappearing.
            </Story>
          </div>
        </section>

        <section className="story-moment projects-moment">
          <div className="story-sticky">
            <Story
              eyebrow="06 / MULTIPLE PROJECTS"
              title={<>One browser.<br /><em>Several living trees.</em></>}
            >
              Pull back to supervise Bonsai, an API, and a docs site together.
              GitHub updates land inside the same workspace rather than becoming another dashboard.
            </Story>
          </div>
        </section>

        <section className="story-moment final-moment">
          <div className="story-sticky story-sticky-center final-copy">
            <Story
              eyebrow="BONSAI / APP"
              title={<>Everything that&apos;s running.<br /><em>One place to see it.</em></>}
              align="center"
            >
              Web for the overview. TUI for keyboard-first focus. CLI for automation.
              Every surface shares the same local worktree model.
            </Story>
          </div>
        </section>

        <section className="install-moment" id="install">
          <div className="install-panel">
            <div className="install-kicker"><StatusDot /> local first / no Go required</div>
            <h2>Start growing branches.</h2>
            <p>Install the release binary, start the local server, then open the web client.</p>

            <div className="platform-switch" role="group" aria-label="Operating system">
              <button
                type="button"
                className={platform === 'unix' ? 'selected' : ''}
                onClick={() => setPlatform('unix')}
              >
                Linux / macOS
              </button>
              <button
                type="button"
                className={platform === 'windows' ? 'selected' : ''}
                onClick={() => setPlatform('windows')}
              >
                Windows
              </button>
            </div>

            <div className="install-command">
              <span>❯</span>
              <code>{INSTALL_COMMANDS[platform]}</code>
              <button type="button" onClick={copyInstall}>{copied ? 'Copied' : 'Copy'}</button>
            </div>

            <div className="serve-command">
              <span>then</span>
              <code>❯ bonsai serve</code>
            </div>

            <div className="install-actions">
              <a className="button-primary" href="/app">Open /app <span>→</span></a>
              <a className="button-secondary" href="https://github.com/Tiago-0liveira/bonsai">GitHub ↗</a>
            </div>
          </div>
        </section>
      </main>

      <footer>
        <span>Bonsai</span>
        <span>worktrees · agents · processes · pull requests · projects</span>
        <a href="https://github.com/Tiago-0liveira/bonsai">GitHub ↗</a>
      </footer>
    </div>
  )
}

export default ParallaxLanding
