package exporter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// harnessWithDelegates copies the export fixture into a temp directory and
// declares a delegate on it. Codex does not support delegates on export, so
// the delegate is never resolved and no network is involved.
func harnessWithDelegates(t *testing.T) string {
	t.Helper()
	src := filepath.Join(testdataDir(), "export-harness")
	dst := t.TempDir()

	err := filepath.Walk(src, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rErr := filepath.Rel(src, p)
		if rErr != nil {
			return rErr
		}
		target := filepath.Join(dst, rel)
		if fi.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, rErr := os.ReadFile(p)
		if rErr != nil {
			return rErr
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copying fixture: %v", err)
	}

	manifest := filepath.Join(dst, ".ynh-plugin", "plugin.json")
	raw, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m["delegates_to"] = []map[string]string{{"git": "example.invalid/org/reviewer"}}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, out, 0o644); err != nil {
		t.Fatal(err)
	}
	return dst
}

// Codex cannot carry delegates in an export. The export must still succeed and
// must say what it left out: a harness quietly missing the hand-offs its
// manifest declares is worse than one that refuses.
func TestExport_WarnsWhenVendorCannotCarryDelegates(t *testing.T) {
	results, err := Export(ExportOptions{
		SourceDir: harnessWithDelegates(t),
		OutputDir: t.TempDir(),
		Vendors:   []string{"codex"},
		Mode:      ModePerVendor,
	})
	if err != nil {
		t.Fatalf("Export must succeed and warn, not fail: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}

	joined := strings.Join(results[0].Warnings, "\n")
	if !strings.Contains(joined, "delegates") {
		t.Errorf("expected a warning naming delegates, got: %q", joined)
	}
	if !strings.Contains(joined, "codex") {
		t.Errorf("warning should name the vendor, got: %q", joined)
	}
}

// The counterpart: a vendor that does support delegates must not warn about
// them. Otherwise the warning is noise and gets ignored.
func TestExport_NoDelegateWarningForSupportingVendor(t *testing.T) {
	// Cursor supports export delegates, so it attempts resolution and fails on
	// an unreachable source. The failure is the point: it proves the delegate
	// was taken seriously rather than skipped with a warning.
	_, err := Export(ExportOptions{
		SourceDir: harnessWithDelegates(t),
		OutputDir: t.TempDir(),
		Vendors:   []string{"cursor"},
		Mode:      ModePerVendor,
	})
	if err == nil {
		t.Fatal("cursor supports delegates, so an unresolvable one must fail the export")
	}
	if !strings.Contains(err.Error(), "delegate") {
		t.Errorf("error should identify the delegate, got: %v", err)
	}
}

// A harness with no delegates must produce no delegate warning at all.
func TestExport_NoDelegatesNoWarning(t *testing.T) {
	results, err := Export(ExportOptions{
		SourceDir: filepath.Join(testdataDir(), "export-harness"),
		OutputDir: t.TempDir(),
		Vendors:   []string{"codex"},
		Mode:      ModePerVendor,
	})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	for _, w := range results[0].Warnings {
		if strings.Contains(w, "delegates") {
			t.Errorf("no delegates declared, but warned: %q", w)
		}
	}
}
