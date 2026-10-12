package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

type AccountService struct {
	Store    AccountStore
	Sessions SessionStore
	Registry *Registry
	Launcher Launcher
	Cache    UsageCache
}

func (s *AccountService) Setup(ctx context.Context, providerID ProviderID, name string, options SetupOptions) (Account, error) {
	if err := ValidateAccountName(name); err != nil {
		return Account{}, err
	}
	provider, err := s.Registry.Get(providerID)
	if err != nil {
		return Account{}, err
	}
	if _, err := s.Store.GetByName(providerID, name); err == nil {
		return Account{}, fmt.Errorf("%w: %s/%s", ErrAccountExists, providerID, name)
	} else if !errors.Is(err, ErrAccountNotFound) {
		return Account{}, err
	}
	id, err := NewAccountID()
	if err != nil {
		return Account{}, err
	}
	now := time.Now().UTC()
	account := Account{ID: id, Provider: providerID, Name: name, CreatedAt: now, UpdatedAt: now}
	if err := ensurePrivateDir(s.Store.AccountDir(id)); err != nil {
		return Account{}, err
	}
	if err := ensurePrivateDir(s.Store.CredentialDir(id)); err != nil {
		_ = os.RemoveAll(s.Store.AccountDir(id))
		return Account{}, err
	}

	session, err := s.Sessions.Create(account, "")
	if err != nil {
		_ = os.RemoveAll(s.Store.AccountDir(id))
		return Account{}, err
	}
	result, setupErr := provider.SetupAccount(ctx, SetupRequest{
		Account: account, RuntimeDir: session.RuntimeDir, HomeDir: session.HomeDir, Options: options,
	})
	cleanupErr := s.Sessions.Cleanup(session)
	if setupErr != nil {
		_ = os.RemoveAll(s.Store.AccountDir(id))
		return Account{}, errors.Join(setupErr, cleanupErr)
	}
	final := result.Account
	final.ID = account.ID
	final.Provider = account.Provider
	final.Name = account.Name
	final.CreatedAt = account.CreatedAt
	final.UpdatedAt = time.Now().UTC()
	if err := s.Store.Create(final); err != nil {
		_ = os.RemoveAll(s.Store.AccountDir(id))
		return Account{}, errors.Join(err, cleanupErr)
	}
	return final, cleanupErr
}

func (s *AccountService) List(context.Context) ([]Account, error) {
	return s.Store.List()
}

func (s *AccountService) Rename(_ context.Context, accountID AccountID, newName string) (Account, error) {
	return s.Store.Rename(accountID, newName)
}

// ProfileUpdate changes a profile's name and launch defaults in one write.
type ProfileUpdate struct {
	Name string // "" keeps the current name
	// Defaults maps profile field IDs (see ProfileEditor) to new values; a nil or
	// empty value returns the field to the provider default.
	Defaults map[string]*string
}

// UpdateProfile renames the account and/or changes its launch defaults under
// the registry lock. Invalid defaults are a ProfileError and nothing is written.
// Running sessions keep the options they were started with.
func (s *AccountService) UpdateProfile(_ context.Context, accountID AccountID, update ProfileUpdate) (Account, error) {
	if update.Name != "" {
		if err := ValidateAccountName(update.Name); err != nil {
			return Account{}, err
		}
	}
	return s.Store.Modify(accountID, func(account Account) (Account, error) {
		if update.Name != "" {
			account.Name = update.Name
		}
		if len(update.Defaults) == 0 {
			return account, nil
		}
		editor, err := s.profileEditor(account)
		if err != nil {
			return Account{}, err
		}
		if unknown := UnknownProfileFields(editor.ProfileFields(), update.Defaults); len(unknown) > 0 {
			return Account{}, &ProfileError{Err: fmt.Errorf("unknown profile default %q (use %s)", unknown[0], strings.Join(FieldIDs(editor.ProfileFields()), ", "))}
		}
		settings, err := editor.UpdateProfile(account, update.Defaults)
		if err != nil {
			return Account{}, &ProfileError{Err: err}
		}
		account.Settings = settings
		return account, nil
	})
}

// UpdateDefaults changes only the account's launch defaults.
func (s *AccountService) UpdateDefaults(ctx context.Context, accountID AccountID, defaults map[string]*string) (Account, error) {
	return s.UpdateProfile(ctx, accountID, ProfileUpdate{Defaults: defaults})
}

func (s *AccountService) profileEditor(account Account) (ProfileEditor, error) {
	if s.Registry == nil {
		return nil, fmt.Errorf("%w: %s", ErrProviderNotFound, account.Provider)
	}
	provider, err := s.Registry.Get(account.Provider)
	if err != nil {
		return nil, err
	}
	editor, ok := provider.(ProfileEditor)
	if !ok {
		return nil, &ProfileError{Err: fmt.Errorf("%s profiles have no launch defaults", Label(provider))}
	}
	return editor, nil
}

// RemoveOptions guard an account removal.
type RemoveOptions struct {
	// InUse returns the active sessions of the account. A non-empty answer
	// stops the removal with an AccountInUseError before anything is deleted.
	InUse func(AccountID) []string
}

// Remove deletes the account. Provider-side cleanup is best effort: its failure
// is returned as a warning and removal continues.
func (s *AccountService) Remove(ctx context.Context, accountID AccountID, options RemoveOptions) ([]string, error) {
	account, err := s.Store.Get(accountID)
	if err != nil {
		return nil, err
	}
	if options.InUse != nil {
		if ids := options.InUse(account.ID); len(ids) > 0 {
			return nil, &AccountInUseError{SessionIDs: append([]string(nil), ids...)}
		}
	}
	if s.Cache != nil {
		if err := s.Cache.RemoveAccount(account.Provider, account.ID); err != nil {
			return nil, err
		}
	}
	var warnings []string
	if s.Registry != nil {
		if provider, err := s.Registry.Get(account.Provider); err == nil {
			if remover, ok := provider.(AccountRemover); ok {
				if err := remover.RemoveAccount(ctx, account); err != nil {
					warnings = append(warnings, fmt.Sprintf("%s provider cleanup failed: %v", Label(provider), err))
				}
			}
		}
	}
	return warnings, s.Store.Remove(accountID)
}

// SessionRecords lists the session records of the account still on disk: its
// running sessions, from the CLI or the web, and any a crash left behind.
// Stores that cannot list sessions report none.
func (s *AccountService) SessionRecords(accountID AccountID) ([]Session, error) {
	lister, ok := s.Sessions.(SessionLister)
	if !ok {
		return nil, nil
	}
	sessions, err := lister.List()
	if err != nil {
		return nil, err
	}
	var out []Session
	for _, session := range sessions {
		if session.AccountID == accountID {
			out = append(out, session)
		}
	}
	return out, nil
}
