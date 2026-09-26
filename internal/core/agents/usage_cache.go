package agents

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type UsageCache interface {
	Get(ProviderID, AccountID) (UsageSnapshot, bool, error)
	Put(UsageSnapshot) error
	RemoveAccount(ProviderID, AccountID) error
}

type FileUsageCache struct {
	root string
}

func NewFileUsageCache(root string) (*FileUsageCache, error) {
	if root == "" {
		var err error
		root, err = DefaultUsageCacheRoot()
		if err != nil {
			return nil, err
		}
	}
	c := &FileUsageCache{root: filepath.Clean(root)}
	if err := ensurePrivateDir(c.root); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *FileUsageCache) path(provider ProviderID, account AccountID) (string, error) {
	if !validOpaqueID(string(provider)) || !validOpaqueID(string(account)) {
		return "", fmt.Errorf("invalid usage cache key")
	}
	return filepath.Join(c.root, string(provider), string(account)+".json"), nil
}

func (c *FileUsageCache) Get(provider ProviderID, account AccountID) (UsageSnapshot, bool, error) {
	path, err := c.path(provider, account)
	if err != nil {
		return UsageSnapshot{}, false, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return UsageSnapshot{}, false, nil
	}
	if err != nil {
		return UsageSnapshot{}, false, err
	}
	var snap UsageSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return UsageSnapshot{}, false, err
	}
	return snap, true, nil
}

func (c *FileUsageCache) Put(snapshot UsageSnapshot) error {
	path, err := c.path(snapshot.Provider, snapshot.AccountID)
	if err != nil {
		return err
	}
	return atomicWriteJSON(path, snapshot)
}

func (c *FileUsageCache) RemoveAccount(provider ProviderID, account AccountID) error {
	path, err := c.path(provider, account)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
