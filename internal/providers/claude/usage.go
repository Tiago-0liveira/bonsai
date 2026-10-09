package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// The usage endpoint is NOT documented by Anthropic. It is what Claude Code's
// own /usage view reads, so it can change or disappear without notice. This file
// is the only place that knows about it; every failure is soft.
//
// Bonsai only ever reads the stored access token. It never refreshes it: a
// refresh rotates the refresh token and would log out running sessions.
const (
	usageBeta    = "oauth-2025-04-20"
	usageTimeout = 10 * time.Second
	usageTTL     = 5 * time.Minute
	// maxUsageBody caps what is read from the endpoint.
	maxUsageBody = 1 << 20
)

// UsageEndpoint is where new providers read usage. Other packages' tests point
// it at a local server so no test ever reaches the network.
var UsageEndpoint = "https://api.anthropic.com/api/oauth/usage"

// UsageTTL tells the usage service how long a snapshot stays fresh; the
// endpoint is rate limited and the numbers move slowly.
func (p *Provider) UsageTTL() time.Duration { return usageTTL }

func unsupported(reason string) error {
	return fmt.Errorf("%w: %s", agents.ErrUsageUnsupported, reason)
}

// Usage reads the 5-hour and weekly utilization of one profile.
func (p *Provider) Usage(ctx context.Context, account agents.Account, opts agents.UsageOptions) (agents.UsageSnapshot, error) {
	settings, err := ParseSettings(account)
	if err != nil {
		return agents.UsageSnapshot{}, fmt.Errorf("invalid profile settings")
	}
	if settings.AuthMode == AuthToken {
		// Setup tokens lack the user:profile scope (observed: always HTTP 403), so
		// the long-lived token is not sent for a request that cannot succeed.
		return agents.UsageSnapshot{}, unsupported("long-lived tokens cannot read usage")
	}
	token, err := p.usageToken(account)
	if err != nil {
		return agents.UsageSnapshot{}, err
	}
	if !opts.Refresh {
		if failure := p.recentUsageFailure(account.ID); failure != nil {
			return agents.UsageSnapshot{}, failure
		}
	}
	body, err := p.fetchUsage(ctx, token)
	if err != nil {
		err = redact(err, token)
		p.rememberUsageFailure(account.ID, err)
		return agents.UsageSnapshot{}, err
	}
	limits, warnings, err := ParseUsage(body)
	if err != nil {
		return agents.UsageSnapshot{}, err
	}
	return agents.UsageSnapshot{Provider: ProviderID, AccountID: account.ID, FetchedAt: p.now().UTC(), Limits: limits, Warnings: warnings}, nil
}

// usageFailureTTL is how long an HTTP failure (rate limit, outage) is remembered
// so repeated dashboard runs do not hammer the endpoint.
const usageFailureTTL = time.Minute

type usageFailure struct {
	err error
	at  time.Time
}

func (p *Provider) recentUsageFailure(id agents.AccountID) error {
	p.usageMu.Lock()
	defer p.usageMu.Unlock()
	if f, ok := p.usageFailures[id]; ok && p.now().Sub(f.at) < usageFailureTTL {
		return f.err
	}
	return nil
}

func (p *Provider) rememberUsageFailure(id agents.AccountID, err error) {
	// Only remote failures count: cancellation is the caller's, and unsupported
	// is already a stable answer.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, agents.ErrUsageUnsupported) {
		return
	}
	p.usageMu.Lock()
	defer p.usageMu.Unlock()
	if p.usageFailures == nil {
		p.usageFailures = map[agents.AccountID]usageFailure{}
	}
	p.usageFailures[id] = usageFailure{err: err, at: p.now()}
}

// usageToken returns the access token Claude Code keeps in the profile's
// .credentials.json (absent on macOS, where it is in the Keychain).
func (p *Provider) usageToken(account agents.Account) (token string, err error) {
	data, err := os.ReadFile(filepath.Join(configDir(p.accounts, account), ".credentials.json"))
	switch {
	case errors.Is(err, os.ErrNotExist) && p.goos() == "darwin":
		return "", unsupported("login is stored in the macOS keychain")
	case errors.Is(err, os.ErrNotExist):
		return "", errors.New("not logged in; start a session and run /login")
	case err != nil:
		return "", errors.New("cannot read login credentials")
	}
	var creds struct {
		OAuth struct {
			AccessToken string `json:"accessToken"`
			ExpiresAt   int64  `json:"expiresAt"` // milliseconds since the epoch
		} `json:"claudeAiOauth"`
	}
	if json.Unmarshal(data, &creds) != nil || creds.OAuth.AccessToken == "" {
		return "", errors.New("login credentials are unreadable")
	}
	if creds.OAuth.ExpiresAt > 0 && !p.now().Before(time.UnixMilli(creds.OAuth.ExpiresAt)) {
		return "", errors.New("usage unavailable until a session refreshes the login")
	}
	return creds.OAuth.AccessToken, nil
}

func (p *Provider) goos() string {
	if p.usageOS != "" {
		return p.usageOS
	}
	return runtime.GOOS
}

func (p *Provider) fetchUsage(ctx context.Context, token string) ([]byte, error) {
	body, status, err := p.getJSON(ctx, p.usageEndpoint, token)
	if err != nil {
		return nil, err
	}
	switch {
	case status == http.StatusOK:
		return body, nil
	case status == http.StatusForbidden:
		return nil, unsupported("this login cannot read usage")
	case status == http.StatusUnauthorized:
		return nil, errors.New("login rejected; start a session to refresh it")
	default:
		return nil, fmt.Errorf("usage request failed (HTTP %d)", status)
	}
}

