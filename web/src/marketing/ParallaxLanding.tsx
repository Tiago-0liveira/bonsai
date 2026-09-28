import { useEffect, useRef, useState, type ReactNode } from 'react'
import './parallax.css'

type Camera = { x: number; y: number; s: number; r: number }
type InstallPlatform = 'unix' | 'windows'

const CAMERA: Camera[] = [
  { x: 30, y: 0, s: 0.78, r: 0 },
  { x: 1, y: 2, s: 1.08, r: -0.15 },
  { x: 26, y: 13, s: 2.05, r: 0.65 },
  { x: -27, y: 15, s: 2.12, r: -0.55 },
  { x: -28, y: 0, s: 2.08, r: 0.5 },
  { x: -27, y: -14, s: 2.02, r: 0.35 },
  { x: 0, y: -11, s: 1.16, r: 0 },
]

const INSTALL_COMMANDS: Record<InstallPlatform, string> = {
  unix: 'curl -fsSL https://raw.githubusercontent.com/Tiago-0liveira/bonsai/main/install.sh | bash',
  windows: 'irm https://raw.githubusercontent.com/Tiago-0liveira/bonsai/main/install.ps1 | iex',
}

const clamp = (v: number) => Math.max(0, Math.min(1, v))
const lerp = (a: number, b: number, t: number) => a + (b - a) * t
const smooth = (t: number) => t * t * (3 - 2 * t)

const chapters = [
  {
    eyebrow: 'WEB CLIENT / PROJECT CANVAS',
    title: <>Your local workspace.<br/><em>Visible in one browser.</em></>,
    body: <>Open <strong>/app</strong> and connect it to the Bonsai server on your machine. The canvas keeps projects, branches, agents, changed files, processes, logs, PRs and checks reachable while work continues in parallel.</>,
    telemetry: (
      <div className="canvas-readout">
        <div className="canvas-tabs"><b>Canvas</b><span>Table</span><span>GitHub</span><span>Logs</span></div>
        <div className="canvas-body">
          <div className="canvas-project">
            <div className="canvas-project-head"><i/><strong>bonsai</strong><span>CI healthy</span></div>
            <div className="canvas-stats"><span><b>10</b> worktrees</span><span><b>2</b> agents</span><span><b>1</b> open PR</span></div>
            <div className="canvas-branches"><span>feat/auth-passkeys</span><span>pr/128-review</span><span>feat/live-preview</span></div>
          </div>
          <div className="canvas-inspector"><small>INSPECTOR</small><b>workspace overview</b><span>branches 11</span><span>changed files 36</span><em>GitHub realtime connected</em></div>
        </div>
      </div>
    ),
  },
  {
    eyebrow: 'WORKTREE / AGENT',
    title: <>Claude keeps coding.<br/><em>You keep moving.</em></>,
    body: <>Give an isolated branch its own agent and leave it running. The branch keeps its filesystem, process and Git state while you jump to another project or inspect a different worktree from <strong>/app</strong>.</>,
    telemetry: (
      <div className="agent-readout">
        <div className="agent-head"><i/><b>feat/auth-passkeys</b><span>RUNNING</span></div>
        <strong>Claude · Sonnet 4.5</strong>
        <small>Implement WebAuthn registration + recovery flow</small>
        <div className="agent-progress"><i/><span>tests 28 / 32</span></div>
      </div>
    ),
  },
  {
    eyebrow: 'PARALLEL REVIEW / PR',
    title: <>Astra reviews the PR.<br/><em>Another branch still builds.</em></>,
    body: <>Keep review work separate from implementation. Diff, checks, comments and PR context stay attached to the review worktree while the feature branch continues running its own agent and processes.</>,
    telemetry: (
      <div className="review-readout">
        <div><b>pr/128-review</b><span>#128</span></div>
        <strong>ChatGPT Astra · PR review</strong>
        <small>Security + regression pass on the auth changes</small>
        <div className="review-checks"><span>CI ✓</span><span>diff +214 −39</span><span>4 comments</span><span>reviewing</span></div>
      </div>
    ),
  },
  {
    eyebrow: 'WORKTREE / LIVE RUNTIME',
    title: <>Every branch can run<br/><em>its own stack.</em></>,
    body: <>Start discovered commands or aliases as background processes per worktree. Bonsai keeps each runtime attached to its branch and exposes detected localhost URLs directly in the web client.</>,
    telemetry: (
      <div className="runtime-readout">
        <div><i/><b>web</b><code>http://localhost:5173</code><em>LIVE</em></div>
        <div><i/><b>api</b><code>http://localhost:7001</code><em>RUN</em></div>
        <div><i className="warm"/><b>tests</b><code>watch · 42 / 42</code><em>PASS</em></div>
      </div>
    ),
  },
  {
    eyebrow: 'GITHUB / REALTIME EVENTS',
    title: <>GitHub changes.<br/><em>The canvas catches up.</em></>,
    body: <>The hosted relay streams normalized GitHub events to the browser. <strong>/app</strong> uses them to invalidate stale views, then refreshes canonical branch, check and PR state from your local Bonsai server.</>,
    telemetry: (
      <div className="event-stream">
        <div><time>18:42:07</time><b>check_run</b><span className="ok">success</span></div>
        <div><time>18:42:11</time><b>pull_request</b><span>#128 synchronize</span></div>
        <div><time>18:42:12</time><b>local refresh</b><span className="ok">canonical ✓</span></div>
      </div>
    ),
  },
  {
    eyebrow: 'ONE LOCAL CORE / MULTIPLE SURFACES',
    title: <>Overview in the web.<br/><em>Focus in the TUI.</em></>,
    body: <>The website, TUI and CLI share the same worktree model. Use the browser to supervise many branches and projects at once, drop into the TUI for keyboard-first depth, or automate the same primitives from the CLI.</>,
    telemetry: (
      <div className="surface-readout">
        <div><small>WEB</small><b>/app</b><span>canvas · agents · files · PRs · logs</span></div>
        <div><small>TUI</small><b>bonsai</b><span>fast keyboard-first branch control</span></div>
        <div><small>CLI</small><b>bonsai create</b><span>scriptable worktree primitives</span></div>
      </div>
    ),
  },
]

