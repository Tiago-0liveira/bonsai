// Package livehooks keeps one Bonsai repository webhook per live-updates
// repository pointed at the current tunnel: find it (stored ID, else the
// ?bonsai=<install-id> marker), create it when missing, re-point it when the
// public URL or secret changes, and read its health from GitHub's last
// delivery. Reconcile and Check never delete a hook; only Remove does, and
// only setup calls it after the user confirmed the change in Review.
package livehooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"github.com/Tiago-0liveira/bonsai/internal/git/github/app"
	"github.com/Tiago-0liveira/bonsai/internal/server/webhooks"
)

// GitHubHost is the only host live updates support.
const GitHubHost = "github.com"

// MarkerParam is the hook URL query parameter that carries the install ID.
const MarkerParam = "bonsai"

// waitForPing is how long a freshly configured hook stays waiting_for_ping
// before GitHub's last delivery result decides its state.
const waitForPing = 30 * time.Second

// API is the part of the GitHub client the hooks need (app.Client).
type API interface {
	RepoAdmin(ctx context.Context, repo string) (bool, error)
	ListHooks(ctx context.Context, repo string) ([]app.Hook, error)
	GetHook(ctx context.Context, repo string, id int64) (app.Hook, error)
	CreateHook(ctx context.Context, repo string, spec app.HookSpec) (app.Hook, error)
	UpdateHook(ctx context.Context, repo string, id int64, spec app.HookSpec) (app.Hook, error)
	DeleteHook(ctx context.Context, repo string, id int64) error
	PingHook(ctx context.Context, repo string, id int64) error
}

var _ API = (*app.Client)(nil)

// Desired is what every Bonsai hook of this machine must look like.
type Desired struct {
	PublicURL string
	InstallID string
	Secret    []byte
}

func (d Desired) spec() app.HookSpec {
	return app.HookSpec{URL: HookURL(d.PublicURL, d.InstallID), Secret: string(d.Secret), Events: webhooks.LiveHookEvents}
}

// HookURL is the URL GitHub posts to: the receiver route on the public
// origin, marked with the install ID.
func HookURL(publicURL, installID string) string {
	return strings.TrimRight(publicURL, "/") + webhooks.LivePath + "?" + MarkerParam + "=" + url.QueryEscape(installID)
}

// InstallIDOf returns the install ID a Bonsai hook URL is marked with.
func InstallIDOf(hookURL string) (string, bool) {
	u, err := url.Parse(hookURL)
	if err != nil || u.Path != webhooks.LivePath {
		return "", false
	}
	id := u.Query().Get(MarkerParam)
	return id, id != ""
}

// IsOwnHook reports whether hook is this machine's Bonsai hook.
func IsOwnHook(hook app.Hook, installID string) bool {
	id, ok := InstallIDOf(hook.Config.URL)
	return ok && installID != "" && id == installID
}

// SecretFingerprint identifies a secret without revealing it.
func SecretFingerprint(secret []byte) string {
	sum := sha256.Sum256(append([]byte("bonsai-webhook-secret\x00"), secret...))
	return hex.EncodeToString(sum[:8])
}

// Reconcile brings repo's hook to want and returns its new state: needs
// admin, created, re-pointed (then pinged) or, when nothing changed, its
// health. It never deletes a hook.
func Reconcile(ctx context.Context, api API, repo string, want Desired, current config.WebLiveRepository, now time.Time) config.WebLiveRepository {
	next := current
	next.CheckedAt = now
	admin, err := api.RepoAdmin(ctx, repo)
	if err != nil {
		return failed(next, repo, err)
	}
	if !admin {
		next.State = config.WebLiveStateNeedsAdmin
		next.LastError = "you are not an admin of " + repo + ", so it stays on standard updates"
		return next
	}
	hook, found, err := Find(ctx, api, repo, current.HookID, want.InstallID)
	if err != nil {
		return failed(next, repo, err)
	}
	return apply(ctx, api, repo, want, next, hook, found, now)
}

// Check is the periodic health read: one GET of the stored hook (answered
// with a free 304 while nothing changed). A missing or drifted hook goes
// through Reconcile.
func Check(ctx context.Context, api API, repo string, want Desired, current config.WebLiveRepository, now time.Time) config.WebLiveRepository {
	if current.HookID == 0 {
		return Reconcile(ctx, api, repo, want, current, now)
	}
	hook, err := api.GetHook(ctx, repo, current.HookID)
	if domain.Code(err) == "not_found" && !app.IsHookScope(err) {
		current.HookID = 0
		return Reconcile(ctx, api, repo, want, current, now)
	}
	next := current
	next.CheckedAt = now
	if err != nil {
		return failed(next, repo, err)
	}
	return apply(ctx, api, repo, want, next, hook, true, now)
}

