package procstore

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Log markers are bonsai-written delimiter lines inside a process log: the run
// started, it exited (with its code), the user stopped it, it is restarting.
// They are stored as a single line prefixed with an ASCII record separator so
// they can never be confused with a line the process itself printed, and so a
// reader can style them per kind. Nothing but bonsai writes MarkerSentinel, and
// readers that do not understand markers still see a readable text body.
const MarkerSentinel = "\x1e"

// Marker kinds.
const (
	MarkerStart   = "start"   // a run began (first start or a restart)
	MarkerExit    = "exit"    // the run ended on its own (Code holds the exit status)
	MarkerStopped = "stopped" // the user killed it
	MarkerRestart = "restart" // it exited and the restart policy is relaunching it
)

// Marker is one delimiter line. Text is the human-readable body composed by the
// writer (the daemon); readers only decide how to draw it.
type Marker struct {
	Kind string
	Code int // exit status, MarkerExit only (0 = success)
	Text string
}

// Encode renders mk as the on-disk log line, newline included.
func (mk Marker) Encode() string {
	text := strings.ReplaceAll(mk.Text, "\t", " ")
	return MarkerSentinel + mk.Kind + "\t" + strconv.Itoa(mk.Code) + "\t" + text + "\n"
}

// ParseMarker decodes a log line written by Encode. The bool is false for any
// ordinary process-output line.
func ParseMarker(line string) (Marker, bool) {
	if !strings.HasPrefix(line, MarkerSentinel) {
		return Marker{}, false
	}
	parts := strings.SplitN(strings.TrimSuffix(strings.TrimPrefix(line, MarkerSentinel), "\r"), "\t", 3)
	if len(parts) != 3 {
		return Marker{}, false
	}
	code, err := strconv.Atoi(parts[1])
	if err != nil {
		return Marker{}, false
	}
	return Marker{Kind: parts[0], Code: code, Text: parts[2]}, true
}

// Line renders a marker as a delimiter rule of the given width, e.g.
// "── exited 1 · failed · ran 2.4s ─────────". Callers add color.
func (mk Marker) Line(width int) string {
	body := "── " + mk.Text + " "
	if width < 8 {
		width = 8
	}
	pad := width - utf8.RuneCountInString(body)
	if pad < 1 {
		return body
	}
	return body + strings.Repeat("─", pad)
}

// MarkerRenderer converts marker lines to plain delimiter rules in a log stream
// arriving in arbitrary chunks (the CLI's `bonsai logs`). A chunk that ends
// mid-marker is held back until its newline arrives, so a marker is never
// printed half-decoded; ordinary output is passed through untouched and
// unbuffered.
type MarkerRenderer struct {
	Width int
	buf   string
}

// Render returns chunk with any complete marker lines replaced by rules.
func (r *MarkerRenderer) Render(chunk string) string {
	s := r.buf + chunk
	r.buf = ""
	if i := strings.LastIndexByte(s, '\n'); i < len(s)-1 {
		tail := s[i+1:]
		// Only hold back a partial line that could still become a marker.
		if strings.HasPrefix(tail, MarkerSentinel) {
			r.buf = tail
			s = s[:i+1]
		}
	}
	if !strings.Contains(s, MarkerSentinel) {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if mk, ok := ParseMarker(line); ok {
			lines[i] = mk.Line(r.width())
		}
	}
	return strings.Join(lines, "\n")
}

// Flush returns any held-back partial marker line (call at end of stream).
func (r *MarkerRenderer) Flush() string {
	out := r.buf
	r.buf = ""
	return out
}

func (r *MarkerRenderer) width() int {
	if r.Width <= 0 {
		return 60
	}
	return r.Width
}
