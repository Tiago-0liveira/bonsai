package local

import (
	"context"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const maxFileSize = 4 << 20

func validPath(path string) error {
	if path == "" || !filepath.IsLocal(path) || strings.ContainsAny(path, "\x00\\") {
		return domain.ErrInvalid
	}
	for _, p := range strings.Split(filepath.ToSlash(path), "/") {
		if p == ".." || (strings.EqualFold(p, ".git") || strings.EqualFold(p, ".bonsai")) {
			return domain.ErrForbidden
		}
	}
	return nil
}
func (s *Service) Files(ctx context.Context, id string) ([]domain.FileEntry, error) {
	_, dir, e := s.target(ctx, id)
	if e != nil {
		return nil, e
	}
	out, e := run(ctx, dir, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if e != nil {
		return nil, e
	}
	st, e := status(ctx, dir)
	if e != nil {
		return nil, e
	}
	statuses := map[string]string{}
	for _, f := range st.Files {
		statuses[f.Path] = f.Index + f.Worktree
	}
	files := []domain.FileEntry{}
	seen := map[string]bool{}
	for _, p := range strings.Split(out, "\x00") {
		if p == "" || seen[p] || validPath(p) != nil {
			continue
		}
		seen[p] = true
		files = append(files, domain.FileEntry{Path: p, Status: statuses[p]})
	}
	return files, nil
}
func (s *Service) ReadFile(ctx context.Context, id, path string) (domain.FileContent, error) {
	if e := ctx.Err(); e != nil {
		return domain.FileContent{}, e
	}
	if e := validPath(path); e != nil {
		return domain.FileContent{}, e
	}
	_, dir, e := s.target(ctx, id)
	if e != nil {
		return domain.FileContent{}, e
	}
	f, e := openWorktreeFile(dir, filepath.ToSlash(path))
	if e != nil {
		return domain.FileContent{}, domain.ErrForbidden
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		return domain.FileContent{}, e
	}
	if !info.Mode().IsRegular() {
		return domain.FileContent{}, domain.ErrForbidden
	}
	b, e := io.ReadAll(io.LimitReader(f, maxFileSize+1))
	if e != nil {
		return domain.FileContent{}, e
	}
	if len(b) > maxFileSize {
		return domain.FileContent{}, domain.E("too_large", "file exceeds 4 MiB")
	}
	binary := !utf8.Valid(b) || strings.ContainsRune(string(b), '\x00')
	content := string(b)
	if binary {
		content = ""
	}
	return domain.FileContent{Path: path, Content: content, Binary: binary}, nil
}
