package localapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/client"
	"github.com/Tiago-0liveira/bonsai/internal/webtunnel"
)

// publicURLSource returns the tunnel's current public origin, or why there
// is none (tunnel not running, URL not printed yet, ...).
type publicURLSource func(ctx context.Context, options webtunnel.Options) (string, error)

// tunnelLogLines is how much of the tunnel's log is searched for its URL; a
// quick tunnel prints it within its first dozen lines.
const tunnelLogLines = 400

// daemonPublicURL learns the public URL of the tunnel that the bonsai web
// daemon (home) supervises: from the preset's configuration, its log, the
// ngrok agent API or `tailscale status`.
func daemonPublicURL(home string) publicURLSource {
	daemon := client.ForUserHome(home)
	return func(ctx context.Context, options webtunnel.Options) (string, error) {
		if options.Preset == webtunnel.ExternalURL {
			// The user's own proxy: nothing for Bonsai to watch.
			if u, ok := webtunnel.ConfiguredURL(options); ok {
				return u, nil
			}
			return "", errors.New("updates.live.public_url is not set")
		}
		if err := tunnelRunning(daemon); err != nil {
			return "", err
		}
		if u, ok := webtunnel.ConfiguredURL(options); ok {
			return u, nil
		}
		switch {
		case options.Preset == webtunnel.Tailscale:
			return tailscaleURL(ctx)
		case webtunnel.LearnedFromLog(options.Preset):
			text, err := tunnelLog(ctx, daemon)
			if err != nil {
				return "", err
			}
			if u, ok := webtunnel.URLFromLog(options.Preset, options.URLPattern, text); ok {
				return u, nil
			}
			if options.Preset == webtunnel.Ngrok {
				return ngrokURL(ctx, options.WebhookPort)
			}
			return "", errors.New("the tunnel has not printed its public URL yet")
		}
		return "", fmt.Errorf("tunnel %s has no public URL", options.Preset)
	}
}

func tunnelRunning(daemon *client.Client) error {
	if !daemon.Running() {
		return errors.New("the bonsai web daemon is not running")
	}
	group, err := daemon.ServeStatus(procstore.WebServeGroupID)
	if err != nil {
		return fmt.Errorf("read the tunnel's state: %w", err)
	}
	for _, p := range group.Processes {
		if p.Name != webtunnel.SidecarName {
			continue
		}
		if p.State == "running" || p.State == "ready" {
			return nil
		}
		if p.ExitError != "" {
			return fmt.Errorf("the tunnel is %s (%s); see bonsai web logs tunnel", p.State, p.ExitError)
		}
		return fmt.Errorf("the tunnel is %s; see bonsai web logs tunnel", p.State)
	}
	return errors.New("the tunnel is not running (is its program installed? run bonsai web again)")
}

func tunnelLog(ctx context.Context, daemon *client.Client) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var b strings.Builder
	err := daemon.ServeLogs(ctx, procstore.WebServeGroupID, webtunnel.SidecarName, false, tunnelLogLines, "", false, func(chunk string) error {
		b.WriteString(chunk)
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("read the tunnel log: %w", err)
	}
	return b.String(), nil
}

func ngrokURL(ctx context.Context, port int) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:4040/api/tunnels", nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", errors.New("ngrok has not reported its public URL yet")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	return webtunnel.ParseNgrokTunnels(raw, port)
}

func tailscaleURL(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tailscale", "status", "--json").Output()
	if err != nil {
		return "", fmt.Errorf("tailscale status: %w", err)
	}
	return webtunnel.ParseTailscaleStatus(out)
}
