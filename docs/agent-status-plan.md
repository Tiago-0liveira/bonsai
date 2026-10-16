# Agent activity status: implementation plan

Status: plan, revised 2026-10-16. Base: `feat/tui-agents` @ `1beaf2b` (agent
holders, cross-process admission, `Summary.Origin`, TUI agents tab + worktree
glyphs). First draft was
based on `ed616b7` and assumed an in-API `agentterminal.Manager`; that no longer
exists (see §2).
CLIs checked locally: Claude Code `2.1.296`, `codex-cli 0.162.1`, `agy 1.3.4`.
Sources: the binaries themselves (embedded docs, JSON schemas, `codex app-server
generate-json-schema`), code.claude.com/docs/en/hooks, learn.chatgpt.com/docs/hooks.

## 1. Goal

Every agent session shows what the agent is doing, not just whether its process
lives:

| User-facing status | Meaning |
| --- | --- |
| **Starting** | holder is preparing/launching the process |
| **Working** | agent is in a turn (thinking, running tools) |
| **Needs input** | agent is blocked on the user: permission prompt, question, plan approval, MCP elicitation |
| **Idle** (a.k.a. done) | turn finished; agent waits for the next prompt |
| **Stalled** | working, but no signal at all for a long time (flag on Working) |
| **Exited / Failed** | process ended |

The status must reach **every client of a session**: the web UI, the TUI
(worktree glyphs, Agents tab) and `bonsai agents`, whether or not `bonsai web`
runs. It is produced by **one provider-neutral event system** that later
consumers (desktop/browser notifications, sounds, webhooks, an audit log) can
subscribe to with a few lines, without touching providers or the holder.

Non-goals: deciding permissions for the agent (hooks are observe-only, always
fail-open), resuming sessions, cost/token tracking.

## 2. What exists today (`feat/tui-agents` @ `1beaf2b`)

- **Holders own sessions.** Each session runs in a detached `bonsai
  __agent-host` process (`internal/core/agenthost/host.go`, wired in
  `main.go:75`). The holder owns the PTY, the bounded `agentterminal.Stream`
  and the lifecycle (`agentterminal.RunSession`, `run.go`). It serves clients on
  `<SockDir>/agents/<id>.sock` (NDJSON, `protocol.go`, `Version = 1`):
  `ModeTerminal` (replay + live + input) and `ModeStatus` (summaries only).
- **Every summary change** goes through `host.update` → `host.publish`:
  `stream.Status` (terminal clients), a `status` frame to each status monitor,
  and `writeRecord` (rewrites `<web home>/agents/<id>.json`). Fine for rare
  lifecycle edges, too heavy as-is for activity edges.
- **API side.** `agenthost.Manager` (`manager.go`) lives only in localapi. It
  follows each holder over a status connection; `Manager.apply` calls
  `changed(project)` on **every** `status` frame, which is
  `localapi.publishAgents` (`internal/server/localapi/agents.go:418`): rewrites
  the `api_agent_mutations` journal of every project and commits a full browser
  snapshot. `Manager.watch` ignores frame types it does not know.
- **Cross-process start.** `agenthost.Starter` (`start.go`) admits under
  `<Records>/admit.lock`; `Summary.Origin` is `web|tui|cli`. Records-dir
  watching (`agenthost.Watch`, runtime adoption in the API) and the real TUI
  backend (`internal/agentsbackend`) are planned in TUI track A2/A3, not merged.
- **TUI.** `internal/ui/agentsapi` is the TUI contract; `Sessions.Changes`
  fires on record create/update/remove. Worktree rows show per-provider glyphs
  for active sessions and a failed marker (`internal/ui/agents.go:284`
  `agentBadge`); the Agents tab has a session strip. Today only the fake
  backend exists.
- **CLI.** `bonsai agents ls/attach/stop` read records via `agenthost.List`.
- **Web.** `mapAgent` (`web/src/api/agents.ts:150`) collapses lifecycle into
  `state: 'running' | 'finished'`; `idle` exists in `StatusDot`
  (`BranchesIsland.tsx:10`) but is never produced. Status rendering is spread
  over `BranchesIsland`/`BottomWorkspace` (`StatusDot`), `AppShell`
  (`runtimeDot`), `Inspector`, `ProfileSessions`, `BonsaiNode`,
  `buildCanvasGraph`, `CommandPalette`, `WorktreeCard`, `DeleteWorktreeDialog`.
  `localClient.ts` only knows `agents_changed` (refetch).
- Providers inject `BONSAI_AGENT_PROVIDER|ACCOUNT_ID|SESSION_ID` into the
  agent env (`internal/providers/*/environment.go`); each session has a private
  `RuntimeDir` (0700) removed by `SessionStore.Cleanup` at the end of
  `RunSession`. Antigravity runs with `HOME = session.HomeDir`.
