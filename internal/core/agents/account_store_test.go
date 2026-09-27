package agents

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testAccount(id AccountID, provider ProviderID, name string) Account {
	now := time.Now().UTC()
	return Account{ID: id, Provider: provider, Name: name, CreatedAt: now, UpdatedAt: now}
}

func TestFileAccountStoreCRUDAndProviderScopedNames(t *testing.T) {
	root := t.TempDir()
	store, err := NewFileAccountStore(root)
	if err != nil {
		t.Fatal(err)
	}
	a := testAccount("acct_a", "antigravity", "personal")
	b := testAccount("acct_b", "fake", "personal")
	if err := store.Create(a); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(b); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(testAccount("acct_c", "antigravity", "personal")); !errors.Is(err, ErrAccountExists) {
		t.Fatalf("duplicate name error = %v", err)
	}
	renamed, err := store.Rename(a.ID, "google-main")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.ID != a.ID {
		t.Fatalf("rename changed stable id: %s -> %s", a.ID, renamed.ID)
	}
	if got, err := store.GetByName("antigravity", "google-main"); err != nil || got.ID != a.ID {
		t.Fatalf("renamed lookup = %#v, %v", got, err)
	}
	if err := store.Remove(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(a.ID); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("removed account lookup error = %v", err)
	}
}

func TestAccountMetadataDoesNotContainCredentialFiles(t *testing.T) {
	root := t.TempDir()
	store, err := NewFileAccountStore(root)
	if err != nil {
		t.Fatal(err)
	}
	account := testAccount("acct_secret", "antigravity", "personal")
	if err := store.Create(account); err != nil {
		t.Fatal(err)
	}
	const sentinel = "ACCESS_SECRET_DO_NOT_LEAK"
	if err := os.WriteFile(filepath.Join(store.CredentialDir(account.ID), "fake-secret"), []byte(sentinel), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(root, "accounts.json"),
		filepath.Join(store.AccountDir(account.ID), "account.json"),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), sentinel) {
			t.Fatalf("credential leaked into %s", path)
		}
	}
}

func TestValidateAccountName(t *testing.T) {
	for _, name := range []string{"personal", "work.main", "a_b-c", "A1"} {
		if err := ValidateAccountName(name); err != nil {
			t.Errorf("valid name %q rejected: %v", name, err)
		}
	}
	for _, name := range []string{"", "../escape", "bad name", "-starts-dash", strings.Repeat("a", 65)} {
		if err := ValidateAccountName(name); !errors.Is(err, ErrInvalidAccountName) {
			t.Errorf("invalid name %q error = %v", name, err)
		}
	}
}
