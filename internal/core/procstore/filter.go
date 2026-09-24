package procstore

import (
	"strings"
)

// FilterLog applies tail and grep filtering to a log string.
func FilterLog(data string, tailLines int, grep string, insensitive bool) string {
	res := data
	if tailLines > 0 {
		res = LastLines(res, tailLines)
	}
	if grep != "" {
		res = FilterGrep(res, grep, insensitive)
	}
	return res
}

// LastLines returns the last n lines of s.
func LastLines(s string, n int) string {
	if n <= 0 || s == "" {
		return s
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n") + "\n"
}

// FilterGrep keeps only lines containing pattern (empty pattern = keep all).
func FilterGrep(s, pattern string, insensitive bool) string {
	if pattern == "" {
		return s
	}
	pat := pattern
	if insensitive {
		pat = strings.ToLower(pat)
	}
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		hay := line
		if insensitive {
			hay = strings.ToLower(hay)
		}
		if strings.Contains(hay, pat) {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return b.String()
}
