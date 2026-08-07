package config

import (
	"path/filepath"
	"strconv"
	"strings"
)

// HookVars returns the substitution variables available to lifecycle hook and
// alias commands. In a command string, "{name}" is replaced by the value.
//
//	{new_worktree} / {worktree}   absolute path of the target worktree
//	{worktree_name}               worktree directory basename (e.g. repo-feat-x)
//	{main} / {main_branch_path}    absolute path of the main worktree
//	{branch}                      branch name
//	{base_branch}                 upstream base branch (origin/main -> main)
//	{repo}                        repository directory name
//	{pr_number}                   connected PR number, or "" if none
func HookVars(mainDir, worktreePath, branch, upstream string, prNumber int) map[string]string {
	pr := ""
	if prNumber > 0 {
		pr = strconv.Itoa(prNumber)
	}
	return map[string]string{
		"new_worktree":     worktreePath,
		"worktree":         worktreePath,
		"worktree_name":    filepath.Base(worktreePath),
		"main":             mainDir,
		"main_branch_path": mainDir,
		"branch":           branch,
		"base_branch":      BaseBranch(upstream),
		"repo":             filepath.Base(mainDir),
		"pr_number":        pr,
	}
}

// BaseBranch reduces an upstream ref like "origin/main" to its branch ("main").
func BaseBranch(upstream string) string {
	if upstream == "" {
		return "main"
	}
	if i := strings.LastIndex(upstream, "/"); i >= 0 && i+1 < len(upstream) {
		return upstream[i+1:]
	}
	return upstream
}
