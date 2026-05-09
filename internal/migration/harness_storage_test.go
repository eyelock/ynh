package migration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

var storageM = HarnessStorageMigrator{}

// setupHarnessesDir creates a temp YNH_HOME and returns its harnesses subdir.
func setupHarnessesDir(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	dir := filepath.Join(home, "harnesses")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestHarnessStorageMigrator_Applies(t *testing.T) {
	harnessesDir := setupHarnessesDir(t)

	t.Run("true for flat dir with plugin.json", func(t *testing.T) {
		dir := filepath.Join(harnessesDir, "david")
		writeFile(t, filepath.Join(dir, plugin.PluginDir, plugin.PluginFile), `{"name":"david","version":"0.1.0"}`)
		if !storageM.Applies(dir) {
			t.Error("expected Applies=true")
		}
	})

	t.Run("false for namespaced dir", func(t *testing.T) {
		dir := filepath.Join(harnessesDir, "eyelock--assistants", "david")
		writeFile(t, filepath.Join(dir, plugin.PluginDir, plugin.PluginFile), `{"name":"david","version":"0.1.0"}`)
		if storageM.Applies(dir) {
			t.Error("expected Applies=false for already-namespaced dir")
		}
	})

	t.Run("false when plugin.json missing", func(t *testing.T) {
		dir := filepath.Join(harnessesDir, "bare")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if storageM.Applies(dir) {
			t.Error("expected Applies=false when no plugin.json")
		}
	})
}

func TestHarnessStorageMigrator_Run(t *testing.T) {
	t.Run("moves to namespaced dir using installed.json", func(t *testing.T) {
		harnessesDir := setupHarnessesDir(t)

		dir := filepath.Join(harnessesDir, "david")
		writeFile(t, filepath.Join(dir, plugin.PluginDir, plugin.PluginFile), `{"name":"david","version":"0.1.0"}`)
		writeFile(t, filepath.Join(dir, plugin.PluginDir, plugin.InstalledFile),
			`{"source_type":"github","source":"https://github.com/eyelock/assistants","installed_at":"2026-04-22T00:00:00Z"}`)
		writeFile(t, filepath.Join(dir, "skills", "README.md"), "# skills")

		if err := storageM.Run(dir); err != nil {
			t.Fatalf("Run: %v", err)
		}

		if _, err := os.Stat(dir); err == nil {
			t.Error("flat dir should have been removed")
		}

		dest := filepath.Join(harnessesDir, "eyelock--assistants", "david")
		if _, err := os.Stat(dest); err != nil {
			t.Errorf("namespaced dir not found: %v", err)
		}

		if !plugin.IsPluginDir(dest) {
			t.Error("plugin.json missing from namespaced dir")
		}

		ins, err := plugin.LoadInstalledJSON(dest)
		if err != nil {
			t.Fatalf("LoadInstalledJSON: %v", err)
		}
		if ins.Namespace != "eyelock/assistants" {
			t.Errorf("Namespace = %q, want %q", ins.Namespace, "eyelock/assistants")
		}
	})

	t.Run("falls back to local/unknown when no installed.json", func(t *testing.T) {
		harnessesDir := setupHarnessesDir(t)

		dir := filepath.Join(harnessesDir, "mystery")
		writeFile(t, filepath.Join(dir, plugin.PluginDir, plugin.PluginFile), `{"name":"mystery","version":"0.1.0"}`)

		if err := storageM.Run(dir); err != nil {
			t.Fatalf("Run: %v", err)
		}

		dest := filepath.Join(harnessesDir, "local--unknown", "mystery")
		if _, err := os.Stat(dest); err != nil {
			t.Errorf("fallback dir not found: %v", err)
		}
	})
}

func TestCopyDir(t *testing.T) {
	src := t.TempDir()
	// Populate: a file, a nested dir, and a file inside it.
	writeFile(t, filepath.Join(src, "top.txt"), "hello")
	writeFile(t, filepath.Join(src, "sub", "nested.txt"), "world")

	dst := filepath.Join(t.TempDir(), "dst")
	if err := copyDir(src, dst); err != nil {
		t.Fatalf("copyDir: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dst, "top.txt"))
	if err != nil {
		t.Fatalf("top.txt missing: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("top.txt = %q, want hello", got)
	}

	got2, err := os.ReadFile(filepath.Join(dst, "sub", "nested.txt"))
	if err != nil {
		t.Fatalf("sub/nested.txt missing: %v", err)
	}
	if string(got2) != "world" {
		t.Errorf("sub/nested.txt = %q, want world", got2)
	}
}

func TestCopyFile(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src.txt")
	if err := os.WriteFile(src, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "dst.txt")
	if err := copyFile(src, dst, 0o644); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("dst missing: %v", err)
	}
	if string(got) != "content" {
		t.Errorf("dst = %q, want content", got)
	}
}
