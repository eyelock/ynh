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
)

// Pointer is the on-disk shape of ~/.ynh/installed/<name>.json.
//
// Pointer files register a user-owned source tree (a fork) into the YNH layer
// without copying it. Load(name) prefers a pointer over the tree-shaped
// install at ~/.ynh/harnesses/<name>/. Edits to the source tree are live to
// ynh run with no sync step.
//
// Provenance (what the harness *is*) lives in the source tree at
// .ynh-plugin/installed.json and is the authoritative source for
// p.InstalledFrom. The pointer file's role is registration — name binding
// and where to look — not provenance.
type Pointer struct {
	Name        string `json:"name"`
	SourceType  string `json:"source_type"`
	Source      string `json:"source"`
	InstalledAt string `json:"installed_at"`
}

// PointerPath returns the on-disk path of the pointer file for name.
// Local forks are flat — no namespaced pointer paths in v1.
func PointerPath(name string) string {
	return filepath.Join(config.PointersDir(), name+".json")
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

// loadFromPointer resolves a pointer file to a fully-loaded Harness by
// running LoadDir(ptr.Source). Returns (nil, ErrPointerSourceMissing,
// wrapped with the user-facing relocate/uninstall hint) when the source
// path is gone.
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
	return LoadDir(ptr.Source)
}
