package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"text/tabwriter"
	"path/filepath"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/client"
	git "github.com/Tiago-0liveira/bonsai/internal/git/local"
)

const (
	devDefaultAPIPort     = 7001
	devDefaultWebhookPort = 7002
	devDefaultWebPort     = 7003
)

func cmdServeDevStack(repoDir string, args []string, in io.Reader, out, errOut io.Writer) error {
	action := "start"
	if len(args) > 0 {
		switch args[0] {
		case "status", "attach", "logs", "restart", "stop":
			action, args = args[0], args[1:]
		}
	}
	workspace, err := git.RepoRoot(".")
	if err != nil {
		return err
	}
	workspace, err = filepathAbs(workspace)
	if err != nil {
		return err
	}
	workspaceID := serveWorkspaceID(workspace)
	c := client.For(repoDir)

	switch action {
	case "status":
		if len(args) != 0 {
			return fmt.Errorf("usage: bonsai __serve-dev-stack status")
		}
		group, err := serveGroupForMode(c, workspaceID, procstore.ServeModeDevelopment)
		if err != nil {
			return err
		}
		return printDevServeStatus(out, group)
	case "attach":
		if len(args) != 0 {
			return fmt.Errorf("usage: bonsai __serve-dev-stack attach")
		}
		group, err := requireServeGroupMode(c, workspaceID, procstore.ServeModeDevelopment)
		if err != nil {
			return err
		}
		return runServeTUI(in, out, c, group)
	case "logs":
		return cmdServeLogsForMode(c, workspaceID, procstore.ServeModeDevelopment, "bonsai __serve-dev-stack", args, out, errOut)
	case "stop":
		if len(args) != 0 {
			return fmt.Errorf("usage: bonsai __serve-dev-stack stop")
		}
		if _, err := requireServeGroupMode(c, workspaceID, procstore.ServeModeDevelopment); err != nil {
			return err
		}
		if err := c.ServeStop(workspaceID); err != nil {
			return err
		}
		fmt.Fprintln(out, "development stack stopped")
		return nil
	case "restart":
		if len(args) > 1 {
			return fmt.Errorf("usage: bonsai __serve-dev-stack restart [process]")
		}
		if _, err := requireServeGroupMode(c, workspaceID, procstore.ServeModeDevelopment); err != nil {
			return err
		}
		name := ""
		if len(args) == 1 {
			name = args[0]
		}
		group, err := c.ServeRestart(workspaceID, name)
		if err != nil {
			return err
		}
		return printDevServeStatus(out, group)
	}

	fs := flag.NewFlagSet("__serve-dev-stack", flag.ContinueOnError)
	fs.SetOutput(errOut)
	var detached bool
	fs.BoolVar(&detached, "d", false, "wait for readiness, then detach")
	fs.BoolVar(&detached, "auto-detach", false, "wait for readiness, then detach")
	apiFlag := fs.Int("api-port", 0, "development API port")
	webhookFlag := fs.Int("webhook-port", 0, "development webhook port")
	webFlag := fs.Int("web-port", 0, "development Vite port")
	tunnel := fs.String("tunnel", "", "optional development webhook tunnel: cloudflared or ngrok")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: bonsai __serve-dev-stack [-d] [--api-port PORT] [--webhook-port PORT] [--web-port PORT] [--tunnel cloudflared|ngrok]")
	}

	apiPort, err := resolveDevPort(*apiFlag, "BONSAI_DEV_API_PORT", devDefaultAPIPort)
	if err != nil {
		return err
	}
	webhookPort, err := resolveDevPort(*webhookFlag, "BONSAI_DEV_WEBHOOK_PORT", devDefaultWebhookPort)
	if err != nil {
		return err
	}
	webPort, err := resolveDevPort(*webFlag, "BONSAI_DEV_WEB_PORT", devDefaultWebPort)
	if err != nil {
		return err
	}
	cfg, err := config.LoadFor(repoDir)
	if err != nil {
		return err
	}
	if group, err := c.ServeStatus(workspaceID); err == nil && group != nil && normalizedGroupMode(group) == procstore.ServeModeProduction {
		return fmt.Errorf("production local API is active for this workspace; stop it with bonsai serve stop")
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	spec := procstore.ServeSpec{
		Mode:                   procstore.ServeModeDevelopment,
		WorkspaceID:            workspaceID,
		WorkspacePath:          workspace,
		Executable:             executable,
		APIPort:                apiPort,
		WebhookPort:            webhookPort,
		WebPort:                webPort,
		BrowserOrigin:          fmt.Sprintf("http://127.0.0.1:%d", webPort),
		StartupTimeoutSeconds:  cfg.Serve.StartupTimeout,
		ShutdownTimeoutSeconds: cfg.Serve.ShutdownTimeout,
	}
	if *tunnel != "" {
		sidecar, err := devTunnelSidecar(*tunnel, webhookPort)
		if err != nil {
			return err
		}
		spec.Sidecars = append(spec.Sidecars, sidecar)
		fmt.Fprintln(errOut, "WARNING: development tunnel enabled")
		fmt.Fprintf(errOut, "Only webhook port %d is being exposed.\n", webhookPort)
		fmt.Fprintln(errOut, "The Bonsai API remains loopback-only.")
	}
	group, err := c.ServeStart(spec)
	if err != nil {
		return err
	}
	if detached {
		if group.Reused {
			fmt.Fprintln(out, "development stack already healthy")
		} else {
			fmt.Fprintln(out, "development stack ready; detached")
		}
		return printDevServeStatus(out, group)
	}
	if err := printDevServeStatus(out, group); err != nil {
		return err
	}
	return runServeTUI(in, out, c, group)
}

func filepathAbs(path string) (string, error) {
	return filepath.Abs(path)
}

func resolveDevPort(flagValue int, envName string, fallback int) (int, error) {
	if flagValue != 0 {
		if flagValue < 1 || flagValue > 65535 {
			return 0, fmt.Errorf("%s port %d is invalid", envName, flagValue)
		}
		return flagValue, nil
	}
	if raw := os.Getenv(envName); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 65535 {
			return 0, fmt.Errorf("%s must be a valid TCP port", envName)
		}
		return value, nil
	}
	return fallback, nil
}

func devTunnelSidecar(kind string, webhookPort int) (procstore.ServeSidecar, error) {
	target := fmt.Sprintf("http://127.0.0.1:%d", webhookPort)
	switch kind {
	case "cloudflared":
		return procstore.ServeSidecar{
			Name: "tunnel", Command: []string{"cloudflared", "tunnel", "--url", target},
			Restart: procstore.PolicyOnFailure, MaxRestarts: 3,
		}, nil
	case "ngrok":
		return procstore.ServeSidecar{
			Name: "tunnel", Command: []string{"ngrok", "http", target},
			Restart: procstore.PolicyOnFailure, MaxRestarts: 3,
		}, nil
	default:
		return procstore.ServeSidecar{}, fmt.Errorf("unsupported development tunnel %q (use cloudflared or ngrok)", kind)
	}
}

func printDevServeStatus(out io.Writer, group *procstore.ServeGroup) error {
	if group == nil {
		fmt.Fprintln(out, "no development stack for this workspace")
		return nil
	}
	fmt.Fprintf(out, "Bonsai development stack — %s — %s\n", group.WorkspacePath, group.State)
	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "PROCESS\tSTATUS\tPID\tADDRESS")
	for _, process := range group.Processes {
		address := ""
		if process.ExpectedPort > 0 {
			address = "http://127.0.0.1:" + strconv.Itoa(process.ExpectedPort)
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n", process.Name, process.State, process.PID, address)
	}
	return tw.Flush()
}
