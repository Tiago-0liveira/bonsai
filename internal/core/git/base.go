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

// Keep only the latest snapshot per branch, with a bound for long-running TUIs.
var baseGuesses = struct {
	sync.Mutex
	entries map[string]baseGuess
}{entries: make(map[string]baseGuess)}

type baseGuess struct {
	snapshot string
	base     Base
}

func baseName(ref string) string {
	return strings.TrimPrefix(strings.TrimPrefix(ref, "refs/heads/"), "origin/")
}

func baseRef(dir, name string) (string, error) {
	for _, ref := range []string{"refs/heads/" + name, "refs/remotes/origin/" + name, name} {
		if _, err := run(dir, "rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}"); err == nil {
			return ref, nil
		}
	}
	return "", fmt.Errorf("base ref %q unavailable", name)
}

// ResolveBase reads local evidence only. Descendants cannot be bases: their
// zero unique-commit count would otherwise beat the branch's actual parent.
func ResolveBase(dir, branch, upstream, prBase string) (Base, error) {
	if prBase != "" {
		return Base{Name: baseName(prBase), Source: "pr", ref: prBase}, nil
	}
	if recorded, err := run(dir, "config", "--local", "--get", "branch."+branch+".bonsaiBase"); err == nil && recorded != "" {
		return Base{Name: baseName(recorded), Source: "recorded", ref: recorded}, nil
	}
	fallback := Base{Name: baseName(upstream), Source: "default", ref: upstream}
	head, err := BranchTip(dir, branch)
	if err != nil {
		return fallback, err
	}
	tips, err := run(dir, "for-each-ref", "--format=%(refname:short) %(objectname)", "refs/heads/")
	if err != nil {
		return fallback, err
	}
	upstreamRef, err := baseRef(dir, upstream)
	if err == nil {
		tip, e := run(dir, "rev-parse", "--verify", "--end-of-options", upstreamRef+"^{commit}")
		if e == nil {
			tips += "\n" + upstream + " " + tip
		}
	}
	key, err := run(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return fallback, err
	}
	key += "\n" + branch
	snapshot := head + "\n" + upstream + "\n" + tips
	baseGuesses.Lock()
	defer baseGuesses.Unlock()
	if cached, ok := baseGuesses.entries[key]; ok && cached.snapshot == snapshot {
		return cached.base, nil
	}
	best, count := fallback, -1
	for _, line := range strings.Split(tips, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] == branch || fields[1] == head {
			continue
		}
		contains, e := IsAncestor(dir, head, fields[1])
		if e != nil || contains {
			continue
		}
		mb, e := run(dir, "merge-base", head, fields[1])
		if e != nil {
			continue
		}
		unique, e := run(dir, "rev-list", "--count", mb+".."+head)
		if e != nil {
			continue
		}
		n, e := strconv.Atoi(unique)
		if e == nil && (count < 0 || n < count || (n == count && fields[0] == upstream)) {
			best, count = Base{Name: baseName(fields[0]), Source: "guess", ref: fields[0]}, n
		}
	}
	if len(baseGuesses.entries) >= 256 {
		clear(baseGuesses.entries)
	}
	baseGuesses.entries[key] = baseGuess{snapshot, best}
	return best, nil
}

// BaseMetrics keeps the displayed base consistent with the ref actually counted.
// Missing local PR bases may still be available as remote-tracking refs.
func BaseMetrics(dir, branch, upstream, prBase string) (Metrics, error) {
	base, err := ResolveBase(dir, branch, upstream, prBase)
	if err != nil {
		return Metrics{}, err
	}
	ref, err := baseRef(dir, base.ref)
	if err != nil {
		base = Base{Name: baseName(upstream), Source: "default", ref: upstream}
		ref, err = baseRef(dir, upstream)
	}
	if err != nil {
		return Metrics{}, err
	}
	metrics, err := AheadBehind(dir, branch, ref)
	metrics.Base, metrics.BaseSource = base.Name, base.Source
	return metrics, err
}
