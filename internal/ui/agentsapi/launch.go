package agentsapi

import (
	"context"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// LaunchResolver is implemented by an Accounts backend that can say which launch
// an account would run with the caller's overrides applied: the effective model,
// effort and native permissions, and which came from the profile. It starts
// nothing and refreshes nothing. It is optional: the Garden launch is disabled
// without it.
type LaunchResolver interface {
	ResolveLaunch(context.Context, agents.AccountID, agents.LaunchOptions) (agents.LaunchResolution, error)
}

// OperationStartRequest is a start bound to a caller-chosen operation and
// session identity. The same request never starts a second holder.
type OperationStartRequest struct {
	StartRequest
	OperationID string
	SessionID   agents.SessionID
	Task        agents.TaskRef
}

// OperationSession is a session with what its start receipt proves. Acknowledged
// means the provider itself started, not that the holder's socket was ready.
// Closed means the start is settled and can no longer gain a holder.
type OperationSession struct {
	Session      Session
	Acknowledged bool
	Closed       bool
}

// OperationSessions is implemented by a Sessions backend that can start and
// query a holder by durable identity. It is optional: the Garden launch is
// disabled without it, and ordinary Start and Attach are unchanged.
type OperationSessions interface {
	StartOperation(context.Context, OperationStartRequest) (OperationSession, error)
	QueryOperation(context.Context, agents.SessionID, string) (OperationSession, error)
}

// CachedDefaulter is implemented by an Accounts backend that can return an
// account's launch defaults using only model choices the provider already knows.
// Unlike Accounts.Defaults it never starts a provider or opens a credential
// session, so a form that opens before a confirmation uses it. Live model
// discovery stays behind an explicit action. It is optional.
type CachedDefaulter interface {
	CachedDefaults(ctx context.Context, id agents.AccountID) (Defaults, error)
}
