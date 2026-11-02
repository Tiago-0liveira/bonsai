package agents

import (
	"errors"
	"strings"
	"testing"
)

func TestNativePermissions(t *testing.T) {
	on, off := true, false
	cases := []struct {
		name     string
		provider ProviderID
		launch   LaunchOptions
		want     error
		field    string
	}{
		{"codex missing sandbox", "codex", LaunchOptions{}, ErrNativeUnresolved, "sandbox"},
		{"codex missing approval", "codex", LaunchOptions{Sandbox: "workspace-write"}, ErrNativeUnresolved, "approval policy"},
		{"codex safe", "codex", LaunchOptions{Sandbox: "workspace-write", Approval: "on-request"}, nil, ""},
		{"codex readonly", "codex", LaunchOptions{Sandbox: "read-only", Approval: "untrusted"}, nil, ""},
		{"codex dangerous", "codex", LaunchOptions{Sandbox: "danger-full-access", Approval: "never"}, nil, ""},
		{"codex bypass", "codex", LaunchOptions{Sandbox: "bypass"}, nil, ""},
		{"codex bypass approval", "codex", LaunchOptions{Sandbox: "bypass", Approval: "never"}, ErrNativeUnsupported, "bypass takes no approval policy"},
		{"codex bad sandbox", "codex", LaunchOptions{Sandbox: "secret"}, ErrNativeUnsupported, "unknown sandbox"},
		{"codex bad approval", "codex", LaunchOptions{Sandbox: "read-only", Approval: "secret"}, ErrNativeUnsupported, "unknown approval policy"},
		{"codex foreign permission", "codex", LaunchOptions{PermissionMode: "default"}, ErrNativeUnsupported, "codex uses sandbox and approval"},
		{"codex foreign full access", "codex", LaunchOptions{FullAccess: &off}, ErrNativeUnsupported, "codex uses sandbox and approval"},
		{"claude missing", "claude", LaunchOptions{}, ErrNativeUnresolved, "permission mode"},
		{"claude default", "claude", LaunchOptions{PermissionMode: "default"}, nil, ""},
		{"claude provider decides enum", "claude", LaunchOptions{PermissionMode: "provider-owned"}, nil, ""},
		{"claude sandbox", "claude", LaunchOptions{Sandbox: "read-only"}, ErrNativeUnsupported, "claude uses a permission mode"},
		{"claude approval", "claude", LaunchOptions{Approval: "never"}, ErrNativeUnsupported, "claude uses a permission mode"},
		{"claude full access", "claude", LaunchOptions{FullAccess: &off}, ErrNativeUnsupported, "claude uses a permission mode"},
		{"antigravity missing", "antigravity", LaunchOptions{}, ErrNativeUnresolved, "full access"},
		{"antigravity safe", "antigravity", LaunchOptions{FullAccess: &off}, nil, ""},
		{"antigravity full", "antigravity", LaunchOptions{FullAccess: &on}, nil, ""},
		{"antigravity permission", "antigravity", LaunchOptions{PermissionMode: "default"}, ErrNativeUnsupported, "antigravity uses full access"},
		{"antigravity sandbox", "antigravity", LaunchOptions{Sandbox: "read-only"}, ErrNativeUnsupported, "antigravity uses full access"},
		{"antigravity approval", "antigravity", LaunchOptions{Approval: "never"}, ErrNativeUnsupported, "antigravity uses full access"},
		{"unknown", "other", LaunchOptions{}, ErrNativeUnsupported, "unknown provider"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err := NativePermissions(tt.provider, tt.launch)
			if !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
			if err != nil && !strings.Contains(err.Error(), tt.field) {
				t.Fatalf("missing fixed field/reason: %v", err)
			}
			if tt.want == ErrNativeUnsupported && !errors.Is(err, ErrInvalidLaunch) {
				t.Fatalf("missing invalid launch: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatal("echoed launch value")
			}
		})
	}
}

func TestEscalatedLaunch(t *testing.T) {
	on, off := true, false
	cases := []struct {
		provider ProviderID
		launch   LaunchOptions
		want     bool
	}{
		{"codex", LaunchOptions{}, false}, {"codex", LaunchOptions{Sandbox: "workspace-write"}, false},
		{"codex", LaunchOptions{Sandbox: "read-only", Approval: "never"}, false},
		{"codex", LaunchOptions{Sandbox: "danger-full-access"}, true}, {"codex", LaunchOptions{Sandbox: "bypass"}, true},
		{"claude", LaunchOptions{PermissionMode: "default"}, false}, {"claude", LaunchOptions{PermissionMode: "acceptEdits"}, false},
		{"claude", LaunchOptions{PermissionMode: "plan"}, false}, {"claude", LaunchOptions{PermissionMode: "bypassPermissions"}, true},
		{"claude", LaunchOptions{PermissionMode: "auto"}, true}, {"claude", LaunchOptions{PermissionMode: "dontAsk"}, true},
		{"antigravity", LaunchOptions{}, false}, {"antigravity", LaunchOptions{FullAccess: &off}, false}, {"antigravity", LaunchOptions{FullAccess: &on}, true},
		{"other", LaunchOptions{FullAccess: &on, Sandbox: "bypass", PermissionMode: "auto"}, false},
	}
	for _, tt := range cases {
		if got := EscalatedLaunch(tt.provider, tt.launch); got != tt.want {
			t.Errorf("%s %+v: %v, want %v", tt.provider, tt.launch, got, tt.want)
		}
	}
}
