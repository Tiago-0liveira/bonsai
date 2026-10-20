package codex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// Codex skips hooks it has not been told to trust ("Hooks need review"). The
// trust lives in the profile's config.toml as
// hooks.state."<key>".trusted_hash, where key and hash are Codex's own
// (`hooks/list` reports both). Bonsai trusts exactly the handlers it wrote and
// nothing else: another hook in the same file stays as untrusted as the user
// left it. Everything goes through the app-server's published methods, so the
// formats stay Codex's business, and nothing here uses
// --dangerously-bypass-hook-trust, which would run untrusted hooks too.

// hookSetupTimeout bounds the whole install-and-trust step of a launch, lock
// wait included.
const hookSetupTimeout = 30 * time.Second

const trustedStatus = "trusted"

// hooksLockPath is the lock concurrent sessions of one account take around the
// hooks file and its trust. It is not login.lock: a re-login holds that for as
// long as the login takes, and launches must not wait for it.
func hooksLockPath(accounts agents.AccountStore, account agents.Account) string {
	return filepath.Join(accounts.AccountDir(account.ID), "hooks.lock")
}

// trustRecordPath keeps what was last trusted, next to the profile and out of
// CODEX_HOME, which Codex owns.
func trustRecordPath(accounts agents.AccountStore, account agents.Account) string {
	return filepath.Join(accounts.AccountDir(account.ID), "hook-trust.json")
}

// trustRecord says which hooks Bonsai trusted and under which conditions, so
// a launch can tell without starting the app-server that nothing changed.
type trustRecord struct {
	// Fingerprint covers what changes the hashes Codex computes: the hooks
	// file's content and the Codex binary.
	Fingerprint string `json:"fingerprint"`
	// Hashes maps a hook key to the hash that was trusted.
	Hashes map[string]string `json:"hashes"`
}

// installActivityHooks makes the profile run Bonsai's hooks: it upserts them
// into the profile's hooks.json and trusts them. The two steps share one lock
// because concurrent sessions of an account share the file. Any failure is the
// caller's to ignore: the session then reports activity from its terminal only.
func (p *Provider) installActivityHooks(ctx context.Context, account agents.Account, request *agents.ActivityRequest, executable string) error {
	ctx, cancel := context.WithTimeout(ctx, hookSetupTimeout)
	defer cancel()
	lock, err := agents.LockFileContext(ctx, hooksLockPath(p.accounts, account))
	if err != nil {
		return err
	}
	defer lock.Unlock()
	if err := writeActivityHooks(homeDir(p.accounts, account), request); err != nil {
		return err
	}
	return p.ensureHookTrust(ctx, account, executable)
}

// ensureHookTrust trusts the profile's Bonsai hooks. It is a few file reads
// when nothing changed since the last time, and runs the app-server only after
// the hooks file or the Codex binary did. The caller holds the hooks lock.
func (p *Provider) ensureHookTrust(ctx context.Context, account agents.Account, executable string) error {
	home := homeDir(p.accounts, account)
	fingerprint, err := trustFingerprint(home, executable)
	if err != nil {
		return err
	}
	recordPath := trustRecordPath(p.accounts, account)
	if record := readTrustRecord(recordPath); record.Fingerprint == fingerprint && configTrusts(home, record.Hashes) {
		return nil
	}
	hashes := map[string]string{}
	err = p.withAppServer(ctx, account, func(ctx context.Context, conn *rpcConn) error {
		result, err := conn.call(ctx, "hooks/list", map[string]any{})
		if err != nil {
			return err
		}
		listed, err := bonsaiHooks(result, hooksPath(home))
		if err != nil {
			return err
		}
		if len(listed) == 0 {
			return errors.New("codex lists none of Bonsai's hooks")
		}
		edit := map[string]map[string]string{}
		for _, hook := range listed {
			hashes[hook.Key] = hook.CurrentHash
			if hook.TrustStatus != trustedStatus {
				edit[hook.Key] = map[string]string{"trusted_hash": hook.CurrentHash}
			}
		}
		if len(edit) == 0 {
			return nil
		}
		// An explicit file keeps the write in this profile, whatever the
		// app-server would default to.
		_, err = conn.call(ctx, "config/batchWrite", map[string]any{
			"filePath": configPath(home),
			"edits":    []map[string]any{{"keyPath": "hooks.state", "mergeStrategy": "upsert", "value": edit}},
		})
		return err
	})
	if err != nil {
		return err
	}
	data, err := json.Marshal(trustRecord{Fingerprint: fingerprint, Hashes: hashes})
	if err != nil {
		return err
	}
	return writePrivateFile(recordPath, data)
}

type listedHook struct {
	Key         string `json:"key"`
	CurrentHash string `json:"currentHash"`
	TrustStatus string `json:"trustStatus"`
	EventName   string `json:"eventName"`
	HandlerType string `json:"handlerType"`
	Command     string `json:"command"`
	SourcePath  string `json:"sourcePath"`
	IsManaged   bool   `json:"isManaged"`
}

// bonsaiHooks picks Bonsai's command hooks out of a `hooks/list` reply: those
// that come from the profile's hooks file (not a project's or a plugin's
// namesake) and run Bonsai's hook command. A hook without a key or hash is
// ignored, since there is nothing to trust it by.
func bonsaiHooks(result json.RawMessage, hooksFile string) ([]listedHook, error) {
	var reply struct {
		Data []struct {
			Hooks []listedHook `json:"hooks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(result, &reply); err != nil {
		return nil, fmt.Errorf("codex hooks/list: unreadable reply")
	}
	want := resolvedPath(hooksFile)
	seen := map[string]bool{}
	var out []listedHook
	for _, entry := range reply.Data {
		for _, hook := range entry.Hooks {
			if hook.IsManaged || hook.HandlerType != "command" || !isBonsaiCommand(hook.Command) ||
				hook.Key == "" || hook.CurrentHash == "" || seen[hook.Key] || resolvedPath(hook.SourcePath) != want {
				continue
			}
			seen[hook.Key] = true
			out = append(out, hook)
		}
	}
	return out, nil
}

// resolvedPath compares paths the way the filesystem does: Codex may report
// the profile through a symlinked prefix.
func resolvedPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

// trustFingerprint identifies the inputs of Codex's hook hashes: the hooks
// file and the binary (its path, size and modification time).
func trustFingerprint(home, executable string) (string, error) {
	data, err := os.ReadFile(hooksPath(home))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(executable)
	if err != nil {
		return "", err
	}
	sum := sha256.New()
	sum.Write(data)
	fmt.Fprintf(sum, "\x00%s\x00%d\x00%d", executable, info.Size(), info.ModTime().UnixNano())
	return hex.EncodeToString(sum.Sum(nil)), nil
}

// readTrustRecord returns the zero record when the file is missing or unreadable:
// that only means trusting again.
func readTrustRecord(path string) trustRecord {
	var record trustRecord
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &record)
	}
	return record
}

// configTrusts reports whether the profile's config.toml still holds every
// hash in want, for example not rewritten by a seed or by hand since.
func configTrusts(home string, want map[string]string) bool {
	if len(want) == 0 {
		return false
	}
	data, err := os.ReadFile(configPath(home))
	if err != nil {
		return false
	}
	var config struct {
		Hooks struct {
			State map[string]struct {
				TrustedHash string `toml:"trusted_hash"`
			} `toml:"state"`
		} `toml:"hooks"`
	}
	if toml.Unmarshal(data, &config) != nil {
		return false
	}
	for key, hash := range want {
		if strings.TrimSpace(config.Hooks.State[key].TrustedHash) != hash {
			return false
		}
	}
	return true
}
