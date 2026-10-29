package plan

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
)

// MaxPlanBytes bounds the plan file Bonsai will read. A larger file is refused, never truncated: a cut plan could
// be published as if it were whole.
const MaxPlanBytes = 1 << 20

// planSettle is how recently a file must have changed to still count as being written.
const planSettle = 2 * time.Second

// PlanFileState says whether an attempt's plan file can be used yet.
type PlanFileState string

const (
	// PlanFileNone: no plan file with content exists.
	PlanFileNone PlanFileState = "none"
	// PlanFileWriting: a file exists but the session is working or the file changed moments ago.
	PlanFileWriting PlanFileState = "writing"
	// PlanFileReady: the file has content, is within the cap and nothing is writing it.
	PlanFileReady PlanFileState = "ready"
	// PlanFileInvalid: the path is not a plain file of an acceptable size inside the attempt's own directory.
	PlanFileInvalid PlanFileState = "invalid"
)

// PlanFileInfo is what Bonsai observed about an attempt's plan file. It never holds the content.
type PlanFileInfo struct {
	State   PlanFileState
	Size    int64
	ModTime time.Time
	// Reason is a fixed phrase for an invalid file, never file or provider text.
	Reason string
}

// planLocation is the directory an attempt's plan file will live in, resolved through any symlink above the plans
// root so the path handed to the provider is the resolved one. It creates only the shared root.
func (s *Service) planLocation(issue garden.IssueID, id string) (string, error) {
	root, err := config.GardenPlansRoot()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	return resolveUnder(root, string(s.d.Local), issue, id)
}

func resolveUnder(root, local string, issue garden.IssueID, id string) (string, error) {
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	dir, err := config.GardenPlanDir(local, uint64(issue), id)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolved, rel), nil
}

// createPlanDir makes the attempt's directory, private to the user, before the session starts. It must end up a
// resolved directory: a symlink put there would redirect the planner's one write.
func createPlanDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() {
		return errors.New("plan directory is not a directory")
	}
	if resolved, err := filepath.EvalSymlinks(dir); err != nil || resolved != dir {
		return errors.New("plan directory is behind a symlink")
	}
	return checkPlanTree(dir)
}

// checkPlanTree checks the attempt directory and its issue, repository-key and plans-root parents.
func checkPlanTree(dir string) error {
	for i := 0; i < 4; i++ {
		if err := checkPrivate(dir); err != nil {
			return err
		}
		dir = filepath.Dir(dir)
	}
	return nil
}

// ownedPlanPath is the attempt's plan path after checking it is exactly where this attempt's file belongs under
// the plans root. A record that names any other path (an edited store, an ID collision) is not trusted.
func (s *Service) ownedPlanPath(a Attempt) (string, bool) {
	if a.PlanPath == "" || a.Local != s.d.Local {
		return "", false
	}
	root, err := config.GardenPlansRoot()
	if err != nil {
		return "", false
	}
	if _, err := os.Lstat(root); err != nil {
		return "", true // nothing was ever written: not invalid, just absent
	}
	dir, err := resolveUnder(root, string(a.Local), a.Issue, a.ID)
	if err != nil {
		return "", false
	}
	want := filepath.Join(dir, config.GardenPlanFileName)
	return want, want == a.PlanPath
}

// InspectPlanFile reports the state of an attempt's plan file. activity is the session's live activity (nil
// when unknown); it only matters while the attempt is live. The file is only looked at, never read.
func (s *Service) InspectPlanFile(a Attempt, activity *agentactivity.Activity) PlanFileInfo {
	if a.PlanPath == "" {
		return PlanFileInfo{State: PlanFileNone}
	}
	path, owned := s.ownedPlanPath(a)
	if !owned {
		return PlanFileInfo{State: PlanFileInvalid, Reason: "the plan path is not this attempt's own"}
	}
	if path == "" {
		return PlanFileInfo{State: PlanFileNone}
	}
	if resolved, err := filepath.EvalSymlinks(filepath.Dir(path)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return PlanFileInfo{State: PlanFileNone}
		}
		return PlanFileInfo{State: PlanFileInvalid, Reason: "the plan directory cannot be read"}
	} else if resolved != filepath.Dir(path) {
		return PlanFileInfo{State: PlanFileInvalid, Reason: "the plan directory is behind a symlink"}
	}
	if checkPlanTree(filepath.Dir(path)) != nil {
		return PlanFileInfo{State: PlanFileInvalid, Reason: "the plan directories are not private to this user"}
	}
	fi, err := os.Lstat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return PlanFileInfo{State: PlanFileNone}
	case err != nil:
		return PlanFileInfo{State: PlanFileInvalid, Reason: "the plan file cannot be read"}
	case !fi.Mode().IsRegular():
		return PlanFileInfo{State: PlanFileInvalid, Reason: "the plan file is not a regular file"}
	case fi.Size() > MaxPlanBytes:
		return PlanFileInfo{State: PlanFileInvalid, Size: fi.Size(), ModTime: fi.ModTime(), Reason: "the plan file is too large"}
	case fi.Size() == 0:
		return PlanFileInfo{State: PlanFileNone, ModTime: fi.ModTime()}
	}
	info := PlanFileInfo{State: PlanFileReady, Size: fi.Size(), ModTime: fi.ModTime()}
	working := a.State.Live() && activity != nil && activity.State == agentactivity.Working
	if working || s.d.Now().Sub(fi.ModTime()) < planSettle {
		info.State = PlanFileWriting
	}
	return info
}

