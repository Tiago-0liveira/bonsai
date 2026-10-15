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
	usageTTL = time.Minute
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
	parsed, err := ParseRateLimits(raw)
	if err != nil {
		return agents.UsageSnapshot{}, err
	}
	if parsed.Plan == "" {
		parsed.Plan = id.Plan
	}
	return agents.UsageSnapshot{
		Provider: ProviderID, AccountID: account.ID, FetchedAt: p.now().UTC(),
		Plan: planLabel(parsed.Plan), Limits: parsed.Limits, Credits: parsed.Credits,
		ResetCredits: parsed.ResetCredits, Warnings: parsed.Warnings,
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

// resetCredits is `rateLimitResetCredits`: one-off resets the account was granted.
type resetCredits struct {
	AvailableCount *int `json:"availableCount"`
	Credits        []struct {
		Status    string `json:"status"`
		Title     string `json:"title"`
		GrantedAt *int64 `json:"grantedAt"`
		ExpiresAt *int64 `json:"expiresAt"`
	} `json:"credits"`
}

// ParsedRateLimits is what ParseRateLimits reads from one response.
type ParsedRateLimits struct {
	Limits       []agents.UsageLimit
	Plan         string
	Credits      []agents.UsageCredit
	ResetCredits []agents.ResetCredit
	Warnings     []string
}

// ParseRateLimits reads an `account/rateLimits/read` result. Unknown fields and
// shapes are ignored: valid JSON with nothing recognizable is an empty snapshot
// with a warning, and only text that is not a JSON object is an error.
//
// Windows are named by their length, not their slot: 300 minutes is
// `five_hour`, 10080 is `weekly`, anything else `window_<n>m`. Limits of other
// metered features (`rateLimitsByLimitId`) become their own group, with ids
// prefixed by the limit id.
func ParseRateLimits(data []byte) (ParsedRateLimits, error) {
	var result struct {
		RateLimits            *rateLimitSnapshot           `json:"rateLimits"`
		RateLimitsByLimitID   map[string]rateLimitSnapshot `json:"rateLimitsByLimitId"`
		RateLimitResetCredits json.RawMessage              `json:"rateLimitResetCredits"`
	}
	var probe map[string]json.RawMessage
	if json.Unmarshal(data, &probe) != nil || probe == nil {
		return ParsedRateLimits{}, errors.New("unrecognized usage response")
	}
	if json.Unmarshal(data, &result) != nil {
		return ParsedRateLimits{Warnings: []string{"Codex usage response had no recognizable limits"}}, nil
	}
	var out ParsedRateLimits

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
		out.Plan = cleanText(main.PlanType, 32, "")
		out.Limits = append(out.Limits, snapshotLimits(*main, "", "codex", "")...)
		if credit, ok := creditBalance(*main); ok {
			out.Credits = append(out.Credits, credit)
		}
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
		out.Limits = append(out.Limits, snapshotLimits(snap, id+"_", group, group+" ")...)
	}
	sortLimits(out.Limits)
	out.ResetCredits = parseResetCredits(result.RateLimitResetCredits)
	if len(out.Limits) == 0 {
		out.Warnings = append(out.Warnings, "Codex usage response had no recognizable limits")
	}
	return out, nil
}

// parseResetCredits keeps each granted reset's title, status and dates; the
// credit IDs and descriptions are dropped. A malformed block is ignored.
func parseResetCredits(raw json.RawMessage) []agents.ResetCredit {
	var block resetCredits
	if len(raw) == 0 || json.Unmarshal(raw, &block) != nil {
		return nil
	}
	var out []agents.ResetCredit
	for _, c := range block.Credits {
		out = append(out, agents.ResetCredit{
			Title:     cleanText(c.Title, 64, "Rate limit reset"),
			Status:    cleanText(c.Status, 32, ""),
			GrantedAt: unixTime(c.GrantedAt),
			ExpiresAt: unixTime(c.ExpiresAt),
		})
	}
	return out
}

func unixTime(sec *int64) *time.Time {
	if sec == nil || *sec <= 0 {
		return nil
	}
	t := time.Unix(*sec, 0).UTC()
	return &t
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

// creditBalance turns the `credits` block into a credit balance. The balance
// is a decimal string; one that does not parse is left out.
func creditBalance(snap rateLimitSnapshot) (agents.UsageCredit, bool) {
	c := snap.Credits
	if c == nil {
		return agents.UsageCredit{}, false
	}
	credit := agents.UsageCredit{
		ID: "codex_credits", Label: "Credits", Unit: agents.CreditUnitCredits,
		Unlimited: c.Unlimited, Enabled: c.HasCredits || c.Unlimited,
	}
	if c.Balance != nil {
		if v, err := strconv.ParseFloat(strings.TrimSpace(*c.Balance), 64); err == nil && !math.IsNaN(v) && !math.IsInf(v, 0) {
			credit.Balance = &v
		}
	}
	return credit, true
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
