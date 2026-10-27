// Package shellinit tracks how deeply bonsai-spawned shells are nested, so a
// shell opened from the TUI can show that it is a child of bonsai.
package shellinit

import (
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
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

// Prefix returns the prompt prefix "<glyph><depth> ". An empty glyph uses
// DefaultGlyph.
func Prefix(glyph string, depth int) string {
	if glyph == "" {
		glyph = DefaultGlyph
	}
	return glyph + strconv.Itoa(depth) + " "
}
