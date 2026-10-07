package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

// harnessFormatM is the MigrateChain migrator, which converts wherever it is
// run. Every Run on it below is on a fresh t.TempDir().
var harnessFormatM = HarnessFormatMigrator{SourceTrees: true}

func TestHarnessFormatMigrator_Applies(t *testing.T) {
	t.Run("true when only harness.json exists", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, plugin.HarnessFile), `{"name":"x","version":"0.1.0"}`)
		if !harnessFormatM.Applies(dir) {
			t.Error("expected Applies=true")
		}
	})

	t.Run("false when plugin.json already exists", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, plugin.HarnessFile), `{"name":"x","version":"0.1.0"}`)
		if err := os.MkdirAll(filepath.Join(dir, plugin.PluginDir), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, plugin.PluginDir, plugin.PluginFile), `{"name":"x","version":"0.1.0"}`)
		if harnessFormatM.Applies(dir) {
			t.Error("expected Applies=false")
		}
	})

	t.Run("false when neither file exists", func(t *testing.T) {
		dir := t.TempDir()
		if harnessFormatM.Applies(dir) {
			t.Error("expected Applies=false")
		}
	})
}

func TestHarnessFormatMigrator_Run(t *testing.T) {
	t.Run("converts harness.json to plugin.json", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, plugin.HarnessFile), `{"name":"myharness","version":"1.0.0","description":"test"}`)
		mustBeUnderTemp(t, filepath.Dir(dir), dir)

		if err := harnessFormatM.Run(dir); err != nil {
			t.Fatalf("Run: %v", err)
		}

		if _, err := os.Stat(filepath.Join(dir, plugin.HarnessFile)); err == nil {
			t.Error(".harness.json should have been removed")
		}

		hj, err := plugin.LoadPluginJSON(dir)
		if err != nil {
			t.Fatalf("LoadPluginJSON: %v", err)
		}
		if hj.Name != "myharness" {
			t.Errorf("Name = %q, want %q", hj.Name, "myharness")
		}
	})

	t.Run("extracts installed_from to installed.json", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, plugin.HarnessFile), `{
			"name":"myharness","version":"1.0.0",
			"installed_from":{"source_type":"github","source":"https://github.com/org/repo","installed_at":"2026-04-22T00:00:00Z"}
		}`)

		if err := harnessFormatM.Run(dir); err != nil {
			t.Fatalf("Run: %v", err)
		}

		ins, err := plugin.LoadInstalledJSON(dir)
		if err != nil {
			t.Fatalf("LoadInstalledJSON: %v", err)
		}
		if ins.Source != "https://github.com/org/repo" {
			t.Errorf("Source = %q, want %q", ins.Source, "https://github.com/org/repo")
		}

		hj, err := plugin.LoadPluginJSON(dir)
		if err != nil {
			t.Fatalf("LoadPluginJSON: %v", err)
		}
		if hj.InstalledFrom != nil {
			t.Error("plugin.json should not contain installed_from")
		}
	})

	t.Run("idempotent via Applies guard", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, plugin.HarnessFile), `{"name":"x","version":"0.1.0"}`)

		if err := harnessFormatM.Run(dir); err != nil {
			t.Fatalf("first Run: %v", err)
		}
		if harnessFormatM.Applies(dir) {
			t.Error("Applies should return false after migration")
		}
	})

	t.Run("rewrites $schema URL suffix", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, plugin.HarnessFile),
			`{"$schema":"https://eyelock.github.io/ynh/schema/harness.schema.json","name":"x","version":"0.1.0"}`)

		if err := harnessFormatM.Run(dir); err != nil {
			t.Fatalf("Run: %v", err)
		}
		hj, err := plugin.LoadPluginJSON(dir)
		if err != nil {
			t.Fatalf("LoadPluginJSON: %v", err)
		}
		want := "https://eyelock.github.io/ynh/schema/plugin.schema.json"
		if hj.Schema != want {
			t.Errorf("Schema = %q, want %q", hj.Schema, want)
		}
	})

	t.Run("adds $schema when the legacy manifest had none", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, plugin.HarnessFile), `{"name":"x","version":"0.1.0"}`)

		if err := harnessFormatM.Run(dir); err != nil {
			t.Fatalf("Run: %v", err)
		}
		hj, err := plugin.LoadPluginJSON(dir)
		if err != nil {
			t.Fatalf("LoadPluginJSON: %v", err)
		}
		if hj.Schema != pluginSchemaURL {
			t.Errorf("Schema = %q, want %q", hj.Schema, pluginSchemaURL)
		}
	})

	t.Run("preserves non-legacy $schema URLs", func(t *testing.T) {
		dir := t.TempDir()
		custom := "https://my-org.example.com/schemas/custom.json"
		writeFile(t, filepath.Join(dir, plugin.HarnessFile),
			`{"$schema":"`+custom+`","name":"x","version":"0.1.0"}`)

		if err := harnessFormatM.Run(dir); err != nil {
			t.Fatalf("Run: %v", err)
		}
		hj, err := plugin.LoadPluginJSON(dir)
		if err != nil {
			t.Fatalf("LoadPluginJSON: %v", err)
		}
		if hj.Schema != custom {
			t.Errorf("Schema = %q, want %q (unchanged)", hj.Schema, custom)
		}
	})
}

