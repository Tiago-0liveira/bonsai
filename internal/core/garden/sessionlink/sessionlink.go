// Package sessionlink keeps the explicit, manual links from an agent session to
// a Garden Issue. A link is a local note: it is never an approved plan, an
// attempt, a spec approval or a launch record, it never changes the Issue, and
// its absence says nothing about the session.
package sessionlink

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

const (
	schemaVersion = 1
	// MaxStoreBytes bounds the file before JSON parsing.
	MaxStoreBytes = 1 << 20
	// MaxLinks caps the store; a full store refuses new links.
	MaxLinks = 2000
	// OriginManual is the only origin: Garden launches carry their task in the session record.
	OriginManual = "manual"
)

var (
	ErrCorrupt           = errors.New("session link store is corrupt")
	ErrUnsupportedSchema = errors.New("session link store has a newer schema")
	ErrTooLarge          = errors.New("session link store is full")
	ErrInvalid           = errors.New("invalid session link")
	// ErrConflict: the session is linked to another Issue and replace was not asked.
	ErrConflict = errors.New("session is already linked to another issue")
)

var sessionRE = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

// Link maps one holder session of one clone to an Issue.
type Link struct {
	SessionID   string             `json:"session_id"`
	LocalRepoID garden.LocalRepoID `json:"local_repo_id"`
	IssueNumber int64              `json:"issue_number"`
	IssueID     garden.IssueID     `json:"issue_id"`
	LinkedAt    time.Time          `json:"linked_at"`
	Origin      string             `json:"origin"`
}

func (l Link) valid() bool {
	return sessionRE.MatchString(l.SessionID) && l.LocalRepoID != "" && l.IssueNumber > 0 && l.IssueID != 0 &&
		!l.LinkedAt.IsZero() && l.Origin == OriginManual
}

func key(local garden.LocalRepoID, session string) string { return string(local) + "\x00" + session }

type disk struct {
	SchemaVersion int             `json:"schema_version"`
	Links         map[string]Link `json:"links"`
}

// FileStore is the locked, atomically replaced JSON file every Bonsai process shares.
type FileStore struct{ Path string }

// Open resolves the default location in the user's Bonsai config directory.
func Open() (*FileStore, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return &FileStore{Path: filepath.Join(dir, "bonsai", "garden-session-links.json")}, nil
}

func (s *FileStore) read() (disk, error) {
	d := disk{SchemaVersion: schemaVersion, Links: map[string]Link{}}
	f, err := os.Open(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return d, nil
	}
	if err != nil {
		return disk{}, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxStoreBytes+1))
	if err != nil {
		return disk{}, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if len(b) > MaxStoreBytes {
		return disk{}, fmt.Errorf("%w: file", ErrTooLarge)
	}
	if !utf8.Valid(b) || garden.RejectDuplicateJSONKeys(b) != nil {
		return disk{}, ErrCorrupt
	}
	var head struct {
		SchemaVersion int `json:"schema_version"`
	}
	if json.Unmarshal(b, &head) == nil && head.SchemaVersion > schemaVersion {
		return disk{}, ErrUnsupportedSchema
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	d.Links = nil
	if err := dec.Decode(&d); err != nil {
		return disk{}, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if dec.Decode(new(any)) != io.EOF || d.SchemaVersion != schemaVersion || d.Links == nil || len(d.Links) > MaxLinks {
		return disk{}, ErrCorrupt
	}
	for k, l := range d.Links {
		if k != key(l.LocalRepoID, l.SessionID) || !l.valid() {
			return disk{}, ErrCorrupt
		}
	}
	return d, nil
}

func (s *FileStore) write(d disk) error {
	d.SchemaVersion = schemaVersion
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	if len(b) > MaxStoreBytes {
		return ErrTooLarge
	}
	return gitstore.WriteJSON(s.Path, b)
}

func (s *FileStore) withLock(ctx context.Context, fn func(*disk) (bool, error)) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		l, err := procstore.TryLock(s.Path + ".lock")
		if err == nil {
			defer func() { _ = l.Unlock() }()
			d, err := s.read()
			if err != nil {
				return err
			}
			write, err := fn(&d)
			if err != nil || !write {
				return err
			}
			return s.write(d)
		}
		if !errors.Is(err, procstore.ErrLocked) {
			return err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// List returns the links of one clone, oldest first. Links made from another
// clone are not returned.
func (s *FileStore) List(ctx context.Context, local garden.LocalRepoID) ([]Link, error) {
	var out []Link
	err := s.withLock(ctx, func(d *disk) (bool, error) {
		for _, l := range d.Links {
			if l.LocalRepoID == local {
				out = append(out, l)
			}
		}
		return false, nil
	})
	sort.Slice(out, func(i, j int) bool {
		if !out[i].LinkedAt.Equal(out[j].LinkedAt) {
			return out[i].LinkedAt.Before(out[j].LinkedAt)
		}
		return out[i].SessionID < out[j].SessionID
	})
	return out, err
}

// Get returns the clone's link of a session; found is false when there is none.
func (s *FileStore) Get(ctx context.Context, local garden.LocalRepoID, session string) (Link, bool, error) {
	var out Link
	var found bool
	err := s.withLock(ctx, func(d *disk) (bool, error) {
		out, found = d.Links[key(local, session)]
		return false, nil
	})
	return out, found, err
}

// Put records a link. The same session and Issue again returns the stored link
// unchanged (created false). Another Issue for a linked session fails with
// ErrConflict unless replace is set. The check and the write share one lock.
func (s *FileStore) Put(ctx context.Context, l Link, replace bool) (stored Link, created bool, err error) {
	l.Origin = OriginManual
	if !l.valid() {
		return Link{}, false, ErrInvalid
	}
	err = s.withLock(ctx, func(d *disk) (bool, error) {
		k := key(l.LocalRepoID, l.SessionID)
		if cur, ok := d.Links[k]; ok {
			if cur.IssueID == l.IssueID && cur.IssueNumber == l.IssueNumber {
				stored = cur
				return false, nil
			}
			if !replace {
				return false, ErrConflict
			}
		} else if len(d.Links) >= MaxLinks {
			return false, ErrTooLarge
		}
		d.Links[k] = l
		stored, created = l, true
		return true, nil
	})
	if err != nil {
		return Link{}, false, err
	}
	return stored, created, nil
}
