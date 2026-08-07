// Package clipboard copies text to the system clipboard via the first available
// external tool. bonsai shells out instead of linking a clipboard library so the
// binary stays dependency-light and works across macOS / Wayland / X11.
package clipboard

import (
	"fmt"
	"os/exec"
	"strings"
)

// tool is one clipboard backend: the binary and any arguments it needs. The text
// is always fed via stdin, never argv.
type tool struct {
	bin  string
	args []string
}

// tools are probed in order; the first present binary wins.
var tools = []tool{
	{"pbcopy", nil},                        // macOS
	{"wl-copy", nil},                       // Wayland
	{"xclip", []string{"-selection", "c"}}, // X11
	{"xsel", []string{"--clipboard", "--input"}},
}

// lookPath is the probing function, swappable in tests.
type lookPath func(string) (string, error)

// pick returns the first tool whose binary resolves via lookPath.
func pick(lookup lookPath) (tool, bool) {
	for _, t := range tools {
		if _, err := lookup(t.bin); err == nil {
			return t, true
		}
	}
	return tool{}, false
}

// Copy writes text to the system clipboard. It returns a descriptive error when
// no supported clipboard tool is installed.
func Copy(text string) error {
	t, ok := pick(exec.LookPath)
	if !ok {
		return fmt.Errorf("no clipboard tool found (pbcopy/wl-copy/xclip/xsel)")
	}
	cmd := exec.Command(t.bin, t.args...)
	cmd.Stdin = strings.NewReader(text)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w: %s", t.bin, err, strings.TrimSpace(string(out)))
	}
	return nil
}
