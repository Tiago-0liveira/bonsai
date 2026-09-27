// Package exec manages independent background subprocesses. Any number may run
// per worktree path; their stdout/stderr is routed into byte buffers the UI can
// poll and select between.
package exec

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

// Process is a single running (or finished) subprocess with captured output.
type Process struct {
	ID      int
	Path    string
	Label   string
	Command string

	cmd  *exec.Cmd
	buf  *syncBuffer
	mu   sync.Mutex
	done bool
	err  error
	// killed marks a user-initiated kill so the exit reads "stopped", not
	// "failed" (a SIGKILL'd process exits non-zero).
	killed bool
}

// syncBuffer is a goroutine-safe bytes.Buffer.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// Output returns the captured stdout+stderr so far.
func (p *Process) Output() string { return p.buf.String() }

// urlRE matches any http(s) URL up to whitespace or common delimiters.
var urlRE = regexp.MustCompile(`https?://[^\s"'<>]+`)

// ansiRE strips SGR color sequences before scanning, since Spawn forces color
// and a URL may be wrapped in escape codes.
var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// isLoopbackURL reports whether u's host is a loopback/wildcard bind address.
func isLoopbackURL(u string) bool {
	host := strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	if strings.HasPrefix(host, "[") {
		if i := strings.IndexByte(host, ']'); i >= 0 {
			host = host[:i+1]
		}
	} else if i := strings.IndexAny(host, ":/"); i >= 0 {
		host = host[:i]
	}
	switch host {
	case "localhost", "127.0.0.1", "0.0.0.0", "[::1]", "::1":
		return true
	}
	return false
}

// LastLocalURL returns the last local (loopback) URL found in s, or "".
// Dev servers usually print their address once at startup; the last match wins
// so a restarted server's fresh port is preferred. Trailing punctuation is
// trimmed so prose like "…on http://localhost:3000." yields a clean URL.
func LastLocalURL(s string) string {
	matches := urlRE.FindAllString(ansiRE.ReplaceAllString(s, ""), -1)
	for i := len(matches) - 1; i >= 0; i-- {
		u := strings.TrimRight(matches[i], ".,;:")
		if isLoopbackURL(u) {
			return u
		}
	}
	return ""
}

// LastURL returns the last local URL this process has printed, or "".
func (p *Process) LastURL() string { return LastLocalURL(p.Output()) }

// Done reports whether the process has exited.
func (p *Process) Done() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.done
}

// Err returns the exit error, if any (valid once Done).
func (p *Process) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

// Status returns a short human-readable run state.
func (p *Process) Status() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.done {
		return "running"
	}
	if p.killed {
		return "stopped"
	}
	if p.err != nil {
		return "failed"
	}
	return "done"
}

// Kill terminates the process if still running. The process runs in its own
// group (see Spawn), so we signal the whole group: a shell like dash forks its
// command instead of exec-replacing itself, and killing only the shell would
// orphan that child. The orphan keeps the captured-output pipe open, which stalls
// cmd.Wait until it too exits — so the group kill is what lets the process report
// Done promptly.
func (p *Process) Kill() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.done || p.cmd == nil || p.cmd.Process == nil {
		return
	}
	p.killed = true
	killProcessTree(p.cmd)
}

// Manager owns every Process, grouped by worktree path.
type Manager struct {
	mu     sync.Mutex
	proc   map[string][]*Process
	nextID int
}

// NewManager builds an empty Manager.
func NewManager() *Manager {
	return &Manager{proc: map[string][]*Process{}}
}

// Spawn starts `sh -c command` in path as a new process (existing processes for
// that path keep running). label is a short display name. Output is captured
// into the returned Process buffer.
func (m *Manager) Spawn(path, label, command string) (*Process, error) {
	buf := &syncBuffer{}
	cmd := shellCommand(command)
	cmd.Dir = path
	cmd.Stdout = buf
	cmd.Stderr = buf
	// Coax color out of tools even though stdout is a pipe, not a TTY. Many CLIs
	// honor these; the viewport renders the ANSI.
	cmd.Env = append(os.Environ(), "CLICOLOR_FORCE=1", "FORCE_COLOR=1")
	// Own process group so Kill can reap the shell and any child it forks.
	setProcessGroup(cmd)

	m.mu.Lock()
	m.nextID++
	id := m.nextID
	m.mu.Unlock()

	p := &Process{ID: id, Path: path, Label: label, Command: command, cmd: cmd, buf: buf}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.proc[path] = append(m.proc[path], p)
	m.mu.Unlock()

	go func() {
		err := cmd.Wait()
		p.mu.Lock()
		p.done = true
		p.err = err
		p.mu.Unlock()
	}()

	return p, nil
}

// List returns every process for a path, oldest first.
func (m *Manager) List(path string) []*Process {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Process, len(m.proc[path]))
	copy(out, m.proc[path])
	return out
}

// All returns every tracked process across all paths.
func (m *Manager) All() []*Process {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*Process
	for _, ps := range m.proc {
		out = append(out, ps...)
	}
	return out
}

// GetByID returns the process with id under path, if present.
func (m *Manager) GetByID(path string, id int) (*Process, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.proc[path] {
		if p.ID == id {
			return p, true
		}
	}
	return nil, false
}

