package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestServeDefaultsAndSidecarConfig(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Serve.APIPort != 7001 || cfg.Serve.WebhookPort != 7002 || cfg.Serve.WebPort != 7003 {
		t.Fatalf("serve defaults = %+v", cfg.Serve)
	}
	if cfg.Serve.StartupTimeout != 30 {
		t.Fatalf("serve startup timeout = %d, want 30", cfg.Serve.StartupTimeout)
	}

	data := []byte(`serve:
  api_port: 7101
  webhook_port: 7102
  web_port: 7103
  server_config: dev/server.json
  sidecars:
    - name: tunnel
      command: [cloudflared, tunnel]
      required: true
      restart: always
      environment:
        EXTRA: value
`)
	if err := os.WriteFile(filepath.Join(dir, ".bonsai.yaml"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Serve.APIPort != 7101 || len(cfg.Serve.Sidecars) != 1 {
		t.Fatalf("serve config = %+v", cfg.Serve)
	}
	if got := cfg.Serve.Sidecars[0]; got.Name != "tunnel" || !got.Required || got.Environment["EXTRA"] != "value" {
		t.Fatalf("sidecar = %+v", got)
	}
}
