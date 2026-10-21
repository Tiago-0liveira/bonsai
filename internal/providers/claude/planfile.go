package claude

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// planFileName is the only name a planner may write; Bonsai reads it back by that name.
const planFileName = "plan.md"

// planFileArgs replace readOnlyArgs for a planner that may leave one plan file.
// Write joins the tool set (the other file tools stay absent) and the plan
// directory joins the working directories, because --restricted confines file
// tools to those. dontAsk then denies everything no rule allows, so the single
// allow rule in planFileSettings is the only write that can happen: there is
// nobody to approve anything else, and the plan mode that readOnlyArgs adds
// for its prompt behavior is not needed. --setting-sources "" (an empty list is
// accepted: the 2.1.306 parser returns no sources for "") repeats what
// --restricted already does for user, project and local settings files, so no
// profile, seeded or repository settings.json can add an allow rule beside ours.
// Managed (administrator policy) settings and --settings still apply: policy is
// outside Bonsai's control, and a policy that allows more is the administrator's
// choice. An older Claude Code that lacks a flag
// exits with an unknown-option error, so the launch fails closed.
func planFileArgs(planFile string) []string {
	return []string{"--restricted", "--tools", "Read,Grep,Glob,Write", "--strict-mcp-config", "--permission-mode", "dontAsk", "--setting-sources", "", "--add-dir", filepath.Dir(planFile)}
}

// planFileSettings is the permission policy, for --settings. Claude Code
// matches every file-writing tool, Write included, against Edit(path) rules, and
// reads a rule path starting with // as absolute from the file system root.
// The deny rules repeat what --tools already leaves out, so a profile or a
// future default cannot bring those tools back.
type planFileSettings struct {
	Hooks       map[string][]settingsEntry `json:"hooks,omitempty"`
	Permissions planFilePermissions        `json:"permissions"`
}

type planFilePermissions struct {
	Allow []string `json:"allow"`
	Deny  []string `json:"deny"`
}

// planFileLaunch returns the extra arguments and the one merged --settings JSON.
// The activity hooks are best effort, as everywhere else: without them the
// permission policy is still sent.
func planFileLaunch(planFile string, activity *agents.ActivityRequest) (args []string, settings string, err error) {
	if err := validatePlanFile(planFile); err != nil {
		return nil, "", fmt.Errorf("%w: %v", agents.ErrInvalidLaunch, err)
	}
	policy := planFileSettings{Permissions: planFilePermissions{
		Allow: []string{"Edit(/" + filepath.ToSlash(planFile) + ")"},
		Deny:  []string{"Bash", "WebFetch", "WebSearch"},
	}}
	if activity.Usable() {
		policy.Hooks = activityHooks(activity)
	}
	raw, err := json.Marshal(policy)
	if err != nil {
		return nil, "", fmt.Errorf("plan file settings: %w", err)
	}
	return planFileArgs(planFile), string(raw), nil
}

// validatePlanFile refuses any path whose permission rule could match more than
// the one file, or could not be written as a rule at all. Rule paths are
// gitignore-style patterns, so a wildcard, bracket, brace, negation, escape or
// parenthesis in the path would widen or break the rule; a home directory with
// such a character fails closed instead of being escaped.
func validatePlanFile(planFile string) error {
	switch {
	case planFile == "" || !filepath.IsAbs(planFile) || filepath.VolumeName(planFile) != "" || !strings.HasPrefix(planFile, "/"):
		return fmt.Errorf("the plan file must be an absolute path")
	case filepath.Clean(planFile) != planFile:
		return fmt.Errorf("the plan file path must be clean")
	case filepath.Base(planFile) != planFileName:
		return fmt.Errorf("the plan file must be named %s", planFileName)
	case strings.ContainsAny(planFile, "*?[]{}()!\\\"'`$,;:"):
		return fmt.Errorf("the plan file path contains a character a permission rule cannot match exactly")
	}
	for _, r := range planFile {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("the plan file path has a control character")
		}
	}
	return nil
}
