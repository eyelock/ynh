package migration

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/eyelock/ynh/internal/namespace"
	"github.com/eyelock/ynh/internal/plugin"
)

// SchemaVersion 2 introduces canonical-id-keyed on-disk layout:
//   - Pointer files at ~/.ynh/installed/<host--org--repo--name>.json
//     instead of ~/.ynh/installed/<name>.json
//   - Tree-shaped installs at ~/.ynh/harnesses/<host--org--repo--name>/
//     instead of either ~/.ynh/harnesses/<name>/ (flat) or
//     ~/.ynh/harnesses/<ns--repo>/<name>/ (two-level namespaced)
//   - installed.json carries an explicit "id" field and a host-prefixed
//     "namespace" field
//
// The migration routine MigrateToSchema2 converts a schema-1 home to
// schema 2 in place, recording every action in a manifest. It is the
// SINGLE place legacy schema-1 layout is read; the rest of the codebase
// speaks only schema 2.

// SchemaVersionPath is the file recording the current on-disk schema
// version of ~/.ynh. Absent or content "1" means schema 1 (pre-migration).
// Content "2" means migration has completed.
const SchemaVersionPath = ".schema-version"

// MigrationManifestPath is where the persisted manifest of the last
// migration run lives. Idempotent: the file is overwritten on every
// re-run, so consumers (TermQ) can read the same mapping any time.
const MigrationManifestPath = ".migration-manifest.json"

// QuarantineDir is where unmigratable entries are moved when the user
// passes --skip-broken. Subdirs:
//   - pointers/  — pointer files that couldn't be migrated
//   - harnesses/ — install dirs that couldn't be migrated
//   - orphan-cache/ — cache dirs with no matching install record
const QuarantineDir = ".quarantine"

// CurrentSchemaVersion is the schema version this binary writes.
const CurrentSchemaVersion = 2

// ReadSchemaVersion returns the on-disk schema version of home. Absent or
// invalid content is reported as 1 (pre-schema-version was introduced —
// nothing on disk corresponds to schema 0).
func ReadSchemaVersion(home string) int {
	data, err := os.ReadFile(filepath.Join(home, SchemaVersionPath))
	if err != nil {
		return 1
	}
	s := strings.TrimSpace(string(data))
	switch s {
	case "2":
		return 2
	default:
		return 1
	}
}

// WriteSchemaVersion stamps the schema version file. Called as the last
// step of MigrateToSchema2 so that partial failure leaves the home at
// schema 1 and the next run resumes. Creates the home directory if it
// doesn't already exist — defensive against being called on a partial
// home where the parent dir was removed between read and write.
func WriteSchemaVersion(home string, version int) error {
	if err := os.MkdirAll(home, 0o755); err != nil {
		return fmt.Errorf("creating home dir for schema version: %w", err)
	}
	path := filepath.Join(home, SchemaVersionPath)
	return os.WriteFile(path, []byte(fmt.Sprintf("%d\n", version)), 0o644)
}

// MigrateOpts controls migration behaviour.
type MigrateOpts struct {
	// SkipBroken: when true, entries that fail to migrate are moved to
	// the quarantine directory and migration continues. When false (the
	// default), the first failure aborts and the home is left at schema 1.
	SkipBroken bool
	// DryRun: when true, no on-disk changes are made; the returned
	// manifest reflects what *would* happen.
	DryRun bool
}

// ManifestEntry records a single id-rewrite or move performed by the
// migration. TermQ reads this to rewrite its persisted ids in one pass.
type ManifestEntry struct {
	// OldID is the legacy identifier — for pointer files it's the bare
	// name; for tree installs the namespaced "<ns>/<name>" or bare name.
	OldID string `json:"old_id"`
	// NewID is the canonical, host-prefixed id ("host/org/repo/name" or
	// "local/name").
	NewID string `json:"new_id"`
	// Kind is "pointer", "install_tree_flat", or "install_tree_ns".
	Kind string `json:"kind"`
	// OldPath and NewPath are the absolute on-disk paths before/after.
	OldPath string `json:"old_path"`
	NewPath string `json:"new_path"`
}

