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
	"strings"

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

// cmdServe is the deprecated `bonsai serve`. It used to start a local API per
// repository; it now drives the single user-level `bonsai web` stack, which
// serves every configured project and reaches the same healthy end state.
//
//	bonsai serve                 → bonsai web --attach --no-open
//	bonsai serve -d              → bonsai web --no-open
//	bonsai serve --api-port N    → … --port N
//	bonsai serve status|attach|logs|restart|stop → bonsai web <same>
func cmdServe(args []string, in io.Reader, out, errOut io.Writer) error {
	fmt.Fprintln(errOut, "bonsai serve is deprecated: use bonsai web (one local API for all your projects).")
	if len(args) > 0 {
		switch args[0] {
		case "stop":
			// Groups started by bonsai serve before bonsai web existed live in
			// this repository's daemon; stop them too so they are never stuck.
			stopLegacyServeGroups(out, errOut)
			return cmdWeb(args, in, out, errOut)
		case "status", "attach", "logs", "restart":
			return cmdWeb(args, in, out, errOut)
		}
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
	if *apiPort < 0 || *apiPort > 65535 {
		return fmt.Errorf("--api-port must be between 1 and 65535")
	}

	// The alias keeps its old behaviour: never an interactive setup.
	opts := webStartOptions{attach: !detached, noOpen: true, noSetup: true}
	if *apiPort != 0 {
		opts.port, opts.portFlag = *apiPort, true
	}
	if cfg := legacyServeConfig(errOut); cfg != nil {
		// Only values that differ from the old defaults were chosen by the
		// user; the defaults defer to the bonsai web settings.
		if cfg.Serve.APIPort != config.DefaultWebAPIPort {
			opts.preferredPort = cfg.Serve.APIPort
		}
		if cfg.Serve.StartupTimeout != 30 {
			opts.startupTimeout = cfg.Serve.StartupTimeout
		}
		if cfg.Serve.ShutdownTimeout != 5 {
			opts.shutdownTimeout = cfg.Serve.ShutdownTimeout
		}
	}
	w, err := newWebCLI(in, out, errOut)
	if err != nil {
		return err
	}
	return w.start(opts)
}

// legacyServeConfig loads the repository's .bonsai.yaml when the alias runs
// inside one, warning about keys bonsai serve no longer reads.
func legacyServeConfig(errOut io.Writer) *config.Config {
	repoDir, err := git.MainRoot(".")
	if err != nil {
		return nil
	}
	cfg, err := config.LoadFor(repoDir)
	if err != nil {
		return nil
	}
	for _, key := range cfg.DeprecatedServeKeys {
		fmt.Fprintf(errOut, "warning: %s is deprecated and ignored by normal bonsai serve; use the internal development stack instead\n", key)
	}
	return cfg
}

// stopLegacyServeGroups stops every per-repo production serve group the
// current repository's daemon still runs. Development stacks are left alone.
func stopLegacyServeGroups(out, errOut io.Writer) {
	repoDir, err := git.MainRoot(".")
	if err != nil {
		return
	}
	c := client.For(repoDir)
	if _, err := c.Ping(); err != nil {
		return // no daemon, no groups
	}
	for _, g := range persistedServeGroups(repoDir) {
		if g.mode != procstore.ServeModeProduction {
			continue
		}
		if err := c.ServeStop(g.workspaceID); err != nil {
			fmt.Fprintf(errOut, "could not stop the per-repo local API of %s: %v\n", g.workspace, err)
			continue
		}
		fmt.Fprintf(out, "✓ stopped the per-repo local API of %s\n", g.workspace)
	}
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
