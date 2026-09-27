package server

import (
	"net"
	"slices"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
)

func TestValidateProductionServeSpecAndPortCollision(t *testing.T) {
	spec := procstore.ServeSpec{
		Mode:          procstore.ServeModeProduction,
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
		func() procstore.ServeSpec { v := spec; v.Mode = procstore.ServeModeDevelopment; return v }(),
		func() procstore.ServeSpec { v := spec; v.WebhookPort = 7002; return v }(),
		func() procstore.ServeSpec { v := spec; v.WebPort = 7003; return v }(),
		func() procstore.ServeSpec {
			v := spec
			v.Sidecars = []procstore.ServeSidecar{{Name: "tunnel", Command: []string{"cloudflared"}}}
			return v
		}(),
	}
	for i, candidate := range invalid {
		if err := validateServeSpec(candidate); err == nil {
			t.Fatalf("invalid production serve spec %d unexpectedly accepted: %+v", i, candidate)
		}
	}

	const daemonRoot = "/canonical/main-root"
	args := serveAPIArgs(daemonRoot, spec, "production")
	for _, forbidden := range []string{"__serve-webhook", "--capability-file", "--web-port", "--webhook-port", "--development"} {
		if slices.Contains(args, forbidden) {
			t.Fatalf("production serve API args contain development value %q: %v", forbidden, args)
		}
	}
	repoIndex := slices.Index(args, "--repo")
	if repoIndex < 0 || repoIndex+1 >= len(args) || args[repoIndex+1] != daemonRoot {
		t.Fatalf("production serve API args do not use daemon root: %v", args)
	}
	if args[repoIndex+1] == spec.WorkspacePath {
		t.Fatalf("production serve API args used workspace path as daemon root: %v", args)
	}
	modeIndex := slices.Index(args, "--security-mode")
	if modeIndex < 0 || modeIndex+1 >= len(args) || args[modeIndex+1] != "production" {
		t.Fatalf("production serve API args do not force production security: %v", args)
	}

	env := serveEnvironment(spec)
	for _, forbidden := range []string{"BONSAI_WEBHOOK_PORT", "BONSAI_WEB_PORT", "BONSAI_SERVE_SECRET_FILE", "BONSAI_DEV_WEBHOOK_SECRET"} {
		if _, ok := env[forbidden]; ok {
			t.Fatalf("production serve environment exports development value %s", forbidden)
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

func TestValidateDevelopmentServeSpec(t *testing.T) {
	spec := procstore.ServeSpec{
		Mode:          procstore.ServeModeDevelopment,
		WorkspaceID:   "workspace",
		WorkspacePath: t.TempDir(),
		Executable:    "/tmp/bonsai",
		APIPort:       7001,
		WebhookPort:   7002,
		WebPort:       7003,
		BrowserOrigin: "http://127.0.0.1:7003",
	}
	if err := validateDevServeSpec(spec); err != nil {
		t.Fatal(err)
	}
	for i, mutate := range []func(*procstore.ServeSpec){
		func(v *procstore.ServeSpec) { v.Mode = procstore.ServeModeProduction },
		func(v *procstore.ServeSpec) { v.WebhookPort = v.APIPort },
		func(v *procstore.ServeSpec) { v.BrowserOrigin = "http://0.0.0.0:7003" },
		func(v *procstore.ServeSpec) { v.BrowserOrigin = "http://127.0.0.1:7999" },
	} {
		candidate := spec
		mutate(&candidate)
		if err := validateDevServeSpec(candidate); err == nil {
			t.Fatalf("invalid development spec %d unexpectedly accepted: %+v", i, candidate)
		}
	}
	devArgs := serveAPIArgs("/canonical/main-root", spec, "development")
	repoIndex := slices.Index(devArgs, "--repo")
	if repoIndex < 0 || repoIndex+1 >= len(devArgs) || devArgs[repoIndex+1] != "/canonical/main-root" {
		t.Fatalf("development serve API args do not use daemon root: %v", devArgs)
	}
	modeIndex := slices.Index(devArgs, "--security-mode")
	if modeIndex < 0 || modeIndex+1 >= len(devArgs) || devArgs[modeIndex+1] != "development" {
		t.Fatalf("development serve API args do not force development security: %v", devArgs)
	}

	env := serveDevEnvironment(spec)
	if env["BONSAI_API_PORT"] != "7001" || env["BONSAI_WEBHOOK_PORT"] != "7002" || env["BONSAI_WEB_PORT"] != "7003" {
		t.Fatalf("development environment = %#v", env)
	}
	if _, ok := env["BONSAI_DEV_WEBHOOK_SECRET"]; ok {
		t.Fatal("development webhook secret leaked into child environment")
	}
}
