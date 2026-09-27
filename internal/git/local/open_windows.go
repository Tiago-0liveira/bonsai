//go:build windows

package local

import (
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"strings"
)

func openWorktreeFile(dir, path string) (*os.File, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open(path)
	if err != nil {
		return nil, err
	}
	// Validate the opened handle's final target, not a pre-open pathname. os.Root
	// enforces containment; this check also excludes internal credential paths.
	buf := make([]uint16, 32768)
	n, err := windows.GetFinalPathNameByHandle(windows.Handle(f.Fd()), &buf[0], uint32(len(buf)), 0)
	if err != nil || n >= uint32(len(buf)) {
		f.Close()
		return nil, domain.ErrForbidden
	}
	resolved := strings.TrimPrefix(windows.UTF16ToString(buf[:n]), `\\?\`)
	base, err := filepath.EvalSymlinks(dir)
	if err != nil {
		f.Close()
		return nil, err
	}
	rel, err := filepath.Rel(base, resolved)
	if err != nil || validPath(filepath.ToSlash(rel)) != nil {
		f.Close()
		return nil, domain.ErrForbidden
	}
	return f, nil
}
