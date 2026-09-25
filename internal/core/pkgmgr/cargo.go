package pkgmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type cargoProvider struct{}

func (cargoProvider) ID() string { return "cargo" }

type cargoDetection struct {
	ManifestPath      string
	WorkspaceRoot     string
	Metadata          cargoMetadataInfo
	MetadataAttempted bool
}

func (cargoProvider) Detect(ctx Context) (Detection, error) {
	root, manifest := findUp(ctx.Location.InputDir, "Cargo.toml")
	if root == "" {
		return Detection{}, nil
	}
	workspace := findCargoWorkspaceRoot(root)
	return Detection{
		Applicable:    true,
		ID:            "cargo",
		Name:          "cargo",
		Root:          root,
		WorkspaceRoot: workspace,
		Data:          &cargoDetection{ManifestPath: manifest, WorkspaceRoot: workspace},
	}, nil
}

var cargoBuiltins = []string{
	"build", "check", "clean", "doc", "fetch", "fix", "metadata", "package", "publish",
	"run", "test", "tree", "update", "vendor", "version",
}

func (cargoProvider) Commands(ctx Context, detection Detection) ([]Command, error) {
	d, ok := detection.Data.(*cargoDetection)
	if !ok {
		return nil, fmt.Errorf("pkgmgr: invalid cargo detection data")
	}
	if ctx.Options.AllowProviderCLI && !d.MetadataAttempted {
		d.MetadataAttempted = true
		if got, err := readCargoMetadata(ctx.Options.Runner, detection.Root); err == nil {
			d.Metadata = got
		}
	}
	meta := d.Metadata
	commands := make([]Command, 0, len(cargoBuiltins))
	for _, name := range cargoBuiltins {
		cmd := Command{
			ID:          "cargo:builtin:" + name,
			Name:        name,
			Provider:    detection.ID,
			Kind:        CommandBuiltin,
			Invocation:  InvocationSpec{Program: "cargo", Prefix: []string{name}, WorkingDir: detection.Root},
			Source:      Source{Kind: "catalog", File: "Cargo builtin catalog", Pointer: name},
			Confidence:  ConfidenceExact,
			Description: "Cargo " + name,
		}
		cmd.Args = cargoArgs(name, d.ManifestPath, meta)
		if name == "run" || name == "test" {
			cmd.Invocation.PassThrough = PassThroughDoubleDash
		}
		commands = append(commands, cmd)
	}
	return commands, nil
}

func (cargoProvider) FingerprintInputs(ctx Context, detection Detection) ([]FingerprintInput, error) {
	d, ok := detection.Data.(*cargoDetection)
	if !ok {
		return nil, fmt.Errorf("pkgmgr: invalid cargo detection data")
	}
	paths := []string{d.ManifestPath}
	if d.WorkspaceRoot != "" {
		workspaceManifest := filepath.Join(d.WorkspaceRoot, "Cargo.toml")
		if filepath.Clean(workspaceManifest) != filepath.Clean(d.ManifestPath) {
			paths = append(paths, workspaceManifest)
		}
	}
	for _, base := range uniqueDirs(detection.Root, d.WorkspaceRoot) {
		for _, rel := range []string{filepath.Join(".cargo", "config.toml"), filepath.Join(".cargo", "config")} {
			path := filepath.Join(base, rel)
			if _, err := os.Stat(path); err == nil {
				paths = append(paths, path)
			}
		}
	}
	inputs, err := fileInputs("cargo", d.WorkspaceRoot, detection.Root, paths)
	if err != nil {
		return nil, err
	}
	if ctx.Options.AllowProviderCLI {
		d.MetadataAttempted = true
		if meta, err := readCargoMetadata(ctx.Options.Runner, detection.Root); err == nil {
			d.Metadata = meta
			if canonical, err := json.Marshal(meta); err == nil {
				inputs = append(inputs, FingerprintInput{Name: "cargo/metadata.json", Content: canonical})
			}
		}
	}
	return inputs, nil
}

