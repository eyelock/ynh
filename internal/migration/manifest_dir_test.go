package migration

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

// These tests move directories. Per .claude/rules/destructive-operations.md
// every path handed to Run, moveManifestDir or AdoptRefreshedManifest is built
// under t.TempDir() and checked by mustBeUnderTemp first. Nothing here breaks
// a guard to see a test fail.

// mustBeUnderTemp fails the test outright unless path is inside root, a
// directory the test got from t.TempDir().
func mustBeUnderTemp(t *testing.T, root, path string) {
	t.Helper()
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		t.Fatalf("refusing to run a mover on %s: not inside the test's temp dir %s", path, root)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

const legacyPlugin = `{"name":"old","version":"0.1.0"}`
const legacyInstalled = `{"source_type":"git","source":"https://github.com/example-org/repo","installed_at":"2026-01-01T00:00:00Z","sha":"abc123"}`

// legacyTree writes a harness at root/name with its manifest in .ynh-plugin.
func legacyTree(t *testing.T, root, name string, withInstalled bool) string {
	t.Helper()
	dir := filepath.Join(root, name)
	writeFile(t, filepath.Join(dir, plugin.LegacyPluginDir, plugin.PluginFile), legacyPlugin)
	if withInstalled {
		writeFile(t, filepath.Join(dir, plugin.LegacyPluginDir, plugin.InstalledFile), legacyInstalled)
	}
	writeFile(t, filepath.Join(dir, "AGENTS.md"), "# old\n")
	return dir
}

func TestManifestDirMigration_Decisions(t *testing.T) {
	cases := []struct {
		name       string
		setup      func(t *testing.T, dir string)
		wantAction manifestDirAction
		wantReason string
	}{
		{
			name:       "no .ynh-plugin",
			setup:      func(t *testing.T, dir string) { writeFile(t, filepath.Join(dir, "AGENTS.md"), "x") },
			wantAction: manifestDirNone,
		},
		{
			name: "only canonical",
			setup: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, plugin.PluginDir, plugin.PluginFile), legacyPlugin)
			},
			wantAction: manifestDirNone,
		},
		{
			name: ".ynh-plugin without a manifest file",
			setup: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, plugin.LegacyPluginDir, "notes.txt"), "x")
			},
			wantAction: manifestDirNone,
		},
		{
			name: "legacy only",
			setup: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, plugin.LegacyPluginDir, plugin.PluginFile), legacyPlugin)
			},
			wantAction: manifestDirMove,
		},
		{
			name: "registry only",
			setup: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, plugin.LegacyPluginDir, plugin.MarketplaceFile), `{}`)
			},
			wantAction: manifestDirMove,
		},
		{
			name: "legacy beside an unrelated .agents",
			setup: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, plugin.LegacyPluginDir, plugin.PluginFile), legacyPlugin)
				writeFile(t, filepath.Join(dir, plugin.AgentsDir, "skills", "x", "SKILL.md"), "x")
			},
			wantAction: manifestDirMove,
		},
		{
			name: "both directories",
			setup: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, plugin.LegacyPluginDir, plugin.PluginFile), legacyPlugin)
				writeFile(t, filepath.Join(dir, plugin.PluginDir, plugin.PluginFile), legacyPlugin)
			},
			wantAction: manifestDirBlocked,
			wantReason: "nothing was merged",
		},
		{
			name: "both directories, canonical empty",
			setup: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, plugin.LegacyPluginDir, plugin.PluginFile), legacyPlugin)
				if err := os.MkdirAll(filepath.Join(dir, plugin.PluginDir), 0o755); err != nil {
					t.Fatal(err)
				}
			},
			wantAction: manifestDirBlocked,
			wantReason: "both",
		},
		{
			name: ".ynh-plugin is a file",
			setup: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, plugin.LegacyPluginDir), "x")
			},
			wantAction: manifestDirBlocked,
			wantReason: "not a directory",
		},
		{
			name: ".ynh-plugin is a symlink",
			setup: func(t *testing.T, dir string) {
				elsewhere := t.TempDir()
				writeFile(t, filepath.Join(elsewhere, plugin.PluginFile), legacyPlugin)
				if err := os.Symlink(elsewhere, filepath.Join(dir, plugin.LegacyPluginDir)); err != nil {
					t.Fatal(err)
				}
			},
			wantAction: manifestDirBlocked,
			wantReason: "symlink",
		},
		{
			name: ".ynh-plugin holds a symlink",
			setup: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "plugin.json"), legacyPlugin)
				if err := os.MkdirAll(filepath.Join(dir, plugin.LegacyPluginDir), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("../plugin.json", filepath.Join(dir, plugin.LegacyPluginDir, plugin.PluginFile)); err != nil {
					t.Fatal(err)
				}
			},
			wantAction: manifestDirBlocked,
			wantReason: "symlink",
		},
		{
			name: ".agents is a symlink",
			setup: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, plugin.LegacyPluginDir, plugin.PluginFile), legacyPlugin)
				if err := os.Symlink(t.TempDir(), filepath.Join(dir, plugin.AgentsDir)); err != nil {
					t.Fatal(err)
				}
			},
			wantAction: manifestDirBlocked,
			wantReason: "symlink",
		},
		{
			name: ".agents is a file",
			setup: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, plugin.LegacyPluginDir, plugin.PluginFile), legacyPlugin)
				writeFile(t, filepath.Join(dir, plugin.AgentsDir), "x")
			},
			wantAction: manifestDirBlocked,
			wantReason: "not a directory",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tc.setup(t, dir)
			action, reason := manifestDirMigration(dir)
			if action != tc.wantAction {
				t.Fatalf("action = %v, want %v (reason %q)", action, tc.wantAction, reason)
			}
			if !strings.Contains(reason, tc.wantReason) {
				t.Errorf("reason %q does not mention %q", reason, tc.wantReason)
			}
			if got := ManifestDirBlocked(dir); (got != "") != (tc.wantAction == manifestDirBlocked) {
				t.Errorf("ManifestDirBlocked = %q, inconsistent with action %v", got, action)
			}
		})
	}
}

