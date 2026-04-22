package migration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

var harnessFormatM = HarnessFormatMigrator{}

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

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
