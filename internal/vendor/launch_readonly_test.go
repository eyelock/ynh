package vendor

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// sealRunDir makes every file under root read-only and every folder
// unwritable, as a sealed snapshot is, and restores write access when the test
// ends so the temporary folder can be removed.
func sealRunDir(t *testing.T, root string) {
	t.Helper()
	var dirs []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			dirs = append(dirs, p)
			return nil
		}
		return os.Chmod(p, 0o444)
	})
	if err != nil {
		t.Fatal(err)
	}
	// Deepest first, so a folder is sealed after its children.
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := os.Chmod(dirs[i], 0o555); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err == nil {
				if d.IsDir() {
					_ = os.Chmod(p, 0o755)
				} else {
					_ = os.Chmod(p, 0o644)
				}
			}
			return nil
		})
	})
}

// stubVendorCLI puts an executable named name that exits at once first on PATH.
func stubVendorCLI(t *testing.T, name string) {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestLaunchWritesNothingToRunDir: Claude, Codex and Cursor start their CLI
// from a sealed run dir (every file read-only, every folder unwritable) and
// leave it exactly as it was, content and modification times. None of their
// launch paths writes; this pins it (ynh #765). OpenCode and Copilot have their
// own tests, which also cover their launch-time reads.
func TestLaunchWritesNothingToRunDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes through read-only files and folders")
	}
	cases := []struct {
		vendor string
		cli    string
		runs   map[string]func(cfg string) error
	}{
		{"claude", claudeCLI, map[string]func(string) error{
			"interactive":     func(c string) error { return (&Claude{}).LaunchInteractive(c, nil) },
			"initial prompt":  func(c string) error { return (&Claude{}).LaunchWithInitialPrompt(c, "go", nil) },
			"non-interactive": func(c string) error { return (&Claude{}).LaunchNonInteractive(c, "go", nil) },
			"resume":          func(c string) error { return (&Claude{}).LaunchResume(c, "ses_1", "", true, nil) },
			"session id":      func(c string) error { return (&Claude{}).LaunchWithSessionID(c, "ses_1", "go", true, nil) },
		}},
		{"codex", codexCLI, map[string]func(string) error{
			"interactive":     func(c string) error { return (&Codex{}).LaunchInteractive(c, nil) },
			"initial prompt":  func(c string) error { return (&Codex{}).LaunchWithInitialPrompt(c, "go", nil) },
			"non-interactive": func(c string) error { return (&Codex{}).LaunchNonInteractive(c, "go", nil) },
			"resume":          func(c string) error { return (&Codex{}).LaunchResume(c, "ses_1", "", true, nil) },
		}},
		{"cursor", cursorCLI, map[string]func(string) error{
			"interactive":     func(c string) error { return (&Cursor{}).LaunchInteractive(c, nil) },
			"initial prompt":  func(c string) error { return (&Cursor{}).LaunchWithInitialPrompt(c, "go", nil) },
			"non-interactive": func(c string) error { return (&Cursor{}).LaunchNonInteractive(c, "go", nil) },
			"resume":          func(c string) error { return (&Cursor{}).LaunchResume(c, "ses_1", "", true, nil) },
		}},
	}
	for _, tc := range cases {
		for name, run := range tc.runs {
			t.Run(tc.vendor+"/"+name, func(t *testing.T) {
				stubVendorCLI(t, tc.cli)
				cfg := t.TempDir()
				sub := filepath.Join(cfg, "."+tc.vendor)
				if err := os.MkdirAll(sub, 0o755); err != nil {
					t.Fatal(err)
				}
				for p, body := range map[string]string{
					filepath.Join(cfg, "AGENTS.md"):   "base",
					filepath.Join(cfg, "CLAUDE.md"):   "base",
					filepath.Join(sub, "config.json"): "{}",
					filepath.Join(cfg, ".mcp.json"):   `{"mcpServers":{}}`,
				} {
					if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				before := treeDigest(t, cfg)
				sealRunDir(t, cfg)
				if err := run(cfg); err != nil {
					t.Fatalf("launch on a sealed run dir: %v", err)
				}
				if after := treeDigest(t, cfg); after != before {
					t.Errorf("the launch changed the run dir:\nbefore %s\nafter  %s", before, after)
				}
			})
		}
	}
}
