package agym

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"
)

var (
	disallowedBranchChars = regexp.MustCompile(`[^a-z0-9/_-]+`)
	consecutiveDashes     = regexp.MustCompile(`-+`)
	consecutiveSlashes    = regexp.MustCompile(`/+`)
)

// CleanBranchName sanitizes a string returned by an LLM prompt into a valid Git branch name.
func CleanBranchName(raw string) string {
	lines := strings.Split(raw, "\n")
	var candidate string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, "//") {
			continue
		}
		// Strip markdown formatting / quotes
		l = strings.Trim(l, "`'\" \t")
		// Strip common prefixes
		lower := strings.ToLower(l)
		for _, pfx := range []string{"git checkout -b ", "git switch -c ", "branch:", "git branch: "} {
			if strings.HasPrefix(lower, pfx) {
				l = strings.TrimSpace(l[len(pfx):])
				break
			}
		}
		candidate = l
		break
	}

	if candidate == "" {
		return ""
	}

	// Extract first token if multiple words exist on the line
	fields := strings.Fields(candidate)
	if len(fields) > 0 {
		candidate = fields[0]
	}

	// Clean quotes / backticks again
	candidate = strings.Trim(candidate, "`'\" \t")

	// Convert to lower case
	candidate = strings.ToLower(candidate)

	// Disallow .lock suffix
	candidate = strings.TrimSuffix(candidate, ".lock")

	// Replace disallowed characters with a dash
	candidate = disallowedBranchChars.ReplaceAllString(candidate, "-")
	candidate = consecutiveDashes.ReplaceAllString(candidate, "-")
	candidate = consecutiveSlashes.ReplaceAllString(candidate, "/")

	// Trim leading and trailing punctuation
	candidate = strings.Trim(candidate, "/-_. \t")

	// Bound length
	if len(candidate) > 50 {
		candidate = strings.Trim(candidate[:50], "/-_. ")
	}

	return candidate
}

var commonStopWords = map[string]bool{
	"a": true, "an": true, "the": true, "and": true, "or": true,
	"in": true, "on": true, "at": true, "to": true, "for": true,
	"of": true, "with": true, "by": true, "from": true, "is": true,
	"this": true, "that": true, "please": true, "we": true, "i": true,
	"should": true, "would": true, "could": true, "can": true,
}

// SlugifyTask produces a clean deterministic branch name from task text when LLM is unavailable.
func SlugifyTask(task string) string {
	task = strings.TrimSpace(task)
	if task == "" {
		return "feat/agent-task"
	}

	lower := strings.ToLower(task)

	// Determine category prefix
	prefix := "feat/"
	switch {
	case strings.Contains(lower, "fix") || strings.Contains(lower, "bug") || strings.Contains(lower, "patch") ||
		strings.Contains(lower, "error") || strings.Contains(lower, "crash") || strings.Contains(lower, "issue"):
		prefix = "fix/"
	case strings.Contains(lower, "doc") || strings.Contains(lower, "readme"):
		prefix = "docs/"
	case strings.Contains(lower, "test") || strings.Contains(lower, "spec") || strings.Contains(lower, "benchmark"):
		prefix = "test/"
	case strings.Contains(lower, "refactor") || strings.Contains(lower, "clean") || strings.Contains(lower, "perf"):
		prefix = "refactor/"
	}

	// Extract meaningful words
	var words []string
	var current strings.Builder
	for _, r := range lower {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
		} else {
			if current.Len() > 0 {
				w := current.String()
				if !commonStopWords[w] && len(w) > 1 {
					words = append(words, w)
				}
				current.Reset()
			}
		}
	}
	if current.Len() > 0 {
		w := current.String()
		if !commonStopWords[w] && len(w) > 1 {
			words = append(words, w)
		}
	}

	actionVerbs := map[string]bool{
		"fix": true, "add": true, "update": true, "refactor": true, "create": true,
		"make": true, "run": true, "build": true, "implement": true,
	}
	if len(words) > 1 && actionVerbs[words[0]] {
		words = words[1:]
	}

	if len(words) == 0 {
		return prefix + "agent-task"
	}

	if len(words) > 4 {
		words = words[:4]
	}

	slug := strings.Join(words, "-")
	if len(slug) > 40 {
		slug = slug[:40]
	}
	slug = strings.Trim(slug, "-")

	return prefix + slug
}

// GenerateBranchName queries agym non-interactively with a fast model to generate
// a clean git branch name. If agym fails or returns an empty name, it falls back
// to a deterministic slug generated from task.
func (c *ExecClient) GenerateBranchName(ctx context.Context, profile, task string) (string, error) {
	task = strings.TrimSpace(task)
	if task == "" {
		return "feat/agent-task", nil
	}

	resolvedProfile := profile
	if resolvedProfile == "" || resolvedProfile == "auto" {
		if envProf := os.Getenv("AGYM_PROFILE"); envProf != "" {
			resolvedProfile = envProf
		} else {
			// Query agym list to find the first ready profile
			listCtx, cancel := context.WithTimeout(ctx, DefaultInfoTimeout)
			defer cancel()
			out, err := c.runCommand(listCtx, nil, "list")
			if err == nil {
				for _, line := range strings.Split(string(out), "\n") {
					fields := strings.Fields(line)
					if len(fields) >= 2 && strings.HasPrefix(fields[1], "ready") {
						resolvedProfile = fields[0]
						break
					}
				}
			}
		}
	}

	if resolvedProfile != "" && resolvedProfile != "auto" {
		prompt := fmt.Sprintf("Generate a concise git branch name for this task: %q. Return ONLY the branch name (e.g. feat/add-export or fix/auth-leak), lowercase with hyphens, with no explanations, no quotes, and no markdown formatting.", task)

		// Try with fast model first
		cmdCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()

		out, err := c.runCommand(cmdCtx, nil, resolvedProfile, "--model", "gemini-3.8-flash-low", "-p", prompt)
		if err != nil {
			// Fallback without --model in case model name is unsupported
			retryCtx, retryCancel := context.WithTimeout(ctx, 15*time.Second)
			defer retryCancel()
			out, err = c.runCommand(retryCtx, nil, resolvedProfile, "-p", prompt)
		}

		if err == nil {
			cleaned := CleanBranchName(string(out))
			if cleaned != "" {
				return cleaned, nil
			}
		}
	}

	// Fallback to deterministic slug generator
	return SlugifyTask(task), nil
}
