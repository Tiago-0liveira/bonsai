import { useEffect, useRef, useState, type ReactNode } from 'react'
import './parallax.css'

type Camera = { x: number; y: number; s: number; r: number }

const CAMERA: Camera[] = [
  { x: 0, y: 2, s: .9, r: 0 },
  { x: 31, y: 10, s: 1.72, r: -1.2 },
  { x: -30, y: 8, s: 1.92, r: 1.1 },
  { x: 32, y: -8, s: 2.08, r: -.6 },
  { x: -34, y: -4, s: 2.12, r: .8 },
  { x: -22, y: -20, s: 2.34, r: 1.5 },
  { x: 0, y: -3, s: 1.04, r: 0 },
]

const clamp = (v: number) => Math.max(0, Math.min(1, v))
const lerp = (a: number, b: number, t: number) => a + (b - a) * t
const smooth = (t: number) => t * t * (3 - 2 * t)

const chapters = [
  {
    eyebrow: 'SPROUT / WORKTREES',
    title: <>Branch without<br/><em>breaking flow.</em></>,
    body: <>Create an isolated worktree from a new branch, an existing branch, or a GitHub PR. No stash dance. No directory roulette.</>,
    telemetry: <><div className="terminal"><b>❯</b><code>bonsai create feat/runtime</code></div><div className="grid"><span>branch</span><b>feat/runtime</b><span>state</span><b className="ok">READY</b><span>base</span><b>origin/main</b></div></>,
  },
  {
    eyebrow: 'GRAFT / STATE TRANSFER',
    title: <>Carry the state.<br/><em>Not the mess.</em></>,
    body: <>Copy the files a branch actually needs, then let lifecycle hooks prepare or tear down its environment automatically.</>,
    telemetry: <><div className="pipe"><span>.env</span><i/><b>copied</b></div><div className="pipe"><span>npm install</span><i/><b>hooked</b></div><div className="pipe"><span>.bonsai.yaml</span><i/><b>live</b></div></>,
  },
  {
    eyebrow: 'CULTIVATE / PROCESSES',
    title: <>Every branch can<br/><em>run its own world.</em></>,
    body: <>Launch project commands as background processes per worktree. Bonsai keeps many alive at once and surfaces detected localhost URLs.</>,
    telemetry: <><div className="proc"><i/><b>web</b><code>:5173</code><em>LIVE</em></div><div className="proc"><i/><b>api</b><code>:7001</code><em>RUN</em></div><div className="proc"><i className="warm"/><b>tests</b><code>42/42</code><em>PASS</em></div></>,
  },
  {
    eyebrow: 'INSPECT / GIT + PR',
    title: <>Read the branch<br/><em>before it bites.</em></>,
    body: <>Ahead/behind, dirty files, diffs, stashes, disk usage, Actions checks, PR detail and review state — in one branch context.</>,
    telemetry: <><div className="metrics"><span>↑ 4</span><span>↓ 1</span><span>● 3</span><span>? 1</span><b>#42</b><em>CI ✓</em></div><div className="diff add">+ isolated process registry</div><div className="diff del">− shared port assumption</div></>,
  },
  {
    eyebrow: 'PRUNE / CLEAN TEARDOWN',
    title: <>Ship it.<br/><em>Then disappear it.</em></>,
    body: <>Prune previews the teardown pipeline before it touches anything. Merge is opt-in; destructive steps stay explicit.</>,
    telemetry: <><div className="prune"><span>merge PR #42</span><b>→</b><span>delete hook</span><b>→</b><span>worktree</span><b>→</b><span>branch</span></div><small>LOCAL TEARDOWN / PREVIEWED</small></>,
    warm: true,
  },
  {
    eyebrow: 'ONE SYSTEM / TWO SURFACES',
    title: <>TUI when focused.<br/><em>CLI when scripted.</em></>,
    body: <>The same worktree core works interactively or as plain commands you can compose into scripts, terminals, and agent workflows.</>,
    telemetry: <div className="dual"><div><small>TUI</small><b>tab · n · s · x</b><span>stay on the keyboard</span></div><div><small>CLI</small><b>bonsai list</b><span>compose everything</span></div></div>,
  },
]

function Brand() {
  return <svg viewBox="0 0 28 28" aria-hidden="true"><path d="M14 23V10M14 14l-5-4M14 17l6-5M14 10l3-4M9 10H5M20 12h4"/><path d="M6 7h6v5H6zM16 4h5v5h-5zM19 10h6v5h-6zM3 9h5v5H3z"/></svg>
}

