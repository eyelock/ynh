package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
)

// Every path these tests hand to cmdUninstall lives under t.TempDir(): the
// command deletes what it resolves, and it must never be pointed at a real
// home (see .claude/rules/destructive-operations.md).

// TestCmdUninstall_MultipleNames covers the #398 regression: `ynh uninstall`
// used to act on the first name only and silently ignore the rest.
func TestCmdUninstall_MultipleNames(t *testing.T) {
	tests := []struct {
		name      string
		installed []string
		args      []string
		wantErr   string   // substring of the returned error; "" means success
		wantGone  []string // installs that must be removed
		wantKept  []string // installs that must still be present
	}{
		{
			name:      "all names removed",
			installed: []string{"a", "b", "c"},
			args:      []string{"local/a", "local/b", "local/c"},
			wantGone:  []string{"a", "b", "c"},
		},
		{
			name:      "one missing name removes nothing",
			installed: []string{"a", "b"},
			args:      []string{"local/a", "local/missing", "local/b"},
			wantErr:   "1 of 3 harnesses could not be uninstalled, nothing was removed",
			wantKept:  []string{"a", "b"},
		},
		{
			name:      "every failure is counted",
			installed: []string{"a"},
			args:      []string{"local/x", "local/a", "local/y"},
			wantErr:   "2 of 3 harnesses could not be uninstalled, nothing was removed",
			wantKept:  []string{"a"},
		},
		{
			name:      "single missing name keeps the one-name error",
			installed: []string{"a"},
			args:      []string{"local/missing"},
			wantErr:   `harness "local/missing" is not installed`,
			wantKept:  []string{"a"},
		},
		{
			name:      "duplicate name is one uninstall",
			installed: []string{"a", "b"},
			args:      []string{"local/a", "local/a"},
			wantGone:  []string{"a"},
			wantKept:  []string{"b"},
		},
		{
			name:    "no names is a usage error",
			args:    []string{},
			wantErr: "usage: ynh uninstall <name> [<name>...]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("YNH_HOME", filepath.Join(t.TempDir(), ".ynh"))
			if err := config.EnsureDirs(); err != nil {
				t.Fatal(err)
			}
			for _, n := range tt.installed {
				installTestHarness(t, n)
			}

			err := cmdUninstall(tt.args)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("cmdUninstall(%v) failed: %v", tt.args, err)
				}
			} else {
				if err == nil {
					t.Fatalf("cmdUninstall(%v) succeeded, want error containing %q", tt.args, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("cmdUninstall(%v) error = %q, want substring %q", tt.args, err, tt.wantErr)
				}
			}

			for _, n := range tt.wantGone {
				if _, err := os.Stat(harness.InstalledDirByID("local/" + n)); !os.IsNotExist(err) {
					t.Errorf("harness %q still installed", n)
				}
				if _, err := os.Stat(filepath.Join(config.BinDir(), n)); !os.IsNotExist(err) {
					t.Errorf("launcher for %q still present", n)
				}
			}
			for _, n := range tt.wantKept {
				if _, err := os.Stat(harness.InstalledDirByID("local/" + n)); err != nil {
					t.Errorf("harness %q should still be installed: %v", n, err)
				}
				if _, err := os.Stat(filepath.Join(config.BinDir(), n)); err != nil {
					t.Errorf("launcher for %q should still be present: %v", n, err)
				}
			}
		})
	}
}

