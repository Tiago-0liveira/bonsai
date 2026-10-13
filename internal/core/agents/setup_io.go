package agents

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"
)

// ErrNoTerminal means a secret was needed but there is no terminal to ask on.
var ErrNoTerminal = errors.New("no terminal")

// TerminalPrompter reads a secret from the process terminal with echo off. It
// is the CLI's prompter; the API never uses it.
type TerminalPrompter struct{}

func (TerminalPrompter) Secret(_ context.Context, label string) (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", ErrNoTerminal
	}
	fmt.Fprintf(os.Stderr, "Paste the %s (input hidden): ", label)
	data, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", label, err)
	}
	return string(data), nil
}

// withDefaults fills the IO fields a caller left out.
func (s SetupIO) withDefaults(launcher Launcher, out io.Writer) SetupIO {
	if s.Launcher == nil {
		s.Launcher = launcher
	}
	if s.Launcher == nil {
		s.Launcher = NewForegroundLauncher(nil, nil, nil)
	}
	if s.Out == nil {
		s.Out = out
	}
	if s.Out == nil {
		s.Out = io.Discard
	}
	if s.Prompt == nil {
		s.Prompt = TerminalPrompter{}
	}
	return s
}

// HomeRelative shows path relative to home ("~/.codex") when it lies under
// it, for display only.
func HomeRelative(path, home string) string {
	if home == "" {
		return path
	}
	rel, err := filepath.Rel(home, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return path
	}
	if rel == "." {
		return "~"
	}
	return "~" + string(filepath.Separator) + rel
}

// RemoteHost reports whether the user probably sits at another machine than
// the one running Bonsai (WSL, SSH), where a browser sign-in on this host may
// not open: device-code sign-in is then the better default.
func RemoteHost(env func(string) string) bool {
	return env("WSL_DISTRO_NAME") != "" || env("SSH_CONNECTION") != ""
}
