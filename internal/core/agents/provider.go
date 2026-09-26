package agents

import "context"

type Capabilities struct {
	Interactive            bool
	Usage                  bool
	MultiAccount           bool
	ConcurrentSameAccount  bool
	ConcurrentCrossAccount bool
}

type SetupRequest struct {
	Account    Account
	RuntimeDir string
	HomeDir    string
}

type SetupResult struct {
	Account Account
}

type PrepareSessionRequest struct {
	Account Account
	Session Session
	Args    []string
}

type PreparedSession struct {
	Executable string
	Args       []string
	Dir        string
	EnvSet     map[string]string
	EnvUnset   []string
}

type FinalizeSessionRequest struct {
	Account Account
	Session Session
}

type Provider interface {
	ID() ProviderID
	Capabilities() Capabilities
	SetupAccount(context.Context, SetupRequest) (SetupResult, error)
	PrepareSession(context.Context, PrepareSessionRequest) (PreparedSession, error)
	FinalizeSession(context.Context, FinalizeSessionRequest) error
	Usage(context.Context, Account, UsageOptions) (UsageSnapshot, error)
}
