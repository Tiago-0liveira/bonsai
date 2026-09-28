import { useEffect, useRef } from 'react'
import './landing.css'

type WorktreeCardProps = {
  branch: string
  tag?: string
  ahead?: number
  behind?: number
  changed?: number
  pr?: string
  ci?: 'running' | 'passing' | 'failed'
  className?: string
}

function WorktreeCard({ branch, tag = 'feature', ahead = 0, behind = 0, changed = 0, pr, ci, className = '' }: WorktreeCardProps) {
  return (
    <article className={`marketing-node marketing-worktree ${className}`}>
      <div className="node-title-row"><strong>{branch}</strong><span className="node-status-dot" /></div>
      <div className="node-meta"><span>{tag}</span><span>↑ {ahead}</span><span>↓ {behind}</span></div>
      <div className="node-foot"><span>{changed} changed</span>{pr && <span>{pr}</span>}{ci && <span className={`ci-${ci}`}>CI {ci}</span>}</div>
    </article>
  )
}

function AgentCard({ provider, task, state = 'running', runtime = '04:18', className = '' }: { provider: string; task: string; state?: 'running' | 'finished' | 'reviewing' | 'failed'; runtime?: string; className?: string }) {
  return (
    <article className={`marketing-node marketing-agent ${className}`}>
      <div className="agent-top"><strong>{provider}</strong><span>{runtime}</span></div>
      <p>{task}</p>
      <div className={`agent-state agent-${state}`}><span />{state}</div>
    </article>
  )
}

function Topology({ variant = 'split' }: { variant?: 'split' | 'nested' | 'architecture' | 'lifecycle' }) {
  if (variant === 'architecture') {
    return (
      <svg className="topology-svg architecture-lines" viewBox="0 0 720 520" aria-hidden="true">
        <path d="M360 48 V130 M360 188 V270 M360 328 V408" />
        <path d="M360 408 H178 V458 M360 408 H300 V458 M360 408 H420 V458 M360 408 H542 V458" />
      </svg>
    )
  }
  if (variant === 'nested') {
    return <svg className="topology-svg" viewBox="0 0 900 620" aria-hidden="true"><path d="M120 100 H275 V230 H430 V360 H585 V490 H760" /></svg>
  }
  if (variant === 'lifecycle') {
    return <svg className="topology-svg" viewBox="0 0 900 540" aria-hidden="true"><path d="M110 270 H245 C295 270 295 150 350 150 H540 C595 150 595 270 650 270 H790" /></svg>
  }
  return (
    <svg className="topology-svg" viewBox="0 0 900 620" aria-hidden="true">
      <path d="M110 310 H265" />
      <path d="M265 310 C330 310 320 120 390 120 H770" />
      <path d="M265 310 H770" />
      <path d="M265 310 C330 310 320 500 390 500 H770" />
    </svg>
  )
}

function ScrollScene({ id, eyebrow, title, copy, children, className = '', compact = false }: { id: string; eyebrow?: string; title: React.ReactNode; copy?: React.ReactNode; children: React.ReactNode; className?: string; compact?: boolean }) {
  return (
    <section id={id} className={`landing-scene ${compact ? 'landing-scene-compact' : ''} ${className}`} data-scroll-scene>
      <div className="landing-stage">
        <div className="scene-copy">
          {eyebrow && <p className="scene-eyebrow">{eyebrow}</p>}
          <h2>{title}</h2>
          {copy && <div className="scene-body">{copy}</div>}
        </div>
        <div className="scene-visual">{children}</div>
      </div>
    </section>
  )
}

function LandingNav() {
  return (
    <nav className="landing-nav" aria-label="Primary">
      <a className="landing-brand" href="#top" aria-label="Bonsai home"><span className="brand-mark" aria-hidden="true">B</span><span>bonsai</span></a>
      <div className="landing-nav-links">
        <a href="https://github.com/Tiago-0liveira/bonsai">GitHub</a>
        <a href="https://github.com/Tiago-0liveira/bonsai/tree/main/docs">Docs</a>
        <a className="nav-cta" href="/app">Open Bonsai</a>
      </div>
    </nav>
  )
}

