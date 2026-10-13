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
	// aliases are the shortcut models with the efforts of the model each one
	// resolves to, when the list says what that is.
	aliases []agents.ModelOption
	at      time.Time
	ok      bool
}

// Models returns the aliases followed by the models Anthropic lists for this
// profile, each with the efforts the list says it accepts. Listing is best
// effort: a profile that is logged out, expired or rejected simply gets the
// bare aliases. It never refreshes a login, never returns an error, and caches
// both outcomes so opening the dialog does not hit the API.
func (p *Provider) Models(ctx context.Context, account agents.Account) ([]agents.ModelOption, error) {
	p.modelsMu.Lock()
	entry, cached := p.modelsCache[account.ID]
	p.modelsMu.Unlock()
	if cached {
		ttl := modelsFailureTTL
		if entry.ok {
			ttl = modelsTTL
		}
		if p.now().Sub(entry.at) < ttl {
			return append(append([]agents.ModelOption(nil), entry.aliases...), entry.models...), nil
		}
	}
	listed, aliases, err := p.listModels(ctx, account)
	// A cancelled request says nothing about the API, so it is not remembered.
	if err == nil || ctx.Err() == nil {
		p.modelsMu.Lock()
		if p.modelsCache == nil {
			p.modelsCache = map[agents.AccountID]modelsEntry{}
		}
		p.modelsCache[account.ID] = modelsEntry{models: listed, aliases: aliases, at: p.now(), ok: err == nil}
		p.modelsMu.Unlock()
	}
	return append(append([]agents.ModelOption(nil), aliases...), listed...), nil
}

// listModels returns the listed models and the aliases resolved against them.
// Whatever the failure, the aliases come back bare so the dialog still offers them.
func (p *Provider) listModels(ctx context.Context, account agents.Account) (models, aliases []agents.ModelOption, err error) {
	aliases = append([]agents.ModelOption(nil), modelAliases...)
	settings, err := ParseSettings(account)
	if err != nil {
		return nil, aliases, err
	}
	var token string
	if settings.AuthMode == AuthToken {
		file, err := readToken(tokenPath(p.accounts, account))
		if err != nil {
			return nil, aliases, errors.New("token missing")
		}
		token = file.Token
	} else if token, err = p.usageToken(account); err != nil {
		return nil, aliases, err
	}
	body, status, err := p.getJSON(ctx, p.modelsEndpoint, token)
	if err != nil {
		return nil, aliases, redact(err, token)
	}
	if status != 200 {
		return nil, aliases, errors.New("model list unavailable")
	}
	list, err := parseModelList(body)
	if err != nil {
		return nil, aliases, err
	}
	return list.models, resolveAliases(list), nil
}

// resolveAliases gives each alias the efforts of the newest listed model of its
// line, which is the model `claude --model <alias>` resolves to. The list is
// newest first, so the first model of a line wins.
func resolveAliases(list modelList) []agents.ModelOption {
	out := append([]agents.ModelOption(nil), modelAliases...)
	for i, alias := range out {
		for _, m := range list.models {
			if list.lines[m.ID] != alias.ID {
				continue
			}
			out[i].Efforts, out[i].NoEffort = m.Efforts, m.NoEffort
			break
		}
	}
	return out
}

// modelList is a parsed model list: the models, and the line (opus, sonnet...)
// each belongs to by ID.
type modelList struct {
	models []agents.ModelOption
	lines  map[string]string
}

// effortOrder is the order the API's effort levels are shown in, weakest first.
// Only these are offered: `claude --effort` accepts nothing else.
var effortOrder = []string{"low", "medium", "high", "xhigh", "max"}

// ParseModels reads the `{"data":[{"id","display_name","capabilities"}]}` list,
// keeping only well-formed Claude model IDs and dropping duplicates. Each model
// carries the efforts its capabilities list as supported.
func ParseModels(data []byte) ([]agents.ModelOption, error) {
	list, err := parseModelList(data)
	return list.models, err
}

func parseModelList(data []byte) (modelList, error) {
	var doc struct {
		Data []struct {
			ID           string `json:"id"`
			DisplayName  string `json:"display_name"`
			Line         string `json:"line"`
			Capabilities *struct {
				Effort map[string]json.RawMessage `json:"effort"`
			} `json:"capabilities"`
		} `json:"data"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return modelList{}, errors.New("unrecognized model list")
	}
	seen := map[string]bool{}
	list := modelList{lines: map[string]string{}}
	for _, item := range doc.Data {
		id := strings.TrimSpace(item.ID)
		if !strings.HasPrefix(id, "claude-") || seen[id] || validateModel(id) != nil || strings.ContainsAny(id, " \t") {
			continue
		}
		seen[id] = true
		label := strings.TrimSpace(item.DisplayName)
		if label == "" || len(label) > 128 {
			label = id
		}
		option := agents.ModelOption{ID: id, Label: label, Description: id, Source: "api"}
		if item.Capabilities != nil {
			option.Efforts, option.NoEffort = supportedEfforts(item.Capabilities.Effort)
		}
		list.models = append(list.models, option)
		list.lines[id] = item.Line
		if len(list.models) == maxModels {
			break
		}
	}
	return list, nil
}

// supportedEfforts reads a model's `capabilities.effort` object. A model the API
// says has no effort control reports none (noEffort); a missing or unreadable
// object says nothing, so the caller's fallback list applies.
func supportedEfforts(effort map[string]json.RawMessage) (levels []string, noEffort bool) {
	supported := func(key string) (value, present bool) {
		raw, ok := effort[key]
		if !ok {
			return false, false
		}
		var flag struct {
			Supported bool `json:"supported"`
		}
		if json.Unmarshal(raw, &flag) != nil {
			return false, false
		}
		return flag.Supported, true
	}
	if len(effort) == 0 {
		return nil, false
	}
	// The object's own `supported` is a plain boolean, unlike its levels.
	var on bool
	if raw, ok := effort["supported"]; ok && json.Unmarshal(raw, &on) == nil && !on {
		return nil, true
	}
	for _, level := range effortOrder {
		if ok, _ := supported(level); ok {
			levels = append(levels, level)
		}
	}
	return levels, false
}
