package pkgmgr

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func FuzzParsePackageJSON(f *testing.F) {
	f.Add([]byte(`{"scripts":{"dev":"vite"},"packageManager":"pnpm@10.0.0"}`))
	f.Add([]byte("{"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		dir := t.TempDir()
		path := filepath.Join(dir, "package.json")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		a, errA := readNodeManifest(path)
		b, errB := readNodeManifest(path)
		if (errA == nil) != (errB == nil) {
			t.Fatalf("parse error nondeterminism: %v vs %v", errA, errB)
		}
		if errA == nil && !reflect.DeepEqual(a, b) {
			t.Fatalf("parse nondeterminism: %+v vs %+v", a, b)
		}
	})
}

func FuzzParseMakeTargets(f *testing.F) {
	f.Add([]byte("build:\n\t@echo build\n"))
	f.Add([]byte("VAR := $(shell echo nope)\n%.o: %.c\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		dir := t.TempDir()
		path := filepath.Join(dir, "Makefile")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		a, errA := parseMakefile(path)
		b, errB := parseMakefile(path)
		if (errA == nil) != (errB == nil) {
			t.Fatalf("parse error nondeterminism: %v vs %v", errA, errB)
		}
		if errA == nil && !reflect.DeepEqual(a, b) {
			t.Fatalf("parse nondeterminism: %+v vs %+v", a, b)
		}
	})
}

func FuzzParsePackageManagerField(f *testing.F) {
	for _, seed := range []string{"npm@11.0.0", "pnpm@10.0.0", "yarn@4.0.0", "bun@1.2.0", "", "wat@1"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		a := managerFromPackageManager(value)
		b := managerFromPackageManager(value)
		if a != b {
			t.Fatalf("manager detection nondeterministic: %q != %q", a, b)
		}
	})
}

func FuzzResolveInvocation(f *testing.F) {
	f.Add("hello world", "--flag", "日本語")
	f.Fuzz(func(t *testing.T, a, b, c string) {
		cmd := Command{
			ID:         "fuzz",
			Invocation: InvocationSpec{Program: "tool", Prefix: []string{"run"}, PassThrough: PassThroughDoubleDash},
			Args:       []Argument{{ID: "args", Kind: ArgumentPassThrough, Type: ValueUnknown, Variadic: true}},
		}
		first, err := Resolve(cmd, ArgumentValues{"args": {a, b, c}})
		if err != nil {
			t.Fatal(err)
		}
		second, err := Resolve(cmd, ArgumentValues{"args": {a, b, c}})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("Resolve nondeterministic: %#v vs %#v", first, second)
		}
	})
}

func FuzzFingerprintInputs(f *testing.F) {
	f.Add("a", "one", "b", "two")
	f.Fuzz(func(t *testing.T, nameA, dataA, nameB, dataB string) {
		a := []FingerprintInput{{Name: nameA, Content: []byte(dataA)}, {Name: nameB, Content: []byte(dataB)}}
		b := []FingerprintInput{a[1], a[0]}
		first := computeFingerprint([]string{"node:npm", "make"}, a)
		second := computeFingerprint([]string{"make", "node:npm"}, b)
		if first != second {
			t.Fatalf("fingerprint depends on enumeration order: %s != %s", first, second)
		}
	})
}
