package updater

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
)

var forceNonInteractive bool

// SetForceNonInteractiveForTesting sets or unsets forced non-interactive mode for testing.
func SetForceNonInteractiveForTesting(force bool) {
	forceNonInteractive = force
}

// IsTerminal checks if the provided reader is an interactive terminal and not inside CI.
func IsTerminal(in io.Reader) bool {
	if forceNonInteractive {
		return false
	}
	if os.Getenv("CI") != "" || os.Getenv("CONTINUOUS_INTEGRATION") != "" {
		return false
	}
	f, ok := in.(*os.File)
	if !ok {
		// In tests, non-file readers default to interactive unless forced otherwise
		return true
	}
	return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
}

// PromptUserToUpdate prompts the user during background / periodic update checks.
// In non-interactive environments, it logs a one-line notice and returns false.
// If the user answers 'y' or 'Y', it returns true. Otherwise it prints "Update skipped." and returns false.
func PromptUserToUpdate(in io.Reader, out io.Writer, currentVersion, latestVersion, releaseNotesURL string) (bool, error) {
	if !IsTerminal(in) {
		fmt.Fprintf(out, "Notice: A new version %s is available. Run 'bonsai --update' to upgrade.\n", latestVersion)
		return false, nil
	}
	return promptUser(in, out, currentVersion, latestVersion, releaseNotesURL, "Update skipped.")
}

// PromptUserToUpdateExplicit prompts the user during explicit update commands (--update / -u).
// If declined, it prints "Update cancelled." and returns false.
func PromptUserToUpdateExplicit(in io.Reader, out io.Writer, currentVersion, latestVersion, releaseNotesURL string) (bool, error) {
	return promptUser(in, out, currentVersion, latestVersion, releaseNotesURL, "Update cancelled.")
}

func promptUser(in io.Reader, out io.Writer, currentVersion, latestVersion, releaseNotesURL, declineMsg string) (bool, error) {
	fmt.Fprintf(out, "A new version of bonsai is available: %s -> %s\n", currentVersion, latestVersion)
	if releaseNotesURL != "" {
		fmt.Fprintf(out, "Release notes: %s\n", releaseNotesURL)
	}
	fmt.Fprint(out, "Do you want to update now? [y/N]: ")

	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}

	ans := strings.TrimSpace(line)
	if strings.EqualFold(ans, "y") || strings.EqualFold(ans, "yes") {
		return true, nil
	}

	if declineMsg != "" {
		fmt.Fprintln(out, declineMsg)
	}
	return false, nil
}
