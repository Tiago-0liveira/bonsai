package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// procColorPalette is a curated set of mutually distinct ANSI-256 hues used to
// tag processes in the multi-view merged log, so lines from different
// processes stay easy to tell apart regardless of the active theme.
var procColorPalette = []lipgloss.Color{
	"39", "214", "204", "84", "141", "208", "51", "220", "203", "110",
}

// assignProcColor deterministically picks a tag color for a process id. It is
// not truly random: the theme's accent seeds the rotation offset (so the same
// process gets a different color under a different theme) while staying
// stable across ticks — a real-random pick would make every log refresh
// reshuffle colors, defeating the point of "easy to tell apart".
func assignProcColor(id int) lipgloss.Color {
	seed := 0
	for _, r := range string(theme.Current.Accent) {
		seed = seed*31 + int(r)
	}
	if seed < 0 {
		seed = -seed
	}
	idx := (seed + id) % len(procColorPalette)
	if idx < 0 {
		idx += len(procColorPalette)
	}
	return procColorPalette[idx]
}

// renderProcLog turns the daemon's delimiter lines (procstore.Marker) into
// colored rules — green for a clean exit, red for a failure, yellow for a user
// stop or a pending restart, accent for the start of a run — so a run's
// boundaries are obvious while scrolling. Ordinary output lines, including the
// process's own ANSI colors, pass through untouched.
func renderProcLog(text string, width int) string {
	if !strings.Contains(text, procstore.MarkerSentinel) {
		return text
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if mk, ok := procstore.ParseMarker(line); ok {
			lines[i] = markerStyle(mk).Render(mk.Line(width))
		}
	}
	return strings.Join(lines, "\n")
}

// markerStyle picks the color for a marker kind (see renderProcLog).
func markerStyle(mk procstore.Marker) lipgloss.Style {
	switch mk.Kind {
	case procstore.MarkerExit:
		if mk.Code == 0 {
			return procMarkerOK
		}
		return procMarkerFail
	case procstore.MarkerStopped, procstore.MarkerRestart:
		return procMarkerWarn
	default:
		return procMarkerStart
	}
}

// isMarkerLine reports whether a log line is a bonsai delimiter, so filters
// (the output search) can keep run boundaries visible in their results.
func isMarkerLine(line string) bool {
	_, ok := procstore.ParseMarker(line)
	return ok
}
