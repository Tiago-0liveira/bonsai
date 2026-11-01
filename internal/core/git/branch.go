package git

import (
	"strings"
)

// ValidBranchName applies the rules of `git check-ref-format --branch` in pure
// Go, so callers can reject a name before touching the filesystem or running
// git. The returned sentinel's text is a short reason fit for inline display.
func ValidBranchName(name string) error {
	switch {
	case name == "":
		return ErrBranchEmpty
	case name == "@":
		return ErrBranchAt
	case name == "HEAD":
		return ErrBranchHEAD
	case strings.HasPrefix(name, "-"):
		return ErrBranchLeadingDash
	case strings.HasPrefix(name, "/"), strings.HasSuffix(name, "/"):
		return ErrBranchEdgeSlash
	case strings.Contains(name, "//"):
		return ErrBranchDoubleSlash
	case strings.Contains(name, ".."):
		return ErrBranchDoubleDot
	case strings.Contains(name, "@{"):
		return ErrBranchAtBrace
	case strings.HasSuffix(name, "."):
		return ErrBranchTrailingDot
	}
	for _, r := range name {
		switch {
		case r == ' ':
			return ErrBranchSpace
		case r < 0x20 || r == 0x7f:
			return ErrBranchControl
		case strings.ContainsRune("~^:?*[\\", r):
			return ErrBranchForbiddenChar
		}
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") {
			return ErrBranchLeadingDot
		}
		if strings.HasSuffix(part, ".lock") {
			return ErrBranchLockSuffix
		}
	}
	return nil
}

// SuggestBranchName returns a valid name close to an invalid one (`my test`
// becomes `my-test`), or "" when nothing sensible is left.
func SuggestBranchName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == ' ' || r == '\t':
			b.WriteByte('-')
		case r < 0x20 || r == 0x7f || strings.ContainsRune("~^:?*[\\", r):
		default:
			b.WriteRune(r)
		}
	}
	s := b.String()
	for _, pair := range [][2]string{{"..", "."}, {"@{", "@"}, {"//", "/"}} {
		for strings.Contains(s, pair[0]) {
			s = strings.ReplaceAll(s, pair[0], pair[1])
		}
	}
	var parts []string
	for _, part := range strings.Split(s, "/") {
		part = strings.TrimLeft(part, ".-")
		for changed := true; changed; {
			trimmed := strings.TrimSuffix(strings.TrimSuffix(part, ".lock"), ".")
			changed = trimmed != part
			part = trimmed
		}
		if part != "" {
			parts = append(parts, part)
		}
	}
	s = strings.Join(parts, "/")
	if ValidBranchName(s) != nil {
		return ""
	}
	return s
}
