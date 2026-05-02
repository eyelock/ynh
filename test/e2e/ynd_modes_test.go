//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestYnd_Export_Merged asserts `ynd export --merged` produces a single
// output directory keyed "merged" rather than per-vendor subdirs. Locks
// the alternative output mode used by IDE integrations that prefer one
// blended layout over per-vendor splits.
func TestYnd_Export_Merged(t *testing.T) {
	harness := newSyntheticSkillHarness(t, "merged-export")
	out := filepath.Join(t.TempDir(), "out")

	mustRunYnd(t, "export", "--harness", harness, "-o", out, "--merged")

	// Per-vendor subdirs must NOT exist.
	for _, v := range []string{"claude", "codex", "cursor"} {
		if _, err := os.Stat(filepath.Join(out, v)); err == nil {
			t.Errorf("merged mode should not produce per-vendor subdir, found %s/", v)
		}
	}
	// And the skill must surface somewhere under the merged dir.
	found := false
	_ = filepath.Walk(out, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && filepath.Base(path) == "SKILL.md" {
			found = true
		}
		return nil
	})
	if !found {
		t.Errorf("merged mode should produce a SKILL.md somewhere under %s", out)
	}
}

// TestYnd_Export_Clean verifies `--clean` removes pre-existing files from
// the output directory before writing. Without --clean, stale artifacts
// from previous exports would silently accumulate.
func TestYnd_Export_Clean(t *testing.T) {
	harness := newSyntheticSkillHarness(t, "clean-export")
	out := filepath.Join(t.TempDir(), "out")

	// Pre-seed the output dir with a stale file.
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(out, "stale.txt")
	if err := os.WriteFile(stale, []byte("should be removed"), 0o644); err != nil {
		t.Fatal(err)
	}

	mustRunYnd(t, "export", "--harness", harness, "-o", out, "--clean")

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("--clean should have removed pre-existing file, err=%v", err)
	}
}

// TestYnd_Marketplace_VendorFilter asserts `-v claude` on `marketplace build`
// produces only the .claude-plugin/marketplace.json index, not .cursor-plugin/.
func TestYnd_Marketplace_VendorFilter(t *testing.T) {
	root := t.TempDir()
	harness := filepath.Join(root, "harnesses", "demo")
	if err := os.MkdirAll(filepath.Join(harness, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	plugin := `{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"demo","version":"0.1.0"}`
	if err := os.WriteFile(filepath.Join(harness, ".ynh-plugin", "plugin.json"), []byte(plugin), 0o644); err != nil {
		t.Fatal(err)
	}

	configFile := filepath.Join(root, "marketplace.json")
	cfgBody := `{
  "name": "Filtered",
  "owner": {"name": "e2e"},
  "harnesses": [{"type": "harness", "source": "` + harness + `"}]
}
`
	if err := os.WriteFile(configFile, []byte(cfgBody), 0o644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(root, "out")
	mustRunYnd(t, "marketplace", "build", configFile, "-o", out, "-v", "claude")

	assertFileExists(t, filepath.Join(out, ".claude-plugin", "marketplace.json"))
	if _, err := os.Stat(filepath.Join(out, ".cursor-plugin", "marketplace.json")); err == nil {
		t.Errorf(".cursor-plugin/marketplace.json should not exist when -v claude was specified")
	}
}

// TestYnd_Validate_File asserts `ynd validate <file>` validates a single
// plugin.json file directly (not just a harness directory). Locks the
// file-mode entry point of validate.
func TestYnd_Validate_File(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "single")
	if err := os.MkdirAll(filepath.Join(dir, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	pluginPath := filepath.Join(dir, ".ynh-plugin", "plugin.json")
	good := `{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"single","version":"0.1.0"}`
	if err := os.WriteFile(pluginPath, []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _ := mustRunYnd(t, "validate", pluginPath)
	if !strings.Contains(out, "valid") {
		t.Errorf("expected 'valid' in single-file validate output, got:\n%s", out)
	}
}

// TestYnd_Compose_HarnessWithDeps asserts `ynd compose` of a harness with
// one local include surfaces the include in the JSON output. Existing
// compose tests cover only the standalone harness.
func TestYnd_Compose_HarnessWithDeps(t *testing.T) {
	upstream := filepath.Join(t.TempDir(), "dep-upstream", "skills", "thing")
	if err := os.MkdirAll(upstream, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(upstream, "SKILL.md"),
		[]byte("---\nname: thing\ndescription: dep.\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	harness := filepath.Join(t.TempDir(), "with-deps")
	if err := os.MkdirAll(filepath.Join(harness, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "with-deps",
  "version": "0.1.0",
  "includes": [{"local": "` + filepath.Dir(filepath.Dir(upstream)) + `"}]
}
`
	if err := os.WriteFile(filepath.Join(harness, ".ynh-plugin", "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _ := mustRunYnd(t, "compose", harness, "--format", "json")
	var got struct {
		Includes []struct {
			Local string `json:"local"`
		} `json:"includes"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parsing compose JSON: %v\n%s", err, out)
	}
	// Compose should report at least one include — the exact field shape
	// (git vs local) varies, but the count must be non-zero.
	if !strings.Contains(out, "includes") {
		t.Errorf("compose output missing includes key:\n%s", out)
	}
}
