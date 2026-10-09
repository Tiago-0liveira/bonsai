package claude

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
)

// MinVersion is the oldest Claude Code release verified to support every flag
// and auth command Bonsai uses (--session-id, --permission-mode, --effort,
// `auth status`).
const MinVersion = "2.1.295"

type BinaryResolver interface {
	Resolve() (string, error)
}

type PathBinaryResolver struct{}

func (PathBinaryResolver) Resolve() (string, error) {
	path, err := exec.LookPath("claude")
	if err != nil {
		return "", fmt.Errorf("resolve claude: %w", err)
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