// A source tree is moved only by MigrateChain. FormatChain, which every load
// runs, must leave a user's tree exactly as it was.
func TestManifestDirMigrator_SourceTreeOnlyUnderMigrateChain(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	root := t.TempDir()
	dir := legacyTree(t, root, "src", false)

	if (ManifestDirMigrator{}).Applies(dir) {
		t.Fatal("the load-time migrator must not apply to a source tree")
	}
	if _, err := FormatChain().Run(dir); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(dir, plugin.LegacyPluginDir, plugin.PluginFile)) || exists(filepath.Join(dir, plugin.AgentsDir)) {
		t.Fatal("FormatChain changed a source tree")
	}

	mustBeUnderTemp(t, root, dir)
	applied, err := MigrateChain().Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 || applied[0] != (ManifestDirMigrator{}).Description() {
		t.Errorf("applied = %v", applied)
	}
	if got := readFile(t, filepath.Join(dir, plugin.PluginDir, plugin.PluginFile)); got != legacyPlugin {
		t.Errorf("plugin.json after move = %q", got)
	}
	if exists(filepath.Join(dir, plugin.LegacyPluginDir)) {
		t.Error(".ynh-plugin still exists after the move")
	}

	// Idempotent.
	applied, err = MigrateChain().Run(dir)
	if err != nil || len(applied) != 0 {
		t.Errorf("second run applied %v, err %v", applied, err)
	}
}

// An install under YNH_HOME/harnesses is ynh's own copy and moves on load,
// provenance included.
func TestManifestDirMigrator_InstalledTreeMovesOnLoad(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	harnesses := filepath.Join(home, "harnesses")
	dir := legacyTree(t, harnesses, "github.com--example-org--repo--old", true)

	if !(ManifestDirMigrator{}).Applies(dir) {
		t.Fatal("the load-time migrator must apply to an install")
	}
	mustBeUnderTemp(t, home, dir)
	if _, err := FormatChain().Run(dir); err != nil {
		t.Fatal(err)
	}

	if got := plugin.InstalledPath(dir); got != filepath.Join(dir, plugin.PluginDir, plugin.InstalledFile) {
		t.Errorf("installed.json is read from %s, want the canonical dir", got)
	}
	ins, err := plugin.LoadInstalledJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ins.Source != "https://github.com/example-org/repo" || ins.SHA != "abc123" || ins.InstalledAt == "" {
		t.Errorf("provenance lost in the move: %+v", ins)
	}
	if exists(filepath.Join(dir, plugin.LegacyPluginDir)) {
		t.Error(".ynh-plugin still exists after the move")
	}
}

