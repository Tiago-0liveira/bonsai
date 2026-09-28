# PTY backend plan

Branch: `feat/daemon-pty-plan`  
Base: `main`

## Goal

Add daemon-owned pseudo-terminal (PTY) sessions to Bonsai so interactive processes can run under the existing daemon supervision model and later be streamed to a web client.

This phase is **backend only**. It must not add an HTTP server, WebSocket implementation, or web-client integration yet.

The target end-state is:

```text
xterm.js
   ⇅
future WebSocket connector
   ⇅
PTYAttachment client API
   ⇅
Bonsai daemon protocol
   ⇅
PTY session hub
   ⇅
Unix PTY / Windows ConPTY
   ⇅
shell / Codex / Claude / arbitrary interactive command
```

The daemon owns terminal lifetime and process supervision. A future web server should only transport terminal events.

---

## Current state on main

The current daemon already provides most of the lifecycle infrastructure PTY sessions need:

- `internal/daemon/server` owns background processes per repository.
- `internal/daemon/protocol` is newline-delimited JSON and is currently protocol version 2.
- `internal/daemon/client` provides daemon autostart, compatibility checks, process operations, log streaming, and attach.
- `internal/core/procstore` persists process metadata and logs under `<repo>/.bonsai/`.
- `internal/daemon/server/proc.go` already owns restart policies, process-tree termination, orphan reconciliation, status transitions, and exit handling.
- `internal/daemon/server/logwriter.go` provides bounded rotating logs.
- `main` supports both shell-command spawning and structured `Program + Args + WorkingDir` spawning.
- `Attach` currently aliases log following. It is intentionally separated from `Logs`, which gives us a natural place to introduce real interactive semantics.

Do not create a second independent terminal-process manager. PTY sessions should use the existing daemon process model.

---

## Design principles

### 1. The daemon owns the PTY

The PTY master or ConPTY handle must live in the daemon, not in the CLI or future web server.

A client disconnect must not terminate the child process or destroy the terminal session.

### 2. One reader per PTY

There must be exactly one goroutine reading from the PTY master.

Multiple attached clients must subscribe to copies of the output. They must never read directly from the PTY, otherwise concurrent viewers would split the byte stream between themselves.

### 3. Logs and live terminal output are different concerns

Live PTY output is an ordered raw byte stream.

Durable process history should continue to use the existing rotating `logWriter`.

The daemon should tee every PTY output chunk into both:

- the durable log writer;
- the live subscriber hub.

### 4. Keep transport concerns outside the PTY implementation

The PTY backend must not know about WebSockets.

Expose a daemon/client API that can later be adapted to WebSockets, SSH, a native GUI, or another transport.

### 5. Preserve structured spawning

PTY support must work with the existing `Program + Args + WorkingDir` model on `main`.

Interactive tools discovered by Bonsai should prefer structured argv over shell interpolation.

### 6. Preserve cross-platform support

Bonsai currently supports Unix and Windows process behavior.

PTY support must account for:

- classic Unix PTYs;
- Windows ConPTY.

Platform-specific code should be hidden behind a Bonsai-owned interface.

---

# Phase 0 — branch hygiene

Before implementation starts, ensure the PTY implementation branch is based on current `main`.

Do not base the implementation on `feat/web-frontend-prototype`.

That branch currently contains useful UI experimentation, but the daemon work belongs on top of the authoritative process/protocol implementation from `main`.

---

# Phase 1 — add a Bonsai PTY abstraction

Create:

```text
internal/core/pty/
    pty.go
    pty_unix.go
    pty_windows.go
    pty_test.go
```

Define a Bonsai-owned interface similar to:

```go
type Session interface {
    Read([]byte) (int, error)
    Write([]byte) (int, error)
    Resize(cols, rows int) error
    Size() (cols, rows int, err error)
    Close() error
}

type Process interface {
    Session() Session
    Wait(context.Context) error
}
```

Exact names can change during implementation, but the daemon should depend on this package rather than directly on a third-party PTY type.

The package should expose an operation equivalent to:

```go
Start(cmd *exec.Cmd, cols, rows int) (Process, error)
```

