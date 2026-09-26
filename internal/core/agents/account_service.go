package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

type AccountService struct {
	Store    AccountStore
	Sessions SessionStore
	Registry *Registry
	Launcher Launcher
	Cache    UsageCache
}

func (s *AccountService) Setup(ctx context.Context, providerID ProviderID, name string) (Account, error) {
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
		Account: account, RuntimeDir: session.RuntimeDir, HomeDir: session.HomeDir,
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

func (s *AccountService) Remove(_ context.Context, accountID AccountID) error {
	account, err := s.Store.Get(accountID)
	if err != nil {
		return err
	}
	if s.Cache != nil {
		if err := s.Cache.RemoveAccount(account.Provider, account.ID); err != nil {
			return err
		}
	}
	return s.Store.Remove(accountID)
}
