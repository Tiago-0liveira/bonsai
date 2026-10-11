package webtunnel

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	quickTunnelURL = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)
	ngrokLogURL    = regexp.MustCompile(`url=(https://[^\s"]+)`)
)

// LearnedFromLog reports whether the preset's public URL appears in the
// tunnel's own output (and so must be read from its log).
func LearnedFromLog(preset string) bool {
	return preset == CloudflaredQuick || preset == Custom || preset == Ngrok
}

// URLFromLog returns the last public URL the tunnel printed. A restarted quick
// tunnel prints a new URL, so the last match wins. pattern is the custom
// preset's url_pattern (group 1 when it has one, else the whole match).
func URLFromLog(preset, pattern, text string) (string, bool) {
	var found string
	switch preset {
	case CloudflaredQuick:
		if all := quickTunnelURL.FindAllString(text, -1); len(all) > 0 {
			found = all[len(all)-1]
		}
	case Ngrok:
		if all := ngrokLogURL.FindAllStringSubmatch(text, -1); len(all) > 0 {
			found = all[len(all)-1][1]
		}
	case Custom:
		re, err := regexp.Compile(pattern)
		if err != nil {
			return "", false
		}
		if all := re.FindAllStringSubmatch(text, -1); len(all) > 0 {
			last := all[len(all)-1]
			found = last[0]
			if len(last) > 1 && last[1] != "" {
				found = last[1]
			}
		}
	default:
		return "", false
	}
	if found == "" {
		return "", false
	}
	normalized, err := NormalizePublicURL(found)
	if err != nil {
		return "", false
	}
	return normalized, true
}

// ConfiguredURL is the public URL of presets whose URL the user configured
// (cloudflared-named, external-url, ngrok with a reserved domain).
func ConfiguredURL(o Options) (string, bool) {
	switch o.Preset {
	case CloudflaredNamed, ExternalURL, Ngrok:
		if o.PublicURL == "" {
			return "", false
		}
		u, err := NormalizePublicURL(o.PublicURL)
		return u, err == nil
	}
	return "", false
}

// ParseNgrokTunnels reads the ngrok agent API (GET 127.0.0.1:4040/api/tunnels)
// and returns the HTTPS public URL of the tunnel forwarding to port.
func ParseNgrokTunnels(raw []byte, port int) (string, error) {
	var body struct {
		Tunnels []struct {
			PublicURL string `json:"public_url"`
			Proto     string `json:"proto"`
			Config    struct {
				Addr string `json:"addr"`
			} `json:"config"`
		} `json:"tunnels"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return "", fmt.Errorf("read ngrok tunnels: %w", err)
	}
	suffix := ":" + strconv.Itoa(port)
	for _, t := range body.Tunnels {
		addr := strings.TrimSuffix(t.Config.Addr, "/")
		if !strings.HasSuffix(addr, suffix) || !strings.HasPrefix(t.PublicURL, "https://") {
			continue
		}
		return NormalizePublicURL(t.PublicURL)
	}
	return "", fmt.Errorf("ngrok has no https tunnel to port %d", port)
}

// ParseTailscaleStatus reads `tailscale status --json` and returns the funnel
// URL of this machine: https://<Self.DNSName without the trailing dot>.
func ParseTailscaleStatus(raw []byte) (string, error) {
	var body struct {
		Self struct {
			DNSName string `json:"DNSName"`
		} `json:"Self"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return "", fmt.Errorf("read tailscale status: %w", err)
	}
	name := strings.TrimSuffix(strings.TrimSpace(body.Self.DNSName), ".")
	if name == "" {
		return "", fmt.Errorf("tailscale status has no DNS name for this machine (is MagicDNS on?)")
	}
	return NormalizePublicURL(name)
}
