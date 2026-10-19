package agenthost

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
)

// RecordVersion is the on-disk record format.
const RecordVersion = 1

// Ended records are kept for `bonsai agents --all` and the API's history, as
// many and as long as the API keeps ended sessions.
const (
	keepEnded   = 32
	keepEndedAt = 24 * time.Hour
)

// LostError is the error of a session whose holder died without recording its end.
const LostError = "Agent host exited unexpectedly."

// Record is a holder's state on disk, rewritten on every change. It never
// holds the prompt or terminal output.
type Record struct {
	Version       int                   `json:"version"`
	ID            string                `json:"id"`
	PID           int                   `json:"pid"`
	Socket        string                `json:"socket"`
	Generation    string                `json:"generation"`
	BinaryVersion string                `json:"binary_version"`
	Protocol      int                   `json:"protocol"`
	WorkDir       string                `json:"work_dir"`
	Summary       agentterminal.Summary `json:"summary"`
	LastOutputAt  *time.Time            `json:"last_output_at,omitempty"`
	// Alive is whether the holder still runs, filled in by List.
	Alive bool `json:"-"`
}

// Paths locate holder records and sockets.
type Paths struct {
	// Records holds <id>.json and <id>.lock, normally <web home>/agents.
	Records string
	// Sockets holds <id>.sock. Unix socket paths are short, so it is the
	// daemon socket directory's agents subdirectory.
	Sockets string
}

// DefaultPaths are the user's holder records and sockets.
func DefaultPaths() (Paths, error) {
	home, err := config.WebHome()
	if err != nil {
		return Paths{}, err
	}
	return Paths{Records: filepath.Join(home, "agents"), Sockets: filepath.Join(procstore.SockDir(), "agents")}, nil
}

func (p Paths) record(id string) string { return filepath.Join(p.Records, id+".json") }
func (p Paths) lock(id string) string   { return filepath.Join(p.Records, id+".lock") }
func (p Paths) socket(id string) string { return filepath.Join(p.Sockets, id+".sock") }

func (p Paths) ensure() error {
	for _, dir := range []string{p.Records, p.Sockets} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
	}
	return nil
}

// validID accepts session IDs only (sess_ and hex), so an ID never names a
// path outside the record and socket directories.
func validID(id string) bool {
	rest, ok := strings.CutPrefix(id, "sess_")
	if !ok || rest == "" || len(rest) > 64 {
		return false
	}
	for _, r := range rest {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func writeRecord(p Paths, r Record) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	path := p.record(r.ID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Read returns one record with its liveness.
func Read(p Paths, id string) (Record, error) {
	if !validID(id) {
		return Record{}, fmt.Errorf("invalid agent session id")
	}
	raw, err := os.ReadFile(p.record(id))
	if err != nil {
		return Record{}, err
	}
	var r Record
	if err := json.Unmarshal(raw, &r); err != nil {
		return Record{}, fmt.Errorf("read agent record %s: %w", id, err)
	}
	if r.ID != id {
		return Record{}, fmt.Errorf("agent record %s names %s", id, r.ID)
	}
	r.Alive = held(p.lock(id))
	if !r.Alive && r.Summary.Active() {
		markLost(&r)
	}
	return r, nil
}

// markLost shows the session of a holder that died mid-session as failed.
func markLost(r *Record) {
	r.Summary.State = "failed"
	r.Summary.Activity = nil
	r.Summary.Error = LostError
	if r.Summary.EndedAt == nil {
		ended := r.Summary.CreatedAt
		if info, err := os.Stat(r.Socket); err == nil {
			ended = info.ModTime().UTC()
		}
		r.Summary.EndedAt = &ended
	}
}

// List returns every record, oldest first. Unreadable records are skipped.
func List(p Paths) ([]Record, error) {
	entries, err := os.ReadDir(p.Records)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Record
	for _, entry := range entries {
		id, ok := strings.CutSuffix(entry.Name(), ".json")
		if !ok || !validID(id) {
			continue
		}
		if r, err := Read(p, id); err == nil {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Summary.CreatedAt.Before(out[j].Summary.CreatedAt) })
	return out, nil
}

// Prune removes the records of ended sessions beyond the newest 32 and those
// older than a day, with their locks and sockets. Records of running holders
// are never touched.
func Prune(p Paths) error {
	records, err := List(p)
	if err != nil {
		return err
	}
	var ended []Record
	for _, r := range records {
		if !r.Alive {
			ended = append(ended, r)
		}
	}
	cutoff := time.Now().Add(-keepEndedAt)
	var errs []error
	for i, r := range ended {
		at := r.Summary.CreatedAt
		if r.Summary.EndedAt != nil {
			at = *r.Summary.EndedAt
		}
		if len(ended)-i <= keepEnded && at.After(cutoff) {
			continue
		}
		errs = append(errs, remove(p, r.ID))
	}
	return errors.Join(errs...)
}

// Remove deletes an ended session's record. A running holder's record stays.
func Remove(p Paths, id string) error {
	r, err := Read(p, id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if r.Alive {
		return fmt.Errorf("agent session %s is still running", id)
	}
	return remove(p, id)
}

func remove(p Paths, id string) error {
	var errs []error
	for _, path := range []string{p.record(id), p.lock(id), p.socket(id)} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
