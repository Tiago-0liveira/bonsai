# Plan: bonsai depth indicator 🪴N

Goal: when the TUI opens a shell, the user can see how many bonsais deep they are. The indicator is a small glyph plus a number in the shell prompt, for example `🪴2`.

## Decisions

- Approach: hybrid. Bonsai injects the prompt prefix automatically by default. An opt-in snippet (`bonsai shell-init`) and starship/p10k recipes are the fallback. A pref turns auto-injection off.
- Counting: number of bonsai shells deep. The first bonsai shell shows `🪴1`. A shell opened from a bonsai inside that shell shows `🪴2`. Outside bonsai, nothing shows.
- Shells: as many as possible. bash, zsh and fish first. Then sh/dash/ksh/mksh, nu, pwsh and cmd as best effort. Windows support is best effort.
- Glyph: configurable. The default is 🪴 (Unicode has no bonsai emoji).
- Position: left prefix of the prompt.
- Also shown: TUI header (nested bonsai only) and an exit banner when a shell returns to the TUI.

## Mechanism

Each bonsai shell gets `BONSAI_DEPTH=<parent+1>`. Bonsai starts the shell with a small startup script (shim). The shim loads the user's normal rc files first, then adds `🪴N ` in front of the prompt. VS Code, kitty and ghostty use the same technique for their shell integration.

## Phase 1: depth plumbing (every OS, no prompt changes)

- New package `internal/core/shellinit`:
  - `Depth()` reads `BONSAI_DEPTH`. A missing, invalid or negative value counts as 0. The value is capped at 99.
  - `Prefix(glyph, depth)` builds the prefix string, for example `🪴2 `.
- `coreexec.ShellCmd` (`internal/core/exec/exec.go`) sets `BONSAI_DEPTH=Depth()+1` and `BONSAI_PROMPT_PREFIX` on the child env only, not on bonsai's own env. The daemon, `spawn` processes and agent PTYs do not increment the depth, but can inherit these variables when bonsai runs inside a nested shell.
- TUI header: a bonsai started from inside a bonsai shell shows `🪴N`, where N = `Depth()+1`. It shows only when N >= 2. The depth is computed in core and passed into the model, so the TUI does no env access.
- Exit banner: on `opDoneMsg{label:"shell"}` (`openShell` in `internal/ui/update.go`), the TUI shows a status line such as `back in bonsai 🪴1`. The shell can't print this itself: the TUI redraws its alt screen on resume, so any line the shell prints disappears at once.
- New personal prefs in `internal/core/config/state.go` `Prefs`, next to `AgentGlyphs`:
  - `DepthGlyph` (`""` means 🪴).
  - `ShellPrompt` (`""` means auto, `"off"` disables injection).

## Phase 2: auto prompt injection (bash, zsh, fish)

- Shim scripts are embedded with `go:embed` and written once per version to `os.UserCacheDir()/bonsai/shell-integration/<version>/`. Nothing is written per launch.
- The shell is detected from the basename of `$SHELL`, with `.exe` stripped.

| Shell | Launch | Hook |
| --- | --- | --- |
| bash | `bash --rcfile shim.bash` | Sources `~/.bashrc`. Then appends a function to the end of `PROMPT_COMMAND` that puts the prefix in front of `PS1` when it is missing. This also works with starship, which rebuilds `PS1` every time. |
| zsh | `ZDOTDIR=<shim dir>`, original saved in `BONSAI_ORIG_ZDOTDIR` | The shim `.zshenv` and `.zshrc` restore the real `ZDOTDIR` and source the user's files. Then they add a hook as the last entry of `precmd_functions`, so it runs after p10k and oh-my-zsh rebuild `PROMPT`. Child processes see the real `ZDOTDIR`, so a nested bonsai keeps working. |
| fish | `fish -C 'source shim.fish'` | `-C` runs after `config.fish`. The shim copies `fish_prompt` and wraps it so the prefix prints first. |

- Guard: the shim skips its hook when `__BONSAI_PROMPT_INSTALLED` is already set, so the prefix never shows twice. The opt-in snippet sets this variable.
- With `ShellPrompt="off"`, bonsai starts a plain shell that only gets the env vars.

## Phase 3: more shells and the opt-in path

- sh, dash, ksh, mksh: set `ENV=shim.sh`. The shim sources the user's original `$ENV`, then prefixes `PS1`. POSIX shells read `$ENV` when interactive.
- nu: `nu -e` with a closure that wraps the existing `PROMPT_COMMAND`.
- pwsh: `pwsh -NoExit -Command` that dot-sources `$PROFILE` and wraps `function prompt`. This also covers Windows.
- cmd.exe: best effort with `cmd /K prompt <prefix>%PROMPT%`. Emoji rendering in cmd is unreliable.
- Unknown shells get the env vars only.
- New command `bonsai shell-init <bash|zsh|fish|nu|pwsh>`. It prints a snippet for the user to `eval` in their rc file. The snippet installs the same hook and sets the guard.
- Docs add a starship `[custom.bonsai]` module and a p10k `prompt_bonsai` segment for themes that fight the auto hook. Users of these set `ShellPrompt="off"`.

## Tests

- Table tests: depth parsing, shell detection, and the args and env built for each shell.
- Integration tests for each installed shell, skipped when `exec.LookPath` can't find it. They use `HOME`, `ZDOTDIR` and rc files under `t.TempDir()` (never the real home). They run the shim non-interactively and assert that:
  - the prompt variable starts with the prefix;
  - the user's own `PS1`/`PROMPT` survives after the prefix;
  - nesting produces `🪴2`;
  - the guard prevents a double prefix.
- TUI tests for the header indicator and the exit banner.

## Docs

- `README.md`: short mention, linking to the docs.
- `docs/tui-cli.md`: full section covering prefs, `bonsai shell-init`, starship and p10k recipes, and how to turn the feature off.

## Known limits

- Running `exec zsh` inside a bonsai shell drops the hook, because the real `ZDOTDIR` is restored. The env vars stay, so the snippet path still works.
- p10k instant prompt can briefly show the prompt without the prefix until the first hook runs.
- Some fonts render 🪴 at the wrong width. The `DepthGlyph` pref fixes that.

## Rollout

1. Phase 1: low risk and useful on its own (env var, header, banner, prefs).
2. Phase 2: covers most users (bash, zsh, fish auto-injection).
3. Phase 3: can ship separately (other shells, `shell-init`, recipes).
