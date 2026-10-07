package pkgmgr

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// Exercise installed tools with a controlled argv sink, without installing any
// packages. Missing tools are reported as skipped subtests in smaller CI images.
func TestNodeManagerActualArgv(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node unavailable")
	}
	for _, manager := range []string{"npm", "pnpm", "yarn", "bun"} {
		t.Run(manager, func(t *testing.T) {
			if _, err := exec.LookPath(manager); err != nil {
				t.Skip(manager + " unavailable")
			}
			dir := t.TempDir()
			manifest := nodeManifest{Scripts: map[string]string{"install": "node record.cjs"}}
			data, _ := json.Marshal(map[string]any{"scripts": manifest.Scripts})
			if err := os.WriteFile(filepath.Join(dir, "package.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "record.cjs"), []byte(`require('fs').writeFileSync('argv.json',JSON.stringify(process.argv.slice(2)))`), 0600); err != nil {
				t.Fatal(err)
			}
			commands, err := (nodeProvider{}).Commands(Context{}, Detection{ID: "node:" + manager, Root: dir, Data: nodeDetection{Manifest: manifest, Manager: manager}})
			if err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{{"dev", "--port", "3000", "argument with spaces"}, {}} {
				invocation, err := Resolve(commands[0], ArgumentValues{"args": args})
				if err != nil {
					t.Fatal(err)
				}
				// Installed package managers can start slowly on busy Windows runners
				// while the rest of the Go suite launches subprocesses in parallel.
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				cmd := exec.CommandContext(ctx, invocation.Program, invocation.Args...)
				cmd.Dir = invocation.Dir
				output, err := cmd.CombinedOutput()
				contextErr := ctx.Err()
				cancel()
				if err != nil {
					t.Fatalf("%s %v: %v (context: %v)\n%s", manager, invocation.Args, err, contextErr, output)
				}
				data, err := os.ReadFile(filepath.Join(dir, "argv.json"))
				if err != nil {
					t.Fatal(err)
				}
				var got []string
				if err := json.Unmarshal(data, &got); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, args) {
					t.Fatalf("argv=%#v want %#v", got, args)
				}
			}
		})
	}
}
