package claude

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// maxCredentialsFile bounds how much of .credentials.json is read for the plan.
const maxCredentialsFile = 1 << 20

// storedPlan reads the subscription Claude Code keeps beside the login in the
// profile's .credentials.json, so no process starts. It is "" when the file is
// missing (macOS keeps it in the keychain) or says nothing.
func (p *Provider) storedPlan(account agents.Account) string {
	f, err := os.Open(filepath.Join(configDir(p.accounts, account), ".credentials.json"))
	if err != nil {
		return ""
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxCredentialsFile))
	if err != nil {
		return ""
	}
	var creds struct {
		OAuth struct {
			SubscriptionType string `json:"subscriptionType"`
			RateLimitTier    string `json:"rateLimitTier"`
		} `json:"claudeAiOauth"`
	}
	if json.Unmarshal(data, &creds) != nil {
		return ""
	}
	return planLabel(creds.OAuth.SubscriptionType, creds.OAuth.RateLimitTier)
}

// planLabel names a subscription for display. The rate limit tier only tells
// the Max plans apart ("default_claude_max_20x"); unknown subscriptions pass
// through as sent.
func planLabel(subscription, tier string) string {
	subscription = strings.ToLower(strings.TrimSpace(subscription))
	switch subscription {
	case "":
		return ""
	case "pro":
		return "Pro"
	case "max":
		tier = strings.ToLower(tier)
		switch {
		case strings.Contains(tier, "20x"):
			return "Max 20x"
		case strings.Contains(tier, "5x"):
			return "Max 5x"
		}
		return "Max"
	case "team":
		return "Team"
	case "enterprise":
		return "Enterprise"
	case "free":
		return "Free"
	}
	return cleanLabel(subscription, 32)
}

// cleanLabel keeps a provider-sent name printable and short.
func cleanLabel(s string, limit int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return -1
	}, strings.TrimSpace(s))
	if r := []rune(s); len(r) > limit {
		s = string(r[:limit])
	}
	return s
}
