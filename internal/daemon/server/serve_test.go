package server

import (
	"net"
	"slices"
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

	invalid := []procstore.ServeSpec{
		func() procstore.ServeSpec { v := spec; v.APIPort = 0; return v }(),
		func() procstore.ServeSpec { v := spec; v.BrowserOrigin = ""; return v }(),
		func() procstore.ServeSpec { v := spec; v.BrowserOrigin = "https://evil.example"; return v }(),
		func() procstore.ServeSpec { v := spec; v.Development = true; return v }(),
		func() procstore.ServeSpec {
			v := spec
			v.Development = true
			v.BrowserOrigin = "http://localhost:5174"
			return v
		}(),
	}
	for i, candidate := range invalid {
		if err := validateServeSpec(candidate); err == nil {
			t.Fatalf("invalid serve spec %d unexpectedly accepted: %+v", i, candidate)
		}
	}

	dev := spec
	dev.Development = true
	dev.BrowserOrigin = "http://localhost:5173"
	if err := validateServeSpec(dev); err != nil {
		t.Fatalf("explicit development origin rejected: %v", err)
	}

	args := serveAPIArgs(spec)
	for _, forbidden := range []string{"__serve-webhook", "--capability-file", "--web-port", "--webhook-port"} {
		if slices.Contains(args, forbidden) {
			t.Fatalf("serve API args contain legacy value %q: %v", forbidden, args)
		}
	}
	if slices.Contains(args, "--development") {
		t.Fatalf("production serve API args unexpectedly enable development: %v", args)
	}
	if devArgs := serveAPIArgs(dev); !slices.Contains(devArgs, "--development") {
		t.Fatalf("development serve API args = %v", devArgs)
	}

	env := serveEnvironment(spec)
	for _, forbidden := range []string{"BONSAI_WEBHOOK_PORT", "BONSAI_WEB_PORT", "BONSAI_SERVE_SECRET_FILE"} {
		if _, ok := env[forbidden]; ok {
			t.Fatalf("serve environment exports legacy %s", forbidden)
		}
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