// The load-time migrator never converts a source tree. Run refuses with the
// fix and leaves the tree exactly as it was (#406).
func TestHarnessFormatMigrator_SourceTreeRefusedOutsideMigrateChain(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	root := t.TempDir()
	dir := filepath.Join(root, "src")
	body := `{"name":"x","version":"0.1.0"}`
	writeFile(t, filepath.Join(dir, plugin.HarnessFile), body)

	if !(HarnessFormatMigrator{}).Applies(dir) {
		t.Fatal("Applies must report a legacy tree wherever it is, so the chain can refuse it")
	}
	mustBeUnderTemp(t, root, dir)
	_, err := FormatChain().Run(dir)
	want := dir + " uses the legacy .harness.json manifest, which ynh no longer reads; convert it with: ynd migrate " + dir
	if err == nil || err.Error() != want {
		t.Fatalf("FormatChain().Run error = %v\nwant %s", err, want)
	}
	if got := readFile(t, filepath.Join(dir, plugin.HarnessFile)); got != body {
		t.Errorf(".harness.json changed: %q", got)
	}
	if exists(filepath.Join(dir, plugin.AgentsDir)) {
		t.Error("FormatChain wrote into a source tree")
	}
}

// A cached clone is the upstream's repository: the fix is its maintainer's.
func TestLegacyHarnessManifest_CachedClone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	dir := filepath.Join(home, "cache", "github.com--example-org--repo")
	writeFile(t, filepath.Join(dir, plugin.HarnessFile), `{"name":"x","version":"0.1.0"}`)

	err := LegacyHarnessManifest(dir)
	if err == nil || !strings.Contains(err.Error(), "cached copy of a remote repository") ||
		!strings.Contains(err.Error(), "ask its maintainer") {
		t.Fatalf("LegacyHarnessManifest = %v, want the upstream advice", err)
	}
}

func TestLegacyHarnessManifest_NilWithoutLegacyOnlyTree(t *testing.T) {
	t.Run("no manifest", func(t *testing.T) {
		if err := LegacyHarnessManifest(t.TempDir()); err != nil {
			t.Error(err)
		}
	})
	t.Run("plugin.json wins over a stale .harness.json", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, plugin.HarnessFile), `{"name":"x"}`)
		writeFile(t, filepath.Join(dir, plugin.PluginDir, plugin.PluginFile), `{"name":"x"}`)
		if err := LegacyHarnessManifest(dir); err != nil {
			t.Error(err)
		}
	})
}

// An install from an old binary is ynh's own copy and converts on load.
func TestHarnessFormatMigrator_InstallConvertsOnLoad(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	dir := filepath.Join(home, "harnesses", "local--old")
	writeFile(t, filepath.Join(dir, plugin.HarnessFile), `{"name":"old","version":"0.1.0"}`)

	mustBeUnderTemp(t, home, dir)
	applied, err := FormatChain().Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 || applied[0] != (HarnessFormatMigrator{}).Description() {
		t.Errorf("applied = %v", applied)
	}
	if exists(filepath.Join(dir, plugin.HarnessFile)) || !plugin.IsPluginDir(dir) {
		t.Error("install was not converted")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
