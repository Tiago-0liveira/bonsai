package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/termscreen"
)

func TestAttachControlsSurviveAgentScreenChanges(t *testing.T) {
	for _, readOnly := range []bool{true, false} {
		var out bytes.Buffer
		d := newAttachDisplay(&out, 80, 24, readOnly, !readOnly)
		t.Cleanup(d.close)
		for _, output := range []string{
			"\x1b[?1049h\x1b[2J\x1b[Hagent screen",
			strings.Repeat("scrolling\r\n", 40),
			"\x1b[?1049l\x1b[2J\x1b[Hredrawn screen",
		} {
			d.term.Write([]byte(output))
			d.dirty = true
			d.render()
			screen := termscreen.New(80, 24)
			_, _ = screen.Write(out.Bytes())
			visible := screen.String()
			for _, want := range []string{"Ctrl+] d: detach (keep running)", "Ctrl+] k: kill (confirm)"} {
				if !strings.Contains(visible, want) {
					t.Fatalf("controls erased by agent output:\n%s", visible)
				}
			}
		}
		d.confirm, d.dirty = true, true
		d.term.Write([]byte("\x1b[2J\x1b[Hstill running"))
		d.render()
		screen := termscreen.New(80, 24)
		_, _ = screen.Write(out.Bytes())
		if !strings.Contains(screen.String(), "Kill agent? [y/N]") {
			t.Fatalf("confirmation erased:\n%s", screen.String())
		}
	}
}

func TestAttachControlsFitAfterResize(t *testing.T) {
	for _, readOnly := range []bool{true, false} {
		var out bytes.Buffer
		d := newAttachDisplay(&out, 100, 30, readOnly, !readOnly)
		t.Cleanup(d.close)
		d.resize(40, 12)
		d.render()
		screen := termscreen.New(40, 12)
		_, _ = screen.Write(out.Bytes())
		visible := screen.String()
		if !strings.Contains(visible, "Ctrl+] d: detach (keep running)") || !strings.Contains(visible, "Ctrl+] k: kill (confirm)") {
			t.Fatalf("narrow controls missing:\n%s", visible)
		}
		for _, line := range strings.Split(d.term.View(), "\n") {
			if ansi.StringWidth(line) != 40 {
				t.Fatalf("viewport did not resize: %q", line)
			}
		}
	}
}
