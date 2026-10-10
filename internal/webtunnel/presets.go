// Package webtunnel describes the tunnels `bonsai web` can run for live
// updates: the argv for each preset, how its public URL is learned, and the
// argv validation that keeps the tunnel pointed at the webhook receiver and
// nothing else. It is pure (no processes, no network, no config files) so the
// CLI that builds the serve spec, the daemon that validates it and the API
// that learns the public URL all share one definition.
package webtunnel

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Presets. The IDs are the values of updates.live.tunnel in web.json and the
// Live choice IDs of the setup flow.
const (
	CloudflaredQuick = "cloudflared-quick"
	CloudflaredNamed = "cloudflared-named"
	Ngrok            = "ngrok"
	Tailscale        = "tailscale"
	ExternalURL      = "external-url"
	Custom           = "custom"
)

// PortPlaceholder is replaced by the webhook port in a custom command.
const PortPlaceholder = "{port}"

// SidecarName is the daemon serve-group process name of the tunnel.
const SidecarName = "tunnel"

// Options is everything a preset needs; most presets use only some fields.
type Options struct {
	Preset      string
	WebhookPort int
	// TunnelName is the named Cloudflare tunnel (cloudflared-named).
	TunnelName string
	// PublicURL is the URL GitHub posts to: required for cloudflared-named
	// and external-url, the reserved domain for ngrok (optional).
	PublicURL string
	// Command is the custom preset's argv template; it must contain {port}.
	Command []string
	// URLPattern is the custom preset's regular expression for the public
	// URL in the tunnel's output.
	URLPattern string
}

// Known reports whether preset is one of the presets above.
func Known(preset string) bool {
	switch preset {
	case CloudflaredQuick, CloudflaredNamed, Ngrok, Tailscale, ExternalURL, Custom:
		return true
	}
	return false
}

var tunnelNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ValidTunnelName reports whether name can be a named Cloudflare tunnel.
func ValidTunnelName(name string) bool { return tunnelNamePattern.MatchString(name) }

// ValidateOptions rejects options a preset cannot run with. It does not check
// that the tunnel program is installed.
func ValidateOptions(o Options) error {
	if !Known(o.Preset) {
		return fmt.Errorf("unknown tunnel %q (expected %s, %s, %s, %s, %s or %s)", o.Preset,
			CloudflaredQuick, CloudflaredNamed, Ngrok, Tailscale, ExternalURL, Custom)
	}
	if o.WebhookPort < 1 || o.WebhookPort > 65535 {
		return fmt.Errorf("webhook port %d is not a valid port", o.WebhookPort)
	}
	switch o.Preset {
	case CloudflaredNamed:
		if !tunnelNamePattern.MatchString(o.TunnelName) {
			return fmt.Errorf("tunnel %s needs tunnel_name (letters, digits, '.', '_' or '-')", o.Preset)
		}
		if _, err := NormalizePublicURL(o.PublicURL); err != nil {
			return fmt.Errorf("tunnel %s needs public_url: %w", o.Preset, err)
		}
	case ExternalURL:
		if _, err := NormalizePublicURL(o.PublicURL); err != nil {
			return fmt.Errorf("tunnel %s needs public_url: %w", o.Preset, err)
		}
	case Ngrok:
		if o.PublicURL != "" {
			if _, err := NormalizePublicURL(o.PublicURL); err != nil {
				return fmt.Errorf("ngrok public_url: %w", err)
			}
		}
	case Custom:
		if len(o.Command) == 0 || strings.TrimSpace(o.Command[0]) == "" {
			return fmt.Errorf("tunnel %s needs a command", o.Preset)
		}
		found := false
		for _, arg := range o.Command {
			found = found || strings.Contains(arg, PortPlaceholder)
		}
		if !found {
			return fmt.Errorf("custom tunnel command must contain %s (the webhook port)", PortPlaceholder)
		}
		if strings.TrimSpace(o.URLPattern) == "" {
			return fmt.Errorf("custom tunnel needs url_pattern (a regular expression for its public URL)")
		}
		if _, err := regexp.Compile(o.URLPattern); err != nil {
			return fmt.Errorf("custom tunnel url_pattern: %w", err)
		}
	}
	return nil
}

// Argv is the tunnel command for o, nil when the preset runs no tunnel
// (external-url: the user runs their own proxy).
func Argv(o Options) ([]string, error) {
	if err := ValidateOptions(o); err != nil {
		return nil, err
	}
	port := strconv.Itoa(o.WebhookPort)
	target := "http://127.0.0.1:" + port
	switch o.Preset {
	case CloudflaredQuick:
		return []string{"cloudflared", "tunnel", "--no-autoupdate", "--url", target}, nil
	case CloudflaredNamed:
		// --url names the webhook port as the origin. cloudflared applies it
		// only when the user's config has no ingress rules; those are theirs,
		// and the API refuses any tunnelled Host either way.
		return []string{"cloudflared", "tunnel", "--no-autoupdate", "run", "--url", target, o.TunnelName}, nil
	case Ngrok:
		argv := []string{"ngrok", "http", "127.0.0.1:" + port, "--log", "stdout"}
		if o.PublicURL != "" {
			public, _ := NormalizePublicURL(o.PublicURL)
			argv = append(argv, "--url", public)
		}
		return argv, nil
	case Tailscale:
		// Foreground: stopping the process turns the funnel off.
		return []string{"tailscale", "funnel", port}, nil
	case Custom:
		argv := make([]string, len(o.Command))
		for i, arg := range o.Command {
			argv[i] = strings.ReplaceAll(arg, PortPlaceholder, port)
		}
		return argv, nil
	}
	return nil, nil
}

// NormalizePublicURL accepts an HTTPS origin (a bare host is read as
// https://host) and returns it without a trailing slash. GitHub posts to it,
// so anything else (plain HTTP, credentials, query, fragment, a path) is
// rejected.
func NormalizePublicURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty URL")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid URL %q", raw)
	}
	if u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") {
		return "", fmt.Errorf("%q must be an https:// origin with no path, query or credentials", raw)
	}
	return "https://" + strings.ToLower(u.Host), nil
}
