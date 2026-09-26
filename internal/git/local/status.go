package local

import (
	"context"
	"crypto/sha256"
	"fmt"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (s *Service) Status(ctx context.Context, id string) (domain.WorkingTreeStatus, error) {
	r, dir, e := s.target(ctx, id)
	if e != nil {
		return domain.WorkingTreeStatus{}, e
	}
	u, e := r.lock(ctx)
	if e != nil {
		return domain.WorkingTreeStatus{}, e
	}
	defer u()
	return status(ctx, dir)
}
func status(ctx context.Context, dir string) (domain.WorkingTreeStatus, error) {
	out, e := run(ctx, dir, "status", "--porcelain=v2", "--branch", "--show-stash", "-z", "--untracked-files=all")
	if e != nil {
		return domain.WorkingTreeStatus{}, e
	}
	st, e := parseStatus(out)
	if e != nil {
		return st, e
	}
	if st.Upstream != "" {
		st.LocalRemoteRefSHA, _ = trimmed(ctx, dir, "rev-parse", "--verify", "--end-of-options", st.Upstream)
	}
	if st.HeadSHA != "" {
		c, e := lastCommit(ctx, dir)
		if e != nil {
			return st, e
		}
		st.LastCommit = &c
	}
	gd, e := trimmed(ctx, dir, "rev-parse", "--absolute-git-dir")
	if e != nil {
		return st, e
	}
	st.GitState = "normal"
	for _, v := range []struct{ path, state string }{{"rebase-merge", "rebase"}, {"rebase-apply", "rebase"}, {"MERGE_HEAD", "merge"}, {"CHERRY_PICK_HEAD", "cherry-pick"}, {"REVERT_HEAD", "revert"}} {
		if _, err := os.Stat(filepath.Join(gd, v.path)); err == nil {
			st.GitState = v.state
			break
		}
	}
	// File content changes matter even when porcelain status stays "modified".
	// Hash changed regular files without spawning a Git process per file.
	digest := sha256.New()
	root, err := os.OpenRoot(dir)
	if err != nil {
		return st, err
	}
	defer root.Close()
	for _, f := range st.Files {
		if validPath(f.Path) != nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return st, err
		}
		fmt.Fprint(digest, f.Path, "\x00", f.Index, f.Worktree)
		info, err := root.Lstat(f.Path)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, _ := root.Readlink(f.Path)
			fmt.Fprint(digest, target)
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		file, err := openWorktreeFile(dir, f.Path)
		if err != nil {
			continue
		}
		_, err = io.Copy(digest, file)
		file.Close()
		if err != nil {
			return st, err
		}
	}
	st.ContentVersion = fmt.Sprintf("%x", digest.Sum(nil))

	return st, nil
}
func parseStatus(out string) (domain.WorkingTreeStatus, error) {
	st := domain.WorkingTreeStatus{Files: []domain.FileStatus{}, Conflicted: []string{}}
	records := strings.Split(out, "\x00")
	for i := 0; i < len(records); i++ {
		line := records[i]
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "# ") {
			key, v, ok := strings.Cut(line[2:], " ")
			if !ok {
				continue
			}
			switch key {
			case "branch.oid":
				if v != "(initial)" {
					st.HeadSHA = v
				}
			case "branch.head":
				st.Branch = v
			case "branch.upstream":
				st.Upstream = v
			case "branch.ab":
				fmt.Sscanf(v, "+%d -%d", &st.Ahead, &st.Behind)
			case "stash":
				st.StashCount, _ = strconv.Atoi(v)
			}
			continue
		}
		var path, xy string
		switch line[0] {
		case '?':
			path = line[2:]
			xy = "??"
			st.Untracked++
		case '1', '2', 'u':
			n := 9
			if line[0] == '2' {
				n = 10
			}
			if line[0] == 'u' {
				n = 11
			}
			p := strings.SplitN(line, " ", n)
			if len(p) != n || len(p[1]) != 2 {
				return st, fmt.Errorf("invalid porcelain status")
			}
			path = p[n-1]
			xy = p[1]
			if line[0] == '2' {
				i++
			} // original rename path is a separate NUL record
			if line[0] == 'u' {
				st.Conflicted = append(st.Conflicted, path)
			}
			if xy[0] != '.' {
				st.Staged++
			}
			if xy[1] != '.' || p[2] != "N..." {
				st.Modified++
			}
		default:
			continue
		}
		st.Files = append(st.Files, domain.FileStatus{Path: path, Index: string(xy[0]), Worktree: string(xy[1])})
	}
	st.Dirty = len(st.Files) > 0
	return st, nil
}
func lastCommit(ctx context.Context, dir string) (domain.Commit, error) {
	out, e := trimmed(ctx, dir, "log", "-1", "--format=%H%x00%s%x00%an%x00%ct")
	if e != nil {
		return domain.Commit{}, e
	}
	p := strings.Split(out, "\x00")
	if len(p) != 4 {
		return domain.Commit{}, fmt.Errorf("invalid commit output")
	}
	sec, e := strconv.ParseInt(p[3], 10, 64)
	return domain.Commit{SHA: p[0], Subject: p[1], Author: p[2], When: time.Unix(sec, 0).UTC()}, e
}
