package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
)

// installForkTestHarness writes a minimal schema-2 harness install at
// YNH_HOME/harnesses/local--<name>/ (canonical id "local/<name>") so the
// flat name remains free for the fork's pointer registration. Returns the
// installed directory path; callers pass the canonical id ("local/<name>")
// to cmdForkTo.
func installForkTestHarness(t *testing.T, home, name string, ins *plugin.InstalledJSON) string {
	t.Helper()
	dir := harness.InstalledDirByID("local/" + name)
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
	if err := cmdForkTo([]string{"local/demo", "-o", dest}, &stdout, io.Discard); err != nil {
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
	if err := cmdForkTo([]string{"local/demo"}, &stdout, io.Discard); err != nil {
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
	err := cmdForkTo([]string{"local/demo", "-o", dest}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected error for existing destination, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' in error, got: %v", err)
	}
}

// installNamespacedForkTestHarness writes a schema-2 namespaced harness at
// YNH_HOME/harnesses/<host--ns--name>/, mirroring how registry installs land.
// fsNS retains the legacy "ns--repo" form for caller-side compatibility; the
// helper appends --<name> internally to produce the schema-2 fsname.
func installNamespacedForkTestHarness(t *testing.T, home, fsNS, name string, ins *plugin.InstalledJSON) string {
	t.Helper()
	dir := filepath.Join(home, "harnesses", fsNS+"--"+name)
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
	if err := cmdForkTo([]string{"eyelock/assistants/researcher", "-o", dest}, io.Discard, io.Discard); err != nil {
		t.Fatalf("cmdForkTo: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dest, plugin.PluginDir, plugin.PluginFile)); err != nil {
		t.Errorf("plugin.json not found in dest: %v", err)
	}

	ptr, err := harness.LoadPointerByID("local/researcher")
	if err != nil || ptr == nil {
		t.Fatalf("LoadPointerByID: ptr=%v err=%v", ptr, err)
	}
	if ptr.ForkedFrom == nil || ptr.ForkedFrom.RegistryName != "eyelock-assistants" {
		t.Errorf("forked_from registry not preserved: %+v", ptr.ForkedFrom)
	}
}

func TestCmdFork_NotFound(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	dest := filepath.Join(t.TempDir(), "nowhere")
	err := cmdForkTo([]string{"local/nonexistent", "-o", dest}, io.Discard, io.Discard)
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
	if err := cmdForkTo([]string{"local/demo", "-o", dest}, io.Discard, io.Discard); err != nil {
		t.Fatalf("cmdForkTo: %v", err)
	}

	// Provenance lives on the pointer (schema 3+), not in the source tree.
	ptr, err := harness.LoadPointerByID("local/demo")
	if err != nil || ptr == nil {
		t.Fatalf("LoadPointerByID: ptr=%v err=%v", ptr, err)
	}
	if ptr.SourceType != "local" {
		t.Errorf("source_type = %q, want local", ptr.SourceType)
	}
	if ptr.ForkedFrom == nil {
		t.Fatal("forked_from is nil")
	}
	if ptr.ForkedFrom.SourceType != "registry" {
		t.Errorf("forked_from.source_type = %q, want registry", ptr.ForkedFrom.SourceType)
	}
	if ptr.ForkedFrom.Source != "github.com/org/demo" {
		t.Errorf("forked_from.source = %q, want github.com/org/demo", ptr.ForkedFrom.Source)
	}
	if ptr.ForkedFrom.SHA != "deadbeef" {
		t.Errorf("forked_from.sha = %q, want deadbeef", ptr.ForkedFrom.SHA)
	}
	if ptr.ForkedFrom.RegistryName != "org-registry" {
		t.Errorf("forked_from.registry_name = %q, want org-registry", ptr.ForkedFrom.RegistryName)
	}

	// User's source tree must not carry ynh-owned provenance.
	if _, err := os.Stat(filepath.Join(dest, plugin.PluginDir, plugin.InstalledFile)); !os.IsNotExist(err) {
		t.Errorf("installed.json should not exist in source tree: err=%v", err)
	}
}

func TestCmdFork_ForkedFromLocalFallback(t *testing.T) {
	// Harness with no installed.json — forked_from.source_type should be "local"
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	installForkTestHarness(t, home, "demo", nil) // no provenance

	dest := filepath.Join(t.TempDir(), "my-demo")
	if err := cmdForkTo([]string{"local/demo", "-o", dest}, io.Discard, io.Discard); err != nil {
		t.Fatalf("cmdForkTo: %v", err)
	}

	ptr, err := harness.LoadPointerByID("local/demo")
	if err != nil || ptr == nil {
		t.Fatalf("LoadPointerByID: ptr=%v err=%v", ptr, err)
	}
	if ptr.ForkedFrom == nil {
		t.Fatal("forked_from is nil")
	}
	if ptr.ForkedFrom.SourceType != "local" {
		t.Errorf("forked_from.source_type = %q, want local", ptr.ForkedFrom.SourceType)
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
	if err := cmdForkTo([]string{"local/demo", "-o", dest, "--format", "json"}, &stdout, io.Discard); err != nil {
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

	err := cmdForkTo([]string{"local/demo", "--nope"}, io.Discard, io.Discard)
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

	err := cmdForkTo([]string{"local/demo", "-o"}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected error for missing -o value")
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

func TestCmdFork_WritesPointerAndLauncher(t *testing.T) {
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
	if err := cmdForkTo([]string{"local/demo", "-o", dest}, io.Discard, io.Discard); err != nil {
		t.Fatalf("cmdForkTo: %v", err)
	}

	// Pointer file written under ~/.ynh/installed/local--demo.json
	ptr, err := harness.LoadPointerByID("local/demo")
	if err != nil {
		t.Fatalf("LoadPointerByID: %v", err)
	}
	if ptr == nil {
		t.Fatal("pointer not written")
	}
	absDest, _ := filepath.Abs(dest)
	if ptr.Source != absDest {
		t.Errorf("pointer.source = %q, want %q", ptr.Source, absDest)
	}
	if ptr.SourceType != "local" {
		t.Errorf("pointer.source_type = %q, want local", ptr.SourceType)
	}

	// Launcher generated under ~/.ynh/bin/demo
	launcher := filepath.Join(config.BinDir(), "demo")
	if _, err := os.Stat(launcher); err != nil {
		t.Errorf("launcher not generated: %v", err)
	}
}

func TestCmdFork_ClashWithExistingFlatTree(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	// Pre-existing tree for "local/demo": write at both the schema-2 path
	// (so LoadQualified resolves the source) and at the legacy flat path
	// (so the schema-1 clash check in fork.go fires).
	for _, dir := range []string{
		harness.InstalledDirByID("local/demo"),
		harness.InstalledDir("demo"),
	} {
		if err := os.MkdirAll(filepath.Join(dir, plugin.PluginDir), 0o755); err != nil {
			t.Fatal(err)
		}
		hj := `{"name":"demo","version":"0.1.0","default_vendor":"claude"}`
		if err := os.WriteFile(filepath.Join(dir, plugin.PluginDir, plugin.PluginFile), []byte(hj), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	dest := filepath.Join(t.TempDir(), "my-demo")
	err := cmdForkTo([]string{"local/demo", "-o", dest}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected clash error, got nil")
	}
	if !strings.Contains(err.Error(), "already installed") {
		t.Errorf("expected 'already installed' in error, got: %v", err)
	}
}

func TestCmdFork_ClashWithExistingPointer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	installForkTestHarness(t, home, "demo", nil)

	// First fork registers the pointer
	dest1 := filepath.Join(t.TempDir(), "demo-one")
	if err := cmdForkTo([]string{"local/demo", "-o", dest1}, io.Discard, io.Discard); err != nil {
		t.Fatalf("first fork: %v", err)
	}

	// Second fork must clash on the pointer
	dest2 := filepath.Join(t.TempDir(), "demo-two")
	err := cmdForkTo([]string{"local/demo", "-o", dest2}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected clash error on second fork, got nil")
	}
	if !strings.Contains(err.Error(), "already installed") {
		t.Errorf("expected 'already installed' in error, got: %v", err)
	}
}

func TestCmdUninstall_PointerRemovesPointerNotSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	installForkTestHarness(t, home, "demo", nil)

	dest := filepath.Join(t.TempDir(), "my-demo")
	if err := cmdForkTo([]string{"local/demo", "-o", dest}, io.Discard, io.Discard); err != nil {
		t.Fatalf("fork: %v", err)
	}

	// cmdUninstall by canonical id routes through the pointer-first path;
	// the source tree under the user's chosen path is preserved — the test
	// contract is that uninstalling a forked pointer removes the
	// registration, not the user-owned source.
	if err := cmdUninstall([]string{"local/demo"}); err != nil {
		t.Fatalf("uninstall: %v", err)
	}

	// Pointer gone (schema-2 id-keyed, which is what fork now writes)
	if ptr, _ := harness.LoadPointerByID("local/demo"); ptr != nil {
		t.Errorf("pointer still present after uninstall: %+v", ptr)
	}
	// Source tree preserved
	if _, err := os.Stat(filepath.Join(dest, plugin.PluginDir, plugin.PluginFile)); err != nil {
		t.Errorf("source tree was deleted on uninstall: %v", err)
	}
	// Launcher cleaned up
	if _, err := os.Stat(filepath.Join(config.BinDir(), "demo")); err == nil {
		t.Errorf("launcher still present after uninstall")
	}
}

func TestCmdFork_WithName(t *testing.T) {
	// --name lets a user fork a harness while keeping upstream installed.
	// Source "demo" is flat-installed; fork registers as "my-demo" with
	// the manifest rewritten so identity is coherent.
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	// Source as a schema-2 install for "local/demo" (the case --name solves)
	srcDir := harness.InstalledDirByID("local/demo")
	if err := os.MkdirAll(filepath.Join(srcDir, plugin.PluginDir), 0o755); err != nil {
		t.Fatal(err)
	}
	hj := `{"name":"demo","version":"0.1.0","default_vendor":"claude"}`
	if err := os.WriteFile(filepath.Join(srcDir, plugin.PluginDir, plugin.PluginFile), []byte(hj), 0o644); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "fork-tree")
	if err := cmdForkTo([]string{"local/demo", "-o", dest, "--name", "my-demo"}, io.Discard, io.Discard); err != nil {
		t.Fatalf("cmdForkTo: %v", err)
	}

	// Pointer registered under the new name, source unchanged
	ptr, err := harness.LoadPointerByID("local/my-demo")
	if err != nil || ptr == nil {
		t.Fatalf("pointer for new name not written: %v", err)
	}
	if old, _ := harness.LoadPointerByID("local/demo"); old != nil {
		t.Errorf("pointer under source name should not exist: %+v", old)
	}

	// Manifest in the fork tree was rewritten — identity coherence
	got, err := plugin.LoadPluginJSON(dest)
	if err != nil {
		t.Fatalf("LoadPluginJSON: %v", err)
	}
	if got.Name != "my-demo" {
		t.Errorf("fork manifest name = %q, want my-demo", got.Name)
	}

	// Source manifest untouched
	srcManifest, _ := plugin.LoadPluginJSON(srcDir)
	if srcManifest.Name != "demo" {
		t.Errorf("source manifest name was mutated: %q", srcManifest.Name)
	}

	// Launcher under the new name
	if _, err := os.Stat(filepath.Join(config.BinDir(), "my-demo")); err != nil {
		t.Errorf("launcher for new name not generated: %v", err)
	}

	// forked_from preserved (provenance of the upstream identity)
	if ptr.ForkedFrom == nil {
		t.Error("forked_from should be populated")
	}
}

func TestCmdFork_NameInvalid(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	installForkTestHarness(t, home, "demo", nil)

	dest := filepath.Join(t.TempDir(), "fork-tree")
	err := cmdForkTo([]string{"local/demo", "-o", dest, "--name", "bad/name"}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected validation error for invalid --name")
	}
	if !strings.Contains(err.Error(), "--name") {
		t.Errorf("expected '--name' in error, got: %v", err)
	}
}

func TestCmdFork_NameClashesOnNewName(t *testing.T) {
	// Clash check uses the new name, not the source name.
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	installForkTestHarness(t, home, "demo", nil)

	// Pre-register a pointer named "my-demo"
	if err := harness.SavePointer(&harness.Pointer{
		Name: "my-demo",
		InstalledJSON: plugin.InstalledJSON{
			SourceType:  "local",
			Source:      t.TempDir(),
			InstalledAt: "2026-05-01T00:00:00Z",
		},
	}); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "fork-tree")
	err := cmdForkTo([]string{"local/demo", "-o", dest, "--name", "my-demo"}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected clash error on --name target")
	}
	if !strings.Contains(err.Error(), "my-demo") {
		t.Errorf("expected 'my-demo' in error, got: %v", err)
	}
}

func TestCmdFork_MissingNameValue(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	err := cmdForkTo([]string{"local/demo", "--name"}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected error for missing --name value")
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

	// Pointer-form (local) install: the provenance — including forked_from
	// — lives on the pointer file, not in the source tree.
	_ = home
	ptr, err := harness.LoadPointerByID("local/myfork")
	if err != nil || ptr == nil {
		t.Fatalf("LoadPointerByID after install: ptr=%v err=%v", ptr, err)
	}
	if ptr.ForkedFrom == nil {
		t.Fatal("forked_from not preserved after ynh install")
	}
	if ptr.ForkedFrom.Source != "github.com/example/upstream" {
		t.Errorf("forked_from.source = %q, want github.com/example/upstream", ptr.ForkedFrom.Source)
	}
}
