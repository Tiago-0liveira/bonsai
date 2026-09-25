package pkgmgr

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMakeParsing(t *testing.T) {
	dir := t.TempDir()
	makefile := "" +
		"# top comment\r\n" +
		".PHONY: build test\r\n" +
		"build:\r\n" +
		"\t@echo build: not-a-target\r\n" +
		"test: build\r\n" +
		"deploy-prod:\r\n" +
		"foo.bar:\r\n" +
		"foo_bar:\r\n" +
		"build: duplicate\r\n" +
		"%.o: %.c\r\n" +
		"VAR := value\r\n" +
		"VAR2 = value\r\n" +
		"VAR3 ?= value\r\n" +
		"VAR4 += value"
	write(t, dir, "Makefile", makefile)

	project, err := Discover(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, cmd := range project.Commands {
		if cmd.Provider == "make" {
			got = append(got, cmd.Name)
		}
	}
	want := []string{"build", "test", "deploy-prod", "foo.bar", "foo_bar"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("make targets = %v, want %v", got, want)
	}

	build := commandByID(t, project, "make:target:build")
	if build.Source.Line != 3 || build.Source.File != filepath.Join(dir, "Makefile") {
		t.Fatalf("build provenance = %+v", build.Source)
	}
	if build.Description != "Make phony target" {
		t.Fatalf("build description = %q", build.Description)
	}
	if test := commandByID(t, project, "make:target:test"); test.Description != "Make phony target" {
		t.Fatalf("test description = %q", test.Description)
	}
	for _, skipped := range []string{"make:target:.PHONY", "make:target:%.o", "make:target:VAR"} {
		for _, cmd := range project.Commands {
			if cmd.ID == skipped {
				t.Fatalf("unexpected command %q", skipped)
			}
		}
	}
}

func TestMakeLongLineAndNoTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	longPrereqs := strings.Repeat("dep ", 20000)
	write(t, dir, "Makefile", "long: "+longPrereqs+"\nfinal:")

	project, err := Discover(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	commandByID(t, project, "make:target:long")
	commandByID(t, project, "make:target:final")
}

func TestMakeDiscoveryNeverExecutesShellExpressions(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "SHOULD_NOT_EXIST")
	makefile := "X := $(shell touch " + marker + ")\n\nbuild:\n\t@echo build\n"
	write(t, dir, "Makefile", makefile)

	project, err := Discover(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	commandByID(t, project, "make:target:build")
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("Make discovery executed project code; marker stat error = %v", err)
	}
	for _, cmd := range project.Commands {
		if cmd.Name == "X" {
			t.Fatalf("variable assignment became target: %+v", cmd)
		}
	}
}

func TestMakeCommandOrderingDeterministic(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Makefile", "z:\n\t@true\na:\n\t@true\nm:\n\t@true\n")
	first, err := Discover(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Discover(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Commands, second.Commands) {
		t.Fatalf("Make discovery not deterministic:\nfirst=%+v\nsecond=%+v", first.Commands, second.Commands)
	}
}
