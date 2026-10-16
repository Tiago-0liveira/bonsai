# Track B (part 2): vterm input, replies, focus

Read first: `00-overview.md` (decisions 3, 4).
Branch `feat/tui-agents-b2` from `feat/tui-agents`, worktree `~/bonsai-tui-agents-b2`. Runs in parallel with A2 and C2.
**Only touch** `internal/ui/components/vterm/**` (and `go.mod`/`go.sum` if a dependency bump is unavoidable). No sockets, no key routing, no prefix handling: D4 wires those.

## Facts (verified at 1beaf2b)

- `internal/ui/components/vterm/vterm.go`: `Model{mu, emu *vt.Emulator, pipe io.Closer, focused, replaying, title, lines, dirty, closed, drainDone, closeOnce}`. `New(cols, rows)` starts `go m.drain()` which reads the emulator's input side forever and **discards** it today (`vterm.go:62-73`). `SetReplaying` exists but nothing reads the flag yet. `SetFocused` only affects cursor rendering.
- x/vt v0.1.0: the emulator's input side is a synchronous `io.Pipe`. Key encodings (`SendKey(uv.KeyEvent)`, `Paste`) and terminal replies (DSR/CPR, DA1/DA2, OSC 10/11, DECRQM) both come out of `emu.Read`. If nobody reads, a `Write` that triggers a reply blocks forever: the drain goroutine must keep reading no matter what.
- Bubble Tea v1.3.10 `tea.KeyMsg{Type, Runes, Alt, Paste}`; `tea.KeyCtrlCloseBracket` is the prefix key and is owned by the caller.
- TUI enables mouse cell motion (`main.go` comment near `tea.WithMouseCellMotion`): mouse forwarding is out of scope.

## B2

### Step 1 [you]: `Input()` channel
- `func (m *Model) Input() <-chan []byte`: drain goroutine copies each chunk from `emu.Read` and sends non-blocking into a bounded channel (cap 64); when full, drop the oldest chunk and increment a counter (`Dropped() uint64` for tests). Never blocks the drain.
- While `replaying` is true, the drain discards chunks instead of queueing them: replies produced during replay answer queries the agent made long ago and would inject garbage into its stdin.
- The caller (D4) forwards `Input()` chunks to the holder only when it is the writer, else drops them. Document that contract on `Input()`.
- `Close()` stops the drain (close the pipe), closes the channel once, waits for `drainDone`.

### Step 2 [you]: keys → bytes
`func (m *Model) SendKey(k tea.KeyMsg) bool` and `func (m *Model) SendPaste(s string)`:
- Translate `tea.KeyMsg` to `uv.KeyEvent` and call `emu.SendKey`, so the emulator applies the modes the agent enabled (DECCKM application cursor keys, keypad mode, kitty/xterm modifier encodings). Hand-encoding would break arrows in Claude Code / Codex.
- `k.Paste` → `SendPaste(string(k.Runes))` → `emu.Paste` (bracketed only when the app enabled 2004).
- Cover: printable runes incl. multi-rune IME input, enter, tab, shift+tab, backspace, delete, esc, arrows, home/end, pgup/pgdn, insert, F1-F12, ctrl+a..z, ctrl+space, alt+key, ctrl/alt/shift+arrows.
- `tea.KeyCtrlCloseBracket` → return false, encode nothing. Unknown key types → false.
- `SendKey` must not hold `m.mu` while the emulator writes to the pipe in a way that could deadlock with `Write` (check x/vt locking; the drain goroutine never takes `m.mu`).

### Step 3 [you]: focus events
`SetFocused(true/false)` also calls the emulator's focus/blur so `ESC[I` / `ESC[O` reach `Input()` when the app enabled mode 1004. One-line comment at the place mouse would be handled: mouse is owned by the TUI (cell motion), not forwarded.

### Step 4 [builder]: tests (`vterm_test.go`, table tests, `-race`)
- SendKey table: assert bytes read from `Input()` for each key, in normal mode and after feeding `ESC[?1h` (application cursor): up = `ESC[A` vs `ESC OA`. Alt modifiers, ctrl letters, F-keys, shift+tab.
- Paste with and without `ESC[?2004h`.
- `KeyCtrlCloseBracket` → false, nothing on `Input()`.
- `Write("\x1b[6n")` with nobody reading `Input()` returns promptly 1000× (proves no blocking); channel never exceeds cap; `Dropped()` grows.
- Replay suppression: `SetReplaying(true)`, write `ESC[6n` → nothing on `Input()`; `SetReplaying(false)`, write `ESC[6n` → `ESC[<row>;<col>R`.
- Focus: with `ESC[?1004h`, `SetFocused(true)` → `ESC[I`; without it → nothing.
- `Close` with a pending `Input()` reader and with none: returns, channel closed, no goroutine leak.

### B2 done when
`go test -race -count=3 ./internal/ui/components/vterm/...`, benchmarks not worse than B1 (B1 shipped replay ~330 ms / 2 MiB vs a 150 ms target, cost in x/vt `ScrollUp`; View < 2 ms at 120×40). If D3 manual testing shows a visible replay stall, profile and fix then, not here, global checks. reviewer (ask: no blocking path in `Write`/`SendKey`; no goroutine leak on `Close`; replay replies never surface). Commit `feat(ui): encode terminal input and replies in vterm`.
