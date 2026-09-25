package pkgmgr

import (
	"fmt"
	"os"
	"path/filepath"
)

type goProvider struct{}

func (goProvider) ID() string { return "go" }

type goDetection struct {
	ManifestPath  string
	WorkspacePath string
}

func (goProvider) Detect(ctx Context) (Detection, error) {
	root, manifest := findProjectFileWithin(ctx.Location.InputDir, ctx.Location.RepositoryRoot, ctx.Options.searchDepth(), "go.mod", "go.work")
	if root == "" {
		return Detection{}, nil
	}

	workspaceRoot := ""
	workspacePath := ""
	if filepath.Base(manifest) == "go.work" {
		workspaceRoot, workspacePath = root, manifest
	} else if wr, wp := findUp(root, "go.work"); wr != "" {
		workspaceRoot, workspacePath = wr, wp
	}

	return Detection{
		Applicable:    true,
		ID:            "go",
		Name:          "go",
		Root:          root,
		WorkspaceRoot: workspaceRoot,
		Data: goDetection{
			ManifestPath:  manifest,
			WorkspacePath: workspacePath,
		},
	}, nil
}

var goBuiltins = []struct {
	id     string
	name   string
	prefix []string
}{
	{"build", "build", []string{"build", "./..."}},
	{"clean", "clean", []string{"clean"}},
	{"fmt", "fmt", []string{"fmt", "./..."}},
	{"generate", "generate", []string{"generate", "./..."}},
	{"mod-download", "mod download", []string{"mod", "download"}},
	{"mod-tidy", "mod tidy", []string{"mod", "tidy"}},
	{"run", "run", []string{"run", "."}},
	{"test", "test", []string{"test", "./..."}},
	{"vet", "vet", []string{"vet", "./..."}},
}

func (goProvider) Commands(_ Context, detection Detection) ([]Command, error) {
	d, ok := detection.Data.(goDetection)
	if !ok {
		return nil, fmt.Errorf("pkgmgr: invalid go detection data")
	}
	commands := make([]Command, 0, len(goBuiltins))
	for _, builtin := range goBuiltins {
		src := Source{Kind: "catalog", File: "Go builtin catalog", Pointer: builtin.name}
		commands = append(commands, Command{
			ID:          "go:builtin:" + builtin.id,
			Name:        builtin.name,
			Description: "go " + builtin.name,
			Provider:    detection.ID,
			Kind:        CommandBuiltin,
			Args: []Argument{{
				ID:         "args",
				Name:       "arguments",
				Kind:       ArgumentPassThrough,
				Type:       ValueUnknown,
				Variadic:   true,
				Source:     src,
				Confidence: ConfidenceExact,
			}},
			Invocation: InvocationSpec{
				Program:     "go",
				Prefix:      append([]string(nil), builtin.prefix...),
				WorkingDir:  detection.Root,
				PassThrough: PassThroughAppend,
			},
			Source:     Source{Kind: "go manifest", File: d.ManifestPath, Pointer: builtin.name},
			Confidence: ConfidenceExact,
		})
	}
	return commands, nil
}

func (goProvider) FingerprintInputs(_ Context, detection Detection) ([]FingerprintInput, error) {
	d, ok := detection.Data.(goDetection)
	if !ok {
		return nil, fmt.Errorf("pkgmgr: invalid go detection data")
	}
	var paths []string
	paths = append(paths, d.ManifestPath)
	for _, base := range uniqueDirs(detection.Root, detection.WorkspaceRoot) {
		for _, name := range []string{"go.mod", "go.sum", "go.work", "go.work.sum"} {
			path := filepath.Join(base, name)
			if _, err := os.Stat(path); err == nil {
				paths = append(paths, path)
			}
		}
	}
	base := detection.WorkspaceRoot
	if base == "" {
		base = detection.Root
	}
	return fileInputs("go", base, detection.Root, paths)
}
