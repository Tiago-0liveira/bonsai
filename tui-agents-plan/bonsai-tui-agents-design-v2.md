# Bonsai TUI — Agents Tab Design v2

Based on your screenshot, I would **keep the existing two-pane layout almost exactly as it is**, add agent indicators to the worktree list on the left, and introduce an **Agents tab on the right**.

The Agents tab would have three responsibilities:

1. Show all running agents associated with the selected worktree, with a way to switch between them.
2. Embed the **real, fully interactive agent terminal**, just like the web client, with usage and account information in its header.
3. Provide a separate account-management modal containing all profiles, usage, and launch defaults.

I would also make the Agents tab available even when the selected worktree has no running agents, so you can launch a new one immediately.

## 1. Unicode symbols for providers

Yes, this is entirely possible. You can use Unicode characters that resemble each provider's visual identity without relying on images, special fonts, or Nerd Fonts.

There's no standard Unicode equivalent of the official Claude, Codex, or Antigravity logos, but we can choose recognizable approximations.

| Claude | Codex | Antigravity |
|:---:|:---:|:---:|
| **✳** | **◎** | **✦** |
| Sunburst / asterisk | Concentric symbol | Four-point star |

**Alternative characters to test in your actual terminal**

| Claude | Codex | Antigravity |
|:---:|:---:|:---:|
| ✻ ✺ ✳ | ◈ ⬡ ◎ | ⟡ ✧ ✦ |

I would start with **`✳` Claude, `◎` Codex, and `✦` Antigravity**.

Use your existing theme colors to differentiate them further. Keep a single-cell fallback (`C`, `X`, `A`) for fonts or terminals that don't display these symbols consistently. These are visual approximations rather than official brand marks.

## 2. Agent indicators on the worktree list

The left pane is already dense, particularly with your long branch names and paths. I wouldn't introduce another line for each agent.

Instead, append compact symbols to the worktree's first line.

**Illustrative worktree rows**

```text
main (main)
  /home/tiagoliv/bonsai

feat/persistent-agents           ✳ ◎
  /home/tiagoliv/bonsai-feat...

claude/agents-tab-plan           ✳
  /home/tiagoliv/bonsai/.claude...

codex/composable-tui-layout      ◎²
  /home/tiagoliv/bonsai-tui-layout

feature/usage                    ✦
  /home/tiagoliv/bonsai-usage...
```

`◎²` means two Codex sessions. Symbols would inherit provider-specific colors and be aligned to the end of the row when space permits.

I suggest the following behavior:

- One running agent: show its provider symbol.
- Multiple agents from different providers: show their symbols side by side.
- Multiple agents from one provider: show the symbol and a count, for example `✳²`, or `✳2` for more predictable terminal alignment.
- No agents: no indicator.
- Completed agents: no active indicator, although they remain accessible in session history.

Potentially, use a distinct warning marker such as `!` for failed or attention-requiring sessions, but only when that status is reliably known.

**One nice interaction:** selecting a worktree that has agents and pressing `g` opens the Agents tab for that worktree. That should become the fastest way to jump into an active session.

## 3. The actual Agents tab on the right

Here is how I would fit it into your current interface, rather than creating a completely new screen.

**Scenario A — Selected worktree has two running agents**

*Proposed terminal layout*

```text
╭─ Worktrees ────────────╮╭─ Git Log | Processes | Inspect | Diff | Agents (g) ───────────────────╮
│                       ││ AGENTS · feature/auth                                                │
│ main                  ││                                                                        │
│                       ││ Sessions: [1 ✳ Auth fix] [2 ◎ Tests]                 a: All  n: New │
│ feat/auth       ✳ ◎   ││ ────────────────────────────────────────────────────────────────────── │
│ feat/dashboard        ││ ✳ Claude · work · Opus  │  ● Running                                 │
│                       ││ 5h 72% left  ·  Week 41% left  ·  Reset 2h 14m            u: Usage │
│ feat/payments    ✦   ││ ────────────────────────────────────────────────────────────────────── │
│                       ││                                                                        │
│                       ││  > Investigate the authentication bug                                 │
│                       ││                                                                        │
│                       ││  Reading src/auth/session.go                                           │
│                       ││  Editing src/auth/middleware.go                                        │
│                       ││                                                                        │
│                       ││  Running npm test...                                                   │
│                       ││                                                                        │
│                       ││  > _                                                                   │
│                       ││                                                                        │
│                       ││                                                                        │
╰───────────────────────╯╰────────────────────────────────────────────────────────────────────────╯
 tab focus · g agents · ctrl+] menu · ? keys
```

Notice the three fixed sections within the right pane:

1. **Session strip:** Running agents for the selected worktree, with a visible `a: All` shortcut for the full list.
2. **Account and usage header:** Provider, profile, model, lifecycle state, and cached quota details for the selected session's account.
3. **Interactive PTY:** All remaining height goes to the real agent terminal, so the agent has as much room as possible.

The terminal region should be sized using the space remaining after the fixed header rows. Whenever the pane is resized, Bonsai needs to resize the attached PTY accordingly.

### Scenario B — Many agents in one worktree

When the number of running agents exceeds the available width, don't try to render every session label.

```text
╭─ Agents (g) ───────────────────────────────────────────────────────╮
│ Sessions: [1 ✳ Auth] [2 ◎ Tests] [3 ✳ API] [+4 more]   a: All  │
│ ───────────────────────────────────────────────────────────────────│
│ ◎ Codex · personal · gpt-5.5           ● Running                 │
│ 5h 91% left  ·  Week 66% left                     u: Usage      │
│ ───────────────────────────────────────────────────────────────────│
│                                                                  │
│                 INTERACTIVE CODEX TERMINAL                       │
│                                                                  │
╰──────────────────────────────────────────────────────────────────╯
```

Press `1`, `2`, or `3` in Bonsai navigation mode for a quick switch, or `a` for the complete session picker.

```text
╭────────────── AGENTS IN feature/auth ──────────────╮
│                                                    │
│  ● ✳ Fix authentication     Claude · work          │
│  ● ◎ Run integration tests  Codex · personal       │
│  ● ✳ Review error handling  Claude · personal      │
│  ● ✦ Refactor routes        Antigravity · work     │
│  ✓ ◎ Previous attempt       Codex · work           │
│                                                    │
│  / Search    Enter Open    n New    Esc Close      │
╰────────────────────────────────────────────────────╯
```

This would behave like a searchable picker, consistent with the other Bonsai selectors.

I would preserve each worktree's last-selected session, so navigating between worktrees automatically restores the session you were previously viewing.

### Scenario C — No running agents

```text
╭─ Agents (g) ───────────────────────────────────────────────────────╮
│ No running agents                                  a: All  n: New │
│ ───────────────────────────────────────────────────────────────────│
│                                                                  │
│                   No active agents                               │
│                                                                  │
│           Start an agent in feature/dashboard                    │
│                                                                  │
│               [n] New agent                                     │
│                                                                  │
│               [u] View account usage                             │
│                                                                  │
╰──────────────────────────────────────────────────────────────────╯
```

Previously completed sessions could still be accessed from the `a: All` picker.

### A useful discovery in your current branch