// QuarantineEntry records a path that couldn't be migrated.
type QuarantineEntry struct {
	OriginalPath string `json:"original_path"`
	Quarantined  string `json:"quarantined"`
	Reason       string `json:"reason"`
}

// Manifest is the persisted record of a migration run.
type Manifest struct {
	SchemaVersion int               `json:"schema_version"`
	MigratedAt    string            `json:"migrated_at"`
	Entries       []ManifestEntry   `json:"entries"`
	Quarantined   []QuarantineEntry `json:"quarantined"`
}

// ErrMigrationAborted is returned when migration hit an unmigratable
// entry without --skip-broken set. The home directory is unchanged
// except for entries already migrated before the failure.
var ErrMigrationAborted = errors.New("migration aborted on broken entry")

// MigrateToSchema2 migrates a YNH home from schema 1 to schema 2.
// Idempotent: re-running on a partially-migrated home is safe; entries
// already in their schema-2 location are skipped.
//
// Order of operations:
//  1. Walk ~/.ynh/installed/, rewrite pointer files to id-keyed layout.
//  2. Walk ~/.ynh/harnesses/, rewrite tree-shaped installs to id-keyed
//     layout (collapsing the flat / namespaced split into one level).
//  3. Stamp ~/.ynh/.migration-manifest.json with the actions taken.
//  4. Stamp ~/.ynh/.schema-version with "2".
//
// Step 4 is the last write — partial failure before it leaves the home
// at schema 1, so the next invocation re-runs from where it left off.
func MigrateToSchema2(home string, opts MigrateOpts) (*Manifest, error) {
	m := &Manifest{
		SchemaVersion: CurrentSchemaVersion,
		MigratedAt:    time.Now().UTC().Format(time.RFC3339),
	}

	if err := migratePointers(home, opts, m); err != nil {
		return m, err
	}
	if err := migrateInstallTrees(home, opts, m); err != nil {
		return m, err
	}
	if err := migrateLaunchers(home, opts, m); err != nil {
		return m, err
	}

	if !opts.DryRun {
		// Only persist a manifest when migration actually moved something.
		// A no-op migration on a fresh home has nothing to record, and
		// leaving an empty .migration-manifest.json on disk just clutters
		// the home and confuses users who'd reasonably wonder what was
		// migrated.
		if len(m.Entries) > 0 || len(m.Quarantined) > 0 {
			if err := writeManifest(home, m); err != nil {
				return m, fmt.Errorf("writing migration manifest: %w", err)
			}
		}
		if err := WriteSchemaVersion(home, CurrentSchemaVersion); err != nil {
			return m, fmt.Errorf("stamping schema version: %w", err)
		}
	}
	return m, nil
}

// migratePointers walks ~/.ynh/installed/ and rewrites each pointer file
// to its schema-2 id-keyed path, stamping the canonical id into the
// content. Files already at their schema-2 path with id set are skipped.
func migratePointers(home string, opts MigrateOpts, m *Manifest) error {
	pointersDir := filepath.Join(home, "installed")
	entries, err := os.ReadDir(pointersDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading pointers dir: %w", err)
	}

	// Sort for deterministic manifest ordering — useful for tests and
	// for human review of dry-run output.
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		oldPath := filepath.Join(pointersDir, e.Name())
		if err := migrateOnePointer(oldPath, pointersDir, opts, m); err != nil {
			return err
		}
	}
	return nil
}

// pointerJSON is the schema-2 pointer shape, kept local to migration to
// avoid cycling internal/harness back into internal/migration.
type pointerJSON struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name"`
	SourceType  string `json:"source_type"`
	Source      string `json:"source"`
	InstalledAt string `json:"installed_at"`
}

