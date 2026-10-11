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

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/client"
)

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
		return nil, fmt.Errorf("production local API is active; use bonsai web")
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