function Brand() {
  return (
    <svg viewBox="0 0 28 28" aria-hidden="true">
      <path d="M14 23V10M14 14l-5-4M14 17l6-5M14 10l3-4M9 10H5M20 12h4"/>
      <path d="M6 7h6v5H6zM16 4h5v5h-5zM19 10h6v5h-6zM3 9h5v5H3z"/>
    </svg>
  )
}

function WorkNode({
  n,
  x,
  y,
  title,
  meta,
  state,
  warm = false,
}: {
  n: number
  x: number
  y: number
  title: string
  meta: string
  state: string
  warm?: boolean
}) {
  return (
    <g className={'worknode wn' + n} transform={'translate(' + x + ' ' + y + ')'}>
      <rect className="worknode-shell" width="270" height="82" rx="7"/>
      <path className="worknode-rail" d="M1 1V81"/>
      <circle className={warm ? 'worknode-dot warm' : 'worknode-dot'} cx="19" cy="19" r="4"/>
      <text className="worknode-title" x="31" y="23">{title}</text>
      <text className="worknode-meta" x="16" y="49">{meta}</text>
      <text className={warm ? 'worknode-state warm' : 'worknode-state'} x="254" y="68" textAnchor="end">{state}</text>
    </g>
  )
}

function FoliagePad({
  className,
  cx,
  cy,
  sx = 1,
  sy = 1,
}: {
  className: string
  cx: number
  cy: number
  sx?: number
  sy?: number
}) {
  return (
    <g className={'pad ' + className} transform={'translate(' + cx + ' ' + cy + ') scale(' + sx + ' ' + sy + ')'}>
      <ellipse cx="-72" cy="8" rx="62" ry="30"/>
      <ellipse cx="-24" cy="-9" rx="72" ry="35"/>
      <ellipse cx="38" cy="2" rx="69" ry="34"/>
      <ellipse cx="85" cy="15" rx="48" ry="25"/>
      <path className="leaf-lines" d="M-116 11h78M-57-12h103M12 7h114M-12 27h94"/>
    </g>
  )
}

