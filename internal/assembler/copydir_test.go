package assembler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyDir(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	// Create source structure
	for _, dir := range []string{"skills/hello", ".git/objects"} {
		if err := os.MkdirAll(filepath.Join(src, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range map[string]string{
		"skills/hello/SKILL.md": "hello",
		"root.txt":              "root",
		".git/HEAD":             "ref",
	} {
		if err := os.WriteFile(filepath.Join(src, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := CopyDir(src, dst); err != nil {
		t.Fatalf("CopyDir failed: %v", err)
	}

	// Verify files were copied
	data, err := os.ReadFile(filepath.Join(dst, "skills", "hello", "SKILL.md"))
	if err != nil {
		t.Fatalf("skill not copied: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("skill content = %q, want %q", string(data), "hello")
	}

	data, err = os.ReadFile(filepath.Join(dst, "root.txt"))
	if err != nil {
		t.Fatalf("root.txt not copied: %v", err)
	}
	if string(data) != "root" {
		t.Errorf("root content = %q, want %q", string(data), "root")
	}

	// Verify .git was skipped
	if _, err := os.Stat(filepath.Join(dst, ".git")); !os.IsNotExist(err) {
		t.Error(".git directory should have been skipped")
	}
}

func TestCopyDir_PreservesPermissions(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "out")

	if err := os.MkdirAll(filepath.Join(src, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "scripts", "run.sh"), []byte("#!/bin/bash"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := CopyDir(src, dst); err != nil {
		t.Fatalf("CopyDir failed: %v", err)
	}

	info, err := os.Stat(filepath.Join(dst, "scripts", "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("execute permission not preserved: got %o", info.Mode())
	}
}

func TestCopyDir_EmptyDir(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	if err := CopyDir(src, dst); err != nil {
		t.Fatalf("CopyDir on empty dir failed: %v", err)
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir,
		"-c", "user.email=t@example.com", "-c", "user.name=t"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCopyDir_Symlinks(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T, src string)
		linkPath string
		wantLink string
	}{
		{
			name: "pnpm style relative link to a directory",
			setup: func(t *testing.T, src string) {
				writeTree(t, src, map[string]string{"pkg/index.js": "x"})
				if err := os.MkdirAll(filepath.Join(src, "node_modules", "@scope"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("../../pkg", filepath.Join(src, "node_modules", "@scope", "pkg")); err != nil {
					t.Fatal(err)
				}
			},
			linkPath: "node_modules/@scope/pkg",
			wantLink: "../../pkg",
		},
		{
			name: "link to a file",
			setup: func(t *testing.T, src string) {
				writeTree(t, src, map[string]string{"real.txt": "x"})
				if err := os.Symlink("real.txt", filepath.Join(src, "alias.txt")); err != nil {
					t.Fatal(err)
				}
			},
			linkPath: "alias.txt",
			wantLink: "real.txt",
		},
		{
			name: "dangling link",
			setup: func(t *testing.T, src string) {
				if err := os.Symlink("nowhere", filepath.Join(src, "dangling")); err != nil {
					t.Fatal(err)
				}
			},
			linkPath: "dangling",
			wantLink: "nowhere",
		},
		{
			name: "link out of the tree is copied, not followed",
			setup: func(t *testing.T, src string) {
				outside := t.TempDir()
				writeTree(t, outside, map[string]string{"secret": "s"})
				if err := os.Symlink(outside, filepath.Join(src, "escape")); err != nil {
					t.Fatal(err)
				}
			},
			linkPath: "escape",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, dst := t.TempDir(), t.TempDir()
			tt.setup(t, src)
			wantLink := tt.wantLink
			if wantLink == "" {
				wantLink, _ = os.Readlink(filepath.Join(src, tt.linkPath))
			}

			if err := CopyDir(src, dst); err != nil {
				t.Fatalf("CopyDir: %v", err)
			}

			got, err := os.Readlink(filepath.Join(dst, tt.linkPath))
			if err != nil {
				t.Fatalf("copy is not a symlink: %v", err)
			}
			if got != wantLink {
				t.Errorf("link text = %q, want %q", got, wantLink)
			}
			if _, err := os.Stat(filepath.Join(dst, tt.linkPath, "secret")); tt.name == "link out of the tree is copied, not followed" && err != nil {
				t.Errorf("link should still resolve to its original target: %v", err)
			}
		})
	}
}

func TestCopyDir_ReplacesExistingLinkWithoutWritingThrough(t *testing.T) {
	src, dst, victimDir := t.TempDir(), t.TempDir(), t.TempDir()
	writeTree(t, src, map[string]string{"f.txt": "new"})
	victim := filepath.Join(victimDir, "victim")
	writeTree(t, victimDir, map[string]string{"victim": "keep"})
	if err := os.Symlink(victim, filepath.Join(dst, "f.txt")); err != nil {
		t.Fatal(err)
	}

	if err := CopyDir(src, dst); err != nil {
		t.Fatalf("CopyDir: %v", err)
	}

	if b, _ := os.ReadFile(victim); string(b) != "keep" {
		t.Errorf("copy wrote through a destination symlink: victim = %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "f.txt")); string(b) != "new" {
		t.Errorf("dst f.txt = %q, want new", b)
	}
}

func TestCopyTree(t *testing.T) {
	t.Run("git tree honours gitignore", func(t *testing.T) {
		src, dst := t.TempDir(), filepath.Join(t.TempDir(), "out")
		gitIn(t, src, "init", "-q")
		writeTree(t, src, map[string]string{
			".gitignore":                "node_modules/\n.env\n",
			"plugin.json":               "{}",
			"skills/a/SKILL.md":         "a",
			"gone.md":                   "tracked then deleted",
			"node_modules/dep/index.js": "dep",
			"pkg/index.js":              "pkg",
			".env":                      "SECRET=1",
			"untracked-not-ignored.md":  "new",
		})
		if err := os.Symlink("../../pkg", filepath.Join(src, "node_modules", "link")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("pkg", filepath.Join(src, "tracked-link")); err != nil {
			t.Fatal(err)
		}
		gitIn(t, src, "add", ".gitignore", "plugin.json", "skills", "gone.md", "pkg", "tracked-link")
		gitIn(t, src, "commit", "-q", "-m", "init")
		if err := os.Remove(filepath.Join(src, "gone.md")); err != nil {
			t.Fatal(err)
		}

		if err := CopyTree(src, dst); err != nil {
			t.Fatalf("CopyTree: %v", err)
		}

		for _, want := range []string{"plugin.json", "skills/a/SKILL.md", "pkg/index.js", "untracked-not-ignored.md", ".gitignore"} {
			if _, err := os.Lstat(filepath.Join(dst, want)); err != nil {
				t.Errorf("missing %s: %v", want, err)
			}
		}
		if got, err := os.Readlink(filepath.Join(dst, "tracked-link")); err != nil || got != "pkg" {
			t.Errorf("tracked symlink not preserved: %q, %v", got, err)
		}
		for _, unwanted := range []string{"node_modules", ".env", "gone.md", ".git"} {
			if _, err := os.Lstat(filepath.Join(dst, unwanted)); err == nil {
				t.Errorf("%s should not have been copied", unwanted)
			}
		}
	})

	t.Run("non-git dir copies everything but .git", func(t *testing.T) {
		src, dst := t.TempDir(), t.TempDir()
		writeTree(t, src, map[string]string{
			"plugin.json":               "{}",
			"node_modules/dep/index.js": "dep",
			".env":                      "x",
			".git/HEAD":                 "ref",
		})
		if err := os.Symlink("dep", filepath.Join(src, "node_modules", "l")); err != nil {
			t.Fatal(err)
		}

		if err := CopyTree(src, dst); err != nil {
			t.Fatalf("CopyTree: %v", err)
		}

		for _, want := range []string{"plugin.json", "node_modules/dep/index.js", ".env", "node_modules/l"} {
			if _, err := os.Lstat(filepath.Join(dst, want)); err != nil {
				t.Errorf("missing %s: %v", want, err)
			}
		}
		if _, err := os.Lstat(filepath.Join(dst, ".git")); err == nil {
			t.Error(".git should not have been copied")
		}
	})

	t.Run("ignored source dir falls back to a full copy", func(t *testing.T) {
		repo, dst := t.TempDir(), t.TempDir()
		gitIn(t, repo, "init", "-q")
		writeTree(t, repo, map[string]string{".gitignore": "installed/\n", "installed/h/plugin.json": "{}"})
		if err := CopyTree(filepath.Join(repo, "installed", "h"), dst); err != nil {
			t.Fatalf("CopyTree: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dst, "plugin.json")); err != nil {
			t.Errorf("ignored source copied nothing: %v", err)
		}
	})
}

func TestCopyDirDeref(t *testing.T) {
	t.Run("file and directory links are copied as content", func(t *testing.T) {
		src, dst := t.TempDir(), t.TempDir()
		writeTree(t, src, map[string]string{
			"shared/x":       "shared-x",
			"shared/lib/y":   "lib-y",
			"skill/SKILL.md": "s",
		})
		if err := os.Symlink("../shared/x", filepath.Join(src, "skill", "x")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("../shared/lib", filepath.Join(src, "skill", "lib")); err != nil {
			t.Fatal(err)
		}

		if err := CopyDirDeref(filepath.Join(src, "skill"), dst); err != nil {
			t.Fatalf("CopyDirDeref: %v", err)
		}

		for name, want := range map[string]string{"x": "shared-x", "lib/y": "lib-y", "SKILL.md": "s"} {
			b, err := os.ReadFile(filepath.Join(dst, name))
			if err != nil || string(b) != want {
				t.Errorf("%s = %q, %v; want %q", name, b, err, want)
			}
			if fi, _ := os.Lstat(filepath.Join(dst, name)); fi != nil && fi.Mode()&os.ModeSymlink != 0 {
				t.Errorf("%s is still a symlink", name)
			}
		}
	})

	t.Run("through the artifact path", func(t *testing.T) {
		repo, out := t.TempDir(), t.TempDir()
		writeTree(t, repo, map[string]string{"shared/x": "sx", "skills/s/SKILL.md": "s"})
		if err := os.Symlink("../../shared/x", filepath.Join(repo, "skills", "s", "x")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("../../shared", filepath.Join(repo, "skills", "s", "dir")); err != nil {
			t.Fatal(err)
		}
		err := CopyAllArtifacts(repo, out, map[string]string{"skills": "skills"}, nil)
		if err != nil {
			t.Fatalf("CopyAllArtifacts: %v", err)
		}
		for _, name := range []string{"skills/s/x", "skills/s/dir/x"} {
			if b, err := os.ReadFile(filepath.Join(out, name)); err != nil || string(b) != "sx" {
				t.Errorf("%s = %q, %v", name, b, err)
			}
		}
	})

	tests := []struct {
		name  string
		setup func(t *testing.T, src string)
		want  string
	}{
		{"link to an ancestor", func(t *testing.T, src string) {
			writeTree(t, src, map[string]string{"a/f": "f"})
			if err := os.Symlink("..", filepath.Join(src, "a", "up")); err != nil {
				t.Fatal(err)
			}
		}, "symlink cycle at"},
		{"link to itself", func(t *testing.T, src string) {
			if err := os.Symlink("self", filepath.Join(src, "self")); err != nil {
				t.Fatal(err)
			}
		}, "dangling symlink"},
		{"dangling link", func(t *testing.T, src string) {
			if err := os.Symlink("nowhere", filepath.Join(src, "gone")); err != nil {
				t.Fatal(err)
			}
		}, "dangling symlink"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, dst := t.TempDir(), t.TempDir()
			tt.setup(t, src)
			err := CopyDirDeref(src, dst)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want containing %q", err, tt.want)
			}
		})
	}
}
