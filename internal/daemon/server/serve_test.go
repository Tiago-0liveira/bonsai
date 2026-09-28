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
		ServerConfig:  "/tmp/server.json",
		APIPort:       7001,
		WebhookPort:   7002,
		WebPort:       7003,
	}
	if err := validateServeSpec(spec); err != nil {
		t.Fatal(err)
	}
	spec.WebPort = spec.APIPort
	if err := validateServeSpec(spec); err == nil {
		t.Fatal("expected duplicate-port validation error")
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
