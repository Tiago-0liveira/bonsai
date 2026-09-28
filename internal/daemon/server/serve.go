package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	coreexec "github.com/Tiago-0liveira/bonsai/internal/core/exec"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/protocol"
)

const (
	serveDefaultStartupTimeout  = 30 * time.Second
	serveDefaultShutdownTimeout = 5 * time.Second
	serveSidecarGrace           = 600 * time.Millisecond
)

type serveRuntime struct {
	Spec       procstore.ServeSpec `json:"spec"`
	StartedAt  time.Time           `json:"started_at"`
	ProcessIDs map[string]int      `json:"process_ids"`
	SecretFile string              `json:"secret_file"`
}

func (s *Server) serveDir() string {
	return filepath.Join(s.store.Dir(), "serve")
}

func safeServeID(id string) string {
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "workspace"
	}
	return b.String()
}

func (s *Server) serveStatePath(id string) string {
	return filepath.Join(s.serveDir(), safeServeID(id)+".json")
}

func (s *Server) serveSecretPath(id string) string {
	return filepath.Join(s.serveDir(), safeServeID(id)+".secret")
}

func (s *Server) writeServeRuntime(rt *serveRuntime) error {
	if err := os.MkdirAll(s.serveDir(), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(rt, "", "  ")
	if err != nil {
		return err
	}
	path := s.serveStatePath(rt.Spec.WorkspaceID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *Server) loadServeGroups() {
	entries, err := os.ReadDir(s.serveDir())
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.serveDir(), entry.Name()))
		if err != nil {
			continue
		}
		var rt serveRuntime
		if json.Unmarshal(data, &rt) != nil || rt.Spec.WorkspaceID == "" {
			continue
		}
		if rt.ProcessIDs == nil {
			rt.ProcessIDs = map[string]int{}
		}
		s.serveGroups[rt.Spec.WorkspaceID] = &rt
	}
}

