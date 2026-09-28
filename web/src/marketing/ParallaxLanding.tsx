import { useEffect, useRef, useState, type ReactNode } from 'react'
import './parallax.css'

type Camera = { x: number; y: number; s: number; r: number }
type InstallPlatform = 'unix' | 'windows'

const CAMERA: Camera[] = [
  { x: 24, y: 2, s: .88, r: 0 },
  { x: 2, y: 3, s: 1.24, r: -.2 },
  { x: 29, y: 14, s: 1.98, r: 1.1 },
  { x: -30, y: 14, s: 2.12, r: -1.1 },
  { x: -31, y: -7, s: 2.08, r: -.7 },
  { x: 30, y: -7, s: 2.08, r: .8 },
  { x: 0, y: -18, s: 1.18, r: 0 },
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
    title: <>One browser.<br/><em>Every active branch.</em></>,
    body: <>Open <strong>/app</strong>, connect to the Bonsai server running on your machine, and keep projects, worktrees, agents, files, processes and PR context visible without flattening everything into terminal tabs.</>,
    telemetry: <div className="project-stack">
      <div><b>bonsai</b><span>11 worktrees</span><em>2 agents</em></div>
      <div><b>orchard-api</b><span>7 worktrees</span><em>3 processes</em></div>
      <div><b>docs-site</b><span>4 worktrees</span><em>1 PR</em></div>
    </div>,
  },
  {
    eyebrow: 'WORKTREE / AGENT',
    title: <>Give a branch<br/><em>its own operator.</em></>,
    body: <>An isolated worktree can stay busy while you move elsewhere. Keep implementation running in one branch and return when the agent, tests, or process state changes.</>,
    telemetry: <div className="agent-readout"><div className="agent-head"><i/><b>feat/auth-passkeys</b><span>RUNNING</span></div><strong>Claude · Sonnet 4.5</strong><small>Implement WebAuthn registration flow</small><div className="agent-progress"><i/><span>tests 28/32</span></div></div>,
  },
  {
    eyebrow: 'PARALLEL REVIEW / PR',
    title: <>Review here.<br/><em>Build somewhere else.</em></>,
    body: <>Keep a review worktree separate from feature work. Diff, GitHub checks and PR context stay attached to that branch while another worktree continues implementing.</>,
    telemetry: <div className="review-readout"><div><b>pr/128-review</b><span>#128</span></div><strong>ChatGPT Astra · PR review</strong><small>4 comments · 2 suggested fixes</small><div className="review-checks"><span>CI ✓</span><span>diff +214 −39</span><span>reviewing</span></div></div>,
  },
  {
    eyebrow: 'WORKTREE / LIVE RUNTIME',
    title: <>Each branch can<br/><em>run a whole stack.</em></>,
    body: <>Start discovered project commands or aliases as background processes per worktree. Bonsai surfaces detected localhost URLs so the browser becomes the control plane for live environments.</>,
    telemetry: <div className="runtime-readout">
      <div><i/><b>web</b><code>http://localhost:5173</code><em>LIVE</em></div>
      <div><i/><b>api</b><code>http://localhost:7001</code><em>RUN</em></div>
      <div><i className="warm"/><b>tests</b><code>watch · 42/42</code><em>PASS</em></div>
    </div>,
  },
  {
    eyebrow: 'GITHUB / REALTIME EVENTS',
    title: <>GitHub moves.<br/><em>Your canvas follows.</em></>,
    body: <>The relay streams normalized GitHub events to the browser. Those events invalidate local state, then <strong>/app</strong> refreshes the canonical branch, check and PR data from your local Bonsai server.</>,
    telemetry: <div className="event-stream">
      <div><time>18:42:07</time><b>check_run</b><span>success</span></div>
      <div><time>18:42:11</time><b>pull_request</b><span>#128 synchronize</span></div>
      <div><time>18:42:12</time><b>local refresh</b><span className="ok">canonical ✓</span></div>
    </div>,
  },
  {
    eyebrow: 'ONE LOCAL CORE / THREE SURFACES',
    title: <>Web for overview.<br/><em>TUI for focus.</em></>,
    body: <>The web client, TUI and CLI all sit on the same worktree model. Stay visual while many branches work in parallel, drop into the TUI for deep keyboard focus, or script the CLI when automation wins.</>,
    telemetry: <div className="surface-readout"><div><small>WEB</small><b>/app</b><span>canvas · agents · files · PRs</span></div><div><small>TUI</small><b>bonsai</b><span>keyboard-first branch control</span></div><div><small>CLI</small><b>bonsai create</b><span>scriptable primitives</span></div></div>,
  },
]

