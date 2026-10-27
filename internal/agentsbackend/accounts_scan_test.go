//go:build !windows

package agentsbackend

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestRemoveRefusesUnreadableRecords(t *testing.T) {
	f := newFixture(t)
	if err := os.Remove(f.paths.Records); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.paths.Records, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.acc.List(context.Background()); !errors.Is(err, ErrAccountUnavailable) {
		t.Fatalf("List = %v", err)
	}
	if err := f.acc.Remove(context.Background(), f.account.ID); !errors.Is(err, ErrAccountUnavailable) {
		t.Fatalf("Remove = %v", err)
	}
	if _, err := f.acc.rt.Accounts.Get(f.account.ID); err != nil {
		t.Fatalf("account removed despite failed scan: %v", err)
	}
}
