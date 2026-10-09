package claude

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// maxSeedBytes caps the total size copied into a new profile.
const maxSeedBytes = 20 << 20

// Seeding is a one-time, allowlisted snapshot of the user's own Claude setup.
// Credentials, history, sessions, plugin records and every other .claude.json
// key are never copied (Claude reinstalls enabled plugins on its own).
var (
	seedFiles = []string{"settings.json", "CLAUDE.md", "keybindings.json"}
	seedDirs  = []string{"agents", "commands", "skills", "output-styles"}
)

// settingsCredentialKeys are removed from a seeded settings.json: they run
// credential helpers or carry credentials, which would override the profile's
// own login.
//
// forceLogin* pin the login to the host user's method or organization, which
// would block signing a different account into the profile.
var settingsCredentialKeys = []string{"apiKeyHelper", "awsAuthRefresh", "awsCredentialExport", "otelHeadersHelper", "forceLoginMethod", "forceLoginOrgUUID"}

type seedReport struct {
	copied  []string
	skipped []string
}

type seeder struct {
	dst       string
	remaining int64
	report    seedReport
}

// seedProfile copies the allowlisted items from srcDir into dst. jsonCandidates
// are the places a .claude.json holding user MCP servers may live, in order.
func seedProfile(srcDir string, jsonCandidates []string, dst string, limit int64) (seedReport, error) {
	resolved, err := filepath.EvalSymlinks(srcDir)
	if err != nil {
		return seedReport{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return seedReport{}, err
	}
	if !info.IsDir() {
		return seedReport{}, fmt.Errorf("seed source %s is not a directory", srcDir)
	}
	s := &seeder{dst: dst, remaining: limit}
	for _, name := range seedFiles {
		s.file(filepath.Join(resolved, name), name)
	}
	for _, name := range seedDirs {
		s.dir(filepath.Join(resolved, name), name)
	}
	s.mcpServers(jsonCandidates)
	return s.report, nil
}

func (s *seeder) skip(name, why string) {
	s.report.skipped = append(s.report.skipped, name+" ("+why+")")
}

func (s *seeder) file(src, rel string) {
	info, err := os.Lstat(src)
	if err != nil {
		return
	}
	if !info.Mode().IsRegular() {
		s.skip(rel, "not a regular file")
		return
	}
	var data []byte
	if data, err = readBounded(src, s.remaining); err != nil {
		s.skip(rel, err.Error())
		return
	}
	if rel == "settings.json" {
		if data, err = filterSettings(data); err != nil {
			s.skip(rel, "invalid JSON")
			return
		}
	}
	if err := s.write(rel, data, info.Mode()); err != nil {
		s.skip(rel, err.Error())
		return
	}
	s.report.copied = append(s.report.copied, rel)
}

func (s *seeder) dir(src, rel string) {
	info, err := os.Lstat(src)
	if err != nil {
		return
	}
	if !info.IsDir() {
		s.skip(rel, "not a directory")
		return
	}
	count := 0
	_ = filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == src {
			return nil
		}
		name, _ := filepath.Rel(filepath.Dir(src), path)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			s.skip(name, "symlink")
		case d.IsDir():
		case !d.Type().IsRegular():
			s.skip(name, "not a regular file")
		default:
			fi, err := d.Info()
			if err != nil {
				return nil
			}
			data, err := readBounded(path, s.remaining)
			if err != nil {
				s.skip(name, err.Error())
				return nil
			}
			if err := s.write(name, data, fi.Mode()); err != nil {
				s.skip(name, err.Error())
				return nil
			}
			count++
		}
		return nil
	})
	if count > 0 {
		s.report.copied = append(s.report.copied, fmt.Sprintf("%s/ (%d files)", rel, count))
	}
}

// mcpServers takes only the user-level mcpServers key from a .claude.json.
func (s *seeder) mcpServers(candidates []string) {
	for _, path := range candidates {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		data, err := readBounded(path, s.remaining)
		if err != nil {
			s.skip(".claude.json", err.Error())
			return
		}
		var file struct {
			MCPServers json.RawMessage `json:"mcpServers"`
		}
		if json.Unmarshal(data, &file) != nil {
			s.skip(".claude.json", "invalid JSON")
			return
		}
		var servers map[string]json.RawMessage
		if json.Unmarshal(file.MCPServers, &servers) != nil || len(servers) == 0 {
			return
		}
		out, err := json.MarshalIndent(map[string]json.RawMessage{"mcpServers": file.MCPServers}, "", "  ")
		if err != nil {
			return
		}
		if err := s.write(".claude.json", out, 0o600); err != nil {
			s.skip(".claude.json", err.Error())
			return
		}
		s.report.copied = append(s.report.copied, fmt.Sprintf("mcpServers (%d)", len(servers)))
		return
	}
}

// write stores data under dst with owner-only permissions, keeping only the
// owner's execute bit so skill scripts stay runnable.
func (s *seeder) write(rel string, data []byte, srcMode fs.FileMode) error {
	if int64(len(data)) > s.remaining {
		return fmt.Errorf("size limit reached")
	}
	target := filepath.Join(s.dst, rel)
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	mode := fs.FileMode(0o600)
	if srcMode&0o100 != 0 {
		mode = 0o700
	}
	if err := os.WriteFile(target, data, mode); err != nil {
		return err
	}
	if err := os.Chmod(target, mode); err != nil {
		return err
	}
	s.remaining -= int64(len(data))
	return nil
}

func readBounded(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("size limit reached")
	}
	return data, nil
}

// filterSettings drops credential helpers and Anthropic/cloud-provider
// variables from a settings.json so seeding cannot override the profile login.
func filterSettings(data []byte) ([]byte, error) {
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}
	for _, key := range settingsCredentialKeys {
		delete(settings, key)
	}
	if raw, ok := settings["env"]; ok {
		var env map[string]json.RawMessage
		if json.Unmarshal(raw, &env) == nil {
			for name := range env {
				upper := strings.ToUpper(name)
				if strings.HasPrefix(upper, "ANTHROPIC_") || strings.HasPrefix(upper, "CLAUDE_CODE_USE_") || upper == "CLAUDE_CODE_OAUTH_TOKEN" || strings.HasPrefix(upper, "AWS_") {
					delete(env, name)
				}
			}
			if filtered, err := json.Marshal(env); err == nil {
				settings["env"] = filtered
			}
		}
	}
	return json.MarshalIndent(settings, "", "  ")
}

// seedSource resolves the directory to seed from and where its MCP servers live.
func (p *Provider) seedSource(from string) (dir string, jsonCandidates []string, explicit bool, err error) {
	home, homeErr := p.hostHome()
	defaultDir := ""
	if homeErr == nil {
		defaultDir = filepath.Join(home, ".claude")
	}
	switch {
	case from != "":
		dir, explicit = from, true
	case p.hostEnv("CLAUDE_CONFIG_DIR") != "":
		dir = p.hostEnv("CLAUDE_CONFIG_DIR")
	case homeErr == nil:
		dir = defaultDir
	default:
		return "", nil, false, homeErr
	}
	if abs, absErr := filepath.Abs(dir); absErr == nil {
		dir = abs
	}
	if homeErr == nil && filepath.Clean(dir) == defaultDir {
		jsonCandidates = append(jsonCandidates, filepath.Join(home, ".claude.json"))
	}
	jsonCandidates = append(jsonCandidates, filepath.Join(dir, ".claude.json"))
	return dir, jsonCandidates, explicit, nil
}
