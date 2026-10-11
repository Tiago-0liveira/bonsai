package codex

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
)

// MinVersion is the oldest Codex CLI release verified to support every flag and
// command Bonsai uses (--no-daemon, -c cli_auth_credentials_store, `login
// status`, `login --device-auth`, `login --with-api-key`).
const MinVersion = "0.162.1"

type BinaryResolver interface {
	Resolve() (string, error)
}

type PathBinaryResolver struct{}

func (PathBinaryResolver) Resolve() (string, error) {
	path, err := exec.LookPath("codex")
	if err != nil {
		return "", fmt.Errorf("resolve codex: %w", err)
	}
	return path, nil
}

var versionRE = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

type semver [3]int

func parseVersion(s string) (semver, string, bool) {
	m := versionRE.FindStringSubmatch(s)
	if m == nil {
		return semver{}, "", false
	}
	var v semver
	for i := range v {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return semver{}, "", false
		}
		v[i] = n
	}
	return v, m[0], true
}

func (v semver) less(o semver) bool {
	for i := range v {
		if v[i] != o[i] {
			return v[i] < o[i]
		}
	}
	return false
}
