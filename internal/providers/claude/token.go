package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// tokenRE matches `claude setup-token` output: a fixed prefix and base64url body.
var tokenRE = regexp.MustCompile(`^sk-ant-oat01-[A-Za-z0-9_-]{16,400}$`)

// tokenLifetime is the documented validity of a setup-token token.
const tokenLifetime = 365 * 24 * time.Hour

type tokenFile struct {
	Version   int       `json:"version"`
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

func validToken(token string) bool { return tokenRE.MatchString(token) }

func readToken(path string) (tokenFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return tokenFile{}, err
	}
	var file tokenFile
	if err := json.Unmarshal(data, &file); err != nil || file.Version != 1 || !validToken(file.Token) {
		return tokenFile{}, fmt.Errorf("invalid token file")
	}
	return file, nil
}

func writeToken(path, token string, now time.Time) error {
	data, err := json.Marshal(tokenFile{Version: 1, Token: token, CreatedAt: now.UTC(), ExpiresAt: now.UTC().Add(tokenLifetime)})
	if err != nil {
		return err
	}
	return writePrivateFile(path, data)
}

// writePrivateFile writes data 0600 via a temp file and rename.
func writePrivateFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
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
