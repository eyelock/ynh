package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
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