// TestCmdUninstall_ConfigWrittenOnlyOnChange covers #490: uninstall used to
// save config.json unconditionally, so a fresh home gained a config file and
// an unrelated config was rewritten even when no sources entry was removed.
func TestCmdUninstall_ConfigWrittenOnlyOnChange(t *testing.T) {
	tests := []struct {
		name        string
		config      string // config.json before uninstall; "" means no file
		wantNoFile  bool   // config.json must not exist afterwards
		wantSources []string
		wantSame    bool // config.json must be byte-identical afterwards
	}{
		{
			name:       "no config stays absent",
			wantNoFile: true,
		},
		{
			name:     "config without a matching source is not rewritten",
			config:   "{\n    \"default_vendor\": \"codex\",\n    \"sources\": [{\"name\": \"other\", \"path\": \"/x\"}]\n}\n",
			wantSame: true,
		},
		{
			name:        "matching source entry is removed and saved",
			config:      `{"default_vendor":"claude","sources":[{"name":"a","path":"/a"},{"name":"other","path":"/x"}]}`,
			wantSources: []string{"other"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("YNH_HOME", t.TempDir())
			if err := config.EnsureDirs(); err != nil {
				t.Fatal(err)
			}
			if tt.config != "" {
				if err := os.WriteFile(config.ConfigPath(), []byte(tt.config), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			installTestHarness(t, "a")

			if err := cmdUninstall([]string{"local/a"}); err != nil {
				t.Fatalf("cmdUninstall failed: %v", err)
			}

			data, err := os.ReadFile(config.ConfigPath())
			if tt.wantNoFile {
				if !os.IsNotExist(err) {
					t.Fatalf("config.json exists after uninstall (err=%v): %s", err, data)
				}
				return
			}
			if err != nil {
				t.Fatalf("reading config.json: %v", err)
			}
			if tt.wantSame && string(data) != tt.config {
				t.Errorf("config.json rewritten:\n got %s\nwant %s", data, tt.config)
			}
			if tt.wantSources != nil {
				cfg, err := config.Load()
				if err != nil {
					t.Fatal(err)
				}
				var got []string
				for _, s := range cfg.Sources {
					got = append(got, s.Name)
				}
				if strings.Join(got, ",") != strings.Join(tt.wantSources, ",") {
					t.Errorf("sources = %v, want %v", got, tt.wantSources)
				}
			}
		})
	}
}

// TestConfigCommands_NoOpLeavesNoConfig pins the audit done for #490: every
// other command that saves config.json refuses a no-op with an error before
// saving, so a fresh home must still have no config.json afterwards.
func TestConfigCommands_NoOpLeavesNoConfig(t *testing.T) {
	tests := []struct {
		name string
		run  func() error
	}{
		{"sources remove missing", func() error { return cmdSourcesRemove([]string{"missing"}, io.Discard) }},
		{"registry remove missing", func() error { return cmdRegistryRemove([]string{"https://example.invalid/r"}) }},
		{"backend remove missing", func() error { return cmdBackendRemove([]string{"missing"}) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("YNH_HOME", t.TempDir())
			if err := tt.run(); err == nil {
				t.Fatal("expected an error for a no-op")
			}
			if _, err := os.Stat(config.ConfigPath()); !os.IsNotExist(err) {
				t.Errorf("config.json written by a no-op (stat err=%v)", err)
			}
		})
	}
}

// TestCmdUninstall_SourceTreeMessage covers #533: uninstalling a pointer
// install used to report its source tree as "left in place" even when the
// tree was already gone. The line must appear only when a directory is there.
// Uninstalling a pointer never deletes its source; every path here is still
// under t.TempDir().
func TestCmdUninstall_SourceTreeMessage(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T, src string) // creates whatever is at src
		wantLine bool
	}{
		{
			name:     "tree present",
			setup:    func(t *testing.T, src string) { mkdirAll(t, src) },
			wantLine: true,
		},
		{
			name:  "tree absent",
			setup: func(t *testing.T, src string) {},
		},
		{
			name: "path is a regular file",
			setup: func(t *testing.T, src string) {
				if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "path is a symlink to a tree",
			setup: func(t *testing.T, src string) {
				real := filepath.Join(filepath.Dir(src), "real")
				mkdirAll(t, real)
				if err := os.Symlink(real, src); err != nil {
					t.Fatal(err)
				}
			},
			wantLine: true,
		},
		{
			name: "path is a dangling symlink",
			setup: func(t *testing.T, src string) {
				if err := os.Symlink(filepath.Join(filepath.Dir(src), "missing"), src); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("YNH_HOME", t.TempDir())
			src := filepath.Join(t.TempDir(), "fork")
			tt.setup(t, src)
			if err := harness.SavePointer(&harness.Pointer{
				Name: "fork",
				InstalledJSON: plugin.InstalledJSON{
					SourceType:  "local",
					Source:      src,
					InstalledAt: "2026-10-06T00:00:00Z",
				},
			}); err != nil {
				t.Fatal(err)
			}

			var out bytes.Buffer
			var err error
			captureStdout(t, &out, func() { err = cmdUninstall([]string{"local/fork"}) })
			if err != nil {
				t.Fatalf("cmdUninstall failed: %v", err)
			}

			if !strings.Contains(out.String(), `Uninstalled harness "fork"`) {
				t.Errorf("missing confirmation in output:\n%s", out.String())
			}
			gotLine := strings.Contains(out.String(), "Source tree left in place")
			if gotLine != tt.wantLine {
				t.Errorf("source-tree line printed = %v, want %v; output:\n%s", gotLine, tt.wantLine, out.String())
			}
			if tt.wantLine {
				if fi, err := os.Stat(src); err != nil || !fi.IsDir() {
					t.Errorf("source tree reported left in place but is not there: err=%v", err)
				}
			}
		})
	}
}

func mkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}
