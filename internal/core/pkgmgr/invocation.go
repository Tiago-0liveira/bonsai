package pkgmgr

import (
	"fmt"
	"sort"
	"strconv"
)

// PassThroughMode controls how arbitrary trailing args are appended.
type PassThroughMode string

const (
	PassThroughNone       PassThroughMode = ""
	PassThroughAppend     PassThroughMode = "append"
	PassThroughDoubleDash PassThroughMode = "double-dash"
)

// InvocationSpec is a shell-free execution template.
type InvocationSpec struct {
	Program     string          `json:"program"`
	Prefix      []string        `json:"prefix,omitempty"`
	WorkingDir  string          `json:"working_dir"`
	PassThrough PassThroughMode `json:"pass_through,omitempty"`
}

// Invocation is the final program + argv + working directory to spawn.
type Invocation struct {
	Program string
	Args    []string
	Dir     string
}

// Resolve validates values against the known schema and constructs argv.
func Resolve(cmd Command, values ArgumentValues) (Invocation, error) {
	if cmd.Invocation.Program == "" {
		return Invocation{}, fmt.Errorf("pkgmgr: command %q has no program", cmd.ID)
	}
	args := append([]string(nil), cmd.Invocation.Prefix...)

	positionals := append([]Argument(nil), cmd.Args...)
	sort.SliceStable(positionals, func(i, j int) bool { return positionals[i].Position < positionals[j].Position })

	for _, arg := range cmd.Args {
		if arg.Kind != ArgumentFlag {
			continue
		}
		vals := values[arg.ID]
		if len(vals) == 0 {
			if arg.Required && arg.Default == nil {
				return Invocation{}, fmt.Errorf("pkgmgr: missing required argument %q", arg.ID)
			}
			if arg.Default != nil {
				vals = []string{*arg.Default}
			} else {
				continue
			}
		}
		if !arg.Variadic && len(vals) > 1 {
			return Invocation{}, fmt.Errorf("pkgmgr: argument %q is not variadic", arg.ID)
		}
		flag := ""
		if len(arg.Flags) > 0 {
			flag = arg.Flags[len(arg.Flags)-1]
		}
		if arg.Type == ValueBool {
			v, err := strconv.ParseBool(vals[0])
			if err != nil {
				return Invocation{}, fmt.Errorf("pkgmgr: %s: %w", arg.ID, err)
			}
			if v && flag != "" {
				args = append(args, flag)
			}
			continue
		}
		for _, v := range vals {
			if err := validateValue(arg, v); err != nil {
				return Invocation{}, err
			}
			if flag != "" {
				args = append(args, flag)
			}
			args = append(args, v)
		}
	}

	for _, arg := range positionals {
		if arg.Kind != ArgumentPositional {
			continue
		}
		vals := values[arg.ID]
		if len(vals) == 0 && arg.Default != nil {
			vals = []string{*arg.Default}
		}
		if len(vals) == 0 && arg.Required {
			return Invocation{}, fmt.Errorf("pkgmgr: missing required argument %q", arg.ID)
		}
		if !arg.Variadic && len(vals) > 1 {
			return Invocation{}, fmt.Errorf("pkgmgr: argument %q is not variadic", arg.ID)
		}
		for _, v := range vals {
			if err := validateValue(arg, v); err != nil {
				return Invocation{}, err
			}
			args = append(args, v)
		}
	}

	for _, arg := range cmd.Args {
		if arg.Kind != ArgumentPassThrough {
			continue
		}
		vals := values[arg.ID]
		if len(vals) == 0 {
			continue
		}
		if cmd.Invocation.PassThrough == PassThroughDoubleDash {
			args = append(args, "--")
		}
		args = append(args, vals...)
	}
	return Invocation{Program: cmd.Invocation.Program, Args: args, Dir: cmd.Invocation.WorkingDir}, nil
}

func validateValue(arg Argument, value string) error {
	switch arg.Type {
	case ValueInt:
		if _, err := strconv.Atoi(value); err != nil {
			return fmt.Errorf("pkgmgr: argument %q expects int", arg.ID)
		}
	case ValueFloat:
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			return fmt.Errorf("pkgmgr: argument %q expects float", arg.ID)
		}
	case ValueEnum:
		if len(arg.Choices) > 0 {
			for _, choice := range arg.Choices {
				if value == choice {
					return nil
				}
			}
			return fmt.Errorf("pkgmgr: argument %q value %q is not an allowed choice", arg.ID, value)
		}
	}
	return nil
}
