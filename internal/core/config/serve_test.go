package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestServeDefaultsAndConfig(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Serve.APIPort != 7001 {
		t.Fatalf("serve api port = %d, want 7001", cfg.Serve.APIPort)
	}
	if cfg.Serve.StartupTimeout != 30 {
		t.Fatalf("serve startup timeout = %d, want 30", cfg.Serve.StartupTimeout)
	}
	if cfg.Serve.ShutdownTimeout != 5 {
		t.Fatalf("serve shutdown timeout = %d, want 5", cfg.Serve.ShutdownTimeout)
	}

	data := []byte(`serve:
  api_port: 7101
  startup_timeout_seconds: 12
  shutdown_timeout_seconds: 3
`)
	if err := os.WriteFile(filepath.Join(dir, ".bonsai.yaml"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Serve.APIPort != 7101 || cfg.Serve.StartupTimeout != 12 || cfg.Serve.ShutdownTimeout != 3 {
		t.Fatalf("serve config = %+v", cfg.Serve)
	}
}
