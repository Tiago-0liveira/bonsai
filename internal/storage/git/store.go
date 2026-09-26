// Package git provides a durable single-process metadata store. Transactions
// replace a synced file atomically; never store file contents or Git history.
package git

import (
	"encoding/json"
	"errors"
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
	return s, nil
}
func (s *Store) View(fn func(Data) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	f, e := os.CreateTemp(filepath.Dir(s.path), ".git-state-*")
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
	if e = os.Rename(f.Name(), s.path); e != nil {
		return e
	}
	s.data = next
	// Directory sync makes the rename durable across an OS crash.
	dir, e := os.Open(filepath.Dir(s.path))
	if e != nil {
		return e
	}
	defer dir.Close()
	return dir.Sync()
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
