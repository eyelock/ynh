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
	_, err := LoadNS("github.com--example-org", "missing")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestInstalledDirNS_NamespacedDir_Match(t *testing.T) {
	a := InstalledDirNS("github.com/example-org/x", "foo")
	b := NamespacedDir("github.com/example-org/x", "foo")
	if a != b {
		t.Errorf("InstalledDirNS != NamespacedDir: %q vs %q", a, b)
	}
}

func TestInstalledDirNS_UsesFSName(t *testing.T) {
	got := InstalledDirNS("github.com/example-org/x", "foo")
	// Slashes in namespace are flattened to "--" for filesystem safety.
	if strings.Contains(filepath.Base(filepath.Dir(got)), "/") {
		t.Errorf("namespace not sanitized for filesystem: %q", got)
	}
	if filepath.Base(got) != "foo" {
		t.Errorf("expected name as last segment, got %q", got)
	}
}

func TestLoadFile_LoadsSingleFileManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ephemeral.json")
	// A single-file manifest, as --harness-file takes it.
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

// treeListing returns every path under dir, so a test can prove a read wrote
// nothing.
func treeListing(t *testing.T, dir string) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(dir, func(p string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		paths = append(paths, p)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

// --harness-file pointed at a legacy .harness.json is refused like every
// other read since #417 (#449), and nothing is converted or written.
func TestLoadFile_RefusesLegacyHarnessJSON(t *testing.T) {
	tests := []struct {
		name       string
		withPlugin bool
		want       []string
	}{
		{
			name: "legacy manifest only",
			want: []string{"uses the legacy .harness.json manifest, which ynh no longer reads", "ynd migrate "},
		},
		{
			name:       "plugin.json beside it",
			withPlugin: true,
			want:       []string{"legacy .harness.json manifest, which ynh no longer reads", "ynh run "},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, plugin.HarnessFile)
			if err := os.WriteFile(path, []byte(`{"name":"old","version":"0.1.0"}`), 0o644); err != nil {
				t.Fatal(err)
			}
			if tt.withPlugin {
				if err := os.MkdirAll(filepath.Join(dir, ".agents/harness"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, ".agents/harness", "plugin.json"),
					[]byte(`{"name":"new","version":"0.1.0"}`), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			before := treeListing(t, dir)

			_, err := LoadFile(path)
			if err == nil {
				t.Fatal("expected a legacy .harness.json to be refused")
			}
			for _, w := range tt.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q does not contain %q", err, w)
				}
			}
			if !strings.Contains(err.Error(), dir) {
				t.Errorf("error %q does not name the tree %s", err, dir)
			}
			if after := treeListing(t, dir); strings.Join(after, "\n") != strings.Join(before, "\n") {
				t.Errorf("refusal changed the tree:\nbefore %v\nafter  %v", before, after)
			}
		})
	}
}

// A current-format plugin.json still loads through --harness-file.
func TestLoadFile_LoadsPluginJSONPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".agents/harness", "plugin.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"name":"current","version":"0.1.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if got.Name != "current" {
		t.Errorf("Name = %q, want current", got.Name)
	}
}

// The rejection hint lists only the forms the calling command accepts
// (#448): an id-only command must not offer a path.
func TestBadRefHints(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantPath  bool
		wantLocal bool
	}{
		{"id only", BadRefError("demo"), false, true},
		{"id or path", BadRefOrPathError("demo"), true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.err.Error()
			if !strings.Contains(msg, `"demo" is not a valid harness id`) {
				t.Errorf("missing rejection: %s", msg)
			}
			if got := strings.Contains(msg, "./<path>"); got != tt.wantPath {
				t.Errorf("offers ./<path> = %v, want %v: %s", got, tt.wantPath, msg)
			}
			if got := strings.Contains(msg, "local/<name>"); got != tt.wantLocal {
				t.Errorf("offers local/<name> = %v, want %v: %s", got, tt.wantLocal, msg)
			}
		})
	}
	for _, err := range []error{BadRefError(""), BadRefOrPathError("")} {
		if err.Error() != "missing harness reference" {
			t.Errorf("empty ref: %v", err)
		}
	}
}
