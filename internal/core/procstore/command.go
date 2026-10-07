package procstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CommandIdentity preserves argument boundaries and stable ownership, even for
// records whose worktree is no longer registered. Environment is never exposed.
func CommandIdentity(r *Record) string {
	dir := r.WorkingDir
	if dir == "" {
		dir = r.Worktree
	}
	args := r.Args
	if args == nil {
		args = []string{}
	}
	shell := ""
	if r.Program == "" {
		shell = r.Command
	}
	data, _ := json.Marshal(struct {
		Version               int
		Owner, Dir, Program   string
		Args                  []string
		Shell, Group, Service string
	}{1, filepath.Clean(r.Worktree), filepath.Clean(dir), r.Program, args, shell, r.ServeGroup, r.ServeName})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func ExecutionOrder(r *Record) uint64 {
	if r.ExecutionOrder != 0 {
		return r.ExecutionOrder
	}
	return uint64(r.ID)
}
func (s *Store) NextExecutionOrder() (uint64, error) {
	data, err := os.ReadFile(filepath.Join(s.Dir(), "execution-order"))
	var order uint64
	if err == nil {
		order, err = strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid execution order counter: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return 0, err
	}
	records, err := s.ListRecords()
	if err != nil {
		return 0, err
	}
	for _, r := range records {
		if v := ExecutionOrder(r); v > order {
			order = v
		}
	}
	// The ID counter also covers deleted legacy records.
	id, err := s.LastAllocatedID()
	if err != nil {
		return 0, err
	}
	if uint64(id) > order {
		order = uint64(id)
	}
	order++
	if order == 0 {
		return 0, fmt.Errorf("execution order exhausted")
	}
	return order, writeAtomic(filepath.Join(s.Dir(), "execution-order"), []byte(strconv.FormatUint(order, 10)), 0600)
}

type ProcessVisibility struct {
	Cutoffs map[string]uint64 `json:"cutoffs"`
	Deleted map[int]bool      `json:"deleted"`
}

func (s *Store) readVisibility() (ProcessVisibility, error) {
	v := ProcessVisibility{Cutoffs: map[string]uint64{}, Deleted: map[int]bool{}}
	data, err := os.ReadFile(filepath.Join(s.Dir(), "process-visibility.json"))
	if os.IsNotExist(err) {
		return v, nil
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal(data, &v)
	if v.Cutoffs == nil {
		v.Cutoffs = map[string]uint64{}
	}
	if v.Deleted == nil {
		v.Deleted = map[int]bool{}
	}
	return v, err
}
func (s *Store) HideExecution(r *Record) (ProcessVisibility, error) {
	v, err := s.readVisibility()
	if err != nil {
		return v, err
	}
	key := r.CommandKey
	if key == "" {
		key = CommandIdentity(r)
	}
	order := ExecutionOrder(r)
	if order > v.Cutoffs[key] {
		v.Cutoffs[key] = order
	}
	v.Deleted[r.ID] = true
	data, err := json.Marshal(v)
	if err != nil {
		return v, err
	}
	return v, writeAtomic(filepath.Join(s.Dir(), "process-visibility.json"), data, 0600)
}

// Deletion intents retain retry metadata separately from browser authority.
// The committed visibility file is published only after the intent is cleared.
type removalIntent struct {
	Record      *Record `json:"record"`
	PriorCutoff uint64  `json:"prior_cutoff"`
}

func (s *Store) removalPath(id int) string {
	return filepath.Join(s.Dir(), "deletions", strconv.Itoa(id)+".json")
}
func (s *Store) removalIntents() ([]removalIntent, error) {
	entries, err := os.ReadDir(filepath.Join(s.Dir(), "deletions"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []removalIntent
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.Dir(), "deletions", entry.Name()))
		if err != nil {
			return nil, err
		}
		var intent removalIntent
		if err := json.Unmarshal(data, &intent); err != nil {
			return nil, err
		}
		if intent.Record == nil || intent.Record.ID <= 0 || intent.Record.CommandKey == "" {
			return nil, fmt.Errorf("invalid removal intent")
		}
		out = append(out, intent)
	}
	return out, nil
}
func (s *Store) ReadVisibility() (ProcessVisibility, error) {
	v, err := s.readVisibility()
	if err != nil {
		return v, err
	}
	intents, err := s.removalIntents()
	if err != nil {
		return v, err
	}
	for _, intent := range intents {
		r := intent.Record
		delete(v.Deleted, r.ID)
		if v.Cutoffs[r.CommandKey] == ExecutionOrder(r) {
			v.Cutoffs[r.CommandKey] = intent.PriorCutoff
		}
	}
	return v, nil
}
func (s *Store) BeginRemoval(r *Record) error {
	// Retries preserve the original cutoff until the transaction completes.
	if s.RemovalPending(r.ID) {
		return nil
	}
	v, err := s.readVisibility()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.removalPath(r.ID)), 0700); err != nil {
		return err
	}
	if err := syncDir(s.Dir()); err != nil {
		return err
	}
	copy := *r
	copy.CommandKey = CommandIdentity(r)
	copy.ExecutionOrder = ExecutionOrder(r)
	data, err := json.Marshal(removalIntent{Record: &copy, PriorCutoff: v.Cutoffs[copy.CommandKey]})
	if err != nil {
		return err
	}
	return writeAtomic(s.removalPath(r.ID), data, 0600)
}
func (s *Store) RemovalPending(id int) bool { _, err := os.Stat(s.removalPath(id)); return err == nil }
func (s *Store) PendingRemovalRecords() ([]*Record, error) {
	intents, err := s.removalIntents()
	if err != nil {
		return nil, err
	}
	records := make([]*Record, 0, len(intents))
	for _, intent := range intents {
		records = append(records, intent.Record)
	}
	return records, nil
}
func (s *Store) RecoverRemovals() error {
	records, err := s.PendingRemovalRecords()
	if err != nil {
		return err
	}
	var failures []error
	for _, r := range records {
		if err := s.CompleteRemoval(r); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
func (s *Store) CompleteRemoval(r *Record) error {
	if err := s.RemoveRecord(r.ID); err != nil {
		return err
	}
	if err := syncDir(s.ProcsDir()); err != nil {
		return err
	}
	if _, err := s.HideExecution(r); err != nil {
		return err
	}
	if err := os.Remove(s.removalPath(r.ID)); err != nil && !os.IsNotExist(err) {
		return err
	}
	if _, err := os.Stat(filepath.Dir(s.removalPath(r.ID))); os.IsNotExist(err) {
		return nil
	}
	return syncDir(filepath.Dir(s.removalPath(r.ID)))
}