func apply(ctx context.Context, api API, repo string, want Desired, next config.WebLiveRepository, hook app.Hook, found bool, now time.Time) config.WebLiveRepository {
	spec := want.spec()
	fingerprint := SecretFingerprint(want.Secret)
	var err error
	pingError := ""
	switch {
	case !found:
		// GitHub pings a new hook by itself.
		if hook, err = api.CreateHook(ctx, repo, spec); err != nil {
			return failed(next, repo, err)
		}
	case drifted(hook, spec) || next.SecretFingerprint != fingerprint || next.HookID != hook.ID:
		// The secret is write-only on GitHub: a hook found without our record
		// of its secret is given it again.
		if hook, err = api.UpdateHook(ctx, repo, hook.ID, spec); err != nil {
			return failed(next, repo, err)
		}
		if err := api.PingHook(ctx, repo, hook.ID); err != nil {
			pingError = "ping: " + Message(repo, err)
		}
	default:
		next.State, next.LastError = health(hook, next, now)
		return next
	}
	next.HookID = hook.ID
	next.HookURL = spec.URL
	next.SecretFingerprint = fingerprint
	next.ConfiguredAt = now
	next.State = config.WebLiveStateWaiting
	next.LastError = pingError
	return next
}

// Ping asks GitHub to ping repo's hook again (a waiting hook whose first ping
// was lost, for example to a quick tunnel that was not reachable yet).
func Ping(ctx context.Context, api API, repo string, current config.WebLiveRepository) error {
	if current.HookID == 0 {
		return domain.E("not_found", "no hook to ping")
	}
	return api.PingHook(ctx, repo, current.HookID)
}

// Remove deletes this machine's Bonsai hook from repo: the stored ID, else
// the hook marked with installID. It reports whether a hook was deleted.
func Remove(ctx context.Context, api API, repo string, current config.WebLiveRepository, installID string) (bool, error) {
	hook, found, err := Find(ctx, api, repo, current.HookID, installID)
	if err != nil || !found {
		return false, err
	}
	if err := api.DeleteHook(ctx, repo, hook.ID); err != nil && domain.Code(err) != "not_found" {
		return false, err
	}
	return true, nil
}

// Find returns this machine's Bonsai hook on repo: the stored hook when it is
// still ours, else the hook marked with installID.
func Find(ctx context.Context, api API, repo string, id int64, installID string) (app.Hook, bool, error) {
	if id != 0 {
		hook, err := api.GetHook(ctx, repo, id)
		switch {
		case err == nil && IsOwnHook(hook, installID):
			return hook, true, nil
		case err != nil && (domain.Code(err) != "not_found" || app.IsHookScope(err)):
			return app.Hook{}, false, err
		}
	}
	hooks, err := api.ListHooks(ctx, repo)
	if err != nil {
		return app.Hook{}, false, err
	}
	for _, hook := range hooks {
		if IsOwnHook(hook, installID) {
			return hook, true, nil
		}
	}
	return app.Hook{}, false, nil
}

func drifted(hook app.Hook, spec app.HookSpec) bool {
	if !hook.Active || hook.Config.URL != spec.URL || hook.Config.ContentType != "json" || hook.Config.InsecureSSL == "1" {
		return true
	}
	have := slices.Clone(hook.Events)
	want := slices.Clone(spec.Events)
	slices.Sort(have)
	slices.Sort(want)
	return !slices.Equal(have, want)
}

// health reads an unchanged hook's state from its last delivery.
func health(hook app.Hook, current config.WebLiveRepository, now time.Time) (string, string) {
	if current.State == config.WebLiveStateWaiting && now.Sub(current.ConfiguredAt) < waitForPing {
		return config.WebLiveStateWaiting, current.LastError
	}
	code := hook.LastResponse.Code
	switch {
	case code == nil || *code == 0:
		if current.State == config.WebLiveStateLive {
			return config.WebLiveStateLive, ""
		}
		return config.WebLiveStateWaiting, ""
	case *code >= 200 && *code < 300:
		return config.WebLiveStateLive, ""
	}
	text := fmt.Sprintf("last delivery got HTTP %d", *code)
	if m := strings.TrimSpace(hook.LastResponse.Message); m != "" {
		text += ": " + m
	}
	return config.WebLiveStateFailing, text
}

// failed records a GitHub error. Missing scope and lost access change the
// state; a transient failure (network, rate limit) keeps the last known one,
// since deliveries may well still arrive.
func failed(next config.WebLiveRepository, repo string, err error) config.WebLiveRepository {
	next.LastError = Message(repo, err)
	switch {
	case app.IsHookScope(err):
		next.State = config.WebLiveStateScopeMissing
	case next.State == "", domain.Code(err) == "not_found", domain.Code(err) == "unauthorized":
		next.State = config.WebLiveStateFailing
	}
	return next
}

// Message is a GitHub error on repo as one line with its fix.
func Message(repo string, err error) string {
	switch {
	case app.IsHookScope(err):
		return "your gh login cannot manage webhooks; run: " + app.ScopeFix(GitHubHost)
	case domain.Code(err) == "not_found":
		return repo + " was not found, or your gh login cannot see it"
	case domain.Code(err) == "unauthorized":
		return "GitHub refused your gh login; run: gh auth login -h " + GitHubHost
	case domain.Code(err) == "rate_limited":
		return "GitHub rate limit reached; retrying later"
	}
	return "GitHub: " + err.Error()
}
