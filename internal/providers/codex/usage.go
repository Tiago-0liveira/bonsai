package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

const (
	usageTTL = 5 * time.Minute
	// usageFailureTTL is how long a failed read is remembered so repeated
	// dashboard runs do not spawn Codex for a login that is rate limited or down.
	usageFailureTTL = time.Minute

	fiveHourMins = 300
	weeklyMins   = 10080
)

// UsageTTL tells the usage service how long a snapshot stays fresh.
func (p *Provider) UsageTTL() time.Duration { return usageTTL }

func unsupported(reason string) error {
	return fmt.Errorf("%w: %s", agents.ErrUsageUnsupported, reason)
}

type usageFailure struct {
	err error
	at  time.Time
}

// Usage reads the 5-hour and weekly rate limits of one profile through
// `codex app-server` (`account/rateLimits/read`). Profiles that can have no
// answer are decided from their auth.json alone, without starting Codex.
func (p *Provider) Usage(ctx context.Context, account agents.Account, opts agents.UsageOptions) (agents.UsageSnapshot, error) {
	settings, err := ParseSettings(account)
	if err != nil {
		return agents.UsageSnapshot{}, errors.New("invalid profile settings")
	}
	id := readIdentity(homeDir(p.accounts, account))
	switch {
	case id.APIKey || (!id.Present && settings.AuthMode == AuthAPIKey):
		return agents.UsageSnapshot{}, unsupported("API key profiles have no plan limits")
	case !id.Present:
		return agents.UsageSnapshot{}, fmt.Errorf("%w: run `bonsai agent run %s -- login`", agents.ErrNotAuthenticated, account.Name)
	}
	var raw json.RawMessage
	if !opts.Refresh {
		if failure := p.recentUsageFailure(account.ID); failure != nil {
			return agents.UsageSnapshot{}, failure
		}
	}
	// Concurrent reads of one profile (dashboard, web poll) share one process.
	v, err := p.flights.do(ctx, "usage:"+string(account.ID), func(ctx context.Context) any {
		var read rateLimitRead
		read.err = p.withAppServer(ctx, account, func(ctx context.Context, conn *rpcConn) error {
			var err error
			read.raw, err = conn.call(ctx, "account/rateLimits/read", map[string]any{})
			return err
		})
		return read
	})
	if err == nil {
		read := v.(rateLimitRead)
		err = read.err
		raw = read.raw
	}
	if err != nil {
		err = fmt.Errorf("codex usage unavailable: %w", sanitizeUsageError(err))
		p.rememberUsageFailure(ctx, account.ID, err)
		return agents.UsageSnapshot{}, err
	}
	limits, plan, warnings, err := ParseRateLimits(raw)
	if err != nil {
		return agents.UsageSnapshot{}, err
	}
	if plan == "" {
		plan = id.Plan
	}
	return agents.UsageSnapshot{
		Provider: ProviderID, AccountID: account.ID, FetchedAt: p.now().UTC(),
		Plan: plan, Limits: limits, Warnings: warnings,
	}, nil
}

type rateLimitRead struct {
	raw json.RawMessage
	err error
}

// sanitizeUsageError keeps what is useful (timeout, code, early exit) and drops
// anything else a process or server could have put into an error.
func sanitizeUsageError(err error) error {
	var rpc *rpcError
	switch {
	case errors.As(err, &rpc):
		return fmt.Errorf("server error (code %d)", rpc.code)
	case errors.Is(err, context.DeadlineExceeded):
		return errors.New("timed out")
	case errors.Is(err, context.Canceled):
		return context.Canceled
	}
	msg := err.Error()
	for _, known := range []string{"exited early", "response too large"} {
		if strings.Contains(msg, known) {
			return errors.New(known)
		}
	}
	return errors.New("app-server failed")
}

func (p *Provider) recentUsageFailure(id agents.AccountID) error {
	p.usageMu.Lock()
	defer p.usageMu.Unlock()
	if f, ok := p.usageFailures[id]; ok && p.now().Sub(f.at) < usageFailureTTL {
		return f.err
	}
	return nil
}

func (p *Provider) rememberUsageFailure(ctx context.Context, id agents.AccountID, err error) {
	// A cancelled caller says nothing about Codex.
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return
	}
	p.usageMu.Lock()
	defer p.usageMu.Unlock()
	if p.usageFailures == nil {
		p.usageFailures = map[agents.AccountID]usageFailure{}
	}
	p.usageFailures[id] = usageFailure{err: err, at: p.now()}
}

// rateLimitWindow is one `primary`/`secondary` entry of a rate limit snapshot.
type rateLimitWindow struct {
	UsedPercent        *float64 `json:"usedPercent"`
	WindowDurationMins *int64   `json:"windowDurationMins"`
	ResetsAt           *int64   `json:"resetsAt"`
}

type rateLimitSnapshot struct {
	LimitID   string           `json:"limitId"`
	LimitName string           `json:"limitName"`
	Primary   *rateLimitWindow `json:"primary"`
	Secondary *rateLimitWindow `json:"secondary"`
	Credits   *struct {
		HasCredits bool    `json:"hasCredits"`
		Unlimited  bool    `json:"unlimited"`
		Balance    *string `json:"balance"`
	} `json:"credits"`
	PlanType string `json:"planType"`
}

