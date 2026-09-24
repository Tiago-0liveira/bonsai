package procstore

import "strings"

// SanitizeOutput makes captured process output safe to draw inside a pane.
//
// A log is raw terminal output: it can carry carriage returns (progress bars
// rewriting their line), cursor movement and erase sequences (spinners, "clear
// line then redraw"), and stray control bytes. Printed verbatim inside a
// bordered layout those escape the pane — \r sends the cursor to column 0 of
// the *screen*, so the rest of the line paints over whatever pane sits to the
// left, and an erase-line sequence wipes that pane's row outright. They also
// defeat wrapping, since they occupy no width.
//
// Color is kept: SGR sequences (ESC[…m) are the one kind that only changes how
// text looks, never where it goes.
func SanitizeOutput(s string) string {
	if !strings.ContainsFunc(s, needsSanitizing) {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		// Markers are bonsai's own, already well-formed (and their sentinel is
		// itself a control byte).
		if _, ok := ParseMarker(line); ok {
			continue
		}
		lines[i] = sanitizeLine(line)
	}
	return strings.Join(lines, "\n")
}

// needsSanitizing reports whether r is anything but plain text, a tab or a
// newline — the cheap pre-check that keeps ordinary logs a single scan.
func needsSanitizing(r rune) bool {
	return (r < 0x20 && r != '\t' && r != '\n') || r == 0x7f
}

// sanitizeLine collapses a line's carriage-return rewrites and drops every
// escape sequence that moves or erases rather than colors.
func sanitizeLine(line string) string {
	// A terminal would show only what was written after the last carriage
	// return, so that is the line's final state. Trailing \r writes nothing, so
	// fall back to the last segment that has content.
	if strings.IndexByte(line, '\r') >= 0 {
		segs := strings.Split(line, "\r")
		line = ""
		for i := len(segs) - 1; i >= 0; i-- {
			if strings.TrimSpace(segs[i]) != "" {
				line = segs[i]
				break
			}
		}
	}
	if !strings.ContainsFunc(line, needsSanitizing) {
		return line
	}

	var b strings.Builder
	b.Grow(len(line))
	for i := 0; i < len(line); {
		c := line[i]
		switch {
		case c == 0x1b:
			n, sgr := escapeLen(line[i:])
			if sgr {
				b.WriteString(line[i : i+n])
			}
			i += n
		case c == '\t':
			b.WriteByte(c)
			i++
		case c < 0x20 || c == 0x7f:
			i++ // bell, backspace, vertical tab, stray sentinels: drop
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// escapeLen measures the escape sequence at the start of s and reports whether
// it is an SGR (color) sequence, the only kind worth keeping.
func escapeLen(s string) (n int, sgr bool) {
	if len(s) < 2 {
		return len(s), false
	}
	switch s[1] {
	case '[': // CSI: parameters, intermediates, then a final byte
		i := 2
		for i < len(s) && s[i] >= 0x30 && s[i] <= 0x3f {
			i++
		}
		for i < len(s) && s[i] >= 0x20 && s[i] <= 0x2f {
			i++
		}
		if i < len(s) {
			final := s[i]
			i++
			return i, final == 'm'
		}
		return i, false
	case ']': // OSC: terminated by BEL or ST (ESC \)
		for i := 2; i < len(s); i++ {
			if s[i] == 0x07 {
				return i + 1, false
			}
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2, false
			}
		}
		return len(s), false
	case 'P', 'X', '^', '_': // DCS / SOS / PM / APC: terminated by ST
		for i := 2; i < len(s); i++ {
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2, false
			}
		}
		return len(s), false
	default: // two-byte escape
		return 2, false
	}
}
