package codex

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

const (
	modelsTTL        = 10 * time.Minute
	modelsFailureTTL = time.Minute
	maxModels        = 100
	maxModelPages    = 5
	// bundledTimeout bounds `codex debug models --bundled`, which prints a large
	// document but needs no network.
	bundledTimeout = 15 * time.Second
	// maxBundledOutput rejects a runaway `debug models` instead of parsing it.
	maxBundledOutput = 8 << 20
)

// effortIDRE is what an effort name from Codex must look like to be offered or
// passed on: it ends up inside a `-c model_reasoning_effort="..."` argument.
var effortIDRE = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,15}$`)

type modelsEntry struct {
	models []agents.ModelOption
	at     time.Time
	ok     bool
}

// Models suggests the models this profile can launch with, default first and
// hidden models dropped. It asks `codex app-server` for logged-in profiles and
// falls back to the list bundled in the binary. It never returns an error: a
// profile that cannot be queried simply gets the bundled list, or none.
// Results (and failures) are cached so opening the dialog does not spawn Codex.
func (p *Provider) Models(ctx context.Context, account agents.Account) ([]agents.ModelOption, error) {
	if models, ok := p.cachedModels(account.ID); ok {
		return models, nil
	}
	v, err := p.flights.do(ctx, "models:"+string(account.ID), func(ctx context.Context) any {
		models, ok := p.fetchModels(ctx, account)
		p.modelsMu.Lock()
		if p.modelCache == nil {
			p.modelCache = map[agents.AccountID]modelsEntry{}
		}
		p.modelCache[account.ID] = modelsEntry{models: models, at: p.now(), ok: ok}
		p.modelsMu.Unlock()
		return models
	})
	if err != nil { // the caller gave up; nothing was learned about Codex
		return nil, nil
	}
	return append([]agents.ModelOption(nil), v.([]agents.ModelOption)...), nil
}

// cachedModels returns a copy of the cached list while it is inside its TTL.
func (p *Provider) cachedModels(id agents.AccountID) ([]agents.ModelOption, bool) {
	p.modelsMu.Lock()
	entry, ok := p.modelCache[id]
	p.modelsMu.Unlock()
	if !ok {
		return nil, false
	}
	ttl := modelsFailureTTL
	if entry.ok {
		ttl = modelsTTL
	}
	if p.now().Sub(entry.at) >= ttl {
		return nil, false
	}
	return append([]agents.ModelOption(nil), entry.models...), true
}

// fetchModels reports whether the live list (not the bundled fallback) was used.
func (p *Provider) fetchModels(ctx context.Context, account agents.Account) ([]agents.ModelOption, bool) {
	// Only a profile with a stored login is worth a process; a logged-out one
	// gets the bundled list straight away.
	if readIdentity(homeDir(p.accounts, account)).Present {
		if models := p.listModels(ctx, account); len(models) > 0 {
			return models, true
		}
	}
	return p.bundledModels(ctx, account), false
}

func (p *Provider) listModels(ctx context.Context, account agents.Account) []agents.ModelOption {
	var all []appModel
	err := p.withAppServer(ctx, account, func(ctx context.Context, conn *rpcConn) error {
		params := map[string]any{}
		for page := 0; page < maxModelPages; page++ {
			raw, err := conn.call(ctx, "model/list", params)
			if err != nil {
				return err
			}
			var result struct {
				Data       []appModel `json:"data"`
				NextCursor *string    `json:"nextCursor"`
			}
			if json.Unmarshal(raw, &result) != nil {
				return errors.New("unrecognized model list")
			}
			all = append(all, result.Data...)
			if result.NextCursor == nil || *result.NextCursor == "" {
				return nil
			}
			params = map[string]any{"cursor": *result.NextCursor}
		}
		return nil
	})
	if err != nil && len(all) == 0 {
		return nil
	}
	return modelOptions(all, "api")
}

// bundledModels reads the model list compiled into the binary. It needs no
// login and no network.
func (p *Provider) bundledModels(ctx context.Context, account agents.Account) []agents.ModelOption {
	prepared, err := p.profileCommand(account, "debug", "models", "--bundled")
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, bundledTimeout)
	defer cancel()
	out, err := p.runner.Output(ctx, prepared, nil)
	if err != nil || len(out) > maxBundledOutput {
		return nil
	}
	models, err := ParseBundledModels(out)
	if err != nil {
		return nil
	}
	return models
}

// appModel is one entry of the app-server `model/list` result.
type appModel struct {
	ID                        string `json:"id"`
	Model                     string `json:"model"`
	DisplayName               string `json:"displayName"`
	Description               string `json:"description"`
	Hidden                    bool   `json:"hidden"`
	IsDefault                 bool   `json:"isDefault"`
	DefaultReasoningEffort    string `json:"defaultReasoningEffort"`
	SupportedReasoningEfforts []struct {
		ReasoningEffort string `json:"reasoningEffort"`
	} `json:"supportedReasoningEfforts"`
}

// modelOptions converts listed models: hidden and malformed ones dropped,
// duplicates removed, the default model first and the rest in listed order.
func modelOptions(models []appModel, source string) []agents.ModelOption {
	seen := map[string]bool{}
	var out []agents.ModelOption
	for _, m := range models {
		id := strings.TrimSpace(first(m.ID, m.Model))
		if m.Hidden || id == "" || seen[id] || validateModel(id) != nil || strings.ContainsAny(id, " \t") {
			continue
		}
		seen[id] = true
		option := agents.ModelOption{
			ID: id, Label: cleanText(m.DisplayName, 128, id), Description: cleanText(m.Description, 256, ""),
			Source: source, Default: m.IsDefault,
		}
		for _, effort := range m.SupportedReasoningEfforts {
			if effortIDRE.MatchString(effort.ReasoningEffort) && !slices.Contains(option.Efforts, effort.ReasoningEffort) {
				option.Efforts = append(option.Efforts, effort.ReasoningEffort)
			}
		}
		if slices.Contains(option.Efforts, m.DefaultReasoningEffort) {
			option.DefaultEffort = m.DefaultReasoningEffort
		}
		// A listed model with no reasoning levels at all has no effort control.
		option.NoEffort = len(option.Efforts) == 0 && m.DefaultReasoningEffort == ""
		out = append(out, option)
		if len(out) == maxModels {
			break
		}
	}
	// Stable: the default moves to the front, everything else keeps its order.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Default && !out[j].Default })
	return out
}

// ParseBundledModels reads `codex debug models --bundled`. That document has no
// default flag; the visible model with the lowest priority is the one Codex
// picks. Entries keep priority order.
func ParseBundledModels(data []byte) ([]agents.ModelOption, error) {
	var doc struct {
		Models []struct {
			Slug                   string `json:"slug"`
			DisplayName            string `json:"display_name"`
			Description            string `json:"description"`
			Visibility             string `json:"visibility"`
			Priority               int    `json:"priority"`
			DefaultReasoningLevel  string `json:"default_reasoning_level"`
			SupportedReasoningLvls []struct {
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
		} `json:"models"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return nil, errors.New("unrecognized bundled model list")
	}
	sort.SliceStable(doc.Models, func(i, j int) bool { return doc.Models[i].Priority < doc.Models[j].Priority })
	converted := make([]appModel, 0, len(doc.Models))
	defaultSet := false
	for _, m := range doc.Models {
		am := appModel{
			ID: m.Slug, DisplayName: m.DisplayName, Description: m.Description,
			Hidden: m.Visibility != "list", DefaultReasoningEffort: m.DefaultReasoningLevel,
		}
		for _, level := range m.SupportedReasoningLvls {
			am.SupportedReasoningEfforts = append(am.SupportedReasoningEfforts, struct {
				ReasoningEffort string `json:"reasoningEffort"`
			}{level.Effort})
		}
		if !am.Hidden && !defaultSet {
			am.IsDefault = true
			defaultSet = true
		}
		converted = append(converted, am)
	}
	return modelOptions(converted, "bundled"), nil
}

// cleanText trims s and drops control characters. Text over max runes becomes
// fallback when there is one, else is cut at max.
func cleanText(s string, max int, fallback string) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s))
	switch runes := []rune(s); {
	case s == "":
		return fallback
	case len(runes) > max && fallback != "":
		return fallback
	case len(runes) > max:
		return strings.TrimSpace(string(runes[:max]))
	}
	return s
}

// effortsFor returns the efforts a profile's cached model list says the model
// accepts, or nil when the list is unknown or fresh data is not cached. An empty
// model means the profile's default model.
func (p *Provider) effortsFor(account agents.Account, model string) []string {
	models, ok := p.cachedModels(account.ID)
	if !ok {
		return nil
	}
	for _, m := range models {
		if (model != "" && m.ID == model) || (model == "" && m.Default) {
			return m.Efforts
		}
	}
	return nil
}