// Plan returns the text of an attempt's ready plan file, for prefilling the plan to publish. It loads the
// attempt, re-checks that the path is its own, re-checks readiness and reads at most MaxPlanBytes through the
// handle it validated. The content is returned to the caller and never logged or stored.
func (s *Service) Plan(ctx context.Context, id string) (string, PlanFileInfo, error) {
	a, err := s.d.Store.Get(ctx, id)
	if err != nil {
		return "", PlanFileInfo{}, err
	}
	if a.Local != s.d.Local {
		return "", PlanFileInfo{}, ErrNotFound
	}
	var activity *agentactivity.Activity
	if a.State.Live() && a.SessionID != "" {
		if st, serr := s.d.Agents.Status(ctx, a.SessionID); serr == nil {
			activity = st.Activity
		}
	}
	info := s.InspectPlanFile(a, activity)
	switch info.State {
	case PlanFileInvalid:
		return "", info, fmt.Errorf("%w: %s", ErrPlanFileInvalid, info.Reason)
	case PlanFileReady:
	default:
		return "", info, ErrPlanNotReady
	}
	text, err := readPlanFile(a.PlanPath)
	if err != nil {
		return "", info, err
	}
	return text, info, nil
}

// readPlanFile opens the file and checks the open handle is the file that was inspected: a plain file, not
// swapped for a link between the check and the read.
func readPlanFile(path string) (string, error) {
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() {
		return "", fmt.Errorf("%w: the plan file is not a regular file", ErrPlanFileInvalid)
	}
	f, err := openPlanFile(path)
	if err != nil {
		return "", fmt.Errorf("%w: the plan file cannot be read", ErrPlanFileInvalid)
	}
	defer f.Close()
	if after, err := f.Stat(); err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) {
		return "", fmt.Errorf("%w: the plan file changed while it was opened", ErrPlanFileInvalid)
	}
	b, err := io.ReadAll(io.LimitReader(f, MaxPlanBytes+1))
	if err != nil {
		return "", fmt.Errorf("%w: the plan file cannot be read", ErrPlanFileInvalid)
	}
	if len(b) > MaxPlanBytes {
		return "", fmt.Errorf("%w: the plan file is too large", ErrPlanFileInvalid)
	}
	if strings.ContainsRune(string(b), 0) {
		return "", fmt.Errorf("%w: the plan file is not text", ErrPlanFileInvalid)
	}
	return strings.ToValidUTF8(string(b), "�"), nil
}

// MarkPublished records that the attempt's plan was published as the plan revision revisionHash from Bonsai. It
// is idempotent: a hash already stored is never replaced, and an empty one is filled in.
func (s *Service) MarkPublished(ctx context.Context, id, revisionHash string) error {
	_, err := s.d.Store.Update(ctx, id, func(cur Attempt) (Attempt, bool, error) {
		if cur.Local != s.d.Local {
			return cur, false, ErrNotFound
		}
		now := s.d.Now().UTC()
		changed := false
		if cur.PlanPublishedAt == nil {
			cur.PlanPublishedAt, changed = &now, true
		}
		if cur.PublishedRevision == "" && revisionHash != "" {
			cur.PublishedRevision, changed = revisionHash, true
		}
		if changed {
			cur.UpdatedAt = now
		}
		return cur, changed, nil
	})
	return err
}
