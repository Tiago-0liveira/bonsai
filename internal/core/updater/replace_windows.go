package updater

import (
	"fmt"
	"os"
)

// Windows permits renaming a running executable, but not overwriting it.
// Retain the old image until the next update, after the process has exited.
func swap(tmp, target string) error {
	backup := target + ".old"
	if err := os.Remove(backup); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(target, backup); err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		if rollback := os.Rename(backup, target); rollback != nil {
			return fmt.Errorf("install: %v; restore %s manually: %w", err, backup, rollback)
		}
		return err
	}
	return nil
}
