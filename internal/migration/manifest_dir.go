package migration

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/plugin"
)

// ManifestDirMigrator renames a harness's .ynh-plugin/ directory to
// .agents/harness/ (#404).
//
// Where it runs is decided by SourceTrees, because the two kinds of tree have
// different owners:
//
//   - The zero value, in FormatChain and DefaultChain, touches only trees ynh
//     owns: installs under config.HarnessesDir(), which ynh copied there and
//     rewrites on every update. Loading one moves its manifest, provenance
//     included, and nobody else's files change.
//   - SourceTrees is set only by MigrateChain, which `ynd migrate` runs. A
//     harness in a user's repository or a cached clone of someone else's is
//     never renamed as a side effect of reading it: `ynh run` in a git working
//     copy must not leave a hundred-line diff behind. Those trees keep working
//     through the read fallback and print a deprecation warning that names
//     `ynd migrate` instead.
//
// The rename is a single os.Rename of the directory. Nothing is copied,
// merged or deleted, so there is no state in which a manifest exists twice or
// not at all. When the move cannot be made that simply, the tree is left alone
// and ManifestDirBlocked says why.
type ManifestDirMigrator struct {
	// SourceTrees allows the rename outside config.HarnessesDir(). Only
	// `ynd migrate`, an explicit request from the tree's owner, sets it.
	SourceTrees bool
}

func (ManifestDirMigrator) Description() string {
	return "manifest dir: " + plugin.LegacyPluginDir + "/ → " + plugin.PluginDir + "/"
}

func (m ManifestDirMigrator) Applies(dir string) bool {
	if !m.SourceTrees && !insideHarnessesDir(dir) {
		return false
	}
	action, _ := manifestDirMigration(dir)
	return action == manifestDirMove
}

func (m ManifestDirMigrator) Run(dir string) error {
	// Decide again rather than trusting an earlier Applies: the tree may have
	// changed in between, and the mover must only ever see a tree the
	// predicate cleared just now.
	if !m.SourceTrees && !insideHarnessesDir(dir) {
		return fmt.Errorf("%s is not a ynh install; run ynd migrate on it", dir)
	}
	if action, reason := manifestDirMigration(dir); action != manifestDirMove {
		if reason == "" {
			reason = "nothing to move"
		}
		return fmt.Errorf("not moving %s: %s", filepath.Join(dir, plugin.LegacyPluginDir), reason)
	}
	return moveManifestDir(dir)
}

// ManifestDirBlocked reports why dir's .ynh-plugin/ cannot be moved, or ""
// when there is nothing to report: no .ynh-plugin/, or one that can be moved.
func ManifestDirBlocked(dir string) string {
	action, reason := manifestDirMigration(dir)
	if action != manifestDirBlocked {
		return ""
	}
	return reason
}

type manifestDirAction int

const (
	// manifestDirNone: no .ynh-plugin/ holding a manifest file.
	manifestDirNone manifestDirAction = iota
	// manifestDirMove: .ynh-plugin/ can be renamed to .agents/harness/.
	manifestDirMove
	// manifestDirBlocked: .ynh-plugin/ exists but is left where it is.
	manifestDirBlocked
)

