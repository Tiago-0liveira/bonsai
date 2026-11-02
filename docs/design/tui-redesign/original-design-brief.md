# Bonsai Garden + Playground — UI/UX Design Brief for a Separate Agent

**Brief status:** Ready for design exploration; **not implementation authorization**  
**Prepared:** 2026-10-19  
**Platform:** Existing Go terminal UI (Bubble Tea + Lip Gloss); no desktop GUI rewrite  
**Repository:** [`Tiago-0liveira/bonsai`](https://github.com/Tiago-0liveira/bonsai)  
**Technical planning reference:** `bonsai-garden-implementation-plan.md`

> **Instruction to the design agent:** Inspect the repository's current **terminal UI** and produce design concepts, interaction models, annotated terminal wireframes, and a recommendation. **Do not implement, refactor, or edit application code. Do not inspect or redesign the `web/` directory.** The user will choose a design and bring it back for a separate implementation phase. Treat the technical plan as constraints, not as a locked visual specification.

---

## 1. Project context

Bonsai is a terminal-first tool for managing Git worktrees, background processes, Git/PR operations, and coding agents. It supports multiple provider accounts and profiles (Claude Code, Codex, Antigravity), model/effort/permission selection, usage visibility, attachable agent sessions, and interaction with those sessions.

The product currently emphasizes **select a worktree first**, then inspect or run actions in the contextual right pane.

The proposed evolution adds two major, independently navigable workspaces:

- **Garden:** A GitHub Issues-backed knowledge, specification, and planning environment. The user captures ideas and bugs, discusses/refines them, explicitly approves a detailed specification, obtains a read-only AI implementation plan, then approves the plan.
- **Playground:** A repository-level operational cockpit for coding agents. It aggregates active and historical sessions across worktrees, exposes what each is doing, provides terminal attachment, and shows changes, branch/worktree, PR number, CI, and review state.
- **Worktrees:** The existing worktree-centered surface remains available; it must not be sacrificed or hidden behind Garden.

**Concept shift:** Worktree-first → **intent-first**. Users should be able to select a work item, approve a plan, and launch an agent without manually creating/selecting its worktree. Worktree management remains available separately.

**Important current-code fact:** At the repository baseline used by the technical plan (`95e1020`), the TUI already has an **Agents right-pane tab** with session list, account/usage surfaces, launch dialogs, and interactive terminal attach. Playground should evolve these existing patterns, not propose a second incompatible terminal client. Check current `main` for changes before sketching.

## 2. Non-negotiable product decisions

1. **Keep terminal UI and Go:** no Rust/GPUI, Electron, browser UI, or web directory changes in this design iteration.
2. **GitHub Issues is the canonical work-item store.** One idea = one Issue throughout its life. GitHub Projects supplies saved board/table views; it is not a competing ideas database.
3. **Per-repository Garden:** any Bonsai user can enable a separate Garden on a GitHub repository. Worktrees/clones of that repo resolve to the same remote Garden; a different repo resolves to its own.
4. **Opt-in, non-destructive:** Garden only owns newly created managed issues or existing issues explicitly imported by the user. Default setup imports **zero** existing issues.
5. **One connected system with multiple views:** Drafts, Features, Bugs, Problems, Planning, Running, Review, not disconnected tables. Type and stage are independent dimensions.
6. **All items begin as Drafts.** Type (`Feature`, `Bug`, `Problem`) may be assigned at creation, changed during draft, and remains on the same issue after approval.
7. **Two approval gates before implementation:** user approves the idea/specification into an `Approved` stage (no process launch); a *separate local action* starts a read-only AI planner; user then approves the exact plan revision into `Ready`.
8. **Local execution only:** ChatGPT/mobile can create/refine/approve ideas and plans via GitHub Issues; cannot start Bonsai agents remotely in the first release. The TUI handles launching implementation and all process controls.
9. **Provider choice at local launch:** let user choose a specific available agent account/profile, model, effort, and permissions. Do not silently default to unrestricted access.
10. **Execution isolation:** implementation gets an automatically created worktree/branch. Draft PR is opened after meaningful changes are pushed; completed sessions close/release resources, retain audit/history, and lead to optional automatic review + human decision.
11. **Remember the last workspace** selected by the user; Garden first in navigation but not always forced open.
12. **Plan only, do not implement.** UI design must allow iteration before coding begins.

## 3. Primary users and jobs to be done

### Persona A — Solo developer with several repositories

"I open Bonsai inside whichever repository I'm working on, find its ideas and issues, pick one, ask for a plan, approve it, and launch an agent without handling Git plumbing myself."

### Persona B — Developer capturing ideas on a phone

"When I'm away, I open ChatGPT, discuss an idea, and keep refining the same GitHub Issue. Later, opening Bonsai in that repository shows the updated specification and approvals. I don't expect agents to start on my powered-off desktop."

### Persona C — Developer supervising concurrent agents

"I want one place to see which agents are running, what task each is implementing, what files changed, which branches and PRs exist, which CI checks failed, and whether an agent needs my input. I need to fully attach and interact with a session."

### Persona D — Contributor on an existing project

"I want to enable Garden without altering the project's backlog or importing old issues without consent. If I choose to import an existing issue, I need to see exactly what Bonsai will change."

## 4. Information architecture to explore

Propose a **top-level workspace switcher**, not an extra miniature right-pane tab.

```text
BONSAI — [Garden] [Playground] [Worktrees]                      repo: acme/my-app
```

This header is just a sketch. Explore other keyboard-first TUI designs: compact tabs, a subtle navigation rail when terminal width allows, or palette-first workspace navigation. The interface should feel like one coherent application, not three independent shells.

### Garden: content hierarchy

```text
Repository Garden
  ├── Drafts                    stage=draft, any type
  ├── Features                  type=feature, stage != draft
  ├── Bugs                      type=bug, stage != draft
  ├── Problems                  type=problem, stage != draft
  ├── Planning                  approved / planning / plan-review / ready
  ├── Running                   active implementation tasks
  ├── Review                    reviewing / human-review
  └── All Items                 user-filtered and searchable

An item detail
  ├── Overview + linked GitHub Issue
  ├── Structured specification
  ├── Decisions / discussion / open questions
  ├── Plan revisions + approval record
  ├── Related items
  └── Linked execution / sessions / PR / CI / review
```

### Playground: content hierarchy

```text
Repository-wide agent sessions
  ├── Active / Needs attention / Completed / Failed
  ├── Filter by task / agent / provider / branch
  └── Selected session
       ├── Live, attachable terminal
       ├── Changes / diff
       ├── Git branch + worktree
       ├── PR + CI checks
       ├── Related issue + approved plan
       └── Stop / reattach / view-only / take control
```

**No duplicate ownership:** The same item may appear in a Feature view and in Running/Review because the former is a type view and the latter a lifecycle view. The UI must communicate that these are *views*, not different issue records.

## 5. User journeys to design end-to-end

### Journey A — First-time setup in an unrelated repository

1. User runs `bonsai` in a Git repository.
2. Garden unavailable: offer **Set up**, **Connect existing Project**, or **Skip**.
3. Show resolved GitHub remote and account, with a confirmation if multiple remotes/fork ambiguity.
4. Create/attach a GitHub Project, configure the Garden label/field scheme; explain if a browser-assisted view/template setup is required.
5. Default **do not import existing issues**; optionally show a deliberate Import action.
6. Setup completes; the Garden is ready and empty or contains only explicitly selected issues.
7. Reopen the repository/worktree; Bonsai remembers the Garden association and the last workspace.

**Design questions:** How compact can the wizard be? Where do auth/scope errors go? What does a read-only connection look like? What if someone closes the wizard halfway through?

### Journey B — Capture and refine a draft

1. User opens Garden → Drafts → New.
2. Chooses Feature, Bug or Problem (or specifies type during writing); default Stage is always Draft.
3. Enters minimal text; issue is created remotely without requiring a full specification.
4. Opens the detail to expand current/desired behavior, requirements, acceptance criteria, constraints and open questions.
5. AI or manual edits create a visible change summary; accidental overwrites are prevented.
6. A later ChatGPT/mobile edit updates the **same Issue**. On refresh, Bonsai shows the revised content and any conflict/staleness indicator.
7. User explicitly presses **Validate idea** (approval gate 1); stage becomes **Approved**, not Planning. The same action can be recorded through ChatGPT/GitHub. No local planner starts automatically.

**Design questions:** List vs Kanban vs table default? Full-screen editor vs side inspector? How do drafts signal type while remaining in one Drafts view?

### Journey C — Plan and approve

1. User selects an **Approved** item (validated idea, no planning run yet).
2. Chooses **Request AI plan** locally. Planning is read-only against repository code and issue context.
3. A clear progress/needs-input indication appears. The planner can ask questions; no worktree or code changes are created.
4. Plan revision is displayed in a readable document with assumptions, touched areas, steps, tests, risks, and acceptance-criteria mapping.
5. User can **Request changes**, **Reject**, or **Approve plan** (approval gate 2).
6. Once approved, item appears as **Ready**, but **no coding agent starts automatically**.
7. A plan revised after approval must be clearly distinguished from the approved snapshot.

**Design questions:** How are approval gates visually unmistakable? Where do plan comparison and open questions live? How should a plan approved through GitHub/ChatGPT appear when Bonsai refreshes?

### Journey D — Launch a coding agent locally

1. User opens a Ready task; chooses **Start implementation**.
2. Dialog shows issue URL/title, exact approved plan revision, proposed branch/worktree, selected account/profile, model, effort, permissions and confirmation.
3. If the source specification changed, show an **approval stale** blocker instead of launching.
4. After confirmation Bonsai creates worktree and session and changes the item to Running.
5. A link opens the new session in Playground.
6. Draft PR appears only when meaningful pushed changes exist; display pending/unavailable PR accurately beforehand.

**Design questions:** How much configuration should the dialog show at once? How does it display provider-specific permissions safely? Can users reuse account defaults without losing visibility into them?

### Journey E — Supervise/attach multiple sessions

1. User opens Playground and sees all relevant sessions in the current repository, including manually launched/unlinked sessions.
2. Sees active, running, waiting for input, exited, failed, and needs attention at a glance.
3. Selects one session. Inspects task, plan, changed files, branch, PR, CI and elapsed time.
4. Can view terminal without taking control; may explicitly take control/attach and type.
5. Can switch sessions quickly while preserving terminal cursor/focus semantics.
6. Agent finishes: session no longer consumes live input but history and task links remain accessible.

**Design questions:** Should default grouping be tasks with sessions nested, or a flat session table with task filters? How to represent two agents on one task? How to handle one agent holding control in another client?

### Journey F — Automated review and human action

1. Implementation exits successfully; review stage starts (configured automation).
2. CI may be pending, passed, failed, or unavailable. If a reviewer agent runs, its work is read-only.
3. Review findings appear with evidence, severity, affected files and recommended action.
4. User chooses **Request fixes**, **Dismiss finding**, **Accept changes**, or **Retry review**.
5. Request fixes starts another locally approved implementation attempt; preserve previous reports.
6. Accepting is **not** the same as automatically merging a PR or deleting a worktree.

### Journey G — Repository switching and non-managed issues

1. User switches between unrelated local repositories: distinct Garden content; last-workspace preference behaves predictably.
2. User sees an existing GitHub Issue not in Garden; it is **not** modified automatically.
3. User opts to import it; preview lists exact label/Project membership changes and states body will not be overwritten.
4. User cancels import; nothing changes. User confirms; issue appears with an import provenance indicator.
5. User removes it from Garden later; original issue remains.

## 6. Screens to sketch (minimum set)

Design at least these twelve distinct screen/states, plus responsive variants:

| ID | Screen/state | Mandatory design content |
|---|---|---|
| G-01 | Garden first-use/disabled | Connect/Create/Skip, repo target, permission issues |
| G-02 | Setup: target/ownership | GitHub owner, repo, project choice, scope and preview |
| G-03 | Garden overview/Drafts | Filtered items, type, stage, priority, freshness, search, create |
| G-04 | Feature/Bug/Problem view | Type-filtered list or board with clear stages |
| G-05 | Item detail | Overview, structured spec, open questions, relationships, actions |
| G-06 | Edit and validation | Minimal draft, detailed edit, revision/conflict handling, approval gate |
| G-07 | Planning progress and failure | Planner identity, read-only state, requests for input, retry |
| G-08 | Plan review/approval | Versioned plan, spec fingerprint, risks, compare and explicit approval |
| P-01 | Playground overview | Repository-global sessions including unlinked/manual; attention indicators |
| P-02 | Selected session + live terminal | Attach read-only/take control; status, terminal, task context |
| P-03 | Changes/PR/CI inspection | Changed files/diff, branch/worktree, PR and checks |
| P-04 | Review findings/human decision | Severity/evidence, request fixes/dismiss/accept, clear consequences |

**Additional required states:** an empty board; offline/stale GitHub data; scope/permission denied; GitHub sync conflict; terminal 80×24; no provider accounts; PTY unsupported; session stopped/failed; external stage label conflict; issue not imported; branch/PR unavailable; changed spec after plan approval.

For each main workspace, provide an interaction map: action, entry point, key sequence, focus movement, escape behavior, expected side effect, confirmation requirements, and result/feedback.

## 7. Terminal design requirements and constraints

### 7.1 Supported layouts

Show sketches at **all three** targets:

1. **80×24** — minimum viable. Use single-column stacked views, item detail as full-screen, and compact navigation. No horizontal overflow.
2. **120×35** — everyday laptop terminal. Consider master/detail split for Garden and sessions/inspector for Playground.
3. **160×48** — wide monitor. May support more information, but don't turn it into a crowded dashboard.

Design gracefully for resize while an agent is attached or a modal is open. Avoid terminal-scrolling bugs caused by controls moving unexpectedly.

### 7.2 Navigation and interaction

- Keyboard-first and discoverable, consistent with Bonsai's current configurable keymap and command palette (`Ctrl+K`).
- Preserve existing Worktrees shortcuts within the Worktrees workspace. Resolve conflicts through workspace-specific key scopes, not globally surprising remaps.
- Existing Agents tab uses `g` to select it, `a` for all sessions, `n` for new agent, and `Ctrl+]` to exit agent terminal input mode. Treat these as valuable precedents; show how Playground interacts with them.
- Clearly distinguish focus on **navigation**, **list**, **detail**, **modal/editor**, and **live terminal**. Terminal input must not trigger global shortcuts accidentally.
- Search/filter stays available. Use compact on-screen shortcut hints and full `?` help.
- Confirm destructive actions, GitHub writes that materially change state, permission escalations, launching agents, and worktree cleanup.
- Do not require mouse interaction. Optional mouse support can be a later enhancement.

### 7.3 Visual priorities

1. **Task identity and current human gate** before decorative statistics.
2. Distinguish `Draft`, `Approved` (idea validated but no planner started), `Planning`, `Plan Review`, `Ready`, `Running`, `Reviewing`, `Human Review`, `Done`, `Blocked` with words/symbols, not color alone.
3. **Needs attention** must stand out from ordinary Running without false urgency.
4. Make it obvious when data is stale, approval has become invalid, an agent has finished, or a PR is still missing.
5. Rich specs should be readable as prose: headings, bullets, checklists, collapsed optional sections, scrolling, sensible line lengths.
6. Keep account usage and agent settings visible where relevant but not crowding everyday Garden browsing.
7. Respect installed Bonsai themes and ASCII fallback for terminals without suitable glyph coverage.
8. Loading, errors, disabled integrations, and empty states need the same level of care as the populated happy path.

### 7.4 Technical design affordances (not implementation code)

- Assume Go/Bubble Tea event-driven views; any blocking GitHub read should have loading/refresh/stale states.
- Do not assume a browser engine, rich HTML, arbitrary drag-and-drop, fonts, images, or a graphical framework.
- Keep controls expressible with standard terminal text, borders, selection, input, overlays and scroll regions.
- Make layouts fit the existing provider terminal attachment model and its focus safety behavior.
- Design render isolation: the user should be able to inspect a large spec without repainting a full history list for every keystroke, though actual optimization is an engineering detail.

## 8. Design explorations requested (provide options)

**Present at least two materially different directions**, rather than one polished solution presented as final.

Suggested contrasts:

### Direction A — Workspace tab bar + focused list/detail

A low-chrome header for `Garden | Playground | Worktrees`. Garden uses a list and full-screen document detail, switching to a split inspector only when space permits. Playground uses a searchable session list and a terminal/details pane. Prioritizes predictability and speed.

### Direction B — Command-centric workspace + contextual panels

Minimal permanent navigation. Command palette/switcher is first-class; contextual side navigation organizes Garden views and Playground sessions. Prioritizes uncluttered terminal density and expert keyboard use.

A third option is welcome if it has a clear different interaction hypothesis. Neither direction is preapproved. Compare honestly at 80×24 and 120×35 with focus/keyboard behavior.

## 9. Conceptual ASCII wireframes (starting points, not visual instructions)

### Garden / wide terminal

```text
┌ BONSAI ── Garden ── Playground ── Worktrees ──── acme/my-app ───────────┐
│ Drafts  Features  Bugs  Problems  Planning  Running  Review   All       │
├─────────────────────────────────┬───────────────────────────────────────┤
│ Search [recover session____]    │ #142 · Recover interrupted sessions   │
│ Filter: type=feature stage=draft │ FEATURE · DRAFT                       │
│                                 │                                       │
│ > #142 Recover agent sessions   │ Summary / Current behavior           │
│   #148 Account fallback         │ Requirements                          │
│   #151 Worktree cleanup warning │ Acceptance criteria                  │
│                                 │ Open questions                        │
│                                 │                                       │
│                                 │ [Edit]  [Validate idea]  [GitHub]    │
├─────────────────────────────────┴───────────────────────────────────────┤
│ ↑↓ select  Enter open  / search  n new  ? help           GitHub synced   │
└─────────────────────────────────────────────────────────────────────────┘
```

### Playground / wide terminal

```text
┌ BONSAI ── Garden ── Playground ── Worktrees ─── acme/my-app ───────────┐
│ Active  Needs attention  All  By task                                    │
├────────────────────────────┬─────────────────────────────────────────────┤
│ > #142 · Codex / Running   │ Codex · feat/142-recovery · PR #62          │
│   #151 · Claude / Waiting  │ [Terminal] [Changes] [PR / CI] [Issue]    │
│   #148 · Claude / Exited   │                                             │
│   Unlinked · Codex         │ > running tests...                         │
│                            │ > 12 passed                                 │
│                            │                                             │
│                            │ [Observe] [Take control] [Stop]            │
├────────────────────────────┴─────────────────────────────────────────────┤
│ / search  Enter inspect  Ctrl+] unfocus terminal    CI: pending          │
└──────────────────────────────────────────────────────────────────────────┘
```

These sketches are intentionally rough. Please critique them, replace them if better, and avoid copying their density blindly into smaller terminals.

## 10. Content design / terminology

Use consistent language. Preferred user-visible names:

| Term | Meaning | Avoid confusing with |
|---|---|---|
| **Garden** | Idea/issue/spec/plan workspace | GitHub Project itself |
| **Playground** | Agent sessions and execution workspace | A separate Git repository |
| **Worktrees** | Low-level Git isolated checkout management | Tasks |
| **Issue** | Canonical GitHub record | Duplicate cards in different views |
| **Draft** | Unapproved issue/spec | GitHub's Project-only draft issue (not used) |
| **Approved** | User validated idea/specification; planner not started | Approved implementation plan / Ready |
| **Plan Approved / Ready** | User approved a specific plan revision | Agent already running |
| **Running** | Local implementation underway | GitHub stage alone being an execution command |
| **Reviewing** | Automated verification underway | User's acceptance step |
| **Human Review** | Findings ready for user decision | Automatic merge |
| **Unlinked** | Manual agent session without a Garden issue | Failed or orphan process |

The design may refine surface wording if it preserves these distinctions.

## 11. Example content for realistic sketches

Use **fabricated examples only**; do not create real GitHub issues or change the user's repo.

| Example | Type | Stage | Interesting UX condition |
|---|---|---|---|
| #142 Recover interrupted sessions | Feature | Draft | Many open questions; long specification |
| #148 Switch agent account after rate limit | Feature | Plan Review | Two plan revisions, latest not yet approved |
| #151 Broken usage refresh | Bug | Running | Agent waiting for user input |
| #155 CI flakes in worktrees | Problem | Human Review | Three findings; one dismissed |
| #161 Improve process status colors | Feature | Ready | Approved plan; launch dialog not started |
| #164 Stale auth token | Bug | Blocked | Cannot launch; permission error |

Show how a multi-page Issue, long plan, and large file diff remain navigable in the terminal.

## 12. Don't lose the ChatGPT/mobile entry point

The UI does **not** need a phone client. It does need to clearly communicate that GitHub Issues can be edited elsewhere and reappear after sync. The designer should include:

- A visible GitHub Issue link / copy URL action.
- Last-synced indicator, refresh affordance, and warnings about remote edits.
- An understandable representation of an externally recorded approval: who approved, what exact revision, and what action is possible locally.
- A conflict preview when local edited text differs from remote changes.
- No misleading `Start remotely` or `Run from ChatGPT` button.

For v1, a reusable ChatGPT prompt/connector integration can update canonical Issues and plans. Remote execution and remote terminal attachment are future product possibilities, **not UX promises** in these sketches.

## 13. Deliverables expected from the design agent

Please provide the following **design artifacts only**:

1. **Current UI audit:** Briefly summarize existing Bonsai TUI layout, keyboard conventions, agent tab/terminal behavior and reuse opportunities. Read `internal/ui/` and relevant agent contracts; ignore `web/`.
2. **Two or three distinct design concepts:** Rationale, strengths, weaknesses, 80×24 and 120×35 examples, with a justified recommended direction.
3. **Detailed annotated wireframes:** Minimum screen matrix in Section 6; wider 160×48 variant for the major Garden and Playground screens.
4. **User flow diagrams:** First-time setup, create/refine/approve, plan/revise/approve, local launch, attach and supervise, review/fix/accept, import/remove.
5. **Navigation and keyboard specification:** Workspace switching, focus order, modal/editor states, terminal attach/unfocus semantics, handling key collisions and customizable bindings.
6. **State/feedback catalog:** Loading, stale, offline, auth error, sync conflicts, session failures, needs input, unsupported PTY, approval stale, no PR, no provider accounts.
7. **Component inventory:** Reusable list, filters, item card/row, spec renderer/editor, plan diff, approval banner, agent session row, PR/CI panel, confirmation dialog, notifications.
8. **Design tradeoff memo:** Explicit decisions that affect engineering complexity or data contracts; no hidden new sources of truth.
9. **A short list of decisions requiring the user's approval:** Especially navigation model, Garden default view, item editor behavior, Playground grouping, review layout and terminal sizes.

**Deliverable format:** Markdown design rationale plus text/ASCII wireframes and/or external static mockups if you work in a design application. Everything must be intelligible without interactive code. Label assumptions and specify screen size on every wireframe. You may suggest a distinctive visual direction, but do not change app code or create production UI.

## 14. Evaluation rubric for the user

The selected concept should score well on:

| Criterion | What excellence looks like |
|---|---|
| **Clarity** | Can find an idea or agent quickly without understanding Git plumbing |
| **Information density** | Rich details when needed without a permanently crowded terminal |
| **Workflow truthfulness** | Clearly separates Draft, idea approval, plan approval, implementation, review |
| **Execution safety** | Explicit launch and permission gates; no ambiguous remote automation |
| **Scalability** | Works with hundreds of Issues, dozens of sessions, large specs |
| **Keyboard ergonomics** | Fast navigation, discoverable focus and predictable shortcuts |
| **Compatibility** | Preserves existing Worktrees and terminal interaction expectations |
| **Per-repo behavior** | Users know which GitHub repo/board is active and how to change it |
| **Resilience** | Offline, stale, failures, conflicts and missing capabilities remain understandable |
| **Feasibility** | Implementable in Bubble Tea/Lip Gloss without a GUI rewrite |

## 15. Reusable prompt for the design agent

> Read the attached `bonsai-garden-ui-design-brief.md` and `bonsai-garden-implementation-plan.md`. Inspect the latest Bonsai repository, **only its terminal UI and relevant agent/session contracts; do not inspect `web/`**. Do not modify any code or create real GitHub Issues. Your task is to propose at least two high-quality, materially different keyboard-first UI architectures for Garden, Playground and preserved Worktrees; sketch all required screens in terminal sizes 80×24, 120×35 and a major-screen 160×48 variant; show navigation, permissions/approval gates, remote-sync awareness, session interaction, diff/PR/CI/review workflows, and failure states. Explain tradeoffs and recommend a direction without presenting it as approved. Produce design artifacts that I can review and bring back for implementation planning.

---

**Design completion condition:** The user can choose a concrete layout and interaction model, including complete critical-state sketches, while the architecture remains GitHub-Issues-first, opt-in per repository, terminal-first, and local-execution-only. **No feature implementation begins until the user returns with and explicitly approves a final design.**