- `PrepareSessionRequest` is `{Account, Session, Args, Launch}`
  (`internal/core/agents/provider.go:152`).
- Codex already talks to `codex app-server` via `withAppServer`
  (`internal/providers/codex/appserver.go:117`).
- `internal/core/notify` sends desktop notifications (best effort).

Consequence for this plan: **the holder is the producer.** Hooks, terminal
scanning, input and lifecycle all meet in the holder process, so the reducer
runs there, and every client learns activity from the holder (socket frame or
record), not from the API.

## 3. Findings: what each CLI can tell us

All three CLIs ship a **lifecycle hook system** (Claude-style JSON on stdin).
That is the primary signal. The PTY that the holder owns is the secondary
signal (terminal notifications and progress escapes). User keystrokes are the
third.

### Claude Code 2.1.296

- Hooks: `command`, `http`, `mcp_tool`, `prompt`, `agent` handlers; `async: true`
  for command hooks. Hook entries **merge** across settings layers, and
  `--settings <file-or-json>` adds a per-launch layer, so Bonsai injects hooks
  per session without touching the profile's `settings.json`.
- Useful events: `SessionStart`, `UserPromptSubmit`, `PreToolUse`,
  `PermissionRequest`, `PostToolUse`, `PostToolUseFailure`, `Notification`
  (matchers `permission_prompt`, `idle_prompt`, `elicitation_dialog`,
  `agent_needs_input`, `agent_completed`, …), `Elicitation`/`ElicitationResult`,
  `Stop`, `StopFailure` (matcher = error kind), `SessionEnd`.
- Questions are a tool: `PreToolUse` with `tool_name: AskUserQuestion` (and plan
  approval: `ExitPlanMode`) means the agent waits for the user.
- Can be disabled by `disableAllHooks`, managed `allowManagedHooksOnly`, or
  `--bare`: we must degrade gracefully.
- PTY: `terminalProgressBarEnabled` emits OSC `9;4` progress; notifications go
  through `preferredNotifChannel` (`iterm2` = OSC 9, `kitty`, `ghostty`,
  `terminal_bell`).

### Codex 0.162.1

- Hooks are **stable and on by default** (`features.hooks`). Same JSON shape as
  Claude plus `turn_id`, `model`. Events: `SessionStart`, `UserPromptSubmit`,
  `PreToolUse`, `PermissionRequest`, `PostToolUse`, `PreCompact`, `PostCompact`,
  `SubagentStart`, `SubagentStop`, `Stop`, `Interrupt`, `SessionEnd`.
  Handlers: `command` (+ `async`, max 8 in flight) and `mcp_tool`; no `http`.
- Config: `hooks.json` or inline `[hooks]` next to each config layer, so
  `$CODEX_HOME/hooks.json` in the profile home Bonsai already owns. The profile
  home is shared by concurrent sessions of one account: the hook command must
  find its session from the environment, never from the file.
