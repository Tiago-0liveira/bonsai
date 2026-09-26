package pkgmgr

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

const (
	providerStdoutLimit = 1 << 20
	providerStderrLimit = 64 << 10
)

// Runner executes allowlisted provider introspection. Tests can inject a fake
// runner so discovery never depends on tools installed on the test machine.
type Runner interface {
	Run(ctx context.Context, dir, program string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, dir, program string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Dir = dir

	var stdout, stderr limitedBuffer
	stdout.max = providerStdoutLimit
	stderr.max = providerStderrLimit
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %s", err, msg)
	}
	return append([]byte(nil), stdout.Bytes()...), nil
}

type limitedBuffer struct {
	bytes.Buffer
	max int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Buffer.Len()+len(p) > b.max {
		remaining := b.max - b.Buffer.Len()
		if remaining > 0 {
			_, _ = b.Buffer.Write(p[:remaining])
		}
		return len(p), fmt.Errorf("provider output exceeded %d bytes", b.max)
	}
	return b.Buffer.Write(p)
}