// The harnesses dir itself is not an install, and neither is anything beside it.
func TestInsideHarnessesDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	h := filepath.Join(home, "harnesses")
	cases := map[string]bool{
		h:                                 false,
		filepath.Join(h, "x"):             true,
		filepath.Join(h, "x", "y"):        true,
		filepath.Join(home, "cache", "x"): false,
		filepath.Join(home, "harnesses2"): false,
		home:                              false,
		t.TempDir():                       false,
	}
	for p, want := range cases {
		if got := insideHarnessesDir(p); got != want {
			t.Errorf("insideHarnessesDir(%s) = %v, want %v", p, got, want)
		}
	}
}

// Both directories present: nothing is moved, merged or overwritten, by either
// chain, and Run refuses if called directly.
func TestManifestDirMigrator_BothDirsLeftAlone(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "both")
	writeFile(t, filepath.Join(dir, plugin.LegacyPluginDir, plugin.PluginFile), `{"name":"legacy"}`)
	writeFile(t, filepath.Join(dir, plugin.LegacyPluginDir, plugin.InstalledFile), legacyInstalled)
	writeFile(t, filepath.Join(dir, plugin.PluginDir, plugin.PluginFile), `{"name":"canonical"}`)

	if (ManifestDirMigrator{SourceTrees: true}).Applies(dir) {
		t.Fatal("must not apply when both dirs exist")
	}
	mustBeUnderTemp(t, root, dir)
	if err := (ManifestDirMigrator{SourceTrees: true}).Run(dir); err == nil {
		t.Fatal("Run on a split tree must refuse")
	}
	if got := readFile(t, filepath.Join(dir, plugin.PluginDir, plugin.PluginFile)); got != `{"name":"canonical"}` {
		t.Errorf("canonical plugin.json changed: %q", got)
	}
	if got := readFile(t, filepath.Join(dir, plugin.LegacyPluginDir, plugin.PluginFile)); got != `{"name":"legacy"}` {
		t.Errorf("legacy plugin.json changed: %q", got)
	}
	if exists(filepath.Join(dir, plugin.PluginDir, plugin.InstalledFile)) {
		t.Error("installed.json was merged into the canonical dir")
	}
}

// A .ynh-plugin symlink is never followed: neither it nor its target moves.
func TestManifestDirMigrator_SymlinkLeftAlone(t *testing.T) {
	root := t.TempDir()
	elsewhere := t.TempDir()
	writeFile(t, filepath.Join(elsewhere, plugin.PluginFile), legacyPlugin)
	dir := filepath.Join(root, "linked")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(dir, plugin.LegacyPluginDir)); err != nil {
		t.Fatal(err)
	}

	if (ManifestDirMigrator{SourceTrees: true}).Applies(dir) {
		t.Fatal("must not apply to a symlinked .ynh-plugin")
	}
	mustBeUnderTemp(t, root, dir)
	if err := (ManifestDirMigrator{SourceTrees: true}).Run(dir); err == nil {
		t.Fatal("Run on a symlinked .ynh-plugin must refuse")
	}
	if fi, err := os.Lstat(filepath.Join(dir, plugin.LegacyPluginDir)); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Error("the symlink was changed")
	}
	if !exists(filepath.Join(elsewhere, plugin.PluginFile)) || exists(filepath.Join(dir, plugin.AgentsDir)) {
		t.Error("the symlink target was touched")
	}
}

// The load-time migrator refuses to Run on a source tree even if asked
// directly, not only through Applies.
func TestManifestDirMigrator_RunRefusesSourceTreeWithoutScope(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	root := t.TempDir()
	dir := legacyTree(t, root, "src", false)
	mustBeUnderTemp(t, root, dir)
	if err := (ManifestDirMigrator{}).Run(dir); err == nil {
		t.Fatal("expected a refusal")
	}
	if !exists(filepath.Join(dir, plugin.LegacyPluginDir, plugin.PluginFile)) {
		t.Error("the tree was changed")
	}
}

