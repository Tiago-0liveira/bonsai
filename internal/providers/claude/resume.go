package claude

import (
	"context"
	"path/filepath"
	"regexp"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

var sessionUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// ResumeTarget checks the profile still holds the conversation transcript. The
// id is validated first because it is joined into a glob pattern.
func (p *Provider) ResumeTarget(_ context.Context, req agents.ResumeRequest) (agents.ResumeTarget, error) {
	id := req.ProviderSessionID
	if !sessionUUID.MatchString(id) {
		return agents.ResumeTarget{}, agents.NotResumable(agents.ReasonNotFound)
	}
	// Claude files transcripts under a project directory named after the cwd.
	matches, err := filepath.Glob(filepath.Join(configDir(p.accounts, req.Account), "projects", "*", id+".jsonl"))
	if err != nil || len(matches) == 0 {
		return agents.ResumeTarget{}, agents.NotResumable(agents.ReasonNoConversation)
	}
	return agents.ResumeTarget{ID: id}, nil
}

var _ agents.Resumer = (*Provider)(nil)
