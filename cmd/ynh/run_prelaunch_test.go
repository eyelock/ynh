//go:build unix

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// treeText lists every file under root with its content, following nothing.
func treeText(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		rel, _ := filepath.Rel(root, p)
		b.WriteString(rel + "\x00" + string(data) + "\x00")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// The files OpenCode needs at launch (its on_session_start output and the
// composed AGENTS.md) are written before the snapshot is sealed. Two runs of one
// harness from different working folders therefore get different snapshots, each
// carrying its own folder's hook output, and the first run's files never change
// under it (ynh #765). The stand-in launch also checks that a vendor launch
// finds the files already in place.
func TestCmdRun_OpenCodeLaunchFilesAreSealedInTheSnapshot(t *testing.T) {
	isolateRun(t)
	work := t.TempDir()
	harnessDir := filepath.Join(work, "h")
	writeSkillHarness(t, harnessDir, "h")
	manifest := `{"name":"h","version":"0.1.0","default_vendor":"opencode","hooks":{"on_session_start":[{"command":"pwd"}]}}`
	if err := os.WriteFile(filepath.Join(harnessDir, ".agents/harness", "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	dirA, dirB := t.TempDir(), t.TempDir()

	var launched []string
	stubLaunch(t, func(runDir string, _ runArgs) error {
		got, err := os.ReadFile(filepath.Join(runDir, ".opencode", "AGENTS.md"))
		if err != nil {
			t.Errorf("the composed AGENTS.md was not in the run dir at launch: %v", err)
		}
		if !strings.Contains(string(got), filepath.Base(mustGetwd(t))) {
			t.Errorf("AGENTS.md lacks this folder's session-start output:\n%s", got)
		}
		launched = append(launched, runDir)
		return nil
	})

	t.Chdir(dirA)
	if err := cmdRun([]string{harnessDir, "-v", "opencode"}); err != nil {
		t.Fatal(err)
	}
	first := treeText(t, launched[0])

	t.Chdir(dirB)
	if err := cmdRun([]string{harnessDir, "-v", "opencode"}); err != nil {
		t.Fatal(err)
	}
	if launched[0] == launched[1] {
		t.Fatalf("both runs used one snapshot, %s", launched[0])
	}
	if _, err := os.Stat(launched[0]); err != nil {
		return // a later change may delete an unlocked snapshot; nothing left to change
	}
	if after := treeText(t, launched[0]); after != first {
		t.Errorf("the first snapshot changed under its session after the second run")
	}
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	d, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return d
}
