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
		case "status", "attach", "logs", "restart", "stop":
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
	if *apiPort == 0 {
		*apiPort = legacyServePort(errOut)
	}

	webArgs := []string{"--no-open"}
	if !detached {
		webArgs = append(webArgs, "--attach")
	}
	if *apiPort != 0 {
		webArgs = append(webArgs, "--port", strconv.Itoa(*apiPort))
	}
	return cmdWeb(webArgs, in, out, errOut)
}

// legacyServePort honours a repository's explicit serve.api_port when the
// alias runs inside one. Zero means "use the bonsai web settings".
func legacyServePort(errOut io.Writer) int {
	repoDir, err := git.MainRoot(".")
	if err != nil {
		return 0
	}
	cfg, err := config.LoadFor(repoDir)
	if err != nil {
		return 0
	}
	for _, key := range cfg.DeprecatedServeKeys {
		fmt.Fprintf(errOut, "warning: %s is deprecated and ignored by normal bonsai serve; use the internal development stack instead\n", key)
	}
	if cfg.Serve.APIPort != config.DefaultWebAPIPort {
		return cfg.Serve.APIPort
	}
	return 0
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
