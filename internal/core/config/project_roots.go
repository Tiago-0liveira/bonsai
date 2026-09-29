package config

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

type ProjectRoot struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}
type ProjectRoots struct {
	Version  int                    `json:"version"`
	Revision uint64                 `json:"revision"`
	Roots    []ProjectRoot          `json:"roots"`
	Requests map[string]RootRequest `json:"requests,omitempty"`
}
type RootRequest struct {
	Identity string          `json:"identity"`
	Result   json.RawMessage `json:"result"`
}

var ErrRootRevision = errors.New("project roots changed; reload settings and retry")
var ErrRootIdempotency = errors.New("idempotency key was used for a different request")

func ProjectRootsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "bonsai", "project-roots.json"), nil
}
func CanonicalDirectory(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") || (os.PathSeparator == '\\' && strings.HasPrefix(path, `~\`)) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		suffix := ""
		if len(path) > 1 {
			suffix = path[2:]
		}
		path = filepath.Join(home, suffix)
	} else if strings.HasPrefix(path, "~") {
		return "", fmt.Errorf("only ~ or ~/ paths are supported")
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("choose an absolute directory path or ~/ path")
	}
	path, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path is not a directory")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err = f.Readdirnames(1); err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return path, nil
}
func PathID(kind, path string) string {
	return fmt.Sprintf("%s-v1-%x", kind, sha256.Sum256([]byte(filepath.Clean(path))))[:len(kind)+36]
}

// ProjectID hashes a stable filesystem identity for an existing repository path.
// Git and callers can spell the same path differently through symlinks or, on
// Windows, casing; resolve those differences before exposing the public ID.
func ProjectID(path string) string {
	identity := filepath.Clean(path)
	if absolute, err := filepath.Abs(identity); err == nil {
		identity = absolute
	}
	if resolved, err := filepath.EvalSymlinks(identity); err == nil {
		identity = filepath.Clean(resolved)
	}
	if runtime.GOOS == "windows" {
		identity = strings.ToLower(identity)
	}
	return PathID("project", identity)
}
func ContainsPath(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// OwningRoot prefers a main-repository ancestor, then a discovery/worktree ancestor.
func OwningRoot(roots []ProjectRoot, main string, discoveries ...string) (ProjectRoot, bool) {
	candidates := func(paths []string) []ProjectRoot {
		var out []ProjectRoot
		for _, r := range roots {
			for _, p := range paths {
				if ContainsPath(r.Path, p) {
					out = append(out, r)
					break
				}
			}
		}
		return out
	}
	rs := candidates([]string{main})
	if len(rs) == 0 {
		rs = candidates(discoveries)
	}
	sort.Slice(rs, func(i, j int) bool {
		di, dj := pathDepth(rs[i].Path), pathDepth(rs[j].Path)
		if di != dj {
			return di > dj
		}
		return rs[i].Path < rs[j].Path
	})
	if len(rs) == 0 {
		return ProjectRoot{}, false
	}
	return rs[0], true
}
func ReadProjectRoots(path string) (ProjectRoots, error) {
	out := ProjectRoots{Version: 1, Roots: []ProjectRoot{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out = ProjectRoots{}
	if err = json.Unmarshal(b, &out); err != nil {
		return out, fmt.Errorf("read project roots %s: %w", path, err)
	}
	if out.Version != 1 || out.Roots == nil {
		return out, fmt.Errorf("invalid or unsupported project roots configuration: %s", path)
	}
	seen := map[string]bool{}
	for _, r := range out.Roots {
		if !filepath.IsAbs(r.Path) || r.ID != PathID("root", r.Path) || seen[r.ID] {
			return out, fmt.Errorf("invalid project root in %s", path)
		}
		seen[r.ID] = true
	}
	return out, nil
}

// WithProjectRoots holds the cross-process configuration lock through fn.
func WithProjectRoots(path string, fn func(ProjectRoots) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	lock, err := procstore.Lock(path + ".lock")
	if err != nil {
		return err
	}
	defer lock.Unlock()
	cfg, err := ReadProjectRoots(path)
	if err != nil {
		return err
	}
	return fn(cfg)
}
func UpdateProjectRoots(path, key string, revision uint64, add, remove string) (ProjectRoots, error) {
	var result ProjectRoots
	if strings.TrimSpace(key) == "" || len(key) > 128 {
		return result, fmt.Errorf("Idempotency-Key is required (maximum 128 characters)")
	}
	identity := fmt.Sprintf("%d\x00%s\x00%s", revision, add, remove)
	err := WithProjectRoots(path, func(cfg ProjectRoots) error {
		if prior, ok := cfg.Requests[key]; ok {
			if prior.Identity != identity {
				return ErrRootIdempotency
			}
			return json.Unmarshal(prior.Result, &result)
		}
		if cfg.Revision != revision {
			return ErrRootRevision
		}
		if remove != "" {
			found := false
			next := []ProjectRoot{}
			for _, r := range cfg.Roots {
				if r.ID == remove {
					found = true
				} else {
					next = append(next, r)
				}
			}
			if !found {
				return fmt.Errorf("root not found")
			}
			cfg.Roots = next
		} else {
			canonical, err := CanonicalDirectory(add)
			if err != nil {
				return err
			}
			duplicate := false
			info, _ := os.Stat(canonical)
			for _, r := range cfg.Roots {
				other, e := os.Stat(r.Path)
				if canonical == r.Path || (e == nil && os.SameFile(info, other)) {
					duplicate = true
					break
				}
			}
			if !duplicate {
				cfg.Roots = append(cfg.Roots, ProjectRoot{ID: PathID("root", canonical), Path: canonical})
			}
		}
		cfg.Revision++
		result = cfg
		result.Requests = nil
		raw, err := json.Marshal(result)
		if err != nil {
			return err
		}
		if cfg.Requests == nil {
			cfg.Requests = map[string]RootRequest{}
		}
		cfg.Requests[key] = RootRequest{Identity: identity, Result: raw}
		raw, err = json.Marshal(cfg)
		if err != nil {
			return err
		}
		return gitstore.WriteJSON(path, raw)
	})
	return result, err
}

func pathDepth(path string) int {
	path = strings.TrimPrefix(filepath.Clean(path), filepath.VolumeName(path))
	return len(strings.FieldsFunc(path, func(r rune) bool { return r == rune(filepath.Separator) }))
}

// WithProjectRootsContext bounds waiting for an in-flight creation or edit.
func WithProjectRootsContext(ctx context.Context, path string, fn func(ProjectRoots) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		lock, err := procstore.TryLock(path + ".lock")
		if err == nil {
			defer lock.Unlock()
			cfg, err := ReadProjectRoots(path)
			if err != nil {
				return err
			}
			return fn(cfg)
		}
		if !errors.Is(err, procstore.ErrLocked) {
			return err
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