function Tree() {
  return (
    <div className="world" aria-hidden="true">
      <div className="grid-bg"/>
      <div className="haze haze-a"/>
      <div className="haze haze-b"/>
      <div className="reticle"><span/><span/></div>

      <div className="camera" data-camera>
        <svg className="tree" viewBox="0 0 1200 820">
          <defs>
            <filter id="glow" x="-70%" y="-70%" width="240%" height="240%">
              <feGaussianBlur stdDeviation="6" result="b"/>
              <feMerge><feMergeNode in="b"/><feMergeNode in="SourceGraphic"/></feMerge>
            </filter>
            <linearGradient id="trunkFill" x1=".15" y1=".05" x2=".86" y2=".96">
              <stop offset="0" stopColor="#8b6348"/>
              <stop offset=".38" stopColor="#684a37"/>
              <stop offset=".7" stopColor="#493126"/>
              <stop offset="1" stopColor="#261710"/>
            </linearGradient>
            <linearGradient id="branchStroke" x1="0" y1="0" x2="1" y2="1">
              <stop offset="0" stopColor="#815d46"/>
              <stop offset=".52" stopColor="#5f4333"/>
              <stop offset="1" stopColor="#36251d"/>
            </linearGradient>
          </defs>

          <g className="rings">
            <circle cx="603" cy="405" r="326"/>
            <circle cx="603" cy="405" r="251"/>
            <circle cx="603" cy="405" r="176"/>
            <path d="M603 40V777M236 405H972"/>
          </g>

          <path className="bonsai-shadow" d="M538 751C548 710 560 676 555 641C550 604 529 573 535 535C542 491 568 468 573 431C578 394 562 362 571 324C582 278 613 247 610 208C607 176 591 153 598 127C604 103 626 87 647 94C669 101 674 121 666 146C658 174 669 196 669 222C670 267 638 301 634 340C630 377 643 412 633 452C623 493 596 519 593 554C590 592 611 622 614 661C617 700 604 728 593 751Z"/>
          <path className="trunk-silhouette" d="M551 748C561 710 570 677 566 642C562 606 544 575 550 537C557 496 580 472 584 435C589 397 575 365 584 329C595 284 624 251 621 211C619 183 606 160 612 138C617 120 632 108 646 112C660 116 663 131 658 149C650 177 660 198 660 224C661 264 632 298 628 339C624 378 637 414 626 451C616 489 590 517 586 553C582 593 601 625 604 662C607 700 595 727 585 749Z"/>

          <g className="bark-lines">
            <path d="M570 704C580 671 580 634 573 602M565 566C559 530 565 497 578 468M588 433C598 404 603 374 600 345M606 311C617 282 630 258 632 229M631 195C628 169 630 147 640 125"/>
            <path d="M589 711C596 680 595 650 590 620M582 583C579 551 585 521 596 493M607 459C616 433 619 405 615 378M617 341C625 314 637 288 640 260"/>
            <path d="M556 678C550 650 550 625 554 600M552 548C550 521 557 500 570 482"/>
          </g>

          <g className="primary-branches">
            <path className="branch-stroke b1" d="M581 595C531 577 493 551 452 533C401 511 345 519 286 552"/>
            <path className="twig b1" d="M452 533C409 548 377 574 347 605"/>
            <path className="branch-stroke b2" d="M575 484C533 461 500 430 468 391C431 347 376 340 309 356"/>
            <path className="twig b2" d="M468 391C431 378 397 383 361 401"/>
            <path className="branch-stroke b3" d="M613 408C659 390 704 360 738 316C774 270 822 246 881 232"/>
            <path className="twig b3" d="M739 316C786 315 826 330 866 359"/>
            <path className="branch-stroke b4" d="M602 519C653 501 696 474 734 437C773 399 820 383 881 390"/>
            <path className="twig b4" d="M734 437C779 444 817 462 852 489"/>
            <path className="branch-stroke b5" d="M594 632C650 620 706 598 755 564C801 532 850 522 914 539"/>
            <path className="twig b5" d="M755 564C799 574 835 596 867 626"/>
            <path className="branch-stroke apex" d="M603 337C615 302 617 269 608 236C600 205 601 177 613 152"/>
          </g>

          <g className="fine-twigs">
            <path d="M349 527C326 513 302 509 273 513M366 552C337 561 317 577 298 596M390 520C370 499 346 486 318 481"/>
            <path d="M395 353C372 335 347 328 317 331M418 378C389 389 365 404 344 427M438 365C422 340 402 320 378 307"/>
            <path d="M798 266C822 250 847 243 877 245M812 305C841 306 865 316 891 333M834 238C853 220 875 209 903 205"/>
            <path d="M811 405C838 396 863 395 891 401M817 451C847 462 872 478 894 501M847 386C868 371 892 364 921 366"/>
            <path d="M842 537C869 529 895 531 924 539M837 584C868 597 894 615 916 638M879 528C901 515 925 510 953 513"/>
            <path d="M607 230C623 209 639 198 662 188M607 195C594 176 585 157 586 136"/>
          </g>

          <g className="foliage">
            <FoliagePad className="p1" cx={301} cy={523} sx={1.02} sy={0.86}/>
            <FoliagePad className="p2" cx={320} cy={343} sx={1.08} sy={0.88}/>
            <FoliagePad className="p3" cx={872} cy={224} sx={1.08} sy={0.9}/>
            <FoliagePad className="p4" cx={885} cy={386} sx={1.12} sy={0.9}/>
            <FoliagePad className="p5" cx={918} cy={536} sx={1.08} sy={0.9}/>
            <FoliagePad className="p6" cx={616} cy={142} sx={0.82} sy={0.78}/>
          </g>

          <g className="roots">
            <path d="M573 724C530 727 486 741 445 766M583 724C633 727 683 741 731 769M569 730C550 745 534 765 523 790M590 730C609 748 623 768 636 792"/>
          </g>
          <g className="pot">
            <path className="pot-rim" d="M432 725H766L748 753H451Z"/>
            <path className="pot-body" d="M457 752H742L716 806H483Z"/>
            <path className="pot-line" d="M485 771H716M501 790H700"/>
          </g>
          <g className="root-chip" transform="translate(514 741)">
            <rect width="170" height="48" rx="5"/>
            <text x="13" y="20">LOCAL CORE</text>
            <text className="sub" x="13" y="37">WEB · TUI · CLI</text>
          </g>

          <WorkNode n={1} x={112} y={598} title="bonsai / workspace" meta="11 branches · 2 agents · 36 files" state="/APP"/>
          <WorkNode n={2} x={112} y={286} title="feat/auth-passkeys" meta="Claude · Sonnet 4.5 · coding" state="RUNNING"/>
          <WorkNode n={3} x={816} y={128} title="pr/128-review" meta="ChatGPT Astra · PR review" state="4 COMMENTS" warm/>
          <WorkNode n={4} x={825} y={338} title="feat/live-preview" meta="web :5173 · api :7001 · tests" state="3 PROC"/>
          <WorkNode n={5} x={822} y={548} title="github/events" meta="Actions · PR · relay · refresh" state="REALTIME"/>
          <WorkNode n={6} x={98} y={684} title="orchard-api / feat/cache" meta="project 02 · background agent" state="PARALLEL"/>
        </svg>

        <div className="depth d1">local://worktree-graph</div>
        <div className="depth d2">branch isolation / realtime state</div>
      </div>

      <div className="scan"/>
    </div>
  )
}

