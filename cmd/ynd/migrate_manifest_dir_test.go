package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

// ynd migrate moves .ynh-plugin/ to .agents/harness/ (#404). Every target is
// under t.TempDir(); nothing here points the command at a real tree.

func writeLegacyManifest(t *testing.T, dir string) {
	t.Helper()
	p := filepath.Join(dir, plugin.LegacyPluginDir, plugin.PluginFile)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"name":"old","version":"0.1.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMigrate_ManifestDir_DryRunListsTheMove(t *testing.T) {
	root := t.TempDir()
	harness := filepath.Join(root, "old")
	writeLegacyManifest(t, harness)

	var out bytes.Buffer
	withStdout(t, &out, func() {
		if err := cmdMigrate([]string{"--dry-run", root}); err != nil {
			t.Errorf("dry run: %v", err)
		}
	})
	for _, want := range []string{harness, ".ynh-plugin/ → .agents/harness/", "Dry run: nothing was changed."} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("dry run output missing %q:\n%s", want, out.String())
		}
	}
	if _, err := os.Stat(filepath.Join(harness, plugin.LegacyPluginDir, plugin.PluginFile)); err != nil {
		t.Errorf("dry run changed the tree: %v", err)
	}
}

func TestMigrate_ManifestDir_MovesIt(t *testing.T) {
	root := t.TempDir()
	harness := filepath.Join(root, "old")
	writeLegacyManifest(t, harness)

	var out bytes.Buffer
	withStdout(t, &out, func() {
		if err := cmdMigrate([]string{"-y", root}); err != nil {
			t.Errorf("migrate: %v", err)
		}
	})
	if _, err := os.Stat(filepath.Join(harness, plugin.PluginDir, plugin.PluginFile)); err != nil {
		t.Errorf("manifest not moved: %v\n%s", err, out.String())
	}
	if _, err := os.Lstat(filepath.Join(harness, plugin.LegacyPluginDir)); !os.IsNotExist(err) {
		t.Errorf(".ynh-plugin still there after migrate")
	}
}

func TestMigrate_ManifestDir_BothDirsLeftAlone(t *testing.T) {
	root := t.TempDir()
	harness := filepath.Join(root, "both")
	writeLegacyManifest(t, harness)
	canonical := filepath.Join(harness, plugin.PluginDir, plugin.PluginFile)
	if err := os.MkdirAll(filepath.Dir(canonical), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonical, []byte(`{"name":"new","version":"0.1.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	withStdout(t, &out, func() {
		if err := cmdMigrate([]string{"-y", root}); err != nil {
			t.Errorf("migrate: %v", err)
		}
	})
	if !strings.Contains(out.String(), "Left alone: "+harness) || !strings.Contains(out.String(), "nothing was merged") {
		t.Errorf("both-dirs tree not reported:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(harness, plugin.LegacyPluginDir, plugin.PluginFile)); err != nil {
		t.Errorf("legacy manifest moved: %v", err)
	}
	data, err := os.ReadFile(canonical)
	if err != nil || !strings.Contains(string(data), `"new"`) {
		t.Errorf("canonical manifest changed: %q %v", data, err)
	}
}
