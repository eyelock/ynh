package migration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// MigrateToSchema3 migrates a YNH home from schema 2 to schema 3.
//
// Schema 3 collapses local-source installs into pointer-form: content
// stays in the user's source tree and the full provenance record lives
// on the pointer file at PointersDir/<id-fsname>.json. The user's
// source tree no longer carries .ynh-plugin/installed.json — that is
// ynh-owned metadata and belongs in the ynh home.
//
// Two passes:
//
//  1. Tree-form copies under HarnessesDir whose installed.json records
//     source_type ∈ {local, source} are converted: the installed.json
//     is read, a pointer carrying the full record is written, and the
//     copy dir is removed. The source tree at ins.Source is untouched.
//
//  2. Pointer files under PointersDir whose embedded record lacks
//     forked_from / resolved (legacy schema-1/2 pointers) have those
//     fields absorbed from <ins.Source>/.ynh-plugin/installed.json,
//     after which the source-tree installed.json is deleted.
//
// Idempotent: re-running on a partially-migrated home is safe. Entries
// whose source path no longer exists are quarantined when opts.SkipBroken
// is set; otherwise migration aborts on the first such entry.
func MigrateToSchema3(home string, opts MigrateOpts) (*Manifest, error) {
	m := &Manifest{
		SchemaVersion: 3,
		MigratedAt:    time.Now().UTC().Format(time.RFC3339),
	}

	if err := collapseLocalInstalls(home, opts, m); err != nil {
		return m, err
	}
	if err := absorbPointerProvenance(home, opts, m); err != nil {
		return m, err
	}

	if !opts.DryRun {
		if len(m.Entries) > 0 || len(m.Quarantined) > 0 {
			if err := writeManifest(home, m); err != nil {
				return m, fmt.Errorf("writing migration manifest: %w", err)
			}
		}
		if err := WriteSchemaVersion(home, 3); err != nil {
			return m, fmt.Errorf("stamping schema version: %w", err)
		}
	}
	return m, nil
}

// installedJSONShape mirrors plugin.InstalledJSON for the migrator. We
// re-declare it here to keep internal/migration free of harness/plugin
// imports (canonicalid.go follows the same pattern).
type installedJSONShape struct {
	SourceType   string                `json:"source_type"`
	Source       string                `json:"source"`
	Ref          string                `json:"ref,omitempty"`
	SHA          string                `json:"sha,omitempty"`
	Path         string                `json:"path,omitempty"`
	Namespace    string                `json:"namespace,omitempty"`
	RegistryName string                `json:"registry_name,omitempty"`
	InstalledAt  string                `json:"installed_at"`
	ForkedFrom   *forkedFromShape      `json:"forked_from,omitempty"`
	Resolved     []resolvedSourceShape `json:"resolved,omitempty"`
}

type forkedFromShape struct {
	SourceType   string `json:"source_type"`
	Source       string `json:"source"`
	Ref          string `json:"ref,omitempty"`
	SHA          string `json:"sha,omitempty"`
	Path         string `json:"path,omitempty"`
	RegistryName string `json:"registry_name,omitempty"`
	Version      string `json:"version,omitempty"`
}

type resolvedSourceShape struct {
	Git  string `json:"git"`
	Ref  string `json:"ref,omitempty"`
	Path string `json:"path,omitempty"`
	SHA  string `json:"sha"`
}

// schema3Pointer is the schema-3 pointer shape: id + name plus the full
// installed.json record inlined at the top level (the encoding produced
// by harness.Pointer when InstalledJSON is anonymously embedded).
type schema3Pointer struct {
	ID string `json:"id,omitempty"`
	installedJSONShape
	Name string `json:"name"`
}

// MarshalJSON ensures Name and ID appear before the embedded record's
// fields, matching the existing on-disk layout written by SavePointerByID.
func (p schema3Pointer) MarshalJSON() ([]byte, error) {
	// Build an ordered map by serialising piecewise.
	type alias schema3Pointer
	return json.Marshal(alias(p))
}

func collapseLocalInstalls(home string, opts MigrateOpts, m *Manifest) error {
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
		copyDir := filepath.Join(harnessesDir, e.Name())
		// Skip schema-1 namespace directories that contain children but no
		// manifest at the top — MigrateToSchema2 should have flattened
		// these, but if a home was hand-edited or partially migrated, leave
		// them alone for the schema-1→2 migration to handle.
		if _, err := os.Stat(filepath.Join(copyDir, ".ynh-plugin", "plugin.json")); err != nil {
			continue
		}
		insPath := filepath.Join(copyDir, ".ynh-plugin", "installed.json")
		insData, err := os.ReadFile(insPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("reading %s: %w", insPath, err)
		}
		var ins installedJSONShape
		if err := json.Unmarshal(insData, &ins); err != nil {
			return quarantineOrAbort(copyDir, fmt.Errorf("invalid installed.json: %w", err), opts, m)
		}
		if !isLocalSourceShape(&ins) {
			continue
		}

		// Source path must exist with a manifest — without it we can't
		// promote to pointer-form since the load target would be broken.
		// Quarantine or abort per opts.
		loadDir := ins.Source
		if ins.Path != "" {
			loadDir = filepath.Join(ins.Source, ins.Path)
		}
		manifestPath := filepath.Join(loadDir, ".ynh-plugin", "plugin.json")
		if _, err := os.Stat(manifestPath); err != nil {
			return quarantineOrAbort(copyDir,
				fmt.Errorf("source path missing or has no manifest: %s", loadDir),
				opts, m)
		}

		id := fsNameToID(e.Name())
		newPath := filepath.Join(home, "installed", e.Name()+".json")

		if opts.DryRun {
			m.Entries = append(m.Entries, ManifestEntry{
				OldID:   id,
				NewID:   id,
				Kind:    "install_to_pointer",
				OldPath: copyDir,
				NewPath: newPath,
			})
			continue
		}

		ptr := schema3Pointer{
			ID:                 id,
			Name:               leafName(id),
			installedJSONShape: ins,
		}
		if err := writePointer(home, e.Name(), ptr); err != nil {
			return fmt.Errorf("writing pointer for %s: %w", id, err)
		}
		if err := os.RemoveAll(copyDir); err != nil {
			return fmt.Errorf("removing copy dir %s: %w", copyDir, err)
		}
		m.Entries = append(m.Entries, ManifestEntry{
			OldID:   id,
			NewID:   id,
			Kind:    "install_to_pointer",
			OldPath: copyDir,
			NewPath: newPath,
		})
	}
	return nil
}