func migrateOnePointer(oldPath, pointersDir string, opts MigrateOpts, m *Manifest) error {
	data, err := os.ReadFile(oldPath)
	if err != nil {
		return quarantineOrAbort(oldPath, fmt.Errorf("read: %w", err), opts, m)
	}
	var p pointerJSON
	if err := json.Unmarshal(data, &p); err != nil {
		return quarantineOrAbort(oldPath, fmt.Errorf("parse: %w", err), opts, m)
	}
	if p.Name == "" {
		return quarantineOrAbort(oldPath, errors.New("pointer has no name"), opts, m)
	}

	// Derive canonical id from source + name. CanonicalID handles every
	// shape we know about (https/ssh/local/empty); a result of "local/<name>"
	// for a non-recognised source URL is the documented schema-2 behaviour.
	id := namespace.CanonicalID(p.Source, p.Name)
	if id == "" {
		return quarantineOrAbort(oldPath, errors.New("could not derive canonical id"), opts, m)
	}

	newPath := filepath.Join(pointersDir, namespace.IDToFSName(id)+".json")

	if oldPath == newPath && p.ID == id {
		// Already at the right path with the right id stamped — schema 2 form.
		return nil
	}

	p.ID = id
	if opts.DryRun {
		m.Entries = append(m.Entries, ManifestEntry{
			OldID:   p.Name,
			NewID:   id,
			Kind:    "pointer",
			OldPath: oldPath,
			NewPath: newPath,
		})
		return nil
	}

	out, err := json.MarshalIndent(&p, "", "  ")
	if err != nil {
		return quarantineOrAbort(oldPath, fmt.Errorf("marshal: %w", err), opts, m)
	}
	out = append(out, '\n')
	if err := os.WriteFile(newPath, out, 0o644); err != nil {
		return quarantineOrAbort(oldPath, fmt.Errorf("write %s: %w", newPath, err), opts, m)
	}
	if oldPath != newPath {
		if err := os.Remove(oldPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing old pointer %s: %w", oldPath, err)
		}
	}
	m.Entries = append(m.Entries, ManifestEntry{
		OldID:   p.Name,
		NewID:   id,
		Kind:    "pointer",
		OldPath: oldPath,
		NewPath: newPath,
	})
	return nil
}

// legacyLauncherTemplate is the schema-1 launcher script that pre-canonical-id
// binaries wrote to ~/.ynh/bin/<name>. Migration recognises this exact shape
// (with the bare name interpolated) and rewrites it to use the canonical id;
// any other content is treated as a hand-edit and either quarantined
// (--skip-broken) or aborts the migration.
const legacyLauncherTemplate = `#!/bin/bash
# Generated by ynh - do not edit
exec ynh run %q "$@"
`

// migrateLaunchers walks ~/.ynh/bin/ and rewrites each generated launcher
// script so it invokes `ynh run <canonical-id>` instead of the bare name.
// Schema 2's resolver rejects bare names, so unmigrated launchers are
// non-functional shortcuts. Hand-edited launchers (content not matching the
// legacy template) are quarantined with --skip-broken or abort otherwise —
// migration must not silently clobber user customisation.
func migrateLaunchers(home string, opts MigrateOpts, m *Manifest) error {
	binDir := filepath.Join(home, "bin")
	entries, err := os.ReadDir(binDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading bin dir: %w", err)
	}

	// Build a lookup from bare name → new canonical id from the entries
	// the migration just produced. Pointer entries record OldID == bare name;
	// install-tree entries do too (the legacy fsname).
	idByName := map[string]string{}
	for _, e := range m.Entries {
		idByName[e.OldID] = e.NewID
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(binDir, e.Name())
		if err := migrateOneLauncher(path, home, idByName, opts, m); err != nil {
			return err
		}
	}
	return nil
}

