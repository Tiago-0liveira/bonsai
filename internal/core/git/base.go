package git

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// Base identifies the comparison branch and the evidence used to select it.
type Base struct {
	Name, Source string
	ref          string
}

var baseGuesses = struct {
	sync.Mutex
	entries map[string]baseGuess
	clock   uint64
}{entries: make(map[string]baseGuess)}

type baseGuess struct {
	snapshot string
	base     Base
	used     uint64
}

func baseName(ref string, remotes []string) string {
	if name, ok := strings.CutPrefix(ref, "refs/heads/"); ok {
		return name
	}
	ref = strings.TrimPrefix(ref, "refs/remotes/")
	prefix := ""
	for _, remote := range remotes {
		if strings.HasPrefix(ref, remote+"/") && len(remote)+1 > len(prefix) {
			prefix = remote + "/"
		}
	}
	return strings.TrimPrefix(ref, prefix)
}

func configuredRemotes(dir string) []string {
	out, _ := run(dir, "remote")
	return strings.Fields(out)
}

func baseRefs(name string, remotes []string) []string {
	branch := baseName(name, remotes)
	refs := []string{"refs/heads/" + branch}
	// Prefer an explicitly selected remote before other configured remotes.
	for _, remote := range remotes {
		ref := "refs/remotes/" + remote + "/" + branch
		if name == remote+"/"+branch || name == ref {
			refs = append(refs, ref)
		}
	}
	for _, remote := range remotes {
		refs = append(refs, "refs/remotes/"+remote+"/"+branch)
	}
	return refs
}

func baseRef(dir, name string, remotes []string) (string, error) {
	refs := baseRefs(name, remotes)
	for _, ref := range refs {
		// Qualification alone does not exclude revision suffixes such as ~1.
		if _, err := run(dir, "check-ref-format", ref); err != nil {
			continue
		}
		if tip, err := run(dir, "rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}"); err == nil {
			return tip, nil
		}
	}
	return "", fmt.Errorf("base ref %q unavailable", name)
}

// RecordWorktreeBase persists branch evidence only; arbitrary start revisions
// are left to the heuristic rather than becoming relative, moving bases.
func RecordWorktreeBase(dir, branch, start string) error {
	if start == "" {
		start = "HEAD"
	}
	ref, err := run(dir, "rev-parse", "--symbolic-full-name", "--verify", "--end-of-options", start)
	if err != nil || (!strings.HasPrefix(ref, "refs/heads/") && !strings.HasPrefix(ref, "refs/remotes/")) {
		return nil
	}
	name := baseName(ref, configuredRemotes(dir))
	if _, err := run(dir, "config", "--local", "branch."+branch+".bonsaiBase", name); err != nil {
		return fmt.Errorf("record worktree base: %w", err)
	}
	return nil
}

// ResolveBase reads local evidence only. Descendants cannot be bases: their
// zero unique-commit count would otherwise beat the branch's actual parent.
func ResolveBase(dir, branch, upstream, prBase string) (Base, error) {
	return resolveBase(dir, branch, upstream, prBase, configuredRemotes(dir))
}

func resolveBase(dir, branch, upstream, prBase string, remotes []string) (Base, error) {
	if prBase != "" {
		return Base{Name: baseName(prBase, remotes), Source: "pr", ref: prBase}, nil
	}
	if recorded, err := run(dir, "config", "--local", "--get", "branch."+branch+".bonsaiBase"); err == nil && recorded != "" {
		return Base{Name: baseName(recorded, remotes), Source: "recorded", ref: recorded}, nil
	}
	fallback := Base{Name: baseName(upstream, remotes), Source: "default", ref: upstream}
	head, err := BranchTip(dir, branch)
	if err != nil {
		return fallback, err
	}
	tips, err := run(dir, "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads/", "refs/remotes/")
	if err != nil {
		return fallback, err
	}
	key, err := run(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return fallback, err
	}
	key += "\n" + branch
	snapshot := head + "\n" + upstream + "\n" + strings.Join(remotes, "\n") + "\n" + tips
	baseGuesses.Lock()
	baseGuesses.clock++
	if cached, ok := baseGuesses.entries[key]; ok && cached.snapshot == snapshot {
		cached.used = baseGuesses.clock
		baseGuesses.entries[key] = cached
		baseGuesses.Unlock()
		return cached.base, nil
	}
	baseGuesses.Unlock()

	defaultRef := ""
	available := map[string]bool{}
	for _, line := range strings.Split(tips, "\n") {
		if fields := strings.Fields(line); len(fields) == 2 {
			available[fields[0]] = true
		}
	}
	for _, ref := range baseRefs(upstream, remotes) {
		if available[ref] {
			defaultRef = ref
			break
		}
	}
	best, count := fallback, -1
	counts := map[string]int{}
	for _, line := range strings.Split(tips, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] == "refs/heads/"+branch || fields[1] == head {
			continue
		}
		name := baseName(fields[0], remotes)
		if !strings.HasPrefix(fields[0], "refs/heads/") && fields[0] != defaultRef {
			continue
		}
		n, known := counts[fields[1]]
		if !known {
			counts[fields[1]] = -1
			// Counting against the candidate includes all merge bases in criss-cross
			// histories. Zero also cheaply excludes descendants of the head.
			unique, e := run(dir, "rev-list", "--count", fields[1]+".."+head)
			if e != nil {
				continue
			}
			n, e = strconv.Atoi(unique)
			if e != nil || n == 0 || (count >= 0 && n > count) {
				continue
			}
			if _, e = run(dir, "merge-base", head, fields[1]); e != nil {
				continue
			}
			counts[fields[1]] = n
		}
		if n < 0 {
			continue
		}
		if count < 0 || n < count || (n == count && name == fallback.Name) {
			best, count = Base{Name: name, Source: "guess", ref: fields[0]}, n
		}
	}
	baseGuesses.Lock()
	defer baseGuesses.Unlock()
	if _, exists := baseGuesses.entries[key]; !exists && len(baseGuesses.entries) >= 256 {
		oldest := ""
		var used uint64
		for k, entry := range baseGuesses.entries {
			if oldest == "" || entry.used < used {
				oldest, used = k, entry.used
			}
		}
		delete(baseGuesses.entries, oldest)
	}
	baseGuesses.clock++
	baseGuesses.entries[key] = baseGuess{snapshot: snapshot, base: best, used: baseGuesses.clock}
	return best, nil
}

// BaseMetrics keeps the displayed base consistent with the ref actually counted.
// Missing local PR bases may still be available as remote-tracking refs.
func BaseMetrics(dir, branch, upstream, prBase string) (Metrics, error) {
	remotes := configuredRemotes(dir)
	base, err := resolveBase(dir, branch, upstream, prBase, remotes)
	if err != nil {
		return Metrics{}, err
	}
	ref, err := baseRef(dir, base.ref, remotes)
	if err != nil {
		base = Base{Name: baseName(upstream, remotes), Source: "default", ref: upstream}
		ref, err = baseRef(dir, upstream, remotes)
	}
	if err != nil {
		return Metrics{}, err
	}
	head, err := BranchTip(dir, branch)
	if err != nil {
		return Metrics{}, err
	}
	metrics, err := AheadBehind(dir, head, ref)
	metrics.Base, metrics.BaseSource = base.Name, base.Source
	return metrics, err
}
