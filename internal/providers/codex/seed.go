package codex

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// maxSeedBytes caps the total size copied into a new profile.
const maxSeedBytes = 20 << 20

// Seeding is a one-time, allowlisted snapshot of the user's own Codex setup.
// auth.json, sessions, history, SQLite state, logs, caches, plugins, packages
// and app-server state are never copied: only these entries are read at all.
var (
	seedFiles = []string{"AGENTS.md"}
	seedDirs  = []string{"prompts", "skills", "rules"}
	// seedSkipDirs are maintained by Codex itself, which installs them again.
	seedSkipDirs = []string{filepath.Join("skills", ".system")}
	// seedDeniedNames are never copied from inside a seeded directory: they are
	// credentials or another provider's config.
	seedDeniedNames = []string{"auth.json", "*.config.toml", "*.key*", "*.gpg", "*.pem", "*.sqlite*", "history.jsonl"}
)

// configDroppedKeys are removed from a seeded config.toml. The store and login
// pins would block the profile's own login; profiles and projects are host
// paths and trust decisions that belong to the host's own home.
var configDroppedKeys = []string{"cli_auth_credentials_store", "forced_login_method", "forced_chatgpt_workspace_id", "profile", "profiles", "projects"}

// providerCredentialKeys mark a model_providers entry that reads or carries a
// credential; such entries are dropped with any model_provider selecting them.
var providerCredentialKeys = []string{"env_key", "experimental_bearer_token", "bearer_token", "http_headers", "env_http_headers", "auth"}

type seedReport struct {
	copied  []string
	skipped []string
}

type seeder struct {
	dst       string
	remaining int64
	report    seedReport
}

// seed copies the user's own Codex setup into the new profile and reports it.
func (p *Provider) seed(out io.Writer, from, dst string) (string, error) {
	src, explicit, err := p.seedSource(from)
	if err != nil {
		return "", err
	}
	if _, statErr := os.Stat(src); statErr != nil {
		if explicit {
			return "", fmt.Errorf("seed source: %w", statErr)
		}
		fmt.Fprintf(out, "nothing to seed: %s does not exist\n", src)
		return "", nil
	}
	report, err := seedProfile(src, dst, maxSeedBytes)
	if err != nil {
		return "", fmt.Errorf("seed profile: %w", err)
	}
	if len(report.copied) == 0 {
		fmt.Fprintf(out, "nothing to seed from %s\n", src)
	} else {
		fmt.Fprintf(out, "seeded profile from %s: %s\n", src, strings.Join(report.copied, ", "))
	}
	for _, skipped := range report.skipped {
		fmt.Fprintf(out, "  skipped %s\n", skipped)
	}
	return src, nil
}

// seedSource resolves the directory to seed from: --seed-from, else the host's
// CODEX_HOME, else ~/.codex.
func (p *Provider) seedSource(from string) (dir string, explicit bool, err error) {
	switch {
	case from != "":
		dir, explicit = from, true
	case p.hostEnv("CODEX_HOME") != "":
		dir = p.hostEnv("CODEX_HOME")
	default:
		home, err := p.hostHome()
		if err != nil {
			return "", false, err
		}
		dir = filepath.Join(home, ".codex")
	}
	if abs, absErr := filepath.Abs(dir); absErr == nil {
		dir = abs
	}
	return dir, explicit, nil
}

// seedProfile copies the allowlisted items from srcDir into dst.
func seedProfile(srcDir, dst string, limit int64) (seedReport, error) {
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
	s.config(filepath.Join(resolved, "config.toml"))
	for _, name := range seedFiles {
		s.file(filepath.Join(resolved, name), name)
	}
	for _, name := range seedDirs {
		s.dir(resolved, name)
	}
	return s.report, nil
}

func (s *seeder) skip(name, why string) {
	s.report.skipped = append(s.report.skipped, name+" ("+why+")")
}

