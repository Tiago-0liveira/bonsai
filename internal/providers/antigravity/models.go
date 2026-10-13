package antigravity

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

const (
	modelsTimeout    = 15 * time.Second
	modelsTTL        = 10 * time.Minute
	modelsFailureTTL = time.Minute
	maxModels        = 200
)

type modelsEntry struct {
	models []agents.ModelOption
	at     time.Time
	ok     bool
}

// Models lists the models agy offers this profile (`agy models`), with the
// efforts of the models it lists once per effort. It never returns an error: a
// failure is an empty list, cached briefly so opening the dialog does not start
// agy every time.
func (p *Provider) Models(ctx context.Context, account agents.Account) ([]agents.ModelOption, error) {
	if models, ok := p.cachedModels(account.ID); ok {
		return models, nil
	}
	ctx, cancel := context.WithTimeout(ctx, modelsTimeout)
	defer cancel()
	var models []agents.ModelOption
	err := p.withProfile(ctx, account, func(ctx context.Context, run profileRun) error {
		out, err := p.runner()(ctx, run.executable, run.dir, run.env, "models")
		models = ParseModels(out)
		return err
	})
	// A cancelled caller says nothing about agy, so it is not remembered.
	if errors.Is(err, context.Canceled) {
		return []agents.ModelOption{}, nil
	}
	ok := err == nil && len(models) > 0
	if !ok {
		models = nil
	}
	p.modelsMu.Lock()
	if p.modelsCache == nil {
		p.modelsCache = map[agents.AccountID]modelsEntry{}
	}
	p.modelsCache[account.ID] = modelsEntry{models: models, at: p.clock(), ok: ok}
	p.modelsMu.Unlock()
	return append([]agents.ModelOption{}, models...), nil
}

// cachedModels returns the profile's model list while it is fresh.
func (p *Provider) cachedModels(id agents.AccountID) ([]agents.ModelOption, bool) {
	p.modelsMu.Lock()
	defer p.modelsMu.Unlock()
	entry, ok := p.modelsCache[id]
	if !ok {
		return nil, false
	}
	ttl := modelsFailureTTL
	if entry.ok {
		ttl = modelsTTL
	}
	if p.clock().Sub(entry.at) >= ttl {
		return nil, false
	}
	return append([]agents.ModelOption{}, entry.models...), true
}

// knownModels is the last successful list, however old, for launch checks that
// must not start agy. Nil when there is none.
func (p *Provider) knownModels(id agents.AccountID) []agents.ModelOption {
	p.modelsMu.Lock()
	defer p.modelsMu.Unlock()
	return p.modelsCache[id].models
}

var (
	effortIDRE    = regexp.MustCompile(`^(.+)-([A-Za-z0-9]+)$`)
	effortLabelRE = regexp.MustCompile(`^(.*?)\s*\(([^()]+)\)$`)
	// effortOrder sorts the efforts agy names; unknown words follow in listed order.
	effortOrder = []string{"low", "medium", "high", "xhigh", "max"}
)

