package agents

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLockFileSerializesGoroutines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "account.lock")
	first, err := LockFile(path)
	if err != nil {
		t.Fatal(err)
	}
	acquired := make(chan *FileLock, 1)
	errs := make(chan error, 1)
	go func() {
		second, err := LockFile(path)
		if err != nil {
			errs <- err
			return
		}
		acquired <- second
	}()

	select {
	case <-acquired:
		t.Fatal("second lock acquired while first is held")
	case err := <-errs:
		t.Fatalf("second lock failed: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	if err := first.Unlock(); err != nil {
		t.Fatal(err)
	}
	select {
	case second := <-acquired:
		if err := second.Unlock(); err != nil {
			t.Fatal(err)
		}
	case err := <-errs:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("second lock did not acquire after unlock")
	}
}
