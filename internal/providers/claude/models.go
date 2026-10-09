package claude

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// ModelsEndpoint is the model list Claude Code itself reads (`GET /v1/models`).
// Other packages' tests point it at a local server.
var ModelsEndpoint = "https://api.anthropic.com/v1/models?limit=1000"

const (
	modelsTTL        = 10 * time.Minute
	modelsFailureTTL = time.Minute
	maxModels        = 200
)

// modelAliases are accepted by `claude --model` and always resolve to the newest
// model of that family, so they are offered even when the list cannot be fetched.
var modelAliases = []agents.ModelOption{
	{ID: "opus", Label: "Opus", Description: "Latest Opus", Source: "alias"},
	{ID: "sonnet", Label: "Sonnet", Description: "Latest Sonnet", Source: "alias"},
	{ID: "haiku", Label: "Haiku", Description: "Latest Haiku", Source: "alias"},
	{ID: "fable", Label: "Fable", Description: "Latest Fable", Source: "alias"},
}

type modelsEntry struct {
	models []agents.ModelOption
	at     time.Time
	ok     bool
}

// Models returns the aliases followed by the models Anthropic lists for this
// profile. Listing is best effort: a profile that is logged out, expired or
// rejected simply gets the aliases. It never refreshes a login, never returns an
// error, and caches both outcomes so opening the dialog does not hit the API.
func (p *Provider) Models(ctx context.Context, account agents.Account) ([]agents.ModelOption, error) {
	out := append([]agents.ModelOption(nil), modelAliases...)
	p.modelsMu.Lock()
	entry, cached := p.modelsCache[account.ID]
	p.modelsMu.Unlock()
	if cached {
		ttl := modelsFailureTTL
		if entry.ok {
			ttl = modelsTTL
		}
		if p.now().Sub(entry.at) < ttl {
			return append(out, entry.models...), nil
		}
	}
	listed, err := p.listModels(ctx, account)
	// A cancelled request says nothing about the API, so it is not remembered.
	if err == nil || ctx.Err() == nil {
		p.modelsMu.Lock()
		if p.modelsCache == nil {
			p.modelsCache = map[agents.AccountID]modelsEntry{}
		}
		p.modelsCache[account.ID] = modelsEntry{models: listed, at: p.now(), ok: err == nil}
		p.modelsMu.Unlock()
	}
	return append(out, listed...), nil
}

func (p *Provider) listModels(ctx context.Context, account agents.Account) ([]agents.ModelOption, error) {
	settings, err := ParseSettings(account)
	if err != nil {
		return nil, err
	}
	var token string
	if settings.AuthMode == AuthToken {
		file, err := readToken(tokenPath(p.accounts, account))
		if err != nil {
			return nil, errors.New("token missing")
		}
		token = file.Token
	} else if token, err = p.usageToken(account); err != nil {
		return nil, err
	}
	body, status, err := p.getJSON(ctx, p.modelsEndpoint, token)
	if err != nil {
		return nil, redact(err, token)
	}
	if status != 200 {
		return nil, errors.New("model list unavailable")
	}
	return ParseModels(body)
}

// ParseModels reads the `{"data":[{"id","display_name"}]}` list, keeping only
// well-formed Claude model IDs and dropping duplicates.
func ParseModels(data []byte) ([]agents.ModelOption, error) {
	var list struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if json.Unmarshal(data, &list) != nil {
		return nil, errors.New("unrecognized model list")
	}
	seen := map[string]bool{}
	var out []agents.ModelOption
	for _, item := range list.Data {
		id := strings.TrimSpace(item.ID)
		if !strings.HasPrefix(id, "claude-") || seen[id] || validateModel(id) != nil || strings.ContainsAny(id, " \t") {
			continue
		}
		seen[id] = true
		label := strings.TrimSpace(item.DisplayName)
		if label == "" || len(label) > 128 {
			label = id
		}
		out = append(out, agents.ModelOption{ID: id, Label: label, Description: id, Source: "api"})
		if len(out) == maxModels {
			break
		}
	}
	return out, nil
}