func migrateOneLauncher(path, home string, idByName map[string]string, opts MigrateOpts, m *Manifest) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return quarantineOrAbort(path, fmt.Errorf("read launcher: %w", err), opts, m)
	}

	bareName := filepath.Base(path)
	canonicalID, knownName := idByName[bareName]
	if !knownName {
		// No manifest entry → infer canonical id from any existing
		// schema-2 install. If neither exists, treat as orphan and skip
		// — `ynh prune` is the right tool for stale launchers without
		// a backing harness.
		if !installExistsForLauncher(home, bareName) {
			return nil
		}
		canonicalID = "local/" + bareName
	}

	expectedLegacy := fmt.Sprintf(legacyLauncherTemplate, bareName)
	expectedNew := fmt.Sprintf(legacyLauncherTemplate, canonicalID)

	switch string(content) {
	case expectedNew:
		// Already migrated — no action.
		return nil
	case expectedLegacy:
		// Legacy template — rewrite with canonical id.
		if opts.DryRun {
			m.Entries = append(m.Entries, ManifestEntry{
				OldID:   bareName,
				NewID:   canonicalID,
				Kind:    "launcher",
				OldPath: path,
				NewPath: path,
			})
			return nil
		}
		if err := os.WriteFile(path, []byte(expectedNew), 0o755); err != nil {
			return quarantineOrAbort(path, fmt.Errorf("rewrite launcher: %w", err), opts, m)
		}
		m.Entries = append(m.Entries, ManifestEntry{
			OldID:   bareName,
			NewID:   canonicalID,
			Kind:    "launcher",
			OldPath: path,
			NewPath: path,
		})
		return nil
	default:
		// Hand-edited or unknown shape — refuse to clobber.
		return quarantineOrAbort(path,
			fmt.Errorf("launcher content does not match the generated template (hand-edited?); refusing to overwrite"),
			opts, m)
	}
}

// installExistsForLauncher reports whether bareName has a backing install
// on disk under either the schema-2 pointer-file layout or the schema-2
// install-tree layout. Used to decide whether an unknown launcher is a
// migration candidate (true → strict regenerate/quarantine) or an orphan
// (false → leave for `ynh prune`).
func installExistsForLauncher(home, bareName string) bool {
	// Schema-2 pointer file path: installed/local--<name>.json
	pointer := filepath.Join(home, "installed", "local--"+bareName+".json")
	if _, err := os.Stat(pointer); err == nil {
		return true
	}
	// Schema-2 install tree: harnesses/local--<name>/
	tree := filepath.Join(home, "harnesses", "local--"+bareName)
	if info, err := os.Stat(tree); err == nil && info.IsDir() {
		return true
	}
	return false
}

// migrateInstallTrees walks ~/.ynh/harnesses/ and rewrites each tree-shaped
// install (flat or namespaced) to its schema-2 id-keyed path. installed.json
// inside each tree is updated to carry the canonical id and host-prefixed
// namespace.
func migrateInstallTrees(home string, opts MigrateOpts, m *Manifest) error {
	harnessesDir := filepath.Join(home, "harnesses")
	entries, err := os.ReadDir(harnessesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading harnesses dir: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		entryPath := filepath.Join(harnessesDir, e.Name())
		// Heuristic: a directory whose name contains "--" is the legacy
		// namespaced parent (e.g. "eyelock--assistants/"); its children
		// are the actual harness dirs. A directory without "--" is either
		// a flat install or already a schema-2 id-keyed install.
		if isLegacyNamespacedParent(entryPath) {
			if err := migrateNamespacedParent(entryPath, harnessesDir, opts, m); err != nil {
				return err
			}
			continue
		}
		// Flat install OR already-migrated schema-2 install.
		if err := migrateFlatOrSchema2Install(entryPath, harnessesDir, opts, m); err != nil {
			return err
		}
	}
	return nil
}

