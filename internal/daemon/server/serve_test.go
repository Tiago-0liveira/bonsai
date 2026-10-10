package server

import (
	"net"
	"slices"
	"strings"
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
	custom := spec
	custom.BrowserOrigin = "https://app.bonsai.tiagoliv.com"
	if err := validateServeSpec(custom); err != nil {
		t.Fatalf("custom production origin rejected: %v", err)
	}

	invalid := []procstore.ServeSpec{
		func() procstore.ServeSpec { v := spec; v.APIPort = 0; return v }(),
		func() procstore.ServeSpec { v := spec; v.BrowserOrigin = ""; return v }(),
		func() procstore.ServeSpec { v := spec; v.BrowserOrigin = "http://app.example.com"; return v }(),
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

	// The user-level web stack may disable the hosted app (empty origin); its
	// API then allows only its own origin. A repository-scoped serve may not.
	localOnly := spec
	localOnly.Scope, localOnly.WorkspaceID, localOnly.BrowserOrigin = procstore.ServeScopeUser, procstore.WebServeGroupID, ""
	if err := validateServeSpec(localOnly); err != nil {
		t.Fatalf("user-scoped serve without a hosted origin rejected: %v", err)
	}
	localArgs := serveAPIArgs("/ignored", localOnly, "production")
	if i := slices.Index(localArgs, "--browser-origin"); i < 0 || i+1 >= len(localArgs) || localArgs[i+1] != "" {
		t.Fatalf("user-scoped API args must pass the empty browser origin explicitly: %v", localArgs)
	}
	localOnly.BrowserOrigin = "http://app.example.com"
	if err := validateServeSpec(localOnly); err == nil {
		t.Fatal("user-scoped serve accepted a non-HTTPS hosted origin")
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

	user := spec
	user.Scope = procstore.ServeScopeUser
	user.WorkspaceID = procstore.WebServeGroupID
	if err := validateServeSpec(user); err != nil {
		t.Fatalf("user-scoped serve spec rejected: %v", err)
	}
	userArgs := serveAPIArgs(daemonRoot, user, "production")
	if slices.Contains(userArgs, "--repo") || slices.Contains(userArgs, daemonRoot) {
		t.Fatalf("user-scoped API args name a launch repository: %v", userArgs)
	}
	if i := slices.Index(userArgs, "--security-mode"); i < 0 || userArgs[i+1] != "production" {
		t.Fatalf("user-scoped API args do not force production security: %v", userArgs)
	}
	for i, candidate := range []procstore.ServeSpec{
		func() procstore.ServeSpec { v := user; v.WorkspaceID = "other"; return v }(),
		func() procstore.ServeSpec { v := user; v.Scope = "machine"; return v }(),
		func() procstore.ServeSpec { v := user; v.WebPort = 7003; return v }(),
	} {
		if err := validateServeSpec(candidate); err == nil {
			t.Fatalf("invalid user-scoped spec %d accepted: %+v", i, candidate)
		}
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
		func(v *procstore.ServeSpec) { v.Scope = procstore.ServeScopeUser },
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

func TestValidateLiveServeSpec(t *testing.T) {
	quick := []string{"cloudflared", "tunnel", "--no-autoupdate", "--url", "http://127.0.0.1:7002"}
	live := procstore.ServeSpec{
		Mode:          procstore.ServeModeProduction,
		Scope:         procstore.ServeScopeUser,
		WorkspaceID:   procstore.WebServeGroupID,
		WorkspacePath: t.TempDir(),
		Executable:    "/tmp/bonsai",
		APIPort:       7001,
		BrowserOrigin: "https://app.bonsai.dev",
		WebhookPort:   7002,
		Sidecars:      []procstore.ServeSidecar{{Name: "tunnel", Command: quick}},
	}
	if err := validateServeSpec(live); err != nil {
		t.Fatalf("live spec rejected: %v", err)
	}
	receiverOnly := live
	receiverOnly.Sidecars = nil // external-url: the user runs their own proxy
	if err := validateServeSpec(receiverOnly); err != nil {
		t.Fatalf("receiver without tunnel rejected: %v", err)
	}
	with := func(mutate func(*procstore.ServeSpec)) procstore.ServeSpec {
		v := live
		v.Sidecars = append([]procstore.ServeSidecar(nil), live.Sidecars...)
		mutate(&v)
		return v
	}
	tunnel := func(command ...string) func(*procstore.ServeSpec) {
		return func(v *procstore.ServeSpec) { v.Sidecars[0].Command = command }
	}
	for name, candidate := range map[string]procstore.ServeSpec{
		"api host:port in argv":  with(tunnel("cloudflared", "tunnel", "--url", "http://127.0.0.1:7002", "--metrics", "127.0.0.1:7001")),
		"api bare port in argv":  with(tunnel("cloudflared", "tunnel", "--url", "http://127.0.0.1:7002", "--metrics", ":7001")),
		"tunnel targets the api": with(tunnel("cloudflared", "tunnel", "--url", "http://127.0.0.1:7001")),
		"no webhook reference":   with(tunnel("cloudflared", "tunnel", "--url", "http://127.0.0.1:8080")),
		"project service":        with(tunnel("ngrok", "http", "127.0.0.1:7002", "--also", "127.0.0.1:3000")),
		"empty command":          with(tunnel()),
		"two sidecars": with(func(v *procstore.ServeSpec) {
			v.Sidecars = append(v.Sidecars, procstore.ServeSidecar{Name: "tunnel2", Command: quick})
		}),
		"other name":         with(func(v *procstore.ServeSpec) { v.Sidecars[0].Name = "proxy" }),
		"environment":        with(func(v *procstore.ServeSpec) { v.Sidecars[0].Environment = map[string]string{"TUNNEL_ORIGIN": "x"} }),
		"working directory":  with(func(v *procstore.ServeSpec) { v.Sidecars[0].Cwd = "/" }),
		"required":           with(func(v *procstore.ServeSpec) { v.Sidecars[0].Required = true }),
		"restart policy":     with(func(v *procstore.ServeSpec) { v.Sidecars[0].Restart = procstore.PolicyAlways }),
		"max restarts":       with(func(v *procstore.ServeSpec) { v.Sidecars[0].MaxRestarts = 50 }),
		"webhook is api":     with(func(v *procstore.ServeSpec) { v.WebhookPort = 7001 }),
		"webhook invalid":    with(func(v *procstore.ServeSpec) { v.WebhookPort = 70000 }),
		"tunnel w/o webhook": with(func(v *procstore.ServeSpec) { v.WebhookPort = 0 }),
		"web port":           with(func(v *procstore.ServeSpec) { v.WebPort = 7003 }),
		"repository scope": with(func(v *procstore.ServeSpec) {
			v.Scope, v.WorkspaceID = procstore.ServeScopeRepository, "workspace"
		}),
	} {
		if err := validateServeSpec(candidate); err == nil {
			t.Errorf("%s: accepted %+v", name, candidate.Sidecars)
		}
	}
	// A repository-scoped serve keeps the old refusal text, which older CLIs
	// recognise.
	repo := with(func(v *procstore.ServeSpec) { v.Scope, v.WorkspaceID = procstore.ServeScopeRepository, "workspace" })
	if err := validateServeSpec(repo); err == nil || err.Error() != "production serve cannot supervise development services" {
		t.Fatalf("repository scope error = %v", err)
	}

	args := serveAPIArgs("/ignored", live, "production")
	if i := slices.Index(args, "--webhook-port"); i < 0 || args[i+1] != "7002" {
		t.Fatalf("live API args lack the webhook port: %v", args)
	}
	for _, arg := range args {
		if strings.Contains(arg, "secret") {
			t.Fatalf("API args mention a secret: %v", args)
		}
	}
	if args := serveAPIArgs("/ignored", receiverOnly, "production"); !slices.Contains(args, "--webhook-port") {
		t.Fatalf("receiver-only API args lack the webhook port: %v", args)
	}
	standard := live
	standard.WebhookPort, standard.Sidecars = 0, nil
	if args := serveAPIArgs("/ignored", standard, "production"); slices.Contains(args, "--webhook-port") {
		t.Fatalf("standard API args name a webhook port: %v", args)
	}
}

func TestSameLiveServeAndProductionProcesses(t *testing.T) {
	quick := []string{"cloudflared", "tunnel", "--url", "http://127.0.0.1:7002"}
	a := procstore.ServeSpec{WebhookPort: 7002, Sidecars: []procstore.ServeSidecar{{Name: "tunnel", Command: quick}}}
	if !sameLiveServe(a, a) {
		t.Fatal("identical live specs differ")
	}
	if !sameLiveServe(procstore.ServeSpec{}, procstore.ServeSpec{}) {
		t.Fatal("standard specs differ")
	}
	for name, b := range map[string]procstore.ServeSpec{
		"standard":     {},
		"other port":   {WebhookPort: 7012, Sidecars: a.Sidecars},
		"no tunnel":    {WebhookPort: 7002},
		"other tunnel": {WebhookPort: 7002, Sidecars: []procstore.ServeSidecar{{Name: "tunnel", Command: []string{"tailscale", "funnel", "7002"}}}},
	} {
		if sameLiveServe(a, b) {
			t.Errorf("%s: treated as the same live spec", name)
		}
	}
	if !onlyProductionProcesses(map[string]int{"api": 1, "tunnel": 2}) || !onlyProductionProcesses(map[string]int{"api": 1}) {
		t.Fatal("api + tunnel is a production group")
	}
	if onlyProductionProcesses(map[string]int{"api": 1, "web": 2}) {
		t.Fatal("a development process passed as production")
	}
}
