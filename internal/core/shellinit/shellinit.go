// Package shellinit tracks how deeply bonsai-spawned shells are nested, so a
// shell opened from the TUI can show that it is a child of bonsai.
package shellinit

import (
	"os"
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
	// MaxDepth caps the depth so a corrupt value cannot grow the prompt.
	MaxDepth = 99
)

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
