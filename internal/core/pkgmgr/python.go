package pkgmgr

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

type pythonProvider struct{}

func (pythonProvider) ID() string { return "python" }

type pythonDetection struct {
	ManifestPath string
	Manager      string
	Scripts      map[string]string
}

type pythonPyProject struct {
	Project struct {
		Scripts map[string]string `toml:"scripts"`
	} `toml:"project"`
	Tool struct {
		Poetry struct {
			Scripts map[string]any `toml:"scripts"`
		} `toml:"poetry"`
	} `toml:"tool"`
}

func (pythonProvider) Detect(ctx Context) (Detection, error) {
	root, manifest := findProjectFileWithin(
		ctx.Location.InputDir,
		ctx.Location.RepositoryRoot,
		ctx.Options.searchDepth(),
		"pyproject.toml", "Pipfile", "requirements.txt", "setup.py", "uv.lock", "poetry.lock",
	)
	if root == "" {
		return Detection{}, nil
	}

	manager := detectPythonManager(root)
	scripts, err := readPythonScripts(filepath.Join(root, "pyproject.toml"))
	if err != nil {
		return Detection{}, fmt.Errorf("read python project scripts: %w", err)
	}
	return Detection{
		Applicable: true,
		ID:         "python:" + manager,
		Name:       manager,
		Root:       root,
		Data: pythonDetection{
			ManifestPath: manifest,
			Manager:      manager,
			Scripts:      scripts,
		},
	}, nil
}

func detectPythonManager(root string) string {
	if fileExists(filepath.Join(root, "uv.lock")) {
		return "uv"
	}
	if fileExists(filepath.Join(root, "poetry.lock")) {
		return "poetry"
	}
	if fileExists(filepath.Join(root, "Pipfile")) {
		return "pipenv"
	}
	if data, err := os.ReadFile(filepath.Join(root, "pyproject.toml")); err == nil {
		text := string(data)
		if strings.Contains(text, "[tool.uv") {
			return "uv"
		}
		if strings.Contains(text, "[tool.poetry") {
			return "poetry"
		}
	}
	return "python"
}

func readPythonScripts(path string) (map[string]string, error) {
	scripts := map[string]string{}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return scripts, nil
	}
	if err != nil {
		return nil, err
	}
	var project pythonPyProject
	if err := toml.Unmarshal(data, &project); err != nil {
		return nil, err
	}
	for name, target := range project.Project.Scripts {
		scripts[name] = target
	}
	for name, raw := range project.Tool.Poetry.Scripts {
		if target, ok := raw.(string); ok {
			scripts[name] = target
		}
	}
	return scripts, nil
}

func (pythonProvider) Commands(_ Context, detection Detection) ([]Command, error) {
	d, ok := detection.Data.(pythonDetection)
	if !ok {
		return nil, fmt.Errorf("pkgmgr: invalid python detection data")
	}

	names := make([]string, 0, len(d.Scripts))
	for name := range d.Scripts {
		names = append(names, name)
	}
	sort.Strings(names)

	commands := make([]Command, 0, len(names)+3)
	for _, name := range names {
		program, prefix := pythonManagedCommand(d.Manager, name)
		src := Source{Kind: "pyproject.toml", File: filepath.Join(detection.Root, "pyproject.toml"), Pointer: "scripts." + name}
		commands = append(commands, Command{
			ID:          "python:script:" + name,
			Name:        name,
			Description: d.Scripts[name],
			Provider:    detection.ID,
			Kind:        CommandProject,
			Args: []Argument{{
				ID: "args", Name: "arguments", Kind: ArgumentPassThrough, Type: ValueUnknown,
				Variadic: true, Source: src, Confidence: ConfidenceExact,
			}},
			Invocation: InvocationSpec{
				Program: program, Prefix: prefix, WorkingDir: detection.Root, PassThrough: PassThroughAppend,
			},
			Source: src, Confidence: ConfidenceExact, Raw: d.Scripts[name],
		})
	}

	for _, builtin := range []string{"install", "test", "unittest"} {
		if _, exists := d.Scripts[builtin]; exists {
			continue
		}
		program, prefix := pythonBuiltinInvocation(d.Manager, builtin, detection.Root)
		src := Source{Kind: "catalog", File: "Python builtin catalog", Pointer: builtin}
		commands = append(commands, Command{
			ID:          "python:builtin:" + builtin,
			Name:        builtin,
			Description: "Python " + builtin,
			Provider:    detection.ID,
			Kind:        CommandBuiltin,
			Args: []Argument{{
				ID: "args", Name: "arguments", Kind: ArgumentPassThrough, Type: ValueUnknown,
				Variadic: true, Source: src, Confidence: ConfidenceHigh,
			}},
			Invocation: InvocationSpec{
				Program: program, Prefix: prefix, WorkingDir: detection.Root, PassThrough: PassThroughAppend,
			},
			Source: src, Confidence: ConfidenceHigh,
		})
	}
	return commands, nil
}

func pythonManagedCommand(manager, name string) (string, []string) {
	switch manager {
	case "uv":
		return "uv", []string{"run", name}
	case "poetry":
		return "poetry", []string{"run", name}
	case "pipenv":
		return "pipenv", []string{"run", name}
	default:
		return name, nil
	}
}

func pythonBuiltinInvocation(manager, name, root string) (string, []string) {
	if name == "install" {
		switch manager {
		case "uv":
			return "uv", []string{"sync"}
		case "poetry":
			return "poetry", []string{"install"}
		case "pipenv":
			return "pipenv", []string{"install"}
		default:
			if fileExists(filepath.Join(root, "requirements.txt")) {
				return "python", []string{"-m", "pip", "install", "-r", "requirements.txt"}
			}
			return "python", []string{"-m", "pip", "install", "-e", "."}
		}
	}

	module := "pytest"
	if name == "unittest" {
		module = "unittest"
	}
	switch manager {
	case "uv":
		return "uv", []string{"run", "python", "-m", module}
	case "poetry":
		return "poetry", []string{"run", "python", "-m", module}
	case "pipenv":
		return "pipenv", []string{"run", "python", "-m", module}
	default:
		return "python", []string{"-m", module}
	}
}

func (pythonProvider) FingerprintInputs(_ Context, detection Detection) ([]FingerprintInput, error) {
	d, ok := detection.Data.(pythonDetection)
	if !ok {
		return nil, fmt.Errorf("pkgmgr: invalid python detection data")
	}
	paths := []string{d.ManifestPath}
	for _, name := range []string{
		"pyproject.toml", "uv.lock", "poetry.lock", "Pipfile", "Pipfile.lock",
		"requirements.txt", "requirements-dev.txt", "setup.py", "setup.cfg",
	} {
		path := filepath.Join(detection.Root, name)
		if fileExists(path) {
			paths = append(paths, path)
		}
	}
	return fileInputs("python", "", detection.Root, paths)
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