function Tree() {
  return (
    <div className="world" aria-hidden="true">
      <div className="grid-bg"/><div className="haze haze-a"/><div className="haze haze-b"/>
      <div className="reticle"><span/><span/></div>
      <div className="camera" data-camera>
        <svg className="tree" viewBox="0 0 1200 820">
          <defs>
            <filter id="glow" x="-70%" y="-70%" width="240%" height="240%"><feGaussianBlur stdDeviation="7" result="b"/><feMerge><feMergeNode in="b"/><feMergeNode in="SourceGraphic"/></feMerge></filter>
            <linearGradient id="trunk" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stopColor="#3f322b"/><stop offset=".55" stopColor="#6a4b39"/><stop offset="1" stopColor="#291d17"/></linearGradient>
          </defs>
          <g className="rings"><circle cx="600" cy="408" r="318"/><circle cx="600" cy="408" r="250"/><circle cx="600" cy="408" r="176"/><path d="M600 44V772M236 408H964"/></g>
          <g className="ghosts"><path d="M602 662C552 592 526 548 516 500C506 454 472 425 415 407C351 387 308 345 275 296"/><path d="M610 616C641 545 685 502 739 474C799 443 824 401 839 344"/><path d="M579 565C529 520 483 494 421 491C356 488 315 515 266 548"/><path d="M624 538C672 497 710 456 726 402C742 348 781 301 846 264"/><path d="M569 489C527 437 499 382 507 326C514 276 479 234 423 207"/><path d="M633 478C681 433 711 382 710 324C708 270 743 226 802 196"/></g>
          <g className="trunk"><path className="shadow" d="M575 714C581 640 572 595 552 540C530 477 545 411 579 360C615 305 626 242 610 176"/><path className="core" d="M604 716C604 640 593 590 578 536C560 470 573 414 606 359C640 302 651 240 632 173"/></g>
          <Branch n={1} path="M590 565C538 517 486 489 421 491C354 492 315 517 260 552" twig="M419 491C384 457 348 441 303 439" chipX={205} chipY={566} label="01 / SPROUT" sub="isolated worktree" leaves={[[219,526,37],[247,547,52],[279,420,47],[309,441,62],[348,472,42]]}/>
          <Branch n={2} path="M624 538C672 497 710 456 726 402C742 348 781 301 846 264" twig="M726 402C773 392 815 401 859 429" chipX={836} chipY={284} label="02 / GRAFT" sub="copy state + hooks" leaves={[[817,240,48],[855,259,65],[835,412,50],[876,433,72],[769,335,44]]}/>
          <Branch n={3} path="M569 489C527 437 499 382 507 326C514 276 479 234 423 207" twig="M507 326C461 315 417 325 374 354" chipX={270} chipY={148} label="03 / CULTIVATE" sub="processes + live URLs" leaves={[[365,187,47],[407,207,68],[334,341,57],[383,360,62],[453,270,43]]}/>
          <Branch n={4} path="M633 478C681 433 711 382 710 324C708 270 743 226 802 196" twig="M710 324C756 314 802 325 853 361" chipX={818} chipY={126} label="04 / INSPECT" sub="git + CI + PR" leaves={[[767,174,49],[810,194,67],[815,345,53],[858,365,70],[731,257,45]]}/>
          <Branch n={5} path="M610 616C641 545 685 502 739 474C799 443 824 401 839 344" twig="M739 474C789 482 831 513 867 560" chipX={848} chipY={584} label="05 / PRUNE" sub="clean teardown" leaves={[[825,319,48],[862,340,62],[832,548,50],[871,568,70],[775,456,43]]}/>
          <g className="roots"><path d="M603 706C536 716 477 737 427 775M603 706C674 714 740 738 793 776M601 717C585 742 575 764 568 792M604 717C622 744 636 766 648 792"/><g className="chip root-chip" transform="translate(503 733)"><rect width="204" height="56" rx="4"/><text x="14" y="21">06 / ONE SYSTEM</text><text className="sub" x="14" y="40">TUI ↔ CLI</text></g></g>
        </svg>
        <div className="depth d1">root://worktrees</div><div className="depth d2">branch isolation 100%</div>
      </div>
      <div className="scan"/>
    </div>
  )
}

function Branch({ n, path, twig, chipX, chipY, label, sub, leaves }: { n: number; path: string; twig: string; chipX: number; chipY: number; label: string; sub: string; leaves: number[][] }) {
  return <g className={'branch b' + n}><path d={path}/><path d={twig}/><g className="leaves">{leaves.map((l, i) => <rect key={i} x={l[0]} y={l[1]} width={l[2]} height={10} rx={2}/>)}</g><g className="chip" transform={'translate(' + chipX + ' ' + chipY + ')'}><rect width="205" height="56" rx="4"/><text x="14" y="21">{label}</text><text className="sub" x="14" y="40">{sub}</text></g></g>
}

