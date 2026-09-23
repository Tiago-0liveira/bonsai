package updater

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// State represents the cached update check status stored locally.
type State struct {
	LastCheckedAt int64  `json:"last_checked_at"`
	LatestVersion string `json:"latest_version"`
	ReleaseURL    string `json:"release_url"`
	DownloadURL   string `json:"download_url"`
}

var (
	stateMu           sync.RWMutex
	statePathOverride string
)

// StatePath returns the file path to the update state file.
func StatePath() (string, error) {
	stateMu.RLock()
	override := statePathOverride
	stateMu.RUnlock()
	if override != "" {
		return override, nil
	}

	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "bonsai", "update_state.json"), nil
}

// SetStatePathForTesting overrides the state file path during unit tests.
func SetStatePathForTesting(p string) {
	stateMu.Lock()
	statePathOverride = p
	stateMu.Unlock()
}

// ClearStatePathForTesting clears any active test override for the state file path.
func ClearStatePathForTesting() {
	stateMu.Lock()
	statePathOverride = ""
	stateMu.Unlock()
}

// LoadState reads the update state from the cache file.
// If the state file does not exist, it returns nil, nil.
func LoadState() (*State, error) {
	path, err := StatePath()
	if err != nil {
		return nil, err
	}

	legacyPath := filepath.Join(filepath.Dir(path), "update.json")

	// If legacy update.json exists and is newer than update_state.json (or update_state.json is missing):
	infoState, errState := os.Stat(path)
	infoLegacy, errLegacy := os.Stat(legacyPath)
	if errLegacy == nil {
		if errState != nil || infoLegacy.ModTime().After(infoState.ModTime()) {
			data, lErr := os.ReadFile(legacyPath)
			if lErr != nil {
				return nil, lErr
			}
			type legacyCache struct {
				Checked time.Time
				Release Release
			}
			var lc legacyCache
			if err := json.Unmarshal(data, &lc); err != nil {
				return nil, err
			}
			return &State{
				LastCheckedAt: lc.Checked.Unix(),
				LatestVersion: lc.Release.Tag,
				ReleaseURL:    lc.Release.HTMLURL,
			}, nil
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// SaveState atomically writes the update state to disk.
func SaveState(st *State) error {
	path, err := StatePath()
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp(dir, "update_state-*")
	if err != nil {
		return err
	}
	tmpName := tmpFile.Name()
	defer os.Remove(tmpName)

	if err := json.NewEncoder(tmpFile).Encode(st); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpName, path); err != nil {
		return err
	}

	// Also mirror to legacy update.json for backward compatibility
	legacyPath := filepath.Join(dir, "update.json")
	type legacyCache struct {
		Checked time.Time
		Release Release
	}
	lc := legacyCache{
		Checked: time.Unix(st.LastCheckedAt, 0),
		Release: Release{
			Tag:     st.LatestVersion,
			HTMLURL: st.ReleaseURL,
		},
	}
	if b, err := json.Marshal(lc); err == nil {
		_ = os.WriteFile(legacyPath, b, 0o600)
	}

	return nil
}

// ShouldCheckForUpdate returns true if the state file does not exist or
// if more than CheckIntervalSeconds has passed since the last check.
func ShouldCheckForUpdate() bool {
	st, err := LoadState()
	if err != nil || st == nil {
		return true
	}
	return ShouldCheckForUpdateAt(st.LastCheckedAt, time.Now().Unix())
}

// ShouldCheckForUpdateAt evaluates whether a check is due given a last checked timestamp and now.
func ShouldCheckForUpdateAt(lastCheckedAt, now int64) bool {
	if lastCheckedAt <= 0 {
		return true
	}
	diff := now - lastCheckedAt
	return diff >= CheckIntervalSeconds
}
