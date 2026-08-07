// Package gh wraps the GitHub CLI (`gh`) for the operations bonsai needs.
package gh

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// PR is an open pull request.
type PR struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Head   string `json:"headRefName"`
}

// ListPRs returns open pull requests for the repo containing dir via
// `gh pr list --json`. Requires the gh CLI to be installed and authenticated.
func ListPRs(dir string) ([]PR, error) {
	cmd := exec.Command("gh", "pr", "list", "--json", "number,title,headRefName", "--limit", "50")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("gh pr list: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("gh pr list (is gh installed?): %w", err)
	}
	var prs []PR
	if err := json.Unmarshal(out, &prs); err != nil {
		return nil, fmt.Errorf("parse gh output: %w", err)
	}
	return prs, nil
}

// MergePR merges pull request number via `gh pr merge --merge` (a merge commit),
// letting GitHub target the PR's own base branch.
func MergePR(dir string, number int) error {
	cmd := exec.Command("gh", "pr", "merge", strconv.Itoa(number), "--merge")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gh pr merge: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
