package agents

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var accountNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

type AccountStore interface {
	Create(Account) error
	Get(AccountID) (Account, error)
	GetByName(ProviderID, string) (Account, error)
	List() ([]Account, error)
	Rename(AccountID, string) (Account, error)
	Update(Account) error
	Remove(AccountID) error
	AccountDir(AccountID) string
	CredentialDir(AccountID) string
}

type accountIndex struct {
	Version  int       `json:"version"`
	Accounts []Account `json:"accounts"`
}

type accountRecord struct {
	Version int `json:"version"`
	Account
}

type FileAccountStore struct {
	root string
}

func NewFileAccountStore(root string) (*FileAccountStore, error) {
	if root == "" {
		var err error
		root, err = DefaultAgentsDataRoot()
		if err != nil {
			return nil, err
		}
	}
	s := &FileAccountStore{root: filepath.Clean(root)}
	if err := ensurePrivateDir(s.root); err != nil {
		return nil, err
	}
	if err := ensurePrivateDir(filepath.Join(s.root, "accounts")); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *FileAccountStore) Root() string { return s.root }

func (s *FileAccountStore) AccountDir(id AccountID) string {
	return filepath.Join(s.root, "accounts", string(id))
}

func (s *FileAccountStore) CredentialDir(id AccountID) string {
	return filepath.Join(s.AccountDir(id), "credentials")
}

func (s *FileAccountStore) registryLockPath() string {
	return filepath.Join(s.root, "accounts.lock")
}

func (s *FileAccountStore) indexPath() string {
	return filepath.Join(s.root, "accounts.json")
}

func ValidateAccountName(name string) error {
	if !accountNameRE.MatchString(name) {
		return fmt.Errorf("%w: %q", ErrInvalidAccountName, name)
	}
	return nil
}