function Brand() {
  return <svg viewBox="0 0 28 28" aria-hidden="true"><path d="M14 23V10M14 14l-5-4M14 17l6-5M14 10l3-4M9 10H5M20 12h4"/><path d="M6 7h6v5H6zM16 4h5v5h-5zM19 10h6v5h-6zM3 9h5v5H3z"/></svg>
}

function WorkNode({ n, x, y, title, meta, state, warm = false }: { n: number; x: number; y: number; title: string; meta: string; state: string; warm?: boolean }) {
  return <g className={'worknode wn' + n} transform={'translate(' + x + ' ' + y + ')'}>
    <rect className="worknode-shell" width="252" height="76" rx="5"/>
    <circle className={warm ? 'worknode-dot warm' : 'worknode-dot'} cx="18" cy="18" r="4"/>
    <text className="worknode-title" x="30" y="22">{title}</text>
    <text className="worknode-meta" x="15" y="45">{meta}</text>
    <text className={warm ? 'worknode-state warm' : 'worknode-state'} x="237" y="61" textAnchor="end">{state}</text>
  </g>
}

function Tree() {
  return (
    <div className="world" aria-hidden="true">
      <div className="grid-bg"/><div className="haze haze-a"/><div className="haze haze-b"/>
      <div className="reticle"><span/><span/></div>
      <div className="camera" data-camera>
        <svg className="tree" viewBox="0 0 1200 820">
          <defs>
            <filter id="glow" x="-70%" y="-70%" width="240%" height="240%"><feGaussianBlur stdDeviation="6" result="b"/><feMerge><feMergeNode in="b"/><feMergeNode in="SourceGraphic"/></feMerge></filter>
            <linearGradient id="trunkFill" x1=".15" y1=".1" x2=".85" y2=".95"><stop offset="0" stopColor="#7d5b45"/><stop offset=".48" stopColor="#594033"/><stop offset="1" stopColor="#2b1b14"/></linearGradient>
            <linearGradient id="branchStroke" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stopColor="#75533f"/><stop offset="1" stopColor="#3a281f"/></linearGradient>
          </defs>

          <g className="rings"><circle cx="602" cy="401" r="318"/><circle cx="602" cy="401" r="246"/><circle cx="602" cy="401" r="173"/><path d="M602 42V774M239 401H965"/></g>

          <g className="bonsai-shadow">
            <path d="M548 738C561 689 573 637 565 589C558 545 531 506 533 459C536 409 569 374 586 333C609 278 604 225 584 172C575 148 584 130 600 126C617 122 630 137 635 160C650 224 660 279 640 341C625 388 594 426 593 466C592 507 619 544 624 588C630 643 613 694 608 740Z"/>
          </g>
          <path className="trunk-silhouette" d="M561 738C571 688 583 638 575 588C568 541 543 505 545 460C547 414 579 377 597 335C621 279 616 222 596 170C589 152 594 142 604 139C616 135 623 147 627 165C641 227 649 279 630 338C614 389 583 425 582 466C580 510 608 545 613 590C619 641 603 693 597 739Z"/>

          <g className="primary-branches">
            <path className="branch-stroke b1" d="M588 575C536 532 488 508 429 506C366 505 319 531 267 569"/>
            <path className="twig b1" d="M431 506C389 479 347 468 299 474"/>
            <path className="branch-stroke b2" d="M611 536C665 500 707 457 729 405C750 355 787 314 850 286"/>
            <path className="twig b2" d="M728 405C780 396 826 407 873 438"/>
            <path className="branch-stroke b3" d="M575 480C535 437 514 391 520 340C527 289 490 251 429 226"/>
            <path className="twig b3" d="M521 339C470 329 427 341 381 374"/>
            <path className="branch-stroke b4" d="M623 477C672 434 700 390 701 340C702 289 741 247 804 217"/>
            <path className="twig b4" d="M702 339C751 327 804 341 857 373"/>
            <path className="branch-stroke b5" d="M607 623C656 574 699 535 748 511C802 485 836 448 852 397"/>
            <path className="twig b5" d="M747 511C801 518 844 545 885 589"/>
          </g>

          <g className="fine-twigs">
            <path d="M348 505C328 491 310 482 284 479M370 518C338 529 319 543 300 558"/>
            <path d="M794 335C818 322 839 317 867 320M812 420C840 430 858 445 878 467"/>
            <path d="M473 299C449 281 427 274 399 274M450 347C424 355 401 369 380 391"/>
            <path d="M767 268C790 249 812 240 842 238M767 349C796 355 820 368 842 389"/>
            <path d="M803 483C827 472 849 468 879 471M803 538C831 550 851 566 871 587"/>
          </g>

          <g className="foliage">
            <g className="pad pad-a"><path d="M224 448C244 415 280 402 318 410C344 382 399 385 420 419C455 419 479 443 474 468C454 488 417 491 382 485C343 500 295 493 272 474C249 474 232 465 224 448Z"/><path className="leaf-lines" d="M257 449h77m-51-22h98m-69 43h126"/></g>
            <g className="pad pad-b"><path d="M300 535C321 509 356 501 387 508C414 488 458 493 475 520C507 524 522 545 511 565C486 580 451 579 422 571C386 588 344 580 329 558C311 556 301 548 300 535Z"/><path className="leaf-lines" d="M326 540h82m-51-19h91m-61 39h88"/></g>
            <g className="pad pad-c"><path d="M342 207C361 177 399 166 431 175C457 151 505 157 519 188C548 194 559 217 544 237C520 250 489 247 466 241C429 259 385 250 370 229C353 227 344 219 342 207Z"/><path className="leaf-lines" d="M370 210h79m-48-19h86m-61 38h92"/></g>
            <g className="pad pad-d"><path d="M331 326C349 299 381 289 414 297C438 275 480 282 495 309C523 313 537 333 524 353C500 367 470 365 444 359C410 374 370 368 354 348C340 347 332 339 331 326Z"/><path className="leaf-lines" d="M356 331h74m-43-19h82m-58 38h87"/></g>
            <g className="pad pad-e"><path d="M741 191C761 158 801 149 836 159C864 136 914 144 928 177C960 181 975 205 960 227C934 242 898 239 871 231C833 250 787 244 769 219C753 217 743 208 741 191Z"/><path className="leaf-lines" d="M770 198h84m-49-21h91m-63 42h98"/></g>
            <g className="pad pad-f"><path d="M789 304C810 276 846 267 880 276C906 255 950 262 965 290C995 295 1008 317 993 337C969 351 937 349 910 342C874 359 833 352 815 331C800 329 791 320 789 304Z"/><path className="leaf-lines" d="M816 310h82m-48-20h91m-63 40h93"/></g>
            <g className="pad pad-g"><path d="M807 431C827 404 860 395 893 402C917 382 960 388 978 414C1007 416 1022 438 1008 458C984 474 951 472 922 465C888 481 848 474 830 453C815 451 807 443 807 431Z"/><path className="leaf-lines" d="M834 437h78m-45-20h88m-61 40h92"/></g>
            <g className="pad pad-h"><path d="M822 540C844 511 879 503 913 512C939 489 985 496 1002 526C1031 530 1044 552 1029 572C1004 588 971 586 943 579C905 596 864 589 846 566C831 564 823 555 822 540Z"/><path className="leaf-lines" d="M849 547h80m-48-21h92m-62 41h96"/></g>
          </g>

          <g className="roots"><path d="M585 724C525 731 468 748 414 781M589 724C657 729 724 750 786 782M581 729C558 748 543 768 530 794M596 729C617 751 632 772 643 795"/><g className="root-chip" transform="translate(505 736)"><rect width="196" height="54" rx="4"/><text x="14" y="21">LOCAL BONSAI CORE</text><text className="sub" x="14" y="40">WEB · TUI · CLI</text></g></g>

          <WorkNode n={1} x={250} y={560} title="project / bonsai" meta="11 worktrees · 2 agents" state="CONNECTED"/>
          <WorkNode n={2} x={238} y={132} title="feat/auth-passkeys" meta="Claude · Sonnet 4.5" state="RUNNING"/>
          <WorkNode n={3} x={804} y={126} title="pr/128-review" meta="ChatGPT Astra · review" state="PR #128"/>
          <WorkNode n={4} x={846} y={340} title="feat/live-preview" meta="web :5173 · api :7001" state="3 PROC"/>
          <WorkNode n={5} x={840} y={566} title="github/events" meta="Actions · PR · SSE" state="LIVE"/>
        </svg>
        <div className="depth d1">local://worktree-graph</div><div className="depth d2">branch isolation / realtime state</div>
      </div>
      <div className="scan"/>
    </div>
  )
}

