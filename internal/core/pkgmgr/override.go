package pkgmgr

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

type pkgmgrOverrides struct {
	Commands []commandOverride `mapstructure:"commands"`
}

type commandOverride struct {
	ID          string              `mapstructure:"id"`
	Name        string              `mapstructure:"name"`
	Description string              `mapstructure:"description"`
	Hide        bool                `mapstructure:"hide"`
	Args        []argumentOverride  `mapstructure:"args"`
	Command     *invocationOverride `mapstructure:"command"`
}

type argumentOverride struct {
	ID          string   `mapstructure:"id"`
	Name        string   `mapstructure:"name"`
	Description string   `mapstructure:"description"`
	Kind        string   `mapstructure:"kind"`
	Type        string   `mapstructure:"type"`
	Required    bool     `mapstructure:"required"`
	Variadic    bool     `mapstructure:"variadic"`
	Flags       []string `mapstructure:"flags"`
	Position    int      `mapstructure:"position"`
	Default     *string  `mapstructure:"default"`
	Choices     []string `mapstructure:"choices"`
}

type invocationOverride struct {
	Program    string   `mapstructure:"program"`
	Args       []string `mapstructure:"args"`
	WorkingDir string   `mapstructure:"working_dir"`
}

func loadOverrides(loc Location) (pkgmgrOverrides, string, []byte, error) {
	path := findOverridePath(loc)
	if path == "" {
		return pkgmgrOverrides{}, "", nil, nil
	}
	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return pkgmgrOverrides{}, path, nil, err
	}
	sub := v.Sub("pkgmgr")
	if sub == nil {
		return pkgmgrOverrides{}, path, nil, nil
	}
	var out pkgmgrOverrides
	if err := sub.Unmarshal(&out); err != nil {
		return pkgmgrOverrides{}, path, nil, err
	}
	canonical, err := json.Marshal(sub.AllSettings())
	if err != nil {
		return pkgmgrOverrides{}, path, nil, err
	}
	return out, path, canonical, nil
}

func findOverridePath(loc Location) string {
	start := loc.ProjectRoot
	if start == "" {
		start = loc.InputDir
	}
	boundary := loc.ProjectRoot
	if loc.WorkspaceRoot != "" {
		boundary = loc.WorkspaceRoot
	}
	if loc.RepositoryRoot != "" {
		boundary = loc.RepositoryRoot
	}
	for cur := start; ; cur = filepath.Dir(cur) {
		path := filepath.Join(cur, ".bonsai.yaml")
		if _, err := os.Stat(path); err == nil {
			return path
		}
		if filepath.Clean(cur) == filepath.Clean(boundary) {
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
	}
	return ""
}

func applyOverrides(commands []Command, cfg pkgmgrOverrides, path, projectRoot string) ([]Command, error) {
	if len(cfg.Commands) == 0 {
		return commands, nil
	}
	byID := make(map[string]int, len(commands))
	for i := range commands {
		byID[commands[i].ID] = i
	}
	hidden := map[string]bool{}
	for _, override := range cfg.Commands {
		if override.ID == "" {
			return nil, fmt.Errorf("pkgmgr: override command id is required")
		}
		src := Source{Kind: "override", File: path, Pointer: "pkgmgr.commands." + override.ID}
		if idx, ok := byID[override.ID]; ok {
			if override.Hide {
				hidden[override.ID] = true
				continue
			}
			cmd := &commands[idx]
			if override.Name != "" {
				cmd.Name = override.Name
			}
			if override.Description != "" {
				cmd.Description = override.Description
			}
			if len(override.Args) > 0 {
				cmd.Args = mergeOverrideArgs(cmd.Args, override.Args, src)
			}
			if override.Command != nil {
				cmd.Invocation = overrideInvocation(*override.Command, projectRoot)
			}
			cmd.Source = src
			cmd.Confidence = ConfidenceExact
			continue
		}
		if override.Hide {
			continue
		}
		if override.Command == nil || override.Command.Program == "" {
			return nil, fmt.Errorf("pkgmgr: custom command %q requires command.program", override.ID)
		}
		name := override.Name
		if name == "" {
			name = override.ID
		}
		cmd := Command{
			ID:          override.ID,
			Name:        name,
			Description: override.Description,
			Provider:    "override",
			Kind:        CommandOverride,
			Invocation:  overrideInvocation(*override.Command, projectRoot),
			Source:      src,
			Confidence:  ConfidenceExact,
		}
		cmd.Args = mergeOverrideArgs(nil, override.Args, src)
		byID[cmd.ID] = len(commands)
		commands = append(commands, cmd)
	}
	if len(hidden) == 0 {
		return commands, nil
	}
	out := commands[:0]
	for _, cmd := range commands {
		if !hidden[cmd.ID] {
			out = append(out, cmd)
		}
	}
	return out, nil
}

func overrideInvocation(in invocationOverride, root string) InvocationSpec {
	dir := in.WorkingDir
	if dir == "" {
		dir = root
	} else if !filepath.IsAbs(dir) {
		dir = filepath.Join(root, dir)
	}
	return InvocationSpec{Program: in.Program, Prefix: append([]string(nil), in.Args...), WorkingDir: filepath.Clean(dir)}
}

func mergeOverrideArgs(existing []Argument, overrides []argumentOverride, src Source) []Argument {
	out := append([]Argument(nil), existing...)
	byID := make(map[string]int, len(out))
	for i := range out {
		byID[out[i].ID] = i
	}
	for _, override := range overrides {
		if override.ID == "" {
			continue
		}
		idx, ok := byID[override.ID]
		if !ok {
			out = append(out, Argument{ID: override.ID, Kind: ArgumentFlag, Type: ValueUnknown})
			idx = len(out) - 1
			byID[override.ID] = idx
		}
		arg := &out[idx]
		if override.Name != "" {
			arg.Name = override.Name
		}
		if override.Description != "" {
			arg.Description = override.Description
		}
		if override.Kind != "" {
			arg.Kind = ArgumentKind(override.Kind)
		}
		if override.Type != "" {
			arg.Type = ValueType(override.Type)
		}
		if len(override.Flags) > 0 {
			arg.Flags = append([]string(nil), override.Flags...)
		}
		if override.Position != 0 {
			arg.Position = override.Position
		}
		if override.Default != nil {
			v := *override.Default
			arg.Default = &v
		}
		if len(override.Choices) > 0 {
			arg.Choices = append([]string(nil), override.Choices...)
		}
		arg.Required = arg.Required || override.Required
		arg.Variadic = arg.Variadic || override.Variadic
		arg.Source = src
		arg.Confidence = ConfidenceExact
	}
	return out
}
