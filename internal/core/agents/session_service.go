package agents

import (
	"context"
	"errors"
)

type SessionService struct {
	Accounts AccountStore
	Sessions SessionStore
	Registry *Registry
	Launcher Launcher
}

func (s *SessionService) RunForeground(ctx context.Context, accountID AccountID, workDir string, args []string) error {
	account, err := s.Accounts.Get(accountID)
	if err != nil {
		return err
	}
	provider, err := s.Registry.Get(account.Provider)
	if err != nil {
		return err
	}
	session, err := s.Sessions.Create(account, workDir)
	if err != nil {
		return err
	}
	prepared, prepareErr := provider.PrepareSession(ctx, PrepareSessionRequest{
		Account: account, Session: session, Args: append([]string(nil), args...),
	})
	if prepareErr != nil {
		return errors.Join(prepareErr, s.Sessions.Cleanup(session))
	}
	runErr := s.Launcher.RunForeground(ctx, prepared)
	finalizeErr := provider.FinalizeSession(ctx, FinalizeSessionRequest{Account: account, Session: session})
	cleanupErr := s.Sessions.Cleanup(session)
	return errors.Join(runErr, finalizeErr, cleanupErr)
}
