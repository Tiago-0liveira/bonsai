package pkgmgr

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
		if !validCommandID(override.ID) {
			return nil, fmt.Errorf("pkgmgr: invalid override command id %q", override.ID)
		}
		if err := validateArgumentOverrides(override.Args); err != nil {
			return nil, fmt.Errorf("pkgmgr: override %q: %w", override.ID, err)
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
				merged, err := mergeOverrideArgs(cmd.Args, override.Args, src)
				if err != nil {
					return nil, fmt.Errorf("pkgmgr: override %q: %w", override.ID, err)
				}
				cmd.Args = merged
			}
			if override.Command != nil {
				if override.Command.Program == "" {
					return nil, fmt.Errorf("pkgmgr: override %q command.program is required", override.ID)
				}
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
		args, err := mergeOverrideArgs(nil, override.Args, src)
		if err != nil {
			return nil, fmt.Errorf("pkgmgr: override %q: %w", override.ID, err)
		}
		cmd := Command{
			ID:          override.ID,
			Name:        name,
			Description: override.Description,
			Provider:    "override",
			ProjectRoot: projectRoot,
			Kind:        CommandOverride,
			Args:        args,
			Invocation:  overrideInvocation(*override.Command, projectRoot),
			Source:      src,
			Confidence:  ConfidenceExact,
		}
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

func validCommandID(id string) bool {
	return id != "" && strings.TrimSpace(id) == id && !strings.ContainsAny(id, " \t\r\n")
}

func validateArgumentOverrides(overrides []argumentOverride) error {
	seen := map[string]bool{}
	positions := map[int]string{}
	for _, override := range overrides {
		if override.ID == "" {
			return fmt.Errorf("argument id is required")
		}
		if seen[override.ID] {
			return fmt.Errorf("duplicate argument id %q", override.ID)
		}
		seen[override.ID] = true

		if override.Kind != "" {
			switch ArgumentKind(override.Kind) {
			case ArgumentFlag, ArgumentPositional, ArgumentPassThrough:
			default:
				return fmt.Errorf("argument %q has invalid kind %q", override.ID, override.Kind)
			}
		}
		if override.Type != "" {
			switch ValueType(override.Type) {
			case ValueBool, ValueString, ValueInt, ValueFloat, ValuePath, ValueEnum, ValueUnknown:
			default:
				return fmt.Errorf("argument %q has invalid type %q", override.ID, override.Type)
			}
		}
		if ArgumentKind(override.Kind) == ArgumentPositional {
			if override.Position < 0 {
				return fmt.Errorf("argument %q has invalid position %d", override.ID, override.Position)
			}
			if other, exists := positions[override.Position]; exists {
				return fmt.Errorf("arguments %q and %q share position %d", other, override.ID, override.Position)
			}
			positions[override.Position] = override.ID
		}
		if ValueType(override.Type) == ValueEnum {
			if len(override.Choices) == 0 {
				return fmt.Errorf("enum argument %q requires choices", override.ID)
			}
			if override.Default != nil && !containsString(override.Choices, *override.Default) {
				return fmt.Errorf("enum argument %q default %q is not a choice", override.ID, *override.Default)
			}
		}
	}
	return nil
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

func mergeOverrideArgs(existing []Argument, overrides []argumentOverride, src Source) ([]Argument, error) {
	if err := validateArgumentOverrides(overrides); err != nil {
		return nil, err
	}
	out := append([]Argument(nil), existing...)
	byID := make(map[string]int, len(out))
	for i := range out {
		byID[out[i].ID] = i
	}
	for _, override := range overrides {
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
		if override.Kind == string(ArgumentPositional) || override.Position != 0 {
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
	return out, nil
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
