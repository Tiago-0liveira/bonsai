package review

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ReportFileName is the one file a reviewer run may write, inside its own directory.
const ReportFileName = "report.json"

// ReportDir is the Bonsai-owned directory tree reviewer reports are written to: <root>/<clone>/<attempt>/r<revision>/.
// It is outside every worktree, so the repository under review cannot plant or read the file, and the provider is
// told to write exactly that one path. Same discipline as the planner's plan directory: owner-only directories,
// resolved (no symlink) path, regular file only, size cap. Path elements are digests, never raw identifiers.
type ReportDir struct{ Root string }

// DefaultReportDir follows the user's home, so tests can redirect it.
func DefaultReportDir() (ReportDir, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return ReportDir{}, err
	}
	return ReportDir{Root: filepath.Join(home, ".bonsai", "garden", "reviews")}, nil
}

func element(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:12])
}

// Path is the report file of one run, resolved through any symlink above the root so the path handed to the
// provider is the resolved one. It creates nothing; a root that does not exist yields an os.ErrNotExist error.
func (d ReportDir) Path(local, attempt string, revision int) (string, error) {
	if d.Root == "" || local == "" || !idRE.MatchString(attempt) || revision < 1 {
		return "", fmt.Errorf("%w: report location", ErrInvalid)
	}
	root, err := filepath.EvalSymlinks(d.Root)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, element(local), element(attempt), fmt.Sprintf("r%d", revision), ReportFileName), nil
}

// Prepare creates the run's private directory before the reviewer starts and returns the report path. A leftover
// regular file at the path is removed so an earlier file can never be read as this run's report.
func (d ReportDir) Prepare(local, attempt string, revision int) (string, error) {
	if d.Root == "" {
		return "", fmt.Errorf("%w: report location", ErrInvalid)
	}
	if err := os.MkdirAll(d.Root, 0o700); err != nil {
		return "", err
	}
	path, err := d.Path(local, attempt, revision)
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := checkReportDir(dir); err != nil {
		return "", err
	}
	switch fi, err := os.Lstat(path); {
	case errors.Is(err, os.ErrNotExist):
	case err != nil || !fi.Mode().IsRegular():
		return "", ErrReportUnsafe
	default:
		if err := os.Remove(path); err != nil {
			return "", err
		}
	}
	return path, nil
}

// checkReportDir checks the run directory and its attempt, clone and root parents.
func checkReportDir(dir string) error {
	if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() {
		return ErrReportUnsafe
	}
	if resolved, err := filepath.EvalSymlinks(dir); err != nil || resolved != dir {
		return ErrReportUnsafe
	}
	for i := 0; i < 4; i++ {
		if err := checkPrivate(dir); err != nil {
			return ErrReportUnsafe
		}
		dir = filepath.Dir(dir)
	}
	return nil
}

// Read returns the report file of a run, at most MaxReportBytes+1 bytes (a longer file then fails
// ParseReportFile as too large rather than being cut). ErrReportMissing means no file exists; ErrReportUnsafe means
// the directories are not private, a link is involved or the file is not a plain file.
func (d ReportDir) Read(local, attempt string, revision int) ([]byte, error) {
	path, err := d.Path(local, attempt, revision)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrReportMissing
	}
	if err != nil {
		return nil, err
	}
	if err := checkReportDir(filepath.Dir(path)); err != nil {
		if _, serr := os.Lstat(filepath.Dir(path)); errors.Is(serr, os.ErrNotExist) {
			return nil, ErrReportMissing
		}
		return nil, err
	}
	before, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrReportMissing
	}
	if err != nil || !before.Mode().IsRegular() {
		return nil, ErrReportUnsafe
	}
	f, err := openReportFile(path)
	if err != nil {
		return nil, ErrReportUnsafe
	}
	defer f.Close()
	if after, err := f.Stat(); err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) {
		return nil, ErrReportUnsafe
	}
	b, err := io.ReadAll(io.LimitReader(f, MaxReportBytes+1))
	if err != nil {
		return nil, ErrReportUnsafe
	}
	return b, nil
}
