// Package git provides a durable metadata store. Cross-process locked transactions
// replace a synced file atomically; never store file contents or Git history.
package git

import (
	"encoding/json"
	"errors"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"os"
	"path/filepath"
	"sync"
)

type Data map[string]map[string]json.RawMessage
type Store struct {
	mu   sync.Mutex
	path string
	data Data
}

func Open(path string) (*Store, error) {
	s := &Store{path: path, data: Data{}}
	b, e := os.ReadFile(path)
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		return nil, e
	}
	if e == nil {
		if e = json.Unmarshal(b, &s.data); e != nil {
			return nil, e
		}
	}
	if s.data == nil {
		return nil, errors.New("invalid null store")
	}
	return s, nil
}
func (s *Store) View(fn func(Data) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.reload(); err != nil {
		return err
	}
	return fn(clone(s.data))
}
func clone(d Data) Data {
	b, _ := json.Marshal(d)
	var out Data
	_ = json.Unmarshal(b, &out)
	return out
}
func (s *Store) Update(fn func(Data) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	lock, err := procstore.Lock(s.path + ".lock")
	if err != nil {
		return err
	}
	defer lock.Unlock()
	if err := s.reload(); err != nil {
		return err
	}
	next := clone(s.data)
	if e := fn(next); e != nil {
		return e
	}
	b, e := json.Marshal(next)
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(s.path), 0700); e != nil {
		return e
	}
	if e = WriteJSON(s.path, b); e != nil {
		return e
	}
	s.data = next
	return nil
}
func (s *Store) reload() error {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.data = Data{}
		return nil
	}
	if err != nil {
		return err
	}
	var next Data
	if err := json.Unmarshal(b, &next); err != nil {
		return err
	}
	if next == nil {
		return errors.New("invalid null store")
	}
	s.data = next
	return nil
}

// WriteJSON atomically replaces a JSON document. Callers serialize transactions.
func WriteJSON(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".git-state-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if e = os.Rename(f.Name(), path); e != nil {
		return e
	}
	return syncDir(filepath.Dir(path))
}

func Put(d Data, table, key string, value any) error {
	b, e := json.Marshal(value)
	if e != nil {
		return e
	}
	if d[table] == nil {
		d[table] = map[string]json.RawMessage{}
	}
	d[table][key] = b
	return nil
}
func Get[T any](d Data, table, key string) (T, bool) {
	var v T
	b, ok := d[table][key]
	if !ok {
		return v, false
	}
	if json.Unmarshal(b, &v) != nil {
		return v, false
	}
	return v, true
}
