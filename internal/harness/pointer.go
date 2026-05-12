package harness

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/namespace"
	"github.com/eyelock/ynh/internal/plugin"
)

// Pointer is the on-disk shape of ~/.ynh/installed/<id-fsname>.json.
//
// Pointer files register a user-owned source tree into the YNH layer
// without copying it. LoadByID resolves via the pointer first, so edits
// to the source tree are live to ynh run with no sync step.
//
// The pointer carries both registration (ID/Name) and the full
// provenance record by embedding plugin.InstalledJSON. This keeps the
// authored source tree free of ynh-owned metadata: source_type, source,
// resolved SHAs, and forked_from all live in the pointer file rather
// than in <source>/.ynh-plugin/installed.json. Tree-form installs (see
// topology.go) continue to keep their installed.json next to their
// content; pointer-form installs do not.
//
// Legacy pointer files written by v0.3.x and earlier carry only
// SourceType, Source, and InstalledAt. Those fields parse cleanly into
// the embedded record; the schema-3 migration backfills the rest from
// the source tree's installed.json.
type Pointer struct {
	// ID is the canonical, host-prefixed harness id. Empty for legacy
	// pointer files written by pre-schema-2 binaries; the schema-2
	// migration backfills it.
	ID                   string `json:"id,omitempty"`
	Name                 string `json:"name"`
	plugin.InstalledJSON        // anonymous embed: fields serialise flat
}

// PointerPath returns the on-disk path of the schema-1 (name-keyed) pointer
// file for name. Retained for the migration window; schema-2 callers use
// PointerPathByID.
func PointerPath(name string) string {
	return filepath.Join(config.PointersDir(), name+".json")
}

// PointerPathByID returns the on-disk path of the schema-2 (id-keyed)
// pointer file. The filename is the canonical id with "/" → "--".
//
//	"github.com/eyelock/assistants/planner" → "<PointersDir>/github.com--eyelock--assistants--planner.json"
//	"local/planner"                         → "<PointersDir>/local--planner.json"
func PointerPathByID(id string) string {
	return filepath.Join(config.PointersDir(), namespace.IDToFSName(id)+".json")
}

// LoadPointerByID reads a schema-2 (id-keyed) pointer file. Returns
// (nil, nil) when no pointer exists for this id.
func LoadPointerByID(id string) (*Pointer, error) {
	path := PointerPathByID(id)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading pointer %s: %w", path, err)
	}
	var p Pointer
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("invalid pointer %s: %w", path, err)
	}
	return &p, nil
}

// SavePointerByID writes p to its schema-2 (id-keyed) pointer file path.
// Pointers must carry a non-empty ID to use this — for backwards
// compatibility with code that still constructs pointers without setting
// ID, fall back to SavePointer.
func SavePointerByID(p *Pointer) error {
	if p.ID == "" {
		return fmt.Errorf("pointer for %q has no ID set; cannot save under schema 2", p.Name)
	}
	if err := os.MkdirAll(config.PointersDir(), 0o755); err != nil {
		return fmt.Errorf("creating pointers dir: %w", err)
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling pointer: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(PointerPathByID(p.ID), data, 0o644); err != nil {
		return fmt.Errorf("writing pointer: %w", err)
	}
	return nil
}

// RemovePointerByID deletes the schema-2 (id-keyed) pointer file. Returns
// nil if no pointer exists.
func RemovePointerByID(id string) error {
	err := os.Remove(PointerPathByID(id))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing pointer: %w", err)
	}
	return nil
}

// LoadPointer reads and parses the pointer file for name. Returns
// (nil, nil) when no pointer exists.
func LoadPointer(name string) (*Pointer, error) {
	path := PointerPath(name)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading pointer %s: %w", path, err)
	}
	var p Pointer
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("invalid pointer %s: %w", path, err)
	}
	return &p, nil
}

// SavePointer writes p to its pointer-file path. Creates the pointers
// directory if needed.
func SavePointer(p *Pointer) error {
	if err := os.MkdirAll(config.PointersDir(), 0o755); err != nil {
		return fmt.Errorf("creating pointers dir: %w", err)
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling pointer: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(PointerPath(p.Name), data, 0o644); err != nil {
		return fmt.Errorf("writing pointer: %w", err)
	}
	return nil
}

// RemovePointer deletes the pointer file for name. Returns nil if no
// pointer exists — uninstall semantics: missing pointer is not an error
// for the caller that already knows it wants the registration gone.
func RemovePointer(name string) error {
	err := os.Remove(PointerPath(name))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing pointer: %w", err)
	}
	return nil
}

// ListPointers returns all pointer-shaped installs in name order.
func ListPointers() ([]ListEntry, error) {
	dir := config.PointersDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var results []ListEntry
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		ptr, err := LoadPointer(name)
		if err != nil || ptr == nil {
			continue
		}
		// Always include the entry, even when the source path is missing.
		// A broken pointer is still a registration the user owns — surfacing
		// it in `ynh ls` (where LoadDir will produce a per-row error) is
		// strictly better than silently showing a different install of the
		// same name from the namespaced tree.
		results = append(results, ListEntry{
			Name: ptr.Name,
			Dir:  ptr.Source,
		})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Name < results[j].Name })
	return results, nil
}

// ErrPointerSourceMissing is returned when a pointer file references a
// source path that no longer exists on disk. Wrapped with an actionable
// message at the call site.
var ErrPointerSourceMissing = errors.New("pointer source path missing")

// loadFromPointer resolves a pointer file to a fully-loaded Harness.
// Returns (nil, ErrPointerSourceMissing, wrapped with the user-facing
// relocate/uninstall hint) when the source path is gone.
//
// The pointer's embedded plugin.InstalledJSON is the authoritative
// provenance record (schema 3+). For legacy pointers (pre-schema-3,
// carrying only source_type/source/installed_at), the rest of the
// provenance still lives at <source>/.ynh-plugin/installed.json; we
// merge it in so reads work before the schema-3 migration has run.
// After migration, the pointer carries the full record and the source
// tree is free of ynh metadata.
func loadFromPointer(ptr *Pointer) (*Harness, error) {
	if _, err := os.Stat(ptr.Source); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf(
				"harness %q is registered but its source path no longer exists:\n"+
					"  %s\n\n"+
					"If you moved it, restore the directory.\n"+
					"If it's gone for good, run: ynh uninstall %s",
				ptr.Name, ptr.Source, ptr.Name)
		}
		return nil, fmt.Errorf("stat pointer source %s: %w", ptr.Source, err)
	}
	ins := ptr.InstalledJSON
	if len(ins.Resolved) == 0 && ins.ForkedFrom == nil {
		if disk, err := plugin.LoadInstalledJSON(ptr.Source); err == nil && disk != nil {
			ins = *disk
		}
	}
	return loadDirWithProvenance(ptr.Source, &ins)
}
