package codex

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func fakeIDToken(claims string) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"RS256"}`)) + "." + enc([]byte(claims)) + ".c2ln"
}

func TestReadIdentity(t *testing.T) {
	for _, tc := range []struct {
		name string
		auth string
		want identity
		text string
	}{
		{name: "missing", want: identity{}},
		{name: "garbage", auth: "{", want: identity{}},
		{name: "chatgpt",
			auth: `{"auth_mode":"chatgpt","OPENAI_API_KEY":null,"tokens":{"id_token":"` + fakeIDToken(`{"email":"me@example.com","https://api.openai.com/auth":{"chatgpt_plan_type":"plus","chatgpt_account_id":"acct-1"}}`) + `","access_token":"a","refresh_token":"r","account_id":"acct-0"},"last_refresh":"x"}`,
			want: identity{Present: true, Email: "me@example.com", Plan: "plus", AccountID: "acct-1"}, text: "me@example.com"},
		{name: "no plan", auth: `{"tokens":{"id_token":"` + fakeIDToken(`{"email":"me@example.com"}`) + `","account_id":"acct-0"}}`,
			want: identity{Present: true, Email: "me@example.com", AccountID: "acct-0"}, text: "me@example.com"},
		{name: "bad token", auth: `{"tokens":{"id_token":"not.a-jwt!.x","account_id":"acct-0"}}`, want: identity{Present: true, AccountID: "acct-0"}},
		{name: "api key", auth: `{"auth_mode":"apikey","OPENAI_API_KEY":"sk-x"}`, want: identity{Present: true, APIKey: true}, text: "API key"},
		{name: "api key mode only", auth: `{"auth_mode":"api_key"}`, want: identity{Present: true, APIKey: true}, text: "API key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			if tc.auth != "" {
				if err := os.WriteFile(authPath(home), []byte(tc.auth), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got := readIdentity(home)
			if got != tc.want || got.String() != tc.text {
				t.Fatalf("identity = %+v (%q), want %+v (%q)", got, got.String(), tc.want, tc.text)
			}
		})
	}
}

func TestSameLogin(t *testing.T) {
	a := identity{Email: "Me@example.com", AccountID: "acct-1"}
	for _, tc := range []struct {
		other identity
		want  bool
	}{
		{identity{Email: "me@example.com", AccountID: "acct-1"}, true},
		{identity{Email: "me@example.com"}, true},
		{identity{Email: "me@example.com", AccountID: "acct-2"}, false},
		{identity{Email: "you@example.com", AccountID: "acct-1"}, false},
	} {
		if got := a.sameLogin(tc.other); got != tc.want {
			t.Errorf("sameLogin(%+v) = %v", tc.other, got)
		}
	}
	if (identity{}).sameLogin(identity{}) {
		t.Error("empty identities are not the same login")
	}
}

// The auth.json fields Bonsai reads exist in the shape Phase 0 recorded from a
// real Codex login.
func TestAuthFileShapeMatchesPhase0(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "phase0", "auth.shape.json"))
	if err != nil {
		t.Fatal(err)
	}
	var shape struct {
		AuthMode string `json:"auth_mode"`
		APIKey   string `json:"OPENAI_API_KEY"`
		Tokens   struct {
			IDToken   string `json:"id_token"`
			AccountID string `json:"account_id"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(data, &shape); err != nil {
		t.Fatal(err)
	}
	if shape.AuthMode != "str" || shape.APIKey == "" || shape.Tokens.IDToken != "str" || shape.Tokens.AccountID != "str" {
		t.Fatalf("shape = %+v", shape)
	}
}
