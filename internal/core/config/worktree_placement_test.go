package config

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fakeLaunchPlacementRepo(t *testing.T) (string, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(root, "project")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	c := exec.Command("git", "init", "-b", "main")
	c.Dir = repo
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git init: %s %v", out, err)
	}
	path := filepath.Join(t.TempDir(), "roots.json")
	if _, err := UpdateProjectRoots(path, "add", 0, root, ""); err != nil {
		t.Fatal(err)
	}
	return path, repo
}
func fakeLaunchWriteConfig(t *testing.T, repo, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, ".bonsai.yaml"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestWorktreePlacementPreviewDefaultCustomAndSlashNormalization(t *testing.T) {
	for _, tc := range []struct{ name, config, branch, suffix string }{{"default", "", "feature/api", "project-feature-api"}, {"custom root", "worktree:\n  root: ../isolated\n", "feature/api", "project-feature-api"}, {"template", "worktree:\n  root: ../isolated\n  path_template: '{branch}'\n", "feature/api", "feature-api"}} {
		t.Run(tc.name, func(t *testing.T) {
			settings, repo := fakeLaunchPlacementRepo(t)
			if tc.config != "" {
				fakeLaunchWriteConfig(t, repo, tc.config)
			}
			before, _ := os.ReadFile(settings)
			p, err := PreviewWorktreePlacement(context.Background(), settings, repo, tc.branch)
			if err != nil {
				t.Fatal(err)
			}
			wantRoot := filepath.Join(filepath.Dir(repo), ".bonsai", "worktrees", ProjectID(repo))
			if tc.config != "" {
				wantRoot = filepath.Join(filepath.Dir(repo), "isolated")
			}
			if p.Path != filepath.Join(wantRoot, tc.suffix) || p.Root != wantRoot || p.Revision != 1 || len(p.ConfigHash) != 64 {
				t.Fatalf("placement=%+v root=%s", p, wantRoot)
			}
			if _, err := os.Lstat(p.Root); !os.IsNotExist(err) {
				t.Fatalf("Preview created root: %v", err)
			}
			after, _ := os.ReadFile(settings)
			if !bytes.Equal(before, after) {
				t.Fatal("Preview changed root configuration")
			}
			same, err := PreviewWorktreePlacement(context.Background(), settings, repo, strings.ReplaceAll(tc.branch, "/", "-"))
			if err != nil || same.Path != p.Path {
				t.Fatalf("slash normalization differs: %+v %v", same, err)
			}
		})
	}
}
func TestWorktreePlacementRejectsEscapesLinksAndOccupiedDestinations(t *testing.T) {
	for _, mode := range []string{"relative escape", "absolute escape", "root destination", "main destination", "ancestor symlink", "dangling destination", "occupied destination", "missing canonical root"} {
		t.Run(mode, func(t *testing.T) {
			settings, repo := fakeLaunchPlacementRepo(t)
			branch := "feature"
			switch mode {
			case "relative escape":
				fakeLaunchWriteConfig(t, repo, "worktree:\n  root: ../isolated\n  path_template: '../../escape'\n")
			case "absolute escape":
				fakeLaunchWriteConfig(t, repo, "worktree:\n  path_template: '"+filepath.ToSlash(filepath.Join(t.TempDir(), "escape"))+"'\n")
			case "root destination":
				fakeLaunchWriteConfig(t, repo, "worktree:\n  root: ../isolated\n  path_template: '.'\n")
			case "main destination":
				fakeLaunchWriteConfig(t, repo, "worktree:\n  root: ..\n  path_template: 'project'\n")
			case "ancestor symlink":
				link := filepath.Join(filepath.Dir(repo), "linked")
				if err := os.Symlink(t.TempDir(), link); err != nil {
					t.Skip(err)
				}
				fakeLaunchWriteConfig(t, repo, "worktree:\n  root: ../linked/child\n")
			case "dangling destination", "occupied destination":
				fakeLaunchWriteConfig(t, repo, "worktree:\n  root: ../isolated\n  path_template: '{branch}'\n")
				root := filepath.Join(filepath.Dir(repo), "isolated")
				if err := os.Mkdir(root, 0700); err != nil {
					t.Fatal(err)
				}
				if mode == "dangling destination" {
					if err := os.Symlink(filepath.Join(t.TempDir(), "missing"), filepath.Join(root, branch)); err != nil {
						t.Skip(err)
					}
				} else if err := os.Mkdir(filepath.Join(root, branch), 0700); err != nil {
					t.Fatal(err)
				}
			case "missing canonical root":
				if err := os.Rename(filepath.Dir(repo), filepath.Dir(repo)+"-moved"); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Rename(filepath.Dir(repo)+"-moved", filepath.Dir(repo)) })
			}
			if p, err := PreviewWorktreePlacement(context.Background(), settings, repo, branch); err == nil {
				t.Fatalf("unsafe placement accepted: %+v", p)
			}
		})
	}
}
func TestWorktreePlacementRevisionAndConfigDigestBindHooksWithoutPersistingCommands(t *testing.T) {
	settings, repo := fakeLaunchPlacementRepo(t)
	first, err := PreviewWorktreePlacement(context.Background(), settings, repo, "one")
	if err != nil {
		t.Fatal(err)
	}
	fakeLaunchWriteConfig(t, repo, "upstream: origin/main\nhooks:\n  on_worktree_create:\n    - 'echo secret-hook-command'\n")
	second, err := PreviewWorktreePlacement(context.Background(), settings, repo, "one")
	if err != nil || second.ConfigHash == first.ConfigHash || strings.Contains(second.ConfigHash, "secret") {
		t.Fatalf("digest=%+v %v", second, err)
	}
	if _, err := UpdateProjectRoots(settings, "add-second", first.Revision, t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}
	third, err := PreviewWorktreePlacement(context.Background(), settings, repo, "one")
	if err != nil || third.Revision <= first.Revision {
		t.Fatalf("root revision=%+v %v", third, err)
	}
	called := false
	err = WithWorktreePlacement(context.Background(), settings, repo, "one", func(p WorktreePlacement) error {
		called = true
		if p != third {
			t.Fatalf("locked placement differs: %+v / %+v", p, third)
		}
		if _, err := os.Stat(p.Root); err != nil {
			t.Error(err)
		}
		return nil
	})
	if err != nil || !called {
		t.Fatalf("locked placement: %v called=%v", err, called)
	}
}