// getJSON is the one place Bonsai sends a Claude bearer token over HTTP: GET
// only, https or loopback only, no redirects, bounded body, and errors that
// carry neither the token nor the response. The body is returned only for 200.
func (p *Provider) getJSON(ctx context.Context, endpoint, token string) (body []byte, status int, err error) {
	ctx, cancel := context.WithTimeout(ctx, usageTimeout)
	defer cancel()
	if !safeUsageEndpoint(endpoint) {
		return nil, 0, errors.New("endpoint must use https")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, errors.New("request failed")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", usageBeta)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Accept", "application/json")
	client := p.usageClient
	if client == nil {
		client = noRedirectClient
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, fmt.Errorf("usage request failed: %w", ctx.Err())
		}
		return nil, 0, errors.New("usage request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, nil
	}
	body, err = io.ReadAll(io.LimitReader(resp.Body, maxUsageBody))
	if err != nil {
		return nil, 0, errors.New("usage request failed")
	}
	return body, resp.StatusCode, nil
}

// noRedirectClient never follows redirects: the bearer token must only ever go
// to the configured endpoint.
var noRedirectClient = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// safeUsageEndpoint allows https, and plain http only to the loopback interface
// (the local test servers), so the token never travels in clear text.
func safeUsageEndpoint(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	host := u.Hostname()
	return u.Scheme == "http" && (host == "localhost" || net.ParseIP(host).IsLoopback())
}

// redact guarantees the secret never reaches an error message, whatever the
// transport or server put into it.
func redact(err error, secret string) error {
	if err == nil || secret == "" || !strings.Contains(err.Error(), secret) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), secret, "[redacted]"))
}

// bucketKeyRE matches the flat buckets: five_hour, seven_day, seven_day_opus, ...
var bucketKeyRE = regexp.MustCompile(`^(five_hour|seven_day(_[a-z]+)?)$`)

var kindIDs = map[string]string{"session": "five_hour", "weekly_all": "seven_day", "weekly_opus": "seven_day_opus", "weekly_sonnet": "seven_day_sonnet"}

var limitLabels = map[string]string{"five_hour": "Session (5h)", "seven_day": "Weekly", "seven_day_opus": "Weekly Opus", "seven_day_sonnet": "Weekly Sonnet"}

// ParseUsage reads both response shapes: the newer limits[] array and the flat
// buckets ({"five_hour": {"utilization": 10, "resets_at": ...}}). Unknown or
// extra fields are ignored. Valid JSON with nothing recognizable is an empty
// snapshot with a warning; text that is not a JSON object is an error.
func ParseUsage(data []byte) (limits []agents.UsageLimit, warnings []string, err error) {
	var root map[string]json.RawMessage
	if json.Unmarshal(data, &root) != nil || root == nil {
		return nil, nil, errors.New("unrecognized usage response")
	}
	seen := map[string]bool{}
	add := func(id, group string, percent float64, resets string) {
		if seen[id] || math.IsNaN(percent) || math.IsInf(percent, 0) {
			return
		}
		seen[id] = true
		remaining := 1 - math.Min(math.Max(percent, 0), 100)/100
		limit := agents.UsageLimit{ID: id, Group: "claude", Label: limitLabels[id], Window: windowOf(id, group), RemainingFraction: &remaining}
		if limit.Label == "" {
			limit.Label = strings.ReplaceAll(id, "_", " ")
		}
		if t, err := time.Parse(time.RFC3339Nano, resets); err == nil {
			utc := t.UTC()
			limit.ResetsAt = &utc
		}
		limits = append(limits, limit)
	}

	var list []struct {
		Kind     string   `json:"kind"`
		Group    string   `json:"group"`
		Percent  *float64 `json:"percent"`
		ResetsAt string   `json:"resets_at"`
		Scope    *string  `json:"scope"`
	}
	if raw, ok := root["limits"]; ok {
		_ = json.Unmarshal(raw, &list) // a malformed list falls back to the flat buckets
	}
	for _, item := range list {
		if item.Percent == nil || item.Kind == "" {
			continue
		}
		id, ok := kindIDs[item.Kind]
		if !ok {
			id = item.Kind
			if item.Scope != nil && *item.Scope != "" {
				id += "_" + *item.Scope
			}
		}
		add(id, item.Group, *item.Percent, item.ResetsAt)
	}
	for key, raw := range root {
		if !bucketKeyRE.MatchString(key) {
			continue
		}
		var bucket struct {
			Utilization *float64 `json:"utilization"`
			ResetsAt    string   `json:"resets_at"`
		}
		if json.Unmarshal(raw, &bucket) != nil || bucket.Utilization == nil {
			continue
		}
		add(key, "", *bucket.Utilization, bucket.ResetsAt)
	}
	sortLimits(limits)
	if len(limits) == 0 {
		warnings = append(warnings, "Claude usage response had no recognizable limits")
	}
	return limits, warnings, nil
}

func windowOf(id, group string) string {
	switch {
	case id == "five_hour" || group == "session":
		return "5h"
	case strings.HasPrefix(id, "seven_day") || strings.HasPrefix(id, "weekly") || group == "weekly":
		return "weekly"
	}
	return ""
}

// sortLimits orders 5h first, then the weekly total, then per-model weekly
// limits by id, so output is stable regardless of map iteration order.
func sortLimits(limits []agents.UsageLimit) {
	rank := func(id string) int {
		switch id {
		case "five_hour":
			return 0
		case "seven_day":
			return 1
		}
		return 2
	}
	sort.Slice(limits, func(i, j int) bool {
		ri, rj := rank(limits[i].ID), rank(limits[j].ID)
		if ri != rj {
			return ri < rj
		}
		return limits[i].ID < limits[j].ID
	})
}
