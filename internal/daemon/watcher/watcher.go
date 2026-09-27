// Package watcher accelerates reconciliation with filesystem notifications.
// Periodic scans recover from missed events, overflows and newly created refs.
package watcher

import (
	"context"
	"encoding/json"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"github.com/fsnotify/fsnotify"
	"io/fs"
	"path/filepath"
	"strings"
	"time"
)

type Watcher struct {
	Local         domain.LocalGitService
	RepositoryID  string
	Roots         []string
	DiscoverRoots func(context.Context) []string
	WorktreePaths func(context.Context) map[string]string
	Interval      time.Duration
	Publish       func(context.Context, domain.RepositoryState) error
}

func (w *Watcher) Run(ctx context.Context) error {
	watcher, e := fsnotify.NewWatcher()
	if e != nil {
		return e
	}
	defer watcher.Close()
	watched := map[string]bool{}
	add := func() {
		roots := w.Roots
		if w.DiscoverRoots != nil {
			roots = w.DiscoverRoots(ctx)
		}
		for _, root := range roots {
			filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
				if e != nil {
					return nil
				}
				if !d.IsDir() {
					return nil
				}
				name := d.Name()
				if name == "node_modules" || name == "objects" || name == ".cache" || (name == ".bonsai" && p != root) {
					return filepath.SkipDir
				}
				if !watched[p] {
					if watcher.Add(p) == nil {
						watched[p] = true
					}
				}
				return nil
			})
		}
	}
	add()
	interval := w.Interval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var last string
	var previous domain.RepositoryState
	refresh := func(changed []string) {
		var snapshot domain.RepositoryState
		partial := len(changed) > 0 && last != "" && w.WorktreePaths != nil
		affected := map[string]bool{}
		if partial {
			paths := w.WorktreePaths(ctx)
			for _, name := range changed {
				match := ""
				longest := 0
				for id, root := range paths {
					rel, err := filepath.Rel(root, name)
					if err != nil || !filepath.IsLocal(rel) {
						continue
					}
					if rel == "." || strings.Contains("/"+filepath.ToSlash(rel)+"/", "/.git/") {
						partial = false
						break
					}
					if len(root) > longest {
						match = id
						longest = len(root)
					}
				}
				if match == "" || !partial {
					partial = false
					break
				}
				affected[match] = true
			}
		}
		if partial {
			// Copy the canonical snapshot; only affected worktree statuses are recomputed.
			b, _ := json.Marshal(previous)
			_ = json.Unmarshal(b, &snapshot)
			for i := range snapshot.Worktrees {
				id := snapshot.Worktrees[i].ID
				if !affected[id] {
					continue
				}
				st, err := w.Local.Status(ctx, id)
				if err != nil {
					return
				}
				snapshot.Worktrees[i].Status = &st
				snapshot.Worktrees[i].HeadSHA = st.HeadSHA
				snapshot.Worktrees[i].Branch = st.Branch
				snapshot.AffectedWorktrees = append(snapshot.AffectedWorktrees, id)
			}
		} else {
			var err error
			snapshot, err = w.Local.Repository(ctx, w.RepositoryID)
			if err != nil {
				return
			}
		}
		b, _ := json.Marshal(snapshot)
		if string(b) == last {
			return
		}
		if w.Publish(ctx, snapshot) == nil {
			last = string(b)
			previous = snapshot
		}
	}
	refresh(nil)
	changed := map[string]bool{}
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	var pending <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if event.Op&fsnotify.Remove != 0 {
				delete(watched, event.Name)
			}
			if strings.HasSuffix(event.Name, ".lock") {
				continue
			}
			changed[event.Name] = true
			if pending == nil {
				timer.Reset(100 * time.Millisecond)
				pending = timer.C
			}
		case _, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			refresh(nil)
			add()
		case <-pending:
			pending = nil
			paths := []string{}
			for path := range changed {
				paths = append(paths, path)
			}
			changed = map[string]bool{}
			refresh(paths)
			add()
		case <-ticker.C:
			refresh(nil)
			add()
		}
	}
}