### Unix requirements

The child must actually run with a controlling terminal.

The implementation must validate:

- stdin is a TTY;
- stdout is a TTY;
- stderr is a TTY;
- terminal dimensions can be queried and changed;
- control characters behave like a normal interactive terminal.

Do not approximate this by wiring pipes to stdin/stdout.

### Windows requirements

Use ConPTY through a maintained library or platform wrapper.

Keep ConPTY details inside `internal/core/pty`.

The daemon must not need Windows-specific branching for reads, writes, resize, or wait behavior.

### Dependency choice

A cross-platform library such as Charm's `xpty` is a reasonable starting point because it abstracts Unix PTY and Windows ConPTY behind one API.

However, keep it behind Bonsai's own interface so dependency replacement does not leak through the daemon.

---

# Phase 2 — represent PTY mode in procstore

Extend `procstore.Record` with process I/O mode.

Suggested shape:

```go
const (
    IOModePipe = "pipe"
    IOModePTY  = "pty"
)

type Record struct {
    // existing fields...

    IOMode string `json:"io_mode,omitempty"`

    PTYCols int `json:"pty_cols,omitempty"`
    PTYRows int `json:"pty_rows,omitempty"`
}
```

Existing records with no `IOMode` must behave as `pipe` for backwards compatibility.

Do not create a separate terminal registry.

The existing process ID is also the PTY session ID.

That keeps these concepts unified:

- list;
- status;
- kill;
- remove;
- restart;
- worktree ownership;
- branch ownership;
- persistent metadata;
- daemon idle behavior;
- global daemon discovery.

### Restart policy default

Interactive PTY sessions should normally default to `PolicyNo`.

A shell, Codex session, Claude session, or REPL should not automatically restart after the user exits unless explicitly requested.

---

# Phase 3 — extend the spawn request

Extend protocol spawn fields with PTY options.

Suggested request fields:

```go
PTY     bool `json:"pty,omitempty"`
PTYCols int  `json:"pty_cols,omitempty"`
PTYRows int  `json:"pty_rows,omitempty"`
```

Defaults:

- cols: 80
- rows: 24

Preserve both current invocation styles:

### Shell form

```text
Command
Worktree
WorkingDir
```

### Structured form

```text
Program
Args
Worktree
WorkingDir
```

Prefer structured form for web-driven/discovered commands.

Add client APIs such as:

```go
SpawnPTY(...)
SpawnPTYExec(...)
```

They should parallel the existing `Spawn` and `SpawnExec` methods.

---

# Phase 4 — add PTY state to managedProc

Extend `managedProc` in `internal/daemon/server/server.go`.

Suggested state:

```go
type managedProc struct {
    // existing fields...

    ptySession pty.Session
    ptyHub     *ptyHub
}
```

Do not replace the existing pipe-based path.

`start()` should branch based on the record's I/O mode.

### Pipe process

Keep current behavior:

```text
cmd stdout/stderr -> logWriter
```

### PTY process

Use:

```text
cmd stdin/stdout/stderr -> PTY slave / ConPTY
PTY master -> one daemon pump
daemon pump -> logWriter + subscriber hub
```

The PTY handle belongs to `managedProc` for the current generation.

A restart must create a fresh PTY.

---

# Phase 5 — implement the PTY output pump

Add a new server file:

```text
internal/daemon/server/pty.go
```

Implement one output goroutine per active PTY.

Conceptually:

```go
for {
    n, err := session.Read(buf)
    if n > 0 {
        chunk := copyBytes(buf[:n])

        logWriter.Write(chunk)
        hub.Publish(chunk)
    }

    if err != nil {
        break
    }
}
```

Requirements:

- never expose the read buffer directly after reuse;
- preserve byte ordering;
- do not decode UTF-8 in the daemon;
- do not strip ANSI sequences;
- do not split by line;
- continue feeding the existing log writer;
- cleanly close subscribers when the session ends;
- coordinate with `Wait` so PTY EOF and process exit cannot leak goroutines.

Terminal data is a byte stream, not a log-line stream.

---

# Phase 6 — introduce ptyHub

Implement a per-session broadcaster owned by the daemon.