export function LandingPage() {
  const rootRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const root = rootRef.current
    if (!root) return
    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)')
    const scenes = Array.from(root.querySelectorAll<HTMLElement>('[data-scroll-scene]'))
    const observer = new IntersectionObserver((entries) => {
      for (const entry of entries) entry.target.classList.toggle('is-visible', entry.isIntersecting)
    }, { rootMargin: '-18% 0px -18% 0px', threshold: 0.08 })
    scenes.forEach(scene => observer.observe(scene))

    if (reduced.matches) return () => observer.disconnect()

    const motions = Array.from(root.querySelectorAll<HTMLElement>('[data-parallax]')).map(element => {
      const depth = Number(element.dataset.parallax ?? '0.4')
      const distance = 86 * depth
      const animation = element.animate([
        { transform: `translate3d(0, ${distance}px, 0)` },
        { transform: `translate3d(0, ${-distance}px, 0)` },
      ], { duration: 1000, fill: 'both', easing: 'linear' })
      animation.pause()
      return { element, scene: element.closest<HTMLElement>('[data-scroll-scene]'), animation }
    })

    let queued = false
    const render = () => {
      queued = false
      const height = window.innerHeight || 1
      for (const item of motions) {
        const scene = item.scene
        if (!scene) continue
        const rect = scene.getBoundingClientRect()
        const travel = rect.height + height
        const progress = Math.max(0, Math.min(1, (height - rect.top) / travel))
        item.animation.currentTime = progress * 1000
      }
    }
    const queue = () => {
      if (queued) return
      queued = true
      requestAnimationFrame(render)
    }
    window.addEventListener('scroll', queue, { passive: true })
    window.addEventListener('resize', queue)
    render()

    return () => {
      observer.disconnect()
      window.removeEventListener('scroll', queue)
      window.removeEventListener('resize', queue)
      motions.forEach(item => item.animation.cancel())
    }
  }, [])

  return (
    <div ref={rootRef} className="landing-page" id="top">
      <LandingNav />

      <header className="hero-scene landing-scene" data-scroll-scene>
        <div className="landing-stage hero-stage">
          <div className="hero-copy" data-parallax="0.15">
            <p className="scene-eyebrow">local parallel development</p>
            <h1>Work in parallel.<br />Keep the repository coherent.</h1>
            <p>Bonsai gives every branch, worktree and agent its own place — so multiple streams of work can move at once without becoming one pile of context.</p>
            <div className="hero-actions"><a className="button-primary" href="/app">Open Bonsai</a><a className="button-secondary" href="https://github.com/Tiago-0liveira/bonsai">View source</a></div>
          </div>
          <div className="hero-graph graph-field" aria-label="Repository graph showing main splitting into three worktrees">
            <Topology />
            <div className="main-node marketing-node" data-parallax="0.15"><span>main</span><small>clean · synced</small></div>
            <WorktreeCard branch="feat/web-client" ahead={7} changed={3} pr="PR #26" ci="running" className="worktree-a" />
            <WorktreeCard branch="fix/daemon-cleanup" tag="bug" ahead={3} behind={1} changed={2} pr="PR #23" ci="failed" className="worktree-b" />
            <WorktreeCard branch="feat/release-flow" tag="feature" ahead={2} behind={0} pr="PR #22" ci="passing" className="worktree-c" />
            <div className="graph-caption" data-parallax="1">parallel work has a shape</div>
          </div>
        </div>
      </header>

      <ScrollScene id="problem" eyebrow="the coordination cost" title={<>Parallel work usually collapses back into one terminal.</>} copy={<><p>A second branch is easy.</p><p>Five branches, three agents, two review passes, a dev server, a failed check and a half-finished rebase are not.</p><strong>The problem is remembering where everything belongs.</strong></>}>
        <div className="graph-field chaos-field">
          <Topology />
          <WorktreeCard branch="feat/web-client" ahead={7} changed={3} className="worktree-a faint" />
          <WorktreeCard branch="fix/daemon-cleanup" tag="bug" changed={2} className="worktree-b faint" />
          <WorktreeCard branch="feat/release-flow" className="worktree-c faint" />
          <div className="ghost-terminal ghost-one" data-parallax="1">$ pnpm dev<br /><span>localhost:5173</span></div>
          <div className="ghost-terminal ghost-two" data-parallax="0.82">agent · review/auth<br /><span>waiting for tests…</span></div>
          <div className="ghost-terminal ghost-three" data-parallax="0.68">PR #26 · checks<br /><span>integration running</span></div>
          <div className="stable-label">context is noisy · structure is stable</div>
        </div>
      </ScrollScene>

      <ScrollScene id="worktrees" eyebrow="worktrees as operating lanes" title={<>One piece of work.<br />One place for its context.</>} copy={<><p>A Bonsai worktree is an operating lane for a change. The branch, files, running processes, agents, pull request and merge target stay attached to the same piece of work.</p><strong>Switch work. Don't reconstruct context.</strong></>}>
        <div className="focus-orbit">
          <WorktreeCard branch="feat/web-client" ahead={7} changed={3} pr="PR #26" ci="running" className="focus-worktree" />
          <span className="orbit-pill orbit-one" data-parallax="0.35">Git status · 3 dirty</span>
          <span className="orbit-pill orbit-two" data-parallax="0.65">merge target → main</span>
          <span className="orbit-pill orbit-three" data-parallax="0.48">terminal · dev server</span>
          <span className="orbit-pill orbit-four" data-parallax="0.9">PR #26 · Open</span>
          <span className="orbit-pill orbit-five" data-parallax="0.72">CI · running</span>
          <span className="orbit-pill orbit-six" data-parallax="0.55">3 changed files</span>
        </div>
      </ScrollScene>

      <ScrollScene id="agents" eyebrow="branch-local agents" title="Give every agent its own ground to work on." copy={<p>Start an agent on a specific worktree. Implementation can move on one branch while tests run on another and a review agent inspects a third — without sharing one mutable checkout.</p>}>
        <div className="agent-forest">
          <div className="agent-column column-a"><WorktreeCard branch="feat/web-client" changed={3} pr="PR #26" /><div className="agent-tether" /><AgentCard provider="Codex" task="Implementation · connect hosted client to local Bonsai API" /><AgentCard provider="Claude" task="Review · local capability flow" state="reviewing" runtime="02:41" /></div>
          <div className="agent-column column-b" data-parallax="0.35"><WorktreeCard branch="fix/daemon-cleanup" tag="bug" changed={2} /><div className="agent-tether" /><AgentCard provider="Codex" task="Testing · lifecycle cleanup" runtime="06:03" /></div>
          <div className="agent-column column-c" data-parallax="0.55"><WorktreeCard branch="feat/release-flow" /><div className="agent-tether" /><AgentCard provider="Gemini" task="Research · release artifact flow" state="finished" runtime="08:17" /></div>
        </div>
      </ScrollScene>

      <ScrollScene id="progress" eyebrow="independent progress" title={<>The branch can keep working<br />while you move somewhere else.</>} copy={<p>Bonsai keeps the work visible after your attention moves. Running agents, finished sessions and branch status stay attached to the branch that produced them.</p>}>
        <div className="progress-grid">
          <div className="progress-lane selected"><span className="lane-label">feat/web-client</span><AgentCard provider="Codex" task="editing → tests → finished" state="finished" runtime="07:44" /><div className="history-shelf">History · implementation · 07:44</div></div>
          <div className="progress-lane"><span className="lane-label">fix/daemon-cleanup</span><AgentCard provider="Codex" task="running → failed check → debugging" state="failed" runtime="11:20" /><div className="terminal-strip">go test ./... · 1 failed</div></div>
          <div className="progress-lane"><span className="lane-label">feat/release-flow</span><AgentCard provider="Claude" task="reviewing → comment ready" state="reviewing" runtime="03:12" /><div className="history-shelf">review comment ready</div></div>
        </div>
      </ScrollScene>

      <ScrollScene id="topology" eyebrow="merge topology" title="Parallel doesn't have to mean flat." copy={<p>Feature work can branch from feature work. Bonsai shows the merge path instead of pretending every branch points directly at main.</p>}>
        <div className="nested-graph graph-field">
          <Topology variant="nested" />
          <div className="nested-card nested-main marketing-node">main</div>
          <WorktreeCard branch="feat/web" className="nested-one" />
          <WorktreeCard branch="feat/web-auth" changed={2} pr="PR #31" className="nested-two" />
          <WorktreeCard branch="review/auth-cleanup" tag="review" changed={1} pr="PR #34" ci="passing" className="nested-three" />
          <span className="merge-note merge-note-one">merge target</span><span className="merge-note merge-note-two">merge target</span>
        </div>
      </ScrollScene>

      <ScrollScene id="inspector" eyebrow="focused detail" title="Know where attention is needed." copy={<p>Bonsai keeps branch health next to the work itself. Dirty trees, failed checks, behind branches, open pull requests and running agents are visible without reconstructing state from separate tools.</p>}>
        <div className="inspector-demo">
          <div className="inspector-graph"><Topology /><WorktreeCard branch="feat/web-client" ahead={7} changed={3} pr="PR #26" ci="running" className="inspector-selected" /></div>
          <aside className="marketing-inspector" data-parallax="0.35"><p className="inspector-label">INSPECTOR</p><h3>feat/web-client</h3><div className="inspector-stats"><span><strong>3</strong> uncommitted</span><span><strong>7</strong> ahead</span><span><strong>0</strong> behind</span></div><div className="inspector-row">PR #26 <span>Open</span></div><div className="checks"><span className="check-pass">✓ typecheck</span><span className="check-running">● integration</span><span className="check-fail">× windows</span></div><div className="inspector-row">Agents on this branch <strong>2</strong></div></aside>
        </div>
      </ScrollScene>

      <ScrollScene id="terminal" eyebrow="real tools, attached to context" title={<>Visual when you need orientation.<br />Terminal when you need control.</>} copy={<p>Open an agent terminal. Inspect files and diffs. Run the actual Git operation. Bonsai adds structure around the tools you already trust.</p>}>
        <div className="terminal-scene"><WorktreeCard branch="feat/web-client" ahead={7} changed={3} pr="PR #26" className="terminal-worktree" /><div className="dock-labels"><span>Files</span><span>Diff</span><span>Pull Request</span><span>Checks</span><span>Logs</span></div><div className="terminal-panel" data-parallax="0.3"><div className="terminal-chrome"><span /><span /><span /><strong>feat/web-client · shell</strong></div><pre><span>$ go test ./...</span>{'\n'}✓ internal/server/localapi{`\n`}✓ web client contract{`\n\n`}<span>$ git status</span>{`\n`}On branch feat/web-client{`\n`}Changes not staged for commit: 3</pre></div></div>
      </ScrollScene>

      <ScrollScene id="interfaces" eyebrow="one local system" title={<>The web app is not the product boundary.<br />Bonsai is.</>} copy={<><p>Web when you want the map.<br />TUI when you live in the terminal.<br />CLI when you want automation.</p></>}>
        <div className="interfaces-diagram"><div className="interface-frame web-frame" data-parallax="0.2"><span>WEB</span><div className="mini-map"><i /><i /><i /></div></div><div className="interface-frame tui-frame" data-parallax="0.45"><span>TUI</span><pre>bonsai{`\n`}├ feat/web{`\n`}└ fix/daemon</pre></div><div className="interface-frame cli-frame" data-parallax="0.65"><span>CLI</span><pre>$ bonsai wt list{`\n`}$ bonsai agent start</pre></div><div className="backend-root">Bonsai local backend</div></div>
      </ScrollScene>

      <ScrollScene id="local" eyebrow="local by design" title="Your work stays where the work is." copy={<p>Bonsai's hosted service exists to relay authenticated GitHub events. Project files, terminal sessions, agent execution and local Git operations are handled by Bonsai on your machine.</p>} className="quiet-scene">
        <div className="architecture-diagram"><Topology variant="architecture" /><div className="arch-node arch-github">GitHub<small>event metadata</small></div><div className="arch-node arch-relay">Bonsai relay<small>configured HTTPS origin</small></div><div className="arch-node arch-browser">browser<small>hosted frontend</small></div><div className="arch-node arch-local">Bonsai on your machine<small>127.0.0.1:7001</small></div><span className="arch-leaf leaf-files">files</span><span className="arch-leaf leaf-git">git</span><span className="arch-leaf leaf-agents">agents</span><span className="arch-leaf leaf-terminals">terminals</span></div>
      </ScrollScene>

      <ScrollScene id="lifecycle" eyebrow="the lifecycle" title={<>Grow.<br />Review.<br />Merge.<br />Prune.</>} copy={<p>A branch appears, gets its own worker and context, becomes a pull request, passes checks, merges into its target, and leaves the tree clean again.</p>}>
        <div className="lifecycle-graph graph-field"><Topology variant="lifecycle" /><div className="lifecycle-main marketing-node">main <span>advances</span></div><WorktreeCard branch="feat/local-web" ahead={5} changed={2} pr="PR #42" ci="passing" className="lifecycle-worktree" /><AgentCard provider="Codex" task="Implementation" state="finished" className="lifecycle-agent" /><div className="lifecycle-merge">merged ✓</div></div>
      </ScrollScene>

      <section className="final-cta" data-scroll-scene>
        <div className="final-inner"><p className="scene-eyebrow">bonsai</p><h2>Keep more work moving.<br />Keep less of it in your head.</h2><div className="hero-actions"><a className="button-primary" href="/app">Open Bonsai</a><a className="button-secondary" href="https://github.com/Tiago-0liveira/bonsai">View on GitHub</a></div><div className="install-line"><span>Install locally.</span><code>bonsai serve</code><span>Open the web workspace.</span></div></div>
      </section>

      <footer className="landing-footer"><a href="/app">Web</a><span>TUI</span><span>CLI</span><a href="https://github.com/Tiago-0liveira/bonsai/tree/main/docs">Docs</a><a href="https://github.com/Tiago-0liveira/bonsai">GitHub</a></footer>
    </div>
  )
}

export default LandingPage