// isLegacyNamespacedParent reports whether a dir under ~/.ynh/harnesses/
// looks like the schema-1 two-level layout — i.e. its name contains "--"
// AND it contains harness dirs as immediate children (not a plugin.json
// at the root). The "--" check alone is insufficient because schema 2
// uses "--" in id-fsnames for both directory levels merged into one;
// what distinguishes them is whether a plugin manifest sits at the root.
func isLegacyNamespacedParent(dir string) bool {
	if !strings.Contains(filepath.Base(dir), "--") {
		return false
	}
	if plugin.IsPluginDir(dir) || plugin.IsLegacyPluginDir(dir) {
		return false
	}
	// Confirm it has at least one child dir with a plugin manifest.
	children, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, c := range children {
		if !c.IsDir() {
			continue
		}
		childPath := filepath.Join(dir, c.Name())
		if plugin.IsPluginDir(childPath) || plugin.IsLegacyPluginDir(childPath) {
			return true
		}
	}
	return false
}

func migrateNamespacedParent(parent, harnessesDir string, opts MigrateOpts, m *Manifest) error {
	children, err := os.ReadDir(parent)
	if err != nil {
		return quarantineOrAbort(parent, fmt.Errorf("read namespaced parent: %w", err), opts, m)
	}
	for _, c := range children {
		if !c.IsDir() {
			continue
		}
		childPath := filepath.Join(parent, c.Name())
		if !plugin.IsPluginDir(childPath) && !plugin.IsLegacyPluginDir(childPath) {
			continue
		}
		if err := migrateOneInstall(childPath, harnessesDir, c.Name(), "install_tree_ns", opts, m); err != nil {
			return err
		}
	}
	// After all children migrated, remove the now-empty namespaced parent.
	if !opts.DryRun {
		remaining, _ := os.ReadDir(parent)
		if len(remaining) == 0 {
			_ = os.Remove(parent)
		}
	}
	return nil
}

func migrateFlatOrSchema2Install(dir, harnessesDir string, opts MigrateOpts, m *Manifest) error {
	if !plugin.IsPluginDir(dir) && !plugin.IsLegacyPluginDir(dir) {
		// Empty / unrelated dir — leave it alone.
		return nil
	}
	return migrateOneInstall(dir, harnessesDir, filepath.Base(dir), "install_tree_flat", opts, m)
}

func migrateOneInstall(installDir, harnessesDir, oldID, kind string, opts MigrateOpts, m *Manifest) error {
	ins, err := plugin.LoadInstalledJSON(installDir)
	if err != nil {
		return quarantineOrAbort(installDir, fmt.Errorf("load installed.json: %w", err), opts, m)
	}
	if ins == nil {
		return quarantineOrAbort(installDir, errors.New("missing installed.json"), opts, m)
	}

	name := loadHarnessName(installDir)
	if name == "" {
		return quarantineOrAbort(installDir, errors.New("plugin manifest has no name"), opts, m)
	}

	id := namespace.CanonicalID(ins.Source, name)
	if id == "" {
		return quarantineOrAbort(installDir, errors.New("could not derive canonical id"), opts, m)
	}

	newPath := filepath.Join(harnessesDir, namespace.IDToFSName(id))

	// Stamp id and host-prefixed namespace into installed.json. Schema 1
	// stored namespace as "<org>/<repo>" (host-stripped); schema 2 stores
	// it as the full namespace prefix of the id.
	host, _ := splitHostFromID(id)
	newNamespace := strings.TrimSuffix(strings.TrimSuffix(id, "/"+name), "")
	if host == "local" {
		// Local installs keep namespace = "local" (matches id minus name).
		newNamespace = "local"
	}

	alreadyMigrated := installDir == newPath && ins.Namespace == newNamespace
	if alreadyMigrated {
		return nil
	}

	ins.Namespace = newNamespace
	if opts.DryRun {
		m.Entries = append(m.Entries, ManifestEntry{
			OldID:   oldID,
			NewID:   id,
			Kind:    kind,
			OldPath: installDir,
			NewPath: newPath,
		})
		return nil
	}

	if err := plugin.SaveInstalledJSON(installDir, ins); err != nil {
		return quarantineOrAbort(installDir, fmt.Errorf("rewrite installed.json: %w", err), opts, m)
	}

	if installDir != newPath {
		if err := os.Rename(installDir, newPath); err != nil {
			return quarantineOrAbort(installDir, fmt.Errorf("rename to %s: %w", newPath, err), opts, m)
		}
	}

	m.Entries = append(m.Entries, ManifestEntry{
		OldID:   oldID,
		NewID:   id,
		Kind:    kind,
		OldPath: installDir,
		NewPath: newPath,
	})
	return nil
}

