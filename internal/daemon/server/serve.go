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
	Spec           procstore.ServeSpec `json:"spec"`
	StartedAt      time.Time           `json:"started_at"`
	ProcessIDs     map[string]int      `json:"process_ids"`
	CapabilityFile string              `json:"capability_file"`
}

type serveCapability struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
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

func (s *Server) serveCapabilityPath(id string) string {
	return filepath.Join(s.serveDir(), safeServeID(id)+".capability")
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

func (s *Server) newServeCapability(id string) (string, serveCapability, error) {
	if err := os.MkdirAll(s.serveDir(), 0o700); err != nil {
		return "", serveCapability{}, err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", serveCapability{}, err
	}
	cap := serveCapability{
		Token:     hex.EncodeToString(raw),
		ExpiresAt: time.Now().Add(15 * time.Minute).UTC(),
	}
	data, err := json.Marshal(cap)
	if err != nil {
		return "", serveCapability{}, err
	}
	path := s.serveCapabilityPath(id)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", serveCapability{}, err
	}
	return path, cap, nil
}

func readServeCapability(path string) (serveCapability, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return serveCapability{}, err
	}
	var cap serveCapability
	if err := json.Unmarshal(data, &cap); err != nil {
		return serveCapability{}, err
	}
	if cap.Token == "" || cap.ExpiresAt.IsZero() {
		return serveCapability{}, fmt.Errorf("invalid serve capability")
	}
	return cap, nil
}

func (s *Server) removeServeArtifacts(id string) {
	_ = os.Remove(s.serveStatePath(id))
	_ = os.Remove(s.serveCapabilityPath(id))
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
		modern := existing.CapabilityFile != "" &&
			existing.Spec.BrowserOrigin != "" &&
			len(existing.ProcessIDs) == 1 &&
			existing.ProcessIDs["api"] != 0
		if modern && group.State == "ready" {
			group.Reused = true
			return group, nil
		}
		if err := s.stopServeRuntime(existing); err != nil {
			return nil, fmt.Errorf("stop unhealthy serve group: %w", err)
		}
	}

	if err := checkServePorts(spec.APIPort); err != nil {
		return nil, err
	}
	if _, err := os.Stat(spec.Executable); err != nil {
		return nil, fmt.Errorf("bonsai executable: %w", err)
	}

	capabilityFile, _, err := s.newServeCapability(spec.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("create local API capability: %w", err)
	}
	rt := &serveRuntime{
		Spec:           *spec,
		StartedAt:      time.Now().UTC(),
		ProcessIDs:     map[string]int{},
		CapabilityFile: capabilityFile,
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
	start := func(name, program string, args []string, cwd string, port int) error {
		id, err := s.spawnServeProcess(
			rt,
			name,
			program,
			args,
			cwd,
			serveEnvironment(*spec),
			port,
			true,
			procstore.Policy{Mode: procstore.PolicyOnFailure, MaxRestarts: 3},
		)
		if err != nil {
			return err
		}
		if err := s.waitServeProcess(id, port, timeout); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		return nil
	}

	if err := start(
		"api",
		spec.Executable,
		[]string{
			"__serve-api",
			"--repo", spec.WorkspacePath,
			"--port", strconv.Itoa(spec.APIPort),
			"--browser-origin", spec.BrowserOrigin,
			"--capability-file", capabilityFile,
		},
		spec.WorkspacePath,
		spec.APIPort,
	); err != nil {
		s.cleanupFailedServe(rt)
		return nil, err
	}

	group := s.serveSnapshot(rt)
	if group.State != "ready" {
		s.cleanupFailedServe(rt)
		return nil, fmt.Errorf("serve group did not become ready")
	}
	return group, nil
}

func validateServeSpec(spec procstore.ServeSpec) error {
	if spec.WorkspaceID == "" || spec.WorkspacePath == "" || spec.Executable == "" {
		return fmt.Errorf("serve requires workspace id/path and executable")
	}
	if spec.APIPort < 1 || spec.APIPort > 65535 {
		return fmt.Errorf("api port %d is invalid", spec.APIPort)
	}
	if spec.BrowserOrigin == "" {
		return fmt.Errorf("browser origin is required")
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
		"BONSAI_API_HOST":    "127.0.0.1",
		"BONSAI_API_PORT":    strconv.Itoa(spec.APIPort),
		"BONSAI_WORKSPACE":   spec.WorkspacePath,
		"BONSAI_SERVE_GROUP": spec.WorkspaceID,
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
		State: "ready", StartedAt: rt.StartedAt, APIPort: rt.Spec.APIPort, BrowserOrigin: rt.Spec.BrowserOrigin,
	}
	if cap, err := readServeCapability(rt.CapabilityFile); err == nil {
		group.CapabilityToken = cap.Token
		group.CapabilityExpiresAt = cap.ExpiresAt
	}
	names := make([]string, 0, len(rt.ProcessIDs))
	for name := range rt.ProcessIDs {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		order := map[string]int{"api": 0}
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
		order := map[string]int{"api": 1}
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
		if _, ok := rt.ProcessIDs["api"]; ok {
			names = append(names, "api")
		}
		var legacy []string
		for name := range rt.ProcessIDs {
			if name != "api" {
				legacy = append(legacy, name)
			}
		}
		sort.Strings(legacy)
		names = append(names, legacy...)
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