Suggested responsibilities:

```go
type ptyHub struct {
    // subscriber registry
    // output sequence counter
    // bounded replay buffer
    // serialized input writes
    // last known terminal size
    // closed state
}
```

It should support operations equivalent to:

```go
Subscribe(afterSeq uint64) *Subscription
Publish(data []byte)
WriteInput(data []byte) error
Resize(cols, rows int) error
Close(exitInfo ...)
```

## Subscriber rules

Each subscriber receives every output chunk after its subscription point.

A slow subscriber must not block the PTY reader indefinitely.

Use bounded queues.

If a subscriber cannot keep up, terminate that subscriber rather than allowing one browser tab to stall the process.

## Sequence numbers

Assign monotonically increasing output sequence numbers.

Example:

```json
{
  "seq": 418,
  "data": "..."
}
```

Sequence IDs make later WebSocket reconnect behavior straightforward.

## Replay buffer

Maintain a bounded in-memory replay ring for active PTYs.

Target a modest default such as hundreds of KiB per PTY, not many MiB.

This system is expected to manage many projects, so memory cost must scale predictably.

The rotating process log remains the durable history.

The replay buffer exists only to bridge short client disconnects.

---

# Phase 7 — turn Attach into a real interactive protocol

The current attach path is output-only log streaming.

PTY attach needs a full-duplex daemon connection.

Bump:

```go
protocol.Version
```

from 2 to 3 because connection semantics become meaningfully different.

Keep `Logs` unchanged.

Introduce explicit PTY request/event kinds.

Possible client-to-daemon frames:

```text
ptyAttach
ptyInput
ptyResize
ptyDetach
```

Possible daemon-to-client frames:

```text
ptyAttached
ptyOutput
ptyExit
ptyError
```

A reasonable attach handshake:

```json
{
  "kind": "ptyAttach",
  "id": 12,
  "after_seq": 400
}
```

The server responds with metadata:

```json
{
  "kind": "ptyAttached",
  "id": 12,
  "cols": 120,
  "rows": 34,
  "next_seq": 401
}
```

Then output frames continue until detach or process exit.

Input frame:

```json
{
  "kind": "ptyInput",
  "data": "..."
}
```

Resize frame:

```json
{
  "kind": "ptyResize",
  "cols": 120,
  "rows": 34
}
```

Use byte-oriented fields in the Go protocol rather than assuming terminal data is valid text.

If represented as `[]byte` in JSON, standard encoding can safely preserve arbitrary bytes.

Do not reuse `LogChunk string` for PTY data.

---

# Phase 8 — add a high-level client attachment API

The future web server should not manually construct daemon protocol frames.

Add a client abstraction roughly like:

```go
type PTYEvent struct {
    Seq  uint64
    Data []byte

    // optional lifecycle fields
}

type PTYAttachment interface {
    Events() <-chan PTYEvent
    Write([]byte) error
    Resize(cols, rows int) error
    Close() error
}
```

Client usage should look approximately like:

```go
att, err := client.AttachPTY(ctx, processID, afterSeq)
if err != nil {
    return err
}
defer att.Close()

go func() {
    for event := range att.Events() {
        // future websocket writer
    }
}()

att.Write(userInput)
att.Resize(cols, rows)
```

This API is the contract the future connector should consume.

The future web transport should not depend on:

- socket paths;
- procstore internals;
- NDJSON details;
- daemon autostart details;
- PTY library types.

---

# Phase 9 — process lifecycle integration

PTY sessions must participate in the existing state machine.

## Kill

Explicit Bonsai kill should continue using process-tree termination.

Do not convert a "Stop" operation into writing Ctrl-C.

## Ctrl-C

A terminal keypress that produces byte `0x03` should be written through the PTY.

The terminal driver should deliver SIGINT naturally to the foreground process group on Unix.

This is different from the Bonsai process-manager kill command.

## Exit

When the child exits:

1. wait for the process;
2. drain remaining PTY output;
3. close the PTY;
4. publish the final exit event;
5. close live subscribers;
6. run the existing `onExit` status/restart state machine.

