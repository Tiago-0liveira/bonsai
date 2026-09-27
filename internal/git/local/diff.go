package local

import (
	"context"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"strconv"
	"strings"
)

func diffArgs(ctx context.Context, dir string, req domain.DiffRequest) ([]string, error) {
	args := []string{"--no-pager", "diff", "--no-ext-diff", "--no-textconv", "--no-color", "--no-renames"}
	switch req.Mode {
	case "", "working":
	case "staged":
		args = append(args, "--cached")
	case "branch":
		base, e := ref(ctx, dir, req.Base)
		if e != nil {
			return nil, e
		}
		args = append(args, base+"...HEAD")
	default:
		return nil, domain.ErrInvalid
	}
	return args, nil
}
func (s *Service) Diff(ctx context.Context, req domain.DiffRequest) (domain.Diff, error) {
	return s.diff(ctx, req, "")
}
func (s *Service) diff(ctx context.Context, req domain.DiffRequest, path string) (domain.Diff, error) {
	_, dir, e := s.target(ctx, req.WorktreeID)
	if e != nil {
		return domain.Diff{}, e
	}
	args, e := diffArgs(ctx, dir, req)
	if e != nil {
		return domain.Diff{}, e
	}
	tail := []string{"--", ":(exclude).bonsai", ":(exclude)**/.bonsai/**", ":(exclude)**/.git/**"}
	if path != "" {
		if e = validPath(path); e != nil {
			return domain.Diff{}, e
		}
		tail = append(tail, ":(literal)"+path)
	}
	patch, e := run(ctx, dir, append(append([]string{}, args...), tail...)...)
	if e != nil {
		return domain.Diff{}, e
	}
	if len(patch) > 16<<20 {
		return domain.Diff{}, domain.E("too_large", "diff exceeds 16 MiB")
	}
	stat, e := run(ctx, dir, append(append(args, "--numstat", "-z"), tail...)...)
	if e != nil {
		return domain.Diff{}, e
	}
	result := domain.Diff{Patch: patch, Files: []domain.FileDiff{}}
	for _, line := range strings.Split(stat, "\x00") {
		p := strings.SplitN(line, "\t", 3)
		if len(p) != 3 {
			continue
		}
		a, _ := strconv.Atoi(p[0])
		d, _ := strconv.Atoi(p[1])
		if p[0] == "-" {
			a = -1
			d = -1
		}
		result.Files = append(result.Files, domain.FileDiff{Path: p[2], Additions: a, Deletions: d})
	}
	return result, nil
}
func (s *Service) DiffFile(ctx context.Context, req domain.FileDiffRequest) (domain.FileDiff, error) {
	if e := validPath(req.Path); e != nil {
		return domain.FileDiff{}, e
	}
	d, e := s.diff(ctx, req.DiffRequest, req.Path)
	if e != nil {
		return domain.FileDiff{}, e
	}
	f := domain.FileDiff{Path: req.Path, Patch: d.Patch}
	if len(d.Files) > 0 {
		f.Additions = d.Files[0].Additions
		f.Deletions = d.Files[0].Deletions
	}
	return f, nil
}
