package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	websetupui "github.com/Tiago-0liveira/bonsai/internal/ui/websetup"
	"github.com/Tiago-0liveira/bonsai/internal/websetup/checks"
	"github.com/Tiago-0liveira/bonsai/internal/webtunnel"
)

// prepareCloudflare only runs from the confirmed setup plan. In particular,
// doctor and normal startup never create tunnels or change DNS.
func (b *setupBackend) prepareCloudflare(ctx context.Context, cfg config.WebConfig, progress func(websetupui.Step)) error {
	options := cfg.Updates.Live.TunnelOptions()
	if err := webtunnel.ValidateOptions(options); err != nil {
		return err
	}
	if options.Preset != webtunnel.CloudflaredNamed {
		return fmt.Errorf("Cloudflare provisioning needs a named tunnel")
	}
	c := checks.CheckCloudflared(ctx, b.env, checksOptions(cfg))
	if c.State != checks.OK {
		fix := "run cloudflared tunnel login"
		if c.Fix != nil {
			fix = c.Fix.Command
		}
		return fmt.Errorf("%s; %s, then retry Apply", c.Detail, fix)
	}
	run := b.cloudflaredRun
	if run == nil {
		run = runSetupCloudflared
	}
	command := func(args ...string) (string, error) {
		out, err := run(ctx, args...)
		if err != nil {
			detail := strings.TrimSpace(out)
			if detail != "" {
				return "", fmt.Errorf("cloudflared %s: %w\n%s", strings.Join(args, " "), err, detail)
			}
			return "", fmt.Errorf("cloudflared %s: %w", strings.Join(args, " "), err)
		}
		return out, nil
	}
	progress(websetupui.Step{ID: "cloudflare", Label: "Checking Cloudflare tunnel", State: websetupui.StepRunning, Detail: options.TunnelName})
	out, err := command("tunnel", "list", "--output", "json", "--name", options.TunnelName)
	if err != nil {
		return err
	}
	var tunnels []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(out), &tunnels); err != nil {
		return fmt.Errorf("read cloudflared tunnel list: %w", err)
	}
	exists := false
	for _, tunnel := range tunnels {
		exists = exists || tunnel.Name == options.TunnelName
	}
	if !exists {
		progress(websetupui.Step{ID: "cloudflare", Label: "Creating Cloudflare tunnel", State: websetupui.StepRunning, Detail: "cloudflared tunnel create " + options.TunnelName})
		if _, err := command("tunnel", "create", options.TunnelName); err != nil {
			return err
		}
	}
	public, _ := webtunnel.NormalizePublicURL(options.PublicURL)
	u, _ := url.Parse(public)
	args := []string{"tunnel", "route", "dns", options.TunnelName, u.Hostname()}
	progress(websetupui.Step{ID: "cloudflare", Label: "Routing Cloudflare hostname", State: websetupui.StepRunning, Detail: "cloudflared " + strings.Join(args, " ")})
	if _, err := command(args...); err != nil {
		return err
	}
	progress(websetupui.Step{ID: "cloudflare", Label: "Cloudflare tunnel configured", State: websetupui.StepDone, Detail: options.TunnelName + " · " + u.Hostname()})
	return nil
}

func runSetupCloudflared(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "cloudflared", args...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		return out.String() + "\n" + errOut.String(), err
	}
	return out.String(), nil
}