func validOpaqueID(id string) bool {
	return id != "" && len(id) <= 128 && id != "." && id != ".." &&
		!strings.ContainsAny(id, `/\`) && !strings.ContainsRune(id, 0)
}

func (s *FileAccountStore) safeAccountDir(id AccountID) (string, error) {
	if !validOpaqueID(string(id)) {
		return "", fmt.Errorf("invalid account id")
	}
	p := s.AccountDir(id)
	root := filepath.Join(s.root, "accounts")
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("account path escapes agents root")
	}
	return p, nil
}

func (s *FileAccountStore) Create(account Account) error {
	if err := ValidateAccountName(account.Name); err != nil {
		return err
	}
	if account.Provider == "" || !validOpaqueID(string(account.ID)) {
		return fmt.Errorf("invalid account identity")
	}
	lock, err := LockFile(s.registryLockPath())
	if err != nil {
		return err
	}
	defer lock.Unlock()

	idx, err := s.loadIndex()
	if err != nil {
		return err
	}
	for _, existing := range idx.Accounts {
		if existing.ID == account.ID || (existing.Provider == account.Provider && existing.Name == account.Name) {
			return fmt.Errorf("%w: %s/%s", ErrAccountExists, account.Provider, account.Name)
		}
	}
	now := time.Now().UTC()
	if account.CreatedAt.IsZero() {
		account.CreatedAt = now
	}
	if account.UpdatedAt.IsZero() {
		account.UpdatedAt = now
	}
	dir, err := s.safeAccountDir(account.ID)
	if err != nil {
		return err
	}
	if err := ensurePrivateDir(dir); err != nil {
		return err
	}
	if err := ensurePrivateDir(s.CredentialDir(account.ID)); err != nil {
		return err
	}
	if err := atomicWriteJSON(filepath.Join(dir, "account.json"), accountRecord{Version: 1, Account: account}); err != nil {
		return err
	}
	idx.Accounts = append(idx.Accounts, account)
	sortAccounts(idx.Accounts)
	return atomicWriteJSON(s.indexPath(), idx)
}

func (s *FileAccountStore) Get(id AccountID) (Account, error) {
	if !validOpaqueID(string(id)) {
		return Account{}, fmt.Errorf("%w: %s", ErrAccountNotFound, id)
	}
	idx, err := s.loadIndex()
	if err != nil {
		return Account{}, err
	}
	for _, a := range idx.Accounts {
		if a.ID == id {
			return a, nil
		}
	}
	return Account{}, fmt.Errorf("%w: %s", ErrAccountNotFound, id)
}

func (s *FileAccountStore) GetByName(provider ProviderID, name string) (Account, error) {
	idx, err := s.loadIndex()
	if err != nil {
		return Account{}, err
	}
	for _, a := range idx.Accounts {
		if a.Provider == provider && a.Name == name {
			return a, nil
		}
	}
	return Account{}, fmt.Errorf("%w: %s/%s", ErrAccountNotFound, provider, name)
}

func (s *FileAccountStore) List() ([]Account, error) {
	idx, err := s.loadIndex()
	if err != nil {
		return nil, err
	}
	out := append([]Account(nil), idx.Accounts...)
	sortAccounts(out)
	return out, nil
}

func (s *FileAccountStore) Rename(id AccountID, newName string) (Account, error) {
	if err := ValidateAccountName(newName); err != nil {
		return Account{}, err
	}
	lock, err := LockFile(s.registryLockPath())
	if err != nil {
		return Account{}, err
	}
	defer lock.Unlock()

	idx, err := s.loadIndex()
	if err != nil {
		return Account{}, err
	}
	pos := -1
	for i := range idx.Accounts {
		if idx.Accounts[i].ID == id {
			pos = i
			break
		}
	}
	if pos < 0 {
		return Account{}, fmt.Errorf("%w: %s", ErrAccountNotFound, id)
	}
	for _, a := range idx.Accounts {
		if a.ID != id && a.Provider == idx.Accounts[pos].Provider && a.Name == newName {
			return Account{}, fmt.Errorf("%w: %s/%s", ErrAccountExists, a.Provider, newName)
		}
	}
	idx.Accounts[pos].Name = newName
	idx.Accounts[pos].UpdatedAt = time.Now().UTC()
	account := idx.Accounts[pos]
	if err := s.writeAccountRecord(account); err != nil {
		return Account{}, err
	}
	sortAccounts(idx.Accounts)
	if err := atomicWriteJSON(s.indexPath(), idx); err != nil {
		return Account{}, err
	}
	return account, nil
}

func (s *FileAccountStore) Update(account Account) error {
	if err := ValidateAccountName(account.Name); err != nil {
		return err
	}
	lock, err := LockFile(s.registryLockPath())
	if err != nil {
		return err
	}
	defer lock.Unlock()

	idx, err := s.loadIndex()
	if err != nil {
		return err
	}
	pos := -1
	for i := range idx.Accounts {
		if idx.Accounts[i].ID == account.ID {
			pos = i
			break
		}
	}
	if pos < 0 {
		return fmt.Errorf("%w: %s", ErrAccountNotFound, account.ID)
	}
	old := idx.Accounts[pos]
	if account.Provider != old.Provider {
		return fmt.Errorf("account provider is immutable")
	}
	for _, a := range idx.Accounts {
		if a.ID != account.ID && a.Provider == account.Provider && a.Name == account.Name {
			return fmt.Errorf("%w: %s/%s", ErrAccountExists, account.Provider, account.Name)
		}
	}
	account.CreatedAt = old.CreatedAt
	account.UpdatedAt = time.Now().UTC()
	if err := s.writeAccountRecord(account); err != nil {
		return err
	}
	idx.Accounts[pos] = account
	sortAccounts(idx.Accounts)
	return atomicWriteJSON(s.indexPath(), idx)
}

func (s *FileAccountStore) Remove(id AccountID) error {
	lock, err := LockFile(s.registryLockPath())
	if err != nil {
		return err
	}
	defer lock.Unlock()

	idx, err := s.loadIndex()
	if err != nil {
		return err
	}
	out := idx.Accounts[:0]
	found := false
	var provider ProviderID
	for _, a := range idx.Accounts {
		if a.ID == id {
			found = true
			provider = a.Provider
			continue
		}
		out = append(out, a)
	}
	if !found {
		return fmt.Errorf("%w: %s", ErrAccountNotFound, id)
	}
	dir, err := s.safeAccountDir(id)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	idx.Accounts = out
	sortAccounts(idx.Accounts)
	if err := atomicWriteJSON(s.indexPath(), idx); err != nil {
		return fmt.Errorf("remove %s/%s metadata: %w", provider, id, err)
	}
	return nil
}

func (s *FileAccountStore) loadIndex() (accountIndex, error) {
	data, err := os.ReadFile(s.indexPath())
	if errors.Is(err, os.ErrNotExist) {
		return accountIndex{Version: 1}, nil
	}
	if err != nil {
		return accountIndex{}, err
	}
	var idx accountIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return accountIndex{}, fmt.Errorf("read account index: %w", err)
	}
	if idx.Version != 1 {
		return accountIndex{}, fmt.Errorf("unsupported account index version %d", idx.Version)
	}
	return idx, nil
}

func (s *FileAccountStore) writeAccountRecord(account Account) error {
	dir, err := s.safeAccountDir(account.ID)
	if err != nil {
		return err
	}
	if err := ensurePrivateDir(dir); err != nil {
		return err
	}
	return atomicWriteJSON(filepath.Join(dir, "account.json"), accountRecord{Version: 1, Account: account})
}

func sortAccounts(accounts []Account) {
	sort.Slice(accounts, func(i, j int) bool {
		if accounts[i].Provider != accounts[j].Provider {
			return accounts[i].Provider < accounts[j].Provider
		}
		if accounts[i].Name != accounts[j].Name {
			return accounts[i].Name < accounts[j].Name
		}
		return accounts[i].ID < accounts[j].ID
	})
}

func atomicWriteJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := ensurePrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func ResolveAccount(store AccountStore, selector string) (Account, error) {
	if selector == "" {
		return Account{}, fmt.Errorf("%w: empty selector", ErrAccountNotFound)
	}
	if a, err := store.Get(AccountID(selector)); err == nil {
		return a, nil
	}
	if provider, name, ok := strings.Cut(selector, "/"); ok && provider != "" && name != "" {
		return store.GetByName(ProviderID(provider), name)
	}
	all, err := store.List()
	if err != nil {
		return Account{}, err
	}
	var matches []Account
	for _, a := range all {
		if a.Name == selector {
			matches = append(matches, a)
		}
	}
	switch len(matches) {
	case 0:
		return Account{}, fmt.Errorf("%w: %s", ErrAccountNotFound, selector)
	case 1:
		return matches[0], nil
	default:
		return Account{}, fmt.Errorf("%w: %q matches multiple providers; use provider/name or account id", ErrAccountAmbiguous, selector)
	}
}