func cargoArgs(command, manifest string, meta cargoMetadataInfo) []Argument {
	src := Source{Kind: "catalog", File: "Cargo builtin catalog", Pointer: command}
	flag := func(id string, typ ValueType, flags ...string) Argument {
		return Argument{ID: id, Name: id, Kind: ArgumentFlag, Type: typ, Flags: flags, Source: src, Confidence: ConfidenceExact}
	}
	shared := []Argument{
		flag("package", ValueString, "-p", "--package"),
		flag("workspace", ValueBool, "--workspace"),
		{ID: "exclude", Name: "exclude", Kind: ArgumentFlag, Type: ValueString, Flags: []string{"--exclude"}, Variadic: true, Source: src, Confidence: ConfidenceExact},
		flag("features", ValueString, "--features"),
		flag("all-features", ValueBool, "--all-features"),
		flag("no-default-features", ValueBool, "--no-default-features"),
		flag("target", ValueString, "--target"),
		flag("target-dir", ValuePath, "--target-dir"),
		flag("release", ValueBool, "--release"),
		flag("profile", ValueString, "--profile"),
		flag("jobs", ValueInt, "-j", "--jobs"),
		flag("locked", ValueBool, "--locked"),
		flag("offline", ValueBool, "--offline"),
		flag("frozen", ValueBool, "--frozen"),
	}

	var args []Argument
	switch command {
	case "build", "check":
		args = append(args, shared...)
	case "run":
		bin := flag("bin", ValueString, "--bin")
		if len(meta.Bins) > 0 {
			bin.Type = ValueEnum
			bin.Choices = append([]string(nil), meta.Bins...)
			bin.Source = Source{Kind: "cargo metadata", File: manifest, Pointer: "targets.bin"}
			bin.Confidence = ConfidenceExact
		}
		args = append(args, shared[0], bin)
		args = append(args, shared[1:]...)
		args = append(args, Argument{ID: "args", Name: "arguments", Kind: ArgumentPassThrough, Type: ValueUnknown, Variadic: true, Source: src, Confidence: ConfidenceExact})
	case "test":
		test := flag("test", ValueString, "--test")
		if len(meta.Tests) > 0 {
			test.Type = ValueEnum
			test.Choices = append([]string(nil), meta.Tests...)
			test.Source = Source{Kind: "cargo metadata", File: manifest, Pointer: "targets.test"}
		}
		args = append(args, shared[0], test)
		args = append(args, shared[1:]...)
		args = append(args, Argument{ID: "args", Name: "arguments", Kind: ArgumentPassThrough, Type: ValueUnknown, Variadic: true, Source: src, Confidence: ConfidenceExact})
	case "clean":
		args = append(args,
			flag("package", ValueString, "-p", "--package"),
			flag("target", ValueString, "--target"),
			flag("target-dir", ValuePath, "--target-dir"),
			flag("release", ValueBool, "--release"),
			flag("profile", ValueString, "--profile"),
		)
	case "version":
	}

	if len(meta.Packages) > 0 {
		for i := range args {
			if args[i].ID == "package" || args[i].ID == "exclude" {
				args[i].Type = ValueEnum
				args[i].Choices = append([]string(nil), meta.Packages...)
				args[i].Source = Source{Kind: "cargo metadata", File: manifest, Pointer: "workspace.packages"}
				args[i].Confidence = ConfidenceExact
			}
		}
	}
	if len(meta.Features) > 0 {
		for i := range args {
			if args[i].ID == "features" {
				args[i].Choices = append([]string(nil), meta.Features...)
				args[i].Source = Source{Kind: "cargo metadata", File: manifest, Pointer: "features"}
				args[i].Confidence = ConfidenceHigh
			}
		}
	}
	return args
}

func findCargoWorkspaceRoot(projectRoot string) string {
	for cur := projectRoot; ; cur = filepath.Dir(cur) {
		data, err := os.ReadFile(filepath.Join(cur, "Cargo.toml"))
		if err == nil && hasTOMLSection(data, "workspace") {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
	}
	return ""
}

func hasTOMLSection(data []byte, section string) bool {
	needle := "[" + section + "]"
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(strings.SplitN(line, "#", 2)[0]) == needle {
			return true
		}
	}
	return false
}

type cargoMetadataInfo struct {
	Packages []string
	Bins     []string
	Tests    []string
	Examples []string
	Benches  []string
	Features []string
}

type cargoMetadataJSON struct {
	Packages []struct {
		ID       string              `json:"id"`
		Name     string              `json:"name"`
		Features map[string][]string `json:"features"`
		Targets  []struct {
			Name string   `json:"name"`
			Kind []string `json:"kind"`
		} `json:"targets"`
	} `json:"packages"`
	WorkspaceMembers []string `json:"workspace_members"`
}

func readCargoMetadata(runner Runner, dir string) (cargoMetadataInfo, error) {
	if runner == nil {
		runner = execRunner{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out, err := runner.Run(ctx, dir, "cargo", "metadata", "--format-version", "1", "--no-deps")
	if err != nil {
		return cargoMetadataInfo{}, fmt.Errorf("cargo metadata: %w", err)
	}
	if len(out) > providerStdoutLimit {
		return cargoMetadataInfo{}, fmt.Errorf("cargo metadata output exceeded %d bytes", providerStdoutLimit)
	}

	var raw cargoMetadataJSON
	if err := json.Unmarshal(out, &raw); err != nil {
		return cargoMetadataInfo{}, err
	}
	members := map[string]bool{}
	for _, id := range raw.WorkspaceMembers {
		members[id] = true
	}
	sets := map[string]map[string]bool{
		"packages": {}, "bins": {}, "tests": {}, "examples": {}, "benches": {}, "features": {},
	}
	for _, pkg := range raw.Packages {
		if len(members) > 0 && !members[pkg.ID] {
			continue
		}
		sets["packages"][pkg.Name] = true
		for feature := range pkg.Features {
			sets["features"][feature] = true
		}
		for _, target := range pkg.Targets {
			for _, kind := range target.Kind {
				switch kind {
				case "bin":
					sets["bins"][target.Name] = true
				case "test":
					sets["tests"][target.Name] = true
				case "example":
					sets["examples"][target.Name] = true
				case "bench":
					sets["benches"][target.Name] = true
				}
			}
		}
	}
	return cargoMetadataInfo{
		Packages: sortedSet(sets["packages"]),
		Bins:     sortedSet(sets["bins"]),
		Tests:    sortedSet(sets["tests"]),
		Examples: sortedSet(sets["examples"]),
		Benches:  sortedSet(sets["benches"]),
		Features: sortedSet(sets["features"]),
	}, nil
}

func sortedSet(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
