package antigravity

import (
	"fmt"
	"os/exec"
)

type BinaryResolver interface {
	Resolve() (string, error)
}

type PathBinaryResolver struct{}

func (PathBinaryResolver) Resolve() (string, error) {
	path, err := exec.LookPath("agy")
	if err != nil {
		return "", fmt.Errorf("resolve agy: %w", err)
	}
	return path, nil
}