Carefully avoid racing PTY EOF with `cmd.Wait`.

## Restart

Restart must:

1. stop the existing generation;
2. close its PTY and hub;
3. increment the generation as today;
4. create a brand-new PTY;
5. restore the last known cols/rows;
6. start the command;
7. expose the new generation to future attachments.

Do not reuse the previous PTY handle.

## Remove

A PTY process must be terminal before normal removal, matching existing process semantics.

Removing it must release:

- PTY handle;
- output pump;
- subscriber hub;
- record;
- logs.

---

# Phase 10 — daemon shutdown and idle semantics

An attached client connection must not itself keep the daemon alive forever after the underlying process is gone.

Active PTY processes count as active processes exactly like existing supervised processes.

The current idle shutdown mechanism should therefore remain process-based rather than connection-based.

On forced daemon shutdown:

- terminate managed processes;
- close PTYs;
- close hubs/subscriptions;
- remove the daemon socket as today.

---

# Phase 11 — define daemon-crash behavior explicitly

There are two very different failure cases.

## Client/web connection dies

The daemon remains alive and owns the PTY.

Expected behavior:

- process continues;
- PTY remains valid;
- output continues to be logged;
- replay buffer accumulates recent output;
- client can reconnect and attach again.

This is a primary requirement.

## Daemon process dies

The daemon loses the PTY master/ConPTY handle.

The OS child may remain alive, but Bonsai can no longer reconstruct the interactive terminal simply from its PID.

For the first PTY implementation:

- retain existing orphan/lost process reconciliation;
- mark the process appropriately;
- do not claim that interactive reattachment is available after daemon death.

True terminal survival across daemon restarts would require a persistent intermediary comparable to tmux/screen and is out of scope.

---

# Phase 12 — log behavior

Continue using `logWriter` for persistent transcript data.

PTY output should be written exactly as received.

Keep:

- ANSI escape codes;
- carriage returns;
- control sequences;
- progress redraws.

Do not sanitize terminal output before writing it.

This means plain `bonsai logs` may contain terminal control sequences for PTY processes. That is acceptable for the first implementation and preserves fidelity.

If a normalized text log is wanted later, derive it separately instead of corrupting the terminal stream.

Lifecycle markers may continue to be written to the durable Bonsai log, but should not be injected into the raw live PTY byte stream.

Live attach should send lifecycle events as structured protocol messages.

---

# Phase 13 — security and correctness constraints

## Working directory validation

Continue preserving the distinction between:

- owning worktree;
- actual working directory.

A nested package may run from `apps/web` while still belonging to the repository worktree.

## Structured argv

Use `Program + Args` whenever the caller already has structured invocation data.

Do not round-trip structured commands through `sh -c`.

## Terminal size validation

Reject nonsensical dimensions.

Use conservative bounds such as:

- cols > 0;
- rows > 0;
- upper limit to prevent pathological allocations/OS calls.

## Backpressure

Never allow a slow terminal viewer to block:

- process output;
- log writing;
- other subscribers.

## Input serialization

Multiple clients may eventually attach to one PTY.

All writes to the PTY should be serialized through the hub.

For the initial implementation, multiple writers may be permitted, but this should be a deliberate behavior.

A future layer can add exclusive-control ownership without changing the PTY abstraction.

---

# Phase 14 — tests

Add focused tests before any WebSocket work begins.

## Core PTY tests

Verify:

- child sees stdin as a terminal;
- child sees stdout as a terminal;
- child sees stderr as a terminal;
- bytes written to PTY reach the process;
- process output reaches the PTY reader;
- resize changes terminal dimensions;
- close unblocks reads;
- non-zero exits are reported correctly.

Run platform-specific coverage where CI supports it.

## Daemon integration tests

Add tests under `internal/daemon/server`.

### Spawn

Spawn a helper under PTY mode and assert:

- record says PTY;
- correct worktree is retained;
- correct nested working directory is retained;
- structured argv is preserved;
- initial dimensions are persisted.

### Input/output

Start a helper that echoes raw input.

Assert:

- write input through client attachment;
- daemon forwards it;
- output event arrives;
- exact bytes are preserved.