- **Trust gate**: non-managed hooks are skipped until trusted ("Hooks need
  review"); trust is stored as `hooks.state."<key>".trusted_hash` in
  `config.toml`. The app-server exposes `hooks/list` (each hook's `key`,
  `currentHash`, `trustStatus`, `sourcePath`) and `config/value/write` /
  `config/batchWrite`, so Bonsai can trust **exactly its own hooks**
  non-interactively. `--dangerously-bypass-hook-trust` would also run untrusted
  user hooks: not used.
- `request_user_input` tool = question. MCP elicitations have no hook.
- PTY: `tui.notifications` (`agent-turn-complete`, `approval-requested`,
  `async-question`), `tui.notification_method` (osc9/bel),
  `tui.notification_condition` (`unfocused|always`), all `-c`-overridable.

### Antigravity `agy` 1.3.4

- Hooks via `hooks.json` (named groups), global file `~/.gemini/config/hooks.json`.
  Sessions run with `HOME = session.HomeDir`, so the file is per session and
  private. Events: `SessionStart`, `PreToolUse`, `PostToolUse`, `PreInvocation`
  (before each model call), `PostInvocation`, `Stop` (with `terminationReason`:
  `NO_TOOL_CALL`, `ERROR`, `USER_CANCELED`, … and `fullyIdle`). Command only,
  **synchronous**, stdout **must be JSON**.
- **No permission/notification hook.** `PreToolUse` requires a `decision`, so
  Bonsai must not register it (any answer changes permission behaviour).
- PTY: agy emits OSC `9;4` progress (`9;4;3` busy … `9;4;0` clear) and terminal
  notifications for permission prompts and questions when notifications are on
  (settings key to confirm in S0). That is our only "needs input" signal for
  agy, marked *inferred*.

### Signal matrix (target)

| Status | Claude | Codex | Antigravity |
| --- | --- | --- | --- |
| Working | `UserPromptSubmit`, `PreToolUse`, `PostToolUse` | `UserPromptSubmit`, `PreToolUse`, `PostToolUse` | `PreInvocation`, `PostToolUse`, OSC 9;4 busy |
| Needs input: permission | `PermissionRequest`, `Notification:permission_prompt` | `PermissionRequest` | terminal notification (inferred) |
| Needs input: question | `PreToolUse:AskUserQuestion`, `ExitPlanMode`, `Elicitation`, `Notification:agent_needs_input` | `PreToolUse:request_user_input`, OSC 9 `async-question` | terminal notification (inferred) |
| Idle | `Stop`, `StopFailure`, `Notification:idle_prompt`, `SessionStart` | `Stop`, `Interrupt`, `SessionStart` | `Stop`, `SessionStart` |
| Exited | holder lifecycle | holder lifecycle | holder lifecycle |

## 4. State model

Two orthogonal axes. Lifecycle (`Summary.State`) stays exactly as it is: it is
recorded, journaled, drives admission, Stop and `keep_running`. **Activity** is
new and only meaningful while lifecycle is `running`.

```go
// internal/core/agentactivity

type State string

const (
	Unknown State = "unknown" // running, nothing reported yet or provider reports nothing
	Working State = "working"
	Waiting State = "waiting" // needs the user
	Idle    State = "idle"    // turn over, ready for a prompt
)

type Reason string // Waiting: permission | question | plan | elicitation
                   // Idle:    ready | turn_complete | interrupted | error

type Activity struct {
	State   State     `json:"state"`
	Reason  Reason    `json:"reason,omitempty"`
	Detail  string    `json:"detail,omitempty"` // tool name or short sanitized text, ≤ 120 runes
	Since   time.Time `json:"since"`
	Source  Source    `json:"source"`            // hook | terminal | input | lifecycle
	Stalled bool      `json:"stalled,omitempty"` // Working with no signal for StallAfter
	// Inferred is true when the state came from a heuristic (terminal or input)
	// rather than a provider hook, so clients can soften it.
	Inferred bool `json:"inferred,omitempty"`
}

// Changed reports a transition worth publishing everywhere: State, Reason,
// Stalled or Inferred differ. A Detail-only change is not one (see §5.3).
func (a Activity) Changed(b Activity) bool
```

The **display status** is derived in one place per stack: Go helper
`agentactivity.Display(summary)` (used by the TUI and `bonsai agents`) and one
TS function (web). Lifecycle `starting|stopping|exited|failed` wins, otherwise
the activity state (`unknown` displays as "Running").

### Normalized signals

Providers never set states. They translate their raw hook payloads into
provider-neutral **signals**; one pure reducer turns signals into activity.

```go
type Kind string

const (
	SessionStarted      Kind = "session_started"
	PromptSubmitted     Kind = "prompt_submitted"
	ToolStarted         Kind = "tool_started"
	ToolFinished        Kind = "tool_finished"
	PermissionRequested Kind = "permission_requested"
	InputRequested      Kind = "input_requested"  // question / plan / elicitation (Reason says which)
	InputResolved       Kind = "input_resolved"
	TurnCompleted       Kind = "turn_completed"
	TurnFailed          Kind = "turn_failed"
	Interrupted         Kind = "interrupted"
	SessionEnded        Kind = "session_ended"
	// Produced by the holder itself, not by providers:
	TerminalBusy         Kind = "terminal_busy"         // OSC 9;4;1|3
	TerminalClear        Kind = "terminal_clear"        // OSC 9;4;0
	TerminalNotification Kind = "terminal_notification" // OSC 9 / 777 / 99 / BEL, classified by the provider
	UserSubmitted        Kind = "user_submitted"        // Enter or Esc from the writing client
)

type Signal struct {
	Kind   Kind      `json:"kind"`
	At     time.Time `json:"at"`
	Source Source    `json:"source"`
	Reason Reason    `json:"reason,omitempty"`
	Detail string    `json:"detail,omitempty"`
}

// Reduce is pure and table-tested.
func Reduce(current Activity, s Signal, now time.Time) Activity
```

### Reducer rules

| Signal | From | To |
| --- | --- | --- |
| `SessionStarted` | any | Idle(ready) |
| `PromptSubmitted`, `ToolStarted`, `TerminalBusy` | Unknown, Idle, Working | Working |
| `ToolStarted`, `TerminalBusy` | Waiting | *unchanged* (parallel tools must not hide a pending prompt) |
| `ToolFinished`, `InputResolved` | any | Working |
| `PermissionRequested` | any | Waiting(permission, tool) |
| `InputRequested` | any | Waiting(reason) |
| `TerminalNotification` | classified by the provider into one of the above; unclassified ones are dropped |
| `UserSubmitted` | Waiting | Working (`Inferred`): the user just answered |
| `UserSubmitted` | other | unchanged (the prompt hook will follow) |
| `TurnCompleted` | any | Idle(turn_complete) |
| `TurnFailed` | any | Idle(error, detail = error kind) |
| `Interrupted` | any | Idle(interrupted) |
| `TerminalClear` | Working | Idle(turn_complete, `Inferred`) only for providers that declare `ProgressMeansTurn` (agy) |
| `SessionEnded` | any | unchanged; lifecycle exit follows |
| timer: no signal for `StallAfter` (default 10 min) | Working | Working + `Stalled` |

Ordering: async hooks can arrive slightly out of order. Each signal carries the
time the hook command **started**; a hook signal older than the current state's
`Since` never moves the state backwards (it may only fill `Detail`).
Duplicates (Claude's `PermissionRequest` + `Notification:permission_prompt`)
reduce to the same state and produce no transition.

## 5. Architecture

```
                         ┌──────────────── holder process (bonsai __agent-host) ───────────────┐
 provider CLI ──hook──> bonsai __agent-hook ──append──> RuntimeDir/activity.jsonl              │
                         │                                   │ tail (fsnotify + poll)           │
 PTY output ────────────>│ OutputObserver (OSC/BEL) ──┐      ▼                                  │
 writer input ──────────>│ host "input" (Enter/Esc) ──┼──> Tracker ──Reduce──> Activity          │
 lifecycle (RunSession) ─│────────────────────────────┘       │                                  │
                         │                                    ├─> holder Bus (in-holder consumers)│
                         │                                    ├─> stream.Status (terminal clients)│
                         │                                    ├─> `activity` frame (status conns) │
                         │                                    └─> record <id>.json (transitions)  │
                         └────────────────────────────────────────────────────────────────────────┘
        status conn │                                      record watch │
                    ▼                                                   ▼
   agenthost.Manager (API) ──> API Bus ──> ws agent_activity      TUI (agentsapi.Changes): glyphs,
                                       └─> debounced snapshot     Agents tab; `bonsai agents ls`
```

### 5.1 Package `internal/core/agentactivity` (new)

Stdlib only: `internal/core/agents` imports it (§5.5), so it must not import
`agents`, providers, `agenthost` or `agentterminal`.

- `types.go`: `State`, `Reason`, `Source`, `Activity`, `Kind`, `Signal`.
- `reduce.go`: `Reduce` + rules above.
- `tracker.go`: `Tracker{activity, lastSignal, stallTimer}`;
  `Apply(Signal) (Activity, changed bool)`; owns the stall timer; injectable
  clock for tests.
- `observer.go`: `OutputObserver`, a stateful byte scanner (handles sequences
  split across reads, nested OSC, BEL inside OSC) reporting OSC `9;4;n`,
  OSC `9;text`, OSC `777;notify;…`, OSC `99`, lone BEL. Pure, no I/O.
- `spool.go`: `AppendSignals(path, []Signal)` (hook side) and `Tail` (holder
  side): reads from the last offset, keeps a partial trailing line, ignores
  malformed lines and lines > 4 KiB.
- `display.go`: `Display(lifecycle string, a *Activity) (key, label string)`.
- `bus.go`: the extension point:

```go
type EventType string

const (
	SignalReceived   EventType = "signal"            // every normalized signal (verbose, holder only)
	ActivityChanged  EventType = "activity_changed"  // transitions only
	LifecycleChanged EventType = "lifecycle_changed" // starting → running → exited …
)

type SessionRef struct {
	ID, ProjectID, WorktreeID, Name, Origin string
	Provider, AccountID                     string // agents.ProviderID / AccountID values
}

type Event struct {
	Type      EventType
	At        time.Time
	Session   SessionRef
	Signal    *Signal   // SignalReceived
	From, To  *Activity // ActivityChanged
	Lifecycle string    // LifecycleChanged: new lifecycle state
}

type Filter func(Event) bool

// Subscribe registers fn for matching events. Every subscriber has its own
// goroutine and bounded queue (256); a slow subscriber drops its oldest
// events and never blocks the publisher. cancel is idempotent.
func (b *Bus) Subscribe(name string, filter Filter, fn func(Event)) (cancel func())

// Ready-made filters.
func Became(states ...State) Filter // ActivityChanged into one of states
func Lifecycle(states ...string) Filter
func Project(id string) Filter
func All(filters ...Filter) Filter
```

The `Bus` type is the same in every process; there is one instance per
process that cares: one in each holder (its own session only) and one in the
API (fed by `Manager`, all sessions). The TUI does not need a bus: it re-reads
records on `Changes`.

Adding a feature later is one subscription, e.g. browser-side events in the API:

```go
bus.Subscribe("ws-activity", agentactivity.Became(agentactivity.Waiting, agentactivity.Idle),
	func(e agentactivity.Event) { s.broadcastActivity(e) })
```

### 5.2 Holder integration (`internal/core/agenthost`, `internal/core/agentterminal`)

- `agentterminal.Summary` gains `Activity *agentactivity.Activity
  json:"activity,omitempty"`, set only while `State == "running"`, cleared on
  every lifecycle change away from running. Additive JSON: no `RecordVersion`
  or protocol `Version` bump; old clients ignore it.
- `agentterminal.runInStream` gets an optional output hook (`func([]byte)`)
  called with each chunk before `stream.Write`; it never alters bytes.
  `RunSession` passes it through. The holder wires it to its `OutputObserver`.
- `host` gets a `Tracker` and a `Bus`. Signal sources inside the holder:
  - output observer → `TerminalBusy|TerminalClear|TerminalNotification`
    (notification classified by the provider's `TerminalClassifier`, else
    dropped);
  - `handle` case `"input"`: the data comes from the current writer only (the
    stream enforces it); `\r` or a lone `\x1b` → `UserSubmitted`. The TUI's
    `Ctrl+]` prefix never reaches the holder;
  - spool tail goroutine (§5.4), started after `PrepareSession`, stopped before
    `SessionStore.Cleanup` removes `RuntimeDir`;
  - lifecycle: `RunSession`'s `update` → `LifecycleChanged` on the holder bus.
- `host.applyActivity(sig)` under `h.mu`:
  - `Tracker.Apply`; nothing changed → return;
  - always: `stream.Status(summary)` and an `activity` frame
    (`Frame{Type: "activity", Status: &summary}`) to every status monitor,
    rate-limited to one per 250 ms per holder for Detail-only changes;
  - only when `Activity.Changed` (a transition): `writeRecord` and
    `bus.Publish(ActivityChanged)`. Detail-only changes never touch disk.
- Holder `Deps` gain `HookCommand []string` (default: `os.Executable()` +
  `__agent-hook`); the in-process test launcher (`InProcess`) passes a test
  helper instead, since `os.Executable()` is the test binary there.

### 5.3 Publication paths

- **`Manager` (API).** `watch` handles the new `activity` frame: updates
  `e.summary` and publishes `ActivityChanged`/no-op on the API bus, **without**
  `m.notify` (that is the heavy `publishAgents` path). `status` frames keep
  today's behaviour. Older holders never send `activity`; newer holders talking
  to an older API are ignored by `watch`'s default branch.
- **Records (TUI, `bonsai agents`).** The holder writes the record on every
  transition, so `agentsapi.Sessions.Changes` (TUI track A2 `agenthost.Watch`)
  fires and the TUI re-lists. Transitions are at most a few per minute per
  agent; Detail-only changes stay off disk.
- **localapi.** One bus subscription:
  - websocket event `agent_activity {project_id, session_id, activity}` sent
    immediately (same broadcast path as `agents_changed`, authenticated WS
    only);
  - debounced (500 ms per project) `stateSync.commit(…, "agents", …)` with
    `manager.List(project)` so bootstrap snapshots are right. It does **not**
    rewrite `api_agent_mutations`: activity is not persisted history.
  - `snapshotsSemanticallyEqual` keeps dropping no-op commits.
- Warm-start sanitization: `Activity.Detail` is a tool name or a ≤ 120-rune
  control-character-free string, never prompts, tool inputs or output.

### 5.4 Hook transport: `bonsai __agent-hook <provider> <event>`

A hidden top-level command, like `__agent-host`, handled in `main.go` before
CLI parsing so it stays fast (no daemon, no config load beyond the provider
registry).

1. Reads `$BONSAI_AGENT_ACTIVITY`; if unset, missing, not a regular file under
   the agent sessions root, or > 4 MiB: print `HookOutput`, exit 0.
2. Reads stdin (cap 1 MiB), calls the provider's `TranslateHook`.
3. Appends signals as one JSON line per signal with a single `write(2)` on an
   `O_APPEND|O_WRONLY` descriptor (never creates the file).
4. Prints `HookOutput(event)`, **always exits 0**. Total budget ~50 ms; hook
   config timeout 5 s so a hung disk can never stall the agent.

The holder creates `RuntimeDir/activity.jsonl` (0600) before `PrepareSession`
and tails it (fsnotify, 500 ms poll fallback).

Why a spool file and not a socket/HTTP:
- no token to distribute; the holder socket stays a clean client protocol;
  nothing exposed through the web tunnel;
- works for every handler type (Codex and agy only support `command`);
- independent of `bonsai web`; lines are tiny (≈ 200 B), carry no secrets,
  and die with `RuntimeDir` at cleanup;
- trivially testable (write lines, assert transitions).

### 5.5 Provider contract (optional interfaces, `internal/core/agents`)

The core stays provider-neutral (`docs/development.md` → "Agent provider
contract"): the holder never tests a provider ID. To keep `agents` free of an
import cycle, the hook-facing types live in `agentactivity` and `agents`
imports it (not the reverse).

```go
// ActivityRequest is set on PrepareSessionRequest when the holder wants
// activity. Nil means: install nothing.
type ActivityRequest struct {
	HookCommand []string // absolute argv of `bonsai __agent-hook <provider>`
	SpoolEnv    string   // env var name the hook command reads ("BONSAI_AGENT_ACTIVITY")
	SpoolPath   string   // RuntimeDir/activity.jsonl, created 0600 by the holder
}

type PrepareSessionRequest struct {
	Account  Account
	Session  Session
	Args     []string
	Launch   LaunchOptions
	Activity *ActivityRequest
}

// ActivityReporter is implemented by providers whose CLI can report activity.
// PrepareSession installs the hooks (flags, env, files) when req.Activity is set.
type ActivityReporter interface {
	ActivityProfile() ActivityProfile
	// TranslateHook turns one raw hook payload into signals. It runs inside the
	// hook command, so it must be fast and must never return tool inputs,
	// prompts or file contents, only kinds, reasons and short details.
	TranslateHook(event string, payload []byte) []agentactivity.Signal
	// HookOutput is what the hook command prints on stdout (agy needs "{}").
	HookOutput(event string) []byte
}

// TerminalClassifier maps a terminal notification to a signal (Codex
// "approval-requested", agy permission notice). Optional.
type TerminalClassifier interface {
	ClassifyTerminalNotification(title, body string) (agentactivity.Signal, bool)
}

type ActivityProfile struct {
	Native            []agentactivity.State // states reported by hooks
	Inferred          []agentactivity.State // states only inferred from the terminal
	ProgressMeansTurn bool                  // OSC 9;4 clear means the turn ended (agy)
}
```

`GET /api/agents/providers` gains `activity: {native, inferred}` so the web can
say "status inferred" for agy's waiting state.

### 5.6 Web

- `api/agents.ts`: `AgentSummary.activity`; `mapAgent` keeps `lifecycleState`
  and stores `activity`; `Agent.state` becomes `'running' | 'waiting' | 'idle'
  | 'finished'`.
- `lib/agentStatus.ts`: the single `agentDisplayStatus(agent) → {key, label,
  tone, pulse, inferred}`; every surface listed in §2 switches to one
  `<AgentStatusBadge>` / `<AgentStatusDot>` (replacing the agent use of
  `StatusDot`; process dots stay).
- `api/localClient.ts`: `agent_activity` listener patches one agent in the
  store (no refetch); stale events (older `since`) are dropped.
- Attention: worktree rows and canvas nodes show "needs input" prominently;
  `document.title` gets a `(n)` prefix while any agent waits.
- Counters ("running" in `buildCanvasGraph`, Inspector metrics) count
  working + waiting.

### 5.7 TUI and `bonsai agents`

- `agentBadge`: a session in Waiting renders its provider glyph in the
  attention style plus `!` (ASCII-safe), ahead of plain active glyphs; Idle
  sessions render dimmed. Width stays predictable (one extra cell at most).
- Agents tab session strip: label from `agentactivity.Display`, with the
  reason (`needs permission: Bash`) for Waiting.
- `bonsai agents ls`: `STATUS` column uses `Display` instead of the raw
  lifecycle.

## 6. Provider work

### Claude (`internal/providers/claude`)

- `launchArgs`: when `Activity` is set and the explicit args carry no
  `--settings`/`--bare`, append `--settings '<json>'` with command hooks
  (`async: true`, `timeout: 5`) for `SessionStart`, `UserPromptSubmit`,
  `PreToolUse` (`*`), `PermissionRequest`, `PostToolUse`, `PostToolUseFailure`,
  `Notification`, `Elicitation`, `ElicitationResult`, `Stop`, `StopFailure`,
  `SessionEnd`. Command: `'<bonsai>' __agent-hook claude <Event>`.
- Env: `BONSAI_AGENT_ACTIVITY=<spool>`. `CLAUDE*` scrubbing is unaffected.
- `TranslateHook`: `PreToolUse` → `ToolStarted`, except `AskUserQuestion` →
  `InputRequested(question)` and `ExitPlanMode` → `InputRequested(plan)`;
  `PostToolUse` of those → `InputResolved`; `Notification` by
  `notification_type`; `StopFailure` → `TurnFailed(detail = error kind)`.
- Interrupt (Esc) fires no hook: covered by `UserSubmitted` (Esc) while
  Waiting, and by the terminal progress clear if enabled; else the next
  prompt fixes it. Confirm in S0.

### Codex (`internal/providers/codex`)

- Profile `$CODEX_HOME/hooks.json`: Bonsai upserts its own entries (identified
  by the `__agent-hook codex` command), keeps any others, writes atomically
  (`writePrivateFile`). Events: `SessionStart`, `UserPromptSubmit`,
  `PreToolUse`, `PermissionRequest`, `PostToolUse`, `Stop`, `Interrupt`,
  `SessionEnd`; `async: true` except `SessionEnd`/`Interrupt` (always sync).
  The write happens under the account lock (`agents/account_lock.go`), since
  concurrent sessions of one account share the profile.
- **Trust**: `ensureHookTrust(account)` via `withAppServer`: `hooks/list`,
  take entries whose `sourcePath` is the profile `hooks.json` and whose
  command is Bonsai's, write `hooks.state."<key>".trusted_hash = currentHash`
  with `config/batchWrite`. Skipped when `config.toml` already holds the
  matching hashes (cheap TOML read), so the app-server runs only after the
  file changes (new Bonsai path/version). Also run at profile setup/reauth.
  If trust fails: launch anyway, activity degrades to terminal-only.
- Launch: `-c tui.notifications=["approval-requested","async-question"]
  -c tui.notification_method="osc9" -c tui.notification_condition="always"`
  for the terminal fallback (unless the explicit args set them).
- `TranslateHook`: `PreToolUse:request_user_input` → `InputRequested(question)`;
  `Interrupt` → `Interrupted`; `Stop` → `TurnCompleted`.
- `ClassifyTerminalNotification`: approval → `PermissionRequested`, question →
  `InputRequested`.

### Antigravity (`internal/providers/antigravity`)

- `PrepareSession` (after `credentialMgr.Materialize`): write
  `<session home>/.gemini/config/hooks.json` with one named group
  `"bonsai-activity"`: `SessionStart`, `PreInvocation`, `PostInvocation`,
  `PostToolUse` (`*`), `Stop`. **Never `PreToolUse`.** `FinalizeSession` /
  reconcile must not copy it back into the profile, and the plan-probe home
  never gets it.
- Turn on terminal notifications in the session's agy settings (key confirmed
  in S0) so permission prompts and questions reach the PTY.
- `HookOutput` → `{}` for every event (agy requires JSON; for `Stop`, anything
  but `"continue"` lets it stop).
- `TranslateHook`: `PreInvocation`/`PostInvocation`/`PostToolUse` → Working;
  `Stop` by `terminationReason`: `NO_TOOL_CALL` and friends → `TurnCompleted`,
  `ERROR` → `TurnFailed`, `USER_CANCELED` → `Interrupted`.
- `ActivityProfile{Inferred: [Waiting], ProgressMeansTurn: true}` +
  `ClassifyTerminalNotification`.

## 7. Phases

Integration branch `feat/agent-status`, cut from `feat/tui-agents` (the branch
furthest ahead of `main`; rebase on it as TUI phases merge); phase branches
`feat/agent-status-<phase>` with stacked PRs into it; the final PR targets
`feat/tui-agents` (or `main` if the TUI track has merged by then).
One concern per commit, no fixup commits. Each phase ends with `test-runner` on
the touched packages (`go test -race` for `agentactivity`, `agenthost`,
`agentterminal`, `localapi`), then `reviewer`.

```
S0 ── S1 ── S2 ──┬── S3 (Claude) ──┬── S5 (Codex)
                 │                  └── S6 (Antigravity)
                 ├── S4a (web)        ── needs S3 for real data, not for code
                 └── S4b (TUI/CLI)    ── needs TUI A2/A3 (record watch, agentsbackend)
                                         S7 (notifications) after S4a
```

### S0: Spike (evidence only, `docs/evidence/agent-status/`)

Throwaway profiles, a hook that just logs stdin + env, run inside a real
holder (`bonsai __agent-host`) so the env and `HOME` match production.
1. Claude: `--settings` hooks merge with the profile's own hooks; hook process
   inherits `BONSAI_AGENT_ACTIVITY`; exact event order for permission prompt,
   `AskUserQuestion`, plan approval, Esc interrupt, API error; whether
   `--settings` can be repeated.
2. Codex: profile `hooks.json` is discovered; `hooks/list` + `config/batchWrite`
   trust works with no "Hooks need review" dialog; `key`/hash stability across
   restarts and concurrent sessions of one account; `PermissionRequest` on
   `on-request`; `Interrupt`; OSC 9 text for each notification type.
3. agy: `~/.gemini/config/hooks.json` under the session `HOME` is honoured;
   `{}` is safe for every event; settings key that enables notifications and
   the exact bytes written on permission prompt / question; OSC 9;4 pattern
   across a turn.
4. Hook command latency through `bonsai __agent-hook` (target < 30 ms p95).

Exit: signal matrix (§3) confirmed or corrected; decisions recorded in this file.

### S1: `agentactivity` core (pure, no wiring)

Types, `Reduce`, `Tracker` (stall timer, injected clock), `OutputObserver`,
spool `AppendSignals`/`Tail`, `Display`, `Bus` + filters. Table tests for every
reducer row and ordering rule; observer tests for split sequences, nested OSC,
BEL in OSC; spool tests for partial lines, concurrent appenders, oversize and
malformed lines, missing file; bus tests for slow subscriber drop and
idempotent cancel. No other package changes.

Done when: package at full branch coverage of `Reduce`; `go test -race`
clean.

### S2: Holder integration (terminal + input only)

`Summary.Activity`; `runInStream` output hook; holder `Tracker`, observer,
`UserSubmitted` from `input`, holder bus with `LifecycleChanged`; `activity`
frame to status monitors; record write on transitions only; `Manager.watch`
consumes `activity` frames into an API bus without `notify`. Providers
untouched, so activity comes only from OSC 9;4 / input (Claude with the
progress bar on, agy) or stays `unknown`.

Tests: in-process holder (`InProcess`) with a fake provider whose script
prints OSC 9;4 sequences; assert summary activity over the status connection,
the record content, that Detail-only changes do not rewrite the record, and
that `publishAgents` is not called for activity frames.

### S3: Hook transport + Claude (first vertical slice)

`ActivityRequest`, `ActivityReporter`, `TerminalClassifier`, holder creates
and tails the spool, `__agent-hook` in `main.go`, `Deps.HookCommand`; Claude
hooks via `--settings`; extend `testdata/fakeclaude.sh` to call the hook
command so tests drive real transitions end to end through a holder. Update
the provider contract in `docs/development.md` and `docs/agents.md`.

### S4a: API publication + web

Bus subscriber in localapi (`agent_activity` WS event, debounced snapshot
commit); providers endpoint `activity` field; web status model,
`AgentStatusBadge`/`AgentStatusDot` across every surface in §2, store patching,
title counter; vitest units + one e2e (mock server emitting `agent_activity`).
Both stacks' checks; `build` + `bundle:check` (new module).

### S4b: TUI + `bonsai agents`

Requires TUI track A2 (records watch) and A3 (`agentsbackend`) merged.
`agentBadge` waiting/idle rendering (+ ASCII), Agents tab strip label,
`bonsai agents ls` STATUS column; update `internal/ui` goldens/tests with the
fake backend carrying `Activity`; `docs/tui-cli.md`.

### S5: Codex

`hooks.json` upsert under the account lock, trust via app-server (fake
app-server in `fakecodex.sh`), notification `-c` flags, translator,
classifier, trust at setup/reauth.

### S6: Antigravity

Session `hooks.json`, notifications setting, translator, classifier,
`ProgressMeansTurn`; probe home untouched.

### S7: Notifications (first consumer, proves the bus)

Decide where desktop notifications run. Recommended: in the **holder** (one per
session, always present, works without `bonsai web` and with the TUI), as a
holder-bus subscriber gated by a user preference read at holder start; the API
never sends desktop notifications, so there are no duplicates. Web: browser
`Notification` when the tab is hidden and an agent becomes Waiting or Idle;
per-type toggles in settings. Docs: `docs/agents.md`, `docs/web.md`.

## 8. Risks and decisions to watch

- **Hooks disabled by user policy** (`disableAllHooks`, managed-only hooks,
  `--bare`): activity falls back to terminal signals or stays `unknown`. Every
  client treats `unknown` as plain "Running", never as an error.
- **Mixed versions.** A holder keeps the binary that started it. Old holders
  never report activity (`unknown`); new holders' `activity` frames are ignored
  by old APIs. No protocol bump needed; covered by a manager test with a
  holder that sends unknown frame types.
- **Codex trust format changes**: only published app-server methods are used;
  failure degrades, never blocks launch.
- **Process spawn per tool call** (Claude/Codex `PreToolUse`/`PostToolUse`):
  async hooks, tiny binary work; measured in S0. If too costly, drop
  `PreToolUse` and rely on `PostToolUse` + `UserPromptSubmit`.
- **Bonsai binary path changes** (`bonsai update`, `go run`): hook commands
  embed the holder's `os.Executable()`. Claude/agy write it per session; Codex
  re-trusts on change; a session started before an update keeps its own path.
- **Record churn**: only transitions rewrite records; Detail updates travel
  over sockets only, rate-limited.
- **Snapshot churn**: activity never takes `publishAgents`; the debounced
  commit is the only snapshot path.
- **Privacy**: signals carry no prompts, tool inputs or outputs; `Detail` is a
  tool name or a ≤ 120-rune control-character-free string. Same rule for
  records, snapshots and warm-start data.
- **Spool lifetime**: the tail must stop before `SessionStore.Cleanup`; late
  hooks after cleanup find no file and exit 0.
- **agy waiting is inferred** until a hook exists; every client marks it.
- **TUI dependency**: S4b cannot start before TUI tracks A2/A3; S2's record
  writes make it a pure rendering change once they land.
