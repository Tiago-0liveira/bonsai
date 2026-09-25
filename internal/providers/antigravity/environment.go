package antigravity

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func buildEnvironment(account agents.Account, session agents.Session) map[string]string {
	env := map[string]string{
		"HOME":                      session.HomeDir,
		"GEMINI_FORCE_FILE_STORAGE": "true",
		"BONSAI_AGENT_PROVIDER":     string(ProviderID),
		"BONSAI_AGENT_ACCOUNT_ID":   string(account.ID),
		"BONSAI_AGENT_SESSION_ID":   string(session.ID),
	}
	hostHome, _ := os.UserHomeDir()
	if hostHome != "" {
		if _, ok := os.LookupEnv("GIT_CONFIG_GLOBAL"); !ok {
			gitConfig := filepath.Join(hostHome, ".gitconfig")
			if _, err := os.Stat(gitConfig); err == nil {
				env["GIT_CONFIG_GLOBAL"] = gitConfig
			}
		}
		if _, ok := os.LookupEnv("GH_CONFIG_DIR"); !ok {
			ghConfig := filepath.Join(hostHome, ".config", "gh")
			if _, err := os.Stat(ghConfig); err == nil {
				env["GH_CONFIG_DIR"] = ghConfig
			}
		}
	}
	if runtime.GOOS == "windows" {
		env["USERPROFILE"] = session.HomeDir
		env["LOCALAPPDATA"] = filepath.Join(session.HomeDir, "AppData", "Local")
		env["APPDATA"] = filepath.Join(session.HomeDir, "AppData", "Roaming")
		_ = os.MkdirAll(env["LOCALAPPDATA"], 0o700)
		_ = os.MkdirAll(env["APPDATA"], 0o700)

		volume := filepath.VolumeName(session.HomeDir)
		if volume != "" {
			env["HOMEDRIVE"] = volume
			rest := strings.TrimPrefix(session.HomeDir, volume)
			if rest == "" {
				rest = string(filepath.Separator)
			}
			env["HOMEPATH"] = rest
		}
	}
	for k, v := range platformCredentialEnv(session) {
		env[k] = v
	}
	return env
}
