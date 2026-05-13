package harness

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

func TestIsValidName(t *testing.T) {
	cases := []struct {
		name string
		ok   bool
	}{
		{"simple", true},
		{"with-dash", true},
		{"with.dot", true},
		{"with_underscore", true},
		{"123-numeric-start", true},
		{"a", true},
		{"", false},
		{"-leading-dash", false},
		{".leading-dot", false},
		{"_leading-underscore", false},
		{"has space", false},
		{"has/slash", false},
		{"has@at", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsValidName(c.name); got != c.ok {
				t.Errorf("IsValidName(%q) = %v, want %v", c.name, got, c.ok)
			}
		})
	}
}

func TestValidNamePattern(t *testing.T) {
	p := ValidNamePattern()
	if p == "" {
		t.Fatal("expected non-empty pattern")
	}
	if !strings.Contains(p, "a-z") {
		t.Errorf("pattern does not look like a regex: %q", p)
	}
}

func TestBadRefError(t *testing.T) {
	err := BadRefError("")
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Errorf("empty ref: want 'missing' error, got %v", err)
	}
	err = BadRefError("garbage")
	if err == nil || !strings.Contains(err.Error(), "canonical id") {
		t.Errorf("garbage ref: want canonical-id hint, got %v", err)
	}
}

func TestLoadQualified_BadRef(t *testing.T) {
	_, err := LoadQualified("not-a-canonical-id")
	if err == nil || !strings.Contains(err.Error(), "canonical id") {
		t.Errorf("want canonical-id error, got %v", err)
	}
}

func TestLoadQualified_NotInstalled(t *testing.T) {
	overrideHarnessesDir(t)
	_, err := LoadQualified("local/missing")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestLoadNS_NotFound(t *testing.T) {
	overrideHarnessesDir(t)
	_, err := LoadNS("github.com--acme", "missing")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestInstalledDirNS_NamespacedDir_Match(t *testing.T) {
	a := InstalledDirNS("github.com/acme/x", "foo")
	b := NamespacedDir("github.com/acme/x", "foo")
	if a != b {
		t.Errorf("InstalledDirNS != NamespacedDir: %q vs %q", a, b)
	}
}

func TestInstalledDirNS_UsesFSName(t *testing.T) {
	got := InstalledDirNS("github.com/acme/x", "foo")
	// Slashes in namespace are flattened to "--" for filesystem safety.
	if strings.Contains(filepath.Base(filepath.Dir(got)), "/") {
		t.Errorf("namespace not sanitized for filesystem: %q", got)
	}
	if filepath.Base(got) != "foo" {
		t.Errorf("expected name as last segment, got %q", got)
	}
}

func TestLoadFile_LoadsLegacyManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".harness.json")
	// Write a minimal legacy manifest (LoadHarnessFile reads a single file form).
	hj := &plugin.HarnessJSON{
		Name:          "legacy",
		Version:       "0.1.0",
		Description:   "test",
		DefaultVendor: "claude",
	}
	data, err := json.MarshalIndent(hj, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if got.Name != "legacy" || got.DefaultVendor != "claude" {
		t.Errorf("unexpected harness: %+v", got)
	}
}

func TestLoadFile_FileNotFound(t *testing.T) {
	_, err := LoadFile(filepath.Join(t.TempDir(), "no-such.json"))
	if err == nil {
		t.Fatal("expected error reading missing file")
	}
}