func absorbPointerProvenance(home string, opts MigrateOpts, m *Manifest) error {
	pointersDir := filepath.Join(home, "installed")
	entries, err := os.ReadDir(pointersDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading pointers dir: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		ptrPath := filepath.Join(pointersDir, e.Name())
		data, err := os.ReadFile(ptrPath)
		if err != nil {
			return fmt.Errorf("reading %s: %w", ptrPath, err)
		}
		var ptr schema3Pointer
		if err := json.Unmarshal(data, &ptr); err != nil {
			return fmt.Errorf("invalid pointer %s: %w", ptrPath, err)
		}
		// Already-absorbed pointers carry resolved or forked_from. Skip.
		if len(ptr.Resolved) > 0 || ptr.ForkedFrom != nil {
			continue
		}
		if ptr.Source == "" {
			continue
		}
		loadDir := ptr.Source
		if ptr.Path != "" {
			loadDir = filepath.Join(ptr.Source, ptr.Path)
		}
		diskInsPath := filepath.Join(loadDir, ".ynh-plugin", "installed.json")
		diskData, err := os.ReadFile(diskInsPath)
		if err != nil {
			// Nothing to absorb. The pointer is already canonical (legacy
			// fork with no installed.json side-file) — no work to do.
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("reading %s: %w", diskInsPath, err)
		}
		var diskIns installedJSONShape
		if err := json.Unmarshal(diskData, &diskIns); err != nil {
			return fmt.Errorf("invalid installed.json at %s: %w", diskInsPath, err)
		}

		if opts.DryRun {
			m.Entries = append(m.Entries, ManifestEntry{
				OldID:   ptr.ID,
				NewID:   ptr.ID,
				Kind:    "fork_provenance_absorb",
				OldPath: diskInsPath,
				NewPath: ptrPath,
			})
			continue
		}

		// Merge: source-tree fields fill anything missing on the pointer.
		// The pointer's source path remains authoritative — we never
		// overwrite the registration's where-to-look with disk data.
		ptr.Ref = stringOr(ptr.Ref, diskIns.Ref)
		ptr.SHA = stringOr(ptr.SHA, diskIns.SHA)
		ptr.Path = stringOr(ptr.Path, diskIns.Path)
		ptr.Namespace = stringOr(ptr.Namespace, diskIns.Namespace)
		ptr.RegistryName = stringOr(ptr.RegistryName, diskIns.RegistryName)
		ptr.ForkedFrom = diskIns.ForkedFrom
		ptr.Resolved = diskIns.Resolved

		fsName := strings.TrimSuffix(e.Name(), ".json")
		if err := writePointer(home, fsName, ptr); err != nil {
			return fmt.Errorf("rewriting pointer %s: %w", ptrPath, err)
		}
		if err := os.Remove(diskInsPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing source-tree installed.json %s: %w", diskInsPath, err)
		}
		m.Entries = append(m.Entries, ManifestEntry{
			OldID:   ptr.ID,
			NewID:   ptr.ID,
			Kind:    "fork_provenance_absorb",
			OldPath: diskInsPath,
			NewPath: ptrPath,
		})
	}
	return nil
}

func isLocalSourceShape(ins *installedJSONShape) bool {
	if ins == nil || ins.Source == "" {
		return false
	}
	switch ins.SourceType {
	case "local", "source":
		return true
	}
	return false
}

func writePointer(home, fsName string, ptr schema3Pointer) error {
	pointersDir := filepath.Join(home, "installed")
	if err := os.MkdirAll(pointersDir, 0o755); err != nil {
		return fmt.Errorf("creating pointers dir: %w", err)
	}
	data, err := json.MarshalIndent(ptr, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling pointer: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(pointersDir, fsName+".json"), data, 0o644)
}

// fsNameToID reverses the "/" → "--" id-fsname encoding (mirrors
// namespace.FSNameToID, redeclared locally to avoid an import cycle).
func fsNameToID(fsName string) string {
	return strings.ReplaceAll(fsName, "--", "/")
}

// leafName returns the trailing segment of a canonical id ("local/foo" → "foo").
func leafName(id string) string {
	if i := strings.LastIndex(id, "/"); i >= 0 {
		return id[i+1:]
	}
	return id
}

func stringOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