// ParseRateLimits reads an `account/rateLimits/read` result. Unknown fields and
// shapes are ignored: valid JSON with nothing recognizable is an empty snapshot
// with a warning, and only text that is not a JSON object is an error.
//
// Windows are named by their length, not their slot: 300 minutes is
// `five_hour`, 10080 is `weekly`, anything else `window_<n>m`. Limits of other
// metered features (`rateLimitsByLimitId`) become their own group, with ids
// prefixed by the limit id.
func ParseRateLimits(data []byte) (limits []agents.UsageLimit, plan string, warnings []string, err error) {
	var result struct {
		RateLimits          *rateLimitSnapshot           `json:"rateLimits"`
		RateLimitsByLimitID map[string]rateLimitSnapshot `json:"rateLimitsByLimitId"`
	}
	var probe map[string]json.RawMessage
	if json.Unmarshal(data, &probe) != nil || probe == nil {
		return nil, "", nil, errors.New("unrecognized usage response")
	}
	if json.Unmarshal(data, &result) != nil {
		return nil, "", []string{"Codex usage response had no recognizable limits"}, nil
	}

	main := result.RateLimits
	if main == nil {
		if s, ok := result.RateLimitsByLimitID["codex"]; ok {
			main = &s
		}
	}
	mainID := ""
	if main != nil {
		mainID = main.LimitID
		if mainID == "" {
			mainID = "codex"
		}
		plan = cleanText(main.PlanType, 32, "")
		limits = append(limits, snapshotLimits(*main, "", "codex", "")...)
		warnings = append(warnings, creditWarnings(*main)...)
	}
	extraIDs := make([]string, 0, len(result.RateLimitsByLimitID))
	for id := range result.RateLimitsByLimitID {
		if id != mainID {
			extraIDs = append(extraIDs, id)
		}
	}
	sort.Strings(extraIDs)
	for _, key := range extraIDs {
		snap := result.RateLimitsByLimitID[key]
		id := cleanText(first(snap.LimitID, key), 64, "")
		if id == "" {
			continue
		}
		group := cleanText(first(snap.LimitName, id), 64, id)
		limits = append(limits, snapshotLimits(snap, id+"_", group, group+" ")...)
	}
	sortLimits(limits)
	if len(limits) == 0 {
		warnings = append(warnings, "Codex usage response had no recognizable limits")
	}
	return limits, plan, warnings, nil
}

// snapshotLimits turns the windows of one snapshot into limits.
func snapshotLimits(snap rateLimitSnapshot, idPrefix, group, labelPrefix string) []agents.UsageLimit {
	var out []agents.UsageLimit
	seen := map[string]bool{}
	for slot, w := range []*rateLimitWindow{snap.Primary, snap.Secondary} {
		if w == nil || w.UsedPercent == nil || math.IsNaN(*w.UsedPercent) || math.IsInf(*w.UsedPercent, 0) {
			continue
		}
		name, label, window := windowName(w.WindowDurationMins, slot)
		if seen[name] {
			continue
		}
		seen[name] = true
		remaining := 1 - math.Min(math.Max(*w.UsedPercent, 0), 100)/100
		limit := agents.UsageLimit{
			ID: idPrefix + name, Group: group, Label: labelPrefix + label, Window: window, RemainingFraction: &remaining,
		}
		if w.ResetsAt != nil && *w.ResetsAt > 0 {
			t := time.Unix(*w.ResetsAt, 0).UTC()
			limit.ResetsAt = &t
		}
		out = append(out, limit)
	}
	return out
}

// windowName maps a window length in minutes to (id, label, window). A window
// without a length falls back to its slot so nothing is silently dropped.
func windowName(mins *int64, slot int) (id, label, window string) {
	switch {
	case mins == nil || *mins <= 0:
		if slot == 0 {
			return "primary", "primary", ""
		}
		return "secondary", "secondary", ""
	case *mins == fiveHourMins:
		return "five_hour", "5h", "5h"
	case *mins == weeklyMins:
		return "weekly", "Weekly", "weekly"
	}
	n := *mins
	id = "window_" + strconv.FormatInt(n, 10) + "m"
	switch {
	case n%1440 == 0:
		label = strconv.FormatInt(n/1440, 10) + "d"
	case n%60 == 0:
		label = strconv.FormatInt(n/60, 10) + "h"
	default:
		label = strconv.FormatInt(n, 10) + "m"
	}
	return id, label, label
}

func creditWarnings(snap rateLimitSnapshot) []string {
	c := snap.Credits
	switch {
	case c == nil:
		return nil
	case c.Unlimited:
		return []string{"Unlimited credits"}
	case c.HasCredits && c.Balance != nil:
		if balance := cleanText(*c.Balance, 32, ""); balance != "" {
			return []string{"Credit balance: " + balance}
		}
	case c.HasCredits:
		return []string{"Credits available"}
	}
	return nil
}

// sortLimits puts the main group first (5h, weekly, other windows by length),
// then each extra group by id, so output does not depend on map order.
func sortLimits(limits []agents.UsageLimit) {
	rank := func(l agents.UsageLimit) int {
		switch l.ID {
		case "five_hour":
			return 0
		case "weekly":
			return 1
		case "primary", "secondary":
			return 2
		}
		if strings.HasPrefix(l.ID, "window_") {
			return 3
		}
		return 4
	}
	sort.SliceStable(limits, func(i, j int) bool {
		ri, rj := rank(limits[i]), rank(limits[j])
		if ri != rj {
			return ri < rj
		}
		return limits[i].ID < limits[j].ID
	})
}
