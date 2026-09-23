package procstore

import (
	"strings"
	"testing"
)

func TestSanitizeOutputLeavesPlainTextAlone(t *testing.T) {
	in := "building\n\tindented\nlistening on http://localhost:3000\n"
	if got := SanitizeOutput(in); got != in {
		t.Fatalf("SanitizeOutput changed plain text:\n%q\n%q", in, got)
	}
}

func TestSanitizeOutputCollapsesCarriageReturns(t *testing.T) {
	// A progress bar rewriting its line: only the final state survives, and no
	// \r is left to send the cursor into the neighboring pane.
	got := SanitizeOutput("build  10%\rbuild  50%\rbuild 100%\ndone\n")
	want := "build 100%\ndone\n"
	if got != want {
		t.Fatalf("SanitizeOutput = %q, want %q", got, want)
	}
	// A trailing \r wrote nothing; keep the last segment that had content.
	if got := SanitizeOutput("spinner |\r"); got != "spinner |" {
		t.Fatalf("trailing CR = %q, want %q", got, "spinner |")
	}
}

func TestSanitizeOutputKeepsColorDropsMovement(t *testing.T) {
	in := "\x1b[32mok\x1b[0m \x1b[2Kerased \x1b[1Amoved \x1b[?25lhidden\n"
	got := SanitizeOutput(in)
	if !strings.Contains(got, "\x1b[32mok\x1b[0m") {
		t.Errorf("color was stripped: %q", got)
	}
	for _, bad := range []string{"\x1b[2K", "\x1b[1A", "\x1b[?25l"} {
		if strings.Contains(got, bad) {
			t.Errorf("layout-breaking sequence %q survived: %q", bad, got)
		}
	}
	if !strings.Contains(got, "erased moved hidden") {
		t.Errorf("text around the sequences was lost: %q", got)
	}
}

func TestSanitizeOutputDropsOSCAndStrayControls(t *testing.T) {
	got := SanitizeOutput("title\x1b]0;set window title\x07 rest\x07\x08\x0b done\n")
	if strings.ContainsAny(got, "\x1b\x07\x08\x0b") {
		t.Fatalf("control bytes survived: %q", got)
	}
	if !strings.Contains(got, "title rest") || !strings.Contains(got, "done") {
		t.Fatalf("text lost: %q", got)
	}
}

func TestSanitizeOutputPreservesMarkers(t *testing.T) {
	mk := Marker{Kind: MarkerExit, Code: 1, Text: "exited 1 · failed"}
	got := SanitizeOutput("out\r\n" + mk.Encode())
	parsed, ok := ParseMarker(strings.TrimSuffix(strings.Split(got, "\n")[1], "\n"))
	if !ok || parsed != mk {
		t.Fatalf("marker did not survive sanitizing: %q", got)
	}
}
