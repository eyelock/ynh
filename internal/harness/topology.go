package harness

import (
	"path/filepath"

	"github.com/eyelock/ynh/internal/plugin"
)

// Install topologies
//
// An installed harness lives on disk in one of two shapes. The shape is
// chosen at install time from the source kind and is the single source of
// truth for both reads and writes — they always land at the same place.
//
//	Pointer-form    ── for local sources the user owns (local path,
//	                   sources: entry, fork). One file in PointersDir()
//	                   carrying both the registration (name binding) and
//	                   the provenance (ref/sha/path/resolved/forked_from).
//	                   No content is copied; LoadByID resolves straight to
//	                   the user's working tree. Edits made through
//	                   ynh include/delegate, or by hand, are immediately
//	                   visible to ynh run.
//
//	Tree-form       ── for remote sources (git, registry). Content is
//	                   copied to HarnessesDir()/<id-fsname>/ and the
//	                   provenance lives next to it in
//	                   .ynh-plugin/installed.json. The user does not
//	                   maintain this tree; ynh update refreshes it from
//	                   upstream.
//
// The on-disk source_type field discriminates which topology a record
// belongs to:
//
//	source_type=local    → pointer-form  (ynh install /path, ynh fork)
//	source_type=source   → pointer-form  (ynh install <name> via sources:)
//	source_type=git      → tree-form     (ynh install <git-url>)
//	source_type=registry → tree-form     (ynh install <name> via registry)
//
// Forks share the local source_type and are distinguished by a non-nil
// ForkedFrom on the record. IsLocalSource is the single classifier:
// true for pointer-form, false for tree-form. Every code path that
// needs to choose between "consult the user's source tree" and "consult
// the install copy" routes through it.

// localLoadDir returns the on-disk directory holding a pointer-form
// install's content: ins.Source joined with ins.Path. The join handles
// installs created with --path (where the harness lives in a subdir of
// the user's repo); for installs at the source root, Path is empty and
// the join is a no-op.
func localLoadDir(ins *plugin.InstalledJSON) string {
	if ins == nil {
		return ""
	}
	if ins.Path == "" {
		return ins.Source
	}
	return filepath.Join(ins.Source, ins.Path)
}

// LoadInstalledRecord returns the install provenance for h, regardless
// of topology:
//   - pointer-form (local/source): from the pointer file at
//     PointersDir/<id-fsname>.json
//   - tree-form (git/registry): from <h.Dir>/.ynh-plugin/installed.json
//
// canonID is the canonical id of h ("local/<name>" or
// "<host>/<org>/<repo>/<name>"); callers usually already have it
// (e.g. it's the ref the user passed), so we don't recompute from h.
// Returns (nil, nil) when no record is found in either location.
func LoadInstalledRecord(canonID string, h *Harness) (*plugin.InstalledJSON, error) {
	if ptr, err := LoadPointerByID(canonID); err != nil {
		return nil, err
	} else if ptr != nil {
		ins := ptr.InstalledJSON
		return &ins, nil
	}
	disk, err := plugin.LoadInstalledJSON(h.Dir)
	if err != nil {
		return nil, nil
	}
	return disk, nil
}

// SaveInstalledRecord writes the install record to the correct location
// for h's topology — pointer file for pointer-form installs, sibling
// installed.json for tree-form. See LoadInstalledRecord for the
// topology detection rule.
func SaveInstalledRecord(canonID string, h *Harness, ins *plugin.InstalledJSON) error {
	if ptr, _ := LoadPointerByID(canonID); ptr != nil {
		ptr.InstalledJSON = *ins
		return SavePointerByID(ptr)
	}
	return plugin.SaveInstalledJSON(h.Dir, ins)
}

// IsLocalSource reports whether the installed.json record describes a
// pointer-form install — one whose content lives in a user-owned source
// tree rather than a copy under HarnessesDir(). Returns false for nil
// records and for records with an empty Source path; callers can treat
// those as tree-form safely.
func IsLocalSource(ins *plugin.InstalledJSON) bool {
	if ins == nil {
		return false
	}
	if ins.Source == "" {
		return false
	}
	switch ins.SourceType {
	case "local", "source":
		return true
	}
	return false
}
