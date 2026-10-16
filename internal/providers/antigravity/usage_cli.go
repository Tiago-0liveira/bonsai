package antigravity

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	coreexec "github.com/Tiago-0liveira/bonsai/internal/core/exec"
)

// cliTimeout bounds one non-interactive agy command. agy can hang on a network
// or login problem, and nothing else would end it, so a usage read that fell
// back to it would never finish.
const cliTimeout = 45 * time.Second

// cliRunner runs one non-interactive agy command and returns its standard
// output. Tests replace it.
type cliRunner func(ctx context.Context, executable, dir string, envSet map[string]string, args ...string) ([]byte, error)

func runCLI(ctx context.Context, executable, dir string, envSet map[string]string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, cliTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir = dir
	// agy starts helper processes; ending only the main one would leave them
	// running, so the whole group goes.
	coreexec.SetProcessGroup(cmd)
	cmd.Cancel = func() error {
		coreexec.KillProcessTree(cmd)
		return nil
	}
	cmd.Env = agents.BuildEnvironment(os.Environ(), envSet, nil)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = nil // discarded: auth and quota messages may carry account details
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Run(); err != nil {
		// Do not include provider output in the error: auth/quota responses may
		// contain account details and should not escape provider-owned parsing.
		return nil, fmt.Errorf("antigravity %s command: %w", commandName(args), err)
	}
	return stdout.Bytes(), nil
}

// commandName names a command for errors: the subcommand or the slash command.
func commandName(args []string) string {
	for i, arg := range args {
		if arg == "-p" && i+1 < len(args) {
			return args[i+1]
		}
	}
	if len(args) > 0 {
		return args[0]
	}
	return "agy"
}

func (p *Provider) runner() cliRunner {
	if p.run != nil {
		return p.run
	}
	return runCLI
}

// profileRun is one throwaway session of a profile: agy runs in its working
// directory with its environment.
type profileRun struct {
	executable string
	dir        string
	env        map[string]string
}

// withProfile runs fn in a throwaway session of the profile: the credentials
// are materialized before and reconciled after (agy may refresh the token), and
// the session is always cleaned up. No credential lock is held while fn runs.
func (p *Provider) withProfile(ctx context.Context, account agents.Account, fn func(context.Context, profileRun) error) error {
	workDir, _ := os.Getwd()
	session, err := p.sessions.Create(account, workDir)
	if err != nil {
		return err
	}
	if err := p.credentialMgr.Materialize(ctx, account, session); err != nil {
		return errors.Join(err, p.sessions.Cleanup(session))
	}
	executable, err := p.binaryResolver.Resolve()
	if err != nil {
		return errors.Join(err, p.sessions.Cleanup(session))
	}
	runErr := fn(ctx, profileRun{executable: executable, dir: session.WorkDir, env: buildEnvironment(account, session)})
	// A caller that gave up (the race was won elsewhere) still gets agy's
	// refreshed token saved: the reconcile runs on its own, bounded context.
	reconcileCtx, cancelReconcile := context.WithTimeout(context.WithoutCancel(ctx), reconcileWait)
	reconcileErr := p.credentialMgr.Reconcile(reconcileCtx, account, session)
	cancelReconcile()
	cleanupErr := p.sessions.Cleanup(session)
	return errors.Join(runErr, reconcileErr, cleanupErr)
}

// reconcileWait bounds the credential lock wait of a reconcile that runs
// after its caller was cancelled.
const reconcileWait = 5 * time.Second
