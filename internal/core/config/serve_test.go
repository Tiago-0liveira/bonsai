package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestObsoleteServeYAMLIsInert(t *testing.T) {
	dir := t.TempDir()
	before, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".bonsai.yaml"), []byte(`serve:
  api_port: 99999
  startup_timeout_seconds: -1
  shutdown_timeout_seconds: -1
  webhook_port: 1
  web_port: 2
  server_config: obsolete.json
`), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := Load(dir)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("obsolete settings changed config: %+v, %v", after, err)
	}
}
