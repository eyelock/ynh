package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/config"
)

// TestCmdPrune_ReportsOnlyWhatItRemoved pins the audit done for #533: prune
// used to print "Removed ..." for a stale launcher or run dir whether or not
// the removal worked, and "Removing orphaned installation" before the log
// that records it was saved. Each line must describe what actually happened.
//
// prune deletes. Every path it can reach here is under t.TempDir(), via
// YNH_HOME; nothing outside it is ever handed to cmdPrune.
func TestCmdPrune_ReportsOnlyWhatItRemoved(t *testing.T) {
	tests := []struct {
		name     string
		stuck    func(t *testing.T) // makes the removal fail; nil means it succeeds
		setup    func(t *testing.T)
		wantErr  bool
		wantLine string
		wantGone string // path that must be gone when the line is printed
	}{
		{
			name:     "stale launcher removed",
			setup:    staleLauncher,
			wantLine: "Removed stale launcher",
			wantGone: "bin/stale",
		},
		{
			name:     "stale launcher that cannot be removed",
			setup:    staleLauncher,
			stuck:    func(t *testing.T) { readOnly(t, config.BinDir()) },
			wantLine: "Removed stale launcher",
		},
		{
			name:     "stale run dir removed",
			setup:    staleRunDir,
			wantLine: "Removed stale run dir",
			wantGone: "run/stale",
		},
		{
			name:     "stale run dir that cannot be removed",
			setup:    staleRunDir,
			stuck:    func(t *testing.T) { readOnly(t, config.RunDir()) },
			wantLine: "Removed stale run dir",
		},
		{
			name:     "orphaned installation removed",
			setup:    orphanRecord,
			wantLine: "Removed orphaned installation",
		},
		{
			name:  "orphaned installation whose log cannot be saved",
			setup: orphanRecord,
			stuck: func(t *testing.T) {
				if err := os.Chmod(config.SymlinksPath(), 0o444); err != nil {
					t.Fatal(err)
				}
			},
			wantErr:  true,
			wantLine: "orphaned installation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.stuck != nil && os.Geteuid() == 0 {
				t.Skip("root can remove anything")
			}
			home := t.TempDir()
			t.Setenv("YNH_HOME", home)
			if err := config.EnsureDirs(); err != nil {
				t.Fatal(err)
			}
			tt.setup(t)
			if tt.stuck != nil {
				tt.stuck(t)
			}

			var out bytes.Buffer
			var err error
			captureStdout(t, &out, func() { err = cmdPrune() })
			if (err != nil) != tt.wantErr {
				t.Fatalf("cmdPrune error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.stuck != nil && strings.Contains(out.String(), "No orphaned installations found") {
				t.Errorf("reported nothing found while something could not be removed:\n%s", out.String())
			}
			printed := strings.Contains(out.String(), tt.wantLine)
			if printed != (tt.stuck == nil) {
				t.Errorf("%q printed = %v, want %v; output:\n%s", tt.wantLine, printed, tt.stuck == nil, out.String())
			}
			if tt.wantGone != "" {
				if _, err := os.Lstat(filepath.Join(home, tt.wantGone)); !os.IsNotExist(err) {
					t.Errorf("%s reported removed but still present: err=%v", tt.wantGone, err)
				}
			}
		})
	}
}

func staleLauncher(t *testing.T) {
	t.Helper()
	path := filepath.Join(config.BinDir(), "stale")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexec ynh run local/stale \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func staleRunDir(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(config.RunDir(), "stale"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// orphanRecord writes a symlink log entry whose links are all gone.
func orphanRecord(t *testing.T) {
	t.Helper()
	log := `{"installations":[{"harness":"gone","vendor":"cursor","project":"/nowhere","symlinks":[]}]}`
	if err := os.WriteFile(config.SymlinksPath(), []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
}

// readOnly stops entries being removed from dir, and restores it so
// t.TempDir() can clean up.
func readOnly(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
}
