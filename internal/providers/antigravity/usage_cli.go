package antigravity

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func runUsageCLI(ctx context.Context, executable, dir string, envSet map[string]string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, executable,
		"--dangerously-skip-permissions",
		"-p", "/usage",
		"--output-format", "json",
	)
	cmd.Dir = dir
	cmd.Env = agents.BuildEnvironment(os.Environ(), envSet, nil)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// Do not include provider output in the error: auth/quota responses may
		// contain account details and should not escape provider-owned parsing.
		return nil, fmt.Errorf("antigravity usage command: %w", err)
	}
	return stdout.Bytes(), nil
}
