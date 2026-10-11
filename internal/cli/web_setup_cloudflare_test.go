package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	websetupui "github.com/Tiago-0liveira/bonsai/internal/ui/websetup"
	setup "github.com/Tiago-0liveira/bonsai/internal/websetup"
	"github.com/Tiago-0liveira/bonsai/internal/webtunnel"
)

func cloudflareSetup(t *testing.T) (*setupBackend, config.WebConfig) {
	t.Helper()
	b, _ := offlineSetup(t)
	b.env.LookPath = func(name string) (string, error) { return name, nil }
	b.env.Run = func(context.Context, string, ...string) (string, string, int, error) {
		return "cloudflared version 2026.9.3", "", 0, nil
	}
	b.env.Getenv = func(string) string { return "" }
	b.env.HomeDir = t.TempDir()
	b.env.Exists = func(string) bool { return true }
	cfg := config.DefaultWebConfig()
	cfg.Updates.Mode = config.WebUpdatesLive
	cfg.Updates.Live.Tunnel = webtunnel.CloudflaredNamed
	cfg.Updates.Live.TunnelName = "bonsai-wsl"
	cfg.Updates.Live.PublicURL = "https://bonsai-local-app.example.com"
	return b, cfg
}

func TestCloudflareSetupCreatesRoutesAndReuses(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "create", true: "reuse"}[existing], func(t *testing.T) {
			b, cfg := cloudflareSetup(t)
			var commands []string
			b.cloudflaredRun = func(_ context.Context, args ...string) (string, error) {
				commands = append(commands, strings.Join(args, " "))
				if args[1] == "list" {
					if existing {
						return `[{"name":"bonsai-wsl"}]`, nil
					}
					return `[{"name":"some-other-tunnel"}]`, nil
				}
				return "ok", nil
			}
			if err := b.prepareCloudflare(context.Background(), cfg, func(websetupui.Step) {}); err != nil {
				t.Fatal(err)
			}
			want := "tunnel list --output json --name bonsai-wsl|tunnel create bonsai-wsl|tunnel route dns bonsai-wsl bonsai-local-app.example.com"
			if existing {
				want = strings.Replace(want, "|tunnel create bonsai-wsl", "", 1)
			}
			if got := strings.Join(commands, "|"); got != want {
				t.Fatalf("commands = %s, want %s", got, want)
			}
		})
	}
}

func TestCloudflareSetupFailuresStopApplyAndShowOutput(t *testing.T) {
	for _, failedCommand := range []string{"list", "create", "route"} {
		t.Run(failedCommand, func(t *testing.T) {
			b, cfg := cloudflareSetup(t)
			var commands []string
			b.cloudflaredRun = func(_ context.Context, args ...string) (string, error) {
				commands = append(commands, args[1])
				if args[1] == failedCommand {
					return "Cloudflare: DNS record already exists", errors.New("exit status 1")
				}
				return "[]", nil
			}
			var steps []websetupui.Step
			result := b.Apply(context.Background(), setup.Plan{Config: cfg, PrepareCloudflare: true}, func(s websetupui.Step) { steps = append(steps, s) })
			if result.OK || b.wasSaved() {
				t.Fatal("failed command must stop Apply before settings are saved")
			}
			if _, err := os.Stat(b.w.configPath); !os.IsNotExist(err) {
				t.Fatalf("settings were written: %v", err)
			}
			last := steps[len(steps)-1]
			if last.State != websetupui.StepFailed || !strings.Contains(last.Detail, "DNS record already exists") || !strings.Contains(last.Detail, "exit status 1") || !strings.Contains(last.Detail, "cloudflared tunnel "+failedCommand) {
				t.Fatalf("failure = %+v", last)
			}
			if commands[len(commands)-1] != failedCommand {
				t.Fatalf("continued after failure: %v", commands)
			}
		})
	}
}

func TestCloudflareSetupRequiresLoginAndRejectsInvalidList(t *testing.T) {
	b, cfg := cloudflareSetup(t)
	b.env.Exists = func(string) bool { return false }
	calls := 0
	b.cloudflaredRun = func(context.Context, ...string) (string, error) {
		calls++
		return "not JSON", nil
	}
	if err := b.prepareCloudflare(context.Background(), cfg, func(websetupui.Step) {}); err == nil || !strings.Contains(err.Error(), "cloudflared tunnel login") || calls != 0 {
		t.Fatalf("err = %v, commands = %d", err, calls)
	}
	b.env.Exists = func(string) bool { return true }
	if err := b.prepareCloudflare(context.Background(), cfg, func(websetupui.Step) {}); err == nil || !strings.Contains(err.Error(), "read cloudflared tunnel list") || calls != 1 {
		t.Fatalf("err = %v, commands = %d", err, calls)
	}
}

func TestCloudflareCommandKeepsJSONSeparateAndIncludesFailureOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell fixture")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf 'cloudflare diagnostic\\n' >&2\nif [ \"$1\" = fail ]; then\n  printf 'command failed\\n'\n  exit 7\nfi\nprintf '[]\\n'\n"
	if err := os.WriteFile(filepath.Join(dir, "cloudflared"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := runSetupCloudflared(context.Background(), "list")
	if err != nil || strings.TrimSpace(out) != "[]" {
		t.Fatalf("JSON polluted by stderr: %q, %v", out, err)
	}
	out, err = runSetupCloudflared(context.Background(), "fail")
	if err == nil || !strings.Contains(err.Error(), "exit status 7") || !strings.Contains(out, "command failed") || !strings.Contains(out, "cloudflare diagnostic") {
		t.Fatalf("failure lost output: %q, %v", out, err)
	}
}
