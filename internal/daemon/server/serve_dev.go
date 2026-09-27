package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
)

func (s *Server) serveDevSecretPath(id string) string {
	return filepath.Join(s.serveDir(), safeServeID(id)+".dev-webhook-secret")
}

func (s *Server) newServeDevSecret(id string) (string, error) {
	if err := os.MkdirAll(s.serveDir(), 0o700); err != nil {
		return "", err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	path := s.serveDevSecretPath(id)
	if err := os.WriteFile(path, []byte(hex.EncodeToString(raw)), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func validateDevServeSpec(spec procstore.ServeSpec) error {
	if normalizedServeMode(spec) != procstore.ServeModeDevelopment {
		return fmt.Errorf("development stack requires development mode")
	}
	if spec.WorkspaceID == "" || spec.WorkspacePath == "" || spec.Executable == "" {
		return fmt.Errorf("development stack requires workspace id/path and executable")
	}
	for name, port := range map[string]int{"api": spec.APIPort, "webhook": spec.WebhookPort, "web": spec.WebPort} {
		if port < 1 || port > 65535 {
			return fmt.Errorf("%s port %d is invalid", name, port)
		}
	}
	if spec.APIPort == spec.WebhookPort || spec.APIPort == spec.WebPort || spec.WebhookPort == spec.WebPort {
		return fmt.Errorf("api, webhook, and web ports must be distinct")
	}
	u, err := url.Parse(spec.BrowserOrigin)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("development browser origin must be an explicit loopback HTTP origin")
	}
	if u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" {
		return fmt.Errorf("development browser origin must use 127.0.0.1 or localhost")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port != spec.WebPort {
		return fmt.Errorf("development browser origin port must match web port")
	}
	return nil
}

func (s *Server) serveStartDevStack(spec *procstore.ServeSpec) (*procstore.ServeGroup, error) {
	if err := validateDevServeSpec(*spec); err != nil {
		return nil, err
	}

	s.mu.Lock()
	existing := s.serveGroups[spec.WorkspaceID]
	s.mu.Unlock()
	if existing != nil {
		group := s.serveSnapshot(existing)
		modern := normalizedServeMode(existing.Spec) == procstore.ServeModeDevelopment &&
			existing.ProcessIDs["api"] != 0 &&
			existing.ProcessIDs["webhook"] != 0 &&
			existing.ProcessIDs["web"] != 0
		if modern && (group.State == "ready" || group.State == "degraded") {
			group.Reused = true
			return group, nil
		}
		if err := s.stopServeRuntime(existing); err != nil {
			return nil, fmt.Errorf("stop unhealthy development stack: %w", err)
		}
	}

	if err := checkServePorts(spec.APIPort, spec.WebhookPort, spec.WebPort); err != nil {
		return nil, err
	}
	if _, err := os.Stat(spec.Executable); err != nil {
		return nil, fmt.Errorf("bonsai executable: %w", err)
	}
	webDir := filepath.Join(spec.WorkspacePath, "web")
	if fi, err := os.Stat(webDir); err != nil || !fi.IsDir() {
		if err == nil {
			err = fmt.Errorf("not a directory")
		}
		return nil, fmt.Errorf("development web directory %s: %w", webDir, err)
	}

	secretFile, err := s.newServeDevSecret(spec.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("create development webhook secret: %w", err)
	}
	rt := &serveRuntime{
		Spec:       *spec,
		StartedAt:  time.Now().UTC(),
		ProcessIDs: map[string]int{},
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
	baseEnv := serveDevEnvironment(*spec)
	start := func(name, program string, args []string, cwd string, extra map[string]string, port int, required bool, policy procstore.Policy) error {
		env := cloneStringMap(baseEnv)
		for key, value := range extra {
			env[key] = value
		}
		id, err := s.spawnServeProcess(rt, name, program, args, cwd, env, port, required, policy)
		if err != nil {
			return err
		}
		if err := s.waitServeProcess(id, port, timeout); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		return nil
	}

	corePolicy := procstore.Policy{Mode: procstore.PolicyOnFailure, MaxRestarts: 3}
	if err := start("api", spec.Executable, []string{
		"__serve-api",
		"--repo", spec.WorkspacePath,
		"--port", strconv.Itoa(spec.APIPort),
		"--browser-origin", spec.BrowserOrigin,
		"--security-mode", "development",
	}, spec.WorkspacePath, nil, spec.APIPort, true, corePolicy); err != nil {
		s.cleanupFailedServe(rt)
		return nil, err
	}
	if err := start("webhook", spec.Executable, []string{
		"__serve-webhook",
		"--port", strconv.Itoa(spec.WebhookPort),
		"--browser-origin", spec.BrowserOrigin,
		"--secret-file", secretFile,
	}, spec.WorkspacePath, nil, spec.WebhookPort, true, corePolicy); err != nil {
		s.cleanupFailedServe(rt)
		return nil, err
	}
	webEnv := map[string]string{
		"VITE_BONSAI_LOCAL_API_ORIGIN": fmt.Sprintf("http://127.0.0.1:%d", spec.APIPort),
		"VITE_BONSAI_RELAY_ORIGIN":     fmt.Sprintf("http://127.0.0.1:%d", spec.WebhookPort),
	}
	if err := start("web", "pnpm", []string{
		"dev", "--", "--host", "127.0.0.1", "--port", strconv.Itoa(spec.WebPort), "--strictPort",
	}, webDir, webEnv, spec.WebPort, true, corePolicy); err != nil {
		s.cleanupFailedServe(rt)
		return nil, err
	}

	seen := map[string]bool{"api": true, "webhook": true, "web": true}
	for _, sidecar := range spec.Sidecars {
		if sidecar.Name == "" || seen[sidecar.Name] {
			s.cleanupFailedServe(rt)
			return nil, fmt.Errorf("invalid or duplicate development sidecar name %q", sidecar.Name)
		}
		seen[sidecar.Name] = true
		if len(sidecar.Command) == 0 || strings.TrimSpace(sidecar.Command[0]) == "" {
			s.cleanupFailedServe(rt)
			return nil, fmt.Errorf("development sidecar %q has no command", sidecar.Name)
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
		return nil, fmt.Errorf("development stack did not become ready")
	}
	return group, nil
}

func serveDevEnvironment(spec procstore.ServeSpec) map[string]string {
	return map[string]string{
		"BONSAI_API_HOST":     "127.0.0.1",
		"BONSAI_API_PORT":     strconv.Itoa(spec.APIPort),
		"BONSAI_WEBHOOK_HOST": "127.0.0.1",
		"BONSAI_WEBHOOK_PORT": strconv.Itoa(spec.WebhookPort),
		"BONSAI_WEB_HOST":     "127.0.0.1",
		"BONSAI_WEB_PORT":     strconv.Itoa(spec.WebPort),
		"BONSAI_WORKSPACE":    spec.WorkspacePath,
		"BONSAI_SERVE_GROUP":  spec.WorkspaceID,
	}
}

func (s *Server) serveRestartDevStack(rt *serveRuntime, processName string) (*procstore.ServeGroup, error) {
	var names []string
	if processName != "" {
		if _, ok := rt.ProcessIDs[processName]; !ok {
			return nil, fmt.Errorf("development stack has no process %q", processName)
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
		id := rt.ProcessIDs[name]
		rec, err := s.restart(id)
		if err != nil {
			return nil, fmt.Errorf("restart %s: %w", name, err)
		}
		if err := s.waitServeProcess(rec.ID, rec.ExpectedPort, timeout); err != nil {
			return nil, fmt.Errorf("restart %s: %w", name, err)
		}
	}
	return s.serveSnapshot(rt), nil
}
