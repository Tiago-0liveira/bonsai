import { useEffect, useRef, useState, type ReactNode } from 'react'
import './parallax.css'

type Camera = { x: number; y: number; s: number; r: number }
type InstallPlatform = 'unix' | 'windows'

const CAMERA: Camera[] = [
  // Start with the whole bonsai parked safely to the right of the hero copy.
  { x: 34, y: 1, s: 0.72, r: 0 },
  // Ease into the whole workspace before visiting individual branches.
  { x: 4, y: 1, s: 0.96, r: 0 },
  // The branch tour mostly travels vertically through the tree. Horizontal
  // movement stays within a few viewport units so the tree never pendulums.
  { x: -3, y: 10, s: 1.52, r: 0 },
  { x: -4, y: 2, s: 1.50, r: 0 },
  { x: -3, y: -7, s: 1.46, r: 0 },
  { x: 3, y: -11, s: 1.42, r: 0 },
  { x: 0, y: -6, s: 1.04, r: 0 },
]

const INSTALL_COMMANDS: Record<InstallPlatform, string> = {
  unix: 'curl -fsSL https://raw.githubusercontent.com/Tiago-0liveira/bonsai/main/install.sh | bash',
  windows: 'irm https://raw.githubusercontent.com/Tiago-0liveira/bonsai/main/install.ps1 | iex',
}

const clamp = (v: number) => Math.max(0, Math.min(1, v))
const lerp = (a: number, b: number, t: number) => a + (b - a) * t
const smoother = (t: number) => t * t * t * (t * (t * 6 - 15) + 10)

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