function Scene({ i, side, children }: { i: number; side: 'left' | 'right'; children: ReactNode }) {
  return <section id={i === 6 ? 'interfaces' : undefined} className={'scene scene-' + side} data-scene data-i={i}><div className="sticky"><article className="card">{children}</article></div></section>
}

export function ParallaxLanding() {
  const rootRef = useRef<HTMLDivElement>(null)
  const [copied, setCopied] = useState(false)

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
      const a = scenes.map(s => s.offsetTop + s.offsetHeight * .5)
      let from = 0
      while (from < a.length - 1 && pos >= a[from + 1]) from++
      const to = Math.min(from + 1, a.length - 1)
      const raw = from === to ? 0 : clamp((pos - a[from]) / Math.max(1, a[to] - a[from]))
      const t = smooth(raw)
      const c0 = CAMERA[Math.min(from, CAMERA.length - 1)]
      const c1 = CAMERA[Math.min(to, CAMERA.length - 1)]
      const c = reduced.matches ? CAMERA[0] : { x: lerp(c0.x,c1.x,t), y: lerp(c0.y,c1.y,t), s: lerp(c0.s,c1.s,t), r: lerp(c0.r,c1.r,t) }
      camera.style.setProperty('--x', c.x + 'vw')
      camera.style.setProperty('--y', c.y + 'vh')
      camera.style.setProperty('--s', String(c.s))
      camera.style.setProperty('--r', c.r + 'deg')
      const next = raw < .5 ? from : to
      if (next !== active) {
        active = next
        root.dataset.stage = String(active)
        scenes.forEach((s, i) => s.classList.toggle('active', i === active))
      }
      const journey = clamp(window.scrollY / Math.max(1, document.documentElement.scrollHeight - window.innerHeight))
      root.style.setProperty('--journey', String(journey))
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
      await navigator.clipboard.writeText('go install github.com/Tiago-0liveira/bonsai@latest')
      setCopied(true)
      setTimeout(() => setCopied(false), 1400)
    } catch { setCopied(false) }
  }

  return (
    <div className="parallax-landing" ref={rootRef} data-stage="0">
      <Tree/>
      <nav className="topbar">
        <a className="brand" href="#top"><span><Brand/></span>BONSAI</a>
        <div className="navlinks"><a className="selected" href="#top">Showcase</a><a href="#capabilities">Capabilities</a><a href="#interfaces">TUI / CLI</a><a href="#install">Install</a></div>
        <a className="github" href="https://github.com/Tiago-0liveira/bonsai" target="_blank" rel="noreferrer">GitHub ↗</a>
      </nav>
      <div className="meter" aria-hidden="true"><i><b/></i><span>depth / 06</span></div>

      <main>
        <section id="top" className="scene hero active" data-scene data-i="0"><div className="sticky"><div className="hero-copy"><div className="status"><i/><b>BRANCH ACTIVE</b><span>worktrees in parallel</span></div><p className="eyebrow">[ GIT-WORKTREE-CULTIVATOR ] — ZERO-OVERHEAD CONCURRENCY</p><h1>Grow branches.<br/><em>Prune friction.</em></h1><p className="lede">Bonsai turns git worktrees into a keyboard-first operating system for parallel development.</p><div className="pills"><span>Bare repo stays clean</span><span>Each branch runs alone</span><span>Prune when shipped</span></div><small className="scrollcue">↓ descend into the tree</small></div></div></section>
        <div id="capabilities"/>
        {chapters.map((c, index) => <Scene key={c.eyebrow} i={index + 1} side={index % 2 ? 'right' : 'left'}><div className="rule"><span>{String(index + 1).padStart(2,'0')}</span><i/></div><p className="eyebrow">[ {c.eyebrow} ]</p><h2>{c.title}</h2><div className="body">{c.body}</div><div className={'telemetry' + (c.warm ? ' warmbox' : '')}>{c.telemetry}</div></Scene>)}
        <section id="install" className="install"><div className="install-panel"><p className="eyebrow">[ ROOT CULTIVATION ]</p><h2>Six branches deep.<br/><em>One command away.</em></h2><p>Run Bonsai inside any git repository and start cultivating worktrees from the terminal.</p><div className="install-command"><span>$</span><code>go install github.com/Tiago-0liveira/bonsai@latest</code><button onClick={copy}>{copied ? 'copied' : 'copy'}</button></div><div className="actions"><a className="primary" href="https://github.com/Tiago-0liveira/bonsai" target="_blank" rel="noreferrer">View source ↗</a><a className="secondary" href="/app">Open web canvas</a></div></div></section>
      </main>
      <footer><span>bonsai</span><span>MIT</span><a href="https://github.com/Tiago-0liveira/bonsai/tree/main/docs" target="_blank" rel="noreferrer">docs ↗</a><i/><span>cultivate branches, not friction.</span></footer>
    </div>
  )
}

export default ParallaxLanding
