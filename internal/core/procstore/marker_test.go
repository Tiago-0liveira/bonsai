package procstore

import (
	"strings"
	"testing"
)

func TestMarkerRoundTrip(t *testing.T) {
	in := Marker{Kind: MarkerExit, Code: 2, Text: "exited 2 · failed · ran 1.2s"}
	got, ok := ParseMarker(strings.TrimSuffix(in.Encode(), "\n"))
	if !ok {
		t.Fatalf("ParseMarker(%q) not recognized", in.Encode())
	}
	if got != in {
		t.Fatalf("round trip = %+v, want %+v", got, in)
	}
}

func TestParseMarkerIgnoresProcessOutput(t *testing.T) {
	for _, line := range []string{
		"",
		"listening on http://localhost:3000",
		"── exited 1 · failed ──", // a process printing something marker-shaped
		MarkerSentinel + "exit",   // sentinel but truncated
	} {
		if _, ok := ParseMarker(line); ok {
			t.Fatalf("ParseMarker(%q) = true, want false", line)
		}
	}
}

func TestMarkerLineFillsWidth(t *testing.T) {
	line := Marker{Kind: MarkerStopped, Text: "stopped by user"}.Line(40)
	if !strings.Contains(line, "stopped by user") {
		t.Fatalf("Line() dropped the text: %q", line)
	}
	if n := len([]rune(line)); n != 40 {
		t.Fatalf("Line(40) width = %d, want 40 (%q)", n, line)
	}
	// Too narrow to pad: keep the text rather than truncating it.
	narrow := Marker{Text: "a very long marker body"}.Line(8)
	if !strings.Contains(narrow, "a very long marker body") {
		t.Fatalf("narrow Line() = %q", narrow)
	}
}

func TestMarkerRendererRendersAndPassesThrough(t *testing.T) {
	r := &MarkerRenderer{Width: 20}
	out := r.Render("hello\n" + Marker{Kind: MarkerExit, Text: "exited 0 · success"}.Encode())
	if !strings.HasPrefix(out, "hello\n") {
		t.Fatalf("output line mangled: %q", out)
	}
	if strings.Contains(out, MarkerSentinel) {
		t.Fatalf("sentinel leaked to the reader: %q", out)
	}
	if !strings.Contains(out, "── exited 0 · success") {
		t.Fatalf("marker not rendered: %q", out)
	}
}

func TestMarkerRendererHoldsBackSplitMarker(t *testing.T) {
	r := &MarkerRenderer{Width: 30}
	enc := Marker{Kind: MarkerStart, Text: "started · dev · 10:00:00"}.Encode()
	half := len(enc) / 2

	first := r.Render("log line\n" + enc[:half])
	if first != "log line\n" {
		t.Fatalf("partial marker leaked: %q", first)
	}
	second := r.Render(enc[half:])
	if strings.Contains(second, MarkerSentinel) {
		t.Fatalf("sentinel leaked: %q", second)
	}
	if !strings.Contains(second, "started · dev · 10:00:00") {
		t.Fatalf("rejoined marker not rendered: %q", second)
	}
	if rest := r.Flush(); rest != "" {
		t.Fatalf("Flush leftover = %q, want empty", rest)
	}
}

func TestMarkerRendererDoesNotBufferOrdinaryPartialLines(t *testing.T) {
	r := &MarkerRenderer{}
	if got := r.Render("half a li"); got != "half a li" {
		t.Fatalf("ordinary partial line was buffered: %q", got)
	}
}