func TestManifestDirDeprecation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	t.Run("source tree names ynd migrate", func(t *testing.T) {
		dir := legacyTree(t, t.TempDir(), "src", false)
		got := ManifestDirDeprecation(dir)
		if !strings.Contains(got, "deprecated") || !strings.Contains(got, "ynd migrate "+dir) {
			t.Errorf("got %q", got)
		}
	})
	t.Run("cached clone asks the maintainer", func(t *testing.T) {
		dir := legacyTree(t, filepath.Join(home, "cache"), "clone", false)
		got := ManifestDirDeprecation(dir)
		if !strings.Contains(got, "maintainer") || strings.Contains(got, "ynd migrate "+dir) {
			t.Errorf("got %q", got)
		}
	})
	t.Run("movable install is silent", func(t *testing.T) {
		dir := legacyTree(t, filepath.Join(home, "harnesses"), "inst", true)
		if got := ManifestDirDeprecation(dir); got != "" {
			t.Errorf("got %q, want no warning: ynh moves it itself", got)
		}
	})
	t.Run("blocked tree gives the reason", func(t *testing.T) {
		dir := legacyTree(t, t.TempDir(), "both", false)
		writeFile(t, filepath.Join(dir, plugin.PluginDir, plugin.PluginFile), legacyPlugin)
		got := ManifestDirDeprecation(dir)
		if !strings.Contains(got, "nothing was merged") {
			t.Errorf("got %q", got)
		}
	})
	t.Run("notice writes to the given writer", func(t *testing.T) {
		dir := legacyTree(t, t.TempDir(), "src2", false)
		var buf bytes.Buffer
		ManifestDirNotice(&buf)(dir)
		if !strings.HasPrefix(buf.String(), "warning: "+dir) || !strings.HasSuffix(buf.String(), "\n") {
			t.Errorf("got %q", buf.String())
		}
		buf.Reset()
		ManifestDirNotice(&buf)(legacyTree(t, filepath.Join(home, "harnesses"), "inst2", false))
		if buf.Len() != 0 {
			t.Errorf("a movable install must print nothing, got %q", buf.String())
		}
	})
}

// ynh update overlays a source still on .ynh-plugin onto an install already
// moved to .agents/harness. The fresh plugin.json must win, and the install's
// provenance must survive.
func TestAdoptRefreshedManifest(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	src := legacyTree(t, t.TempDir(), "upstream", false)
	writeFile(t, filepath.Join(src, plugin.LegacyPluginDir, plugin.PluginFile), `{"name":"old","version":"0.2.0"}`)

	dst := filepath.Join(home, "harnesses", "github.com--example-org--repo--old")
	writeFile(t, filepath.Join(dst, plugin.PluginDir, plugin.PluginFile), legacyPlugin)
	writeFile(t, filepath.Join(dst, plugin.PluginDir, plugin.InstalledFile), legacyInstalled)
	// What the overlay leaves behind.
	writeFile(t, filepath.Join(dst, plugin.LegacyPluginDir, plugin.PluginFile), `{"name":"old","version":"0.2.0"}`)

	mustBeUnderTemp(t, home, dst)
	if err := AdoptRefreshedManifest(src, dst); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(dst, plugin.PluginDir, plugin.PluginFile)); !strings.Contains(got, "0.2.0") {
		t.Errorf("stale plugin.json still wins: %q", got)
	}
	if got := readFile(t, filepath.Join(dst, plugin.PluginDir, plugin.InstalledFile)); got != legacyInstalled {
		t.Errorf("installed.json changed: %q", got)
	}
	if exists(filepath.Join(dst, plugin.LegacyPluginDir)) {
		t.Error("the emptied .ynh-plugin should be removed")
	}
}

func TestAdoptRefreshedManifest_LeavesOtherTreesAlone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	src := legacyTree(t, t.TempDir(), "upstream", false)

	// Not an install: never touched.
	root := t.TempDir()
	dst := filepath.Join(root, "notinstall")
	writeFile(t, filepath.Join(dst, plugin.PluginDir, plugin.PluginFile), `{"name":"canonical"}`)
	writeFile(t, filepath.Join(dst, plugin.LegacyPluginDir, plugin.PluginFile), `{"name":"legacy"}`)
	mustBeUnderTemp(t, root, dst)
	if err := AdoptRefreshedManifest(src, dst); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(dst, plugin.PluginDir, plugin.PluginFile)); got != `{"name":"canonical"}` {
		t.Errorf("a non-install was changed: %q", got)
	}

	// An install whose upstream has moved: the canonical file came from
	// upstream, so nothing is stale.
	moved := t.TempDir()
	writeFile(t, filepath.Join(moved, plugin.PluginDir, plugin.PluginFile), `{"name":"new"}`)
	inst := filepath.Join(home, "harnesses", "x")
	writeFile(t, filepath.Join(inst, plugin.PluginDir, plugin.PluginFile), `{"name":"new"}`)
	writeFile(t, filepath.Join(inst, plugin.LegacyPluginDir, plugin.PluginFile), `{"name":"leftover"}`)
	mustBeUnderTemp(t, home, inst)
	if err := AdoptRefreshedManifest(moved, inst); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(inst, plugin.PluginDir, plugin.PluginFile)); got != `{"name":"new"}` {
		t.Errorf("canonical plugin.json replaced: %q", got)
	}
}