func (s *Server) newServeSecret(id string) (string, error) {
	if err := os.MkdirAll(s.serveDir(), 0o700); err != nil {
		return "", err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	path := s.serveSecretPath(id)
	if err := os.WriteFile(path, []byte(hex.EncodeToString(raw)), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func (s *Server) removeServeArtifacts(id string) {
	_ = os.Remove(s.serveStatePath(id))
	_ = os.Remove(s.serveSecretPath(id))
}

func (s *Server) serveStart(spec *procstore.ServeSpec) (*procstore.ServeGroup, error) {
	if spec == nil {
		return nil, fmt.Errorf("missing serve specification")
	}
	if err := validateServeSpec(*spec); err != nil {
		return nil, err
	}

	s.mu.Lock()
	existing := s.serveGroups[spec.WorkspaceID]
	s.mu.Unlock()
	if existing != nil {
		group := s.serveSnapshot(existing)
		if group.State == "ready" || group.State == "degraded" {
			group.Reused = true
			return group, nil
		}
		if err := s.stopServeRuntime(existing); err != nil {
			return nil, fmt.Errorf("stop unhealthy serve group: %w", err)
		}
	}

	if err := checkServePorts(spec.APIPort, spec.WebhookPort, spec.WebPort); err != nil {
		return nil, err
	}
	if _, err := os.Stat(spec.Executable); err != nil {
		return nil, fmt.Errorf("bonsai executable: %w", err)
	}
	if _, err := os.Stat(spec.ServerConfig); err != nil {
		return nil, fmt.Errorf("serve server config: %w", err)
	}
	webDir := filepath.Join(spec.WorkspacePath, "web")
	if fi, err := os.Stat(webDir); err != nil || !fi.IsDir() {
		if err == nil {
			err = fmt.Errorf("not a directory")
		}
		return nil, fmt.Errorf("serve web directory %s: %w", webDir, err)
	}

	secretFile, err := s.newServeSecret(spec.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("create serve session secret: %w", err)
	}
	rt := &serveRuntime{
		Spec:       *spec,
		StartedAt:  time.Now().UTC(),
		ProcessIDs: map[string]int{},
		SecretFile: secretFile,
	}
	s.mu.Lock()
	s.serveGroups[spec.WorkspaceID] = rt
	s.mu.Unlock()
	if err := s.writeServeRuntime(rt); err != nil {
		s.mu.Lock()
		delete(s.serveGroups, spec.WorkspaceID)
		s.mu.Unlock()
		s.removeServeArtifacts(spec.WorkspaceID)
		return nil, err
	}

	timeout := serveDefaultStartupTimeout
	if spec.StartupTimeoutSeconds > 0 {
		timeout = time.Duration(spec.StartupTimeoutSeconds) * time.Second
	}
	env := serveEnvironment(*spec)
	start := func(name, program string, args []string, cwd string, extra map[string]string, port int, required bool, policy procstore.Policy) error {
		processEnv := cloneStringMap(env)
		for key, value := range extra {
			processEnv[key] = value
		}
		id, err := s.spawnServeProcess(rt, name, program, args, cwd, processEnv, port, required, policy)
		if err != nil {
			return err
		}
		if err := s.waitServeProcess(id, port, timeout); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		return nil
	}

	corePolicy := procstore.Policy{Mode: procstore.PolicyOnFailure, MaxRestarts: 3}
	secretEnv := map[string]string{"BONSAI_SERVE_SECRET_FILE": secretFile}
	if err := start("api", spec.Executable,
		[]string{"__serve-api", "--config", spec.ServerConfig, "--port", strconv.Itoa(spec.APIPort),
			"--browser-origin", fmt.Sprintf("http://127.0.0.1:%d", spec.WebPort), "--session-secret-file", secretFile},
		spec.WorkspacePath, secretEnv, spec.APIPort, true, corePolicy); err != nil {
		s.cleanupFailedServe(rt)
		return nil, err
	}
	if err := start("webhook", spec.Executable,
		[]string{"__serve-webhook", "--config", spec.ServerConfig, "--port", strconv.Itoa(spec.WebhookPort), "--api-port", strconv.Itoa(spec.APIPort), "--session-secret-file", secretFile},
		spec.WorkspacePath, secretEnv, spec.WebhookPort, true, corePolicy); err != nil {
		s.cleanupFailedServe(rt)
		return nil, err
	}
	if err := start("web", "npm",
		[]string{"run", "start", "--", "--host", "127.0.0.1", "--port", strconv.Itoa(spec.WebPort), "--strictPort"},
		webDir, nil, spec.WebPort, true, corePolicy); err != nil {
		s.cleanupFailedServe(rt)
		return nil, err
	}

	seen := map[string]bool{"api": true, "webhook": true, "web": true}
	for _, sidecar := range spec.Sidecars {
		if sidecar.Name == "" {
			s.cleanupFailedServe(rt)
			return nil, fmt.Errorf("sidecar name is required")
		}
		if seen[sidecar.Name] {
			s.cleanupFailedServe(rt)
			return nil, fmt.Errorf("duplicate serve process name %q", sidecar.Name)
		}
		seen[sidecar.Name] = true
		if len(sidecar.Command) == 0 || strings.TrimSpace(sidecar.Command[0]) == "" {
			s.cleanupFailedServe(rt)
			return nil, fmt.Errorf("sidecar %q has no command", sidecar.Name)
		}
		cwd := sidecar.Cwd
		if cwd == "" {
			cwd = spec.WorkspacePath
		} else if !filepath.IsAbs(cwd) {
			cwd = filepath.Join(spec.WorkspacePath, cwd)
		}
		mode := sidecar.Restart
		if !procstore.ValidMode(mode) {
			mode = procstore.PolicyOnFailure
		}
		max := sidecar.MaxRestarts
		if max <= 0 {
			max = 3
		}
		if err := start(sidecar.Name, sidecar.Command[0], append([]string(nil), sidecar.Command[1:]...),
			cwd, sidecar.Environment, 0, sidecar.Required, procstore.Policy{Mode: mode, MaxRestarts: max}); err != nil {
			if sidecar.Required {
				s.cleanupFailedServe(rt)
				return nil, err
			}
		}
	}

	group := s.serveSnapshot(rt)
	if group.State != "ready" && group.State != "degraded" {
		s.cleanupFailedServe(rt)
		return nil, fmt.Errorf("serve group did not become ready")
	}
	return group, nil
}

func validateServeSpec(spec procstore.ServeSpec) error {
	if spec.WorkspaceID == "" || spec.WorkspacePath == "" || spec.Executable == "" || spec.ServerConfig == "" {
		return fmt.Errorf("serve requires workspace id/path, executable, and server config")
	}
	for name, port := range map[string]int{"api": spec.APIPort, "webhook": spec.WebhookPort, "web": spec.WebPort} {
		if port < 1 || port > 65535 {
			return fmt.Errorf("%s port %d is invalid", name, port)
		}
	}
	if spec.APIPort == spec.WebhookPort || spec.APIPort == spec.WebPort || spec.WebhookPort == spec.WebPort {
		return fmt.Errorf("api, webhook, and web ports must be distinct")
	}
	return nil
}

func checkServePorts(ports ...int) error {
	for _, port := range ports {
		ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			return fmt.Errorf("port %d is already in use", port)
		}
		_ = ln.Close()
	}
	return nil
}

func serveEnvironment(spec procstore.ServeSpec) map[string]string {
	return map[string]string{
		"BONSAI_API_HOST":     "127.0.0.1",
		"BONSAI_API_PORT":     strconv.Itoa(spec.APIPort),
		"BONSAI_WEBHOOK_HOST": "127.0.0.1",
		"BONSAI_WEBHOOK_PORT": strconv.Itoa(spec.WebhookPort),
		"BONSAI_WEB_HOST":     "127.0.0.1",
		"BONSAI_WEB_PORT":     strconv.Itoa(spec.WebPort),
		"BONSAI_WORKSPACE":    spec.WorkspacePath,
		"BONSAI_SERVE_GROUP":  spec.WorkspaceID,
		"PORT":                strconv.Itoa(spec.WebPort),
	}
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func (s *Server) spawnServeProcess(rt *serveRuntime, name, program string, args []string, cwd string,
	environment map[string]string, expectedPort int, required bool, policy procstore.Policy) (int, error) {
	rec, err := s.spawn(&protocol.Request{
		Kind:          protocol.KindSpawn,
		Worktree:      rt.Spec.WorkspacePath,
		Label:         "serve:" + name,
		Program:       program,
		Args:          append([]string(nil), args...),
		WorkingDir:    cwd,
		Environment:   environment,
		ExpectedPort:  expectedPort,
		ServeGroup:    rt.Spec.WorkspaceID,
		ServeName:     name,
		ServeRequired: required,
		Policy:        &policy,
	})
	if err != nil {
		return 0, err
	}
	rt.ProcessIDs[name] = rec.ID
	if err := s.writeServeRuntime(rt); err != nil {
		_, _ = s.kill(&protocol.Request{ID: rec.ID})
		return 0, err
	}
	return rec.ID, nil
}

func (s *Server) waitServeProcess(id, expectedPort int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	notBefore := time.Now().Add(serveSidecarGrace)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		mp := s.procs[id]
		s.mu.Unlock()
		if mp == nil {
			return fmt.Errorf("process disappeared")
		}
		mp.mu.Lock()
		status := mp.rec.Status
		exitCode := mp.rec.ExitCode
		exitErr := mp.rec.ExitError
		mp.mu.Unlock()
		if procstore.IsTerminal(status) {
			return s.serveStartupError(id, status, exitCode, exitErr)
		}
		alive := status == procstore.StatusRunning || status == procstore.StatusOrphan
		if expectedPort > 0 {
			if alive && portListening(expectedPort) {
				return nil
			}
		} else if alive && time.Now().After(notBefore) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("readiness timed out after %s", timeout)
}

func (s *Server) serveStartupError(id int, status string, exitCode *int, exitErr string) error {
	detail := status
	if exitCode != nil {
		detail += fmt.Sprintf(" (exit %d)", *exitCode)
	}
	if exitErr != "" {
		detail += ": " + exitErr
	}
	if data, err := s.store.ReadCombinedLog(id); err == nil {
		if tail := strings.TrimSpace(procstore.LastLines(string(data), 20)); tail != "" {
			detail += "\n" + tail
		}
	}
	return fmt.Errorf("%s", detail)
}

func portListening(port int) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 150*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (s *Server) serveStatus(id string) (*procstore.ServeGroup, error) {
	s.mu.Lock()
	rt := s.serveGroups[id]
	s.mu.Unlock()
	if rt == nil {
		return nil, nil
	}
	return s.serveSnapshot(rt), nil
}

func (s *Server) serveSnapshot(rt *serveRuntime) *procstore.ServeGroup {
	group := &procstore.ServeGroup{
		ID: rt.Spec.WorkspaceID, WorkspaceID: rt.Spec.WorkspaceID, WorkspacePath: rt.Spec.WorkspacePath,
		State: "ready", StartedAt: rt.StartedAt, APIPort: rt.Spec.APIPort, WebhookPort: rt.Spec.WebhookPort, WebPort: rt.Spec.WebPort,
	}
	names := make([]string, 0, len(rt.ProcessIDs))
	for name := range rt.ProcessIDs {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		order := map[string]int{"api": 0, "webhook": 1, "web": 2}
		ai, aok := order[names[i]]
		aj, bok := order[names[j]]
		if aok != bok {
			return aok
		}
		if aok && ai != aj {
			return ai < aj
		}
		return names[i] < names[j]
	})
	requiredReady := true
	optionalFailed := false
	for _, name := range names {
		id := rt.ProcessIDs[name]
		p := procstore.ServeProcess{Name: name, ID: id}
		s.mu.Lock()
		mp := s.procs[id]
		s.mu.Unlock()
		if mp == nil {
			p.State = procstore.StatusLost
		} else {
			mp.mu.Lock()
			rec := *mp.rec
			mp.mu.Unlock()
			p.PID, p.ProcessGroupID, p.ExpectedPort, p.Required = rec.PID, rec.ProcessGroupID, rec.ExpectedPort, rec.ServeRequired
			p.StartedAt, p.ExitCode, p.ExitError, p.State = rec.StartedAt, rec.ExitCode, rec.ExitError, rec.Status
			alive := rec.Status == procstore.StatusRunning || rec.Status == procstore.StatusOrphan
			if rec.ExpectedPort > 0 && alive {
				if portListening(rec.ExpectedPort) {
					p.State = "ready"
				} else {
					p.State = "starting"
				}
			} else if rec.ExpectedPort == 0 && alive {
				p.State = "running"
			}
		}
		if p.Required && p.State != "ready" && p.State != "running" {
			requiredReady = false
		}
		if !p.Required && (p.State == procstore.StatusFailed || p.State == procstore.StatusLost) {
			optionalFailed = true
		}
		group.Processes = append(group.Processes, p)
	}
	if !requiredReady {
		group.State = "failed"
		for _, p := range group.Processes {
			if p.Required && (p.State == "starting" || p.State == procstore.StatusStarting || p.State == procstore.StatusBackoff) {
				group.State = "starting"
				break
			}
		}
	} else if optionalFailed {
		group.State = "degraded"
	}
	return group
}

func serveShutdownGrace(spec procstore.ServeSpec) time.Duration {
	if spec.ShutdownTimeoutSeconds > 0 {
		return time.Duration(spec.ShutdownTimeoutSeconds) * time.Second
	}
	return serveDefaultShutdownTimeout
}

func (s *Server) cleanupFailedServe(rt *serveRuntime) { _ = s.stopServeRuntime(rt) }

func (s *Server) serveStop(id string) error {
	s.mu.Lock()
	rt := s.serveGroups[id]
	s.mu.Unlock()
	if rt == nil {
		return nil
	}
	return s.stopServeRuntime(rt)
}

func (s *Server) stopServeRuntime(rt *serveRuntime) error {
	names := make([]string, 0, len(rt.ProcessIDs))
	for name := range rt.ProcessIDs {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		order := map[string]int{"api": 3, "webhook": 2, "web": 1}
		ai, aok := order[names[i]]
		aj, bok := order[names[j]]
		if aok != bok {
			return !aok
		}
		if aok && ai != aj {
			return ai < aj
		}
		return names[i] < names[j]
	})
	grace := serveShutdownGrace(rt.Spec)
	var errs []string
	for _, name := range names {
		id := rt.ProcessIDs[name]
		s.mu.Lock()
		mp := s.procs[id]
		s.mu.Unlock()
		if mp == nil {
			continue
		}
		if err := s.stopServeProcess(mp, grace); err != nil {
			errs = append(errs, name+": "+err.Error())
		}
	}
	s.mu.Lock()
	if s.serveGroups[rt.Spec.WorkspaceID] == rt {
		delete(s.serveGroups, rt.Spec.WorkspaceID)
	}
	s.mu.Unlock()
	s.removeServeArtifacts(rt.Spec.WorkspaceID)
	s.closeServeLogSubscribers(rt.Spec.WorkspaceID)
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func (s *Server) stopServeProcess(mp *managedProc, grace time.Duration) error {
	mp.mu.Lock()
	status := mp.rec.Status
	if procstore.IsTerminal(status) {
		mp.mu.Unlock()
		return nil
	}
	if status != procstore.StatusRunning {
		mp.mu.Unlock()
		if !s.killManaged(mp) {
			return fmt.Errorf("failed to confirm stop")
		}
		return nil
	}
	mp.rec.Status = procstore.StatusStopping
	_ = s.store.WriteRecord(mp.rec)
	cmd, pid, done := mp.cmd, mp.rec.PID, mp.waitDone
	mp.mu.Unlock()

	if cmd != nil {
		coreexec.TerminateProcessTree(cmd, grace)
	} else if pid > 0 {
		coreexec.TerminatePIDTree(pid, grace)
	}
	if done != nil {
		select {
		case <-done:
			return nil
		case <-time.After(grace + time.Second):
			if pid > 0 {
				coreexec.KillPID(pid)
			}
			return fmt.Errorf("process did not exit before shutdown deadline")
		}
	}
	return nil
}

func (s *Server) serveRestart(id, processName string) (*procstore.ServeGroup, error) {
	s.mu.Lock()
	rt := s.serveGroups[id]
	s.mu.Unlock()
	if rt == nil {
		return nil, fmt.Errorf("no serve group for workspace")
	}
	var names []string
	if processName != "" {
		if _, ok := rt.ProcessIDs[processName]; !ok {
			return nil, fmt.Errorf("serve group has no process %q", processName)
		}
		names = append(names, processName)
	} else {
		for _, core := range []string{"api", "webhook", "web"} {
			if _, ok := rt.ProcessIDs[core]; ok {
				names = append(names, core)
			}
		}
		var sidecars []string
		for name := range rt.ProcessIDs {
			if name != "api" && name != "webhook" && name != "web" {
				sidecars = append(sidecars, name)
			}
		}
		sort.Strings(sidecars)
		names = append(names, sidecars...)
	}
	timeout := serveDefaultStartupTimeout
	if rt.Spec.StartupTimeoutSeconds > 0 {
		timeout = time.Duration(rt.Spec.StartupTimeoutSeconds) * time.Second
	}
	for _, name := range names {
		pid := rt.ProcessIDs[name]
		rec, err := s.restart(pid)
		if err != nil {
			return nil, fmt.Errorf("restart %s: %w", name, err)
		}
		if err := s.waitServeProcess(rec.ID, rec.ExpectedPort, timeout); err != nil {
			return nil, fmt.Errorf("restart %s: %w", name, err)
		}
	}
	return s.serveSnapshot(rt), nil
}
