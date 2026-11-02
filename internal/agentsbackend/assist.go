package agentsbackend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/agentruntime"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
)

var _ agentsapi.Assist = (*assist)(nil)

const (
	// maxAssistInput bounds what one call writes to the provider's stdin.
	maxAssistInput       = 256 << 10
	defaultAssistTimeout = 5 * time.Minute
)

// assist adapts the provider structured-call capability to the TUI contract.
type assist struct {
	rt *agentruntime.Runtime
}

// Run refuses before any process starts when the account is missing, its
// provider cannot run tool-free, or the model or effort is invalid.
func (a *assist) Run(ctx context.Context, req agentsapi.AssistRequest) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(req.Input) > maxAssistInput {
		return nil, fmt.Errorf("%w: input too large", agents.ErrInvalidLaunch)
	}
	// A hung CLI must not run forever when the caller set no deadline of its own.
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultAssistTimeout)
		defer cancel()
	}
	account, err := a.rt.Accounts.Get(req.AccountID)
	if err != nil {
		return nil, safeAccountError(err)
	}
	provider, err := a.rt.Registry.Get(account.Provider)
	if err != nil {
		return nil, safeAccountError(err)
	}
	runner, ok := provider.(agents.StructuredRunner)
	if !ok || !runner.Structured().Supported {
		reason := agents.StructuredOf(provider).Reason
		return nil, fmt.Errorf("%w: %s: %s", agents.ErrStructuredUnsupported, provider.ID(), reason)
	}
	if v, ok := provider.(agents.LaunchValidator); ok {
		if err := v.ValidateLaunch(account, agents.LaunchOptions{Model: req.Model, Effort: req.Effort}); err != nil {
			return nil, fmt.Errorf("%w: %v", agents.ErrInvalidLaunch, err)
		}
	}
	out, err := runner.RunStructured(ctx, agents.StructuredRequest{
		Account: account, Model: req.Model, Effort: req.Effort,
		System: req.System, Input: req.Input, Schema: req.Schema, MaxOutput: req.MaxOutput,
	})
	if err != nil {
		if errors.Is(err, agents.ErrStructuredOutput) || errors.Is(err, agents.ErrNotAuthenticated) ||
			errors.Is(err, agents.ErrInvalidLaunch) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, ErrAccountUnavailable
	}
	return out, nil
}
