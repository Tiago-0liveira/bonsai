package pkgmgr

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// CacheEntry is one content-addressed discovery result.
type CacheEntry struct {
	SchemaVersion int       `json:"schema_version"`
	Fingerprint   string    `json:"fingerprint"`
	GeneratedAt   time.Time `json:"generated_at"`
	Project       Project   `json:"project"`
}

func cachePath(fingerprint string) (string, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "bonsai", "pkgmgr", "v1", fingerprint+".json"), nil
}

func loadCache(fingerprint string) (*Project, bool) {
	path, err := cachePath(fingerprint)
	if err != nil {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var entry CacheEntry
	if json.Unmarshal(data, &entry) != nil || entry.SchemaVersion != discoverySchemaVersion || entry.Fingerprint != fingerprint || entry.Project.Fingerprint != fingerprint {
		return nil, false
	}
	return &entry.Project, true
}

func storeCache(project Project) error {
	path, err := cachePath(project.Fingerprint)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	entry := CacheEntry{SchemaVersion: discoverySchemaVersion, Fingerprint: project.Fingerprint, GeneratedAt: time.Now().UTC(), Project: project}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".pkgmgr-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
