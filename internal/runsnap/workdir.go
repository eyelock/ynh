package runsnap

import (
	"fmt"
	"os"
	"path/filepath"
)

// WorkDirsName is the folder under SnapshotsDirName that holds the stable
// working folders (see WorkDir). Code that lists SnapshotsDirName's children
// (a pruner) must skip it: it holds links, not snapshots. It cannot clash with a harness's snapshot
// folder: run-dir names start with an id prefix or "_inline-".
const WorkDirsName = ".work"

// WorkDir returns a real folder for name that never changes path, whose
// top-level entries are symlinks into snap (the snapshot's real path), and
// points them at snap, replacing the entries of the previous snapshot.
//
// Codex and Cursor key state by the directory they start in: Codex picks the
// session for `resume --last` by working directory, and Cursor asks for
// Workspace Trust once per directory. A snapshot is a new folder whenever the
// harness changes, so starting in it would lose both. Starting in this folder
// keeps them, and what they read still comes from the newest snapshot.
//
// The folders named in realDirs (".codex", ".cursor") are real folders whose
// own entries are the links, so the file a vendor keys a hook or rule by stays
// a path under the stable folder rather than resolving into a snapshot.
//
// Like swapLink, it must be called under the caller's assembly lock for name.
// Each entry is replaced atomically; the set as a whole is not.
func WorkDir(runRoot, name, snap string, realDirs ...string) (string, error) {
	dir := filepath.Join(runRoot, SnapshotsDirName, WorkDirsName, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating working folder: %w", err)
	}
	real := map[string]bool{}
	for _, d := range realDirs {
		real[d] = true
	}
	if err := syncLinks(dir, snap, real); err != nil {
		return "", err
	}
	return dir, nil
}

// syncLinks makes dir hold exactly one link per entry of src, each to the
// same-named entry of src, except that an entry named in real, when it is a
// folder, becomes a real folder of links.
func syncLinks(dir, src string, real map[string]bool) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("reading snapshot: %w", err)
	}
	want := map[string]bool{}
	for _, e := range entries {
		want[e.Name()] = true
		target := filepath.Join(src, e.Name())
		link := filepath.Join(dir, e.Name())
		if real[e.Name()] && e.IsDir() {
			if fi, err := os.Lstat(link); err == nil && !fi.IsDir() {
				if err := os.Remove(link); err != nil {
					return fmt.Errorf("replacing %s: %w", link, err)
				}
			}
			if err := os.MkdirAll(link, 0o700); err != nil {
				return fmt.Errorf("creating %s: %w", link, err)
			}
			if err := syncLinks(link, target, nil); err != nil {
				return err
			}
			continue
		}
		if err := setLink(link, target); err != nil {
			return err
		}
	}
	old, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading working folder: %w", err)
	}
	for _, e := range old {
		if !want[e.Name()] {
			if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
				return fmt.Errorf("removing stale %s: %w", e.Name(), err)
			}
		}
	}
	return nil
}

// setLink makes link a symlink to target, atomically when link is already a
// symlink; a real file or folder in its way is removed first.
func setLink(link, target string) error {
	if cur, err := os.Readlink(link); err == nil && cur == target {
		return nil
	} else if err != nil {
		if _, lerr := os.Lstat(link); lerr == nil {
			if err := os.RemoveAll(link); err != nil {
				return fmt.Errorf("replacing %s: %w", link, err)
			}
		}
	}
	tmp := fmt.Sprintf("%s.tmp-%d", link, os.Getpid())
	_ = os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return fmt.Errorf("creating link %s: %w", link, err)
	}
	if err := os.Rename(tmp, link); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("swapping link %s: %w", link, err)
	}
	return nil
}
