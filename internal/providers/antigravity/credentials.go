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
	Version    int             `json:"version"`
	Identity   string          `json:"identity"`
	Generation uint64          `json:"generation"`
	UpdatedAt  time.Time       `json:"updated_at"`
	OAuth      json.RawMessage `json:"oauth"`
	Accounts   json.RawMessage `json:"accounts,omitempty"`
	UserID     string          `json:"user_id,omitempty"`
}

type generationMarker struct {
	Generation uint64 `json:"generation"`
	Identity   string `json:"identity"`
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
	if vault.Identity == "" || len(vault.OAuth) == 0 {
		return fmt.Errorf("%w: antigravity credentials are incomplete", agents.ErrNotAuthenticated)
	}
	if err := writePrivateFile(oauthPath(session.HomeDir), vault.OAuth); err != nil {
		return err
	}
	if len(vault.Accounts) > 0 {
		if err := writePrivateFile(accountsPath(session.HomeDir), vault.Accounts); err != nil {
			return err
		}
	}
	if vault.UserID != "" {
		if err := writePrivateFile(userIDPath(session.HomeDir), []byte(vault.UserID)); err != nil {
			return err
		}
	}
	return writePrivateJSON(generationPath(session), generationMarker{
		Generation: vault.Generation,
		Identity: vault.Identity,
	})
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
		return fmt.Errorf("%w: unable to identify authenticated antigravity account", agents.ErrNotAuthenticated)
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
	if candidate.Identity == "" || current.Identity == "" || candidate.Identity != current.Identity {
		return fmt.Errorf("antigravity credential identity mismatch")
	}

	var marker generationMarker
	if data, err := os.ReadFile(generationPath(session)); err == nil {
		_ = json.Unmarshal(data, &marker)
	}
	if marker.Identity != "" && marker.Identity != current.Identity {
		return fmt.Errorf("antigravity credential generation identity mismatch")
	}

	candidate.OAuth = mergeOAuth(current.OAuth, candidate.OAuth)
	if credentialsEquivalent(current, candidate) {
		return nil
	}
	if marker.Generation > 0 && current.Generation > marker.Generation {
		if tokenFreshness(candidate.OAuth).Compare(tokenFreshness(current.OAuth)) <= 0 {
			return nil
		}
	}
	candidate.Version = 1
	candidate.Identity = current.Identity
	candidate.Generation = current.Generation + 1
	candidate.UpdatedAt = time.Now().UTC()
	return m.writeVault(account, candidate)
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
		return credentialVault{}, fmt.Errorf("%w: antigravity credential file missing", agents.ErrNotAuthenticated)
	}
	if err != nil {
		return credentialVault{}, err
	}
	if !json.Valid(oauth) {
		return credentialVault{}, fmt.Errorf("malformed antigravity oauth state")
	}
	accounts, err := readOptional(accountsPath(home))
	if err != nil {
		return credentialVault{}, err
	}
	if len(accounts) > 0 && !json.Valid(accounts) {
		return credentialVault{}, fmt.Errorf("malformed antigravity account state")
	}
	userIDBytes, err := readOptional(userIDPath(home))
	if err != nil {
		return credentialVault{}, err
	}
	userID := strings.TrimSpace(string(userIDBytes))
	identity := identityFromAccounts(accounts)
	if identity == "" {
		identity = identityFromOAuth(oauth)
	}
	if identity == "" && userID != "" {
		identity = "user_id:" + userID
	}
	return credentialVault{
		Version: 1, Identity: identity,
		OAuth: append(json.RawMessage(nil), oauth...),
		Accounts: append(json.RawMessage(nil), accounts...),
		UserID: userID,
	}, nil
}

func readOptional(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return data, err
}

func identityFromAccounts(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	var v map[string]any
	if json.Unmarshal(data, &v) != nil {
		return ""
	}
	for _, key := range []string{"active", "active_account", "activeAccount", "email"} {
		if s, ok := v[key].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func identityFromOAuth(data []byte) string {
	var v map[string]any
	if json.Unmarshal(data, &v) != nil {
		return ""
	}
	for _, key := range []string{"email", "account", "user"} {
		if s, ok := v[key].(string); ok && strings.Contains(s, "@") {
			return strings.TrimSpace(s)
		}
	}
	for _, key := range []string{"id_token", "idToken"} {
		if token, ok := v[key].(string); ok {
			if email := jwtStringClaim(token, "email"); email != "" {
				return email
			}
			if sub := jwtStringClaim(token, "sub"); sub != "" {
				return "sub:" + sub
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
	var v map[string]any
	if json.Unmarshal(data, &v) != nil {
		return time.Time{}
	}
	for _, key := range []string{"expiry_date", "expiryDate", "expires_at", "expiresAt", "expiry"} {
		if t := parseTimeValue(v[key]); !t.IsZero() {
			return t
		}
	}
	for _, key := range []string{"id_token", "idToken", "access_token", "accessToken"} {
		if token, ok := v[key].(string); ok {
			if exp := jwtNumberClaim(token, "exp"); exp > 0 {
				return time.Unix(exp, 0).UTC()
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
	for _, key := range []string{"refresh_token", "refreshToken"} {
		if _, ok := next[key]; !ok {
			if v, ok := cur[key]; ok {
				next[key] = v
			}
		}
	}
	out, err := json.Marshal(next)
	if err != nil {
		return append(json.RawMessage(nil), candidate...)
	}
	return out
}

func credentialsEquivalent(a, b credentialVault) bool {
	return string(a.OAuth) == string(b.OAuth) &&
		string(a.Accounts) == string(b.Accounts) &&
		a.UserID == b.UserID
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