func (s *seeder) config(src string) {
	const rel = "config.toml"
	info, err := os.Lstat(src)
	if err != nil {
		return
	}
	if !info.Mode().IsRegular() {
		s.skip(rel, "not a regular file")
		return
	}
	data, err := readBounded(src, s.remaining)
	if err != nil {
		s.skip(rel, err.Error())
		return
	}
	filtered, dropped, err := filterConfig(data)
	if err != nil {
		s.skip(rel, "invalid TOML")
		return
	}
	for _, key := range dropped {
		s.skip(rel+" "+key, "credential or host-specific")
	}
	if err := s.write(rel, filtered, 0o600); err != nil {
		s.skip(rel, err.Error())
		return
	}
	s.report.copied = append(s.report.copied, rel)
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
	data, err := readBounded(src, s.remaining)
	if err != nil {
		s.skip(rel, err.Error())
		return
	}
	if err := s.write(rel, data, info.Mode()); err != nil {
		s.skip(rel, err.Error())
		return
	}
	s.report.copied = append(s.report.copied, rel)
}

func (s *seeder) dir(root, rel string) {
	src := filepath.Join(root, rel)
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
		name, _ := filepath.Rel(root, path)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			s.skip(name, "symlink")
		case d.IsDir():
			for _, managed := range seedSkipDirs {
				if name == managed {
					return filepath.SkipDir
				}
			}
		case !d.Type().IsRegular():
			s.skip(name, "not a regular file")
		case deniedName(d.Name()):
			s.skip(name, "credential file")
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

func deniedName(name string) bool {
	lower := strings.ToLower(name)
	for _, pattern := range seedDeniedNames {
		if ok, _ := filepath.Match(pattern, lower); ok {
			return true
		}
	}
	return false
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

// filterConfig removes login pins, host-specific tables and credential-bearing
// model providers from a config.toml. It returns the dropped keys, sorted.
// Comments and key order are not preserved.
func filterConfig(data []byte) ([]byte, []string, error) {
	var config map[string]any
	if err := toml.Unmarshal(data, &config); err != nil {
		return nil, nil, err
	}
	if config == nil {
		config = map[string]any{}
	}
	var dropped []string
	for _, key := range configDroppedKeys {
		if _, ok := config[key]; ok {
			delete(config, key)
			dropped = append(dropped, key)
		}
	}
	if providers, ok := config["model_providers"].(map[string]any); ok {
		for name, entry := range providers {
			table, _ := entry.(map[string]any)
			for _, key := range providerCredentialKeys {
				if _, has := table[key]; has || table == nil {
					delete(providers, name)
					dropped = append(dropped, "model_providers."+name)
					if config["model_provider"] == name {
						delete(config, "model_provider")
						dropped = append(dropped, "model_provider")
					}
					break
				}
			}
		}
		if len(providers) == 0 {
			delete(config, "model_providers")
		}
	}
	sort.Strings(dropped)
	out, err := toml.Marshal(config)
	return out, dropped, err
}

// ensureFileStore sets cli_auth_credentials_store = "file" in the profile's
// config.toml, keeping every other key. Codex then keeps the login in the
// profile's auth.json on every OS, never in a keyring entry shared by profiles.
func ensureFileStore(home string) error {
	config := map[string]any{}
	data, err := os.ReadFile(configPath(home))
	switch {
	case err == nil:
		if err := toml.Unmarshal(data, &config); err != nil {
			return fmt.Errorf("config.toml: %w", err)
		}
		if config == nil {
			config = map[string]any{}
		}
	case !os.IsNotExist(err):
		return err
	}
	config["cli_auth_credentials_store"] = "file"
	out, err := toml.Marshal(config)
	if err != nil {
		return err
	}
	return writePrivateFile(configPath(home), out)
}

// writePrivateFile writes data 0600 via a temp file and rename.
func writePrivateFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	cleanup := func() { _ = os.Remove(name) }
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(name, path); err != nil {
		cleanup()
		return err
	}
	return nil
}