// loadHarnessName reads the harness name from plugin.json (or legacy
// .harness.json after the format migrator has already run inside the
// install dir's load path).
func loadHarnessName(dir string) string {
	hj, err := plugin.LoadHarnessJSON(dir)
	if err != nil || hj == nil {
		return ""
	}
	return hj.Name
}

// splitHostFromID returns the first "/"-separated segment of id (the host),
// and the rest. For "local/<name>" the host is "local".
func splitHostFromID(id string) (host, rest string) {
	idx := strings.Index(id, "/")
	if idx < 0 {
		return "", id
	}
	return id[:idx], id[idx+1:]
}

// quarantineOrAbort moves a path to the quarantine dir when --skip-broken
// is set, recording the action in the manifest. Without --skip-broken,
// returns ErrMigrationAborted wrapped with the offending path.
func quarantineOrAbort(path string, reason error, opts MigrateOpts, m *Manifest) error {
	if !opts.SkipBroken {
		return fmt.Errorf("%w: %s: %v\nhint: re-run with --skip-broken to quarantine and continue", ErrMigrationAborted, path, reason)
	}
	if opts.DryRun {
		m.Quarantined = append(m.Quarantined, QuarantineEntry{
			OriginalPath: path,
			Quarantined:  "(dry-run)",
			Reason:       reason.Error(),
		})
		return nil
	}
	home := homeFromPath(path)
	if home == "" {
		// Defensive — should never happen since callers always pass paths
		// rooted under home. Fall through to non-fatal record.
		m.Quarantined = append(m.Quarantined, QuarantineEntry{
			OriginalPath: path,
			Reason:       reason.Error() + " (could not derive home for quarantine)",
		})
		return nil
	}
	qDir := filepath.Join(home, QuarantineDir, "broken")
	if err := os.MkdirAll(qDir, 0o755); err != nil {
		return fmt.Errorf("creating quarantine dir: %w", err)
	}
	dst := filepath.Join(qDir, filepath.Base(path))
	// Avoid clobbering on collision.
	if _, err := os.Stat(dst); err == nil {
		dst = fmt.Sprintf("%s.%d", dst, time.Now().UnixNano())
	}
	if err := os.Rename(path, dst); err != nil {
		return fmt.Errorf("quarantining %s: %w", path, err)
	}
	m.Quarantined = append(m.Quarantined, QuarantineEntry{
		OriginalPath: path,
		Quarantined:  dst,
		Reason:       reason.Error(),
	})
	return nil
}

// homeFromPath returns the YNH home dir given an absolute path under it.
// Walks up looking for the .schema-version file or one of the standard
// subdirs; returns "" if not found.
func homeFromPath(p string) string {
	dir := filepath.Dir(p)
	for i := 0; i < 6 && dir != "/" && dir != "."; i++ {
		// Indicators that this is a YNH home root.
		for _, marker := range []string{"installed", "harnesses", "cache"} {
			if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
				return dir
			}
		}
		dir = filepath.Dir(dir)
	}
	return ""
}

func writeManifest(home string, m *Manifest) error {
	if err := os.MkdirAll(home, 0o755); err != nil {
		return fmt.Errorf("creating home dir for manifest: %w", err)
	}
	path := filepath.Join(home, MigrationManifestPath)
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

// ReadManifest returns the persisted manifest from a previous migration
// run. Returns (nil, nil) when no manifest exists.
func ReadManifest(home string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(home, MigrationManifestPath))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}
	return &m, nil
}