// manifestDirMigration decides what to do with dir's .ynh-plugin/. A pure
// predicate: it only stats and lists, and never touches what it inspects, per
// .claude/rules/destructive-operations.md. Lstat throughout, so a symlink is
// judged as a symlink and never followed out of the tree.
//
// It refuses rather than guesses:
//   - .ynh-plugin is a symlink or not a directory.
//   - .ynh-plugin holds a symlink. A relative link resolves differently one
//     level deeper, so the move would silently repoint it.
//   - .agents/harness already exists, in any form. Merging two manifest
//     directories means choosing between files, which is the owner's call;
//     ynd validate already lists every file in the wrong one.
//   - .agents exists as a symlink or a file, so the rename would land outside
//     the tree or fail half way.
func manifestDirMigration(dir string) (manifestDirAction, string) {
	legacy := filepath.Join(dir, plugin.LegacyPluginDir)
	fi, err := os.Lstat(legacy)
	if err != nil {
		return manifestDirNone, ""
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return manifestDirBlocked, plugin.LegacyPluginDir + " is a symlink; move its contents to " + plugin.PluginDir + " by hand"
	}
	if !fi.IsDir() {
		return manifestDirBlocked, plugin.LegacyPluginDir + " is not a directory"
	}

	entries, err := os.ReadDir(legacy)
	if err != nil {
		return manifestDirBlocked, fmt.Sprintf("cannot read %s: %v", plugin.LegacyPluginDir, err)
	}
	holdsManifest := false
	for _, e := range entries {
		if e.Type()&os.ModeSymlink != 0 {
			return manifestDirBlocked, fmt.Sprintf("%s/%s is a symlink, which would point somewhere else after the move; move it by hand",
				plugin.LegacyPluginDir, e.Name())
		}
		switch e.Name() {
		case plugin.PluginFile, plugin.InstalledFile, plugin.MarketplaceFile:
			holdsManifest = true
		}
	}
	if !holdsManifest {
		// Not something ynh wrote. Leave it, and say nothing: there is no
		// manifest here to warn about either.
		return manifestDirNone, ""
	}

	agents := filepath.Join(dir, plugin.AgentsDir)
	if ai, err := os.Lstat(agents); err == nil {
		if ai.Mode()&os.ModeSymlink != 0 {
			return manifestDirBlocked, plugin.AgentsDir + " is a symlink; move " + plugin.LegacyPluginDir + " to " + plugin.PluginDir + " by hand"
		}
		if !ai.IsDir() {
			return manifestDirBlocked, plugin.AgentsDir + " is not a directory"
		}
	}
	if _, err := os.Lstat(filepath.Join(dir, plugin.PluginDir)); err == nil {
		return manifestDirBlocked, "both " + plugin.PluginDir + " and " + plugin.LegacyPluginDir +
			" exist; nothing was merged. Move what you need from " + plugin.LegacyPluginDir +
			" into " + plugin.PluginDir + " and remove " + plugin.LegacyPluginDir
	}
	return manifestDirMove, ""
}

// moveManifestDir renames dir/.ynh-plugin to dir/.agents/harness. It makes
// no decisions: callers run manifestDirMigration first and call this only on
// manifestDirMove.
func moveManifestDir(dir string) error {
	if err := os.MkdirAll(filepath.Join(dir, plugin.AgentsDir), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", plugin.AgentsDir, err)
	}
	from := filepath.Join(dir, plugin.LegacyPluginDir)
	to := filepath.Join(dir, plugin.PluginDir)
	if err := os.Rename(from, to); err != nil {
		return fmt.Errorf("moving %s to %s: %w", from, to, err)
	}
	return nil
}

// AdoptRefreshedManifest finishes `ynh update` on an install whose upstream is
// still on .ynh-plugin/ (#404).
//
// Update overlays the refreshed source onto the install. Once the install has
// been moved to .agents/harness/, that overlay recreates .ynh-plugin/ holding
// the fresh plugin.json, while the stale one in .agents/harness/ is the one
// that wins. This moves each fresh manifest file the overlay wrote into
// .agents/harness/, replacing the stale copy exactly as the overlay replaces
// every other file, and removes .ynh-plugin/ only if that leaves it empty.
// installed.json is never touched: it is ynh's record, not upstream's.
//
// Installs only: a dst outside config.HarnessesDir() is left alone.
func AdoptRefreshedManifest(src, dst string) error {
	for _, f := range refreshedManifestFiles(src, dst) {
		from := filepath.Join(dst, plugin.LegacyPluginDir, f)
		to := filepath.Join(dst, plugin.PluginDir, f)
		if err := os.Rename(from, to); err != nil {
			return fmt.Errorf("moving refreshed %s into %s: %w", f, plugin.PluginDir, err)
		}
	}
	legacy := filepath.Join(dst, plugin.LegacyPluginDir)
	if fi, err := os.Lstat(legacy); err == nil && fi.IsDir() {
		if entries, err := os.ReadDir(legacy); err == nil && len(entries) == 0 {
			if err := os.Remove(legacy); err != nil {
				return fmt.Errorf("removing empty %s: %w", legacy, err)
			}
		}
	}
	return nil
}

