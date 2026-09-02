package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mkScript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// The defect: calibration runs in the fixture, check runs in the measured
// tree, and a cwd-relative command resolves to a different program in each.
// Both reported green, and nothing said two programs were involved (#363).
func TestResolveCommandProgram_DetectsCwdDependence(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join(root, "fixture")
	tree := filepath.Join(root, "tree")
	harness := filepath.Join(root, "harness")

	// Same path in both roots, different contents. A path comparison would
	// call these identical, which is why the program is hashed.
	mkScript(t, filepath.Join(fixture, "tools", "y.sh"), "#!/bin/sh\nexit 1\n")
	mkScript(t, filepath.Join(tree, "tools", "y.sh"), "#!/bin/sh\nexit 0\n")
	mkScript(t, filepath.Join(harness, "tools", "x.sh"), "#!/bin/sh\nexit 1\n")

	t.Run("cwd-relative resolves differently", func(t *testing.T) {
		a := resolveCommandProgram("./tools/y.sh", fixture, harness)
		b := resolveCommandProgram("./tools/y.sh", tree, harness)
		if a == "" || b == "" {
			t.Fatalf("expected both to resolve, got %q and %q", a, b)
		}
		if a == b {
			t.Error("the two trees hold different scripts at the same path; " +
				"they must not fingerprint the same")
		}
	})

	t.Run("harness-pinned resolves identically", func(t *testing.T) {
		a := resolveCommandProgram(`"$YNH_HARNESS_DIR/tools/x.sh"`, fixture, harness)
		b := resolveCommandProgram(`"$YNH_HARNESS_DIR/tools/x.sh"`, tree, harness)
		if a == "" {
			t.Fatal("a pinned command should resolve")
		}
		if a != b {
			t.Errorf("a harness-pinned command must resolve identically: %q vs %q", a, b)
		}
	})

	t.Run("a PATH binary resolves identically", func(t *testing.T) {
		a := resolveCommandProgram("sh -c true", fixture, harness)
		b := resolveCommandProgram("sh -c true", tree, harness)
		if a == "" || a != b {
			t.Errorf("a command on PATH must resolve identically: %q vs %q", a, b)
		}
	})

	t.Run("an unresolvable command yields nothing", func(t *testing.T) {
		if got := resolveCommandProgram("./definitely-not-here.sh", tree, harness); got != "" {
			t.Errorf("expected no fingerprint, got %q", got)
		}
	})
}

// The fingerprint must reflect contents, not just the path, or the case where
// two trees carry the same filename with different code goes undetected.
func TestResolveCommandProgram_FingerprintsContents(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "d")
	mkScript(t, filepath.Join(dir, "s.sh"), "#!/bin/sh\nexit 0\n")

	before := resolveCommandProgram("./s.sh", dir, root)
	if !strings.HasPrefix(before, "sha256:") {
		t.Fatalf("expected a content hash, got %q", before)
	}
	mkScript(t, filepath.Join(dir, "s.sh"), "#!/bin/sh\nexit 1\n")
	if after := resolveCommandProgram("./s.sh", dir, root); after == before {
		t.Error("editing the script did not change its fingerprint")
	}
}