### ANSI

Emit ANSI escape sequences and assert exact byte preservation.

### Resize

Start a helper that reports its terminal size.

Resize through the attachment API and assert the helper observes the new dimensions.

### Disconnect

Attach, disconnect, leave the child running, reconnect, and verify:

- same process PID/session remains active;
- terminal is still usable;
- recent missed output is replayed.

### Multiple observers

Attach two subscribers.

Emit output once.

Assert both receive the same bytes.

This specifically guards against accidentally allowing multiple readers on the PTY master.

### Slow observer

Create a subscriber that stops consuming.

Generate enough output to overflow its queue.

Assert:

- PTY process continues;
- logger continues;
- healthy subscriber continues;
- slow subscriber is dropped.

### Kill

Kill a PTY process through Bonsai.

Assert:

- process tree terminates;
- PTY read loop exits;
- subscribers receive terminal lifecycle completion;
- process reaches `stopped`.

### Natural exit

Let a PTY process exit normally.

Assert:

- all final bytes are drained before exit event;
- status becomes `done`;
- no goroutines remain blocked.

### Failed exit

Exit non-zero.

Assert existing failure/restart policy behavior still works.

### Restart

Restart a terminal process.

Assert:

- old PTY closes;
- new generation uses a new PTY;
- same invocation metadata is retained;
- terminal dimensions are retained;
- attachment to the new session works.

### Rotation

Produce enough PTY output to rotate logs.

Assert:

- live terminal subscribers do not lose output because of log rotation;
- combined durable log remains readable.

### Protocol compatibility

Verify a version-2 client cannot silently interpret the version-3 interactive protocol.

Keep the existing daemon compatibility policy: an incompatible daemon with active processes must not be destructively replaced.

---

# Phase 15 — future WebSocket contract, not implementation

Do not implement this phase as part of the PTY backend work.

The backend should make the future layer trivial.

Likely mapping:

```text
WebSocket binary/text input
        ↓
PTYAttachment.Write

browser resize event
        ↓
PTYAttachment.Resize

PTYAttachment event
        ↓
WebSocket binary output
```

WebSocket is preferable to SSE for this use case because terminal sessions require full-duplex low-latency communication.

The web connector may later multiplex many terminal sessions over one authenticated WebSocket, using fields such as:

```text
project ID
process ID
session ID
sequence ID
message kind
```

That multiplexing belongs above the daemon client API.

The daemon should remain unaware of projects outside its own repository and unaware of browser transport.

---

# Suggested implementation order

1. Merge/rebase implementation work onto current `main`.
2. Add `internal/core/pty`.
3. Add PTY metadata to `procstore.Record`.
4. Extend spawn protocol with PTY options.
5. Add `SpawnPTY` / `SpawnPTYExec`.
6. Add PTY fields to `managedProc`.
7. Implement PTY start path.
8. Implement the single PTY reader/output pump.
9. Implement `ptyHub` with bounded subscribers and replay.
10. Implement input and resize.
11. Add interactive attach protocol.
12. Bump protocol version to 3.
13. Add `PTYAttachment` to daemon client.
14. Integrate kill/exit/restart/remove/shutdown.
15. Add daemon integration tests.
16. Add Windows-specific tests/build coverage.
17. Only after the backend is stable, design the WebSocket connector.

---

# Definition of done

The PTY backend phase is complete when all of the following are true:

- Bonsai can spawn an interactive process under a real PTY/ConPTY.
- The daemon remains the sole owner of the terminal.
- Input can be written to the running terminal.
- Terminal dimensions can be changed while running.
- Raw output can be streamed to multiple subscribers.
- Temporary client disconnects do not terminate the terminal.
- A reconnect can replay recent missed output using sequence IDs.
- All output is still persisted through the existing rotating log system.
- Existing kill, restart, remove, status, and restart-policy behavior still works.
- Structured `Program + Args + WorkingDir` spawning is preserved.
- Linux/macOS behavior works.
- Windows ConPTY behavior builds and is covered as far as CI permits.
- The daemon client exposes a transport-agnostic attachment API.
- No WebSocket or HTTP implementation exists yet.
