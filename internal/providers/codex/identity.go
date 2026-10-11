package codex

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"strings"
)

// maxAuthFile bounds how much of auth.json is read; real files are a few KB.
const maxAuthFile = 1 << 20

// identity is the display-safe part of a profile's auth.json. It never holds a
// token or key.
type identity struct {
	Present   bool // auth.json exists and parses
	APIKey    bool
	Email     string
	Plan      string
	AccountID string
}

// String renders "email · plan" for a ChatGPT login.
func (id identity) String() string {
	switch {
	case id.Email != "" && id.Plan != "":
		return id.Email + " · " + id.Plan
	case id.Email != "":
		return id.Email
	case id.APIKey:
		return "API key"
	}
	return ""
}

// sameLogin reports whether two profiles are signed in to the same ChatGPT
// account and workspace, and so share usage limits.
func (id identity) sameLogin(other identity) bool {
	if id.Email == "" || !strings.EqualFold(id.Email, other.Email) {
		return false
	}
	return id.AccountID == "" || other.AccountID == "" || id.AccountID == other.AccountID
}

type authFile struct {
	AuthMode string  `json:"auth_mode"`
	APIKey   *string `json:"OPENAI_API_KEY"`
	Tokens   *struct {
		IDToken   string `json:"id_token"`
		AccountID string `json:"account_id"`
	} `json:"tokens"`
}

type idClaims struct {
	Email string `json:"email"`
	Auth  struct {
		PlanType  string `json:"chatgpt_plan_type"`
		AccountID string `json:"chatgpt_account_id"`
	} `json:"https://api.openai.com/auth"`
}

// readIdentity decodes the profile's auth.json. The id_token's claims are read
// for display only; its signature is not (and need not be) checked.
func readIdentity(home string) identity {
	f, err := os.Open(authPath(home))
	if err != nil {
		return identity{}
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxAuthFile))
	if err != nil {
		return identity{}
	}
	var file authFile
	if json.Unmarshal(data, &file) != nil {
		return identity{}
	}
	id := identity{Present: true}
	if file.APIKey != nil && *file.APIKey != "" || strings.EqualFold(strings.ReplaceAll(file.AuthMode, "_", ""), "apikey") {
		id.APIKey = true
	}
	if file.Tokens == nil {
		return id
	}
	id.AccountID = file.Tokens.AccountID
	if claims, ok := decodeClaims(file.Tokens.IDToken); ok {
		id.Email = strings.TrimSpace(claims.Email)
		id.Plan = strings.TrimSpace(claims.Auth.PlanType)
		if claims.Auth.AccountID != "" {
			id.AccountID = claims.Auth.AccountID
		}
	}
	return id
}

func decodeClaims(token string) (idClaims, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return idClaims{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return idClaims{}, false
	}
	var claims idClaims
	if json.Unmarshal(payload, &claims) != nil {
		return idClaims{}, false
	}
	return claims, true
}
