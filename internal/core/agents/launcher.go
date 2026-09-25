package agents

import (
	"context"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
)

type Launcher interface {
	RunForeground(context.Context, PreparedSession) error
}

type ForegroundLauncher struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

func NewForegroundLauncher(in io.Reader, out, errOut io.Writer) *ForegroundLauncher {
	return &ForegroundLauncher{In: in, Out: out, Err: errOut}
}

func (l *ForegroundLauncher) RunForeground(ctx context.Context, prepared PreparedSession) error {
	cmd := exec.CommandContext(ctx, prepared.Executable, prepared.Args...)
	cmd.Dir = prepared.Dir
	cmd.Env = BuildEnvironment(os.Environ(), prepared.EnvSet, prepared.EnvUnset)
	if l.In != nil {
		cmd.Stdin = l.In
	} else {
		cmd.Stdin = os.Stdin
	}
	if l.Out != nil {
		cmd.Stdout = l.Out
	} else {
		cmd.Stdout = os.Stdout
	}
	if l.Err != nil {
		cmd.Stderr = l.Err
	} else {
		cmd.Stderr = os.Stderr
	}
	return cmd.Run()
}

func BuildEnvironment(base []string, set map[string]string, unset []string) []string {
	key := func(s string) string {
		if runtime.GOOS == "windows" {
			return strings.ToUpper(s)
		}
		return s
	}
	env := make(map[string]string, len(base)+len(set))
	names := make(map[string]string, len(base)+len(set))
	for _, entry := range base {
		if i := strings.IndexByte(entry, '='); i >= 0 {
			k, v := entry[:i], entry[i+1:]
			n := key(k)
			env[n], names[n] = v, k
		}
	}
	for _, k := range unset {
		n := key(k)
		delete(env, n)
		delete(names, n)
	}
	for k, v := range set {
		n := key(k)
		env[n], names[n] = v, k
	}
	keys := make([]string, 0, len(env))
	for n := range env {
		keys = append(keys, n)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, n := range keys {
		out = append(out, names[n]+"="+env[n])
	}
	return out
}
