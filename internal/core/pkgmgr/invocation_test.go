package pkgmgr

import (
	"reflect"
	"strings"
	"testing"
)

func TestResolveArgumentValidation(t *testing.T) {
	base := Command{
		ID: "test",
		Invocation: InvocationSpec{Program: "tool", Prefix: []string{"run"}},
		Args: []Argument{
			{ID: "required", Kind: ArgumentPositional, Type: ValueString, Required: true, Position: 0},
			{ID: "count", Kind: ArgumentFlag, Type: ValueInt, Flags: []string{"--count"}},
			{ID: "ratio", Kind: ArgumentFlag, Type: ValueFloat, Flags: []string{"--ratio"}},
			{ID: "mode", Kind: ArgumentFlag, Type: ValueEnum, Flags: []string{"--mode"}, Choices: []string{"fast", "safe"}},
			{ID: "tag", Kind: ArgumentFlag, Type: ValueString, Flags: []string{"--tag"}, Variadic: true},
			{ID: "verbose", Kind: ArgumentFlag, Type: ValueBool, Flags: []string{"-v", "--verbose"}},
			{ID: "optional", Kind: ArgumentFlag, Type: ValueString, Flags: []string{"--optional"}},
		},
	}

	tests := []struct {
		name   string
		values ArgumentValues
		want   []string
		err    string
	}{
		{name: "required missing", values: ArgumentValues{}, err: "missing required"},
		{name: "invalid int", values: ArgumentValues{"required": {"x"}, "count": {"nope"}}, err: "expects int"},
		{name: "invalid float", values: ArgumentValues{"required": {"x"}, "ratio": {"nope"}}, err: "expects float"},
		{name: "invalid enum", values: ArgumentValues{"required": {"x"}, "mode": {"turbo"}}, err: "not an allowed choice"},
		{name: "unknown argument", values: ArgumentValues{"required": {"x"}, "wat": {"1"}}, err: "unknown argument"},
		{name: "duplicate non variadic", values: ArgumentValues{"required": {"x"}, "count": {"1", "2"}}, err: "not variadic"},
		{name: "optional omitted", values: ArgumentValues{"required": {"x"}}, want: []string{"run", "x"}},
		{name: "variadic", values: ArgumentValues{"required": {"x"}, "tag": {"a", "b"}}, want: []string{"run", "--tag", "a", "--tag", "b", "x"}},
		{name: "bool on", values: ArgumentValues{"required": {"x"}, "verbose": {"true"}}, want: []string{"run", "--verbose", "x"}},
		{name: "bool off", values: ArgumentValues{"required": {"x"}, "verbose": {"false"}}, want: []string{"run", "x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve(base, tt.values)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("error = %v, want substring %q", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Args, tt.want) {
				t.Fatalf("args = %#v, want %#v", got.Args, tt.want)
			}
		})
	}
}

func TestResolveExactNodeInvocations(t *testing.T) {
	for _, tc := range []struct {
		name        string
		packageJSON string
		lock        string
		program     string
	}{
		{name: "npm", packageJSON: `{"scripts":{"dev":"vite"}}`, lock: "package-lock.json", program: "npm"},
		{name: "pnpm", packageJSON: `{"scripts":{"dev":"vite"}}`, lock: "pnpm-lock.yaml", program: "pnpm"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, "package.json", tc.packageJSON)
			write(t, dir, tc.lock, "")
			project, err := Discover(dir, Options{})
			if err != nil {
				t.Fatal(err)
			}
			cmd := commandByID(t, project, "node:script:dev")
			got, err := Resolve(cmd, ArgumentValues{"args": {"--port", "3000"}})
			if err != nil {
				t.Fatal(err)
			}
			want := Invocation{Program: tc.program, Args: []string{"run", "dev", "--", "--port", "3000"}, Dir: dir}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Resolve = %#v, want %#v", got, want)
			}
		})
	}
}

func TestResolveExactCargoInvocation(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Cargo.toml", "[package]\nname = \"x\"\nversion = \"0.1.0\"\n")
	project, err := Discover(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	cmd := commandByID(t, project, "cargo:builtin:run")
	got, err := Resolve(cmd, ArgumentValues{
		"package": {"api"},
		"bin":     {"server"},
		"release": {"true"},
		"args":    {"--port", "8080"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := Invocation{
		Program: "cargo",
		Args: []string{
			"run",
			"--package", "api",
			"--bin", "server",
			"--release",
			"--",
			"--port", "8080",
		},
		Dir: dir,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve = %#v, want %#v", got, want)
	}
}

func TestResolveExactMakeInvocation(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Makefile", "deploy:\n\t@echo deploy\n")
	project, err := Discover(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Resolve(commandByID(t, project, "make:target:deploy"), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := Invocation{Program: "make", Args: []string{"deploy"}, Dir: dir}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve = %#v, want %#v", got, want)
	}
}

func TestResolvePreservesValuesAsArgvEntries(t *testing.T) {
	cmd := Command{
		ID: "safe",
		Invocation: InvocationSpec{
			Program:     "tool",
			Prefix:      []string{"run"},
			WorkingDir:  "C:\\repo",
			PassThrough: PassThroughDoubleDash,
		},
		Args: []Argument{{ID: "args", Kind: ArgumentPassThrough, Type: ValueUnknown, Variadic: true}},
	}
	values := []string{"hello world", "日本語", `"quoted value"`, `C:\repo\a b\file.txt`, "; touch nope"}
	got, err := Resolve(cmd, ArgumentValues{"args": values})
	if err != nil {
		t.Fatal(err)
	}
	want := append([]string{"run", "--"}, values...)
	if !reflect.DeepEqual(got.Args, want) {
		t.Fatalf("args = %#v, want %#v", got.Args, want)
	}
}

func TestResolveEmptyPassThroughDoesNotAddSeparator(t *testing.T) {
	cmd := Command{
		ID: "safe",
		Invocation: InvocationSpec{Program: "tool", Prefix: []string{"run"}, PassThrough: PassThroughDoubleDash},
		Args: []Argument{{ID: "args", Kind: ArgumentPassThrough, Type: ValueUnknown, Variadic: true}},
	}
	got, err := Resolve(cmd, ArgumentValues{"args": {}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Args, []string{"run"}) {
		t.Fatalf("args = %#v, want no separator", got.Args)
	}
}
