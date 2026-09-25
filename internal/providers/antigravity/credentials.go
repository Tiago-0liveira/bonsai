package antigravity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

type CredentialManager interface {
	Materialize(context.Context, agents.Account, agents.Session) error
	Reconcile(context.Context, agents.Account, agents.Session) error
	CaptureSetup(context.Context, agents.Account, agents.Session) error
}

type credentialVault struct {
	Version          int             `json:"version"`
	Identity         string          `json:"identity"`
	Generation       uint64          `json:"generation"`
	UpdatedAt        time.Time       `json:"updated_at"`
	OAuth            json.RawMessage `json:"oauth"`
	ProviderSettings json.RawMessage `json:"provider_settings,omitempty"`
}

type generationMarker struct {
	Generation uint64 `json:"generation"`
}

type fileCredentialManager struct {
	accounts agents.AccountStore
}

func NewCredentialManager(accounts agents.AccountStore) CredentialManager {
	return &fileCredentialManager{accounts: accounts}
}

func (m *fileCredentialManager) Materialize(_ context.Context, account agents.Account, session agents.Session) error {
	lock, err := agents.LockFile(credentialLockPath(m.accounts, account))
	if err != nil {
		return err
	}
	defer lock.Unlock()

	vault, err := m.readVault(account)
	if err != nil {
		return err
	}
	if len(vault.OAuth) == 0 {
		return fmt.Errorf("%w: antigravity credentials are incomplete", agents.ErrNotAuthenticated)
	}
	if err := writePrivateFile(oauthPath(session.HomeDir), vault.OAuth); err != nil {
		return err
	}
	if len(vault.ProviderSettings) > 0 {
		if err := writePrivateFile(providerSettingsPath(session.HomeDir), vault.ProviderSettings); err != nil {
			return err
		}
	}
	return writePrivateJSON(generationPath(session), generationMarker{Generation: vault.Generation})
}

func (m *fileCredentialManager) CaptureSetup(_ context.Context, account agents.Account, session agents.Session) error {
	lock, err := agents.LockFile(credentialLockPath(m.accounts, account))
	if err != nil {
		return err
	}
	defer lock.Unlock()

	candidate, err := readSessionCredentialState(session.HomeDir)
	if err != nil {
		return err
	}
	if candidate.Identity == "" {
		candidate.Identity = fallbackIdentity(account)
	}
	candidate.Version = 1
	candidate.Generation = 1
	candidate.UpdatedAt = time.Now().UTC()
	return m.writeVault(account, candidate)
}

func (m *fileCredentialManager) Reconcile(_ context.Context, account agents.Account, session agents.Session) error {
	lock, err := agents.LockFile(credentialLockPath(m.accounts, account))
	if err != nil {
		return err
	}
	defer lock.Unlock()

	current, err := m.readVault(account)
	if err != nil {
		return err
	}
	candidate, err := readSessionCredentialState(session.HomeDir)
	if err != nil {
		if errors.Is(err, agents.ErrNotAuthenticated) {
			return nil
		}
		return err
	}
	if candidate.Identity == "" {
		candidate.Identity = current.Identity
	} else if !isFallbackIdentity(current.Identity, account) && candidate.Identity != current.Identity {
		return fmt.Errorf("antigravity credential identity mismatch")
	}

	var marker generationMarker
	if data, err := os.ReadFile(generationPath(session)); err == nil {
		_ = json.Unmarshal(data, &marker)
	}

	candidate.OAuth = mergeOAuth(current.OAuth, candidate.OAuth)
	if len(candidate.ProviderSettings) == 0 {
		candidate.ProviderSettings = append(json.RawMessage(nil), current.ProviderSettings...)
	}
	if credentialsEquivalent(current, candidate) {
		return nil
	}
	if marker.Generation > 0 && current.Generation > marker.Generation {
		if tokenFreshness(candidate.OAuth).Compare(tokenFreshness(current.OAuth)) <= 0 {
			return nil
		}
	}
	candidate.Version = 1
	if candidate.Identity == "" {
		candidate.Identity = fallbackIdentity(account)
	}
	candidate.Generation = current.Generation + 1
	candidate.UpdatedAt = time.Now().UTC()
	return m.writeVault(account, candidate)
}

func fallbackIdentity(account agents.Account) string {
	return "account:" + string(account.ID)
}

func isFallbackIdentity(identity string, account agents.Account) bool {
	return identity == "" || identity == fallbackIdentity(account)
}

func (m *fileCredentialManager) readVault(account agents.Account) (credentialVault, error) {
	data, err := os.ReadFile(vaultPath(m.accounts, account))
	if errors.Is(err, os.ErrNotExist) {
		return credentialVault{}, fmt.Errorf("%w: antigravity credential vault missing", agents.ErrNotAuthenticated)
	}
	if err != nil {
		return credentialVault{}, err
	}
	var vault credentialVault
	if err := json.Unmarshal(data, &vault); err != nil {
		return credentialVault{}, fmt.Errorf("read antigravity credential vault: %w", err)
	}
	if vault.Version != 1 {
		return credentialVault{}, fmt.Errorf("unsupported antigravity credential vault version %d", vault.Version)
	}
	return vault, nil
}

func (m *fileCredentialManager) writeVault(account agents.Account, vault credentialVault) error {
	if err := os.MkdirAll(m.accounts.CredentialDir(account.ID), 0o700); err != nil {
		return err
	}
	return writePrivateJSON(vaultPath(m.accounts, account), vault)
}

