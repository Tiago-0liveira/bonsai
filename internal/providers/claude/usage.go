package claude

import (
	"context"
	"fmt"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// Usage is not available yet; Capabilities().Usage stays false until it is.
func (p *Provider) Usage(context.Context, agents.Account, agents.UsageOptions) (agents.UsageSnapshot, error) {
	return agents.UsageSnapshot{}, fmt.Errorf("%w: claude usage", agents.ErrUsageUnsupported)
}
