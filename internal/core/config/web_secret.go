package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

// The live-updates webhook secret is the one secret `bonsai web` owns. It is
// 32 random bytes written as 64 hex characters in its own 0600 file, never in
// web.json, argv, logs or the browser. GitHub signs deliveries with the secret
// string exactly as configured on the hook, so the HMAC key is the hex text
// itself, not the bytes it encodes.
const webWebhookSecretBytes = 32

// WebWebhookSecretPath is <user config dir>/bonsai/web-webhook-secret.
func WebWebhookSecretPath() (string, error) {
	dir, err := webConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "web-webhook-secret"), nil
}

// ReadWebWebhookSecret returns the HMAC key stored at path.
func ReadWebWebhookSecret(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	secret := strings.TrimSpace(string(raw))
	if decoded, err := hex.DecodeString(secret); err != nil || len(decoded) != webWebhookSecretBytes {
		return nil, fmt.Errorf("invalid webhook secret in %s; delete it and run bonsai web again to generate a new one", path)
	}
	return []byte(secret), nil
}

// EnsureWebWebhookSecret returns the secret at path, generating it first when
// the file does not exist.
func EnsureWebWebhookSecret(path string) ([]byte, error) {
	var out []byte
	err := withWebLock(path, func() error {
		secret, err := ReadWebWebhookSecret(path)
		if err == nil {
			out = secret
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		out, err = writeWebWebhookSecret(path)
		return err
	})
	return out, err
}

// RotateWebWebhookSecret replaces the secret at path with a new one. Hooks
// signed with the old secret fail verification until they are updated.
func RotateWebWebhookSecret(path string) ([]byte, error) {
	var out []byte
	err := withWebLock(path, func() error {
		var err error
		out, err = writeWebWebhookSecret(path)
		return err
	})
	return out, err
}

func writeWebWebhookSecret(path string) ([]byte, error) {
	raw := make([]byte, webWebhookSecretBytes)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	secret := []byte(hex.EncodeToString(raw))
	// WriteJSON is the atomic 0600 writer; the content need not be JSON.
	if err := gitstore.WriteJSON(path, append(append([]byte(nil), secret...), '\n')); err != nil {
		return nil, err
	}
	return secret, nil
}