// ParseModels reads `agy models`: one "id<TAB>label" line per model after a
// "Fetching available models..." line. Models listed once per effort
// ("gemini-3.8-flash-high<TAB>Gemini 3.8 Flash (High)") become one model with
// those efforts; the first listed is the default. A lone "-medium" model stays
// whole and, like every model listed once, has no effort choice (NoEffort).
// Lines without a tab are ignored.
func ParseModels(out []byte) []agents.ModelOption {
	type line struct{ id, label, base, baseLabel, effort string }
	var lines []line
	seen := map[string]bool{}
	for _, raw := range strings.Split(string(out), "\n") {
		id, label, ok := strings.Cut(strings.TrimRight(raw, "\r"), "\t")
		id, label = strings.TrimSpace(id), strings.TrimSpace(label)
		if !ok || id == "" || seen[id] || validateModel(id) != nil || len(lines) >= maxModels {
			continue
		}
		seen[id] = true
		l := line{id: id, label: cleanLabel(label, id)}
		if m := effortIDRE.FindStringSubmatch(id); m != nil {
			if lm := effortLabelRE.FindStringSubmatch(l.label); lm != nil && strings.EqualFold(m[2], lm[2]) && lm[1] != "" {
				l.base, l.baseLabel, l.effort = m[1], lm[1], strings.ToLower(m[2])
			}
		}
		lines = append(lines, l)
	}
	type key struct{ base, label string }
	members := map[key][]string{}
	for _, l := range lines {
		if l.effort != "" {
			k := key{l.base, l.baseLabel}
			members[k] = append(members[k], l.effort)
		}
	}
	models := []agents.ModelOption{}
	done := map[key]bool{}
	for _, l := range lines {
		k := key{l.base, l.baseLabel}
		if l.effort == "" || len(members[k]) < 2 {
			// agy lists every effort of a model as its own ID, so a model listed
			// once has no effort to choose.
			models = append(models, agents.ModelOption{ID: l.id, Label: l.label, Source: "bundled", NoEffort: true})
			continue
		}
		if done[k] {
			continue
		}
		done[k] = true
		efforts := sortEfforts(members[k])
		models = append(models, agents.ModelOption{ID: l.base, Label: l.baseLabel, Source: "bundled", Efforts: efforts, DefaultEffort: members[k][0]})
	}
	return models
}

func sortEfforts(listed []string) []string {
	out := slices.Clone(listed)
	rank := func(e string) int {
		if i := slices.Index(effortOrder, e); i >= 0 {
			return i
		}
		return len(effortOrder) + slices.Index(listed, e)
	}
	slices.SortStableFunc(out, func(a, b string) int { return rank(a) - rank(b) })
	return out
}

// cleanLabel keeps a listed label printable and short; an empty one is the ID.
func cleanLabel(label, id string) string {
	label = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, label)
	if r := []rune(label); len(r) > 80 {
		label = string(r[:80])
	}
	if label == "" {
		return id
	}
	return label
}

// composeModel turns a model and an effort into the one ID agy takes with
// --model. A listed model with efforts gets its default effort when none is
// chosen; a full ID ("gemini-3.8-flash-low") or a model without efforts passes
// through unchanged, unless an effort is chosen for it explicitly.
func composeModel(model, effort string, models []agents.ModelOption) string {
	if model == "" {
		return ""
	}
	if listed := findModel(models, model); listed != nil {
		switch {
		case len(listed.Efforts) == 0:
			return model
		case effort != "":
			return model + "-" + effort
		case listed.DefaultEffort != "":
			return model + "-" + listed.DefaultEffort
		}
		return model
	}
	if effort == "" {
		return model
	}
	// A full ID of a listed model: swap its effort.
	if base, ok := effortBase(models, model); ok {
		return base + "-" + effort
	}
	return model + "-" + effort
}

func findModel(models []agents.ModelOption, id string) *agents.ModelOption {
	for i := range models {
		if models[i].ID == id {
			return &models[i]
		}
	}
	return nil
}

// effortBase reports the listed model a full ID belongs to ("gemini-3.8-flash"
// for "gemini-3.8-flash-low").
func effortBase(models []agents.ModelOption, id string) (string, bool) {
	for _, m := range models {
		for _, e := range m.Efforts {
			if id == m.ID+"-"+e {
				return m.ID, true
			}
		}
	}
	return "", false
}

var effortRE = regexp.MustCompile(`^[a-z0-9]{1,16}$`)

// validateEffort checks an effort against the model's listed efforts when the
// list knows the model, and its shape otherwise. A model listed without
// efforts accepts any effort and launches without one (composeModel drops it),
// so a leftover choice never blocks that model.
func validateEffort(model, effort string, models []agents.ModelOption) error {
	if effort == "" {
		return nil
	}
	if !effortRE.MatchString(effort) {
		return errors.New("invalid effort")
	}
	listed := findModel(models, model)
	if listed == nil {
		if base, ok := effortBase(models, model); ok {
			listed = findModel(models, base)
		}
	}
	if listed != nil && len(listed.Efforts) > 0 && !slices.Contains(listed.Efforts, effort) {
		return errors.New("unsupported effort " + effort + " for this model (use " + strings.Join(listed.Efforts, ", ") + ")")
	}
	return nil
}
