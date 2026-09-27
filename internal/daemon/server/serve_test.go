package server

import (
	"net"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
)

func TestValidateServeSpecAndPortCollision(t *testing.T) {
	spec := procstore.ServeSpec{
		WorkspaceID:   "workspace",
		WorkspacePath: t.TempDir(),
		Executable:    "/tmp/bonsai",
		APIPort:       7001,
		BrowserOrigin: "https://app.bonsai.dev",
	}
	if err := validateServeSpec(spec); err != nil {
		t.Fatal(err)
	}
	spec.APIPort = 0
	if err := validateServeSpec(spec); err == nil {
		t.Fatal("expected invalid API port")
	}
	spec.APIPort = 7001
	spec.BrowserOrigin = ""
	if err := validateServeSpec(spec); err == nil {
		t.Fatal("expected missing browser origin")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	if err := checkServePorts(port); err == nil {
		t.Fatal("expected occupied-port error")
	}
}