function Scene({ i, children }: { i: number; children: ReactNode }) {
  return <section id={i === 1 ? 'web-client' : i === 6 ? 'surfaces' : undefined} className="scene" data-scene data-i={i}><div className="sticky"><article className="chapter-panel">{children}</article></div></section>
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
      const pos = window.scrollY + window.innerHeight * .48
      const anchors = scenes.map(scene => scene.offsetTop + scene.offsetHeight * .5)
      let from = 0
      while (from < anchors.length - 1 && pos >= anchors[from + 1]) from++
      const to = Math.min(from + 1, anchors.length - 1)
      const raw = from === to ? 0 : clamp((pos - anchors[from]) / Math.max(1, anchors[to] - anchors[from]))
      const t = smooth(raw)
      const c0 = CAMERA[Math.min(from, CAMERA.length - 1)]
      const c1 = CAMERA[Math.min(to, CAMERA.length - 1)]
      const c = reduced.matches ? CAMERA[0] : {
        x: lerp(c0.x, c1.x, t),
        y: lerp(c0.y, c1.y, t),
        s: lerp(c0.s, c1.s, t),
        r: lerp(c0.r, c1.r, t),
      }
      camera.style.setProperty('--x', c.x + 'vw')
      camera.style.setProperty('--y', c.y + 'vh')
      camera.style.setProperty('--s', String(c.s))
      camera.style.setProperty('--r', c.r + 'deg')
      const next = raw < .5 ? from : to
      if (next !== active) {
        active = next
        root.dataset.stage = String(active)
        scenes.forEach((scene, i) => scene.classList.toggle('active', i === active))
      }
      const journey = clamp(window.scrollY / Math.max(1, document.documentElement.scrollHeight - window.innerHeight))
      root.style.setProperty('--meter-height', (journey * 100) + '%')
      root.style.setProperty('--scan-top', (12 + journey * 76) + '%')
    }

    const onScroll = () => { if (!raf) raf = requestAnimationFrame(render) }
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
        <a className="nav-island nav-brand" href="#top" aria-label="Bonsai home"><span><Brand/></span><b>BONSAI</b></a>
        <nav className="nav-island nav-center" aria-label="Landing navigation">
          <a href="#top"><small>01</small><span>Showcase</span></a>
          <a href="#web-client"><small>02</small><span>Web client</span></a>
          <a href="#surfaces"><small>03</small><span>Surfaces</span></a>
          <a href="#install"><small>04</small><span>Install</span></a>
        </nav>
        <a className="nav-island nav-github" href="https://github.com/Tiago-0liveira/bonsai" target="_blank" rel="noreferrer"><span>GitHub</span><b>↗</b></a>
      </header>

      <div className="meter" aria-hidden="true"><i><b/></i><span>depth / 06</span></div>

      <main>
        <section id="top" className="scene hero active" data-scene data-i="0">
          <div className="sticky">
            <div className="hero-copy">
              <div className="status"><i/><b>LOCAL CORE ONLINE</b><span>web · tui · cli</span></div>
              <p className="eyebrow">[ PARALLEL WORKTREE CONTROL PLANE ] — LOCAL-FIRST</p>
              <h1>Grow branches.<br/><em>Watch them work.</em></h1>
              <h2 className="parallel-heading">The browser is your canopy view.</h2>
              <p className="lede">Connect <strong>/app</strong> to the Bonsai server on your machine and watch projects, worktrees, agents, processes and GitHub state evolve in parallel.</p>
              <div className="pills"><span>multi-project canvas</span><span>GitHub realtime</span><span>local filesystem + git</span></div>
              <small className="scrollcue">↓ descend into the tree</small>
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
            <p className="eyebrow">[ ROOT CULTIVATION ]</p>
            <h2>Install the binary.<br/><em>No Go toolchain required.</em></h2>
            <p>The official scripts download the latest release, verify its SHA256 checksum, and install Bonsai for you. Start <code>bonsai serve</code>, then open the web client.</p>

            <div className="platform-switch" role="tablist" aria-label="Install platform">
              <button className={platform === 'unix' ? 'active' : ''} onClick={() => setPlatform('unix')} role="tab" aria-selected={platform === 'unix'}>Linux / macOS</button>
              <button className={platform === 'windows' ? 'active' : ''} onClick={() => setPlatform('windows')} role="tab" aria-selected={platform === 'windows'}>Windows PowerShell</button>
            </div>

            <div className="install-command"><span>❯</span><code>{INSTALL_COMMANDS[platform]}</code><button onClick={copy}>{copied ? 'copied' : 'copy'}</button></div>
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