func readSessionCredentialState(home string) (credentialVault, error) {
	oauth, err := os.ReadFile(oauthPath(home))
	if errors.Is(err, os.ErrNotExist) {
		return credentialVault{}, fmt.Errorf("%w: antigravity credential file missing at %s", agents.ErrNotAuthenticated, oauthPath(home))
	}
	if err != nil {
		return credentialVault{}, err
	}
	if !json.Valid(oauth) {
		return credentialVault{}, fmt.Errorf("malformed antigravity oauth state")
	}
	settings, err := readOptional(providerSettingsPath(home))
	if err != nil {
		return credentialVault{}, err
	}
	if len(settings) > 0 && !json.Valid(settings) {
		return credentialVault{}, fmt.Errorf("malformed antigravity provider settings")
	}
	return credentialVault{
		Version:          1,
		Identity:         identityFromOAuth(oauth),
		OAuth:            append(json.RawMessage(nil), oauth...),
		ProviderSettings: append(json.RawMessage(nil), settings...),
	}, nil
}

func readOptional(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return data, err
}

func identityFromOAuth(data []byte) string {
	var root any
	if json.Unmarshal(data, &root) != nil {
		return ""
	}
	return identityFromJSON(root)
}

func identityFromJSON(v any) string {
	switch x := v.(type) {
	case map[string]any:
		for _, key := range []string{"email", "account", "user"} {
			if s, ok := x[key].(string); ok && strings.Contains(s, "@") {
				return strings.TrimSpace(s)
			}
		}
		for _, key := range []string{"id_token", "idToken"} {
			if token, ok := x[key].(string); ok {
				if email := jwtStringClaim(token, "email"); email != "" {
					return email
				}
				if sub := jwtStringClaim(token, "sub"); sub != "" {
					return "sub:" + sub
				}
			}
		}
		for _, child := range x {
			if identity := identityFromJSON(child); identity != "" {
				return identity
			}
		}
	case []any:
		for _, child := range x {
			if identity := identityFromJSON(child); identity != "" {
				return identity
			}
		}
	}
	return ""
}

func jwtStringClaim(token, claim string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var v map[string]any
	if json.Unmarshal(payload, &v) != nil {
		return ""
	}
	s, _ := v[claim].(string)
	return strings.TrimSpace(s)
}

func tokenFreshness(data []byte) time.Time {
	var root any
	if json.Unmarshal(data, &root) != nil {
		return time.Time{}
	}
	return freshnessFromJSON(root)
}

func freshnessFromJSON(v any) time.Time {
	switch x := v.(type) {
	case map[string]any:
		for _, key := range []string{"expiry_date", "expiryDate", "expires_at", "expiresAt", "expiry"} {
			if t := parseTimeValue(x[key]); !t.IsZero() {
				return t
			}
		}
		for _, key := range []string{"id_token", "idToken", "access_token", "accessToken"} {
			if token, ok := x[key].(string); ok {
				if exp := jwtNumberClaim(token, "exp"); exp > 0 {
					return time.Unix(exp, 0).UTC()
				}
			}
		}
		for _, child := range x {
			if t := freshnessFromJSON(child); !t.IsZero() {
				return t
			}
		}
	case []any:
		for _, child := range x {
			if t := freshnessFromJSON(child); !t.IsZero() {
				return t
			}
		}
	}
	return time.Time{}
}

func jwtNumberClaim(token, claim string) int64 {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return 0
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0
	}
	var v map[string]any
	if json.Unmarshal(payload, &v) != nil {
		return 0
	}
	switch n := v[claim].(type) {
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}

func parseTimeValue(v any) time.Time {
	switch x := v.(type) {
	case string:
		if t, err := time.Parse(time.RFC3339, x); err == nil {
			return t
		}
		if n, err := strconv.ParseInt(x, 10, 64); err == nil {
			return unixTime(n)
		}
	case float64:
		return unixTime(int64(x))
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return unixTime(n)
		}
	}
	return time.Time{}
}

func unixTime(n int64) time.Time {
	if n > 10_000_000_000 {
		n /= 1000
	}
	if n <= 0 {
		return time.Time{}
	}
	return time.Unix(n, 0).UTC()
}

func mergeOAuth(current, candidate []byte) json.RawMessage {
	if len(candidate) == 0 {
		return append(json.RawMessage(nil), current...)
	}
	var cur, next map[string]any
	if json.Unmarshal(current, &cur) != nil || json.Unmarshal(candidate, &next) != nil {
		return append(json.RawMessage(nil), candidate...)
	}
	mergeRefreshTokens(cur, next)
	out, err := json.Marshal(next)
	if err != nil {
		return append(json.RawMessage(nil), candidate...)
	}
	return out
}

func mergeRefreshTokens(current, candidate map[string]any) {
	for _, key := range []string{"refresh_token", "refreshToken"} {
		if _, ok := candidate[key]; !ok {
			if v, ok := current[key]; ok {
				candidate[key] = v
			}
		}
	}
	curToken, curOK := current["token"].(map[string]any)
	nextToken, nextOK := candidate["token"].(map[string]any)
	if curOK && nextOK {
		mergeRefreshTokens(curToken, nextToken)
	}
}

func credentialsEquivalent(a, b credentialVault) bool {
	return string(a.OAuth) == string(b.OAuth) &&
		string(a.ProviderSettings) == string(b.ProviderSettings)
}

func writePrivateFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func writePrivateJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateFile(path, append(data, '\n'))
}
