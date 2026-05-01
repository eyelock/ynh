package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

// installForkTestHarness writes a minimal harness into YNH_HOME/harnesses/<name>.
// Returns the installed directory path.
func installForkTestHarness(t *testing.T, home, name string, ins *plugin.InstalledJSON) string {
	t.Helper()
	dir := filepath.Join(home, "harnesses", name)
	if err := os.MkdirAll(filepath.Join(dir, plugin.PluginDir), 0o755); err != nil {
		t.Fatal(err)
	}
	hj := `{"name":"` + name + `","version":"0.1.0","default_vendor":"claude"}`
	if err := os.WriteFile(filepath.Join(dir, plugin.PluginDir, plugin.PluginFile), []byte(hj), 0o644); err != nil {
		t.Fatal(err)
	}
	if ins != nil {
		if err := plugin.SaveInstalledJSON(dir, ins); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestCmdFork_BasicText(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	installForkTestHarness(t, home, "demo", &plugin.InstalledJSON{
		SourceType:  "git",
		Source:      "github.com/example/demo",
		Ref:         "main",
		SHA:         "abc123",
		InstalledAt: "2026-01-01T00:00:00Z",
	})

	dest := filepath.Join(t.TempDir(), "my-demo")
	var stdout bytes.Buffer
	if err := cmdForkTo([]string{"demo", "--to", dest}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdForkTo: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "Forked harness") {
		t.Errorf("expected 'Forked harness' in output, got: %s", out)
	}
	if !strings.Contains(out, dest) {
		t.Errorf("expected dest path in output, got: %s", out)
	}

	// Destination must exist with plugin.json
	if _, err := os.Stat(filepath.Join(dest, plugin.PluginDir, plugin.PluginFile)); err != nil {
		t.Errorf("plugin.json not found in dest: %v", err)
	}
}

func TestCmdFork_DefaultDestination(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	installForkTestHarness(t, home, "demo", nil)

	// Override cwd to a temp dir so the default --to lands predictably
	cwd := t.TempDir()
	if err := os.Chdir(cwd); err != nil {
		t.Skip("cannot chdir in test environment")
	}
	t.Cleanup(func() { _ = os.Chdir("/") })

	var stdout bytes.Buffer
	if err := cmdForkTo([]string{"demo"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdForkTo: %v", err)
	}

	expected := filepath.Join(cwd, "demo")
	if _, err := os.Stat(filepath.Join(expected, plugin.PluginDir, plugin.PluginFile)); err != nil {
		t.Errorf("plugin.json not at expected default dest %s: %v", expected, err)
	}
}

func TestCmdFork_DestAlreadyExists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	installForkTestHarness(t, home, "demo", nil)

	dest := t.TempDir() // already exists
	err := cmdForkTo([]string{"demo", "--to", dest}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected error for existing destination, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' in error, got: %v", err)
	}
}

// installNamespacedForkTestHarness writes a harness under
// YNH_HOME/harnesses/<ns--repo>/<name>/, mirroring how registry installs land.
func installNamespacedForkTestHarness(t *testing.T, home, fsNS, name string, ins *plugin.InstalledJSON) string {
	t.Helper()
	dir := filepath.Join(home, "harnesses", fsNS, name)
	if err := os.MkdirAll(filepath.Join(dir, plugin.PluginDir), 0o755); err != nil {
		t.Fatal(err)
	}
	hj := `{"name":"` + name + `","version":"0.1.0","default_vendor":"claude"}`
	if err := os.WriteFile(filepath.Join(dir, plugin.PluginDir, plugin.PluginFile), []byte(hj), 0o644); err != nil {
		t.Fatal(err)
	}
	if ins != nil {
		if err := plugin.SaveInstalledJSON(dir, ins); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestCmdFork_NamespacedInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	installNamespacedForkTestHarness(t, home, "eyelock--assistants", "researcher", &plugin.InstalledJSON{
		SourceType:   "registry",
		Source:       "github.com/eyelock/assistants",
		Ref:          "main",
		SHA:          "abc123",
		RegistryName: "eyelock-assistants",
		InstalledAt:  "2026-01-01T00:00:00Z",
	})

	dest := filepath.Join(t.TempDir(), "forked-researcher")
	if err := cmdForkTo([]string{"researcher", "--to", dest}, io.Discard, io.Discard); err != nil {
		t.Fatalf("cmdForkTo: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dest, plugin.PluginDir, plugin.PluginFile)); err != nil {
		t.Errorf("plugin.json not found in dest: %v", err)
	}

	ins, err := plugin.LoadInstalledJSON(dest)
	if err != nil {
		t.Fatalf("LoadInstalledJSON: %v", err)
	}
	if ins.ForkedFrom == nil || ins.ForkedFrom.RegistryName != "eyelock-assistants" {
		t.Errorf("forked_from registry not preserved: %+v", ins.ForkedFrom)
	}
}

func TestCmdFork_NotFound(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	dest := filepath.Join(t.TempDir(), "nowhere")
	err := cmdForkTo([]string{"nonexistent", "--to", dest}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected error for non-existent harness")
	}
}

func TestCmdFork_ForkedFromPopulated(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	installForkTestHarness(t, home, "demo", &plugin.InstalledJSON{
		SourceType:   "registry",
		Source:       "github.com/org/demo",
		Ref:          "v0.1.0",
		SHA:          "deadbeef",
		RegistryName: "org-registry",
		InstalledAt:  "2026-01-01T00:00:00Z",
	})

	dest := filepath.Join(t.TempDir(), "my-demo")
	if err := cmdForkTo([]string{"demo", "--to", dest}, io.Discard, io.Discard); err != nil {
		t.Fatalf("cmdForkTo: %v", err)
	}

	ins, err := plugin.LoadInstalledJSON(dest)
	if err != nil {
		t.Fatalf("LoadInstalledJSON: %v", err)
	}
	if ins.SourceType != "local" {
		t.Errorf("source_type = %q, want local", ins.SourceType)
	}
	if ins.ForkedFrom == nil {
		t.Fatal("forked_from is nil")
	}
	if ins.ForkedFrom.SourceType != "registry" {
		t.Errorf("forked_from.source_type = %q, want registry", ins.ForkedFrom.SourceType)
	}
	if ins.ForkedFrom.Source != "github.com/org/demo" {
		t.Errorf("forked_from.source = %q, want github.com/org/demo", ins.ForkedFrom.Source)
	}
	if ins.ForkedFrom.SHA != "deadbeef" {
		t.Errorf("forked_from.sha = %q, want deadbeef", ins.ForkedFrom.SHA)
	}
	if ins.ForkedFrom.RegistryName != "org-registry" {
		t.Errorf("forked_from.registry_name = %q, want org-registry", ins.ForkedFrom.RegistryName)
	}
}

func TestCmdFork_ForkedFromLocalFallback(t *testing.T) {
	// Harness with no installed.json — forked_from.source_type should be "local"
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	installForkTestHarness(t, home, "demo", nil) // no provenance

	dest := filepath.Join(t.TempDir(), "my-demo")
	if err := cmdForkTo([]string{"demo", "--to", dest}, io.Discard, io.Discard); err != nil {
		t.Fatalf("cmdForkTo: %v", err)
	}

	ins, err := plugin.LoadInstalledJSON(dest)
	if err != nil {
		t.Fatalf("LoadInstalledJSON: %v", err)
	}
	if ins.ForkedFrom == nil {
		t.Fatal("forked_from is nil")
	}
	if ins.ForkedFrom.SourceType != "local" {
		t.Errorf("forked_from.source_type = %q, want local", ins.ForkedFrom.SourceType)
	}
}

func TestCmdFork_JSONOutput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	installForkTestHarness(t, home, "demo", &plugin.InstalledJSON{
		SourceType:  "git",
		Source:      "github.com/example/demo",
		InstalledAt: "2026-01-01T00:00:00Z",
	})

	dest := filepath.Join(t.TempDir(), "my-demo")
	var stdout bytes.Buffer
	if err := cmdForkTo([]string{"demo", "--to", dest, "--format", "json"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdForkTo: %v", err)
	}

	var result forkResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, stdout.String())
	}
	if result.Name != "demo" {
		t.Errorf("name = %q, want demo", result.Name)
	}
	if result.Path != dest {
		t.Errorf("path = %q, want %s", result.Path, dest)
	}
	if result.Capabilities == "" {
		t.Errorf("capabilities missing")
	}
	if result.InstalledFrom == nil {
		t.Fatal("installed_from is nil")
	}
	if result.InstalledFrom.SourceType != "local" {
		t.Errorf("installed_from.source_type = %q, want local", result.InstalledFrom.SourceType)
	}
	if result.InstalledFrom.ForkedFrom == nil {
		t.Errorf("installed_from.forked_from is nil")
	}
}

func TestCmdFork_UnknownFlag(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	err := cmdForkTo([]string{"demo", "--nope"}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
	if !strings.Contains(err.Error(), "--nope") {
		t.Errorf("expected flag name in error, got: %v", err)
	}
}

func TestCmdFork_MissingToValue(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	err := cmdForkTo([]string{"demo", "--to"}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected error for missing --to value")
	}
}

func TestCmdUpdate_ForkBlocked(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	installForkTestHarness(t, home, "forked-demo", &plugin.InstalledJSON{
		SourceType:  "local",
		Source:      "/some/local/path",
		InstalledAt: "2026-01-01T00:00:00Z",
		ForkedFrom: &plugin.ForkedFromJSON{
			SourceType: "git",
			Source:     "github.com/example/demo",
		},
	})

	err := cmdUpdate([]string{"forked-demo"})
	if err == nil {
		t.Fatal("expected error for ynh update on a fork")
	}
	if !strings.Contains(err.Error(), "fork") {
		t.Errorf("expected 'fork' in error message, got: %v", err)
	}
}

func TestCmdInstall_PreservesForkedFrom(t *testing.T) {
	// When installing a local dir that has a forked_from in its installed.json,
	// the installed harness should carry the forked_from forward.
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	// Create a local harness directory that looks like it was forked
	srcDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(srcDir, plugin.PluginDir), 0o755); err != nil {
		t.Fatal(err)
	}
	hj := `{"name":"myfork","version":"0.1.0","default_vendor":"claude"}`
	if err := os.WriteFile(filepath.Join(srcDir, plugin.PluginDir, plugin.PluginFile), []byte(hj), 0o644); err != nil {
		t.Fatal(err)
	}
	srcIns := &plugin.InstalledJSON{
		SourceType:  "local",
		Source:      srcDir,
		InstalledAt: "2026-01-01T00:00:00Z",
		ForkedFrom: &plugin.ForkedFromJSON{
			SourceType: "git",
			Source:     "github.com/example/upstream",
			Version:    "0.1.0",
		},
	}
	if err := plugin.SaveInstalledJSON(srcDir, srcIns); err != nil {
		t.Fatal(err)
	}

	if err := cmdInstall([]string{srcDir}); err != nil {
		t.Fatalf("cmdInstall: %v", err)
	}

	installDir := filepath.Join(home, "harnesses", "myfork")
	ins, err := plugin.LoadInstalledJSON(installDir)
	if err != nil {
		t.Fatalf("LoadInstalledJSON after install: %v", err)
	}
	if ins.ForkedFrom == nil {
		t.Fatal("forked_from not preserved after ynh install")
	}
	if ins.ForkedFrom.Source != "github.com/example/upstream" {
		t.Errorf("forked_from.source = %q, want github.com/example/upstream", ins.ForkedFrom.Source)
	}
}
