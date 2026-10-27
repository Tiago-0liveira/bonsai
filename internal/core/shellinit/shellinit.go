// Package shellinit tracks how deeply bonsai-spawned shells are nested, so a
// shell opened from the TUI can show that it is a child of bonsai.
package shellinit

import (
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

const (
	// EnvDepth holds the nesting level of bonsai shells.
	EnvDepth = "BONSAI_DEPTH"
	// EnvPromptPrefix holds the ready-made prompt prefix for the child shell.
	EnvPromptPrefix = "BONSAI_PROMPT_PREFIX"
	// DefaultGlyph is used when no glyph is configured.
	DefaultGlyph = "🪴"
	// envOrigZdotdir and envOrigEnv carry the user's ZDOTDIR/ENV past the
	// zsh and POSIX shims, which put them back.
	envOrigZdotdir = "BONSAI_ORIG_ZDOTDIR"
	envOrigEnv     = "BONSAI_ORIG_ENV"
	// envGuard is the shims' install guard; only nu exports it.
	envGuard = "__BONSAI_PROMPT_INSTALLED"
	// MaxDepth caps the depth so a corrupt value cannot grow the prompt.
	MaxDepth = 99
)

// ScrubEnv returns env without the variables bonsai sets for nested shells.
// Detached processes (daemon, agent host) outlive the shell that started them
// and are shared by every caller, so they must not carry its depth.
func ScrubEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if !slices.ContainsFunc(scrubbed, func(s string) bool { return envKeyEqual(k, s) }) {
			out = append(out, kv)
		}
	}
	return out
}

var scrubbed = []string{EnvDepth, EnvPromptPrefix, envOrigZdotdir, envOrigEnv, envGuard}

// envKeyEqual compares env names the way the OS does: Windows ignores case.
func envKeyEqual(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// Depth returns the current bonsai nesting level from the environment
// (0 when not inside a bonsai shell).
func Depth() int { return DepthFrom(os.Getenv(EnvDepth)) }

// DepthFrom parses a raw depth value: missing, invalid or negative is 0, and
// anything above MaxDepth is capped.
func DepthFrom(v string) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 0 {
		return 0
	}
	return min(n, MaxDepth)
}

// maxGlyphBytes caps the glyph length; the longest legitimate choice is an
// emoji (up to ~4 bytes) or a short word.
const maxGlyphBytes = 16

// safeGlyph reports whether glyph can be embedded verbatim in every shell's
// prompt. state.json is hand-editable, and the prefix reaches bash PS1
// (backslash escapes, $(...), backticks, history "!"), zsh PROMPT ("%" and
// PROMPT_SUBST), and cmd's prompt ("$" codes); quotes and control characters
// would also break the hooks' quoting and "already prefixed?" check. cmd runs
// `cmd /K "prompt <prefix>..."`, strips the outer quotes and still splits on
// "& | < > ^ ( )", so those would run a hand-edited glyph as a command.
func safeGlyph(glyph string) bool {
	if glyph == "" || len(glyph) > maxGlyphBytes {
		return false
	}
	for _, r := range glyph {
		if strings.ContainsRune("\\$%`\"'!&|<>^()", r) || unicode.IsSpace(r) || unicode.IsControl(r) || !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// Prefix returns the prompt prefix "<glyph><depth> ", or "<glyph>:<depth> "
// when the glyph ends in an ASCII letter or digit. An empty or unsafe glyph
// uses DefaultGlyph.
func Prefix(glyph string, depth int) string {
	if !safeGlyph(glyph) {
		glyph = DefaultGlyph
	}
	// Without a separator a text glyph runs into the number ("bonsai2").
	sep := ""
	if last := glyph[len(glyph)-1]; last >= '0' && last <= '9' || last >= 'a' && last <= 'z' || last >= 'A' && last <= 'Z' {
		sep = ":"
	}
	return glyph + sep + strconv.Itoa(depth) + " "
}