// Latest returns the most recently spawned process for a path, if any.
func (m *Manager) Latest(path string) (*Process, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := m.proc[path]
	if len(list) == 0 {
		return nil, false
	}
	return list[len(list)-1], true
}

// KillByID terminates the process with id under path, if running.
func (m *Manager) KillByID(path string, id int) {
	if p, ok := m.GetByID(path, id); ok {
		p.Kill()
	}
}

// Restart kills the process with id (if running) and spawns a fresh one with the
// same label and command, returning the new process.
func (m *Manager) Restart(path string, id int) (*Process, error) {
	p, ok := m.GetByID(path, id)
	if !ok {
		return nil, fmt.Errorf("no process #%d for %s", id, path)
	}
	p.Kill()
	return m.Spawn(path, p.Label, p.Command)
}

// Remove drops the process with id from path's list. It kills the process first
// if still running so no goroutine is left orphaned.
func (m *Manager) Remove(path string, id int) {
	if p, ok := m.GetByID(path, id); ok {
		p.Kill()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	list := m.proc[path]
	out := list[:0]
	for _, p := range list {
		if p.ID == id {
			continue
		}
		out = append(out, p)
	}
	m.proc[path] = out
}

// KillAll terminates every managed process.
func (m *Manager) KillAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, list := range m.proc {
		for _, p := range list {
			p.Kill()
		}
	}
}

// RunHooks executes each command with the platform shell in dir sequentially, stopping at
// the first failure. Before running, "{name}" placeholders are replaced using
// vars (see config.HookVars) and each var is also exported as $BONSAI_<NAME>.
// Empty command strings are skipped.
func RunHooks(dir string, commands []string, vars map[string]string) error {
	env := hookEnv(vars)
	for _, c := range commands {
		c = ExpandVars(c, vars)
		if strings.TrimSpace(c) == "" {
			continue
		}
		cmd := shellCommand(c)
		cmd.Dir = dir
		cmd.Env = env
		if err := cmd.Run(); err != nil {
			return err
		}
	}
	return nil
}

// ExpandVars replaces each "{name}" in s with vars[name].
func ExpandVars(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{"+k+"}", v)
	}
	return s
}

// hookEnv returns the process environment plus a BONSAI_<NAME> entry per var.
func hookEnv(vars map[string]string) []string {
	env := os.Environ()
	for k, v := range vars {
		env = append(env, "BONSAI_"+strings.ToUpper(k)+"="+v)
	}
	return env
}

// shellCommand builds an *exec.Cmd using the appropriate shell for the platform.
func shellCommand(command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		if s := envShell(); s != "" {
			return exec.Command(s, "-c", command)
		}
		comspec := os.Getenv("COMSPEC")
		if comspec == "" {
			comspec = "cmd.exe"
		}
		return exec.Command(comspec, "/c", command)
	}
	return exec.Command("sh", "-c", command)
}

// Command builds a shell *exec.Cmd rooted at dir. The caller wires up
// stdio (used by the CLI to run an alias in the foreground).
func Command(dir, command string) *exec.Cmd {
	cmd := shellCommand(command)
	cmd.Dir = dir
	return cmd
}

// ExecCommand builds a shell-free command rooted at dir. Use it for discovered
// project commands whose program and argv are already modeled structurally.
func ExecCommand(dir, program string, args ...string) *exec.Cmd {
	cmd := exec.Command(program, args...)
	cmd.Dir = dir
	return cmd
}

// SetProcessGroup configures cmd to run in its own process group or job.
func SetProcessGroup(cmd *exec.Cmd) {
	setProcessGroup(cmd)
}

// KillProcessTree terminates cmd and all child processes it spawned.
func KillProcessTree(cmd *exec.Cmd) {
	killProcessTree(cmd)
}

// ShellCmd builds an interactive shell *exec.Cmd rooted at path, suitable for
// tea.ExecProcess (the caller wires up stdio).
func ShellCmd(path string) *exec.Cmd {
	cmd := exec.Command(shell())
	cmd.Dir = path
	return cmd
}

// EditorCmd builds an interactive editor *exec.Cmd rooted at path, suitable
// for tea.ExecProcess. override (a configured editor command, arguments
// honored) takes priority; otherwise the editor comes from $VISUAL or
// $EDITOR, with vi or notepad as the final fallback.
func EditorCmd(path, override string) *exec.Cmd {
	name, args := editor(override)
	cmd := exec.Command(name, args...)
	cmd.Dir = path
	return cmd
}

func editor(override string) (string, []string) {
	raw := override
	if raw == "" {
		raw = envEditor()
	}
	if raw == "" {
		if runtime.GOOS == "windows" {
			return "notepad", nil
		}
		return "vi", nil
	}
	fields := strings.Fields(raw)
	return fields[0], fields[1:]
}

func shell() string {
	if s := envShell(); s != "" {
		return s
	}
	if runtime.GOOS == "windows" {
		if c := os.Getenv("COMSPEC"); c != "" {
			return c
		}
		return "cmd.exe"
	}
	return "/bin/sh"
}
