//go:build e2e

package e2e

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// envelopePaths mirrors cmd/ynh.resolvedPaths for JSON-shape assertions.
type envelopePaths struct {
	Home      string `json:"home"`
	Config    string `json:"config"`
	Harnesses string `json:"harnesses"`
	Symlinks  string `json:"symlinks"`
	Cache     string `json:"cache"`
	Run       string `json:"run"`
	Bin       string `json:"bin"`
}

// TestPaths_JSON_Shape asserts `ynh paths --format json` emits all seven
// path keys with non-empty values. Order is fixed by the struct definition
// in cmd/ynh/paths.go.
func TestPaths_JSON_Shape(t *testing.T) {
	s := newSandbox(t)
	out, _ := s.mustRunYnh(t, "paths", "--format", "json")

	var got envelopePaths
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parsing paths JSON: %v\n%s", err, out)
	}

	for k, v := range map[string]string{
		"home":      got.Home,
		"config":    got.Config,
		"harnesses": got.Harnesses,
		"symlinks":  got.Symlinks,
		"cache":     got.Cache,
		"run":       got.Run,
		"bin":       got.Bin,
	} {
		if v == "" {
			t.Errorf("paths.%s is empty", k)
		}
	}
}

// TestPaths_HonorsYnhHome asserts the sandbox's YNH_HOME is reflected in
// every resolved path — proves env-var override actually controls path
// resolution end-to-end (not just for one entry).
func TestPaths_HonorsYnhHome(t *testing.T) {
	s := newSandbox(t)
	out, _ := s.mustRunYnh(t, "paths", "--format", "json")

	var got envelopePaths
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parsing paths JSON: %v\n%s", err, out)
	}

	assertEqual(t, "home", got.Home, s.home)
	assertEqual(t, "harnesses", got.Harnesses, filepath.Join(s.home, "harnesses"))
	assertEqual(t, "run", got.Run, filepath.Join(s.home, "run"))
	assertEqual(t, "bin", got.Bin, filepath.Join(s.home, "bin"))

	// Other paths should at least live under YNH_HOME.
	for _, p := range []string{got.Config, got.Symlinks, got.Cache} {
		if !strings.HasPrefix(p, s.home) {
			t.Errorf("path %q is not under YNH_HOME (%s)", p, s.home)
		}
	}
}
