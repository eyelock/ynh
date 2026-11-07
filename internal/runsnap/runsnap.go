// Package runsnap turns a harness's run directory into content-addressed
// snapshots behind a stable link (ynh #765).
//
// A run assembles into a staging folder, hashes the tree, and seals it as
// <run>/.snapshots/<name>/<hash>, reusing the snapshot already there when the
// hash matches. <run>/<name> is then swapped, atomically, to a symlink to the
// snapshot. A session that already holds a snapshot keeps reading it
// unchanged however many later runs build or reuse others, because nothing
// ever rewrites a sealed snapshot's path.
//
// Every function here that changes <run>/<name> or .snapshots/<name> must be
// called under the caller's assembly lock (assembler.LockRunDir) for that
// name. This package never deletes a snapshot: until a session carries its
// snapshot's lock for its whole life, a deletion could remove a snapshot a live
// session is reading. Only stale staging folders, which no one else can hold
// under the assembly lock, are cleared.
package runsnap

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// SnapshotsDirName is the folder under the run directory that holds every
// snapshot. Code that lists the run directory must skip it.
const SnapshotsDirName = ".snapshots"

const stagingPrefix = ".staging-"

// Snapshot is a sealed run directory.
type Snapshot struct {
	Name   string // the run-dir name, as in <run>/<name>
	Hash   string // hex SHA-256 of the tree
	Dir    string // the snapshot's real path
	Link   string // the stable link, <run>/<name>
	Reused bool   // an existing snapshot with this hash was used

	release func()
}

// Release drops the snapshot's shared lock. Safe to call more than once.
func (s *Snapshot) Release() {
	if s != nil && s.release != nil {
		s.release()
		s.release = nil
	}
}

// IsSnapshotsDir reports whether a run-directory entry name is the snapshot
// store rather than a harness.
func IsSnapshotsDir(name string) bool { return name == SnapshotsDirName }

// Stage makes an empty staging folder (0700) for name's next assembly, first
// clearing staging folders an earlier, interrupted run left behind. The caller
// must hold the assembly lock: that is what makes those folders safe to clear.
func Stage(runRoot, name string) (string, error) {
	base := filepath.Join(runRoot, SnapshotsDirName, name)
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", fmt.Errorf("creating snapshot dir: %w", err)
	}
	if entries, err := os.ReadDir(base); err == nil {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), stagingPrefix) {
				_ = os.RemoveAll(filepath.Join(base, e.Name()))
			}
		}
	}
	dir, err := os.MkdirTemp(base, stagingPrefix)
	if err != nil {
		return "", fmt.Errorf("creating staging dir: %w", err)
	}
	return dir, nil
}

// HashTree hashes a folder by relative path, file type and permission bits,
// and content (a symlink by its target). Modification times and the folder
// root's own mode do not count, so identical inputs hash identically.
func HashTree(root string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		mode := info.Mode()
		_, _ = fmt.Fprintf(h, "%q %o ", filepath.ToSlash(rel), uint32(mode.Type())|uint32(mode.Perm()))
		switch {
		case mode&os.ModeSymlink != 0:
			t, err := os.Readlink(path)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(h, "L %q\n", t)
		case mode.IsRegular():
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			n, cerr := io.Copy(h, f)
			_ = f.Close()
			if cerr != nil {
				return cerr
			}
			_, _ = fmt.Fprintf(h, "F %d\n", n)
		default:
			_, _ = fmt.Fprintf(h, "D\n")
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("hashing %s: %w", root, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Seal hashes the staged tree, seals it as a snapshot (or discards it and
// reuses the existing snapshot with the same hash), takes a shared lock on the
// snapshot's lock file at lockPath(hash), and swaps <runRoot>/<name> to a link
// to the snapshot. On error nothing is left locked and the staging folder is
// removed.
func Seal(runRoot, name, staging string, lockPath func(hash string) string) (*Snapshot, error) {
	fail := func(err error) (*Snapshot, error) {
		_ = os.RemoveAll(staging)
		return nil, err
	}
	hash, err := HashTree(staging)
	if err != nil {
		return fail(err)
	}
	dest := filepath.Join(runRoot, SnapshotsDirName, name, hash)
	reused := false
	if fi, err := os.Lstat(dest); err == nil && fi.IsDir() {
		reused = true
		_ = os.RemoveAll(staging)
	} else {
		if err := os.Chmod(staging, 0o700); err != nil {
			return fail(fmt.Errorf("sealing snapshot: %w", err))
		}
		if err := os.Rename(staging, dest); err != nil {
			return fail(fmt.Errorf("sealing snapshot: %w", err))
		}
	}
	release, err := LockShared(lockPath(hash))
	if err != nil {
		return nil, err
	}
	link := filepath.Join(runRoot, name)
	if err := swapLink(link, filepath.Join(SnapshotsDirName, name, hash)); err != nil {
		release()
		return nil, err
	}
	return &Snapshot{Name: name, Hash: hash, Dir: dest, Link: link, Reused: reused, release: release}, nil
}

// swapLink makes link a symlink to target (relative to link's folder) such
// that a reader resolving link sees the old target or the new one, never
// nothing. A real directory left by a ynh from before snapshots is removed
// once; that single swap is not atomic.
func swapLink(link, target string) error {
	if fi, err := os.Lstat(link); err == nil && fi.Mode()&os.ModeSymlink == 0 {
		if err := os.RemoveAll(link); err != nil {
			return fmt.Errorf("replacing run dir %s: %w", link, err)
		}
	}
	tmp := fmt.Sprintf("%s.tmp-%d", link, os.Getpid())
	_ = os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return fmt.Errorf("creating run link: %w", err)
	}
	if err := os.Rename(tmp, link); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("swapping run link: %w", err)
	}
	return nil
}
