package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/client"
	git "github.com/Tiago-0liveira/bonsai/internal/git/local"
)

type stringListFlag []string

func (v *stringListFlag) String() string { return strings.Join(*v, ",") }

func (v *stringListFlag) Set(value string) error {
	*v = append(*v, value)
	return nil
}

func cmdServe(repoDir string, args []string, in io.Reader, out, errOut io.Writer) error {
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
	workspace, err = filepath.Abs(workspace)
	if err != nil {
		return err
	}
	workspaceID := serveWorkspaceID(workspace)
	c := client.For(repoDir)

	switch action {
	case "status":
		if len(args) != 0 {
			return fmt.Errorf("usage: bonsai serve status")
		}
		group, err := c.ServeStatus(workspaceID)
		if err != nil {
			return err
		}
		return printServeStatus(out, group)

	case "attach":
		if len(args) != 0 {
			return fmt.Errorf("usage: bonsai serve attach")
		}
		group, err := c.ServeStatus(workspaceID)
		if err != nil {
			return err
		}
		if group == nil {
			return fmt.Errorf("no serve group for this workspace")
		}
		return runServeTUI(in, out, c, group)

	case "logs":
		return cmdServeLogs(c, workspaceID, args, out, errOut)

	case "stop":
		if len(args) != 0 {
			return fmt.Errorf("usage: bonsai serve stop")
		}
		if err := c.ServeStop(workspaceID); err != nil {
			return err
		}
		fmt.Fprintln(out, "serve group stopped")
		return nil

	case "restart":
		if len(args) > 1 {
			return fmt.Errorf("usage: bonsai serve restart [process]")
		}
		name := ""
		if len(args) == 1 {
			name = args[0]
		}
		group, err := c.ServeRestart(workspaceID, name)
		if err != nil {
			return err
		}
		return printServeStatus(out, group)
	}

	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(errOut)
	var detached bool
	fs.BoolVar(&detached, "d", false, "wait for readiness, then detach")
	fs.BoolVar(&detached, "auto-detach", false, "wait for readiness, then detach")
	apiPort := fs.Int("api-port", 0, "override API port")
	webhookPort := fs.Int("webhook-port", 0, "override webhook port")
	webPort := fs.Int("web-port", 0, "override web port")
	serverConfig := fs.String("server-config", "", "Git API server config JSON")
	var scripts stringListFlag
	fs.Var(&scripts, "sidecar-script", "sidecar script (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: bonsai serve [-d|--auto-detach] [--sidecar-script PATH]")
	}

	cfg, err := config.LoadFor(repoDir)
	if err != nil {
		return err
	}
	if *apiPort == 0 {
		*apiPort = cfg.Serve.APIPort
	}
	if *webhookPort == 0 {
		*webhookPort = cfg.Serve.WebhookPort
	}
	if *webPort == 0 {
		*webPort = cfg.Serve.WebPort
	}
	if *serverConfig == "" {
		*serverConfig = cfg.Serve.ServerConfig
	}
	if *serverConfig == "" {
		*serverConfig = os.Getenv("BONSAI_SERVER_CONFIG")
	}
	if *serverConfig == "" {
		candidate := filepath.Join(workspace, "server.json")
		if _, statErr := os.Stat(candidate); statErr == nil {
			*serverConfig = candidate
		}
	}
	if *serverConfig == "" {
		return fmt.Errorf("serve requires serve.server_config, --server-config, BONSAI_SERVER_CONFIG, or %s", filepath.Join(workspace, "server.json"))
	}
	if !filepath.IsAbs(*serverConfig) {
		*serverConfig = filepath.Join(workspace, *serverConfig)
	}
	*serverConfig, err = filepath.Abs(*serverConfig)
	if err != nil {
		return err
	}

	executable, err := os.Executable()
	if err != nil {
		return err
	}
	sidecars := make([]procstore.ServeSidecar, 0, len(cfg.Serve.Sidecars)+len(scripts))
	for _, sc := range cfg.Serve.Sidecars {
		sidecars = append(sidecars, procstore.ServeSidecar{
			Name:        sc.Name,
			Command:     append([]string(nil), sc.Command...),
			Cwd:         sc.Cwd,
			Environment: cloneServeEnv(sc.Environment),
			Restart:     sc.Restart,
			MaxRestarts: sc.MaxRestarts,
			Required:    sc.Required,
		})
	}
	for i, script := range scripts {
		if !filepath.IsAbs(script) {
			script = filepath.Join(workspace, script)
		}
		script, err = filepath.Abs(script)
		if err != nil {
			return err
		}
		sidecars = append(sidecars, procstore.ServeSidecar{
			Name:     "script-" + strconv.Itoa(i+1),
			Command:  []string{script},
			Cwd:      workspace,
			Restart:  procstore.PolicyOnFailure,
			Required: true,
		})
	}

	spec := procstore.ServeSpec{
		WorkspaceID:           workspaceID,
		WorkspacePath:         workspace,
		Executable:            executable,
		ServerConfig:          *serverConfig,
		APIPort:               *apiPort,
		WebhookPort:           *webhookPort,
		WebPort:               *webPort,
		StartupTimeoutSeconds: cfg.Serve.StartupTimeout,
		Sidecars:              sidecars,
	}
	group, err := c.ServeStart(spec)
	if err != nil {
		return err
	}
	if detached {
		if group.Reused {
			fmt.Fprintln(out, "serve group already healthy")
		} else {
			fmt.Fprintln(out, "serve group ready; detached")
		}
		return printServeStatus(out, group)
	}
	return runServeTUI(in, out, c, group)
}

func cmdServeLogs(c *client.Client, workspaceID string, args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("serve logs", flag.ContinueOnError)
	fs.SetOutput(errOut)
	follow := fs.Bool("f", false, "follow combined logs")
	n := fs.Int("n", 200, "number of recent lines")
	process := fs.String("process", "", "restrict to a process name")
	grep := fs.String("grep", "", "show lines containing text")
	insensitive := fs.Bool("i", false, "case-insensitive search")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: bonsai serve logs [-f] [-n N] [--process NAME] [--grep TEXT] [-i]")
	}
	group, err := c.ServeStatus(workspaceID)
	if err != nil {
		return err
	}
	if group == nil {
		return fmt.Errorf("no serve group for this workspace")
	}
	ctx := context.Background()
	stop := func() {}
	if *follow {
		ctx, stop = signal.NotifyContext(context.Background(), os.Interrupt)
	}
	defer stop()
	return c.ServeLogs(ctx, workspaceID, *process, *follow, *n, *grep, *insensitive, func(chunk string) error {
		_, err := io.WriteString(out, chunk)
		return err
	})
}

func cloneServeEnv(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func serveWorkspaceID(path string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(path)))
	return hex.EncodeToString(sum[:8])
}

func printServeStatus(out io.Writer, group *procstore.ServeGroup) error {
	if group == nil {
		fmt.Fprintln(out, "no serve group for this workspace")
		return nil
	}
	fmt.Fprintf(out, "Bonsai Serve — %s — %s\n", group.WorkspacePath, group.State)
	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "PROCESS\tSTATUS\tPID\tADDRESS")
	for _, process := range group.Processes {
		address := ""
		if process.ExpectedPort > 0 {
			address = "127.0.0.1:" + strconv.Itoa(process.ExpectedPort)
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n", process.Name, process.State, process.PID, address)
	}
	return tw.Flush()
}
