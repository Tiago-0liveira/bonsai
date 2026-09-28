package git

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

func TestIndependentStoresDoNotLoseTransactions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	a, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := a
			if i%2 == 0 {
				s = b
			}
			if err := s.Update(func(d Data) error { return Put(d, "records", fmt.Sprint(i), i) }); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	for _, s := range []*Store{a, b} {
		if err := s.View(func(d Data) error {
			if len(d["records"]) != 20 {
				t.Fatalf("lost records: %d", len(d["records"]))
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}