// refreshedManifestFiles decides which files AdoptRefreshedManifest moves: the
// manifest files src ships under .ynh-plugin/ that the overlay wrote to dst's
// .ynh-plugin/ as regular files, when dst is an install whose manifest is in
// .agents/harness/. A pure predicate, like manifestDirMigration.
func refreshedManifestFiles(src, dst string) []string {
	if !insideHarnessesDir(dst) {
		return nil
	}
	if _, err := os.Lstat(filepath.Join(src, plugin.PluginDir, plugin.PluginFile)); err == nil {
		return nil // upstream has moved; nothing stale can win
	}
	if !isRegular(filepath.Join(src, plugin.LegacyPluginDir, plugin.PluginFile)) ||
		!isRegular(filepath.Join(dst, plugin.PluginDir, plugin.PluginFile)) {
		return nil
	}
	if fi, err := os.Lstat(filepath.Join(dst, plugin.LegacyPluginDir)); err != nil || !fi.IsDir() {
		return nil
	}
	var out []string
	for _, f := range []string{plugin.PluginFile, plugin.MarketplaceFile} {
		if isRegular(filepath.Join(src, plugin.LegacyPluginDir, f)) &&
			isRegular(filepath.Join(dst, plugin.LegacyPluginDir, f)) {
			out = append(out, f)
		}
	}
	return out
}

// isRegular reports whether path is a regular file, not following a symlink.
func isRegular(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.Mode().IsRegular()
}

// insideHarnessesDir reports whether dir is strictly inside
// config.HarnessesDir(), where ynh keeps the trees it installed. Decided from
// the path alone.
func insideHarnessesDir(dir string) bool {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	root, err := filepath.Abs(config.HarnessesDir())
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// ManifestDirDeprecation is the warning for a harness whose manifest was read
// from .ynh-plugin/, or "" when none is owed. Commands print it to stderr; see
// plugin.SetLegacyManifestDirNotice.
//
// The advice depends on who can act on the tree:
//   - An install under config.HarnessesDir() that can be moved gets no
//     warning: the format chain moves it on this load or the next, and there
//     is nothing for the user to do.
//   - A cached clone under config.CacheDir() is someone else's repository, and
//     ynd migrate on it would be undone by the next fetch, so the advice is
//     for its maintainer.
//   - A tree ynd migrate would leave alone gets the reason instead.
//   - Anything else is the user's own tree: run ynd migrate on it.
func ManifestDirDeprecation(dir string) string {
	blocked := ManifestDirBlocked(dir)
	if blocked == "" && insideHarnessesDir(dir) {
		return ""
	}
	// Name the tree absolutely: "." is no help once the warning is read
	// out of context.
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	head := fmt.Sprintf("warning: %s keeps its manifest in %s/, which is deprecated and will stop being read in a later release.",
		dir, plugin.LegacyPluginDir)
	switch {
	case within(config.CacheDir(), dir):
		return head + "\n  It is a cached copy of a remote harness; ask its maintainer to run `ynd migrate` and publish the result."
	case blocked != "":
		return head + "\n  To fix: " + blocked + "."
	default:
		return head + "\n  To fix: ynd migrate " + dir
	}
}

// ManifestDirNotice returns a callback for plugin.SetLegacyManifestDirNotice
// that writes ManifestDirDeprecation to w, which commands pass as stderr so a
// structured response on stdout is never touched.
func ManifestDirNotice(w io.Writer) func(dir string) {
	return func(dir string) {
		if msg := ManifestDirDeprecation(dir); msg != "" {
			_, _ = fmt.Fprintln(w, msg)
		}
	}
}

// within reports whether path is root or inside it.
func within(root, path string) bool {
	r, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	p, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(r, p)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
