package migration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

// write a registry.json in a fresh temp dir and return the dir.
func regDir(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "registry.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// "registry.json" is not an ynh-specific filename. npm and others use it, and
// Applies matched on the name alone, so an unrelated file was decoded into
// zero values, overwritten with an empty marketplace, and deleted (#348).
//
// These exercise the predicate only. Nothing here calls Run, so no test can
// delete anything, per .claude/rules/destructive-operations.md.
func TestRegistryMigrator_DoesNotClaimForeignFiles(t *testing.T) {
	cases := []struct {
		name, body string
	}{
		{"npm-style registry", `{"registries":{"npm":"https://registry.npmjs.org"},"authToken":"secret"}`},
		{"unrelated object", `{"version":3,"packages":{}}`},
		{"empty object", `{}`},
		{"ynh shape but no name and no entries", `{"description":"nothing here"}`},
		{"an array, not an object", `[{"name":"x"}]`},
		// Has a name, so the emptiness check alone would let this through.
		// Only rejecting unknown top-level fields catches it.
		{"named, but an npm registry", `{"name":"npm","registries":{"npm":"https://registry.npmjs.org"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := regDir(t, tc.body)
			if (RegistryFormatMigrator{}).Applies(dir) {
				t.Errorf("claimed a file it cannot identify as ynh's; Run would delete it")
			}
			// And the predicate must not have touched it.
			if _, err := os.Stat(filepath.Join(dir, "registry.json")); err != nil {
				t.Errorf("the predicate removed the file: %v", err)
			}
		})
	}
}

// The real thing must still be recognised, or the fix has broken migration.
func TestRegistryMigrator_ClaimsAnYnhRegistry(t *testing.T) {
	cases := []struct {
		name, body string
	}{
		{"name and entries", `{"name":"eyelock","entries":[{"name":"planner","repo":"github.com/eyelock/assistants"}]}`},
		{"name only", `{"name":"eyelock","entries":[]}`},
		{"entries only", `{"name":"","entries":[{"name":"planner","repo":"r"}]}`},
		// The shape internal/registry marshals: capitalised keys, a top-level
		// Namespace, and per-entry fields oldEntry does not model. A real
		// registry must not be refused because of them.
		{"as internal/registry writes it", `{"Name":"test-reg","Description":"d","Namespace":"eyelock/assistants","Entries":[{"Name":"my-harness","Description":"d","Keywords":["go"],"Namespace":"","Repo":"github.com/test/repo","Path":"personas/mine","Ref":"","SHA":"","Version":"1.0.0","Vendors":["claude"]}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !(RegistryFormatMigrator{}).Applies(regDir(t, tc.body)) {
				t.Error("failed to recognise an ynh registry; migration would silently stop working")
			}
		})
	}
}

// Already migrated means nothing to do, regardless of what registry.json holds.
func TestRegistryMigrator_SkipsWhenAlreadyMigrated(t *testing.T) {
	dir := regDir(t, `{"name":"eyelock","entries":[{"name":"x","repo":"r"}]}`)
	if err := os.MkdirAll(filepath.Join(dir, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".ynh-plugin", "marketplace.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if (RegistryFormatMigrator{}).Applies(dir) {
		t.Error("applied despite the new format already existing")
	}
}

// The load-time migrator never converts a registry in a source tree; it
// refuses with the fix and leaves registry.json as it was (#406). MigrateChain
// converts it.
func TestRegistryFormatMigrator_SourceTreeOnlyUnderMigrateChain(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	body := `{"name":"eyelock","entries":[{"name":"planner","repo":"github.com/eyelock/assistants"}]}`
	dir := regDir(t, body)

	mustBeUnderTemp(t, filepath.Dir(dir), dir)
	_, err := FormatChain().Run(dir)
	want := dir + " uses the legacy registry.json, which ynh no longer reads; convert it with: ynd migrate " + dir
	if err == nil || err.Error() != want {
		t.Fatalf("FormatChain().Run error = %v\nwant %s", err, want)
	}
	if got := readFile(t, filepath.Join(dir, "registry.json")); got != body {
		t.Errorf("registry.json changed: %q", got)
	}
	if plugin.IsRegistryDir(dir) {
		t.Error("FormatChain wrote a marketplace.json into a source tree")
	}

	applied, err := MigrateChain().Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 || exists(filepath.Join(dir, "registry.json")) || !plugin.IsRegistryDir(dir) {
		t.Errorf("MigrateChain did not convert the registry: applied %v", applied)
	}
}

// A file that is not an ynh registry is neither converted nor refused.
func TestLegacyRegistry_IgnoresForeignFiles(t *testing.T) {
	dir := regDir(t, `{"registries":{"npm":"https://registry.npmjs.org"}}`)
	if err := LegacyRegistry(dir); err != nil {
		t.Errorf("refused a file that is not ynh's: %v", err)
	}
}
