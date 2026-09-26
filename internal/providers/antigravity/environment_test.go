package antigravity

import (
	"path/filepath"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func TestBuildEnvironmentIsolatesHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "session-home")
	session := agents.Session{
		ID: "sess_one", AccountID: "acct_one", Provider: ProviderID,
		RuntimeDir: filepath.Dir(home), HomeDir: home,
	}
	env := buildEnvironment(agents.Account{ID: "acct_one", Provider: ProviderID}, session)
	if env["HOME"] != home {
		t.Fatalf("HOME = %q, want %q", env["HOME"], home)
	}
	if env["GEMINI_FORCE_FILE_STORAGE"] != "true" {
		t.Fatalf("GEMINI_FORCE_FILE_STORAGE = %q", env["GEMINI_FORCE_FILE_STORAGE"])
	}
	if env["BONSAI_AGENT_ACCOUNT_ID"] != "acct_one" || env["BONSAI_AGENT_SESSION_ID"] != "sess_one" {
		t.Fatalf("missing session markers: %#v", env)
	}
}