function Scene({ i, children }: { i: number; children: ReactNode }) {
  const id = i === 1 ? 'web-client' : i === 2 ? 'worktrees' : i === 6 ? 'surfaces' : undefined
  return (
    <section id={id} className="scene" data-scene data-i={i}>
      <div className="sticky">
        <article className="chapter-panel">{children}</article>
      </div>
    </section>
  )
}

export function ParallaxLanding() {
  const rootRef = useRef<HTMLDivElement>(null)
  const [copied, setCopied] = useState(false)
  const [platform, setPlatform] = useState<InstallPlatform>('unix')

  useEffect(() => {
    const root = rootRef.current
    if (!root) return

    document.body.classList.add('parallax-body')
    document.documentElement.classList.add('parallax-html')

    const scenes = Array.from(root.querySelectorAll<HTMLElement>('[data-scene]'))
    const camera = root.querySelector<HTMLElement>('[data-camera]')
    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)')
    let raf = 0
    let active = -1

    const render = () => {
      raf = 0
      if (!camera) return

      const pos = window.scrollY + window.innerHeight * 0.48
      const anchors = scenes.map((scene) => scene.offsetTop + scene.offsetHeight * 0.5)
      let from = 0
      while (from < anchors.length - 1 && pos >= anchors[from + 1]) from += 1

      const to = Math.min(from + 1, anchors.length - 1)
      const raw = from === to ? 0 : clamp((pos - anchors[from]) / Math.max(1, anchors[to] - anchors[from]))
      const t = smooth(raw)
      const c0 = CAMERA[Math.min(from, CAMERA.length - 1)]
      const c1 = CAMERA[Math.min(to, CAMERA.length - 1)]
      const c = reduced.matches
        ? CAMERA[0]
        : {
            x: lerp(c0.x, c1.x, t),
            y: lerp(c0.y, c1.y, t),
            s: lerp(c0.s, c1.s, t),
            r: lerp(c0.r, c1.r, t),
          }

      camera.style.setProperty('--x', c.x + 'vw')
      camera.style.setProperty('--y', c.y + 'vh')
      camera.style.setProperty('--s', String(c.s))
      camera.style.setProperty('--r', c.r + 'deg')

      const next = raw < 0.5 ? from : to
      if (next !== active) {
        active = next
        root.dataset.stage = String(active)
        scenes.forEach((scene, i) => scene.classList.toggle('active', i === active))
      }

      const journey = clamp(window.scrollY / Math.max(1, document.documentElement.scrollHeight - window.innerHeight))
      root.style.setProperty('--meter-height', journey * 100 + '%')
      root.style.setProperty('--scan-top', 12 + journey * 76 + '%')
    }

    const onScroll = () => {
      if (!raf) raf = requestAnimationFrame(render)
    }

    render()
    addEventListener('scroll', onScroll, { passive: true })
    addEventListener('resize', onScroll)
    reduced.addEventListener('change', onScroll)

    return () => {
      if (raf) cancelAnimationFrame(raf)
      removeEventListener('scroll', onScroll)
      removeEventListener('resize', onScroll)
      reduced.removeEventListener('change', onScroll)
      document.body.classList.remove('parallax-body')
      document.documentElement.classList.remove('parallax-html')
    }
  }, [])

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(INSTALL_COMMANDS[platform])
      setCopied(true)
      setTimeout(() => setCopied(false), 1400)
    } catch {
      setCopied(false)
    }
  }

  return (
    <div className="parallax-landing" ref={rootRef} data-stage="0">
      <Tree/>

      <header className="nav-shell">
        <a className="nav-island nav-brand" href="#top" aria-label="Bonsai home">
          <span><Brand/></span><b>BONSAI</b>
        </a>

        <nav className="nav-island nav-center" aria-label="Landing navigation">
          <a href="#top"><small>01</small><span>Showcase</span></a>
          <a href="#web-client"><small>02</small><span>Web /app</span></a>
          <a href="#worktrees"><small>03</small><span>Worktrees</span></a>
          <a href="#surfaces"><small>04</small><span>Surfaces</span></a>
          <a href="#install"><small>05</small><span>Install</span></a>
        </nav>

        <a className="nav-island nav-github" href="https://github.com/Tiago-0liveira/bonsai" target="_blank" rel="noreferrer">
          <span>GitHub</span><b>↗</b>
        </a>
      </header>

      <div className="meter" aria-hidden="true"><i><b/></i><span>depth / 06</span></div>

      <main>
        <section id="top" className="scene hero active" data-scene data-i="0">
          <div className="sticky">
            <div className="hero-copy">
              <div className="status"><i/><b>LOCAL CORE ONLINE</b><span>browser connected</span></div>
              <p className="eyebrow">[ PARALLEL WORKTREE CONTROL PLANE ] — WEB + TUI + CLI</p>
              <h1>Grow branches.<br/><em>Watch them work.</em></h1>
              <h2 className="parallel-heading">Run multiple projects in parallel. <span>Supervise them from /app.</span></h2>
              <p className="lede">Bonsai connects its browser client to the server running on your machine, so every worktree can keep coding, reviewing, testing and serving while the full workspace stays accessible in one place.</p>

              <div className="hero-actions">
                <a className="hero-primary" href="/app">Open /app <span>↗</span></a>
                <a className="hero-secondary" href="#web-client">Explore the canvas <span>↓</span></a>
              </div>

              <div className="pills"><span>multi-project canvas</span><span>parallel agents</span><span>GitHub realtime</span></div>
              <small className="scrollcue">↓ scroll to enter individual worktrees</small>
            </div>
          </div>
        </section>

        {chapters.map((chapter, index) => (
          <Scene key={chapter.eyebrow} i={index + 1}>
            <div className="chapter-index"><span>{String(index + 1).padStart(2, '0')}</span><i/></div>
            <p className="eyebrow">[ {chapter.eyebrow} ]</p>
            <h2>{chapter.title}</h2>
            <div className="body">{chapter.body}</div>
            <div className="telemetry">{chapter.telemetry}</div>
          </Scene>
        ))}

        <section id="install" className="install">
          <div className="install-panel">
            <p className="eyebrow">[ INSTALL / OFFICIAL RELEASE SCRIPTS ]</p>
            <h2>Install Bonsai.<br/><em>No Go toolchain required.</em></h2>
            <p>The official install scripts download the latest release, verify its SHA256 checksum and place the binary on your machine. Then run <code>bonsai serve</code> and open the hosted web client.</p>

            <div className="platform-switch" role="tablist" aria-label="Install platform">
              <button type="button" className={platform === 'unix' ? 'active' : ''} onClick={() => setPlatform('unix')} role="tab" aria-selected={platform === 'unix'}>Linux / macOS</button>
              <button type="button" className={platform === 'windows' ? 'active' : ''} onClick={() => setPlatform('windows')} role="tab" aria-selected={platform === 'windows'}>Windows PowerShell</button>
            </div>

            <div className="install-command">
              <span>❯</span><code>{INSTALL_COMMANDS[platform]}</code>
              <button type="button" onClick={copy}>{copied ? 'copied' : 'copy'}</button>
            </div>

            <div className="after-install"><code>bonsai serve</code><span>→</span><code>/app</code><span>→</span><b>connected</b></div>
            <div className="actions"><a className="primary" href="/app">Open Bonsai</a><a className="secondary" href="https://github.com/Tiago-0liveira/bonsai" target="_blank" rel="noreferrer">View source ↗</a></div>
          </div>
        </section>
      </main>

      <footer><span>bonsai</span><span>MIT</span><a href="https://github.com/Tiago-0liveira/bonsai/tree/main/docs" target="_blank" rel="noreferrer">docs ↗</a><i/><span>parallel branches · one local core</span></footer>
    </div>
  )
}

export default ParallaxLanding
