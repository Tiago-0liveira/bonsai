package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/shellinit"
)

// cmdShellInitPrompt prints the prompt-depth hook for one shell, to be eval'd
// from an rc file. It is the opt-in twin of the automatic injection and sets
// the same guard, so using both never shows the prefix twice.
func cmdShellInitPrompt(args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: bonsai shell-init [%s]", strings.Join(shellinit.SnippetShells, "|"))
	}
	snippet, err := shellinit.Snippet(args[0])
	if err != nil {
		return fmt.Errorf("usage: bonsai shell-init [%s]: %w", strings.Join(shellinit.SnippetShells, "|"), err)
	}
	_, err = io.WriteString(out, snippet)
	return err
}
