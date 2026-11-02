package antigravity

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// agy keeps each conversation as <id>.db under $HOME/.gemini/antigravity-cli/
// conversations, and cache/last_conversations.json maps a working directory to
// its latest conversation id. The home is private to one bonsai session, so
// everything under it belongs to that session.
func conversationsDir(home string) string {
	return filepath.Join(antigravityDir(home), "conversations")
}

func lastConversationsPath(home string) string {
	return filepath.Join(antigravityDir(home), "cache", "last_conversations.json")
}

// conversationID matches the ids agy names its files with. Anything else is
// never passed on the command line.
var conversationID = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z_-]*$`)

// ResumeTarget implements agents.Resumer. It reads the conversation id from the
// session home and falls back to resuming by context (--continue) when the id
// is ambiguous: the home is private, so agy can only see this session's work.
func (p *Provider) ResumeTarget(_ context.Context, req agents.ResumeRequest) (agents.ResumeTarget, error) {
	ids := conversationIDs(req.Session.HomeDir)
	if len(ids) == 0 {
		return agents.ResumeTarget{}, agents.NotResumable(agents.ReasonNoConversation)
	}
	if len(ids) == 1 {
		return agents.ResumeTarget{ID: ids[0]}, nil
	}
	if id := lastConversation(req.Session.HomeDir, req.Session.WorkDir); id != "" {
		for _, known := range ids {
			if known == id {
				return agents.ResumeTarget{ID: id}, nil
			}
		}
	}
	return agents.ResumeTarget{}, nil
}

func conversationIDs(home string) []string {
	if home == "" {
		return nil
	}
	entries, err := os.ReadDir(conversationsDir(home))
	if err != nil {
		return nil
	}
	var ids []string
	for _, entry := range entries {
		id, ok := strings.CutSuffix(entry.Name(), ".db")
		if ok && entry.Type().IsRegular() && conversationID.MatchString(id) {
			ids = append(ids, id)
		}
	}
	return ids
}

func lastConversation(home, workDir string) string {
	data, err := os.ReadFile(lastConversationsPath(home))
	if err != nil {
		return ""
	}
	var byDir map[string]string
	if json.Unmarshal(data, &byDir) != nil {
		return ""
	}
	return byDir[workDir]
}

// resumeArgs selects the conversation: by id when known, else the most recent
// one in the (private) home.
func resumeArgs(target agents.ResumeTarget) []string {
	if target.ID == "" {
		return []string{"--continue"}
	}
	return []string{"--conversation", target.ID}
}

// reconcileBeforeResume keeps a token agy refreshed in a crashed session: the
// home may be newer than the vault. Reconcile's generation check guards stale
// writes. A retained home has no credential files and needs nothing.
func (p *Provider) reconcileBeforeResume(ctx context.Context, account agents.Account, session agents.Session) error {
	if _, err := os.Stat(oauthPath(session.HomeDir)); err != nil {
		return nil
	}
	err := p.credentialMgr.Reconcile(ctx, account, session)
	if errors.Is(err, agents.ErrNotAuthenticated) {
		return nil
	}
	return err
}

// RetainRuntime implements agents.RuntimeRetainer. FinalizeSession has already
// written refreshed tokens back to the vault, so the retained home keeps agy's
// conversation state and no credentials.
func (p *Provider) RetainRuntime(_ context.Context, _ agents.Account, session agents.Session) (bool, error) {
	for _, path := range []string{
		oauthPath(session.HomeDir),
		providerSettingsPath(session.HomeDir),
		generationPath(session),
	} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}
	return true, nil
}