function BranchTask({
  n,
  x,
  y,
  title,
  detail,
  state,
  warm = false,
}: {
  n: number
  x: number
  y: number
  title: string
  detail: string
  state: string
  warm?: boolean
}) {
  return (
    <g className={'branch-task bt' + n + (warm ? ' warm' : '')} transform={'translate(' + x + ' ' + y + ')'}>
      <rect className="branch-task-shell" width="194" height="60" rx="7"/>
      <path className="branch-task-stem" d="M8 30H-22"/>
      <circle className="branch-task-dot" cx="15" cy="16" r="4"/>
      <text className="branch-task-title" x="27" y="20">{title}</text>
      <text className="branch-task-detail" x="14" y="38">{detail}</text>
      <rect className="branch-task-track" x="14" y="49" width="106" height="2" rx="1"/>
      <rect className="branch-task-progress" x="14" y="49" width="68" height="2" rx="1"/>
      <text className="branch-task-state" x="179" y="52" textAnchor="end">{state}</text>
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
      <ellipse cx="-82" cy="8" rx="54" ry="22"/>
      <ellipse cx="-38" cy="-8" rx="67" ry="28"/>
      <ellipse cx="18" cy="-3" rx="73" ry="30"/>
      <ellipse cx="72" cy="8" rx="59" ry="25"/>
      <ellipse cx="108" cy="18" rx="34" ry="18"/>
      <path className="leaf-lines" d="M-119 7h73M-74-11h94M-26 11h112M31-8h96M50 23h83"/>
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
            <linearGradient id="trunkFill" x1=".1" y1=".05" x2=".9" y2=".95">
              <stop offset="0" stopColor="#936b4d"/>
              <stop offset=".3" stopColor="#72503b"/>
              <stop offset=".63" stopColor="#503528"/>
              <stop offset="1" stopColor="#25160f"/>
            </linearGradient>
            <linearGradient id="branchStroke" x1="0" y1="0" x2="1" y2="1">
              <stop offset="0" stopColor="#825f48"/>
              <stop offset=".52" stopColor="#5f4232"/>
              <stop offset="1" stopColor="#38261d"/>
            </linearGradient>
          </defs>

          <g className="rings">
            <circle cx="602" cy="406" r="326"/>
            <circle cx="602" cy="406" r="250"/>
            <circle cx="602" cy="406" r="176"/>
            <path d="M602 42V775M235 406H971"/>
          </g>

          <path className="bonsai-shadow" d="M526 742C544 704 538 669 516 629C493 586 499 544 532 505C569 461 577 428 557 390C536 349 543 311 577 277C607 247 620 219 608 188C597 160 600 136 619 115C632 100 650 98 662 109C675 120 671 140 657 157C644 173 646 191 655 210C673 247 662 286 627 322C598 352 595 379 612 412C634 454 623 498 587 541C557 576 554 607 570 643C590 687 582 718 566 746Z"/>
          <path className="trunk-silhouette" d="M538 738C555 701 550 668 529 628C508 588 512 550 543 513C578 471 588 433 569 395C549 355 555 319 588 287C618 258 630 226 618 194C607 166 611 142 628 123C639 110 652 107 661 115C670 123 667 139 654 154C641 170 642 190 651 209C668 245 658 281 624 316C592 349 589 379 607 414C628 456 617 495 582 537C551 574 549 607 565 642C584 684 577 714 562 741Z"/>

          <path className="deadwood" d="M555 697C567 663 565 633 551 603C539 578 542 552 557 530M577 501C594 476 600 451 593 426M586 388C577 359 581 335 599 312M614 276C626 253 632 227 626 204"/>
          <g className="bark-lines">
            <path d="M546 710C559 677 558 650 546 620M540 583C536 554 544 527 559 505M576 474C588 448 590 425 584 401M582 363C584 339 595 319 608 301M622 264C631 239 631 217 624 195"/>
            <path d="M566 715C579 684 578 658 568 631M563 594C561 565 569 542 582 520M594 489C604 465 607 441 601 419M600 381C603 354 614 335 625 317"/>
          </g>

          <g className="primary-branches">
            <path className="branch-stroke b1" d="M554 614C497 600 451 573 404 552C359 532 314 537 268 565"/>
            <path className="twig b1" d="M420 558C382 573 350 594 321 621"/>
            <path className="branch-stroke b2" d="M557 515C504 505 462 478 421 441C381 405 331 400 279 416"/>
            <path className="twig b2" d="M424 442C389 433 355 439 322 459"/>
            <path className="branch-stroke b3" d="M573 395C522 384 481 354 440 322C400 291 354 290 307 306"/>
            <path className="twig b3" d="M441 322C406 309 375 312 342 330"/>

            <path className="branch-stroke b4" d="M568 565C637 555 694 532 751 495C803 461 858 459 919 477"/>
            <path className="twig b4" d="M749 496C795 502 833 519 870 546"/>
            <path className="branch-stroke b5" d="M594 460C660 445 715 413 763 376C812 338 865 333 927 346"/>
            <path className="twig b5" d="M763 376C810 382 850 399 888 424"/>
            <path className="branch-stroke b6" d="M608 337C667 318 716 283 756 247C800 209 850 200 908 211"/>
            <path className="twig b6" d="M757 247C801 251 839 266 876 290"/>
            <path className="branch-stroke apex" d="M606 292C619 261 622 231 614 203C607 178 611 153 627 132"/>
          </g>

          <g className="fine-twigs">
            <path d="M347 541C324 526 300 520 271 523M365 558C337 569 316 585 298 605M388 542C365 521 340 510 311 508"/>
            <path d="M354 405C330 390 307 385 277 388M378 425C349 435 325 451 305 474M402 420C385 395 366 378 340 366"/>
            <path d="M381 292C359 278 336 273 307 278M403 311C376 322 354 337 334 358M426 304C410 281 389 264 366 253"/>
            <path d="M812 465C840 455 866 454 897 460M821 501C850 511 875 526 898 548M858 452C879 438 903 432 932 434"/>
            <path d="M823 344C851 335 877 334 907 340M832 381C863 391 887 405 910 427M870 330C892 316 916 311 943 314"/>
            <path d="M818 218C843 207 867 203 896 207M829 254C858 259 883 270 907 290M862 205C882 189 904 182 932 183"/>
            <path d="M616 215C634 197 648 188 669 181M613 179C603 160 600 143 604 127"/>
          </g>

          <g className="foliage">
            <FoliagePad className="p1" cx={302} cy={548} sx={1.03} sy={0.9}/>
            <FoliagePad className="p2" cx={296} cy={405} sx={0.98} sy={0.86}/>
            <FoliagePad className="p3" cx={322} cy={292} sx={0.88} sy={0.8}/>
            <FoliagePad className="p4" cx={896} cy={468} sx={1.05} sy={0.9}/>
            <FoliagePad className="p5" cx={906} cy={337} sx={1.02} sy={0.88}/>
            <FoliagePad className="p6" cx={884} cy={208} sx={0.95} sy={0.82}/>
            <FoliagePad className="p7" cx={627} cy={126} sx={0.72} sy={0.72}/>
          </g>

          <g className="roots">
            <path d="M548 716C505 720 461 738 421 768M559 717C616 719 673 739 726 771M545 722C520 741 502 764 490 790M570 722C595 742 615 766 629 793"/>
          </g>
          <g className="pot">
            <path className="pot-rim" d="M420 724H770L752 753H440Z"/>
            <path className="pot-body" d="M446 752H746L719 806H480Z"/>
            <path className="pot-line" d="M477 772H716M496 790H699"/>
          </g>
          <g className="root-chip" transform="translate(511 744)">
            <rect width="174" height="47" rx="5"/>
            <text x="13" y="20">LOCAL CORE</text>
            <text className="sub" x="13" y="37">WEB · TUI · CLI</text>
          </g>

          <g className="branch-jobs">
            <BranchTask n={1} x={235} y={250} title="feat/auth-passkeys" detail="Claude · Sonnet 4.5" state="CODING"/>
            <BranchTask n={2} x={780} y={176} title="pr/128-review" detail="ChatGPT Astra · security" state="REVIEW" warm/>
            <BranchTask n={3} x={795} y={327} title="feat/live-preview" detail="web :5173 · api :7001" state="3 PROC"/>
            <BranchTask n={4} x={798} y={492} title="feat/payments" detail="tests · watch · 42/42" state="PASS"/>
            <BranchTask n={5} x={225} y={500} title="orchard-api/cache" detail="Claude · Haiku · project 02" state="RUN"/>
            <BranchTask n={6} x={470} y={594} title="github/events" detail="PR · checks · relay" state="LIVE"/>
          </g>
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
      // Give every chapter a short "settled" window at each end, then use
      // smootherstep for the actual camera travel. This keeps the branch in
      // focus long enough to read and removes the pendulum-like movement.
      const travel = clamp((raw - 0.12) / 0.76)
      const t = smoother(travel)
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
      root.style.setProperty('--reticle-left', 50 + c.x * 0.94 + '%')

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
              <small className="scrollcue">↓ scroll — the camera moves into the tree, one worktree at a time</small>
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
