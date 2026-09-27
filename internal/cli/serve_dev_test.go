package cli

import (
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
)

func TestResolveDevPortPrecedence(t *testing.T) {
	t.Setenv("BONSAI_DEV_WEBHOOK_PORT", "8123")
	if got, err := resolveDevPort(0, "BONSAI_DEV_WEBHOOK_PORT", 7002); err != nil || got != 8123 {
		t.Fatalf("environment port = %d, %v", got, err)
	}
	if got, err := resolveDevPort(9002, "BONSAI_DEV_WEBHOOK_PORT", 7002); err != nil || got != 9002 {
		t.Fatalf("flag port = %d, %v", got, err)
	}
	t.Setenv("BONSAI_DEV_WEBHOOK_PORT", "bad")
	if _, err := resolveDevPort(0, "BONSAI_DEV_WEBHOOK_PORT", 7002); err == nil {
		t.Fatal("invalid environment port accepted")
	}
}

func TestDevTunnelExposesOnlyWebhookTarget(t *testing.T) {
	for _, kind := range []string{"cloudflared", "ngrok"} {
		sidecar, err := devTunnelSidecar(kind, 7002)
		if err != nil {
			t.Fatal(err)
		}
		if sidecar.Name != "tunnel" || sidecar.Restart != procstore.PolicyOnFailure {
			t.Fatalf("%s sidecar = %#v", kind, sidecar)
		}
		joined := strings.Join(sidecar.Command, " ")
		if !strings.Contains(joined, "127.0.0.1:7002") {
			t.Fatalf("%s tunnel does not target webhook: %s", kind, joined)
		}
		if strings.Contains(joined, "7001") || strings.Contains(joined, "7003") {
			t.Fatalf("%s tunnel exposes a non-webhook port: %s", kind, joined)
		}
	}
	if _, err := devTunnelSidecar("other", 7002); err == nil {
		t.Fatal("unsupported tunnel accepted")
	}
}
