//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestYnd_Migrate verifies `ynd migrate <dir>` runs the format migration
// chain over a directory tree containing a legacy `.harness.json`,
// converting it to the modern `.ynh-plugin/plugin.json` layout.
//
// The CLI command exposes the same migration chain that `ynh install`
// runs implicitly — locks the developer-facing entry point.
func TestYnd_Migrate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "legacy-tree", "harness-1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"$schema":"https://eyelock.github.io/ynh/schema/harness.schema.json","name":"legacy-1","version":"0.1.0"}`
	if err := os.WriteFile(filepath.Join(dir, ".harness.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	mustRunYnd(t, "migrate", filepath.Dir(dir))

	if _, err := os.Stat(filepath.Join(dir, ".harness.json")); !os.IsNotExist(err) {
		t.Errorf(".harness.json should be removed after migrate, err=%v", err)
	}
	assertFileExists(t, filepath.Join(dir, ".ynh-plugin", "plugin.json"))
}

// TestYnd_Marketplace_Build asserts `ynd marketplace build` reads a
// marketplace config and produces vendor-specific marketplace.json files
// (one per vendor manifest dir).
func TestYnd_Marketplace_Build(t *testing.T) {
	root := t.TempDir()

	// One harness referenced by the marketplace config.
	harness := filepath.Join(root, "harnesses", "demo")
	if err := os.MkdirAll(filepath.Join(harness, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	plugin := `{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"demo","version":"0.1.0","description":"demo harness"}`
	if err := os.WriteFile(filepath.Join(harness, ".ynh-plugin", "plugin.json"), []byte(plugin), 0o644); err != nil {
		t.Fatal(err)
	}

	configFile := filepath.Join(root, "marketplace.json")
	cfgBody := fmt.Sprintf(`{
  "name": "Test Marketplace",
  "owner": {"name": "e2e", "email": "e2e@example.invalid"},
  "harnesses": [
    {"type": "harness", "source": %q}
  ]
}
`, harness)
	if err := os.WriteFile(configFile, []byte(cfgBody), 0o644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(root, "out")
	mustRunYnd(t, "marketplace", "build", configFile, "-o", out)

	// Claude and Cursor each carry a marketplace index in their manifest
	// dir; Codex uses a different distribution flow and is not produced
	// by `marketplace build`.
	for _, vendorDir := range []string{".claude-plugin", ".cursor-plugin"} {
		assertFileExists(t, filepath.Join(out, vendorDir, "marketplace.json"))
	}
}