I also checked your [`feat/persistent-agents` branch](https://github.com/Tiago-0liveira/bonsai/tree/feat/persistent-agents), which is relevant to the latest commits visible in the screenshot.

You've already implemented a detached `agenthost` architecture, and the branch has `bonsai agents attach` support. In particular:

- `internal/core/agenthost` manages detached agent processes and their Unix-socket connections.
- The host protocol supports terminal output, input, resizing, and reconnection.
- It also supports **taking terminal control from another attached client**, rather than only attaching read-only.
- `bonsai agents attach <id|name>` already has a terminal attachment flow.

That's excellent groundwork for the exact TUI experience you're describing.

I would reuse that attachment protocol directly from the TUI wherever possible. **You don't need to implement another PTY manager or force the TUI to depend on the web browser.** The TUI and web client would attach to the same agent host.

The piece that still needs to be built is the embedded terminal renderer and its integration with Bubble Tea's focus and keyboard handling.

## 4. Account settings modal

I would give this a dedicated `m` shortcut while the Agents tab is active.

Rather than navigating away from the current agent, open a large overlay on top of the TUI.

The modal has a persistent account list on the left and three detail tabs on the right: **Overview, Usage, Defaults**.

### Account modal — Overview

```text
╭─ AGENT ACCOUNTS ───────────────────────────────────────────────────────╮
│                                                                       │
│ ACCOUNTS               │ ✳ Claude · work                              │
│                        │                                              │
│ CLAUDE                 │ [Overview]  Usage  Defaults                 │
│ > ✳ work           ●   │ ──────────────────────────────────────────── │
│   ✳ personal       ●   │                                              │
│                        │ Status       Connected                       │
│ CODEX                  │ Identity     work@example.com                │
│   ◎ work           ●   │ Provider     Claude Code                     │
│   ◎ personal       ●   │ Plan         Max                             │
│                        │ Auth         Browser login                   │
│ ANTIGRAVITY            │                                              │
│   ✦ personal       ●   │ Default model    Opus                        │
│                        │ Default effort   High                        │
│                        │                                              │
│                        │ Sessions         2 running                   │
│                        │                                              │
│                        │ [Sign in again]  [Remove account]           │
│                        │                                              │
│ + Add account          │                                              │
├────────────────────────┴──────────────────────────────────────────────┤
│ j/k Navigate  ·  Tab Section  ·  Enter Select  ·  Esc Close          │
╰───────────────────────────────────────────────────────────────────────╯
```

This gives you a compact account manager without a separate full-screen app.

### Account modal — Usage

```text
╭─ AGENT ACCOUNTS ───────────────────────────────────────────────────────╮
│                                                                       │
│ ACCOUNTS               │ ✳ Claude · work                              │
│                        │                                              │
│ CLAUDE                 │ Overview  [Usage]  Defaults                 │
│ > ✳ work           ●   │ ──────────────────────────────────────────── │
│   ✳ personal       ●   │                                              │
│                        │ 5-hour window                                │
│ CODEX                  │ ███████░░░  72% remaining                     │
│   ◎ work           ●   │ Resets in 2h 14m                             │
│   ◎ personal       ●   │                                              │
│                        │ Weekly window                                │
│ ANTIGRAVITY            │ ████░░░░░░  41% remaining                     │
│   ✦ personal       ●   │ Resets in 3d 8h                              │
│                        │                                              │
│                        │ Additional limits                            │
│                        │ Sonnet weekly    82% remaining               │
│                        │ Credits          $12.50                      │
│                        │                                              │
│                        │ Updated 35 seconds ago                       │
│                        │                                              │
│ + Add account          │ [r] Refresh usage                            │
├────────────────────────┴──────────────────────────────────────────────┤
│ j/k Account  ·  Tab Section  ·  r Refresh  ·  Esc Close               │
╰───────────────────────────────────────────────────────────────────────╯
```

This view could show different fields depending on which provider is selected.

For example, Antigravity's model quotas would be displayed as individual model pools, whereas Claude's and Codex's primary windows would use the shared progress-meter components.

I would also put a short usage summary beside each account in the left sidebar when the modal is wide enough.

### Account modal — Defaults

```text
╭─ AGENT ACCOUNTS ───────────────────────────────────────────────────────╮
│                                                                       │
│ ACCOUNTS               │ ◎ Codex · personal                           │
│                        │                                              │
│ CLAUDE                 │ Overview  Usage  [Defaults]                 │
│   ✳ work           ●   │ ──────────────────────────────────────────── │
│   ✳ personal       ●   │                                              │
│                        │ Model                                        │
│ CODEX                  │ > gpt-5.5                              [▼] │
│   ◎ work           ●   │                                              │
│ > ◎ personal       ●   │ Reasoning effort                             │
│                        │ > High                                 [▼] │
│ ANTIGRAVITY            │                                              │
│   ✦ personal       ●   │ Permissions                                  │
│                        │ > Auto                                 [▼] │
│                        │                                              │
│                        │ Other provider-specific options              │
│                        │ ...                                          │
│                        │                                              │
│                        │ These defaults apply to new sessions.       │
│                        │ Running agents are not affected.            │
│                        │                                              │
│ + Add account          │               [Reset]  [Save defaults]      │
├────────────────────────┴──────────────────────────────────────────────┤
│ Tab Section  ·  Enter Edit  ·  Esc Back                               │
╰───────────────────────────────────────────────────────────────────────╯
```

The Defaults section should use Bonsai's existing provider-defined options. That way, when a provider exposes new fields, the modal doesn't require a provider-specific rewrite.

I would also support adding and reauthenticating profiles through this modal eventually, ideally reusing the same setup-job terminal mechanism already available to the web client.

---

## 5. Keyboard controls while the agent is active

One detail I'd change from the earlier proposal: **use the same `Ctrl+]` prefix that your new `bonsai agents attach` command already uses**.

That gives the interface a consistent control mechanism and avoids consuming normal input characters.

When you're inside the actual agent terminal:

```text
╭─ Agents (g) ──────────────────────────────────────────────────────────╮
│ [1 ✳ Auth] [2 ◎ Tests]                             a: All  n: New │
│ ✳ Claude · work  │  5h 72%  ·  Week 41%           Ctrl+] u: Usage │
├──────────────────────────────────────────────────────────────────────╯
│                                                                      │
│                     INTERACTIVE AGENT PTY                            │
│                                                                      │
│                   All normal keys go to agent                        │
│                                                                      │
├──────────────────────────────────────────────────────────────────────┤
│ Ctrl+]  →  Bonsai command prefix                                    │
╰──────────────────────────────────────────────────────────────────────╯
```

After pressing `Ctrl+]`, a compact control strip could appear:

```text
 BONSAI CONTROL
 [d] Detach  [a] Agents  [u] Usage  [m] Accounts  [t] Take control
 [s] Stop    [Esc] Return to agent
```

These would be the proposed interactions:

| Shortcut | Behavior |
|---|---|
| `g` | Open the right-hand Agents tab from normal TUI mode |
| `a` | Open the running/recent agents picker |
| `n` | Start a new agent for the selected worktree |
| `m` | Open the Accounts modal |
| `u` | Open quick account usage |
| `Ctrl+] d` | Detach from the agent, leaving it running |
| `Ctrl+] a` | Open the session picker from terminal mode |
| `Ctrl+] u` | Inspect usage while interacting with the agent |
| `Ctrl+] t` | Request control of a read-only attachment |
| `Ctrl+] s` | Stop the agent after confirmation |

This also solves a subtle issue with your web client: if an agent is currently controlled from the browser, the TUI should initially show its terminal as read-only, not silently steal input.

The user can explicitly take control. Your current agent-host stream supports that handoff.

## 6. Compact layout for smaller terminals

The design should also work on an 80-column terminal. I would simplify the Agents tab automatically.

```text
╭─ Worktrees ───────╮╭─ Agents (g) ─────────────────────────────────╮
│                  ││ [1 ✳ Auth] [+2]                     a: All │
│ main             ││ ✳ work · 5h 72% · W 41%        u: Usage     │
│                  │├─────────────────────────────────────────────┤
│ feat/auth    ✳ ◎ ││                                             │
│                  ││   Interactive Claude terminal               │
│ feat/ui          ││                                             │
│                  ││   > Working...                              │
│                  ││                                             │
│                  ││                                             │
╰──────────────────╯╰─────────────────────────────────────────────╯
```

At very narrow widths, the session strip might become just:

```text
Agents (g)  │  ✳ Auth [1/3]  │  a: Switch
```

The rule should be to prioritize terminal area over extra information. The agent needs usable width and height.

---

## 7. How this should behave internally

Given the current `feat/persistent-agents` architecture, I'd divide implementation work into four fairly clear pieces.

```text
Worktree list
    │
    ├── Session summary cache
    │      └── Active agents grouped by worktree ID
    │
    └── Provider glyphs + running counts


Right-hand Agents tab
    │
    ├── Session selector
    ├── Account usage header
    ├── Embedded VT terminal emulator
    │      └── agenthost terminal attachment
    └── Agent lifecycle controls


Accounts modal
    │
    ├── AccountService
    ├── UsageService
    └── Profile defaults / provider launch fields
```

A few implementation details are particularly important.

**Session indicators should come from authoritative session state**, not by scanning the operating system for Claude or Codex processes. The session already has a worktree identifier, provider, account and lifecycle state. Group active sessions by worktree ID and render their symbols in the left list.

**The right-hand terminal must be a real VT terminal renderer**, not the existing process-log viewport. ANSI/VT sequences need to update a terminal screen buffer, including cursor movement, alternate-screen handling and resizing. Bubble Tea then renders that screen inside the right pane. The terminal's raw output should not simply be passed into Lip Gloss as ordinary text.

**Terminal output should be streamed, not constantly polled.** Session metadata and account usage can update independently at much lower frequency. This keeps the interface responsive when agents produce large amounts of output.

**The Accounts modal must never interrupt the underlying agent.** Opening settings, changing tabs, or switching worktrees should detach or suspend the UI attachment as necessary, not terminate the hosted session.

**The same session should appear in both interfaces.** Launching an agent in the web client should populate the TUI's worktree indicator. Launching it in the TUI should make it available in the browser. Account usage and defaults should likewise come from the same underlying services.

---

## 8. Final proposed interface structure

**Bonsai TUI** — Proposed navigation

```text
Bonsai TUI
    │
    ├── Worktrees pane
    │     Branch list · worktree agent glyphs · running counts
    │
    └── Right-hand Agents tab
          Session strip → account/usage header → interactive PTY
          │
          ├── Agent picker
          │     Running and recent sessions, launch and switch
          │
          └── Accounts modal
                Overview, usage and defaults for every profile
```

For your current layout, I would **not add a separate primary Usage tab**. The account usage header, quick usage overlay, and Usage section inside the Accounts modal already cover that need without adding more permanent navigation.

The design I would commit to is:

- Left pane remains a worktree navigator with compact provider glyphs.
- Right pane gains `Agents (g)` alongside the existing tabs.
- The top of the Agents tab is a compact session switcher.
- The selected session gets a fixed account and usage header.
- The remainder is the fully interactive provider PTY.
- `m` opens a two-column account-management modal.
- `Ctrl+]` provides Bonsai controls without interfering with the agent's normal keyboard interactions.

The biggest remaining design choice is **whether switching the selected worktree should immediately attach its last active agent or leave the terminal detached until you explicitly select one**.

I lean toward remembering and automatically showing the last-selected agent's output, while requiring an explicit action before taking keyboard control. That preserves the convenience of your existing two-pane workflow without accidentally sending input to the wrong agent.
