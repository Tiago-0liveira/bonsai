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
	"github.com/Tiago-0liveira/bonsai/internal/server/localapi"
)

func hostedWebOrigin() string {
	if value := strings.TrimSpace(os.Getenv("BONSAI_FRONTEND_ORIGIN")); value != "" {
		return strings.TrimRight(value, "/")
	}
	return localapi.ProductionBrowserOrigin
}

func hostedWebURL() string {
	return hostedWebOrigin() + "/app"
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
		group, err := serveGroupForMode(c, workspaceID, procstore.ServeModeProduction)
		if err != nil {
			return err
		}
		return printServeStatus(out, group)

	case "attach":
		if len(args) != 0 {
			return fmt.Errorf("usage: bonsai serve attach")
		}
		group, err := requireServeGroupMode(c, workspaceID, procstore.ServeModeProduction)
		if err != nil {
			return err
		}
		return runServeTUI(in, out, c, group)

	case "logs":
		return cmdServeLogsForMode(c, workspaceID, procstore.ServeModeProduction, "bonsai serve", args, out, errOut)

	case "stop":
		if len(args) != 0 {
			return fmt.Errorf("usage: bonsai serve stop")
		}
		if _, err := requireServeGroupMode(c, workspaceID, procstore.ServeModeProduction); err != nil {
			return err
		}
		if err := c.ServeStop(workspaceID); err != nil {
			return err
		}
		fmt.Fprintln(out, "local API stopped")
		return nil

	case "restart":
		if len(args) > 1 || (len(args) == 1 && args[0] != "api") {
			return fmt.Errorf("usage: bonsai serve restart [api]")
		}
		if _, err := requireServeGroupMode(c, workspaceID, procstore.ServeModeProduction); err != nil {
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
		return printServeStatus(out, group)
	}

	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(errOut)
	var detached bool
	fs.BoolVar(&detached, "d", false, "wait for readiness, then detach")
	fs.BoolVar(&detached, "auto-detach", false, "wait for readiness, then detach")
	apiPort := fs.Int("api-port", 0, "override local API port")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: bonsai serve [-d|--auto-detach] [--api-port PORT]")
	}

	cfg, err := config.LoadFor(repoDir)
	if err != nil {
		return err
	}
	for _, key := range cfg.DeprecatedServeKeys {
		fmt.Fprintf(errOut, "warning: %s is deprecated and ignored by normal bonsai serve; use the internal development stack instead\n", key)
	}
	if *apiPort == 0 {
		*apiPort = cfg.Serve.APIPort
	}
	if group, err := c.ServeStatus(workspaceID); err == nil && group != nil && normalizedGroupMode(group) == procstore.ServeModeDevelopment {
		return fmt.Errorf("development stack is active for this workspace; stop it with bonsai __serve-dev-stack stop")
	}

	executable, err := os.Executable()
	if err != nil {
		return err
	}
	spec := procstore.ServeSpec{
		Mode:                   procstore.ServeModeProduction,
		WorkspaceID:            workspaceID,
		WorkspacePath:          workspace,
		Executable:             executable,
		APIPort:                *apiPort,
		BrowserOrigin:          hostedWebOrigin(),
		StartupTimeoutSeconds:  cfg.Serve.StartupTimeout,
		ShutdownTimeoutSeconds: cfg.Serve.ShutdownTimeout,
	}
	group, err := c.ServeStart(spec)
	if err != nil {
		return err
	}
	if detached {
		if group.Reused {
			fmt.Fprintln(out, "local API already healthy")
		} else {
			fmt.Fprintln(out, "local API ready; detached")
		}
		return printServeStatus(out, group)
	}
	if err := printServeAccess(out, group); err != nil {
		return err
	}
	return runServeTUI(in, out, c, group)
}

func normalizedGroupMode(group *procstore.ServeGroup) procstore.ServeMode {
	if group == nil || group.Mode == "" {
		return procstore.ServeModeProduction
	}
	return group.Mode
}

func serveGroupForMode(c *client.Client, workspaceID string, mode procstore.ServeMode) (*procstore.ServeGroup, error) {
	group, err := c.ServeStatus(workspaceID)
	if err != nil || group == nil {
		return group, err
	}
	if normalizedGroupMode(group) != mode {
		return nil, nil
	}
	return group, nil
}

func requireServeGroupMode(c *client.Client, workspaceID string, mode procstore.ServeMode) (*procstore.ServeGroup, error) {
	group, err := c.ServeStatus(workspaceID)
	if err != nil {
		return nil, err
	}
	if group == nil {
		return nil, fmt.Errorf("no serve group for this workspace")
	}
	if normalizedGroupMode(group) != mode {
		if mode == procstore.ServeModeProduction {
			return nil, fmt.Errorf("development stack is active; use bonsai __serve-dev-stack")
		}
		return nil, fmt.Errorf("production local API is active; use bonsai serve")
	}
	return group, nil
}

func cmdServeLogsForMode(c *client.Client, workspaceID string, mode procstore.ServeMode, command string, args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet(command+" logs", flag.ContinueOnError)
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
		return fmt.Errorf("usage: %s logs [-f] [-n N] [--process NAME] [--grep TEXT] [-i]", command)
	}
	if _, err := requireServeGroupMode(c, workspaceID, mode); err != nil {
		return err
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

func serveWorkspaceID(path string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(path)))
	return hex.EncodeToString(sum[:8])
}

func printServeAccess(out io.Writer, group *procstore.ServeGroup) error {
	if group == nil {
		return nil
	}
	if normalizedGroupMode(group) == procstore.ServeModeDevelopment {
		return printDevServeStatus(out, group)
	}
	fmt.Fprintf(out, "Bonsai local API\n  http://127.0.0.1:%d\n\n", group.APIPort)
	fmt.Fprintf(out, "Web client\n  %s\n", hostedWebURL())
	return nil
}

func printServeStatus(out io.Writer, group *procstore.ServeGroup) error {
	if group == nil {
		fmt.Fprintln(out, "no local API for this workspace")
		return nil
	}
	if normalizedGroupMode(group) == procstore.ServeModeDevelopment {
		return printDevServeStatus(out, group)
	}
	fmt.Fprintf(out, "Bonsai local API — %s — %s\n", group.WorkspacePath, group.State)
	fmt.Fprintf(out, "Web client: %s\n", hostedWebURL())
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
